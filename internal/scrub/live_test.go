package scrub

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestLiveScrubPairedTranscriptLoss is conformance §8's paired
// negative/positive spawn-environment test (terminal-multiplexers
// duo-vnext-integration-conformance.md §8, 2026-08-23 amendment), run
// against a real, disposable Herdr server and a real claude binary.
//
// It is opt-in (DUO_SCRUB_LIVE=1) because it spawns claude — real API
// turns, real cost — and a disposable herdr server process. It never
// touches a live user Herdr session: every server it starts gets its own
// XDG_CONFIG_HOME and a uniquely-labelled HERDR_SESSION, exactly like
// internal/host/herdr/live_test.go's isolation, and every resource it
// creates is torn down in t.Cleanup regardless of pass/fail.
//
// # What it proves, and what it does not
//
// The NEGATIVE leg starts a disposable Herdr server whose own spawn
// environment carries the markers (as a wrapping agent harness would
// leave them), then starts an *interactive* claude session in a pane of
// that server and submits one turn. Per docs/adapters/decisions.md
// (Herdr host adapter, "Launch mapping is lossy") every pane inherits
// the server's environment, so this reproduces conformance §8's failure
// signature without Duo ever launching claude directly.
//
// The POSITIVE leg launches a second disposable Herdr server through
// Guard(os.Environ()) — the fail-closed scrub this package exists to
// provide — and repeats the same interactive spawn and turn. The
// transcript-loss signature must not appear.
//
// This test uses interactive claude sessions for both legs, not `claude
// -p`. That is a deliberate, evidence-driven departure from the original
// plan text: a manual live run (recorded in
// testdata/scrub-live-2026-08-23.md) found that a `claude -p` spawn
// through the very same marker-carrying pane does NOT lose its
// transcript at the installed version — only an interactive session
// does. See that file for the full negative-leg comparison across both
// invocation shapes; conformance §8 already anticipates exactly this
// kind of version- and shape-pinned drift ("its expected signature is
// version-pinned evidence, not a universal law").
func TestLiveScrubPairedTranscriptLoss(t *testing.T) {
	if os.Getenv("DUO_SCRUB_LIVE") != "1" {
		t.Skip("set DUO_SCRUB_LIVE=1 to run the live paired transcript-loss test (spawns claude and a disposable herdr server; real API cost)")
	}
	herdrBin, err := exec.LookPath("herdr")
	if err != nil {
		t.Skip("herdr not on PATH; live scrub test cannot run (recorded as debt, see testdata/scrub-live-2026-08-23.md)")
	}
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude not on PATH; live scrub test cannot run (recorded as debt, see testdata/scrub-live-2026-08-23.md)")
	}
	t.Logf("herdr=%s claude=%s", herdrBin, claudeBin)

	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}

	t.Run("negative leg: marker-carrying server, interactive spawn", func(t *testing.T) {
		env := append(Environ(nil), // start from nothing, not the ambient environ
			"HOME="+realHome, // real credentials; only Herdr's own storage is isolated
			"PATH="+os.Getenv("PATH"),
			"TERM="+orDefault(os.Getenv("TERM"), "xterm-256color"),
			"DISABLE_AUTOUPDATER=1",
			// The markers a wrapping harness would leave behind. Real
			// values this project's own live session carries, not
			// placeholders (testdata/scrub-live-2026-08-23.md §0).
			"CLAUDECODE=1",
			"CLAUDE_CODE_CHILD_SESSION=1",
			"AI_AGENT=claude-code-live-scrub-test",
			"CLAUDE_CODE_ENTRYPOINT=cli", // wildcard-covered marker, for full coverage
		)
		srv := startDisposableHerdrServer(t, env)
		result := runPanedTurn(t, srv, "NEGLEGSIGNATURE")

		if !strings.Contains(result.screenText, "Transcript saving is off") {
			t.Errorf("negative leg did not show the expected warning; screen:\n%s", result.screenText)
		}
		if result.transcriptCount != 0 {
			t.Errorf("negative leg wrote %d transcript(s), want 0 (loss signature expected): %v",
				result.transcriptCount, result.transcriptFiles)
		}
		t.Logf("negative leg: session=%s screen_has_warning=%v transcripts=%d",
			result.sessionID, strings.Contains(result.screenText, "Transcript saving is off"), result.transcriptCount)
	})

	t.Run("positive leg: Guard-scrubbed server, interactive spawn", func(t *testing.T) {
		scrubbed, err := Guard(os.Environ())
		if err != nil {
			t.Fatalf("Guard(os.Environ()): %v (this process's own environment could not be scrubbed clean)", err)
		}
		env := append(scrubbed,
			"HOME="+realHome,
			"DISABLE_AUTOUPDATER=1",
		)
		if err := Verify(env); err != nil {
			t.Fatalf("positive leg's own server spawn environment failed Verify before starting the server: %v", err)
		}
		srv := startDisposableHerdrServer(t, env)
		result := runPanedTurn(t, srv, "POSLEGSIGNATURE")

		if strings.Contains(result.screenText, "Transcript saving is off") {
			t.Errorf("positive leg showed the transcript-loss warning; screen:\n%s", result.screenText)
		}
		if result.transcriptCount != 1 {
			t.Errorf("positive leg wrote %d transcript(s), want exactly 1: %v",
				result.transcriptCount, result.transcriptFiles)
		}
		if result.transcriptCount == 1 && !strings.Contains(result.transcriptFiles[0], result.sessionID) {
			t.Errorf("the one transcript (%s) does not name the fresh session id %s",
				result.transcriptFiles[0], result.sessionID)
		}
		t.Logf("positive leg: session=%s transcripts=%v", result.sessionID, result.transcriptFiles)
	})
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// disposableHerdrServer is a running, disposable Herdr server this test
// owns end to end.
type disposableHerdrServer struct {
	socketPath string
	xdgConfig  string
	session    string
}

// startDisposableHerdrServer launches `herdr server` with env as its
// complete spawn environment (no ambient inheritance beyond what env
// itself lists), under a uniquely-labelled, isolated XDG_CONFIG_HOME —
// the same isolation shape internal/host/herdr/live_test.go documents
// for DUO_HERDR_LIVE_SOCKET. It registers cleanup to stop the server and
// remove its scratch directory unconditionally.
func startDisposableHerdrServer(t *testing.T, env []string) disposableHerdrServer {
	t.Helper()
	// A short, non-nested scratch dir: unix socket paths cap sun_path at
	// ~108 bytes on Linux, and t.TempDir() nests under the full
	// (sub)test name, which blows that budget instantly once a
	// herdr/sessions/<session>/herdr.sock suffix is added (observed
	// live: "local socket name length exceeds capacity of sun_path").
	xdg, err := os.MkdirTemp("", "duo-scrub-")
	if err != nil {
		t.Fatalf("create scratch XDG_CONFIG_HOME: %v", err)
	}
	t.Cleanup(func() { removeAllRetrying(xdg) })
	session := "dsl-" + strconv.FormatUint(wireSeq.Add(1), 36)

	// env may still carry ambient HERDR_SESSION / HERDR_SOCKET_PATH /
	// XDG_CONFIG_HOME (Guard only scrubs the CLAUDE_*/CLAUDECODE/AI_AGENT
	// markers, not these) — this process itself runs inside a live Herdr
	// pane, so os.Environ() already has real values for all three.
	// Duplicate entries in an exec.Cmd.Env are implementation-defined
	// (observed live: the ambient HERDR_SESSION won, and the "disposable"
	// server attached to this repo's real live session instead of a
	// fresh one), so strip every prior occurrence before appending the
	// ones this test controls.
	full := append(withoutNames(env, "XDG_CONFIG_HOME", "HERDR_SESSION", "HERDR_SOCKET_PATH"),
		"XDG_CONFIG_HOME="+xdg,
		"HERDR_SESSION="+session,
	)
	if err := Verify(full); err != nil && !hasExactly(full, "HOME", "PATH", "TERM", "DISABLE_AUTOUPDATER", "XDG_CONFIG_HOME", "HERDR_SESSION") {
		// Not fatal on its own (the negative leg deliberately carries
		// markers), just a log line so a reader of -v output can see
		// which leg's server this was without cross-referencing t.Name.
		t.Logf("server spawn environment carries marker(s): %v", err)
	}

	cmd := exec.Command("herdr", "server")
	cmd.Env = full
	logPath := filepath.Join(xdg, "server.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create server log: %v", err)
	}
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	// New session leader so the server outlives this process group and
	// keeps running detached the way a real Duo-owned server would;
	// mirrors the manual `setsid herdr server` shape verified in
	// testdata/scrub-live-2026-08-23.md.
	setSysProcAttrDetached(cmd)

	if err := cmd.Start(); err != nil {
		t.Fatalf("start disposable herdr server: %v", err)
	}
	serverPID := cmd.Process.Pid
	// Don't Wait() on a detached server; reap it only via the stop call
	// below, and otherwise let it run free of this test process's exit.
	go func() { _ = cmd.Process.Release() }()

	sock := filepath.Join(xdg, "herdr", "sessions", session, "herdr.sock")
	deadline := time.Now().Add(10 * time.Second)
	for {
		if fi, statErr := os.Stat(sock); statErr == nil && fi.Mode()&os.ModeSocket != 0 {
			break
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(logPath)
			t.Fatalf("disposable herdr server socket never appeared at %s; server log:\n%s", sock, data)
		}
		time.Sleep(200 * time.Millisecond)
	}

	srv := disposableHerdrServer{socketPath: sock, xdgConfig: xdg, session: session}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = wireCall(ctx, sock, "server.stop", nil, nil)
		time.Sleep(300 * time.Millisecond)
		_ = syscallKill(serverPID) // belt and braces if server.stop didn't land
	})
	return srv
}

// removeAllRetrying removes path, retrying briefly. A just-stopped herdr
// server (or claude, for a project directory) can hold its files open
// for a short window after this test's stop/kill call returns, long
// enough for a single os.RemoveAll to race it and leave an empty
// directory behind (observed live: an empty herdr/sessions/<name>/
// directory survived a single-shot RemoveAll called immediately after
// server.stop). Errors are swallowed after the retries are exhausted:
// this is best-effort teardown of a scratch directory, not a correctness
// requirement, so a stray empty directory failing to vanish must never
// fail the test.
func removeAllRetrying(path string) {
	for i := 0; i < 10; i++ {
		if err := os.RemoveAll(path); err == nil {
			if _, statErr := os.Stat(path); os.IsNotExist(statErr) {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// withoutNames returns env with every entry whose name is in names
// removed, order preserved.
func withoutNames(env []string, names ...string) []string {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	out := make([]string, 0, len(env))
	for _, e := range env {
		if !drop[varName(e)] {
			out = append(out, e)
		}
	}
	return out
}

func hasExactly(env []string, names ...string) bool {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	for _, e := range env {
		if !want[varName(e)] {
			return false
		}
	}
	return true
}

// panedTurnResult is what runPanedTurn observed after one interactive
// prompt/response round trip.
type panedTurnResult struct {
	sessionID       string
	screenText      string
	transcriptCount int
	transcriptFiles []string
}

// runPanedTurn creates a workspace (one pane) on srv, starts claude
// interactively in it, submits one trivial prompt, waits for the reply,
// reads the screen, and reports which transcript file(s) exist for that
// pane's project directory. It never touches a real project: the
// workspace's cwd is a fresh t.TempDir().
func runPanedTurn(t *testing.T, srv disposableHerdrServer, promptWord string) panedTurnResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	projectDir := t.TempDir()

	var created struct {
		Result struct {
			RootPane struct {
				PaneID string `json:"pane_id"`
			} `json:"root_pane"`
		} `json:"result"`
	}
	if err := wireCall(ctx, srv.socketPath, "workspace.create", map[string]any{
		"cwd":   projectDir,
		"label": "duo-scrub-live",
	}, &created.Result); err != nil {
		t.Fatalf("workspace.create: %v", err)
	}
	paneID := created.Result.RootPane.PaneID
	if paneID == "" {
		t.Fatalf("workspace.create returned no pane_id")
	}

	agentName := "duo-scrub-live-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	// agent.start refuses agent_pane_busy while the pane's shell has not
	// reached its interactive prompt yet — proven no-effect and
	// retryable at 0.8.2 (docs/adapters/decisions.md, "startAgent"),
	// which internal/host/herdr.Host.Start also retries on.
	deadline := time.Now().Add(15 * time.Second)
	for {
		err := wireCall(ctx, srv.socketPath, "agent.start", map[string]any{
			"name":    agentName,
			"kind":    "claude",
			"pane_id": paneID,
		}, nil)
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "agent_pane_busy") || time.Now().After(deadline) {
			t.Fatalf("agent.start: %v", err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	// agent.start is asynchronous at 0.8.2 (docs/adapters/decisions.md):
	// it returns launch_pending before the agent process, and its own
	// agent_session, exist. agent.wait is the documented way to block
	// for the interactive session to become ready; agent.get afterward
	// reads back the settled agent_session id.
	if err := wireCall(ctx, srv.socketPath, "agent.wait", map[string]any{
		"target": agentName,
		"until":  []string{"idle", "blocked"},
	}, nil); err != nil {
		t.Logf("agent.wait after agent.start: %v (continuing; the pane read below is the real evidence)", err)
	}

	// agent.get's result payload nests the agent record under "agent"
	// (verified live: {"agent": {"agent_session": {"value": ...}, ...}}).
	var agentInfo struct {
		Agent struct {
			AgentSession struct {
				Value string `json:"value"`
			} `json:"agent_session"`
		} `json:"agent"`
	}
	if err := wireCall(ctx, srv.socketPath, "agent.get", map[string]any{"target": agentName}, &agentInfo); err != nil {
		t.Logf("agent.get: %v", err)
	}
	sessionID := agentInfo.Agent.AgentSession.Value

	promptText := "Reply with exactly the word " + promptWord + " and nothing else."
	waitCtx, waitCancel := context.WithTimeout(ctx, 40*time.Second)
	if err := wireCall(waitCtx, srv.socketPath, "agent.prompt", map[string]any{
		"target": agentName,
		"text":   promptText,
		"wait":   map[string]any{"until": []string{"idle", "blocked"}},
	}, nil); err != nil {
		// agent.prompt's --wait can time out on a fast turn without the
		// turn having failed (observed live,
		// testdata/scrub-live-2026-08-23.md); the pane read below is the
		// actual evidence, not this call's outcome. Still logged, never
		// silently discarded.
		t.Logf("agent.prompt: %v (continuing; the pane read below is the real evidence)", err)
	}
	waitCancel()
	time.Sleep(3 * time.Second)

	// pane.read's result payload nests the actual read under "read"
	// (verified live: {"read": {"text": ..., ...}}), not at the top
	// level.
	var readResult struct {
		Read struct {
			Text string `json:"text"`
		} `json:"read"`
	}
	if err := wireCall(ctx, srv.socketPath, "pane.read", map[string]any{
		"pane_id": paneID,
		"source":  "visible",
		"format":  "text",
	}, &readResult); err != nil {
		t.Fatalf("pane.read: %v", err)
	}

	entries, _ := os.ReadDir(filepath.Join(realClaudeProjectsDir(t), encodeClaudeProjectDir(projectDir)))
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jsonl") {
			files = append(files, e.Name())
		}
	}
	t.Cleanup(func() {
		removeAllRetrying(filepath.Join(realClaudeProjectsDir(t), encodeClaudeProjectDir(projectDir)))
	})

	return panedTurnResult{
		sessionID:       sessionID,
		screenText:      readResult.Read.Text,
		transcriptCount: len(files),
		transcriptFiles: files,
	}
}

// realClaudeProjectsDir is $HOME/.claude/projects. Claude Code's own
// transcript store is not sandboxed by this test (there is no supported
// override that does not itself collide with the scrub markers this
// package removes — CLAUDE_CONFIG_DIR is one of them); each project
// directory this test touches is named from a unique t.TempDir() and
// removed in t.Cleanup, matching the precedent recorded in
// terminal-multiplexers notes/19-herdr-probes.md ("One residual artifact
// remains by design").
func realClaudeProjectsDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return filepath.Join(home, ".claude", "projects")
}

// claudeProjectDirNonAlnum matches encodeClaudeProjectDir's replacement
// target: every character that is not a letter, digit, or hyphen.
var claudeProjectDirNonAlnum = regexp.MustCompile(`[^A-Za-z0-9-]`)

// encodeClaudeProjectDir mirrors Claude Code's own cwd-to-project-directory
// encoding. Verified live (testdata/scrub-live-2026-08-23.md §sanity)
// against two real paths: '/', '.', and '_' all become '-'; an existing
// '-' is left alone. The general rule this implies — replace every
// non-alphanumeric, non-hyphen character with '-' — is what is applied
// here, rather than hardcoding just the two or three separators this
// test happened to observe.
func encodeClaudeProjectDir(absPath string) string {
	return claudeProjectDirNonAlnum.ReplaceAllString(absPath, "-")
}

// --- Minimal NDJSON wire client -------------------------------------
//
// This deliberately does not import internal/host/herdr's unexported
// client: the Step 20 boundary is read-only access to that package, and
// this test's job is to exercise Herdr's socket API directly, the same
// way internal/host/herdr/fakeserver_test.go exercises this package's
// consumer side. The wire shape (one NDJSON request per connection,
// {"id","method","params"} in, {"id","result","error"} out) is exactly
// what internal/host/herdr/wire.go documents and what §0 of
// testdata/scrub-live-2026-08-23.md exercised by hand first.

var wireSeq atomic.Uint64

func wireCall(ctx context.Context, socketPath, method string, params any, out any) error {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", socketPath)
	if err != nil {
		return fmt.Errorf("dial %s: %w", socketPath, err)
	}
	defer func() { _ = conn.Close() }()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	if params == nil {
		params = struct{}{}
	}
	req := map[string]any{
		"id":     "duo-scrub-live-" + strconv.FormatUint(wireSeq.Add(1), 10),
		"method": method,
		"params": params,
	}
	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	if _, err := conn.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write request: %w", err)
	}

	reader := bufio.NewReader(io.LimitReader(conn, 32<<20))
	respLine, err := reader.ReadBytes('\n')
	if err != nil && (!errors.Is(err, io.EOF) || len(respLine) == 0) {
		return fmt.Errorf("read response: %w", err)
	}
	var resp struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(respLine, &resp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("herdr %s: %s: %s", method, resp.Error.Code, resp.Error.Message)
	}
	if out == nil || len(resp.Result) == 0 {
		return nil
	}
	// out is decoded directly from the envelope's "result" payload; a
	// caller declares the shape of *that* payload (e.g. {root_pane:
	// {pane_id: ...}} for workspace.create), not a wrapper around it.
	return json.Unmarshal(resp.Result, out)
}
