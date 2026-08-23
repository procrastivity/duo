package domain_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/store"

	_ "modernc.org/sqlite" // the store's driver, for the lease-expiry helper
)

// expireLease backdates the writer lease so a restarted authority can take it
// over.
//
// A real authority restart is a killed process: its lease row is still in the
// database, unreleased, and the takeover rules in internal/store/lease.go
// admit it once the lease lapses (or once its holder is provably dead). A
// test cannot be provably dead — the pid on the row is this test binary's own
// — so the lapse is what it simulates, exactly as internal/store's own crash
// legs do.
func expireLease(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("opening %s to expire the lease: %v", path, err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.ExecContext(context.Background(),
		`UPDATE writer_lease SET expires_at = '2000-01-01T00:00:00.000Z' WHERE id = 1`); err != nil {
		t.Fatalf("expiring the writer lease: %v", err)
	}
}

// openAuthority opens one authority over path: the store handle, a repository
// over it, and a kernel rebuilt from the durable fact log.
func openAuthority(t *testing.T, path string) (*store.Store, *storerepo.Repo, *domain.Authority) {
	t.Helper()
	s, err := store.OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(%s): %v", path, err)
	}
	repo := storerepo.New(s)
	a, err := domain.Open(context.Background(), repo)
	if err != nil {
		_ = s.Close()
		t.Fatalf("domain.Open: %v", err)
	}
	return s, repo, a
}

// factsOf returns every recorded fact of one kind, from the durable log
// rather than from the kernel's memory.
func factsOf(t *testing.T, repo *storerepo.Repo, kind domain.FactKind) []domain.Fact {
	t.Helper()
	all, err := repo.Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var out []domain.Fact
	for _, f := range all {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// agentCorrelations counts the agent-runtime session correlations on one
// runtime instance. A parked report must add none.
func agentCorrelations(a *domain.Authority, id domain.InstanceID) int {
	n := 0
	for _, c := range a.Correlations(domain.TargetInstance, string(id)) {
		if c.ExternalKind == "agent.session" {
			n++
		}
	}
	return n
}

// TestAuthorityKillAndRestart is decision-01 §4.4 end to end, over a real
// store file, across an un-graceful authority death.
//
// One authority (incarnation A) builds the state a restart has to survive: a
// live instance with proven process birth, two sessions that will turn out to
// contest one live runtime, an instance that exits before the restart, and
// two more for the unreachable-host and incarnation-stamping legs. A is then
// killed — its handle is dropped without Close, so the writer lease is never
// released — and incarnation B replays the log and resolves recovery.
func TestAuthorityKillAndRestart(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "duo.db")
	const root = "/home/dev/Code/duo"

	// --- incarnation A ---------------------------------------------------
	storeA, _, a := openAuthority(t, path)
	incarnationA := a.Incarnation()

	provedFP := herdrFingerprint("w1:p1", "term_a", 4242)
	proved := mustEnroll(t, a, candidate(root, provedFP))

	claimant := mustEnroll(t, a, candidate(root, herdrFingerprint("w1:p2", "term_b", 5353)))
	rival := mustEnroll(t, a, candidate(root, herdrFingerprint("w1:p3", "term_c", 6464)))

	gone := mustEnroll(t, a, candidate(root, herdrFingerprint("w1:p4", "term_d", 7474)))
	if err := a.Exit(ctx, gone.Instance, "host", "herdr reported the pane's process exit"); err != nil {
		t.Fatalf("Exit: %v", err)
	}

	unreachableFP := herdrFingerprint("w1:p5", "term_e", 8484)
	unreachable := mustEnroll(t, a, candidate(root, unreachableFP))

	stamped := mustEnroll(t, a, candidate(root, herdrFingerprint("w1:p6", "term_f", 9494)))

	// --- the kill --------------------------------------------------------
	//
	// storeA is deliberately not closed: Close releases the writer lease,
	// and a killed process does not. Everything committed above must come
	// back from the log alone.
	_ = storeA
	expireLease(t, path)

	// --- incarnation B ---------------------------------------------------
	storeB, repoB, b := openAuthority(t, path)
	t.Cleanup(func() { _ = storeB.Close() })

	t.Run("a restart mints a new authority incarnation", func(t *testing.T) {
		// Behavior 1. The store mints the incarnation with the writer lease;
		// what this asserts is that the kernel surfaces it and stamps it on
		// the facts a restart records, so history can answer which authority
		// run wrote what.
		if incarnationA == "" || b.Incarnation() == "" {
			t.Fatalf("incarnations are %q and %q, want both non-empty", incarnationA, b.Incarnation())
		}
		if b.Incarnation() == incarnationA {
			t.Fatalf("the restarted authority reused incarnation %s", incarnationA)
		}
		if b.Incarnation() != storeB.Incarnation() {
			t.Fatalf("kernel incarnation %s does not match the writer lease's %s",
				b.Incarnation(), storeB.Incarnation())
		}

		// A fact recorded before the kill carries A.
		var found bool
		for _, f := range factsOf(t, repoB, domain.FactSessionEnrolled) {
			if f.SessionID != stamped.Session {
				continue
			}
			found = true
			if f.Incarnation != incarnationA {
				t.Fatalf("pre-restart enrollment fact carries incarnation %q, want %q",
					f.Incarnation, incarnationA)
			}
		}
		if !found {
			t.Fatalf("no enrollment fact for %s survived the restart", stamped.Session)
		}

		// A fact this incarnation records carries B.
		if err := b.ResolveRecovery(ctx, stamped.Instance, domain.RecoveryEvidence{
			Outcome: domain.RecoverySameLive, Actor: "host",
			Fingerprint: ptr(herdrFingerprint("w1:p6", "term_f", 9494)),
			Detail:      "herdr reports the same pane and process",
		}); err != nil {
			t.Fatalf("ResolveRecovery: %v", err)
		}
		decisions := factsOf(t, repoB, domain.FactRecoveryDecision)
		if len(decisions) == 0 {
			t.Fatal("the recovery decision recorded no durable fact")
		}
		for _, f := range decisions {
			if f.Incarnation != b.Incarnation() {
				t.Fatalf("recovery fact carries incarnation %q, want the current %q",
					f.Incarnation, b.Incarnation())
			}
		}
	})

	t.Run("restart preserves a proved-live runtime instance", func(t *testing.T) {
		// Behavior 2, first half. Before any integration answers, the
		// instance is recovering and nothing may write to it.
		if view, _ := b.View(proved.Session); view != domain.ViewRecovering {
			t.Fatalf("view before validation = %s, want recovering", view)
		}
		if gate := b.ExactTargetWrites(proved.Session); gate.Allowed ||
			gate.Refusal != domain.WriteRefusalRecovering {
			t.Fatalf("gate before validation = %+v, want a recovering refusal", gate)
		}

		if err := b.ResolveRecovery(ctx, proved.Instance, domain.RecoveryEvidence{
			Outcome: domain.RecoverySameLive, Actor: "host", Fingerprint: &provedFP,
			Detail: "herdr reports the same pane and the same process birth",
		}); err != nil {
			t.Fatalf("ResolveRecovery(same-live): %v", err)
		}

		session, _ := b.Session(proved.Session)
		if session.Current != proved.Instance {
			t.Fatalf("current instance = %s, want the preserved %s", session.Current, proved.Instance)
		}
		inst, ok := b.Instance(proved.Instance)
		if !ok || inst.State != domain.InstanceLive {
			t.Fatalf("preserved instance = %+v, want a live one", inst)
		}
		if view, _ := b.View(proved.Session); view != domain.ViewAttached {
			t.Fatalf("view after validation = %s, want attached", view)
		}
		claim, held := b.ActiveClaim(provedFP.ClaimRef())
		if !held || claim.Session != proved.Session || claim.Instance != proved.Instance {
			t.Fatalf("claim after the restart = %+v, held=%v", claim, held)
		}
		if gate := b.ExactTargetWrites(proved.Session); !gate.Allowed {
			t.Fatalf("a proved-live session refuses exact-target writes: %+v", gate)
		}
	})

	t.Run("restart quarantines conflicting claims", func(t *testing.T) {
		// Behavior 2, second half. Two Duo sessions answer for one live
		// runtime; §4.4 rule 5 withholds both from automatic control.
		if err := b.ResolveRecovery(ctx, claimant.Instance, domain.RecoveryEvidence{
			Outcome: domain.RecoveryConflicted, Actor: "host", Conflicting: rival.Session,
			Detail: "two duo sessions answer for one live runtime",
		}); err != nil {
			t.Fatalf("ResolveRecovery(conflicted): %v", err)
		}

		for _, id := range []domain.SessionID{claimant.Session, rival.Session} {
			s, _ := b.Session(id)
			if !s.Quarantined {
				t.Fatalf("session %s was not quarantined", id)
			}
			if gate := b.ExactTargetWrites(id); gate.Allowed ||
				gate.Refusal != domain.WriteRefusalQuarantined {
				t.Fatalf("quarantined session %s has gate %+v", id, gate)
			}
			// A quarantine withholds control; it does not release the claim
			// or end the runtime. Releasing would re-open the contested
			// runtime to enrollment, which is the automatic merge §4.2
			// forbids.
			if inst, _ := b.Instance(s.Current); inst.State.Terminal() {
				t.Fatalf("quarantine ended runtime instance %s", s.Current)
			}
		}
		if n := b.ActiveClaims(); n < 2 {
			t.Fatalf("quarantine released claims (%d left)", n)
		}

		// The quarantine lifts only by an owner action, and lifting it does
		// not prove anything about the runtime: the continuity gate still
		// holds the door.
		if err := b.ResolveQuarantine(ctx, claimant.Session, "user:beau",
			domain.Attestation{}, "owner reviewed both sessions"); !errors.Is(err, domain.ErrNotAttested) {
			t.Fatalf("unattested quarantine resolution returned %v, want ErrNotAttested", err)
		}
		if err := b.ResolveQuarantine(ctx, claimant.Session, "user:beau",
			domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
			"owner kept this session and retired the other"); err != nil {
			t.Fatalf("ResolveQuarantine: %v", err)
		}
		s, _ := b.Session(claimant.Session)
		if s.Quarantined {
			t.Fatal("the owner's resolution did not lift the quarantine")
		}
		if gate := b.ExactTargetWrites(claimant.Session); gate.Allowed ||
			gate.Refusal != domain.WriteRefusalContinuityUnverified {
			t.Fatalf("gate after quarantine resolution = %+v, want a continuity refusal", gate)
		}
	})

	t.Run("a late reporter cannot revive an exited instance", func(t *testing.T) {
		// Behavior 3. The instance exited before the kill; the report
		// arrives after the restart, with a perfectly good owner
		// attestation, and still cannot move it.
		before := b.ActiveClaims()
		report := domain.AgentSessionRef{
			IntegrationInstance: "claude@default", SessionID: "agent-late-1",
		}
		err := b.Bind(ctx, domain.BindRequest{
			Session: gone.Session, Instance: gone.Instance, Actor: "claude-hook",
			Attestation:  domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
			AgentSession: report, Transcript: "/home/dev/.claude/projects/duo/late.jsonl",
			Reason: "SessionStart arrived after the process exited",
		})
		if !errors.Is(err, domain.ErrInstanceExited) {
			t.Fatalf("late report returned %v, want ErrInstanceExited", err)
		}
		inst, _ := b.Instance(gone.Instance)
		if inst.State != domain.InstanceExited {
			t.Fatalf("instance state = %s, want exited", inst.State)
		}
		if session, _ := b.Session(gone.Session); session.State != domain.SessionInactive {
			t.Fatalf("session state = %s, want inactive", session.State)
		}
		if _, held := b.ActiveClaim(report.ClaimRef()); held {
			t.Fatal("a late report seized the agent-runtime claim")
		}
		if n := b.ActiveClaims(); n != before {
			t.Fatalf("active claims moved from %d to %d on a late report", before, n)
		}
		if n := agentCorrelations(b, gone.Instance); n != 0 {
			t.Fatalf("a late report added %d agent-runtime correlations", n)
		}
		// It does enrich history: §5.3 keeps the arrival.
		var recorded bool
		for _, f := range factsOf(t, repoB, domain.FactLateReport) {
			if f.InstanceID == gone.Instance {
				recorded = true
			}
		}
		if !recorded {
			t.Fatal("the late report left no durable record")
		}
	})

	t.Run("degraded continuity parks reports and disables exact-target writes", func(t *testing.T) {
		// Behavior 4, all three legs of the triple.
		if err := b.ResolveRecovery(ctx, unreachable.Instance, domain.RecoveryEvidence{
			Outcome: domain.RecoveryUnreachable, Actor: "host",
			Detail: "herdr socket refused the connection",
		}); err != nil {
			t.Fatalf("ResolveRecovery(unreachable): %v", err)
		}

		// Leg 1: the attachment drops to unverified, and rule 4's other
		// half still holds — nothing is inferred about the runtime.
		if c, _ := b.Continuity(unreachable.Session); c != domain.ContinuityUnverified {
			t.Fatalf("continuity = %s, want unverified", c)
		}
		if inst, _ := b.Instance(unreachable.Instance); inst.State != domain.InstanceLive {
			t.Fatalf("an unreachable host changed the instance state to %s", inst.State)
		}
		if _, held := b.ActiveClaim(unreachableFP.ClaimRef()); !held {
			t.Fatal("an unreachable host released the claim reservation")
		}

		// Leg 3: exact-target writes are disabled, and the refusal names
		// the finding rather than the waiting state.
		gate := b.ExactTargetWrites(unreachable.Session)
		if gate.Allowed || gate.Refusal != domain.WriteRefusalContinuityUnverified {
			t.Fatalf("gate = %+v, want a continuity-unverified refusal", gate)
		}

		// Leg 2: an instance report is parked — durably recorded, applied
		// to nothing.
		report := domain.AgentSessionRef{
			IntegrationInstance: "claude@default", SessionID: "agent-degraded-1",
		}
		err := b.Bind(ctx, domain.BindRequest{
			Session: unreachable.Session, Actor: "claude-hook",
			Attestation:  domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
			AgentSession: report, Transcript: "/home/dev/.claude/projects/duo/degraded.jsonl",
			Reason: "SessionStart arrived while the host was unreachable",
		})
		if !errors.Is(err, domain.ErrContinuityUnverified) {
			t.Fatalf("bind during degraded continuity returned %v, want ErrContinuityUnverified", err)
		}
		parked := b.ParkedReports(unreachable.Session)
		if len(parked) != 1 {
			t.Fatalf("parked %d reports, want 1", len(parked))
		}
		if parked[0].AgentSession != report || parked[0].Instance != unreachable.Instance {
			t.Fatalf("parked report = %+v, want the offered evidence", parked[0])
		}
		if _, held := b.ActiveClaim(report.ClaimRef()); held {
			t.Fatal("a parked report seized the agent-runtime claim")
		}
		if n := agentCorrelations(b, unreachable.Instance); n != 0 {
			t.Fatalf("a parked report added %d agent-runtime correlations", n)
		}
		var durable bool
		for _, f := range factsOf(t, repoB, domain.FactReportParked) {
			if f.Parked != nil && f.Parked.ID == parked[0].ID {
				durable = true
			}
		}
		if !durable {
			t.Fatal("the parked report is not in the durable fact log")
		}

		// "Until continuity is re-proven": rule 1 is the re-proof, and it
		// is the only thing that lifts the degraded state.
		if err := b.ResolveRecovery(ctx, unreachable.Instance, domain.RecoveryEvidence{
			Outcome: domain.RecoverySameLive, Actor: "host", Fingerprint: &unreachableFP,
			Detail: "herdr answered again with the same pane and process birth",
		}); err != nil {
			t.Fatalf("ResolveRecovery(same-live after unreachable): %v", err)
		}
		if c, _ := b.Continuity(unreachable.Session); c != domain.ContinuityVerified {
			t.Fatalf("continuity after re-proof = %s, want verified", c)
		}
		if gate := b.ExactTargetWrites(unreachable.Session); !gate.Allowed {
			t.Fatalf("gate after re-proof = %+v, want writes enabled", gate)
		}
		if err := b.Bind(ctx, domain.BindRequest{
			Session: unreachable.Session, Actor: "claude-hook",
			Attestation:  domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
			AgentSession: report,
			Reason:       "the same report, re-offered after continuity was proven",
		}); err != nil {
			t.Fatalf("Bind after re-proof: %v", err)
		}
		if n := agentCorrelations(b, unreachable.Instance); n != 1 {
			t.Fatalf("re-offered report produced %d agent-runtime correlations, want 1", n)
		}
		// The backlog is not replayed automatically: retroactive binding of
		// parked evidence is decision-02's Stage 2 ingestion rule, not this
		// kernel's.
		if n := len(b.ParkedReports(unreachable.Session)); n != 1 {
			t.Fatalf("parked backlog = %d reports, want the original 1 left inert", n)
		}
	})

	t.Run("parked evidence survives another restart", func(t *testing.T) {
		if err := storeB.Close(); err != nil {
			t.Fatalf("closing incarnation B: %v", err)
		}
		storeC, _, c := openAuthority(t, path)
		t.Cleanup(func() { _ = storeC.Close() })

		if c.Incarnation() == b.Incarnation() || c.Incarnation() == incarnationA {
			t.Fatalf("incarnation %s repeats an earlier run", c.Incarnation())
		}
		parked := c.ParkedReports(unreachable.Session)
		if len(parked) != 1 || parked[0].AgentSession.SessionID != "agent-degraded-1" {
			t.Fatalf("replayed parked reports = %+v, want the one recorded before", parked)
		}
		// Still applied to nothing: replay must not turn parked evidence
		// into identity state.
		if n := agentCorrelations(c, unreachable.Instance); n != 1 {
			t.Fatalf("replay produced %d agent-runtime correlations, want only the bound one", n)
		}
	})
}

// ptr returns a pointer to v, for the recovery evidence's optional
// fingerprint.
func ptr[T any](v T) *T { return &v }
