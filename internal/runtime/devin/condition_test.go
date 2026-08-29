package devin_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/devin"
)

func snapshotDevin(t *testing.T, r *devin.Runtime, sessionID, transcript string) runtime.ConditionObservation {
	t.Helper()
	got, err := runtime.SnapshotCondition(context.Background(), r, runtime.ConditionObservationRequest{
		ExternalAgentSessionID: sessionID,
		TranscriptID:           transcript,
	})
	if err != nil {
		t.Fatalf("SnapshotCondition: %v", err)
	}
	return got
}

func TestObserveConditionUserOnlyWorking(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	got := snapshotDevin(t, r, "special-platinum", testdataATIF(t, "atif-user-only.json"))
	if got.Value != runtime.ConditionWorking {
		t.Fatalf("value = %s, want working", got.Value)
	}
	if got.Confidence != runtime.ConditionConfidenceInferred {
		t.Fatalf("confidence = %s, want inferred", got.Confidence)
	}
	if !reasonsContain(got.Reasons, "open turn after user prompt") {
		t.Fatalf("reasons = %v", got.Reasons)
	}
}

func TestObserveConditionUserAndAgentIdle(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	got := snapshotDevin(t, r, "special-platinum", testdataATIF(t, "atif-user-assistant.json"))
	if got.Value != runtime.ConditionIdle {
		t.Fatalf("value = %s, want idle", got.Value)
	}
	if !reasonsContain(got.Reasons, "inferred from ATIF agent step") {
		t.Fatalf("reasons = %v", got.Reasons)
	}
}

func TestObserveConditionMissingPathUnknown(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	got := snapshotDevin(t, r, "special-platinum", filepath.Join(t.TempDir(), "no-such-atif.json"))
	if got.Value != runtime.ConditionUnknown {
		t.Fatalf("value = %s, want unknown", got.Value)
	}
	if !reasonsContain(got.Reasons, "missing transcript") {
		t.Fatalf("reasons = %v, want missing transcript", got.Reasons)
	}
}

func TestObserveConditionEmptyTranscriptUnknown(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	got := snapshotDevin(t, r, "special-platinum", "")
	if got.Value != runtime.ConditionUnknown {
		t.Fatalf("value = %s, want unknown", got.Value)
	}
	if !reasonsContain(got.Reasons, "missing transcript") {
		t.Fatalf("reasons = %v", got.Reasons)
	}
}

func TestObserveConditionHeaderMismatch(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	got := snapshotDevin(t, r, "special-platinum", testdataATIF(t, "atif-other-session.json"))
	if got.Value != runtime.ConditionUnknown {
		t.Fatalf("value = %s, want unknown", got.Value)
	}
	if reasonsContain(got.Reasons, "missing transcript") {
		t.Fatalf("header mismatch must not collapse to missing transcript: %v", got.Reasons)
	}
	if !reasonsContain(got.Reasons, "transcript header does not match session") {
		t.Fatalf("reasons = %v", got.Reasons)
	}
}

func reasonsContain(got []string, want string) bool {
	for _, r := range got {
		if r == want {
			return true
		}
	}
	return false
}
