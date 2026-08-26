// Package fake is the Stage 0 fake agent-runtime adapter: a first-class,
// permanent implementation of the §5.3 runtime interfaces this package
// currently owns (RuntimeCorrelator, ConversationProvider,
// ConditionProvider, RuntimePromptProvider). Every cross-composition
// gate runs it alongside internal/host/fake. Prompt delivery is a
// scriptable stub (SeedPromptEffect); quiet-gate is the delivery composer.
package fake

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
)

// Runtime is the fake agent-runtime adapter.
type Runtime struct {
	mu                    sync.Mutex
	integrationInstanceID string
	bound                 map[string]string // external agent-session ID -> transcript ID
	turns                 map[string][]runtime.ConversationTurn
	conditions            map[string]runtime.ConditionObservation // external agent-session ID -> current snapshot
	conditionStreams      []*conditionStream
	nextPromptEffect      runtime.PromptEffect
}

var (
	_ runtime.RuntimeCorrelator     = (*Runtime)(nil)
	_ runtime.ConversationProvider  = (*Runtime)(nil)
	_ runtime.ConditionProvider     = (*Runtime)(nil)
	_ runtime.RuntimePromptProvider = (*Runtime)(nil)
	_ adapter.Factory[*Runtime]     = Factory{}
)

// New returns a fake runtime adapter for one integration instance.
func New(integrationInstanceID string) *Runtime {
	return &Runtime{
		integrationInstanceID: integrationInstanceID,
		bound:                 make(map[string]string),
		turns:                 make(map[string][]runtime.ConversationTurn),
		conditions:            make(map[string]runtime.ConditionObservation),
	}
}

// Factory is the fake runtime's §5.1 adapter factory: a fixed descriptor,
// an always-supported probe, and a New that hands back a fresh Runtime.
type Factory struct {
	IntegrationInstanceID string
}

// Descriptor implements adapter.Factory.
func (f Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID:                 "fake-runtime",
		Role:                      adapter.RoleRuntime,
		BuildVersion:              "stage0",
		SupportedExternalVersions: []string{"*"},
		ConformanceRecordDigest:   "fake-runtime-v0",
		DiagnosticRedactionPolicy: "none",
	}
}

// Probe implements adapter.Factory. The fake runtime is always reachable
// and always compatible with itself.
func (f Factory) Probe(context.Context) (adapter.Probe, error) {
	return adapter.Probe{
		DetectedVersion:          "fake",
		ProtocolOrFormatIdentity: "duo-fake-runtime/v0",
		ConnectionState:          "connected",
		Compatibility:            adapter.CompatibilitySupported,
	}, nil
}

// New implements adapter.Factory.
func (f Factory) New(context.Context, adapter.Probe) (*Runtime, error) {
	return New(f.IntegrationInstanceID), nil
}

// SeedTranscript registers turns for an external agent-session ID so a
// later ReadConversation call has something to return. Test and gate setup
// use this to script the fake's transcript; it is not part of the §5.3
// contract.
func (r *Runtime) SeedTranscript(externalAgentSessionID string, turns ...runtime.ConversationTurn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns[externalAgentSessionID] = append(r.turns[externalAgentSessionID], turns...)
}

// Correlate implements runtime.RuntimeCorrelator. §5.3: "A transcript path
// or working directory cannot bind a runtime instance" alone, so the fake
// only binds when the claim carries a non-empty ExternalAgentSessionID —
// the one identifier the contract treats as load-bearing.
func (r *Runtime) Correlate(_ context.Context, claim runtime.RuntimeClaim) (runtime.RuntimeCorrelationEvidence, error) {
	if claim.IntegrationInstanceID != r.integrationInstanceID {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
			"fake runtime %s: claim for integration instance %s",
			r.integrationInstanceID, claim.IntegrationInstanceID)
	}
	if claim.ExternalAgentSessionID == "" {
		return runtime.RuntimeCorrelationEvidence{Bound: false}, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	transcriptID, ok := r.bound[claim.ExternalAgentSessionID]
	if !ok {
		transcriptID = "transcript-" + claim.ExternalAgentSessionID
		r.bound[claim.ExternalAgentSessionID] = transcriptID
	}
	return runtime.RuntimeCorrelationEvidence{
		ExternalAgentSessionID: claim.ExternalAgentSessionID,
		TranscriptID:           transcriptID,
		Bound:                  true,
		Confidence:             "fake-exact",
	}, nil
}

// ReadConversation implements runtime.ConversationProvider. The cursor
// format is an opaque decimal offset into the seeded turn slice; callers
// must not construct one themselves other than by round-tripping
// NextCursor.
func (r *Runtime) ReadConversation(_ context.Context, req runtime.ConversationReadRequest) (runtime.ConversationBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	all := r.turns[req.ExternalAgentSessionID]
	offset, err := cursorOffset(req.After)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}
	if offset > len(all) {
		offset = len(all)
	}

	remaining := all[offset:]
	limit := req.Limit
	if limit <= 0 || limit > len(remaining) {
		limit = len(remaining)
	}
	batch := append([]runtime.ConversationTurn(nil), remaining[:limit]...)
	nextOffset := offset + limit

	return runtime.ConversationBatch{
		Turns:      batch,
		NextCursor: strconv.Itoa(nextOffset),
		Complete:   nextOffset >= len(all),
	}, nil
}

func cursorOffset(after string) (int, error) {
	if after == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(after)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("fake runtime: invalid cursor %q", after)
	}
	return n, nil
}

// SeedCondition registers the current condition snapshot for an external
// agent-session ID so a later ObserveCondition / SnapshotCondition call
// has something other than unknown to return. Test and gate setup use
// this to script the fake's condition, including `exited`; mapping
// HostLifecycleSource process-exit onto `exited` stays a caller concern
// and this package does not import internal/host.
func (r *Runtime) SeedCondition(externalAgentSessionID string, obs runtime.ConditionObservation) {
	r.mu.Lock()
	r.conditions[externalAgentSessionID] = obs
	streams := append([]*conditionStream(nil), r.conditionStreams...)
	r.mu.Unlock()
	for _, s := range streams {
		if s.sessionID == externalAgentSessionID {
			s.emit(obs)
		}
	}
}

// ObserveCondition implements runtime.ConditionProvider. The stream
// emits the seeded snapshot immediately (unknown when none was seeded)
// and stays open until Close, matching HostObservationStream.
func (r *Runtime) ObserveCondition(_ context.Context, req runtime.ConditionObservationRequest) (runtime.ConditionObservationStream, error) {
	r.mu.Lock()
	obs, ok := r.conditions[req.ExternalAgentSessionID]
	if !ok {
		obs = runtime.ConditionObservation{
			Value:      runtime.ConditionUnknown,
			Confidence: runtime.ConditionConfidenceUnknown,
			Freshness:  runtime.ConditionFreshnessUnknown,
			ComputedAt: time.Now().UTC(),
		}
	}
	s := &conditionStream{
		sessionID: req.ExternalAgentSessionID,
		ch:        make(chan runtime.ConditionObservation, 16),
	}
	r.conditionStreams = append(r.conditionStreams, s)
	r.mu.Unlock()
	s.emit(obs)
	return s, nil
}

// conditionStream is the fake's runtime.ConditionObservationStream
// implementation.
type conditionStream struct {
	sessionID string
	mu        sync.Mutex
	ch        chan runtime.ConditionObservation
	closed    bool
}

func (s *conditionStream) Observations() <-chan runtime.ConditionObservation { return s.ch }

func (s *conditionStream) Close() error {
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
// full or closed stream drops the observation, matching the host fake's
// lifecycle stream.
func (s *conditionStream) emit(obs runtime.ConditionObservation) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- obs:
	default:
	}
}

// SeedPromptEffect scripts the next DeliverPrompt result. Empty means
// delivered. Test and gate setup use this; it is not part of the §5.3
// contract. Quiet-gate is not implemented here.
func (r *Runtime) SeedPromptEffect(effect runtime.PromptEffect) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextPromptEffect = effect
}

// PromptPath implements runtime.RuntimePromptProvider. The fake always
// offers an exact, native, composer-safe path so a host+runtime pair
// can exercise runtime-path selection without a messaging socket.
func (r *Runtime) PromptPath(context.Context, runtime.RuntimeBinding) (runtime.PromptPathCandidate, error) {
	return runtime.PromptPathCandidate{
		Quality:      "exact",
		Realization:  "native",
		ComposerSafe: true,
	}, nil
}

// DeliverPrompt implements runtime.RuntimePromptProvider. The result is
// whatever SeedPromptEffect last set, defaulting to delivered.
func (r *Runtime) DeliverPrompt(_ context.Context, _ runtime.PromptDeliveryRequest) (runtime.PromptDeliveryResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	effect := r.nextPromptEffect
	if effect == "" {
		effect = runtime.PromptEffectDelivered
	}
	return runtime.PromptDeliveryResult{Effect: effect}, nil
}
