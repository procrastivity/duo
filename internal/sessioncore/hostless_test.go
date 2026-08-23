package sessioncore_test

import (
	"context"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
	"github.com/procrastivity/duo/internal/sessioncore"
)

// TestHostlessFakeSessionReachesNonterminalState is the §5.4 / §10 proof:
// "A session with a fake runtime adapter and no host adapter can reach a
// valid nonterminal lifecycle state." This file imports internal/runtime
// and internal/runtime/fake only — no internal/host import appears here or
// anywhere sessioncore.HostlessSession's definition reaches — so the
// session's progress is driven purely by RuntimeCorrelator evidence, with
// no host attachment in the picture at all.
func TestHostlessFakeSessionReachesNonterminalState(t *testing.T) {
	ctx := context.Background()
	rt := runtimefake.New("fake-runtime-integration")

	sess := sessioncore.NewHostlessSession("session-1", "runtime-instance-1")
	if sess.State != sessioncore.RuntimeInstanceStarting {
		t.Fatalf("new session state = %s, want starting", sess.State)
	}
	if sess.State.Terminal() {
		t.Fatalf("new session must not start in a terminal state")
	}

	evidence, err := rt.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  "fake-runtime-integration",
		ExternalAgentSessionID: "external-session-1",
		TranscriptPath:         "/tmp/transcript.jsonl",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatalf("expected the fake runtime to bind the claim")
	}

	if err := sess.Advance(sessioncore.RuntimeInstanceLive); err != nil {
		t.Fatalf("Advance to live: %v", err)
	}
	if sess.State != sessioncore.RuntimeInstanceLive {
		t.Fatalf("state = %s, want live", sess.State)
	}
	if sess.State.Terminal() {
		t.Fatalf("live must be a nonterminal state")
	}
}

// TestHostlessSessionRejectsIllegalTransitions pins decision-01 §5.1's
// diagram: Exited is terminal, and Starting cannot jump straight to
// StopRequested.
func TestHostlessSessionRejectsIllegalTransitions(t *testing.T) {
	sess := sessioncore.NewHostlessSession("session-2", "runtime-instance-2")

	if err := sess.Advance(sessioncore.RuntimeInstanceStopRequested); err == nil {
		t.Fatalf("expected starting -> stop_requested to be illegal")
	}

	if err := sess.Advance(sessioncore.RuntimeInstanceExited); err != nil {
		t.Fatalf("Advance to exited: %v", err)
	}
	if !sess.State.Terminal() {
		t.Fatalf("exited must report Terminal() true")
	}

	if err := sess.Advance(sessioncore.RuntimeInstanceLive); err == nil {
		t.Fatalf("expected exited -> live to be illegal (exited is terminal)")
	}
}
