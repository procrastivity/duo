package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/claude"
	runtimedevin "github.com/procrastivity/duo/internal/runtime/devin"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

func TestMain(m *testing.M) {
	// Launch waits a bounded time in production. Tests do one poll so
	// existing launches that never seed identity do not hang.
	identityBindTimeout = 0
	os.Exit(m.Run())
}

// identityHosts is a launch.HostSet over one cached fake host so Start and
// the post-spawn bind pass see the same pane. Seed, when set, is applied
// before Start so the test body never calls MarkLive.
type identityHosts struct {
	hosts map[string]*hostfake.Host
	seed  *host.AgentBindState
}

func newIdentityHosts(seed *host.AgentBindState) *identityHosts {
	return &identityHosts{hosts: map[string]*hostfake.Host{}, seed: seed}
}

func (h *identityHosts) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	fake, ok := h.hosts[t.IntegrationInstanceID]
	if !ok {
		fake = hostfake.New(t.IntegrationInstanceID)
		if h.seed != nil {
			fake.SeedAgentBind(*h.seed)
		}
		h.hosts[t.IntegrationInstanceID] = fake
	}
	return fake, nil
}

// delayedPendingHosts seeds identity immediately with launch_pending set,
// then clears that flag on the cached fake after delay via SetPaneAgentBind.
// Start and the bind poll share the same *hostfake.Host.
type delayedPendingHosts struct {
	inner *identityHosts
	ident host.AgentSessionIdentity
	delay time.Duration
}

func (h *delayedPendingHosts) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	inner, err := h.inner.LauncherFor(t)
	if err != nil {
		return nil, err
	}
	return &delayedPendingLauncher{
		Host:  inner.(*hostfake.Host),
		ident: h.ident,
		delay: h.delay,
	}, nil
}

type delayedPendingLauncher struct {
	*hostfake.Host
	ident host.AgentSessionIdentity
	delay time.Duration
}

func (h *delayedPendingLauncher) Start(ctx context.Context, prepared host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	ev, err := h.Host.Start(ctx, prepared)
	if err != nil {
		return ev, err
	}
	paneID := ev.Evidence.PaneID
	ident := h.ident
	delay := h.delay
	fake := h.Host
	go func() {
		time.Sleep(delay)
		fake.SetPaneAgentBind(paneID, host.AgentBindState{
			Session:          &ident,
			LaunchPending:    false,
			InteractiveReady: true,
		})
	}()
	return ev, nil
}

func TestLaunchBindsIdentityAndMarksLiveWithoutHandInjectedMarkLive(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	ident := host.AgentSessionIdentity{
		Source: "herdr:claude",
		Agent:  "claude",
		Kind:   host.AgentSessionKindID,
		Value:  "sess-live-1",
	}
	RegisterAgentRuntime("claude-code", runtimefake.New("claude-code"))
	t.Cleanup(func() { UnregisterAgentRuntime("claude-code") })

	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:          &ident,
		LaunchPending:    false,
		InteractiveReady: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live (test body did not call MarkLive)", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok {
		t.Fatal("agentBindingsFor failed after launch bind")
	}
	if bindings.ExternalAgentSessionID != ident.Value {
		t.Errorf("external agent session = %q, want %q", bindings.ExternalAgentSessionID, ident.Value)
	}
	if bindings.IntegrationInstance != "claude-code" {
		t.Errorf("integration instance = %q, want claude-code", bindings.IntegrationInstance)
	}
	if got := correlationSource(h.authority, sess.Current, "agent.session"); got != string(domain.SourceLaunchPlan) {
		t.Errorf("agent.session source = %q, want %q", got, domain.SourceLaunchPlan)
	}
}

func TestLaunchLeavesStartingWhenIdentityNeverAppears(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	report, err := h.launch(mat, newIdentityHosts(nil), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting", inst.State)
	}
	if _, ok := agentBindingsFor(h.authority, sess); ok {
		t.Fatal("agentBindingsFor succeeded; identity never appeared, so no agent.session correlation")
	}
	for _, c := range h.authority.Correlations(domain.TargetInstance, string(sess.Current)) {
		if c.Status == domain.CorrelationActive && c.ExternalKind == "agent.session" {
			t.Fatalf("invented agent.session correlation %+v", c)
		}
	}
}

func TestLaunchPathIdentityBindsAsAgentSession(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	path := "/tmp/pi-sessions/abc/session.jsonl"
	ident := host.AgentSessionIdentity{
		Source: "herdr:pi",
		Agent:  "pi",
		Kind:   host.AgentSessionKindPath,
		Value:  path,
	}
	RegisterAgentRuntime("claude-code", runtimefake.New("claude-code"))
	t.Cleanup(func() { UnregisterAgentRuntime("claude-code") })

	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:       &ident,
		LaunchPending: false,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, _ := h.authority.Instance(sess.Current)
	if inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok {
		t.Fatal("agentBindingsFor failed for path-shaped identity")
	}
	if bindings.ExternalAgentSessionID != path {
		t.Errorf("agent.session value = %q, want the named path (not a directory scan)", bindings.ExternalAgentSessionID)
	}
}

const piInjectSessionID = "01a02c19-65e1-7346-b418-82ab0d32942c"

func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// startLoopingIdleGreetStandIn accepts until cleanup and writes one NDJSON
// greeting line per connect. identity_bind may dial Ready twice (poll +
// commit).
func startLoopingIdleGreetStandIn(t *testing.T, sockPath, greetLine string) {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen %s: %v", sockPath, err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }()
				_, _ = c.Write([]byte(greetLine + "\n"))
			}(conn)
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
}

func setupPiInjectSocket(t *testing.T, sessionID string) string {
	t.Helper()
	sockPath, err := runtimepi.InjectSocketPath(sessionID)
	if err != nil {
		t.Fatalf("InjectSocketPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir inject socket dir: %v", err)
	}
	return sockPath
}

func piPendingPathIdentity() host.AgentSessionIdentity {
	path := "/tmp/sessions/--cwd--/2026-08-27T00-00-00-000Z_" + piInjectSessionID + ".jsonl"
	return host.AgentSessionIdentity{
		Source: "herdr:pi",
		Agent:  "pi",
		Kind:   host.AgentSessionKindPath,
		Value:  path,
	}
}

func TestFakeRuntimeDoesNotOfferReady(t *testing.T) {
	var rt any = runtimefake.New("claude-code")
	if _, ok := rt.(runtime.RuntimeReadyProvider); ok {
		t.Fatal("fake runtime must not implement RuntimeReadyProvider")
	}
}

func TestPiLaunchPendingIdleMarksLive(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	h := newPiBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	ident := piPendingPathIdentity()
	sockPath := setupPiInjectSocket(t, ident.Value)
	startLoopingIdleGreetStandIn(t, sockPath, `{"idle":true}`)

	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:       &ident,
		LaunchPending: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, _ := h.authority.Instance(sess.Current)
	if inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live while launch_pending and idle (test body did not call MarkLive)", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok || bindings.ExternalAgentSessionID != piInjectSessionID {
		t.Fatalf("want named agent.session bound to peeled uuid, got ok=%v %+v", ok, bindings)
	}
}

func TestPiLaunchPendingNotIdleStaysStarting(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	h := newPiBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	ident := piPendingPathIdentity()
	sockPath := setupPiInjectSocket(t, ident.Value)
	startLoopingIdleGreetStandIn(t, sockPath, `{"idle":false}`)

	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:       &ident,
		LaunchPending: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, _ := h.authority.Instance(sess.Current)
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting while launch_pending and not idle", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok || bindings.ExternalAgentSessionID != piInjectSessionID {
		t.Fatalf("want named agent.session (peeled uuid) while still starting, got ok=%v %+v", ok, bindings)
	}
}

func TestPiLaunchPendingNoListenerStaysStarting(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	h := newPiBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	ident := piPendingPathIdentity()
	_ = setupPiInjectSocket(t, ident.Value) // mkdir parent only; no listener

	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:       &ident,
		LaunchPending: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, _ := h.authority.Instance(sess.Current)
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting with no inject listener", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok || bindings.ExternalAgentSessionID != piInjectSessionID {
		t.Fatalf("want named agent.session (peeled uuid) while still starting, got ok=%v %+v", ok, bindings)
	}
}

func TestLaunchPendingDoesNotMarkLive(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	ident := host.AgentSessionIdentity{
		Kind:  host.AgentSessionKindID,
		Value: "sess-pending-1",
	}
	RegisterAgentRuntime("claude-code", runtimefake.New("claude-code"))
	t.Cleanup(func() { UnregisterAgentRuntime("claude-code") })

	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:       &ident,
		LaunchPending: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, _ := h.authority.Instance(sess.Current)
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting while launch_pending", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok || bindings.ExternalAgentSessionID != ident.Value {
		t.Fatalf("want named agent.session while still starting, got ok=%v %+v", ok, bindings)
	}
}

func TestLaunchMarksLiveWhenLaunchPendingClearsAfterThreeSeconds(t *testing.T) {
	prev := identityBindTimeout
	identityBindTimeout = defaultIdentityBindTimeout
	t.Cleanup(func() { identityBindTimeout = prev })

	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	ident := host.AgentSessionIdentity{
		Source: "herdr:claude",
		Agent:  "claude",
		Kind:   host.AgentSessionKindID,
		Value:  "sess-d3-delay-1",
	}
	RegisterAgentRuntime("claude-code", runtimefake.New("claude-code"))
	t.Cleanup(func() { UnregisterAgentRuntime("claude-code") })

	hosts := &delayedPendingHosts{
		inner: newIdentityHosts(&host.AgentBindState{
			Session:       &ident,
			LaunchPending: true,
		}),
		ident: ident,
		delay: 3*time.Second + 200*time.Millisecond,
	}
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live after launch_pending cleared (test body did not call MarkLive)", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok || bindings.ExternalAgentSessionID != ident.Value {
		t.Fatalf("want named agent.session after D3 flip, got ok=%v %+v", ok, bindings)
	}
}

func TestClaimFromHostIdentityMapsKind(t *testing.T) {
	cwd := "/tmp/duo-ws"
	id := host.AgentSessionIdentity{Kind: host.AgentSessionKindID, Value: "uuid-1"}
	claim, ref := claimFromHostIdentity("claude-code", id, cwd)
	if ref.SessionID != "uuid-1" || claim.ExternalAgentSessionID != "uuid-1" {
		t.Fatalf("id mapping: claim=%+v ref=%+v", claim, ref)
	}
	if claim.TranscriptPath != "" {
		t.Errorf("id kind set TranscriptPath = %q", claim.TranscriptPath)
	}
	if claim.WorkingDirectory != cwd {
		t.Errorf("id kind WorkingDirectory = %q, want workspace root", claim.WorkingDirectory)
	}

	path := "/tmp/pi/session.jsonl"
	p := host.AgentSessionIdentity{Kind: host.AgentSessionKindPath, Value: path}
	claim, ref = claimFromHostIdentity("pi", p, cwd)
	if ref.SessionID != path || claim.ExternalAgentSessionID != path {
		t.Fatalf("path mapping: claim=%+v ref=%+v", claim, ref)
	}
	if claim.TranscriptPath != path {
		t.Errorf("path kind TranscriptPath = %q, want the named path", claim.TranscriptPath)
	}
	if claim.WorkingDirectory != cwd {
		t.Errorf("path kind WorkingDirectory = %q, want workspace root", claim.WorkingDirectory)
	}
}

const (
	piBasicSessionUUID = "019fe2b8-12ed-73ac-b6ca-4b3b9a0b6c80"
	piBasicFixtureSrc  = "../runtime/pi/testdata/basic-with-resume_2026-08-08T18-52-22-125Z_019fe2b8-12ed-73ac-b6ca-4b3b9a0b6c80.jsonl"
)

func copyPiBasicFixtureToTemp(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(piBasicFixtureSrc)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", piBasicFixtureSrc, err)
	}
	path := filepath.Join(t.TempDir(), filepath.Base(piBasicFixtureSrc))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestClaimFromHostIdentityPeelsPathKindUUID(t *testing.T) {
	cwd := "/tmp/duo-ws"

	peelable := copyPiBasicFixtureToTemp(t)
	p := host.AgentSessionIdentity{Kind: host.AgentSessionKindPath, Value: peelable}
	claim, ref := claimFromHostIdentity("pi", p, cwd)
	if ref.SessionID != piBasicSessionUUID {
		t.Errorf("peelable ref.SessionID = %q, want %q", ref.SessionID, piBasicSessionUUID)
	}
	if claim.ExternalAgentSessionID != piBasicSessionUUID {
		t.Errorf("peelable ExternalAgentSessionID = %q, want %q", claim.ExternalAgentSessionID, piBasicSessionUUID)
	}
	if claim.TranscriptPath != peelable {
		t.Errorf("peelable TranscriptPath = %q, want %q", claim.TranscriptPath, peelable)
	}

	emptyPeel := "/tmp/pi/session.jsonl"
	ep := host.AgentSessionIdentity{Kind: host.AgentSessionKindPath, Value: emptyPeel}
	claim, ref = claimFromHostIdentity("pi", ep, cwd)
	if ref.SessionID != emptyPeel || claim.ExternalAgentSessionID != emptyPeel {
		t.Fatalf("empty-peel mapping: claim=%+v ref=%+v", claim, ref)
	}
	if claim.TranscriptPath != emptyPeel {
		t.Errorf("empty-peel TranscriptPath = %q, want %q", claim.TranscriptPath, emptyPeel)
	}
}

func TestLaunchPeelablePathIdentityBindsUUIDAsAgentSession(t *testing.T) {
	h := newPiBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	path := copyPiBasicFixtureToTemp(t)
	ident := host.AgentSessionIdentity{
		Source: "herdr:pi",
		Agent:  "pi",
		Kind:   host.AgentSessionKindPath,
		Value:  path,
	}

	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:          &ident,
		LaunchPending:    false,
		InteractiveReady: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, _ := h.authority.Instance(sess.Current)
	if inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok {
		t.Fatal("agentBindingsFor failed for peelable path-shaped identity")
	}
	if bindings.ExternalAgentSessionID != piBasicSessionUUID {
		t.Errorf("agent.session value = %q, want peeled uuid %q (not the path)", bindings.ExternalAgentSessionID, piBasicSessionUUID)
	}
	if bindings.TranscriptID != path {
		t.Errorf("transcript = %q, want absolute path %q", bindings.TranscriptID, path)
	}
	if bindings.IntegrationInstance != "pi" {
		t.Errorf("integration instance = %q, want pi", bindings.IntegrationInstance)
	}
}

func TestLaunchIDIdentityStoresSlugTranscript(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	claudeDir := t.TempDir()
	rt, err := claude.New("claude-code", "", claudeDir)
	if err != nil {
		t.Fatalf("claude.New: %v", err)
	}
	RegisterAgentRuntime("claude-code", rt)
	t.Cleanup(func() { UnregisterAgentRuntime("claude-code") })

	ident := host.AgentSessionIdentity{
		Source: "herdr:claude",
		Agent:  "claude",
		Kind:   host.AgentSessionKindID,
		Value:  "sess-slug-1",
	}
	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:          &ident,
		LaunchPending:    false,
		InteractiveReady: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok {
		t.Fatal("agentBindingsFor failed after id-kind launch bind")
	}
	if bindings.ExternalAgentSessionID != ident.Value {
		t.Errorf("external agent session = %q, want %q", bindings.ExternalAgentSessionID, ident.Value)
	}
	slug := strings.Map(func(r rune) rune {
		if r == '/' || r == '.' {
			return '-'
		}
		return r
	}, h.root)
	want := filepath.Join(claudeDir, "projects", slug, ident.Value+".jsonl")
	if bindings.TranscriptID != want {
		t.Errorf("transcript = %q, want slug path %q", bindings.TranscriptID, want)
	}
}

func TestRuntimeIDForSessionUsesLaunchTupleWhenUnbound(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)
	report, err := h.launch(mat, newIdentityHosts(nil), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	if got := runtimeIDForSession(h.authority, sess); got != "claude-code" {
		t.Errorf("runtimeIDForSession = %q, want claude-code from the launch tuple", got)
	}
	if got := paneIDForSession(h.authority, sess); got == "" {
		t.Error("paneIDForSession empty; launch attachment should name the pane")
	}
}

func TestLaunchDevinIDIdentityStoresATIFTranscript(t *testing.T) {
	h := newDevinBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	ident := host.AgentSessionIdentity{
		Source: "herdr:devin",
		Agent:  "devin",
		Kind:   host.AgentSessionKindID,
		Value:  "brave-muskmelon",
	}
	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:          &ident,
		LaunchPending:    false,
		InteractiveReady: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok {
		t.Fatal("agentBindingsFor failed after Devin id-kind launch bind")
	}
	if bindings.ExternalAgentSessionID != ident.Value {
		t.Errorf("external agent session = %q, want %q", bindings.ExternalAgentSessionID, ident.Value)
	}
	lr, ok := h.authority.SessionLaunchResolution(sess.ID)
	if !ok {
		t.Fatal("SessionLaunchResolution missing")
	}
	want, err := runtimedevin.ATIFPath(string(lr.ID), "primary")
	if err != nil {
		t.Fatalf("ATIFPath: %v", err)
	}
	if bindings.TranscriptID != want {
		t.Errorf("transcript = %q, want convention path %q", bindings.TranscriptID, want)
	}
}

func correlationSource(a *domain.Authority, instance domain.InstanceID, kind string) string {
	for _, c := range a.Correlations(domain.TargetInstance, string(instance)) {
		if c.Status == domain.CorrelationActive && c.ExternalKind == kind {
			return c.Source
		}
	}
	return ""
}

// --- mint-exit test doubles -------------------------------------------------

// setMintExitTiming overrides identityBindTimeout, identityBindPoll, and
// mintExitProbeInterval for one test, restoring TestMain's defaults
// (identityBindTimeout = 0) on cleanup. The mint-exit scenarios need more
// than TestMain's single poll: confirming a two-consecutive-probe streak,
// or proving an unreachable probe never confirms, both need several loop
// iterations to elapse before the deadline.
func setMintExitTiming(t *testing.T, timeout, poll, probeInterval time.Duration) {
	t.Helper()
	prevTimeout, prevPoll, prevProbe := identityBindTimeout, identityBindPoll, mintExitProbeInterval
	identityBindTimeout, identityBindPoll, mintExitProbeInterval = timeout, poll, probeInterval
	t.Cleanup(func() {
		identityBindTimeout, identityBindPoll, mintExitProbeInterval = prevTimeout, prevPoll, prevProbe
	})
}

// devinPrintMintHosts wraps identityHosts for a print-mint Devin launch
// test double: Start seeds the ATIF export the mint process would have
// written (when seedATIF is true) and then kills the pane synchronously —
// the same way `devin ... --export <path> --print <prompt>` exits right
// after minting, before bindLaunchIdentities' first AgentOnPane poll ever
// runs.
type devinPrintMintHosts struct {
	inner     *identityHosts
	seedATIF  bool
	sessionID string
}

func (h *devinPrintMintHosts) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	inner, err := h.inner.LauncherFor(t)
	if err != nil {
		return nil, err
	}
	return &devinPrintMintLauncher{Host: inner.(*hostfake.Host), hosts: h}, nil
}

type devinPrintMintLauncher struct {
	*hostfake.Host
	hosts *devinPrintMintHosts
}

func (h *devinPrintMintLauncher) Start(ctx context.Context, prepared host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	ev, err := h.Host.Start(ctx, prepared)
	if err != nil {
		return ev, err
	}
	if h.hosts.seedATIF {
		tuple, _ := prepared.Opaque.(host.ResolvedLaunchTuple)
		path, perr := runtimedevin.ATIFPath(prepared.LaunchResolutionID, tuple.Leaf)
		if perr != nil {
			return host.HostLaunchEvidence{}, perr
		}
		if mkErr := os.MkdirAll(filepath.Dir(path), 0o755); mkErr != nil {
			return host.HostLaunchEvidence{}, mkErr
		}
		doc := fmt.Sprintf(`{"schema_version":"1.7","session_id":%q,"steps":[]}`, h.hosts.sessionID)
		if wErr := os.WriteFile(path, []byte(doc), 0o644); wErr != nil {
			return host.HostLaunchEvidence{}, wErr
		}
	}
	h.Kill(host.Attachment{
		IntegrationInstanceID: ev.Evidence.IntegrationInstanceID,
		PaneID:                ev.Evidence.PaneID,
		HostContainerID:       ev.Evidence.HostContainerID,
	})
	return ev, nil
}

// scriptedValidatorHosts wraps identityHosts so its host's ValidateAttachment
// replays a scripted sequence of continuity classes (looping on the final
// entry once exhausted) instead of the fake host's own claim-driven state
// machine. It drives bindStartingIdentity's mint-exit probe through exact
// sequences — a lone process_replaced probe, a run of unreachable probes —
// without choreographing Kill/ReplaceProcess calls timed against the probe
// cadence. An empty ContinuityClass entry is this file's sentinel for "the
// call fails with host.ErrUnreachable".
type scriptedValidatorHosts struct {
	inner   *identityHosts
	classes []host.ContinuityClass
}

func (h *scriptedValidatorHosts) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	inner, err := h.inner.LauncherFor(t)
	if err != nil {
		return nil, err
	}
	return &scriptedValidatorHost{Host: inner.(*hostfake.Host), classes: h.classes}, nil
}

type scriptedValidatorHost struct {
	*hostfake.Host
	mu      sync.Mutex
	classes []host.ContinuityClass
	calls   int
}

func (h *scriptedValidatorHost) ValidateAttachment(context.Context, host.HostAttachmentClaim) (host.HostContinuityEvidence, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	idx := h.calls
	if idx >= len(h.classes) {
		idx = len(h.classes) - 1
	}
	h.calls++
	class := h.classes[idx]
	if class == "" {
		return host.HostContinuityEvidence{}, host.Unreachable(nil)
	}
	return host.ContinuityEvidence(class, host.Evidence{}), nil
}

// heldClaim reports whether the fingerprint the session's current host
// attachment was bound under is still held. It rebuilds the fingerprint
// via fingerprintFromAttachment (session_reconcile.go) so the test does not
// duplicate that mapping.
func heldClaim(t *testing.T, h *bindHarness, sess domain.Session) bool {
	t.Helper()
	att, ok := h.authority.Attachment(sess.Attachment)
	if !ok {
		t.Fatal("no host attachment recorded")
	}
	fp := fingerprintFromAttachment(att)
	_, held := h.authority.ActiveClaim(fp.ClaimRef())
	return held
}

// --- mint-exit scenarios -----------------------------------------------------

// TestPrintMintExitDevinPaneAbsentRecoversIdentity is scenario (a): the
// print-mint process exits (pane_absent) before AgentOnPane ever reports
// identity, but it left the ATIF export behind. bindStartingIdentity
// recovers the minted agent-session id from that export, binds and marks
// the instance live exactly as an on-time identity report would have, and
// releases the launch fingerprint claim the exited mint process held.
func TestPrintMintExitDevinPaneAbsentRecoversIdentity(t *testing.T) {
	setMintExitTiming(t, 200*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond)

	h := newDevinBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	const mintedID = "brave-muskmelon"
	hosts := &devinPrintMintHosts{
		inner:     newIdentityHosts(nil),
		seedATIF:  true,
		sessionID: mintedID,
	}
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live after Devin mint-exit recovery", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok || bindings.ExternalAgentSessionID != mintedID {
		t.Fatalf("want agent-session bound to minted id %q, got ok=%v %+v", mintedID, ok, bindings)
	}
	if heldClaim(t, h, sess) {
		t.Error("fingerprint claim still held after mint-exit release")
	}
}

// TestPrintMintExitDevinNoATIFTakesGenericExitLeg is scenario (b): the same
// pane_absent exit, but the mint process never wrote an ATIF export (or it
// never landed). No agent-session id can be recovered, so the generic exit
// leg runs: Authority.Exit releases every claim through exitInstance, and
// the launch path's loud stderr note fires (the command still succeeds).
func TestPrintMintExitDevinNoATIFTakesGenericExitLeg(t *testing.T) {
	setMintExitTiming(t, 200*time.Millisecond, 5*time.Millisecond, 5*time.Millisecond)

	h := newDevinBindHarness(t)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	hosts := &devinPrintMintHosts{inner: newIdentityHosts(nil), seedATIF: false}
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceExited {
		t.Fatalf("instance state = %s, want exited when no ATIF id could be recovered", inst.State)
	}
	if heldClaim(t, h, sess) {
		t.Error("fingerprint claim still held after Authority.Exit")
	}
	if !strings.Contains(h.err.String(), "mint process exit observed") {
		t.Errorf("no loud stderr note about the mint-process exit:\n%s", h.err.String())
	}
}

// TestPrintMintExitPaneAliveWithoutIdentityIsNotExit is scenario (c): the
// pane is alive and untouched, only the agent row is simply absent (the
// ordinary "still starting" case every earlier test already covers). A
// missing AgentOnPane row alone must never read as exit —
// internal/host/herdr/agent_bind.go's rule that only ValidateAttachment is
// exit evidence has to hold under repeated continuity probing too, not
// just on a single poll.
func TestPrintMintExitPaneAliveWithoutIdentityIsNotExit(t *testing.T) {
	setMintExitTiming(t, 80*time.Millisecond, 5*time.Millisecond, 10*time.Millisecond)

	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	report, err := h.launch(mat, newIdentityHosts(nil), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting: a missing AgentOnPane row alone is not exit evidence", inst.State)
	}
	if !heldClaim(t, h, sess) {
		t.Error("fingerprint claim released though the pane never proved exit")
	}
}

// TestPrintMintExitSingleProcessReplacedProbeDoesNotConfirmExit is scenario
// (d): one process_replaced probe, then the pane reads intact again — the
// spawn-handover window the two-consecutive-agree rule exists to guard.
// A lone probe must never confirm exit.
func TestPrintMintExitSingleProcessReplacedProbeDoesNotConfirmExit(t *testing.T) {
	setMintExitTiming(t, 80*time.Millisecond, 5*time.Millisecond, 10*time.Millisecond)

	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	hosts := &scriptedValidatorHosts{
		inner:   newIdentityHosts(nil),
		classes: []host.ContinuityClass{host.ContinuityProcessReplaced, host.ContinuitySameLive},
	}
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting: a single process_replaced probe must not confirm exit", inst.State)
	}
}

// TestPrintMintExitUnreachableProbeKeepsPolling is scenario (e): every
// continuity probe fails with host.ErrUnreachable. That is a call error,
// never pane-absence, so it must never confirm exit — the wait keeps
// polling to the ordinary deadline exactly as if no prober were wired up.
func TestPrintMintExitUnreachableProbeKeepsPolling(t *testing.T) {
	setMintExitTiming(t, 80*time.Millisecond, 5*time.Millisecond, 10*time.Millisecond)

	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	hosts := &scriptedValidatorHosts{
		inner:   newIdentityHosts(nil),
		classes: []host.ContinuityClass{""},
	}
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting: an unreachable probe must not confirm exit", inst.State)
	}
	if !heldClaim(t, h, sess) {
		t.Error("fingerprint claim released though every probe was unreachable")
	}
}
