package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
)

// TestRestartPlacesLiveInstancesInRecovering is §4.4: a reloaded authority
// puts every previously-live runtime instance in a recovering view until
// validation finishes. It infers nothing on its own.
func TestRestartPlacesLiveInstancesInRecovering(t *testing.T) {
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))

	h.reopen()

	recovering := h.a.Recovering()
	if len(recovering) != 1 || recovering[0] != res.Instance {
		t.Fatalf("recovering = %v, want [%s]", recovering, res.Instance)
	}
	if view, _ := h.a.View(res.Session); view != domain.ViewRecovering {
		t.Fatalf("view = %s, want recovering", view)
	}
	// The claim reservation survives the restart; the pane is not enrollable
	// a second time while it is held.
	if _, held := h.a.ActiveClaim(fp.ClaimRef()); !held {
		t.Fatal("the active claim did not survive the authority restart")
	}
	repeat := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	if !repeat.Repeat || repeat.Session != res.Session {
		t.Fatalf("recovering session was enrolled again: %+v", repeat)
	}
}

// TestSameLiveRecoveryKeepsTheInstanceID is §4.4 rule 1 and §6.3.
func TestSameLiveRecoveryKeepsTheInstanceID(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	h.reopen()

	if err := h.a.ResolveRecovery(ctx, res.Instance, domain.RecoveryEvidence{
		Outcome: domain.RecoverySameLive, Actor: "host", Fingerprint: &fp,
		Detail: "herdr reports the same pane and process",
	}); err != nil {
		t.Fatalf("ResolveRecovery: %v", err)
	}
	if n := len(h.a.Recovering()); n != 0 {
		t.Fatalf("%d instances still recovering, want 0", n)
	}
	if inst, _ := h.a.Instance(res.Instance); inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live", inst.State)
	}
	if view, _ := h.a.View(res.Session); view != domain.ViewAttached {
		t.Fatalf("view = %s, want attached", view)
	}
}

// TestContinuityNeedsProcessBirth guards the probe finding directly: Herdr
// restores a pane under its old pane_id, so container coordinates alone must
// never be accepted as proof that the same execution is still live.
func TestContinuityNeedsProcessBirth(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	fp.Process = domain.ProcessBirth{}
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	h.reopen()

	err := h.a.ResolveRecovery(ctx, res.Instance, domain.RecoveryEvidence{
		Outcome: domain.RecoverySameLive, Actor: "host", Fingerprint: &fp,
	})
	if !errors.Is(err, domain.ErrIntentRequired) {
		t.Fatalf("same-live recovery without process birth returned %v, want a refusal", err)
	}
	if n := len(h.a.Recovering()); n != 1 {
		t.Fatalf("a refused recovery resolved the instance anyway (%d recovering)", n)
	}
}

// TestRecoveryRefusesAChangedProcessBirth is §4.4 rule 3 seen from rule 1's
// side: the container survived, the execution did not.
func TestRecoveryRefusesAChangedProcessBirth(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	h.reopen()

	replacement := herdrFingerprint("w1:p1", "term_a", 9999)
	err := h.a.ResolveRecovery(ctx, res.Instance, domain.RecoveryEvidence{
		Outcome: domain.RecoverySameLive, Actor: "host", Fingerprint: &replacement,
	})
	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("ResolveRecovery returned %v, want a ConflictError", err)
	}

	// Rule 3 is the correct outcome for that evidence: the old instance
	// ends, and only an explicit restart may create its replacement.
	if err := h.a.ResolveRecovery(ctx, res.Instance, domain.RecoveryEvidence{
		Outcome: domain.RecoveryReplaced, Actor: "host", Fingerprint: &replacement,
	}); err != nil {
		t.Fatalf("ResolveRecovery(replaced): %v", err)
	}
	if inst, _ := h.a.Instance(res.Instance); inst.State != domain.InstanceExited {
		t.Fatalf("instance state = %s, want exited", inst.State)
	}
	if s, _ := h.a.Session(res.Session); s.Current != res.Instance {
		t.Fatal("recovery created a replacement instance on its own")
	}
}

// TestUnreachableHostKeepsTheReservation is §4.4 rule 4: "It keeps the claim
// reservation and does not infer exit."
func TestUnreachableHostKeepsTheReservation(t *testing.T) {
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
	if n := len(h.a.Recovering()); n != 1 {
		t.Fatalf("an unreachable host resolved the instance (%d recovering, want 1)", n)
	}
	if inst, _ := h.a.Instance(res.Instance); inst.State != domain.InstanceLive {
		t.Fatalf("an unreachable host changed the instance state to %s", inst.State)
	}
	if _, held := h.a.ActiveClaim(fp.ClaimRef()); !held {
		t.Fatal("an unreachable host released the claim reservation")
	}
}

// TestConflictingClaimQuarantinesBothSessions is §4.4 rule 5.
func TestConflictingClaimQuarantinesBothSessions(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	one := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	two := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p2", "term_b", 5353)))
	h.reopen()

	if err := h.a.ResolveRecovery(ctx, one.Instance, domain.RecoveryEvidence{
		Outcome: domain.RecoveryConflicted, Actor: "host", Conflicting: two.Session,
		Detail: "two sessions answer for one live runtime",
	}); err != nil {
		t.Fatalf("ResolveRecovery: %v", err)
	}
	for _, id := range []domain.SessionID{one.Session, two.Session} {
		s, _ := h.a.Session(id)
		if !s.Quarantined {
			t.Fatalf("session %s is not quarantined", id)
		}
	}
}
