package storerepo

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/store"
)

// These tests live inside the package so they can set beforeCommit, the same
// crash-injection hook internal/store/tx_test.go uses: a failure after the
// boundary's last write and before its commit is the injection that a
// boundary which only rolls back on early errors would survive.

func openStore(t *testing.T, path string) *store.Store {
	t.Helper()
	s, err := store.OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func streamLen(t *testing.T, s *store.Store, stream string) int {
	t.Helper()
	items, err := s.ReadStream(context.Background(), stream, 0, 1000)
	if err != nil {
		t.Fatalf("ReadStream(%s): %v", stream, err)
	}
	return len(items)
}

func assertNothingRecorded(t *testing.T, s *store.Store, when string) {
	t.Helper()
	for _, stream := range []string{factStream, claimStream} {
		if n := streamLen(t, s, stream); n != 0 {
			t.Fatalf("%s: %s holds %d records, want 0", when, stream, n)
		}
	}
}

func enrollRequest() domain.EnrollRequest {
	return domain.EnrollRequest{
		Actor: "user:beau",
		Candidate: domain.Candidate{
			RootPath: "/home/dev/Code/duo",
			Fingerprint: domain.Fingerprint{
				IntegrationInstance: "herdr@/run/user/1000/herdr.sock",
				Epoch: domain.HostEpoch{
					Kind: "herdr.terminal_id", Value: "term_a", Scope: domain.EpochScopePane,
				},
				Container: "w1:p1",
				Process: domain.ProcessBirth{
					PID: 4242, StartedAt: "2026-08-23T10:00:00.000Z", Executable: "/usr/bin/claude",
				},
			},
		},
	}
}

// TestEnrollmentIsAtomicUnderCrash is decision-01 §4.2's "one atomic
// operation": a crash anywhere inside enrollment leaves no session, no
// runtime instance, no correlation, and no claim — and what did commit
// survives the writer's death.
func TestEnrollmentIsAtomicUnderCrash(t *testing.T) {
	ctx := context.Background()
	injected := errors.New("injected crash")
	path := filepath.Join(t.TempDir(), "duo.db")
	s := openStore(t, path)
	repo := New(s)

	a, err := domain.Open(ctx, repo)
	if err != nil {
		t.Fatalf("domain.Open: %v", err)
	}

	// Crash after every write, before the commit.
	repo.beforeCommit = func() error { return injected }
	if _, err := a.Enroll(ctx, enrollRequest()); !errors.Is(err, injected) {
		t.Fatalf("pre-commit crash: err = %v, want the injected error", err)
	}
	assertNothingRecorded(t, s, "after a pre-commit crash")
	if n := len(a.Sessions()); n != 0 {
		t.Fatalf("a crashed enrollment left %d sessions in the kernel", n)
	}
	if n := a.ActiveClaims(); n != 0 {
		t.Fatalf("a crashed enrollment left %d active claims in the kernel", n)
	}

	// Panic in the same position: the store's deferred rollback must unwind
	// it without leaving the transaction open.
	repo.beforeCommit = func() error { panic("injected panic") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic did not propagate")
			}
		}()
		_, _ = a.Enroll(ctx, enrollRequest())
	}()
	assertNothingRecorded(t, s, "after a panic")

	// The clean run commits everything together.
	repo.beforeCommit = nil
	res, err := a.Enroll(ctx, enrollRequest())
	if err != nil {
		t.Fatalf("clean Enroll: %v", err)
	}
	if n := streamLen(t, s, claimStream); n != 1 {
		t.Fatalf("clean enrollment recorded %d claims, want 1", n)
	}
	if n := streamLen(t, s, factStream); n == 0 {
		t.Fatal("clean enrollment recorded no facts")
	}

	// The other half of atomicity: what committed must come back. Reopen the
	// database under a new authority and rebuild the kernel from the log
	// alone.
	if err := s.Close(); err != nil {
		t.Fatalf("closing the store: %v", err)
	}
	recovered, err := domain.Open(ctx, New(openStore(t, path)))
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	session, ok := recovered.Session(res.Session)
	if !ok {
		t.Fatalf("session %s did not survive the reopen", res.Session)
	}
	if session.Current != res.Instance {
		t.Fatalf("recovered current instance = %s, want %s", session.Current, res.Instance)
	}
	claim, held := recovered.ActiveClaim(enrollRequest().Candidate.Fingerprint.ClaimRef())
	if !held || claim.Session != res.Session {
		t.Fatalf("recovered claim = %+v, want it held by %s", claim, res.Session)
	}
	if n := len(recovered.Sessions()); n != 1 {
		t.Fatalf("recovered %d sessions, want 1 — a crashed enrollment left a partial record", n)
	}
}

// TestEveryDomainBoundaryIsAtomic runs the same injection against all four
// §4.2 boundaries this repository writes through, so none of them can be
// wired to something that commits piecemeal.
func TestEveryDomainBoundaryIsAtomic(t *testing.T) {
	ctx := context.Background()
	injected := errors.New("injected crash")

	boundaries := map[string]func(*Repo) func(context.Context, domain.Change) error{
		"CommitIdentity":          func(r *Repo) func(context.Context, domain.Change) error { return r.CommitIdentity },
		"CommitLaunchResolution":  func(r *Repo) func(context.Context, domain.Change) error { return r.CommitLaunchResolution },
		"CommitCommandAcceptance": func(r *Repo) func(context.Context, domain.Change) error { return r.CommitCommandAcceptance },
		"CommitObservation":       func(r *Repo) func(context.Context, domain.Change) error { return r.CommitObservation },
	}

	for name, pick := range boundaries {
		t.Run(name, func(t *testing.T) {
			s := openStore(t, filepath.Join(t.TempDir(), "duo.db"))
			repo := New(s)
			commit := pick(repo)
			change := domain.Change{
				Facts: []domain.Fact{{
					ID: "fact_probe", Kind: domain.FactSessionState,
					At: "2026-08-23T10:00:00.000Z", Actor: "test",
					SessionID: "ses_probe", State: string(domain.SessionActive),
				}},
				Claims: []domain.ClaimToken{{
					Ref:      domain.ClaimRef{Kind: domain.ClaimLiveRuntime, Key: "probe"},
					Session:  "ses_probe",
					Instance: "ri_probe",
				}},
				Audit: domain.AuditEntry{Actor: "test", Target: "ses_probe"},
			}

			repo.beforeCommit = func() error { return injected }
			if err := commit(ctx, change); !errors.Is(err, injected) {
				t.Fatalf("err = %v, want the injected error", err)
			}
			assertNothingRecorded(t, s, "after a pre-commit crash")

			repo.beforeCommit = nil
			if err := commit(ctx, change); err != nil {
				t.Fatalf("clean commit: %v", err)
			}
			if n := streamLen(t, s, factStream); n != 1 {
				t.Fatalf("clean commit recorded %d facts, want 1", n)
			}
			if n := streamLen(t, s, claimStream); n != 1 {
				t.Fatalf("clean commit recorded %d claims, want 1", n)
			}

			// Re-seizing a held claim entry refuses and commits nothing
			// further: the store's uniqueness, not the caller, is what makes
			// a double claim impossible.
			change.Facts[0].ID = "fact_probe2"
			if err := commit(ctx, change); !errors.Is(err, domain.ErrClaimTaken) {
				t.Fatalf("re-seizing a held claim returned %v, want ErrClaimTaken", err)
			}
			if n := streamLen(t, s, factStream); n != 1 {
				t.Fatalf("a refused commit recorded a fact anyway (%d facts)", n)
			}
		})
	}
}

// TestFactRoundTrip proves the durable encoding keeps every field the kernel
// replays. A field lost here would silently change the model on the next
// authority restart rather than failing a commit.
func TestFactRoundTrip(t *testing.T) {
	original := domain.Fact{
		ID: "fact_1", Kind: domain.FactSessionCreated, At: "2026-08-23T10:00:00.000Z",
		Actor: "user:beau", Reason: "enroll", Evidence: "integration=herdr",
		Workspace: &domain.Workspace{ID: "ws_1", RootPath: "/home/dev", CreatedAt: "t"},
		Session: &domain.Session{
			ID: "ses_1", Workspace: "ws_1", State: domain.SessionActive, Owner: "o",
			Creator: "c", Current: "ri_1", Attachment: "att_1", BoundActor: "actor_1",
			Quarantined: true, CreatedAt: "t",
		},
		Instance: &domain.RuntimeInstance{
			ID: "ri_1", Session: "ses_1", State: domain.InstanceLive,
			CredentialFingerprint: "abc", StartedAt: "t", ExitedAt: "u",
		},
		AgentActor: &domain.AgentActor{ID: "actor_1", Name: "reviewer", CreatedAt: "t"},
		Attachment: &domain.HostAttachment{
			ID: "att_1", Session: "ses_1", IntegrationInstance: "herdr@sock",
			Epoch:     domain.HostEpoch{Kind: "herdr.terminal_id", Value: "term_a", Scope: domain.EpochScopePane},
			Container: "w1:p1", State: domain.Attached,
		},
		Correlation: &domain.Correlation{
			ID: "corr_1", TargetKind: domain.TargetInstance, TargetID: "ri_1",
			ExternalKind: "agent.session", ExternalValue: "agent-abc", Scope: "claude@default",
			Source: "owner", Evidence: "attested", FirstVerifiedAt: "t", LastVerifiedAt: "t",
			ValidUntil: "u", Status: domain.CorrelationActive,
		},
		Claim: &domain.Claim{
			Ref:      domain.ClaimRef{Kind: domain.ClaimLiveRuntime, Key: "key"},
			Session:  "ses_1",
			Instance: "ri_1", Generation: 2, Degraded: true, HeldAt: "t",
		},
		WorkspaceID: "ws_1", SessionID: "ses_1", InstanceID: "ri_1",
		AttachmentID: "att_1", ActorID: "actor_1", CorrelationID: "corr_1",
		State: "active", Detail: "detail",
	}

	payload, err := encodeFact(original)
	if err != nil {
		t.Fatalf("encodeFact: %v", err)
	}
	got, err := decodeFact(payload)
	if err != nil {
		t.Fatalf("decodeFact: %v", err)
	}
	if diff := compareFacts(original, got); diff != "" {
		t.Fatalf("round trip lost %s", diff)
	}
}

// compareFacts reports the first field that did not survive a round trip.
func compareFacts(want, got domain.Fact) string {
	switch {
	case want.ID != got.ID || want.Kind != got.Kind || want.At != got.At:
		return "the fact envelope"
	case want.Actor != got.Actor || want.Reason != got.Reason || want.Evidence != got.Evidence:
		return "the attribution fields"
	case *want.Workspace != *got.Workspace:
		return "the workspace"
	case *want.Session != *got.Session:
		return "the session"
	case *want.Instance != *got.Instance:
		return "the runtime instance"
	case *want.AgentActor != *got.AgentActor:
		return "the agent actor"
	case *want.Attachment != *got.Attachment:
		return "the host attachment"
	case *want.Correlation != *got.Correlation:
		return "the correlation"
	case *want.Claim != *got.Claim:
		return "the claim"
	case want.WorkspaceID != got.WorkspaceID || want.SessionID != got.SessionID ||
		want.InstanceID != got.InstanceID || want.AttachmentID != got.AttachmentID ||
		want.ActorID != got.ActorID || want.CorrelationID != got.CorrelationID:
		return "the transition target ids"
	case want.State != got.State || want.Detail != got.Detail:
		return "the transition value"
	}
	return ""
}

// TestLoadPagesTheWholeFactLog guards the replay read. A truncated Load
// would not fail anything loudly — it would quietly rebuild a smaller model
// on the next authority restart.
func TestLoadPagesTheWholeFactLog(t *testing.T) {
	ctx := context.Background()
	s := openStore(t, filepath.Join(t.TempDir(), "duo.db"))
	repo := New(s)

	const total = loadPage*2 + 7
	change := domain.Change{Facts: make([]domain.Fact, 0, total)}
	for i := range total {
		change.Facts = append(change.Facts, domain.Fact{
			ID:        domain.FactID("fact_" + strconv.Itoa(i)),
			Kind:      domain.FactSessionState,
			At:        "2026-08-23T10:00:00.000Z",
			Actor:     "test",
			SessionID: "ses_probe",
			State:     string(domain.SessionActive),
		})
	}
	if err := repo.CommitIdentity(ctx, change); err != nil {
		t.Fatalf("CommitIdentity: %v", err)
	}

	loaded, err := repo.Load(ctx)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != total {
		t.Fatalf("Load returned %d facts, want %d", len(loaded), total)
	}
	for i, f := range loaded {
		if want := domain.FactID("fact_" + strconv.Itoa(i)); f.ID != want {
			t.Fatalf("fact %d is %s, want %s — the log came back out of order", i, f.ID, want)
		}
	}
}
