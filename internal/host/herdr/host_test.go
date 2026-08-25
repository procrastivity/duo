package herdr

import (
	"context"
	"errors"
	"path/filepath"
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

// End-to-end shape of the Stage 1 dogfood bug: PrepareLaunch called twice
// for the two leaves of one build_and_verify launch, same
// LaunchResolutionID, different Leaf. Before the fix both calls produced
// the same AgentName and the second leaf's Start would have collided with
// the first at Herdr's agent.start.
func TestPrepareLaunchGivesEachLeafOfOneLaunchADistinctAgentName(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	builderTuple := testTuple()
	builderTuple.Leaf = "builder"
	verifierTuple := testTuple()
	verifierTuple.Leaf = "verifier"

	builder, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: builderTuple})
	if err != nil {
		t.Fatalf("PrepareLaunch(builder): %v", err)
	}
	verifier, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: verifierTuple})
	if err != nil {
		t.Fatalf("PrepareLaunch(verifier): %v", err)
	}

	builderStaged, ok := builder.Opaque.(preparedLaunch)
	if !ok {
		t.Fatalf("builder prepared.Opaque is %T", builder.Opaque)
	}
	verifierStaged, ok := verifier.Opaque.(preparedLaunch)
	if !ok {
		t.Fatalf("verifier prepared.Opaque is %T", verifier.Opaque)
	}

	if builderStaged.AgentName == verifierStaged.AgentName {
		t.Fatalf("both leaves of one launch got AgentName %q", builderStaged.AgentName)
	}
	if len(builderStaged.AgentName) > 32 || len(verifierStaged.AgentName) > 32 {
		t.Fatalf("AgentName exceeds Herdr's 32-character cap: builder %q, verifier %q",
			builderStaged.AgentName, verifierStaged.AgentName)
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
	tuple.Target = host.LaunchTargetPane
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

// The built-in default placement is a background tab of the focused
// pane's workspace, not a split — PROVISIONAL (dogfood, 2026-08-24),
// pending the config-authored per-kind default (notes/44). An explicit
// tab target takes the same path.
func TestStartCreatesABackgroundTabByDefault(t *testing.T) {
	f := newFakeHerdr(t)
	existing := f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	evidence, err := h.Start(ctx, prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if f.callCount("tab.create") != 1 || f.callCount("pane.split") != 0 {
		t.Fatalf("calls = %v, want one tab.create and no pane.split", f.mutatingCalls())
	}
	if got := f.lastTabParams().WorkspaceID; got != existing.workspaceID {
		t.Errorf("tab.create workspace = %q, want the focused pane's workspace %q", got, existing.workspaceID)
	}
	if evidence.Evidence.PaneID == existing.paneID {
		t.Errorf("Start returned the pre-existing pane %q", evidence.Evidence.PaneID)
	}
	if !birthProven(evidence.Evidence.ProcessBirth) {
		t.Errorf("launch evidence birth %+v is not proven", evidence.Evidence.ProcessBirth)
	}
}

// A target this host does not know is refused at PrepareLaunch, before
// any server call — the CLI validates first, but the adapter must not
// silently fall back on a placement the caller did not ask for.
func TestPrepareLaunchRefusesAnUnknownTarget(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	h := testHost(t, f)

	tuple := testTuple()
	tuple.Target = host.LaunchTarget("window")
	if _, err := h.PrepareLaunch(context.Background(), host.HostLaunchRequest{ResolvedLaunchTuple: tuple}); err == nil {
		t.Fatal("PrepareLaunch accepted an unknown launch target")
	}
	if calls := f.mutatingCalls(); len(calls) != 0 {
		t.Fatalf("the refusal made mutating calls: %v", calls)
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
	if got.Class != host.ContinuitySameLive {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuitySameLive)
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
	if first.Class != host.ContinuityUnproven {
		t.Fatalf("Class = %q, want %q for an empty last-known birth", first.Class, host.ContinuityUnproven)
	}
	again, err := h.ValidateAttachment(ctx, claimFor(pane, first.Evidence.ProcessBirth))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if !again.SameProcess {
		t.Fatalf("SameProcess = false for unchanged evidence %+v", again.Evidence.ProcessBirth)
	}
	if again.Class != host.ContinuitySameLive {
		t.Fatalf("Class = %q, want %q", again.Class, host.ContinuitySameLive)
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
	if got.Class != host.ContinuityTerminalReplaced {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityTerminalReplaced)
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
	if got.Class != host.ContinuityPaneAbsent {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityPaneAbsent)
	}
	if got.Evidence.PaneID != "" {
		t.Fatalf("pane-absent evidence must not carry a live pane: %+v", got.Evidence)
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
	if got.Class != host.ContinuityUnproven {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityUnproven)
	}
	if got.Evidence.PaneID != pane.paneID {
		t.Fatalf("unproven live birth still has a pane; PaneID = %q", got.Evidence.PaneID)
	}
}

func TestValidateAttachmentReportsAReplacedProcess(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)
	ctx := context.Background()

	baseline, err := h.ValidateAttachment(ctx, claimFor(pane, host.ProcessBirthEvidence{}))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	claim := claimFor(pane, baseline.Evidence.ProcessBirth)
	f.mutatePane(pane.paneID, func(p *fakePaneState) { p.fgPID = 8888 })

	got, err := h.ValidateAttachment(ctx, claim)
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.SameProcess {
		t.Fatal("SameProcess = true after the foreground PID changed")
	}
	if got.Class != host.ContinuityProcessReplaced {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityProcessReplaced)
	}
	if got.Evidence.ProcessBirth.PID != 8888 {
		t.Fatalf("live PID = %d, want 8888", got.Evidence.ProcessBirth.PID)
	}
	if got.Evidence.PaneID != pane.paneID {
		t.Fatalf("replaced process must keep the pane; PaneID = %q", got.Evidence.PaneID)
	}
}

func TestValidateAttachmentDialErrorIsUnreachable(t *testing.T) {
	h, err := New(Config{
		IntegrationInstanceID: testInstanceID,
		SocketPath:            filepath.Join(shortTempDir(t), "absent.sock"),
		CallTimeout:           200 * time.Millisecond,
		ResolveProcessBirth:   fakeBirth,
		ResolvePaneEnviron:    cleanPaneEnviron,
	})
	if err != nil {
		t.Fatalf("New dials; it must not: %v", err)
	}
	_, err = h.ValidateAttachment(context.Background(), host.HostAttachmentClaim{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: "w1:p1"},
	})
	if err == nil {
		t.Fatal("ValidateAttachment succeeded against a missing socket")
	}
	if !errors.Is(err, host.ErrUnreachable) {
		t.Fatalf("error = %v, want host.ErrUnreachable", err)
	}
	if ErrorCode(err) != "" {
		t.Fatalf("a dial failure must not look like a protocol error; ErrorCode = %q", ErrorCode(err))
	}
}

// An idle pane reports the shell, not "no process". Empty foreground
// therefore cannot be a distinct class: pidFor falls back to ShellPID,
// and that birth classifies as process-replaced when it is not the claim.
func TestValidateAttachmentEmptyForegroundFallsBackToShell(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.mutatePane(pane.paneID, func(p *fakePaneState) { p.fgPID = 7777 })
	h := testHost(t, f)
	ctx := context.Background()

	baseline, err := h.ValidateAttachment(ctx, claimFor(pane, host.ProcessBirthEvidence{}))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if baseline.Evidence.ProcessBirth.PID != 7777 {
		t.Fatalf("PID = %d, want the foreground 7777", baseline.Evidence.ProcessBirth.PID)
	}
	f.mutatePane(pane.paneID, func(p *fakePaneState) { p.fgPID = 0 })

	got, err := h.ValidateAttachment(ctx, claimFor(pane, baseline.Evidence.ProcessBirth))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.Class != host.ContinuityProcessReplaced {
		t.Fatalf("Class = %q, want %q (shell is a different process, not pane absence)",
			got.Class, host.ContinuityProcessReplaced)
	}
	if got.Evidence.ProcessBirth.PID != pane.shellPID {
		t.Fatalf("fallback PID = %d, want shell %d", got.Evidence.ProcessBirth.PID, pane.shellPID)
	}
}

// PID ≤ 0 (no foreground and no shell) is unproven birth on a surviving
// pane, not ContinuityPaneAbsent and not a fake empty-foreground class.
func TestValidateAttachmentZeroPIDOnALivePaneIsUnproven(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.mutatePane(pane.paneID, func(p *fakePaneState) {
		p.fgPID = 0
		p.shellPID = 0
	})
	h := testHost(t, f)
	ctx := context.Background()

	got, err := h.ValidateAttachment(ctx, claimFor(pane, host.ProcessBirthEvidence{
		PID:             41210,
		StartTime:       time.Unix(1_700_000_000, 0).UTC(),
		StartTimeSource: StartTimeSourceProcfs,
	}))
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.Class != host.ContinuityUnproven {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityUnproven)
	}
	if got.Evidence.PaneID != pane.paneID {
		t.Fatalf("a pane with no PIDs is still present; PaneID = %q", got.Evidence.PaneID)
	}
	if got.SameProcess {
		t.Fatal("SameProcess = true for a zero-PID live birth")
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
	if got.Class != host.ContinuitySameLive {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuitySameLive)
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

// Herdr 0.8.2 validates agent names at agent.start: lowercase start,
// [a-z0-9_-] only, 1-32 characters. invalid_agent_name was observed live
// at the Stage 1 gate with a full-length resolution-ID suffix, so this
// pins the generated name to the live rules for every ID shape the domain
// mints, and every leaf-name shape too (a1249de covered the ID axis; the
// leaf axis was added when leaf joined the signature).
func TestAgentNameFitsHerdrsLiveRules(t *testing.T) {
	cases := []struct{ prefix, leaf, id string }{
		{"duo", "", "lr_0123456789abcdef0123456789abcdef"},
		{"duo", "", "LR_UPPER-Case.and/odd chars"},
		{"duo", "", "short"},
		{"duo", "builder", "lrr_6e2421a36764505fdfe6de7a"},
		{"duo", "verifier", "lrr_6e2421a36764505fdfe6de7a"},
		{"duo", "AnUnreasonablyLongLeafNameNoPresetShouldEverDeclare", "lrr_6e2421a36764505fdfe6de7a"},
	}
	for _, tc := range cases {
		got := agentName(tc.prefix, tc.leaf, tc.id)
		if len(got) == 0 || len(got) > 32 {
			t.Errorf("agentName(%q, %q, %q) = %q: length %d, want 1-32", tc.prefix, tc.leaf, tc.id, got, len(got))
		}
		if got[0] < 'a' || got[0] > 'z' {
			t.Errorf("agentName(%q, %q, %q) = %q: must start with a lowercase letter", tc.prefix, tc.leaf, tc.id, got)
		}
		for _, r := range got {
			switch {
			case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			default:
				t.Errorf("agentName(%q, %q, %q) = %q: invalid rune %q", tc.prefix, tc.leaf, tc.id, got, r)
			}
		}
	}
}

// The bug reproduced live at the Stage 1 dogfood run (2026-08-24):
// launching the two-leaf preset build_and_verify spawned "builder" fine,
// then "verifier" failed at agent.start with agent_name_taken, because
// both leaves of one launch share a LaunchResolutionID and the
// pre-existing agentName ignored leaf entirely. This pins the fix: two
// leaves of the same launch — same prefix, same launch-resolution ID —
// must mint distinct names, each still inside Herdr's cap.
func TestAgentNameDistinguishesLeavesOfTheSameLaunch(t *testing.T) {
	const id = "lrr_6e2421a36764505fdfe6de7a"

	builder := agentName("duo", "builder", id)
	verifier := agentName("duo", "verifier", id)

	if builder == verifier {
		t.Fatalf("agentName(duo, builder, %s) == agentName(duo, verifier, %s) = %q: two leaves of one launch collide", id, id, builder)
	}
	for _, got := range []string{builder, verifier} {
		if len(got) == 0 || len(got) > 32 {
			t.Errorf("agentName(...) = %q: length %d, want 1-32", got, len(got))
		}
	}
}

// A single-leaf preset's PrepareLaunch still gets an empty tuple.Leaf in
// hand-built test tuples (see testTuple), and the fix must not regress
// that shape: no stray separator, no change from the pre-leaf name a
// caller with no leaf to give still gets.
func TestAgentNameWithNoLeafMatchesThePreLeafShape(t *testing.T) {
	const id = "lr-1"
	got := agentName("duo", "", id)
	want := "duo-" + id
	if got != want {
		t.Fatalf("agentName(duo, \"\", %q) = %q, want %q", id, got, want)
	}
}
