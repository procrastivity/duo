//go:build linux

package portablelauncher

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/host/herdr"
)

const (
	liveTestIntegration = "herdr:fixture-zeta"
	liveTestStartedAt   = "2026-09-18T14:23:41.370Z"
)

var liveTestTarget = ProcessIdentity{
	PID:          4317,
	StartTime:    liveTestStartedAt,
	TerminalID:   "terminal-epoch-alpha",
	AttachmentID: "attachment-durable-omega",
}

func TestAuthorityAttachmentClaimResolverReturnsExactDurableClaim(t *testing.T) {
	facts := liveAuthorityFacts()
	resolver := newLiveAuthorityResolver(t, func(context.Context) ([]domain.Fact, error) {
		return facts, nil
	})

	claim, err := resolver.Resolve(context.Background(), liveTestTarget)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	started, err := time.Parse(testProcessBirthLayout, liveTestStartedAt)
	if err != nil {
		t.Fatalf("parse expected start time: %v", err)
	}
	want := host.HostAttachmentClaim{
		Attachment: host.Attachment{
			IntegrationInstanceID: liveTestIntegration,
			HostServerEpoch:       herdr.NoServerEpoch,
			HostContainerID:       liveTestTarget.TerminalID,
			PaneID:                "pane-container-zeta",
		},
		LastKnownProcessBirth: host.ProcessBirthEvidence{
			PID:             liveTestTarget.PID,
			StartTime:       started,
			StartTimeSource: herdr.StartTimeSourceProcfs,
		},
	}
	if !sameAttachmentClaim(claim, want) {
		t.Fatalf("claim = %+v, want %+v", claim, want)
	}
}

func TestAuthorityAttachmentClaimResolverRejectsStaleAndMismatchedFacts(t *testing.T) {
	tests := []struct {
		name   string
		mutate func([]domain.Fact, *ProcessIdentity) []domain.Fact
	}{
		{name: "attachment ID is exact", mutate: func(f []domain.Fact, target *ProcessIdentity) []domain.Fact {
			target.AttachmentID = "attachment-durable-alpha"
			return f
		}},
		{name: "attachment detached", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveAttachmentFact(f).Attachment.State = domain.Detached
			return f
		}},
		{name: "continuity unverified", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveAttachmentFact(f).Attachment.Continuity = domain.ContinuityUnverified
			return f
		}},
		{name: "wrong integration", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveAttachmentFact(f).Attachment.IntegrationInstance = "herdr:fixture-alpha"
			return f
		}},
		{name: "wrong epoch kind", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveAttachmentFact(f).Attachment.Epoch.Kind = "herdr.server"
			return f
		}},
		{name: "wrong epoch scope", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveAttachmentFact(f).Attachment.Epoch.Scope = domain.EpochScopeServer
			return f
		}},
		{name: "wrong terminal", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveAttachmentFact(f).Attachment.Epoch.Value = "terminal-epoch-beta"
			return f
		}},
		{name: "missing pane", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveAttachmentFact(f).Attachment.Container = ""
			return f
		}},
		{name: "wrong PID", mutate: func(f []domain.Fact, target *ProcessIdentity) []domain.Fact {
			target.PID++
			return f
		}},
		{name: "wrong start", mutate: func(f []domain.Fact, target *ProcessIdentity) []domain.Fact {
			target.StartTime = "2026-09-18T14:23:41.371Z"
			return f
		}},
		{name: "noncanonical stored start", mutate: func(f []domain.Fact, target *ProcessIdentity) []domain.Fact {
			liveAttachmentFact(f).Attachment.Process.StartedAt = "2026-09-18T14:23:41Z"
			target.StartTime = "2026-09-18T14:23:41Z"
			return f
		}},
		{name: "inactive owning session", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveSessionFact(f).Session.State = domain.SessionInactive
			return f
		}},
		{name: "different current attachment", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			other := *liveAttachmentFact(f).Attachment
			other.ID = "attachment-other"
			return append(f, domain.Fact{Kind: domain.FactAttachmentCreated, SessionID: other.Session, Attachment: &other})
		}},
		{name: "current instance exited", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			return append(f, domain.Fact{Kind: domain.FactInstanceState, InstanceID: "instance-current-beta", State: string(domain.InstanceExited)})
		}},
		{name: "host claim missing", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			return removeLiveFactKind(f, domain.FactClaimHeld)
		}},
		{name: "host claim wrong owner", mutate: func(f []domain.Fact, _ *ProcessIdentity) []domain.Fact {
			liveClaimFact(f).Claim.Instance = "instance-other"
			return f
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := liveAuthorityFacts()
			target := liveTestTarget
			facts = test.mutate(facts, &target)
			resolver := newLiveAuthorityResolver(t, func(context.Context) ([]domain.Fact, error) {
				return facts, nil
			})
			if claim, err := resolver.Resolve(context.Background(), target); err == nil {
				t.Fatalf("Resolve accepted stale or mismatched facts: %+v", claim)
			}
		})
	}
}

func TestLinuxHerdrLiveControllerOrdersAuthorityHostAndPidfdOperations(t *testing.T) {
	events := &liveEventLog{}
	claim := liveTestClaim(t)
	resolver := &fakeLiveClaimResolver{events: events, claim: claim}
	exactHost := &fakeExactAttachmentHost{
		events: events,
		validations: []fakeValidation{
			{evidence: sameLiveEvidence(claim)},
			{evidence: sameLiveEvidence(claim)},
			{evidence: host.ContinuityEvidence(host.ContinuityPaneAbsent, host.Evidence{})},
		},
	}
	process := newFakeExactProcessControl(events)
	controller := newLiveControllerForTest(t, resolver, exactHost, process)

	if _, err := controller.SuspendExactProcess(context.Background(), liveTestTarget); err != nil {
		t.Fatalf("SuspendExactProcess: %v", err)
	}
	wantSuspend := []string{"resolve", "host.validate", "process.acquire", "host.validate", "process.stop", "process.verify-stopped"}
	if got := events.copy(); !reflect.DeepEqual(got, wantSuspend) {
		t.Fatalf("suspend operations = %v, want %v", got, wantSuspend)
	}

	events.reset()
	if _, err := controller.CloseExactPane(context.Background(), liveTestTarget); err != nil {
		t.Fatalf("CloseExactPane: %v", err)
	}
	wantClose := []string{"process.verify-stopped", "resolve", "host.close", "host.validate", "process.release"}
	if got := events.copy(); !reflect.DeepEqual(got, wantClose) {
		t.Fatalf("close operations = %v, want %v", got, wantClose)
	}
}

func TestLinuxHerdrLiveControllerPostAcquireFailureReleasesPidfd(t *testing.T) {
	events := &liveEventLog{}
	claim := liveTestClaim(t)
	resolver := &fakeLiveClaimResolver{events: events, claim: claim}
	exactHost := &fakeExactAttachmentHost{
		events: events,
		validations: []fakeValidation{
			{evidence: sameLiveEvidence(claim)},
			{evidence: host.ContinuityEvidence(host.ContinuityProcessReplaced, evidenceForClaim(claim))},
		},
	}
	process := newFakeExactProcessControl(events)
	controller := newLiveControllerForTest(t, resolver, exactHost, process)

	if _, err := controller.SuspendExactProcess(context.Background(), liveTestTarget); err == nil {
		t.Fatal("SuspendExactProcess accepted failed post-acquire continuity")
	}
	want := []string{"resolve", "host.validate", "process.acquire", "host.validate", "process.release"}
	if got := events.copy(); !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if len(process.acquired) != 0 || len(controller.acquired) != 0 {
		t.Fatalf("post-acquire failure retained process=%d controller=%d targets", len(process.acquired), len(controller.acquired))
	}
}

func TestLinuxHerdrLiveControllerCloseRequiresStableDurableIdentity(t *testing.T) {
	events := &liveEventLog{}
	claim := liveTestClaim(t)
	resolver := &fakeLiveClaimResolver{events: events, claim: claim}
	exactHost := &fakeExactAttachmentHost{events: events, validations: []fakeValidation{
		{evidence: sameLiveEvidence(claim)}, {evidence: sameLiveEvidence(claim)},
	}}
	process := newFakeExactProcessControl(events)
	controller := newLiveControllerForTest(t, resolver, exactHost, process)
	if _, err := controller.SuspendExactProcess(context.Background(), liveTestTarget); err != nil {
		t.Fatalf("SuspendExactProcess: %v", err)
	}

	resolver.claim.Attachment.PaneID = "pane-container-replaced"
	events.reset()
	if _, err := controller.CloseExactPane(context.Background(), liveTestTarget); err == nil || !strings.Contains(err.Error(), "claim changed") {
		t.Fatalf("CloseExactPane error = %v, want changed durable claim", err)
	}
	want := []string{"process.verify-stopped", "resolve"}
	if got := events.copy(); !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if len(process.acquired) != 1 {
		t.Fatalf("unstable close released pidfd; acquired = %d", len(process.acquired))
	}
}

func TestLinuxHerdrLiveControllerRequiresPaneAbsenceBeforeRelease(t *testing.T) {
	events := &liveEventLog{}
	claim := liveTestClaim(t)
	resolver := &fakeLiveClaimResolver{events: events, claim: claim}
	exactHost := &fakeExactAttachmentHost{events: events, validations: []fakeValidation{
		{evidence: sameLiveEvidence(claim)},
		{evidence: sameLiveEvidence(claim)},
		{evidence: host.ContinuityEvidence(host.ContinuityUnproven, evidenceForClaim(claim))},
	}}
	process := newFakeExactProcessControl(events)
	controller := newLiveControllerForTest(t, resolver, exactHost, process)
	if _, err := controller.SuspendExactProcess(context.Background(), liveTestTarget); err != nil {
		t.Fatalf("SuspendExactProcess: %v", err)
	}

	events.reset()
	if _, err := controller.CloseExactPane(context.Background(), liveTestTarget); err == nil || !strings.Contains(err.Error(), "pane absence") {
		t.Fatalf("CloseExactPane error = %v, want pane-absence refusal", err)
	}
	want := []string{"process.verify-stopped", "resolve", "host.close", "host.validate"}
	if got := events.copy(); !reflect.DeepEqual(got, want) {
		t.Fatalf("operations = %v, want %v", got, want)
	}
	if len(process.acquired) != 1 {
		t.Fatalf("unproven absence released pidfd; acquired = %d", len(process.acquired))
	}
}

func TestLinuxHerdrLiveControllerCleanupClosesThenReleasesWithoutKill(t *testing.T) {
	controller, resolver, exactHost, process, events, claim := suspendedLiveController(t)
	exactHost.validations = append(exactHost.validations,
		fakeValidation{evidence: sameLiveEvidence(claim)},
		fakeValidation{evidence: host.ContinuityEvidence(host.ContinuityPaneAbsent, host.Evidence{})},
	)
	events.reset()

	if err := controller.Cleanup(context.Background()); err != nil {
		t.Fatalf("Cleanup: %v", err)
	}
	want := []string{"resolve", "host.validate", "host.close", "host.validate", "process.release"}
	if got := events.copy(); !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup operations = %v, want %v", got, want)
	}
	if resolver.calls < 2 || len(process.acquired) != 0 || len(controller.acquired) != 0 {
		t.Fatalf("cleanup retained target: resolver calls=%d process=%d controller=%d", resolver.calls, len(process.acquired), len(controller.acquired))
	}
}

func TestLinuxHerdrLiveControllerCleanupCloseFailureUsesOnlyExactSIGKILLAndReportsFailure(t *testing.T) {
	controller, _, exactHost, process, events, claim := suspendedLiveController(t)
	closeFailure := errors.New("injected exact close failure")
	exactHost.closeErr = closeFailure
	exactHost.validations = append(exactHost.validations,
		fakeValidation{evidence: sameLiveEvidence(claim)},
		fakeValidation{evidence: sameLiveEvidence(claim)},
		fakeValidation{evidence: sameLiveEvidence(claim)},
	)
	events.reset()

	err := controller.Cleanup(context.Background())
	if !errors.Is(err, closeFailure) || !strings.Contains(err.Error(), "pane cleanup not proven absent") {
		t.Fatalf("Cleanup error = %v, want close and pane-cleanup failures", err)
	}
	want := []string{"resolve", "host.validate", "host.close", "host.validate", "process.kill", "host.validate", "process.release"}
	if got := events.copy(); !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup operations = %v, want %v", got, want)
	}
	if process.killCalls != 1 || len(process.acquired) != 0 {
		t.Fatalf("cleanup kill calls=%d acquired=%d, want one exact kill and release", process.killCalls, len(process.acquired))
	}
}

func TestLinuxHerdrLiveControllerCleanupNeverKillsWithoutExactSameLiveIdentity(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*fakeLiveClaimResolver, *fakeExactAttachmentHost, host.HostAttachmentClaim)
	}{
		{name: "process replaced", mutate: func(_ *fakeLiveClaimResolver, h *fakeExactAttachmentHost, claim host.HostAttachmentClaim) {
			h.validations = append(h.validations, fakeValidation{evidence: host.ContinuityEvidence(host.ContinuityProcessReplaced, evidenceForClaim(claim))})
		}},
		{name: "continuity unproven", mutate: func(_ *fakeLiveClaimResolver, h *fakeExactAttachmentHost, claim host.HostAttachmentClaim) {
			h.validations = append(h.validations, fakeValidation{evidence: host.ContinuityEvidence(host.ContinuityUnproven, evidenceForClaim(claim))})
		}},
		{name: "same live with mismatched evidence", mutate: func(_ *fakeLiveClaimResolver, h *fakeExactAttachmentHost, claim host.HostAttachmentClaim) {
			evidence := evidenceForClaim(claim)
			evidence.PaneID = "pane-other"
			h.validations = append(h.validations, fakeValidation{evidence: host.ContinuityEvidence(host.ContinuitySameLive, evidence)})
		}},
		{name: "durable claim changed", mutate: func(r *fakeLiveClaimResolver, _ *fakeExactAttachmentHost, _ host.HostAttachmentClaim) {
			r.claim.Attachment.PaneID = "pane-other"
		}},
		{name: "process replaced after close failure", mutate: func(_ *fakeLiveClaimResolver, h *fakeExactAttachmentHost, claim host.HostAttachmentClaim) {
			h.closeErr = errors.New("close raced with replacement")
			h.validations = append(h.validations,
				fakeValidation{evidence: sameLiveEvidence(claim)},
				fakeValidation{evidence: host.ContinuityEvidence(host.ContinuityProcessReplaced, evidenceForClaim(claim))},
			)
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			controller, resolver, exactHost, process, events, claim := suspendedLiveController(t)
			test.mutate(resolver, exactHost, claim)
			events.reset()

			if err := controller.Cleanup(context.Background()); err == nil {
				t.Fatal("Cleanup accepted missing or mismatched exact continuity")
			}
			if process.killCalls != 0 {
				t.Fatalf("Cleanup sent %d kill signals without exact same-live identity", process.killCalls)
			}
			if got := events.copy(); len(got) == 0 || got[len(got)-1] != "process.release" {
				t.Fatalf("cleanup operations = %v, want pidfd release on refusal", got)
			}
		})
	}
}

func TestLinuxHerdrLiveControllerCleanupIsIdempotent(t *testing.T) {
	controller, _, exactHost, _, events, claim := suspendedLiveController(t)
	exactHost.validations = append(exactHost.validations,
		fakeValidation{evidence: sameLiveEvidence(claim)},
		fakeValidation{evidence: host.ContinuityEvidence(host.ContinuityPaneAbsent, host.Evidence{})},
	)
	if err := controller.Cleanup(context.Background()); err != nil {
		t.Fatalf("first Cleanup: %v", err)
	}
	events.reset()
	if err := controller.Cleanup(context.Background()); err != nil {
		t.Fatalf("second Cleanup: %v", err)
	}
	if got := events.copy(); len(got) != 0 {
		t.Fatalf("second Cleanup operations = %v, want none", got)
	}
}

func TestLinuxHerdrLiveControllerConstructorsRejectMissingDependencies(t *testing.T) {
	if _, err := NewAuthorityAttachmentClaimResolver(nil, liveTestIntegration); err == nil {
		t.Fatal("resolver constructor accepted nil loader")
	}
	if _, err := NewAuthorityAttachmentClaimResolver(func(context.Context) ([]domain.Fact, error) { return nil, nil }, ""); err == nil {
		t.Fatal("resolver constructor accepted empty integration")
	}
	if _, err := NewLinuxHerdrLiveFaultController(nil, &fakeExactAttachmentHost{}, func() time.Duration { return 0 }); err == nil {
		t.Fatal("public controller constructor accepted nil resolver")
	}

	resolver := &fakeLiveClaimResolver{}
	exactHost := &fakeExactAttachmentHost{}
	process := newFakeExactProcessControl(&liveEventLog{})
	tests := []struct {
		name     string
		resolver attachmentClaimResolver
		host     ExactAttachmentHost
		clock    MonotonicClock
		process  exactProcessControl
	}{
		{name: "resolver", host: exactHost, clock: func() time.Duration { return 0 }, process: process},
		{name: "host", resolver: resolver, clock: func() time.Duration { return 0 }, process: process},
		{name: "clock", resolver: resolver, host: exactHost, process: process},
		{name: "process", resolver: resolver, host: exactHost, clock: func() time.Duration { return 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newLinuxHerdrLiveFaultController(test.resolver, test.host, test.clock, test.process); err == nil {
				t.Fatalf("constructor accepted missing %s", test.name)
			}
		})
	}
}

func liveAuthorityFacts() []domain.Fact {
	const (
		sessionID  = domain.SessionID("session-owner-alpha")
		instanceID = domain.InstanceID("instance-current-beta")
	)
	session := domain.Session{
		ID: sessionID, Workspace: "workspace-fixture", State: domain.SessionActive,
		Current: instanceID, CreatedAt: "2026-09-18T14:23:40.000Z",
	}
	instance := domain.RuntimeInstance{
		ID: instanceID, Session: sessionID, State: domain.InstanceLive,
		StartedAt: "2026-09-18T14:23:40.100Z",
	}
	attachment := domain.HostAttachment{
		ID: domain.AttachmentID(liveTestTarget.AttachmentID), Session: sessionID,
		IntegrationInstance: liveTestIntegration,
		Epoch: domain.HostEpoch{
			Kind: herdrTerminalEpochKind, Scope: domain.EpochScopePane, Value: liveTestTarget.TerminalID,
		},
		Container: "pane-container-zeta",
		Process: domain.ProcessBirth{
			Host: "fixture-host", PID: liveTestTarget.PID, StartedAt: liveTestTarget.StartTime,
			Executable: "/fixture/bin/pi",
		},
		State: domain.Attached, Continuity: domain.ContinuityVerified,
	}
	fingerprint := domain.Fingerprint{
		IntegrationInstance: attachment.IntegrationInstance,
		Epoch:               attachment.Epoch,
		Container:           attachment.Container,
		Process:             attachment.Process,
	}
	claim := domain.Claim{Ref: fingerprint.ClaimRef(), Session: sessionID, Instance: instanceID}
	return []domain.Fact{
		{ID: "fact-session", Kind: domain.FactSessionCreated, Session: &session},
		{ID: "fact-instance", Kind: domain.FactInstanceStarted, SessionID: sessionID, Instance: &instance},
		{ID: "fact-attachment", Kind: domain.FactAttachmentCreated, SessionID: sessionID, Attachment: &attachment},
		{ID: "fact-claim", Kind: domain.FactClaimHeld, Claim: &claim},
	}
}

func liveAttachmentFact(facts []domain.Fact) *domain.Fact {
	for i := range facts {
		if facts[i].Kind == domain.FactAttachmentCreated {
			return &facts[i]
		}
	}
	panic("test facts have no attachment")
}

func liveSessionFact(facts []domain.Fact) *domain.Fact {
	for i := range facts {
		if facts[i].Kind == domain.FactSessionCreated {
			return &facts[i]
		}
	}
	panic("test facts have no session")
}

func liveClaimFact(facts []domain.Fact) *domain.Fact {
	for i := range facts {
		if facts[i].Kind == domain.FactClaimHeld {
			return &facts[i]
		}
	}
	panic("test facts have no claim")
}

func removeLiveFactKind(facts []domain.Fact, kind domain.FactKind) []domain.Fact {
	out := make([]domain.Fact, 0, len(facts))
	for _, fact := range facts {
		if fact.Kind != kind {
			out = append(out, fact)
		}
	}
	return out
}

func newLiveAuthorityResolver(t *testing.T, load AuthoritySnapshotLoader) *AuthorityAttachmentClaimResolver {
	t.Helper()
	resolver, err := NewAuthorityAttachmentClaimResolver(load, liveTestIntegration)
	if err != nil {
		t.Fatalf("NewAuthorityAttachmentClaimResolver: %v", err)
	}
	return resolver
}

func liveTestClaim(t *testing.T) host.HostAttachmentClaim {
	t.Helper()
	resolver := newLiveAuthorityResolver(t, func(context.Context) ([]domain.Fact, error) {
		return liveAuthorityFacts(), nil
	})
	claim, err := resolver.Resolve(context.Background(), liveTestTarget)
	if err != nil {
		t.Fatalf("Resolve test claim: %v", err)
	}
	return claim
}

type liveEventLog struct {
	events []string
}

func (l *liveEventLog) add(event string) { l.events = append(l.events, event) }
func (l *liveEventLog) copy() []string   { return append([]string(nil), l.events...) }
func (l *liveEventLog) reset()           { l.events = nil }

type fakeLiveClaimResolver struct {
	events *liveEventLog
	claim  host.HostAttachmentClaim
	err    error
	calls  int
}

func (r *fakeLiveClaimResolver) Resolve(context.Context, ProcessIdentity) (host.HostAttachmentClaim, error) {
	if r.events != nil {
		r.events.add("resolve")
	}
	r.calls++
	return r.claim, r.err
}

type fakeValidation struct {
	evidence host.HostContinuityEvidence
	err      error
}

type fakeExactAttachmentHost struct {
	events      *liveEventLog
	validations []fakeValidation
	closeErr    error
}

func (h *fakeExactAttachmentHost) ValidateAttachment(context.Context, host.HostAttachmentClaim) (host.HostContinuityEvidence, error) {
	if h.events != nil {
		h.events.add("host.validate")
	}
	if len(h.validations) == 0 {
		return host.HostContinuityEvidence{}, errors.New("unexpected host validation")
	}
	result := h.validations[0]
	h.validations = h.validations[1:]
	return result.evidence, result.err
}

func (h *fakeExactAttachmentHost) CloseExactAttachment(context.Context, host.HostAttachmentClaim) error {
	if h.events != nil {
		h.events.add("host.close")
	}
	return h.closeErr
}

type fakeExactProcessControl struct {
	events    *liveEventLog
	acquired  map[ProcessIdentity]bool
	failures  map[string]error
	killCalls int
}

func newFakeExactProcessControl(events *liveEventLog) *fakeExactProcessControl {
	return &fakeExactProcessControl{events: events, acquired: make(map[ProcessIdentity]bool), failures: make(map[string]error)}
}

func (p *fakeExactProcessControl) operation(name string, _ ProcessIdentity) error {
	p.events.add("process." + name)
	return p.failures[name]
}

func (p *fakeExactProcessControl) AcquireExactTarget(_ context.Context, target ProcessIdentity) error {
	if err := p.operation("acquire", target); err != nil {
		return err
	}
	p.acquired[target] = true
	return nil
}

func (p *fakeExactProcessControl) SendSIGSTOP(_ context.Context, target ProcessIdentity) error {
	return p.operation("stop", target)
}

func (p *fakeExactProcessControl) VerifyStopped(_ context.Context, target ProcessIdentity) error {
	return p.operation("verify-stopped", target)
}

func (p *fakeExactProcessControl) SendSIGKILL(_ context.Context, target ProcessIdentity) error {
	p.killCalls++
	return p.operation("kill", target)
}

func (p *fakeExactProcessControl) Release(target ProcessIdentity) error {
	err := p.operation("release", target)
	delete(p.acquired, target)
	return err
}

func evidenceForClaim(claim host.HostAttachmentClaim) host.Evidence {
	return host.Evidence{
		IntegrationInstanceID: claim.Attachment.IntegrationInstanceID,
		HostServerEpoch:       claim.Attachment.HostServerEpoch,
		HostContainerID:       claim.Attachment.HostContainerID,
		PaneID:                claim.Attachment.PaneID,
		ProcessBirth:          claim.LastKnownProcessBirth,
	}
}

func sameLiveEvidence(claim host.HostAttachmentClaim) host.HostContinuityEvidence {
	return host.ContinuityEvidence(host.ContinuitySameLive, evidenceForClaim(claim))
}

func newLiveControllerForTest(
	t *testing.T,
	resolver attachmentClaimResolver,
	exactHost ExactAttachmentHost,
	process exactProcessControl,
) *LinuxHerdrLiveFaultController {
	t.Helper()
	controller, err := newLinuxHerdrLiveFaultController(resolver, exactHost, func() time.Duration { return 17 * time.Millisecond }, process)
	if err != nil {
		t.Fatalf("newLinuxHerdrLiveFaultController: %v", err)
	}
	return controller
}

func suspendedLiveController(t *testing.T) (
	*LinuxHerdrLiveFaultController,
	*fakeLiveClaimResolver,
	*fakeExactAttachmentHost,
	*fakeExactProcessControl,
	*liveEventLog,
	host.HostAttachmentClaim,
) {
	t.Helper()
	events := &liveEventLog{}
	claim := liveTestClaim(t)
	resolver := &fakeLiveClaimResolver{events: events, claim: claim}
	exactHost := &fakeExactAttachmentHost{events: events, validations: []fakeValidation{
		{evidence: sameLiveEvidence(claim)}, {evidence: sameLiveEvidence(claim)},
	}}
	process := newFakeExactProcessControl(events)
	controller := newLiveControllerForTest(t, resolver, exactHost, process)
	if _, err := controller.SuspendExactProcess(context.Background(), liveTestTarget); err != nil {
		t.Fatalf("SuspendExactProcess: %v", err)
	}
	return controller, resolver, exactHost, process, events, claim
}
