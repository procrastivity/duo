// Package fake is the Stage 0 fake session-host adapter: a first-class,
// permanent implementation of the §5.2 Stage-1 host interfaces. Every
// cross-composition gate runs it, so it behaves like a real (if trivial)
// host integration — it holds state, returns evidence shaped like the
// contract requires, and lets a caller script host-side events — rather
// than a test-only stub that only exists to satisfy an interface
// signature.
package fake

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/host"
)

// Host is the fake session-host adapter.
type Host struct {
	mu                    sync.Mutex
	integrationInstanceID string
	nextPID               int
	candidates            []host.HostCandidate
	attachments           map[string]host.Evidence // keyed by paneKey
	unreachable           bool
	streams               []*observationStream
	promptOutcome         host.PromptOutcome
}

var (
	_ host.HostDiscovery           = (*Host)(nil)
	_ host.HostLauncher            = (*Host)(nil)
	_ host.HostAttachmentValidator = (*Host)(nil)
	_ host.HostLifecycleSource     = (*Host)(nil)
	_ host.HostPromptProvider      = (*Host)(nil)
	_ adapter.Factory[*Host]       = Factory{}
)

// New returns a fake host adapter for one integration instance.
func New(integrationInstanceID string) *Host {
	return &Host{
		integrationInstanceID: integrationInstanceID,
		nextPID:               1000,
		attachments:           make(map[string]host.Evidence),
	}
}

// Factory is the fake host's §5.1 adapter factory: a fixed descriptor, an
// always-supported probe, and a New that hands back a fresh Host.
type Factory struct {
	IntegrationInstanceID string
}

// Descriptor implements adapter.Factory.
func (f Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID:                 "fake-host",
		Role:                      adapter.RoleHost,
		BuildVersion:              "stage0",
		SupportedExternalVersions: []string{"*"},
		ConformanceRecordDigest:   "fake-host-v0",
		DiagnosticRedactionPolicy: "none",
	}
}

// Probe implements adapter.Factory. The fake host is always reachable and
// always compatible with itself.
func (f Factory) Probe(context.Context) (adapter.Probe, error) {
	return adapter.Probe{
		DetectedVersion:          "fake",
		ProtocolOrFormatIdentity: "duo-fake-host/v0",
		ConnectionState:          "connected",
		Compatibility:            adapter.CompatibilitySupported,
	}, nil
}

// New implements adapter.Factory.
func (f Factory) New(context.Context, adapter.Probe) (*Host, error) {
	return New(f.IntegrationInstanceID), nil
}

// SeedCandidate registers a discoverable candidate for a later Discover
// call. Test and gate setup use this to script what the fake host reports;
// it is not part of the §5.2 contract.
func (h *Host) SeedCandidate(c host.HostCandidate) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.candidates = append(h.candidates, c)
}

// Discover implements host.HostDiscovery.
func (h *Host) Discover(_ context.Context, req host.DiscoveryRequest) ([]host.HostCandidate, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []host.HostCandidate
	for _, c := range h.candidates {
		if c.Evidence.IntegrationInstanceID != req.IntegrationInstanceID {
			continue
		}
		out = append(out, c)
	}
	return out, nil
}

// PrepareLaunch implements host.HostLauncher. It stages the resolved
// launch tuple without starting anything.
func (h *Host) PrepareLaunch(_ context.Context, req host.HostLaunchRequest) (host.PreparedHostLaunch, error) {
	if req.ResolvedLaunchTuple.IntegrationInstanceID != h.integrationInstanceID {
		return host.PreparedHostLaunch{}, fmt.Errorf(
			"fake host %s: launch tuple for integration instance %s",
			h.integrationInstanceID, req.ResolvedLaunchTuple.IntegrationInstanceID)
	}
	return host.PreparedHostLaunch{
		IntegrationInstanceID: h.integrationInstanceID,
		LaunchResolutionID:    req.ResolvedLaunchTuple.LaunchResolutionID,
		Opaque:                req.ResolvedLaunchTuple,
	}, nil
}

// Start implements host.HostLauncher. It mints a fake but internally
// consistent process-birth evidence and records the resulting attachment
// so a later ValidateAttachment or ObserveHostLifecycle call can find it.
func (h *Host) Start(_ context.Context, prepared host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	if prepared.IntegrationInstanceID != h.integrationInstanceID {
		return host.HostLaunchEvidence{}, fmt.Errorf(
			"fake host %s: prepared launch for integration instance %s",
			h.integrationInstanceID, prepared.IntegrationInstanceID)
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	pid := h.nextPID
	h.nextPID++
	// Truncate to the domain wire grain (materialize.CaptureTimeLayout /
	// millisecond). Bind persists StartedAt at that precision; a later
	// ValidateAttachment claim rebuilt from the attachment must Equal the
	// live birth, so the fake must not keep sub-millisecond noise.
	now := time.Now().UTC().Truncate(time.Millisecond)

	evidence := host.Evidence{
		IntegrationInstanceID: h.integrationInstanceID,
		HostServerEpoch:       "fake-epoch-1",
		HostContainerID:       fmt.Sprintf("fake-container-%d", pid),
		ProcessBirth: host.ProcessBirthEvidence{
			PID:             pid,
			StartTime:       now,
			StartTimeSource: "fake-host",
		},
		PaneID: fmt.Sprintf("fake-pane-%d", pid),
	}
	h.attachments[paneKey(evidence.IntegrationInstanceID, evidence.PaneID)] = evidence

	return host.HostLaunchEvidence{Evidence: evidence, StartedAt: now}, nil
}

// ValidateAttachment implements host.HostAttachmentValidator. Lookup is
// by pane ID (Herdr's addressing), so a replaced terminal or process is
// distinct from a vanished pane. Disconnect makes the call fail with
// host.ErrUnreachable without deleting the pane.
func (h *Host) ValidateAttachment(_ context.Context, claim host.HostAttachmentClaim) (host.HostContinuityEvidence, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.unreachable {
		return host.HostContinuityEvidence{}, host.Unreachable(nil)
	}
	evidence, ok := h.attachments[paneKey(claim.Attachment.IntegrationInstanceID, claim.Attachment.PaneID)]
	if !ok {
		return host.ContinuityEvidence(host.ContinuityPaneAbsent, host.Evidence{}), nil
	}
	if claim.Attachment.HostContainerID != "" && claim.Attachment.HostContainerID != evidence.HostContainerID {
		return host.ContinuityEvidence(host.ContinuityTerminalReplaced, evidence), nil
	}
	if !fakeBirthProven(evidence.ProcessBirth) || !fakeBirthProven(claim.LastKnownProcessBirth) {
		return host.ContinuityEvidence(host.ContinuityUnproven, evidence), nil
	}
	if sameFakeBirth(evidence.ProcessBirth, claim.LastKnownProcessBirth) {
		return host.ContinuityEvidence(host.ContinuitySameLive, evidence), nil
	}
	return host.ContinuityEvidence(host.ContinuityProcessReplaced, evidence), nil
}

// Kill simulates the host-side pane disappearing. It removes the
// attachment record so a later ValidateAttachment reports
// ContinuityPaneAbsent, and, if a HostLifecycleSource stream is open for
// that attachment, emits an exited event.
func (h *Host) Kill(a host.Attachment) {
	h.mu.Lock()
	evidence, ok := h.attachments[paneKey(a.IntegrationInstanceID, a.PaneID)]
	if ok {
		delete(h.attachments, paneKey(a.IntegrationInstanceID, a.PaneID))
	}
	streams := append([]*observationStream(nil), h.streams...)
	h.mu.Unlock()

	if !ok {
		return
	}
	event := host.LifecycleEvent{
		Kind:       host.LifecycleExited,
		Evidence:   evidence,
		ObservedAt: time.Now().UTC(),
	}
	for _, s := range streams {
		if s.matches(a) {
			s.emit(event)
		}
	}
}

// ReplaceProcess keeps the pane and terminal identity but mints a new
// process birth. A later ValidateAttachment against the old claim reports
// ContinuityProcessReplaced, not pane absence. Zero evidence means the
// pane was not on record.
func (h *Host) ReplaceProcess(a host.Attachment) host.Evidence {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := paneKey(a.IntegrationInstanceID, a.PaneID)
	evidence, ok := h.attachments[key]
	if !ok {
		return host.Evidence{}
	}
	pid := h.nextPID
	h.nextPID++
	evidence.ProcessBirth = host.ProcessBirthEvidence{
		PID:             pid,
		StartTime:       time.Now().UTC().Truncate(time.Millisecond),
		StartTimeSource: "fake-host",
	}
	h.attachments[key] = evidence
	return evidence
}

// ReplaceTerminal keeps the pane ID but mints a new host-container
// identity, the way a Herdr restart restores pane_id and always issues a
// new terminal_id. A later ValidateAttachment against the old claim
// reports ContinuityTerminalReplaced.
func (h *Host) ReplaceTerminal(a host.Attachment) host.Evidence {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := paneKey(a.IntegrationInstanceID, a.PaneID)
	evidence, ok := h.attachments[key]
	if !ok {
		return host.Evidence{}
	}
	n := h.nextPID
	h.nextPID++
	evidence.HostContainerID = fmt.Sprintf("fake-container-%d", n)
	h.attachments[key] = evidence
	return evidence
}

// UnproveProcess keeps the pane and terminal but drops proven process
// birth from the live fingerprint so ValidateAttachment reports
// ContinuityUnproven.
func (h *Host) UnproveProcess(a host.Attachment) {
	h.mu.Lock()
	defer h.mu.Unlock()
	key := paneKey(a.IntegrationInstanceID, a.PaneID)
	evidence, ok := h.attachments[key]
	if !ok {
		return
	}
	evidence.ProcessBirth = host.ProcessBirthEvidence{
		PID:             evidence.ProcessBirth.PID,
		StartTimeSource: "unavailable",
	}
	h.attachments[key] = evidence
}

// Disconnect makes later ValidateAttachment calls fail with
// host.ErrUnreachable. Pane records stay; this is a call error, not pane
// absence.
func (h *Host) Disconnect() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unreachable = true
}

// ObserveHostLifecycle implements host.HostLifecycleSource.
func (h *Host) ObserveHostLifecycle(_ context.Context, req host.HostObservationRequest) (host.HostObservationStream, error) {
	s := &observationStream{
		attachment: req.Attachment,
		ch:         make(chan host.LifecycleEvent, 16),
	}
	h.mu.Lock()
	h.streams = append(h.streams, s)
	h.mu.Unlock()
	return s, nil
}

// ScriptPromptOutcome sets what DeliverPrompt returns. Empty restores the
// default: delivered, acknowledged false. The fake can prove no_effect and
// delivered under test control so the step-14 fake pair works without a
// live host.
func (h *Host) ScriptPromptOutcome(outcome host.PromptOutcome) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.promptOutcome = outcome
}

// PromptPath implements host.HostPromptProvider. The fake has no TUI
// composer to clobber, so ComposerSafe is true; quality and realization
// match the native complete-turn offer a real host reports.
func (h *Host) PromptPath(_ context.Context, attachment host.Attachment) (host.PromptPathCandidate, error) {
	if attachment.IntegrationInstanceID != "" && attachment.IntegrationInstanceID != h.integrationInstanceID {
		return host.PromptPathCandidate{}, fmt.Errorf(
			"fake host %s: request for integration instance %s",
			h.integrationInstanceID, attachment.IntegrationInstanceID)
	}
	return host.PromptPathCandidate{
		Quality:      "exact",
		Realization:  "native",
		ComposerSafe: true,
	}, nil
}

// DeliverPrompt implements host.HostPromptProvider. Disconnect proves
// no_effect (the pane accepted no input). A scripted outcome otherwise
// wins; the default is delivered with Acknowledged false.
func (h *Host) DeliverPrompt(_ context.Context, req host.PromptRequest) (host.PromptAttemptResult, error) {
	if req.Attachment.IntegrationInstanceID != "" && req.Attachment.IntegrationInstanceID != h.integrationInstanceID {
		return host.PromptAttemptResult{}, fmt.Errorf(
			"fake host %s: request for integration instance %s",
			h.integrationInstanceID, req.Attachment.IntegrationInstanceID)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.unreachable {
		return host.PromptAttemptResult{Outcome: host.PromptOutcomeNoEffect}, nil
	}
	switch h.promptOutcome {
	case host.PromptOutcomeNoEffect:
		return host.PromptAttemptResult{Outcome: host.PromptOutcomeNoEffect}, nil
	case host.PromptOutcomeUnknownEffect:
		return host.PromptAttemptResult{Outcome: host.PromptOutcomeUnknownEffect}, nil
	default:
		return host.PromptAttemptResult{
			Outcome:      host.PromptOutcomeDelivered,
			Acknowledged: false,
		}, nil
	}
}

// paneKey addresses a pane the way Herdr does: pane_id within one
// integration instance. Terminal identity and process birth are live
// evidence on that pane, not part of the lookup key.
func paneKey(instanceID, paneID string) string {
	return instanceID + "|" + paneID
}

func fakeBirthProven(b host.ProcessBirthEvidence) bool {
	return b.PID > 0 && !b.StartTime.IsZero()
}

func sameFakeBirth(live, claimed host.ProcessBirthEvidence) bool {
	return live.PID == claimed.PID && live.StartTime.Equal(claimed.StartTime)
}

func (s *observationStream) matches(a host.Attachment) bool {
	return s.attachment.IntegrationInstanceID == a.IntegrationInstanceID &&
		s.attachment.PaneID == a.PaneID
}

// observationStream is the fake's host.HostObservationStream
// implementation.
type observationStream struct {
	attachment host.Attachment
	mu         sync.Mutex
	ch         chan host.LifecycleEvent
	closed     bool
}

func (s *observationStream) Events() <-chan host.LifecycleEvent { return s.ch }

func (s *observationStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.ch)
	return nil
}

// emit is a non-blocking best-effort send, holding the same mutex Close
// does so the two can never race into a send-on-closed-channel panic. A
// full stream drops the event rather than blocking the fake's caller,
// matching §9's "Adapter backpressure cannot block the authority writer
// indefinitely." A closed stream drops it too.
func (s *observationStream) emit(e host.LifecycleEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- e:
	default:
	}
}
