package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
)

// TestEnrollmentIsIdempotent is decision-01 §4.2 step 3: "Repeating
// enrollment returns the existing Duo session."
func TestEnrollmentIsIdempotent(t *testing.T) {
	h := newHarness(t)
	cand := candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_659ac460306171", 4242))

	first := mustEnroll(t, h.a, cand)
	if first.Repeat {
		t.Fatal("first enrollment reported itself as a repeat")
	}
	second := mustEnroll(t, h.a, cand)

	if second.Session != first.Session {
		t.Fatalf("repeat enrollment returned session %s, want %s", second.Session, first.Session)
	}
	if second.Instance != first.Instance {
		t.Fatalf("repeat enrollment returned instance %s, want %s", second.Instance, first.Instance)
	}
	if !second.Repeat {
		t.Fatal("repeat enrollment did not report itself as a repeat")
	}
	if second.Credential != "" {
		t.Fatal("repeat enrollment minted a second reporter credential")
	}
	if n := len(h.a.Sessions()); n != 1 {
		t.Fatalf("repeat enrollment created %d sessions, want 1", n)
	}
	if n := h.a.ActiveClaims(); n != 1 {
		t.Fatalf("repeat enrollment left %d active claims, want 1", n)
	}

	// The decision must survive an authority restart, not just this
	// process's index.
	h.reopen()
	again, err := h.a.Enroll(context.Background(), domain.EnrollRequest{Candidate: cand, Actor: "user:beau"})
	if err != nil {
		t.Fatalf("Enroll after reopen: %v", err)
	}
	if again.Session != first.Session {
		t.Fatalf("after reopen, enrollment returned session %s, want %s", again.Session, first.Session)
	}
}

// TestConflictingClaimEnrollsNothing is §4.2 step 5: overlapping evidence
// records a conflict and leaves the candidate unenrolled. It never merges.
func TestConflictingClaimEnrollsNothing(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	owner := domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"}
	agent := domain.AgentSessionRef{IntegrationInstance: "claude@default", SessionID: "agent-abc"}

	first := candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242))
	first.AgentSession = agent
	res, err := h.a.Enroll(ctx, domain.EnrollRequest{
		Candidate: first, Actor: "user:beau", Attestation: owner,
	})
	if err != nil {
		t.Fatalf("first Enroll: %v", err)
	}

	sessionsBefore := len(h.a.Sessions())
	claimsBefore := h.a.ActiveClaims()

	// A different pane, but carrying the agent-runtime session the first
	// Duo session already claims: the evidence overlaps two Duo sessions.
	second := candidate("/home/dev/Code/duo", herdrFingerprint("w1:p2", "term_b", 5353))
	second.AgentSession = agent
	_, err = h.a.Enroll(ctx, domain.EnrollRequest{
		Candidate: second, Actor: "user:beau", Attestation: owner,
	})

	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Enroll returned %v, want a ConflictError", err)
	}
	if len(conflict.Holders) != 1 || conflict.Holders[0] != res.Session {
		t.Fatalf("conflict holders = %v, want [%s]", conflict.Holders, res.Session)
	}
	if n := len(h.a.Sessions()); n != sessionsBefore {
		t.Fatalf("conflict created a session: %d, want %d", n, sessionsBefore)
	}
	if n := h.a.ActiveClaims(); n != claimsBefore {
		t.Fatalf("conflict seized a claim: %d active, want %d", n, claimsBefore)
	}

	// Nothing partial survives the restart either, and the conflict is not
	// forgotten: the pane's own claim is still free.
	h.reopen()
	if n := len(h.a.Sessions()); n != sessionsBefore {
		t.Fatalf("after reopen, %d sessions, want %d", n, sessionsBefore)
	}
	if _, held := h.a.ActiveClaim(second.Fingerprint.ClaimRef()); held {
		t.Fatal("the refused candidate's live-runtime claim was seized")
	}
}

// TestWeakEvidenceEnrollsNothing is §4.2's floor plus §4.3's "Duo must not
// guess": evidence that cannot distinguish a live runtime is a conflict, not
// a session.
func TestWeakEvidenceEnrollsNothing(t *testing.T) {
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	fp.Epoch = domain.HostEpoch{} // no epoch-equivalent of any scope

	_, err := h.a.Enroll(context.Background(), domain.EnrollRequest{
		Candidate: candidate("/home/dev/Code/duo", fp), Actor: "user:beau",
	})
	if !errors.Is(err, domain.ErrWeakFingerprint) {
		t.Fatalf("Enroll returned %v, want ErrWeakFingerprint", err)
	}
	if n := len(h.a.Sessions()); n != 0 {
		t.Fatalf("weak evidence created %d sessions, want 0", n)
	}
}

// TestTwoSessionsInOneDirectoryStayDistinct is §6.5. Both sessions share a
// workspace; nothing about identity comes from the shared path.
func TestTwoSessionsInOneDirectoryStayDistinct(t *testing.T) {
	h := newHarness(t)
	root := "/home/dev/Code/duo"

	one := mustEnroll(t, h.a, candidate(root, herdrFingerprint("w1:p1", "term_a", 4242)))
	two := mustEnroll(t, h.a, candidate(root, herdrFingerprint("w1:p2", "term_b", 5353)))

	if one.Session == two.Session {
		t.Fatal("two live runtimes in one directory share a duo session id")
	}
	if one.Instance == two.Instance {
		t.Fatal("two live runtimes in one directory share a runtime-instance id")
	}
	if one.Attachment == two.Attachment {
		t.Fatal("two live runtimes in one directory share a host attachment")
	}
	if one.Credential == two.Credential {
		t.Fatal("two runtime instances share a reporter credential")
	}
	if one.Workspace != two.Workspace {
		t.Fatalf("same root path produced workspaces %s and %s, want one",
			one.Workspace, two.Workspace)
	}
	if n := h.a.ActiveClaims(); n != 2 {
		t.Fatalf("%d active claims, want 2", n)
	}

	// The distinction is durable, not an artifact of this process's index.
	h.reopen()
	sessions := h.a.Sessions()
	if len(sessions) != 2 {
		t.Fatalf("after reopen, %d sessions, want 2", len(sessions))
	}
	for _, s := range sessions {
		if s.Workspace != one.Workspace {
			t.Fatalf("session %s is in workspace %s, want %s", s.ID, s.Workspace, one.Workspace)
		}
	}
}

// TestAgentSessionNeedsAttestation is §3.4 and §6.5: an external
// agent-session ID is not a source. An unattested one is refused, durably,
// rather than guessed at.
func TestAgentSessionNeedsAttestation(t *testing.T) {
	h := newHarness(t)
	cand := candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242))
	cand.AgentSession = domain.AgentSessionRef{
		IntegrationInstance: "claude@default", SessionID: "agent-abc",
	}

	_, err := h.a.Enroll(context.Background(), domain.EnrollRequest{Candidate: cand, Actor: "hook"})
	if !errors.Is(err, domain.ErrNotAttested) {
		t.Fatalf("Enroll returned %v, want ErrNotAttested", err)
	}
	if n := len(h.a.Sessions()); n != 0 {
		t.Fatalf("unattested report created %d sessions, want 0", n)
	}
}

// TestHerdrRestartIsNotTheSameLiveRuntime pins the probe finding that shapes
// the fingerprint: at Herdr 0.8.2 a server restart restores a pane under the
// same pane_id with a new terminal_id (notes/19 §5). Pane coordinates alone
// would collide; the epoch-equivalent is what keeps the two apart.
func TestHerdrRestartIsNotTheSameLiveRuntime(t *testing.T) {
	before := herdrFingerprint("w1:p1", "term_659ac460306171", 4242)
	after := herdrFingerprint("w1:p1", "term_659ac48c658711", 4242)

	if before.ClaimRef() == after.ClaimRef() {
		t.Fatal("a restored pane with a new terminal_id produced the same claim key")
	}

	// And the epoch alone is not enough either: two panes of one incarnation
	// must not collide.
	sameEpochOtherPane := herdrFingerprint("w1:p2", "term_659ac460306171", 4242)
	if before.ClaimRef() == sameEpochOtherPane.ClaimRef() {
		t.Fatal("two containers under one epoch produced the same claim key")
	}
}

// TestDegradedFingerprintIsClaimableButMarked covers §4.2's "when it is
// available": a host that cannot report process birth still enrolls, and the
// claim records that it is weaker.
func TestDegradedFingerprintIsClaimableButMarked(t *testing.T) {
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	fp.Process = domain.ProcessBirth{}

	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	claim, held := h.a.ActiveClaim(fp.ClaimRef())
	if !held {
		t.Fatal("degraded fingerprint did not seize its claim")
	}
	if !claim.Degraded {
		t.Fatal("claim from a process-birth-free fingerprint is not marked degraded")
	}
	if claim.Session != res.Session {
		t.Fatalf("claim holder = %s, want %s", claim.Session, res.Session)
	}
}

// TestPIDAloneIsNotProcessBirth is §3.6: "PID alone is insufficient."
func TestPIDAloneIsNotProcessBirth(t *testing.T) {
	if (domain.ProcessBirth{PID: 4242}).Present() {
		t.Fatal("a PID with no start time counted as process-birth evidence")
	}
	if !(domain.ProcessBirth{PID: 4242, StartedAt: "2026-08-23T10:00:00.000Z"}).Present() {
		t.Fatal("a PID with a start time did not count as process-birth evidence")
	}
}

// TestDurableClaimIndexOutranksAStaleKernel proves the backstop: the store,
// not the in-memory index, is what makes a double claim impossible. The
// second kernel loaded before the first enrolled, so its index says the
// fingerprint is free — and the transaction still commits nothing.
func TestDurableClaimIndexOutranksAStaleKernel(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	stale, err := domain.Open(ctx, testRepo(t, h))
	if err != nil {
		t.Fatalf("second domain.Open: %v", err)
	}
	cand := candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242))
	first := mustEnroll(t, h.a, cand)

	_, err = stale.Enroll(ctx, domain.EnrollRequest{Candidate: cand, Actor: "stale"})
	if !errors.Is(err, domain.ErrClaimTaken) {
		t.Fatalf("stale kernel's Enroll returned %v, want ErrClaimTaken", err)
	}
	if n := len(stale.Sessions()); n != 0 {
		t.Fatalf("refused enrollment left %d sessions in the stale kernel", n)
	}

	h.reopen()
	sessions := h.a.Sessions()
	if len(sessions) != 1 || sessions[0].ID != first.Session {
		t.Fatalf("after reopen, sessions = %v, want only %s", sessions, first.Session)
	}
}

// TestDiscoverClaimsNothing is §5.2's discover verb: it observes and
// describes, and it "does not claim, control, or assign a durable public ID".
func TestDiscoverClaimsNothing(t *testing.T) {
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	cand := candidate("/home/dev/Code/duo", fp)

	found := h.a.Discover(cand)
	if !found.Claimable {
		t.Fatalf("candidate is not claimable: %s", found.Reason)
	}
	if found.Enrolled != "" {
		t.Fatalf("discover reported the candidate enrolled as %s", found.Enrolled)
	}
	if n := len(h.a.Sessions()); n != 0 || h.a.ActiveClaims() != 0 {
		t.Fatal("discover created durable state")
	}

	res := mustEnroll(t, h.a, cand)
	if again := h.a.Discover(cand); again.Enrolled != res.Session {
		t.Fatalf("discover reported %q, want the enrolled session %s", again.Enrolled, res.Session)
	}

	weak := cand
	weak.Fingerprint.Container = ""
	if h.a.Discover(weak).Claimable {
		t.Fatal("a candidate with no exact container was reported claimable")
	}
}
