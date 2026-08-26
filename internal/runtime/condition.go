package runtime

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ConditionValue is the closed schema vocabulary for a runtime-instance
// condition ($defs.condition_value): idle, working, blocked, done, exited,
// unknown.
type ConditionValue string

// Closed condition values. Schema $defs.condition_value.
const (
	ConditionIdle    ConditionValue = "idle"
	ConditionWorking ConditionValue = "working"
	ConditionBlocked ConditionValue = "blocked"
	ConditionDone    ConditionValue = "done"
	ConditionExited  ConditionValue = "exited"
	ConditionUnknown ConditionValue = "unknown"
)

// ConditionConfidence is the closed schema vocabulary for how a condition
// observation was produced ($defs.confidence). It is not the
// adapter-defined Correlate label on RuntimeCorrelationEvidence.
type ConditionConfidence string

// Closed condition-confidence values. Schema $defs.confidence.
const (
	ConditionConfidenceReported  ConditionConfidence = "reported"
	ConditionConfidenceInferred  ConditionConfidence = "inferred"
	ConditionConfidenceHeuristic ConditionConfidence = "heuristic"
	ConditionConfidenceUnknown   ConditionConfidence = "unknown"
)

// ConditionFreshness is the closed schema vocabulary for an observation's
// freshness ($defs.freshness).
type ConditionFreshness string

// Closed condition-freshness values. Schema $defs.freshness.
const (
	ConditionFreshnessFresh   ConditionFreshness = "fresh"
	ConditionFreshnessStale   ConditionFreshness = "stale"
	ConditionFreshnessExpired ConditionFreshness = "expired"
	ConditionFreshnessUnknown ConditionFreshness = "unknown"
)

// ConditionObservationRequest scopes ObserveCondition to one bound
// runtime instance's external identifiers. Field shapes follow
// ConversationReadRequest: the adapter layer does not know Duo session
// IDs. TranscriptID is the Correlate-produced locator (absolute path
// for disk-backed adapters); empty is allowed when the caller has
// identity but no transcript yet.
type ConditionObservationRequest struct {
	ExternalAgentSessionID string
	TranscriptID           string
}

// ConditionObservation is one condition observation. Its vocabulary
// matches $defs.condition_view_data (value, confidence, freshness) plus
// the inspect extras an adapter can fill (observation identity, times,
// degradation reasons). Session ID, runtime-instance ID, and view
// revision are composer/projection fields, not adapter evidence.
//
// Reasons carry degradation notes. Conflict ranking is out of this
// milestone; a single determining source per composition fills the
// public view.
type ConditionObservation struct {
	ObservationID string
	Value         ConditionValue
	Confidence    ConditionConfidence
	Freshness     ConditionFreshness
	EffectiveAt   time.Time
	ComputedAt    time.Time
	Reasons       []string
}

// ConditionObservationStream is an open subscription to condition
// observations for one request. The first observation is the current
// snapshot so session.inspect can read one value without a subscribe
// command. Later items are view changes a Stage 2 subscriber can
// consume. Callers must call Close when done with it.
//
// Adapters emit the current snapshot on open even when the value is
// unknown. They do not have to watch files or install hooks to satisfy
// that contract: a transcript-first adapter computes idle/working/done
// once, emits it, and waits until Close.
type ConditionObservationStream interface {
	Observations() <-chan ConditionObservation
	Close() error
}

// ConditionProvider opens a stream of condition observations for one
// bound runtime instance. §5.3.
type ConditionProvider interface {
	ObserveCondition(context.Context, ConditionObservationRequest) (ConditionObservationStream, error)
}

// SnapshotCondition reads one snapshot from a ConditionProvider: the
// latest observation available after the first one arrives. It always
// closes the stream. session.inspect uses this; no condition subscribe
// command is wired.
//
// "Qualifying" without conflict ranking means every observation from
// this provider for this request. Latest is the last buffered item
// after the first arrives, so an adapter that replays a short history
// at open still yields the current view.
func SnapshotCondition(ctx context.Context, p ConditionProvider, req ConditionObservationRequest) (ConditionObservation, error) {
	stream, err := p.ObserveCondition(ctx, req)
	if err != nil {
		return ConditionObservation{}, err
	}
	defer func() { _ = stream.Close() }()

	var latest ConditionObservation
	select {
	case obs, ok := <-stream.Observations():
		if !ok {
			return ConditionObservation{}, fmt.Errorf("runtime: condition stream closed with no observation")
		}
		latest = obs
	case <-ctx.Done():
		return ConditionObservation{}, ctx.Err()
	}

drain:
	for {
		select {
		case obs, ok := <-stream.Observations():
			if !ok {
				break drain
			}
			latest = obs
		default:
			break drain
		}
	}
	return latest, nil
}

// NewStaticConditionStream returns a ConditionObservationStream that
// emits obs immediately and stays open until Close. Transcript-first
// adapters use this: they compute idle/working/done once, emit it, and
// wait; they do not watch files or install hooks.
func NewStaticConditionStream(obs ConditionObservation) ConditionObservationStream {
	ch := make(chan ConditionObservation, 1)
	ch <- obs
	return &staticConditionStream{ch: ch}
}

type staticConditionStream struct {
	mu     sync.Mutex
	ch     chan ConditionObservation
	closed bool
}

func (s *staticConditionStream) Observations() <-chan ConditionObservation { return s.ch }

func (s *staticConditionStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	close(s.ch)
	return nil
}
