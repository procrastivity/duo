package runtime_test

import (
	"context"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

type stubConditionProvider struct {
	obs []runtime.ConditionObservation
	err error
}

func (s stubConditionProvider) ObserveCondition(context.Context, runtime.ConditionObservationRequest) (runtime.ConditionObservationStream, error) {
	if s.err != nil {
		return nil, s.err
	}
	ch := make(chan runtime.ConditionObservation, len(s.obs))
	for _, o := range s.obs {
		ch <- o
	}
	close(ch)
	return stubConditionStream{ch: ch}, nil
}

type stubConditionStream struct {
	ch <-chan runtime.ConditionObservation
}

func (s stubConditionStream) Observations() <-chan runtime.ConditionObservation { return s.ch }

func (s stubConditionStream) Close() error { return nil }

func TestSnapshotConditionTakesLatestOfBufferedHistory(t *testing.T) {
	at := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	p := stubConditionProvider{
		obs: []runtime.ConditionObservation{
			{ObservationID: "obs-1", Value: runtime.ConditionWorking, Confidence: runtime.ConditionConfidenceInferred, Freshness: runtime.ConditionFreshnessFresh, ComputedAt: at},
			{ObservationID: "obs-2", Value: runtime.ConditionIdle, Confidence: runtime.ConditionConfidenceInferred, Freshness: runtime.ConditionFreshnessFresh, ComputedAt: at},
		},
	}

	got, err := runtime.SnapshotCondition(context.Background(), p, runtime.ConditionObservationRequest{
		ExternalAgentSessionID: "external-1",
	})
	if err != nil {
		t.Fatalf("SnapshotCondition: %v", err)
	}
	if got.Value != runtime.ConditionIdle || got.ObservationID != "obs-2" {
		t.Fatalf("got %+v, want latest idle obs-2", got)
	}
}

func TestSnapshotConditionEmptyStreamErrors(t *testing.T) {
	p := stubConditionProvider{}
	_, err := runtime.SnapshotCondition(context.Background(), p, runtime.ConditionObservationRequest{
		ExternalAgentSessionID: "external-1",
	})
	if err == nil {
		t.Fatal("expected error from a stream that closed with no observation")
	}
}

func TestNewStaticConditionStreamEmitsThenWaitsForClose(t *testing.T) {
	at := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	stream := runtime.NewStaticConditionStream(runtime.ConditionObservation{
		ObservationID: "obs-static",
		Value:         runtime.ConditionIdle,
		Confidence:    runtime.ConditionConfidenceInferred,
		Freshness:     runtime.ConditionFreshnessFresh,
		ComputedAt:    at,
	})

	got, err := runtime.SnapshotCondition(context.Background(), staticProvider{stream: stream}, runtime.ConditionObservationRequest{})
	if err != nil {
		t.Fatalf("SnapshotCondition: %v", err)
	}
	if got.ObservationID != "obs-static" || got.Value != runtime.ConditionIdle {
		t.Fatalf("got %+v, want static idle obs", got)
	}
}

type staticProvider struct {
	stream runtime.ConditionObservationStream
}

func (s staticProvider) ObserveCondition(context.Context, runtime.ConditionObservationRequest) (runtime.ConditionObservationStream, error) {
	return s.stream, nil
}
