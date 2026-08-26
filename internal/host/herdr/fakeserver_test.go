package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/host"
)

// fakeHerdr is a scripted Herdr server: a real Unix socket speaking the
// 0.8.2 NDJSON wire shapes, with the behaviors the P7 probes recorded
// (connection closed after every non-stream response, agent_pane_busy
// refusals, partial pane.created backfill) driveable from a test.
type fakeHerdr struct {
	t    *testing.T
	path string
	ln   net.Listener

	mu              sync.Mutex
	version         string
	protocol        int
	panes           []fakePaneState
	workspaces      []string
	focusedPane     string
	nextPane        int
	nextTerminal    int
	agentStartBusy  int
	agentStartPID   int
	agentStartErr   *wireError
	paneCloseErr    *wireError
	connections     int
	calls           []string
	lastRequestID   string
	lastEnv         map[string]string
	lastSplit       paneSplitParams
	lastTab         tabCreateParams
	tabCloseErr     *wireError
	nextTab         int
	lastAgentStart  agentStartParams
	agents          []fakeAgent
	agentPromptErr  *wireError
	dropPrompt      bool
	lastAgentPrompt agentPromptParams
	backfill        []string
	subscribers     []net.Conn
	stopped         bool
}

type fakePaneState struct {
	paneID      string
	terminalID  string
	workspaceID string
	tabID       string
	shellPID    int
	fgPID       int
}

type fakeAgent struct {
	name             string
	paneID           string
	session          *agentSessionInfo
	launchPending    bool
	interactiveReady bool
}

func newFakeHerdr(t *testing.T) *fakeHerdr {
	t.Helper()
	dir := shortTempDir(t)
	f := &fakeHerdr{
		t:            t,
		path:         filepath.Join(dir, "h.sock"),
		version:      PinnedVersion,
		protocol:     PinnedProtocol,
		nextPane:     1,
		nextTerminal: 1,
	}
	ln, err := net.Listen("unix", f.path)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f.ln = ln
	go f.serve()
	t.Cleanup(f.stop)
	return f
}

// shortTempDir keeps a socket path inside sun_path's ~104 byte budget,
// which a deeply nested TMPDIR can blow on its own.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "duoherdr")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	if len(dir) > 80 {
		alt, altErr := os.MkdirTemp("/tmp", "duoherdr")
		if altErr != nil {
			t.Fatalf("temp dir: %v", altErr)
		}
		dir = alt
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func (f *fakeHerdr) socketPath() string { return f.path }

func (f *fakeHerdr) stop() {
	f.mu.Lock()
	if f.stopped {
		f.mu.Unlock()
		return
	}
	f.stopped = true
	subs := f.subscribers
	f.subscribers = nil
	f.mu.Unlock()
	_ = f.ln.Close()
	for _, c := range subs {
		_ = c.Close()
	}
}

func (f *fakeHerdr) serve() {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return
		}
		f.mu.Lock()
		f.connections++
		f.mu.Unlock()
		go f.handle(conn)
	}
}

func (f *fakeHerdr) handle(conn net.Conn) {
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		_ = conn.Close()
		return
	}
	var req struct {
		ID     string          `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.Unmarshal(line, &req); err != nil {
		_ = conn.Close()
		return
	}
	f.mu.Lock()
	f.calls = append(f.calls, req.Method)
	f.lastRequestID = req.ID
	f.mu.Unlock()

	if req.ID == "" {
		// 0.8.2 requires an id on every request.
		f.respondError(conn, "", CodeInvalidRequest, "missing field `id`")
		_ = conn.Close()
		return
	}

	if req.Method == "events.subscribe" {
		f.serveSubscription(conn, req.ID)
		return
	}

	f.mu.Lock()
	dropPrompt := req.Method == "agent.prompt" && f.dropPrompt
	f.mu.Unlock()
	if dropPrompt {
		// Request reached the server; the response never comes back.
		_ = conn.Close()
		return
	}

	result, wireErr := f.dispatch(req.Method, req.Params)
	if wireErr != nil {
		f.respondError(conn, req.ID, wireErr.Code, wireErr.Message)
	} else {
		f.respond(conn, req.ID, result)
	}
	// Herdr closes the connection after every non-streaming response.
	_ = conn.Close()
}

func (f *fakeHerdr) serveSubscription(conn net.Conn, id string) {
	// Register before the ack. subscribe() returns as soon as it reads
	// subscription_started; emitEvent after that return must already see
	// this conn, or the event is dropped and stream.next() blocks until
	// the suite timeout (TestSubscribeStreamsOnOneConnection).
	f.mu.Lock()
	backfill := append([]string(nil), f.backfill...)
	f.subscribers = append(f.subscribers, conn)
	f.mu.Unlock()
	f.respond(conn, id, map[string]any{"type": "subscription_started"})

	// Partial, indistinguishable-from-real backfill, exactly as 0.8.2
	// replays it: only API-created panes of this server run, never a
	// restored pane, and the synthetic event looks like a fresh creation.
	for _, paneID := range backfill {
		f.writeEvent(conn, "pane_created", map[string]any{
			"type": "pane_created",
			"pane": map[string]any{
				"pane_id":      paneID,
				"terminal_id":  "term_backfill",
				"workspace_id": "w1",
				"tab_id":       "w1:t1",
				"focused":      false,
				"agent_status": "unknown",
				"revision":     0,
			},
		})
	}
}

func (f *fakeHerdr) dispatch(method string, params json.RawMessage) (any, *wireError) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "ping":
		return map[string]any{"type": "pong", "version": f.version, "protocol": f.protocol}, nil
	case "session.snapshot":
		return map[string]any{"type": "session_snapshot", "snapshot": f.snapshotLocked()}, nil
	case "pane.get":
		var p paneTargetParams
		_ = json.Unmarshal(params, &p)
		pane, ok := f.findLocked(p.PaneID)
		if !ok {
			return nil, &wireError{Code: CodePaneNotFound, Message: "pane " + p.PaneID + " not found"}
		}
		return map[string]any{"type": "pane_info", "pane": paneJSON(pane)}, nil
	case "pane.process_info":
		var p paneTargetParams
		_ = json.Unmarshal(params, &p)
		pane, ok := f.findLocked(p.PaneID)
		if !ok {
			return nil, &wireError{Code: CodePaneNotFound, Message: "pane " + p.PaneID + " not found"}
		}
		fg := []map[string]any{}
		if pane.fgPID > 0 {
			fg = append(fg, map[string]any{"pid": pane.fgPID, "name": "fake"})
		}
		return map[string]any{"type": "pane_process_info", "process_info": map[string]any{
			"pane_id":              pane.paneID,
			"shell_pid":            pane.shellPID,
			"foreground_processes": fg,
		}}, nil
	case "workspace.create":
		var p workspaceCreateParams
		_ = json.Unmarshal(params, &p)
		f.lastEnv = p.Env
		pane := f.addPaneLocked("w" + fmt.Sprint(len(f.workspaces)+1))
		return map[string]any{"type": "workspace_created", "root_pane": paneJSON(pane)}, nil
	case "pane.split":
		var p paneSplitParams
		_ = json.Unmarshal(params, &p)
		f.lastEnv = p.Env
		f.lastSplit = p
		pane := f.addPaneLocked("w1")
		return map[string]any{"type": "pane_info", "pane": paneJSON(pane)}, nil
	case "tab.create":
		var p tabCreateParams
		_ = json.Unmarshal(params, &p)
		f.lastEnv = p.Env
		f.lastTab = p
		ws := p.WorkspaceID
		if ws == "" {
			ws = "w1"
		}
		f.nextTab++
		tabID := fmt.Sprintf("%s:t%d", ws, f.nextTab+1)
		pane := f.addPaneLocked(ws)
		f.panes[len(f.panes)-1].tabID = tabID
		pane.tabID = tabID
		return map[string]any{
			"type":      "tab_created",
			"tab":       map[string]any{"tab_id": pane.tabID, "label": p.Label, "focused": false},
			"root_pane": paneJSON(pane),
		}, nil
	case "tab.close":
		var p tabTargetParams
		_ = json.Unmarshal(params, &p)
		if f.tabCloseErr != nil {
			return nil, f.tabCloseErr
		}
		kept := f.panes[:0]
		closed := false
		for _, pane := range f.panes {
			if pane.tabID == p.TabID {
				closed = true
				continue
			}
			kept = append(kept, pane)
		}
		f.panes = kept
		if !closed {
			return nil, &wireError{Code: "tab_not_found", Message: "tab " + p.TabID + " not found"}
		}
		return map[string]any{"type": "tab_closed", "tab_id": p.TabID}, nil
	case "pane.close":
		var p paneTargetParams
		_ = json.Unmarshal(params, &p)
		if f.paneCloseErr != nil {
			return nil, f.paneCloseErr
		}
		if _, ok := f.findLocked(p.PaneID); !ok {
			return nil, &wireError{Code: CodePaneNotFound, Message: "pane " + p.PaneID + " not found"}
		}
		kept := f.panes[:0]
		for _, pane := range f.panes {
			if pane.paneID != p.PaneID {
				kept = append(kept, pane)
			}
		}
		f.panes = kept
		return map[string]any{"type": "pane_closed", "pane_id": p.PaneID}, nil
	case "agent.start":
		var p agentStartParams
		_ = json.Unmarshal(params, &p)
		f.lastAgentStart = p
		if f.agentStartErr != nil {
			return nil, f.agentStartErr
		}
		if f.agentStartBusy > 0 {
			f.agentStartBusy--
			return nil, &wireError{Code: CodeAgentPaneBusy, Message: "pane is not at an interactive prompt"}
		}
		if f.agentStartPID != 0 {
			for i := range f.panes {
				if f.panes[i].paneID == p.PaneID {
					f.panes[i].fgPID = f.agentStartPID
				}
			}
		}
		// agent.start returns immediately with launch_pending at 0.8.2.
		f.agents = append(f.agents, fakeAgent{
			name:          p.Name,
			paneID:        p.PaneID,
			launchPending: true,
		})
		return map[string]any{"type": "agent_started", "argv": []string{p.Kind}}, nil
	case "agent.list":
		agents := make([]map[string]any, 0, len(f.agents))
		for _, a := range f.agents {
			agents = append(agents, agentJSON(a))
		}
		return map[string]any{"type": "agent_list", "agents": agents}, nil
	case "agent.prompt":
		var p agentPromptParams
		_ = json.Unmarshal(params, &p)
		f.lastAgentPrompt = p
		if f.agentPromptErr != nil {
			return nil, f.agentPromptErr
		}
		if p.Text == "" {
			return nil, &wireError{Code: CodeEmptyAgentPrompt, Message: "empty prompt"}
		}
		found := false
		for _, a := range f.agents {
			if a.name == p.Target {
				found = true
				break
			}
		}
		if !found {
			return nil, &wireError{Code: CodeAgentNotReady, Message: "not an active named agent"}
		}
		return map[string]any{
			"type":  "agent_prompted",
			"agent": map[string]any{"name": p.Target},
		}, nil
	default:
		return nil, &wireError{Code: "unknown_method", Message: method}
	}
}

func (f *fakeHerdr) snapshotLocked() map[string]any {
	panes := make([]map[string]any, 0, len(f.panes))
	for _, p := range f.panes {
		panes = append(panes, paneJSON(p))
	}
	workspaces := make([]map[string]any, 0, len(f.workspaces))
	for _, w := range f.workspaces {
		workspaces = append(workspaces, map[string]any{"workspace_id": w})
	}
	return map[string]any{
		"version":         f.version,
		"protocol":        f.protocol,
		"workspaces":      workspaces,
		"tabs":            []any{},
		"panes":           panes,
		"layouts":         []any{},
		"agents":          []any{},
		"focused_pane_id": f.focusedPane,
	}
}

func paneJSON(p fakePaneState) map[string]any {
	// Live 0.8.2 pane records carry no agent_session (notes/19 §1).
	// Identity and launch_pending / interactive_ready live on the agent
	// record; do not invent pane fields the probe shape lacks.
	return map[string]any{
		"pane_id":      p.paneID,
		"terminal_id":  p.terminalID,
		"workspace_id": p.workspaceID,
		"tab_id":       p.tabID,
		"focused":      false,
		"agent_status": "unknown",
		"revision":     0,
	}
}

func agentJSON(a fakeAgent) map[string]any {
	out := map[string]any{
		"name":              a.name,
		"pane_id":           a.paneID,
		"launch_pending":    a.launchPending,
		"interactive_ready": a.interactiveReady,
	}
	if a.session != nil {
		out["agent_session"] = map[string]any{
			"source": a.session.Source,
			"agent":  a.session.Agent,
			"kind":   a.session.Kind,
			"value":  a.session.Value,
		}
	}
	return out
}

func (f *fakeHerdr) findLocked(paneID string) (fakePaneState, bool) {
	for _, p := range f.panes {
		if p.paneID == paneID {
			return p, true
		}
	}
	return fakePaneState{}, false
}

func (f *fakeHerdr) addPaneLocked(workspaceID string) fakePaneState {
	pane := fakePaneState{
		paneID:      fmt.Sprintf("%s:p%d", workspaceID, f.nextPane),
		terminalID:  fmt.Sprintf("term_%d", f.nextTerminal),
		workspaceID: workspaceID,
		tabID:       workspaceID + ":t1",
		shellPID:    5000 + f.nextPane,
		fgPID:       5000 + f.nextPane,
	}
	f.nextPane++
	f.nextTerminal++
	f.panes = append(f.panes, pane)
	if !containsString(f.workspaces, workspaceID) {
		f.workspaces = append(f.workspaces, workspaceID)
	}
	if f.focusedPane == "" {
		f.focusedPane = pane.paneID
	}
	return pane
}

// Test-side controls.

func (f *fakeHerdr) addPane(workspaceID string) fakePaneState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addPaneLocked(workspaceID)
}

func (f *fakeHerdr) removePane(paneID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.panes[:0]
	for _, p := range f.panes {
		if p.paneID != paneID {
			kept = append(kept, p)
		}
	}
	f.panes = kept
}

func (f *fakeHerdr) mutatePane(paneID string, mutate func(*fakePaneState)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.panes {
		if f.panes[i].paneID == paneID {
			mutate(&f.panes[i])
			return
		}
	}
	f.t.Fatalf("fake herdr: pane %s not found", paneID)
}

func (f *fakeHerdr) setVersion(version string, protocol int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.version = version
	f.protocol = protocol
}

func (f *fakeHerdr) setAgentStartBusy(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentStartBusy = n
}

// setAgentStartPID makes a successful agent.start replace the pane's
// foreground process, the way a real agent takes over the terminal a
// moment after the asynchronous start returns.
func (f *fakeHerdr) setAgentStartPID(pid int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentStartPID = pid
}

func (f *fakeHerdr) setAgentStartError(code, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentStartErr = &wireError{Code: code, Message: message}
}

func (f *fakeHerdr) setPaneCloseError(code, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paneCloseErr = &wireError{Code: code, Message: message}
}

func (f *fakeHerdr) paneCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.panes)
}

func (f *fakeHerdr) lastID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastRequestID
}

func (f *fakeHerdr) setBackfill(paneIDs ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backfill = paneIDs
}

func (f *fakeHerdr) connCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.connections
}

func (f *fakeHerdr) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == method {
			n++
		}
	}
	return n
}

func (f *fakeHerdr) mutatingCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		switch c {
		case "workspace.create", "tab.create", "tab.close", "pane.split", "agent.start", "agent.prompt", "pane.send_input", "pane.send_text":
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeHerdr) envOfLastCreate() map[string]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastEnv
}

func (f *fakeHerdr) lastTabParams() tabCreateParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastTab
}

func (f *fakeHerdr) setTabCloseError(code, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.tabCloseErr = &wireError{Code: code, Message: message}
}

func (f *fakeHerdr) lastAgentStartParams() agentStartParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastAgentStart
}

func (f *fakeHerdr) addAgent(name, paneID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents = append(f.agents, fakeAgent{name: name, paneID: paneID})
}

// setAgentBindState sets the 0.8.2 agent-record fields the bind pass
// decodes (session identity + D3 readiness) for an existing agent row.
func (f *fakeHerdr) setAgentBindState(paneID string, session *agentSessionInfo, launchPending, interactiveReady bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.agents {
		if f.agents[i].paneID == paneID {
			f.agents[i].session = session
			f.agents[i].launchPending = launchPending
			f.agents[i].interactiveReady = interactiveReady
			return
		}
	}
	f.t.Fatalf("fake herdr: agent on pane %s not found", paneID)
}

func (f *fakeHerdr) setAgentPromptError(code, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agentPromptErr = &wireError{Code: code, Message: message}
}

func (f *fakeHerdr) setDropPrompt(drop bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropPrompt = drop
}

func (f *fakeHerdr) lastAgentPromptParams() agentPromptParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastAgentPrompt
}

func (f *fakeHerdr) lastSplitParams() paneSplitParams {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSplit
}

// dropSubscribers closes every open subscription connection, simulating a
// stream that dies without the server going away.
func (f *fakeHerdr) dropSubscribers() {
	f.mu.Lock()
	subs := f.subscribers
	f.subscribers = nil
	f.mu.Unlock()
	for _, c := range subs {
		_ = c.Close()
	}
}

// emitEvent pushes one event to every open subscription.
func (f *fakeHerdr) emitEvent(name string, data map[string]any) {
	f.mu.Lock()
	subs := append([]net.Conn(nil), f.subscribers...)
	f.mu.Unlock()
	for _, c := range subs {
		f.writeEvent(c, name, data)
	}
}

func (f *fakeHerdr) writeEvent(conn net.Conn, name string, data map[string]any) {
	payload, err := json.Marshal(map[string]any{"event": name, "data": data})
	if err != nil {
		return
	}
	_, _ = conn.Write(append(payload, '\n'))
}

func (f *fakeHerdr) respond(conn net.Conn, id string, result any) {
	payload, err := json.Marshal(map[string]any{"id": id, "result": result})
	if err != nil {
		return
	}
	_, _ = conn.Write(append(payload, '\n'))
}

func (f *fakeHerdr) respondError(conn net.Conn, id, code, message string) {
	payload, err := json.Marshal(map[string]any{
		"id":    id,
		"error": map[string]string{"code": code, "message": message},
	})
	if err != nil {
		return
	}
	_, _ = conn.Write(append(payload, '\n'))
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// Test fixtures shared by the host tests.

const testInstanceID = "herdr:test"

// fakeBirth is a deterministic ProcessBirthResolver standing in for
// procfs: a PID maps to a fixed, proven start time. Tests that need
// unprovable birth use unprovenBirth instead.
func fakeBirth(_ context.Context, info processInfo) host.ProcessBirthEvidence {
	pid := pidFor(info)
	if pid <= 0 {
		return host.ProcessBirthEvidence{StartTimeSource: StartTimeSourceUnavailable}
	}
	return host.ProcessBirthEvidence{
		PID:             pid,
		StartTime:       time.Unix(int64(1_700_000_000+pid), 0).UTC(),
		StartTimeSource: StartTimeSourceProcfs,
	}
}

// cleanPaneEnviron is the default PaneEnvironResolver for host tests: a
// pane whose inherited environment carries no scrub marker. It is the
// default rather than procfs so every launch test that is not about the
// scrub gate keeps testing what it was written to test, while still going
// through the gate rather than around it — the fake PIDs the fake server
// mints have no /proc entry, and the gate refuses what it cannot read.
func cleanPaneEnviron(_ context.Context, _ int) ([]string, error) {
	return []string{"PATH=/usr/bin", "HOME=/home/tester", "TERM=xterm-256color"}, nil
}

func unprovenBirth(_ context.Context, info processInfo) host.ProcessBirthEvidence {
	return host.ProcessBirthEvidence{PID: pidFor(info), StartTimeSource: StartTimeSourceUnavailable}
}

func testHost(t *testing.T, f *fakeHerdr, mutate ...func(*Config)) *Host {
	t.Helper()
	cfg := Config{
		IntegrationInstanceID: testInstanceID,
		SocketPath:            f.socketPath(),
		CallTimeout:           2 * time.Second,
		SnapshotInterval:      20 * time.Millisecond,
		StartRetryLimit:       5,
		StartRetryDelay:       time.Millisecond,
		LaunchSettleTimeout:   30 * time.Millisecond,
		LaunchSettlePoll:      2 * time.Millisecond,
		ResolveProcessBirth:   fakeBirth,
		ResolvePaneEnviron:    cleanPaneEnviron,
	}
	for _, m := range mutate {
		m(&cfg)
	}
	h, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h
}
