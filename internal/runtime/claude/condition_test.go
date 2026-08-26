package claude_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/claude"
)

func snapshotClaude(t *testing.T, r *claude.Runtime, transcript string) runtime.ConditionObservation {
	t.Helper()
	got, err := runtime.SnapshotCondition(context.Background(), r, runtime.ConditionObservationRequest{
		TranscriptID: transcript,
	})
	if err != nil {
		t.Fatalf("SnapshotCondition: %v", err)
	}
	return got
}

func writeJSONLPrefix(t *testing.T, src string, n int) string {
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
	path := filepath.Join(t.TempDir(), "prefix.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func TestObserveConditionTable(t *testing.T) {
	r := newConversationRuntime(t)
	printResume := filepath.Join("testdata", "claude", "print-tool-use-resume.jsonl")
	interactive := filepath.Join("testdata", "claude", "interactive-truncated.jsonl")
	interrupts := filepath.Join("testdata", "claude", "new-entry-types.jsonl")
	sidechain := filepath.Join("testdata", "claude", "subagent-sidechain-truncated.jsonl")
	forked := filepath.Join("testdata", "claude", "print-forked-session.jsonl")

	tests := []struct {
		name  string
		path  string
		want  runtime.ConditionValue
		fresh runtime.ConditionFreshness
	}{
		{
			name:  "print fixture settled via stop_reason end_turn",
			path:  printResume,
			want:  runtime.ConditionIdle,
			fresh: runtime.ConditionFreshnessFresh,
		},
		{
			name:  "forked print fixture settled",
			path:  forked,
			want:  runtime.ConditionIdle,
			fresh: runtime.ConditionFreshnessFresh,
		},
		{
			name:  "interactive fixture settled via turn_duration",
			path:  interactive,
			want:  runtime.ConditionIdle,
			fresh: runtime.ConditionFreshnessFresh,
		},
		{
			name:  "in-progress prefix of print fixture (user + tool_use)",
			path:  writeJSONLPrefix(t, printResume, 9),
			want:  runtime.ConditionWorking,
			fresh: runtime.ConditionFreshnessFresh,
		},
		{
			name:  "interrupt user entries close the turn",
			path:  interrupts,
			want:  runtime.ConditionIdle,
			fresh: runtime.ConditionFreshnessFresh,
		},
		{
			name:  "all-sidechain fixture has no main-thread evidence",
			path:  sidechain,
			want:  runtime.ConditionUnknown,
			fresh: runtime.ConditionFreshnessUnknown,
		},
		{
			name:  "missing transcript is unknown",
			path:  filepath.Join(t.TempDir(), "does-not-exist.jsonl"),
			want:  runtime.ConditionUnknown,
			fresh: runtime.ConditionFreshnessUnknown,
		},
		{
			name:  "empty TranscriptID is unknown",
			path:  "",
			want:  runtime.ConditionUnknown,
			fresh: runtime.ConditionFreshnessUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := snapshotClaude(t, r, tc.path)
			if got.Value != tc.want {
				t.Fatalf("Value = %q, want %q (reasons %v)", got.Value, tc.want, got.Reasons)
			}
			if got.Confidence != runtime.ConditionConfidenceInferred {
				t.Fatalf("Confidence = %q, want inferred (not reported)", got.Confidence)
			}
			if got.Freshness != tc.fresh {
				t.Fatalf("Freshness = %q, want %q", got.Freshness, tc.fresh)
			}
			if got.ComputedAt.IsZero() {
				t.Fatalf("ComputedAt is zero")
			}
			if got.Value == runtime.ConditionWorking || got.Value == runtime.ConditionIdle {
				if got.EffectiveAt.IsZero() {
					t.Fatalf("EffectiveAt is zero for %s", got.Value)
				}
			}
			if got.Value == runtime.ConditionExited || got.Value == runtime.ConditionDone {
				t.Fatalf("adapter emitted %s: done is unreachable from turn-end; exited is a caller mapping", got.Value)
			}
		})
	}
}

func TestObserveConditionStreamStaysOpenUntilClose(t *testing.T) {
	r := newConversationRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	stream, err := r.ObserveCondition(ctx, runtime.ConditionObservationRequest{
		TranscriptID: filepath.Join("testdata", "claude", "print-tool-use-resume.jsonl"),
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

	select {
	case <-stream.Observations():
		t.Fatal("stream delivered a second observation or closed without Close")
	case <-time.After(20 * time.Millisecond):
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := <-stream.Observations(); ok {
		t.Fatal("Observations still open after Close")
	}
}

// Missing transcript degrades condition to unknown and does not fail
// Correlate: Bound is identity, not a conversation/condition readiness
// check.
func TestMissingTranscriptUnknownDoesNotFailCorrelate(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "no-such-session.jsonl")

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "session-from-hook",
		TranscriptPath:         missing,
		ReporterCredential:     "duo-reporter-secret",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatalf("Correlate must still bind when the transcript path is missing")
	}
	if evidence.TranscriptID != missing {
		t.Fatalf("TranscriptID = %q, want the claim path", evidence.TranscriptID)
	}

	got := snapshotClaude(t, r, evidence.TranscriptID)
	if got.Value != runtime.ConditionUnknown {
		t.Fatalf("Value = %q, want unknown", got.Value)
	}
	if got.Confidence != runtime.ConditionConfidenceInferred {
		t.Fatalf("Confidence = %q, want inferred", got.Confidence)
	}
}
