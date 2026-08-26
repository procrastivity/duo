package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/runtime/claude"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
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

	path := "/tmp/pi/2026-08-26_sess.jsonl"
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

func correlationSource(a *domain.Authority, instance domain.InstanceID, kind string) string {
	for _, c := range a.Correlations(domain.TargetInstance, string(instance)) {
		if c.Status == domain.CorrelationActive && c.ExternalKind == kind {
			return c.Source
		}
	}
	return ""
}
