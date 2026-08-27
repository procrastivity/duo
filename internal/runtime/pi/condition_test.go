package pi_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

func snapshotPi(t *testing.T, r *runtimepi.Runtime, sessionID, transcript string) runtime.ConditionObservation {
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

func reasonsContain(got []string, want string) bool {
	for _, r := range got {
		if r == want {
			return true
		}
	}
	return false
}

func writePiPrefix(t *testing.T, src string, n int) string {
	t.Helper()
	raw, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	lines := strings.Split(string(raw), "\n")
	var kept []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
		if len(kept) == n {
			break
		}
	}
	if len(kept) < n {
		t.Fatalf("fixture %s has %d lines, want at least %d", src, len(kept), n)
	}
	path := filepath.Join(t.TempDir(), filepath.Base(src))
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestObserveConditionTable(t *testing.T) {
	r := runtimepi.New("integration-1", runtimepi.WithSessionsRoot(t.TempDir()))

	tests := []struct {
		name        string
		session     string
		path        string
		want        runtime.ConditionValue
		fresh       runtime.ConditionFreshness
		wantReasons []string
	}{
		{
			name:    "basic-with-resume settled (stopReason stop)",
			session: basicSession,
			path:    basicFixture,
			want:    runtime.ConditionIdle,
			fresh:   runtime.ConditionFreshnessFresh,
		},
		{
			name:    "path-as-session-id with matching transcript (Herdr stores path as both)",
			session: basicFixture,
			path:    basicFixture,
			want:    runtime.ConditionIdle,
			fresh:   runtime.ConditionFreshnessFresh,
		},
		{
			name:    "forked settled",
			session: forkedSession,
			path:    forkedFixture,
			want:    runtime.ConditionIdle,
			fresh:   runtime.ConditionFreshnessFresh,
		},
		{
			name:    "injected-abort settled via stopReason aborted (lastSettledAt analog)",
			session: injectedSession,
			path:    injectedFixture,
			want:    runtime.ConditionIdle,
			fresh:   runtime.ConditionFreshnessFresh,
		},
		{
			name:    "in-progress prefix after user message",
			session: basicSession,
			path:    writePiPrefix(t, basicFixture, 4),
			want:    runtime.ConditionWorking,
			fresh:   runtime.ConditionFreshnessFresh,
		},
		{
			name:    "in-progress prefix after stopReason toolUse",
			session: basicSession,
			path:    writePiPrefix(t, basicFixture, 5),
			want:    runtime.ConditionWorking,
			fresh:   runtime.ConditionFreshnessFresh,
		},
		{
			name:        "missing transcript is unknown",
			session:     basicSession,
			path:        filepath.Join(t.TempDir(), "missing.jsonl"),
			want:        runtime.ConditionUnknown,
			fresh:       runtime.ConditionFreshnessUnknown,
			wantReasons: []string{"missing transcript"},
		},
		{
			name:        "empty TranscriptID under an empty sessions root is unknown",
			session:     basicSession,
			path:        "",
			want:        runtime.ConditionUnknown,
			fresh:       runtime.ConditionFreshnessUnknown,
			wantReasons: []string{"missing transcript"},
		},
		{
			name:        "transcript header session-id mismatch is unknown",
			session:     "00000000-0000-0000-0000-000000000001",
			path:        basicFixture,
			want:        runtime.ConditionUnknown,
			fresh:       runtime.ConditionFreshnessUnknown,
			wantReasons: []string{"transcript header does not match session"},
		},
		{
			name:        "header-only transcript has no turn-boundary evidence",
			session:     basicSession,
			path:        writePiPrefix(t, basicFixture, 1),
			want:        runtime.ConditionUnknown,
			fresh:       runtime.ConditionFreshnessUnknown,
			wantReasons: []string{"transcript has no turn-boundary evidence"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := snapshotPi(t, r, tc.session, tc.path)
			if got.Value != tc.want {
				t.Fatalf("Value = %q, want %q (reasons %v)", got.Value, tc.want, got.Reasons)
			}
			if got.Confidence != runtime.ConditionConfidenceInferred {
				t.Fatalf("Confidence = %q, want inferred (not reported, not the claim idle flag)", got.Confidence)
			}
			if got.Freshness != tc.fresh {
				t.Fatalf("Freshness = %q, want %q", got.Freshness, tc.fresh)
			}
			if got.Value == runtime.ConditionExited || got.Value == runtime.ConditionDone {
				t.Fatalf("adapter emitted %s: done is unreachable from turn-end; exited is a caller mapping", got.Value)
			}
			for _, wantReason := range tc.wantReasons {
				if !reasonsContain(got.Reasons, wantReason) {
					t.Fatalf("Reasons %v does not contain %q", got.Reasons, wantReason)
				}
			}
		})
	}
}

// reported-claim-tui.json carries idle and lastSettledAt from agent_settled.
// ObserveCondition does not promote those fields to reported confidence:
// D2 is transcript-first. The matching injected-abort transcript is idle
// at inferred, and EffectiveAt is the aborted assistant timestamp (the
// lastSettledAt analog), not the claim's slightly-later stamp.
func TestObserveConditionDoesNotTreatReportedClaimAsReported(t *testing.T) {
	reported := loadReportedClaim(t)
	if !reported.Idle || reported.LastSettledAt == "" {
		t.Fatalf("fixture reported-claim-tui.json must carry idle and lastSettledAt: %+v", reported)
	}

	r := runtimepi.New("integration-1", runtimepi.WithSessionsRoot(t.TempDir()))
	got := snapshotPi(t, r, injectedSession, injectedFixture)
	if got.Value != runtime.ConditionIdle {
		t.Fatalf("Value = %q, want idle from the aborted transcript", got.Value)
	}
	if got.Confidence != runtime.ConditionConfidenceInferred {
		t.Fatalf("Confidence = %q, want inferred: the claim's idle flag is not rank-2 reported", got.Confidence)
	}
	if got.EffectiveAt.IsZero() {
		t.Fatal("EffectiveAt is zero; want the lastSettledAt analog from stopReason aborted")
	}
	claimAt, err := time.Parse(time.RFC3339Nano, reported.LastSettledAt)
	if err != nil {
		t.Fatalf("parse lastSettledAt: %v", err)
	}
	// agent_settled fires after the transcript line is written; the
	// analog is the aborted entry, not the claim stamp.
	if !got.EffectiveAt.Before(claimAt) && !got.EffectiveAt.Equal(claimAt) {
		t.Fatalf("EffectiveAt %s is after claim lastSettledAt %s", got.EffectiveAt, claimAt)
	}
}

func TestObserveConditionResolvesFromSessionsRoot(t *testing.T) {
	root := seedSessions(t, basicCwd, basicFixture)
	r := runtimepi.New("integration-1", runtimepi.WithSessionsRoot(root))
	got := snapshotPi(t, r, basicSession, "")
	if got.Value != runtime.ConditionIdle {
		t.Fatalf("Value = %q, want idle from the resolved transcript", got.Value)
	}
}

func TestMissingTranscriptUnknownDoesNotFailCorrelate(t *testing.T) {
	r := runtimepi.New("integration-1",
		runtimepi.WithSessionsRoot(t.TempDir()),
		runtimepi.WithReporterCredential(fixtureToken))
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  "integration-1",
		ExternalAgentSessionID: injectedSession,
		WorkingDirectory:       injectedCwd,
		ReporterCredential:     fixtureToken,
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatalf("Correlate must still bind when no transcript file exists")
	}
	if evidence.TranscriptID != "" {
		t.Fatalf("TranscriptID = %q, want empty", evidence.TranscriptID)
	}

	got := snapshotPi(t, r, evidence.ExternalAgentSessionID, evidence.TranscriptID)
	if got.Value != runtime.ConditionUnknown {
		t.Fatalf("Value = %q, want unknown", got.Value)
	}
	if got.Confidence != runtime.ConditionConfidenceInferred {
		t.Fatalf("Confidence = %q, want inferred", got.Confidence)
	}
}

func TestObserveConditionStreamStaysOpenUntilClose(t *testing.T) {
	r := runtimepi.New("integration-1", runtimepi.WithSessionsRoot(t.TempDir()))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stream, err := r.ObserveCondition(ctx, runtime.ConditionObservationRequest{
		ExternalAgentSessionID: basicSession,
		TranscriptID:           basicFixture,
	})
	if err != nil {
		t.Fatalf("ObserveCondition: %v", err)
	}

	select {
	case obs, ok := <-stream.Observations():
		if !ok {
			t.Fatal("stream closed after the snapshot; it must wait until Close")
		}
		if obs.Value != runtime.ConditionIdle {
			t.Fatalf("Value = %q, want idle", obs.Value)
		}
	case <-ctx.Done():
		t.Fatal("timed out waiting for the snapshot")
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
