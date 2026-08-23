package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
)

// TestRefusedContinuityProofDegradesTheAttachment is the other way into the
// degraded triple: the host answers, but its answer cannot prove that the
// execution behind the container is the one Duo claimed. The conformance
// matrix's "missing identity or continuity evidence" row applies, so the
// refusal has to leave a durable mark rather than only an error.
func TestRefusedContinuityProofDegradesTheAttachment(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	fp.Process = domain.ProcessBirth{} // a host that cannot report process birth
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	h.reopen()

	err := h.a.ResolveRecovery(ctx, res.Instance, domain.RecoveryEvidence{
		Outcome: domain.RecoverySameLive, Actor: "host", Fingerprint: &fp,
	})
	if !errors.Is(err, domain.ErrIntentRequired) {
		t.Fatalf("ResolveRecovery returned %v, want a refusal", err)
	}
	if c, _ := h.a.Continuity(res.Session); c != domain.ContinuityUnverified {
		t.Fatalf("continuity = %s, want unverified", c)
	}
	if n := len(h.a.Recovering()); n != 1 {
		t.Fatalf("a refused proof resolved the instance anyway (%d recovering)", n)
	}
	if gate := h.a.ExactTargetWrites(res.Session); gate.Allowed ||
		gate.Refusal != domain.WriteRefusalContinuityUnverified {
		t.Fatalf("gate = %+v, want a continuity-unverified refusal", gate)
	}

	// A degraded fingerprint can never re-prove continuity: §4.4 rule 1
	// wants process birth, and a fingerprint that gains process birth is a
	// different claim key. The way out is §6.4's — an explicit restart,
	// which ends the old runtime instance and starts a new one.
	next, err := h.a.Restart(ctx, domain.RestartRequest{
		Session:      res.Session,
		Actor:        "user:beau",
		Attestation:  domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
		ExitEvidence: "herdr reports the pane's process is gone",
		Reason:       "operator restarted the agent in place",
	})
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	// The degraded mark belonged to the old execution generation, so it does
	// not follow the session onto the new one.
	if h.a.ExactTargetWrites(res.Session).Refusal == domain.WriteRefusalContinuityUnverified {
		t.Fatal("the new runtime instance inherited the old one's lost continuity")
	}
	if err := h.a.MarkLive(ctx, next, "host", "herdr reports a live process"); err != nil {
		t.Fatalf("MarkLive: %v", err)
	}
	if gate := h.a.ExactTargetWrites(res.Session); !gate.Allowed {
		t.Fatalf("gate after an explicit restart = %+v, want writes enabled", gate)
	}
}

// TestRepeatEnrollmentParksAgentEvidenceWhileDegraded covers the second way
// an instance report reaches the kernel. §4.2 step 3 still returns the
// existing Duo session — that changes no identity state — but the
// agent-runtime evidence the candidate carries is a report, and it parks.
func TestRepeatEnrollmentParksAgentEvidenceWhileDegraded(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	h.reopen()

	if err := h.a.ResolveRecovery(ctx, res.Instance, domain.RecoveryEvidence{
		Outcome: domain.RecoveryUnreachable, Actor: "host", Detail: "herdr socket refused",
	}); err != nil {
		t.Fatalf("ResolveRecovery: %v", err)
	}

	// Without agent-runtime evidence, a repeat enrollment is unaffected: it
	// reports the session that already holds the fingerprint.
	repeat, err := h.a.Enroll(ctx, domain.EnrollRequest{
		Candidate: candidate("/home/dev/Code/duo", fp), Actor: "user:beau",
	})
	if err != nil || !repeat.Repeat || repeat.Session != res.Session {
		t.Fatalf("repeat enrollment = %+v, err = %v", repeat, err)
	}

	cand := candidate("/home/dev/Code/duo", fp)
	cand.AgentSession = domain.AgentSessionRef{
		IntegrationInstance: "claude@default", SessionID: "agent-1",
	}
	_, err = h.a.Enroll(ctx, domain.EnrollRequest{
		Candidate: cand, Actor: "user:beau",
		Attestation: domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
	})
	if !errors.Is(err, domain.ErrContinuityUnverified) {
		t.Fatalf("enrollment with agent evidence returned %v, want ErrContinuityUnverified", err)
	}
	if n := len(h.a.ParkedReports(res.Session)); n != 1 {
		t.Fatalf("parked %d reports, want 1", n)
	}
	if _, held := h.a.ActiveClaim(cand.AgentSession.ClaimRef()); held {
		t.Fatal("a parked report seized the agent-runtime claim")
	}
}

// TestHostlessSessionHasNoContinuityToLose pins the boundary of the degraded
// triple. Continuity is a property of a host attachment; a session with no
// session host at all — the hostless case the architecture keeps open — has
// none to lose, and its gate answers from the runtime instance alone.
func TestHostlessSessionHasNoContinuityToLose(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	res, err := h.a.Launch(ctx, domain.LaunchRequest{
		RootPath: "/home/dev/Code/duo", Actor: "user:beau", Reason: "hostless launch",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if c, ok := h.a.Continuity(res.Session); !ok || c != domain.ContinuityVerified {
		t.Fatalf("hostless continuity = %s, ok = %v", c, ok)
	}
	if gate := h.a.ExactTargetWrites(res.Session); gate.Allowed ||
		gate.Refusal != domain.WriteRefusalNoLiveInstance {
		t.Fatalf("gate for a starting instance = %+v, want no-live-instance", gate)
	}
	if err := h.a.MarkLive(ctx, res.Instance, "host", "the process is live"); err != nil {
		t.Fatalf("MarkLive: %v", err)
	}
	if gate := h.a.ExactTargetWrites(res.Session); !gate.Allowed {
		t.Fatalf("gate for a live hostless session = %+v, want writes enabled", gate)
	}

	// Recovery still cannot invent an attachment to degrade, and it still
	// refuses to call an unproven instance live.
	h.reopen()
	err = h.a.ResolveRecovery(ctx, res.Instance, domain.RecoveryEvidence{
		Outcome: domain.RecoverySameLive, Actor: "host",
	})
	if !errors.Is(err, domain.ErrIntentRequired) {
		t.Fatalf("same-live recovery without a fingerprint returned %v, want a refusal", err)
	}
	if c, _ := h.a.Continuity(res.Session); c != domain.ContinuityVerified {
		t.Fatalf("hostless continuity moved to %s", c)
	}
}
