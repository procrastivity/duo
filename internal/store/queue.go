package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// ErrNoWork reports that no work item is claimable. Items whose attempts
// ended unknown_effect are indeterminate and are never claimable again
// without explicit resolution.
var ErrNoWork = errors.New("store: no claimable work")

// claimTTL bounds one external-call attempt. An attempt still unresolved
// after its claim lapses is reconciled (§4.3), never silently re-claimed.
const claimTTL = 60 * time.Second

// WorkItem is one durable work-queue row.
type WorkItem struct {
	ID       int64
	Kind     string
	Payload  string
	Attempts int
}

// WorkClaim is one claimed item plus the attempt row that was committed
// durably before any external call may run (§4.3 step 2).
type WorkClaim struct {
	Item      WorkItem
	AttemptID int64
	Attempt   int

	// ExternalToken is this attempt's retained external identity: the store
	// mints it and commits it with the claim, so the adapter can carry it
	// into the external call (as an idempotency key, a request ID, or
	// whatever the conformance record names) and reconciliation can still
	// find it after a crash. Unique across all attempts, and a retry gets a
	// new one — a retry is legitimate only where the previous attempt is
	// already proven to have had no effect.
	ExternalToken string
}

// Attempt is one work_attempt row, as surfaced to reconciliation.
type Attempt struct {
	ID            int64
	WorkID        int64
	Attempt       int
	Incarnation   string
	State         string
	ExternalToken string
}

// EnqueueWork adds a durable work item inside the surrounding boundary —
// §4.2 admits work items only through command acceptance, so enqueue exists
// on Tx, not on Store.
func (t *Tx) EnqueueWork(kind, payload string) (int64, error) {
	res, err := t.tx.ExecContext(context.Background(),
		`INSERT INTO work_queue (kind, payload, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		kind, payload, t.now, t.now)
	if err != nil {
		return 0, fmt.Errorf("store: %s: enqueue: %w", t.op, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("store: %s: enqueue: %w", t.op, err)
	}
	return id, nil
}

// ClaimWork claims the oldest pending work item and durably records its
// attempt — including its external token — in the same transaction, per
// §4.3: the attempt row exists before the caller makes the external call,
// and the transaction is closed when ClaimWork returns. ErrNoWork when
// nothing is claimable.
func (s *Store) ClaimWork(ctx context.Context) (*WorkClaim, error) {
	var claim *WorkClaim
	err := s.withTx(ctx, "command-transition", func(t *Tx) error {
		var it WorkItem
		err := t.tx.QueryRowContext(ctx,
			`SELECT id, kind, payload, attempts FROM work_queue
			 WHERE state = 'pending' ORDER BY id LIMIT 1`,
		).Scan(&it.ID, &it.Kind, &it.Payload, &it.Attempts)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNoWork
		}
		if err != nil {
			return fmt.Errorf("store: claim: %w", err)
		}

		attempt := it.Attempts + 1
		token, err := newExternalToken()
		if err != nil {
			return err
		}
		if _, err := t.tx.ExecContext(ctx,
			`UPDATE work_queue SET state = 'claimed', attempts = ?, claimed_by = ?,
				claim_expires_at = ?, updated_at = ?
			 WHERE id = ?`,
			attempt, s.incarnation, timestamp(time.Now().Add(claimTTL)), t.now, it.ID); err != nil {
			return fmt.Errorf("store: claim: %w", err)
		}
		res, err := t.tx.ExecContext(ctx,
			`INSERT INTO work_attempt (work_id, attempt, incarnation, external_token, started_at)
			 VALUES (?, ?, ?, ?, ?)`,
			it.ID, attempt, s.incarnation, token, t.now)
		if err != nil {
			return fmt.Errorf("store: claim: recording attempt: %w", err)
		}
		attemptID, err := res.LastInsertId()
		if err != nil {
			return fmt.Errorf("store: claim: recording attempt: %w", err)
		}

		it.Attempts = attempt
		claim = &WorkClaim{
			Item:          it,
			AttemptID:     attemptID,
			Attempt:       attempt,
			ExternalToken: token,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return claim, nil
}

// CompleteWork commits an attempt's successful result and finishes its work
// item (§4.3 step 5).
func (s *Store) CompleteWork(ctx context.Context, attemptID int64, result string) error {
	return s.withTx(ctx, "command-transition", func(t *Tx) error {
		return t.finishAttempt(ctx, attemptID, "succeeded", result, "done")
	})
}

// FailWork commits an attempt's failure. retry=true returns the item to
// pending for another claim — legitimate only when the failure or a
// reconciliation proof establishes the external call had no effect.
func (s *Store) FailWork(ctx context.Context, attemptID int64, result string, retry bool) error {
	next := "failed"
	if retry {
		next = "pending"
	}
	return s.withTx(ctx, "command-transition", func(t *Tx) error {
		return t.finishAttempt(ctx, attemptID, "failed", result, next)
	})
}

// MarkUnknownEffect resolves an attempt whose external effect cannot be
// proven either way: the attempt becomes unknown_effect and its work item
// becomes indeterminate, which no ClaimWork ever selects (§4.3). The
// decision is audited.
func (s *Store) MarkUnknownEffect(ctx context.Context, attemptID int64, reason string) error {
	return s.withTx(ctx, "command-transition", func(t *Tx) error {
		if err := t.finishAttempt(ctx, attemptID, "unknown_effect", "", "indeterminate"); err != nil {
			return err
		}
		return t.AppendAudit(Audit{
			Actor:  "authority",
			Target: fmt.Sprintf("work_attempt:%d", attemptID),
			Reason: reason,
			Detail: "attempt marked unknown_effect; work item indeterminate",
		})
	})
}

// UnresolvedAttempts lists attempts still 'started' under an incarnation
// other than this handle's — the §4.3 reconciliation set after a restart or
// takeover. Each must be reconciled (CompleteWork, FailWork on proof of no
// effect, or MarkUnknownEffect) before its work item can move again.
func (s *Store) UnresolvedAttempts(ctx context.Context) ([]Attempt, error) {
	if s.incarnation == "" {
		return nil, ErrNotAuthority
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, work_id, attempt, incarnation, state, external_token FROM work_attempt
		 WHERE state = 'started' AND incarnation != ? ORDER BY id`,
		s.incarnation)
	if err != nil {
		return nil, fmt.Errorf("store: unresolved attempts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Attempt
	for rows.Next() {
		var a Attempt
		if err := rows.Scan(&a.ID, &a.WorkID, &a.Attempt, &a.Incarnation, &a.State, &a.ExternalToken); err != nil {
			return nil, fmt.Errorf("store: unresolved attempts: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: unresolved attempts: %w", err)
	}
	return out, nil
}

// newExternalToken mints one attempt's external identity. Random rather than
// derived from (work id, attempt), so it stays unique even if a database is
// copied or restored, and so no external system can be addressed by guessing
// it. The "duo-" prefix keeps it recognisable in an external system's logs.
func newExternalToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("store: minting external attempt token: %w", err)
	}
	return "duo-" + hex.EncodeToString(b[:]), nil
}

// finishAttempt closes one still-open attempt and moves its work item, both
// guarded so a finished attempt cannot be finished twice.
func (t *Tx) finishAttempt(ctx context.Context, attemptID int64, attemptState, result, workState string) error {
	res, err := t.tx.ExecContext(ctx,
		`UPDATE work_attempt SET state = ?, result = ?, finished_at = ?
		 WHERE id = ? AND state = 'started'`,
		attemptState, result, t.now, attemptID)
	if err != nil {
		return fmt.Errorf("store: finishing attempt %d: %w", attemptID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: finishing attempt %d: %w", attemptID, err)
	}
	if n == 0 {
		return fmt.Errorf("store: attempt %d is not open", attemptID)
	}
	if _, err := t.tx.ExecContext(ctx,
		`UPDATE work_queue SET state = ?, claimed_by = NULL, claim_expires_at = NULL, updated_at = ?
		 WHERE id = (SELECT work_id FROM work_attempt WHERE id = ?)`,
		workState, t.now, attemptID); err != nil {
		return fmt.Errorf("store: finishing attempt %d: %w", attemptID, err)
	}
	return nil
}
