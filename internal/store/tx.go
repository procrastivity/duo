package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Tx is one open transaction inside a named atomic boundary. It exposes only
// the substrate primitives; there is no exported general-purpose transaction,
// because the architecture's §4.2 boundaries are the only legal compositions.
type Tx struct {
	s   *Store
	tx  *sql.Tx
	op  string
	now string
}

// The eight §4.2 atomic boundaries, one named helper each. Every helper runs
// its body in a single fenced transaction: everything the body writes commits
// together or not at all. The Stage 1+ domain adds its tables to these same
// boundaries; the substrate fixes their names and atomicity now.

// EnrollmentTx commits workspace or session enrollment, correlations, the
// runtime instance, and the active live-runtime claim.
func (s *Store) EnrollmentTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, "enrollment", fn)
}

// CommandAcceptTx commits command acceptance, caller idempotency, initial
// responsibility state, the audit link, and the durable work item.
func (s *Store) CommandAcceptTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, "command-accept", fn)
}

// CommandTransitionTx commits one command transition and its attempt or
// reconciliation metadata.
func (s *Store) CommandTransitionTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, "command-transition", fn)
}

// ObservationTx commits one accepted observation, any affected current-view
// revisions, and semantic stream items.
func (s *Store) ObservationTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, "observation", fn)
}

// CollaborationTx commits one collaboration mutation, idempotency decision,
// fact, object version, current view, and deterministic subscription-match
// identities.
func (s *Store) CollaborationTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, "collaboration", fn)
}

// NotificationTx commits one notification or acknowledgment transition and
// its causal links.
func (s *Store) NotificationTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, "notification", fn)
}

// ProjectionInstallTx commits one projection-install ownership change; it
// must complete before filesystem placement reports success.
func (s *Store) ProjectionInstallTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, "projection-install", fn)
}

// LaunchResolutionTx commits one successful launch resolution, its
// launch-resolution record, and the session and runtime-instance links the
// record may later gain. A failed resolution returns an error from fn and
// commits nothing.
func (s *Store) LaunchResolutionTx(ctx context.Context, fn func(*Tx) error) error {
	return s.withTx(ctx, "launch-resolution", fn)
}

// withTx runs fn inside one transaction, fenced to the writer lease: the
// lease row is read first, pinning the WAL snapshot, so a takeover committed
// elsewhere makes this commit fail instead of interleaving two writers.
func (s *Store) withTx(ctx context.Context, op string, fn func(*Tx) error) error {
	if s.incarnation == "" {
		return ErrNotAuthority
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: %s: %w", op, err)
	}
	// Rollback after a successful commit is a no-op; this also unwinds a
	// panic in fn without leaving the transaction open.
	defer func() { _ = tx.Rollback() }()

	var holder string
	if err := tx.QueryRowContext(ctx,
		`SELECT incarnation FROM writer_lease WHERE id = 1`).Scan(&holder); err != nil {
		return fmt.Errorf("store: %s: lease fence: %w", op, err)
	}
	if holder != s.incarnation {
		return ErrLeaseLost
	}

	t := &Tx{s: s, tx: tx, op: op, now: timestamp(time.Now())}
	if err := fn(t); err != nil {
		return err
	}
	if s.beforeCommit != nil {
		if err := s.beforeCommit(op); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: %s: commit: %w", op, err)
	}
	return nil
}
