package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func enqueueT(t *testing.T, s *Store, payload string) int64 {
	t.Helper()
	var id int64
	if err := s.CommandAcceptTx(context.Background(), func(tx *Tx) error {
		var err error
		id, err = tx.EnqueueWork("test.kind", payload)
		return err
	}); err != nil {
		t.Fatalf("enqueue %q: %v", payload, err)
	}
	return id
}

func TestWorkClaimExclusivity(t *testing.T) {
	s := openAuthorityT(t)
	ctx := context.Background()

	first := enqueueT(t, s, "one")
	second := enqueueT(t, s, "two")

	c1, err := s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if c1.Item.ID != first {
		t.Fatalf("first claim got item %d, want oldest %d", c1.Item.ID, first)
	}
	c2, err := s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if c2.Item.ID != second {
		t.Fatalf("second claim got item %d, want %d — item %d claimed twice", c2.Item.ID, second, c1.Item.ID)
	}
	if _, err := s.ClaimWork(ctx); !errors.Is(err, ErrNoWork) {
		t.Fatalf("third claim = %v, want ErrNoWork", err)
	}
}

func TestAttemptDurableBeforeExternalCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")
	s, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	enqueueT(t, s, "payload")
	claim, err := s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}

	// This is the moment the external call would run. An independent
	// connection must already see the committed attempt row (§4.3: the
	// attempt exists durably before the call, and its transaction closed).
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reader): %v", err)
	}
	defer func() { _ = r.Close() }()
	var state, incarnation, token string
	if err := r.db.QueryRowContext(ctx,
		`SELECT state, incarnation, external_token FROM work_attempt WHERE id = ?`, claim.AttemptID).
		Scan(&state, &incarnation, &token); err != nil {
		t.Fatalf("attempt row not durable before external call: %v", err)
	}
	if state != "started" || incarnation != s.Incarnation() {
		t.Fatalf("attempt row = (%s, %s), want (started, %s)", state, incarnation, s.Incarnation())
	}
	// §4.3's retained external attempt identity has to be durable before
	// the call too: minted afterwards, a crash mid-call leaves a reconciler
	// nothing to look the effect up by.
	if token == "" || token != claim.ExternalToken {
		t.Fatalf("stored external token = %q, claim reported %q", token, claim.ExternalToken)
	}
}

func TestExternalTokenIsPerAttempt(t *testing.T) {
	s := openAuthorityT(t)
	ctx := context.Background()

	enqueueT(t, s, "one")
	enqueueT(t, s, "two")

	first, err := s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("claim one: %v", err)
	}
	second, err := s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("claim two: %v", err)
	}
	if first.ExternalToken == second.ExternalToken {
		t.Fatalf("two attempts share external token %q", first.ExternalToken)
	}

	// A retry is a new attempt and gets a new identity: reusing the old one
	// would invite the external system to dedup a call meant to run.
	if err := s.FailWork(ctx, first.AttemptID, "no effect proven", true); err != nil {
		t.Fatalf("FailWork(retry): %v", err)
	}
	retry, err := s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("re-claim: %v", err)
	}
	if retry.Item.ID != first.Item.ID {
		t.Fatalf("re-claim got item %d, want the retried item %d", retry.Item.ID, first.Item.ID)
	}
	if retry.ExternalToken == first.ExternalToken {
		t.Fatalf("retry reused external token %q", retry.ExternalToken)
	}
}

func TestCompleteAndFailRetry(t *testing.T) {
	s := openAuthorityT(t)
	ctx := context.Background()

	enqueueT(t, s, "done-path")
	claim, err := s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.CompleteWork(ctx, claim.AttemptID, "ok"); err != nil {
		t.Fatalf("CompleteWork: %v", err)
	}
	if err := s.CompleteWork(ctx, claim.AttemptID, "again"); err == nil {
		t.Fatal("completing a finished attempt succeeded, want refusal")
	}
	if _, err := s.ClaimWork(ctx); !errors.Is(err, ErrNoWork) {
		t.Fatalf("done item claimable again: %v", err)
	}

	enqueueT(t, s, "retry-path")
	claim, err = s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := s.FailWork(ctx, claim.AttemptID, "no effect proven", true); err != nil {
		t.Fatalf("FailWork(retry): %v", err)
	}
	claim, err = s.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("re-claim after retryable failure: %v", err)
	}
	if claim.Attempt != 2 {
		t.Fatalf("re-claim attempt = %d, want 2", claim.Attempt)
	}
	if err := s.FailWork(ctx, claim.AttemptID, "gave up", false); err != nil {
		t.Fatalf("FailWork(final): %v", err)
	}
	if _, err := s.ClaimWork(ctx); !errors.Is(err, ErrNoWork) {
		t.Fatalf("failed item claimable again: %v", err)
	}
}

func TestUnknownEffectNeverReclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")
	s1, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(first): %v", err)
	}
	ctx := context.Background()

	enqueueT(t, s1, "will-be-orphaned")
	claim, err := s1.ClaimWork(ctx)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	// Crash between the durable attempt and the result commit.
	_ = s1.db.Close()
	expireLease(t, path)

	s2, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(recovery): %v", err)
	}
	defer func() { _ = s2.Close() }()

	// The orphaned item is not pending; recovery must not re-claim it
	// before reconciling the attempt.
	if _, err := s2.ClaimWork(ctx); !errors.Is(err, ErrNoWork) {
		t.Fatalf("orphaned item auto-claimed: %v", err)
	}

	unresolved, err := s2.UnresolvedAttempts(ctx)
	if err != nil {
		t.Fatalf("UnresolvedAttempts: %v", err)
	}
	if len(unresolved) != 1 || unresolved[0].ID != claim.AttemptID {
		t.Fatalf("unresolved = %+v, want the orphaned attempt %d", unresolved, claim.AttemptID)
	}
	// The new incarnation gets the dead one's external attempt identity: it
	// is what a conformance-defined result lookup would be keyed by, and it
	// is the only thing that could turn this into anything but
	// unknown_effect.
	if unresolved[0].ExternalToken != claim.ExternalToken {
		t.Fatalf("recovered external token = %q, want the crashed attempt's %q",
			unresolved[0].ExternalToken, claim.ExternalToken)
	}

	// No conformance proof available: unknown_effect, indeterminate.
	if err := s2.MarkUnknownEffect(ctx, claim.AttemptID, "no external attempt token defined"); err != nil {
		t.Fatalf("MarkUnknownEffect: %v", err)
	}
	if _, err := s2.ClaimWork(ctx); !errors.Is(err, ErrNoWork) {
		t.Fatalf("indeterminate item claimable: %v", err)
	}
	var workState string
	if err := s2.db.QueryRowContext(ctx,
		`SELECT state FROM work_queue WHERE id = ?`, claim.Item.ID).Scan(&workState); err != nil {
		t.Fatalf("reading work state: %v", err)
	}
	if workState != "indeterminate" {
		t.Fatalf("work state = %q, want indeterminate", workState)
	}
	var audits int
	if err := s2.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM audit_log WHERE incarnation = ? AND operation = 'command-transition'`,
		s2.Incarnation()).Scan(&audits); err != nil {
		t.Fatalf("reading audit rows: %v", err)
	}
	if audits != 1 {
		t.Fatalf("unknown_effect audit rows = %d, want 1", audits)
	}
}
