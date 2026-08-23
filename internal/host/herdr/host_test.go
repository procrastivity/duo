package herdr

import (
	"context"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/host"
)

func TestHostImplementsScopedInterfaces(t *testing.T) {
	var h any = &Host{}
	if _, ok := h.(host.HostDiscovery); !ok {
		t.Error("Host does not implement HostDiscovery")
	}
	if _, ok := h.(host.HostLauncher); !ok {
		t.Error("Host does not implement HostLauncher")
	}
	if _, ok := h.(host.HostAttachmentValidator); !ok {
		t.Error("Host does not implement HostAttachmentValidator")
	}
	if _, ok := h.(host.HostLifecycleSource); !ok {
		t.Error("Host does not implement HostLifecycleSource")
	}
}

// The identity mapping from notes/19 §5: no server epoch exists, so
// HostServerEpoch stays empty and terminal_id carries the incarnation in
// HostContainerID, with pane_id as addressing only.
func TestDiscoverMapsHerdrIdentityIntoEvidence(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	candidates, err := h.Discover(context.Background(), host.DiscoveryRequest{
		IntegrationInstanceID: testInstanceID,
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	e := candidates[0].Evidence
	if e.HostServerEpoch != "" {
		t.Errorf("HostServerEpoch = %q, want empty: Herdr 0.8.2 has no server epoch", e.HostServerEpoch)
	}
	if e.HostContainerID != pane.terminalID {
		t.Errorf("HostContainerID = %q, want terminal_id %q", e.HostContainerID, pane.terminalID)
	}
	if e.PaneID != pane.paneID {
		t.Errorf("PaneID = %q, want %q", e.PaneID, pane.paneID)
	}
	if e.IntegrationInstanceID != testInstanceID {
		t.Errorf("IntegrationInstanceID = %q", e.IntegrationInstanceID)
	}
	if !birthProven(e.ProcessBirth) {
		t.Errorf("process birth %+v is not proven evidence", e.ProcessBirth)
	}
	if e.ProcessBirth.PID != pane.fgPID {
		t.Errorf("ProcessBirth.PID = %d, want %d", e.ProcessBirth.PID, pane.fgPID)
	}
}

// Discovery reads the pane inventory, not the agent registry: at 0.8.2 an
// agent deregisters durably on foreground loss while its process keeps
// running, so a registry-driven discovery would lose live runtimes.
func TestDiscoverEnumeratesPanesNotTheAgentRegistry(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	f.addPane("w1")
	h := testHost(t, f)

	candidates, err := h.Discover(context.Background(), host.DiscoveryRequest{
		IntegrationInstanceID: testInstanceID,
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidates = %d, want 2", len(candidates))
	}
	// The fake's snapshot reports no agents at all; discovery must not
	// have consulted an agent surface for its inventory.
	if n := f.callCount("agent.list"); n != 0 {
		t.Errorf("agent.list called %d times; the registry is not an inventory", n)
	}
	if n := f.callCount("session.snapshot"); n != 1 {
		t.Errorf("session.snapshot called %d times, want 1", n)
	}
}

func TestDiscoverFiltersByWorkspaceHint(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	wanted := f.addPane("w2")
	h := testHost(t, f)

	candidates, err := h.Discover(context.Background(), host.DiscoveryRequest{
		IntegrationInstanceID: testInstanceID,
		WorkspaceHint:         "w2",
	})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(candidates) != 1 || candidates[0].Evidence.PaneID != wanted.paneID {
		t.Fatalf("candidates = %+v, want only %s", candidates, wanted.paneID)
	}
}

func TestDiscoverRejectsAnotherIntegrationInstance(t *testing.T) {
	f := newFakeHerdr(t)
	h := testHost(t, f)
	if _, err := h.Discover(context.Background(), host.DiscoveryRequest{
		IntegrationInstanceID: "herdr:someone-else",
	}); err == nil {
		t.Fatal("Discover accepted another integration instance")
	}
}

// PrepareLaunch must not create anything: a Herdr pane cannot be staged,
// so a failed launch resolution would otherwise leave an orphan pane.
func TestPrepareLaunchMutatesNothing(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	h := testHost(t, f)

	if _, err := h.PrepareLaunch(context.Background(), host.HostLaunchRequest{
		ResolvedLaunchTuple: testTuple(),
	}); err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	if calls := f.mutatingCalls(); len(calls) != 0 {
		t.Fatalf("PrepareLaunch made mutating calls: %v", calls)
	}
}

// The environment-scrub seam: whatever the resolved tuple carries has to
// reach the pane, because a Herdr pane otherwise inherits the Herdr
// server's environment wholesale.
func TestPreparedLaunchCarriesEnvironmentToThePane(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	tuple := testTuple()
	// Both names are deliberately outside internal/scrub's deny list. This
	// test is about the env map reaching the pane at all; a marker-named
	// variable in a launch request is now its own refusal, covered by
	// TestPrepareLaunchRefusesARequestEnvironmentThatSetsAMarker.
	tuple.Env = map[string]string{"DUO_WORKSPACE": "/tmp", "DUO_SESSION": "s1"}
	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: tuple})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	staged, ok := prepared.Opaque.(preparedLaunch)
	if !ok {
		t.Fatalf("prepared.Opaque is %T", prepared.Opaque)
	}
	if staged.Env["DUO_SESSION"] != "s1" {
		t.Fatalf("prepared launch env = %v", staged.Env)
	}
	if _, err := h.Start(ctx, prepared); err != nil {
		t.Fatalf("Start: %v", err)
	}
	sent := f.envOfLastCreate()
	if sent["DUO_SESSION"] != "s1" || sent["DUO_WORKSPACE"] != "/tmp" {
		t.Fatalf("env sent to Herdr = %v, want the prepared launch's env", sent)
	}
}

func TestStartSplitsAnExistingPaneAndMapsCommandToKind(t *testing.T) {
	f := newFakeHerdr(t)
	existing := f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	tuple := testTuple()
	tuple.Command = "/usr/local/bin/claude"
	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: tuple})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	evidence, err := h.Start(ctx, prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := f.lastSplitParams().TargetPaneID; got != existing.paneID {
		t.Errorf("split target = %q, want %q", got, existing.paneID)
	}
	if got := f.lastAgentStartParams().Kind; got != "claude" {
		t.Errorf("agent kind = %q, want claude (base name of the resolved command)", got)
	}
	if evidence.Evidence.PaneID == existing.paneID {
		t.Errorf("Start returned the pre-existing pane %q", evidence.Evidence.PaneID)
	}
	if evidence.Evidence.HostContainerID == "" {
		t.Error("launch evidence carries no terminal_id")
	}
	if !birthProven(evidence.Evidence.ProcessBirth) {
		t.Errorf("launch evidence birth %+v is not proven", evidence.Evidence.ProcessBirth)
	}
}

// agent.start returns launch_pending at 0.8.2 — before the agent process
// exists — so launch evidence taken at that instant names the pane's
// shell. Observed live: the PID changed 10 ms after Start returned, which
// would have made every fresh attachment fail its own first validation.
func TestStartWaitsForTheShellToHandOverTheTerminal(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.setAgentStartPID(7777)
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	launched, err := h.Start(ctx, prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if launched.Evidence.ProcessBirth.PID != 7777 {
		t.Fatalf("launch evidence PID = %d, want the started process 7777",
			launched.Evidence.ProcessBirth.PID)
	}
	if launched.Evidence.PaneID == pane.paneID {
		t.Fatalf("launch reused the pre-existing pane %s", pane.paneID)
	}

	// And the evidence it returns is the evidence that validates.
	got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
		Attachment:            AttachmentFor(launched.Evidence),
		LastKnownProcessBirth: launched.Evidence.ProcessBirth,
	})
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if !got.SameProcess {
		t.Fatal("the attachment a launch just produced did not validate")
	}
}

// A handover that never happens is weaker evidence, not a failure: the
// launch did happen, and the caller re-fingerprints at correlation time.
func TestStartReturnsBaselineEvidenceWhenNoHandoverHappens(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	launched, err := h.Start(ctx, prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !birthProven(launched.Evidence.ProcessBirth) {
		t.Fatalf("launch evidence = %+v, want the pane's own process", launched.Evidence.ProcessBirth)
	}
}

func TestStartCreatesAWorkspaceWhenTheSessionIsEmpty(t *testing.T) {
	f := newFakeHerdr(t)
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	if _, err := h.Start(ctx, prepared); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if f.callCount("workspace.create") != 1 || f.callCount("pane.split") != 0 {
		t.Fatalf("calls = %v, want one workspace.create", f.mutatingCalls())
	}
}

// agent_pane_busy is a pre-delivery refusal — the shell is not at its
// prompt yet and nothing was typed into the pane — so it is the one code
// Start retries.
func TestStartRetriesAgentPaneBusy(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	f.setAgentStartBusy(3)
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	if _, err := h.Start(ctx, prepared); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := f.callCount("agent.start"); got != 4 {
		t.Fatalf("agent.start attempts = %d, want 4 (3 busy refusals then success)", got)
	}
}

func TestStartGivesUpAfterTheRetryLimit(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	f.setAgentStartBusy(50)
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	if _, err := h.Start(ctx, prepared); err == nil {
		t.Fatal("Start succeeded despite a permanently busy pane")
	}
	if got := f.callCount("agent.start"); got != 5 {
		t.Fatalf("agent.start attempts = %d, want the configured limit of 5", got)
	}
}

// Any refusal that is not agent_pane_busy is surfaced unchanged: nothing
// here may retry an error whose effect is unknown.
func TestStartDoesNotRetryOtherRefusals(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	f.setAgentStartError("agent_kind_mismatch", "pane runs a different agent")

	_, err = h.Start(ctx, prepared)
	if err == nil {
		t.Fatal("Start succeeded despite a refused agent.start")
	}
	if got := ErrorCode(err); got != "agent_kind_mismatch" {
		t.Fatalf("ErrorCode = %q, want the server's own code passed through", got)
	}
	if got := f.callCount("agent.start"); got != 1 {
		t.Fatalf("agent.start attempted %d times; only agent_pane_busy is retryable", got)
	}
}

func TestStartRejectsAForeignPreparedLaunch(t *testing.T) {
	f := newFakeHerdr(t)
	h := testHost(t, f)
	_, err := h.Start(context.Background(), host.PreparedHostLaunch{
		IntegrationInstanceID: testInstanceID,
		LaunchResolutionID:    "lr-1",
		Opaque:                "not ours",
	})
	if err == nil {
		t.Fatal("Start accepted an opaque payload it did not prepare")
	}
}

func TestValidateAttachmentReportsSameProcess(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	first, err := h.ValidateAttachment(ctx, claimFor(pane, host.ProcessBirthEvidence{}))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if first.SameProcess {
		t.Fatal("an empty last-known birth was accepted as proof of continuity")
	}
	again, err := h.ValidateAttachment(ctx, claimFor(pane, first.Evidence.ProcessBirth))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if !again.SameProcess {
		t.Fatalf("SameProcess = false for unchanged evidence %+v", again.Evidence.ProcessBirth)
	}
}

// The duplicate-enrollment guard: a server restart restores pane_id but
// always mints a new terminal_id, so a stale claim must not validate.
func TestValidateAttachmentRejectsANewTerminalOnTheSamePaneID(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	baseline, err := h.ValidateAttachment(ctx, claimFor(pane, host.ProcessBirthEvidence{}))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	claim := claimFor(pane, baseline.Evidence.ProcessBirth)

	// A restart: same pane_id and same PID, new terminal.
	f.mutatePane(pane.paneID, func(p *fakePaneState) { p.terminalID = "term_restarted" })

	got, err := h.ValidateAttachment(ctx, claim)
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.SameProcess {
		t.Fatal("SameProcess = true across a terminal_id change; pane coordinates are not continuity")
	}
	if got.Evidence.HostContainerID != "term_restarted" {
		t.Fatalf("evidence for the new incarnation = %q", got.Evidence.HostContainerID)
	}
}

func TestValidateAttachmentReportsAVanishedPane(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	f.removePane(pane.paneID)
	got, err := h.ValidateAttachment(ctx, claimFor(pane, host.ProcessBirthEvidence{PID: pane.fgPID}))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.SameProcess {
		t.Fatal("SameProcess = true for a pane that no longer exists")
	}
}

// Herdr reports no process start time. Without a second source the birth
// cannot be proven, and an unprovable continuity claim must resolve to
// "new runtime instance", never to a silent merge.
func TestValidateAttachmentWithoutProvenBirthIsNotSameProcess(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f, func(c *Config) { c.ResolveProcessBirth = unprovenBirth })
	ctx := context.Background()

	got, err := h.ValidateAttachment(ctx, claimFor(pane, host.ProcessBirthEvidence{
		PID:             pane.fgPID,
		StartTimeSource: StartTimeSourceUnavailable,
	}))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.SameProcess {
		t.Fatal("SameProcess = true on PID equality alone")
	}
}

func TestConfigRequiresIdentityAndSocket(t *testing.T) {
	if _, err := New(Config{SocketPath: "/tmp/x.sock"}); err == nil {
		t.Error("New accepted a config with no integration instance ID")
	}
	if _, err := New(Config{IntegrationInstanceID: "herdr:x"}); err == nil {
		t.Error("New accepted a config with no socket path")
	}
}

func TestInstanceIDForSession(t *testing.T) {
	if got := InstanceIDForSession("duo17"); got != "herdr:duo17" {
		t.Fatalf("InstanceIDForSession = %q", got)
	}
}

// The round trip a caller actually performs: keep what a launch returned,
// hand it back as a claim, and have it validate.
func TestAttachmentForRoundTripsLaunchEvidence(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	launched, err := h.Start(ctx, prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	attachment := AttachmentFor(launched.Evidence)
	if attachment.PaneID != launched.Evidence.PaneID ||
		attachment.HostContainerID != launched.Evidence.HostContainerID {
		t.Fatalf("attachment = %+v, want the launch evidence's addressing", attachment)
	}
	got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
		Attachment:            attachment,
		LastKnownProcessBirth: launched.Evidence.ProcessBirth,
	})
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if !got.SameProcess {
		t.Fatal("a freshly launched attachment did not validate")
	}
}

func testTuple() host.ResolvedLaunchTuple {
	return host.ResolvedLaunchTuple{
		LaunchResolutionID:    "lr-1",
		IntegrationInstanceID: testInstanceID,
		WorkspacePath:         "/tmp",
		Command:               "claude",
	}
}

func claimFor(pane fakePaneState, birth host.ProcessBirthEvidence) host.HostAttachmentClaim {
	return host.HostAttachmentClaim{
		Attachment: host.Attachment{
			IntegrationInstanceID: testInstanceID,
			HostServerEpoch:       NoServerEpoch,
			HostContainerID:       pane.terminalID,
			PaneID:                pane.paneID,
		},
		LastKnownProcessBirth: birth,
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
