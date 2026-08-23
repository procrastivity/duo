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
	attachments           map[string]host.Evidence // keyed by attachmentKey
	streams               []*observationStream
}

var (
	_ host.HostDiscovery           = (*Host)(nil)
	_ host.HostLauncher            = (*Host)(nil)
	_ host.HostAttachmentValidator = (*Host)(nil)
	_ host.HostLifecycleSource     = (*Host)(nil)
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
	now := time.Now().UTC()

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
	h.attachments[attachmentKey(host.Attachment{
		IntegrationInstanceID: evidence.IntegrationInstanceID,
		HostServerEpoch:       evidence.HostServerEpoch,
		HostContainerID:       evidence.HostContainerID,
		PaneID:                evidence.PaneID,
	})] = evidence

	return host.HostLaunchEvidence{Evidence: evidence, StartedAt: now}, nil
}

// ValidateAttachment implements host.HostAttachmentValidator. It reports
// SameProcess true only when the fake host's recorded process-birth
// evidence for the claimed attachment still matches the caller's last
// known evidence exactly, and the attachment is still on record (an
// attachment the fake never Started, or one Kill removed, reports
// SameProcess false with zero evidence).
func (h *Host) ValidateAttachment(_ context.Context, claim host.HostAttachmentClaim) (host.HostContinuityEvidence, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	evidence, ok := h.attachments[attachmentKey(claim.Attachment)]
	if !ok {
		return host.HostContinuityEvidence{SameProcess: false}, nil
	}
	same := evidence.ProcessBirth == claim.LastKnownProcessBirth
	return host.HostContinuityEvidence{Evidence: evidence, SameProcess: same}, nil
}

// Kill simulates the host-side process behind an attachment exiting. It
// removes the attachment record (so a later ValidateAttachment reports
// SameProcess false) and, if a HostLifecycleSource stream is open for that
// attachment, emits an exited event.
func (h *Host) Kill(a host.Attachment) {
	h.mu.Lock()
	evidence, ok := h.attachments[attachmentKey(a)]
	if ok {
		delete(h.attachments, attachmentKey(a))
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
		if s.attachment == a {
			s.emit(event)
		}
	}
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

// attachmentKey collapses a HostAttachment to a comparable map key.
func attachmentKey(a host.Attachment) string {
	return a.IntegrationInstanceID + "|" + a.HostServerEpoch + "|" + a.HostContainerID + "|" + a.PaneID
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
