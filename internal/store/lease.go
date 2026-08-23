package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// leaseTTL is how long an acquired writer lease stays valid without renewal.
// An authority process renews well inside this window; a process that stops
// renewing (crash, freeze) becomes takeover-eligible when it lapses.
const leaseTTL = 30 * time.Second

// ErrLeaseLost reports that this handle's writer lease is no longer the one
// recorded in the store — another writer took over. The transaction that
// detected it was rolled back.
var ErrLeaseLost = errors.New("store: writer lease lost to another authority")

// ErrNotAuthority reports a write attempted through a handle that never
// acquired the writer lease (Open instead of OpenAuthority).
var ErrNotAuthority = errors.New("store: handle holds no writer lease")

// WriterActiveError is the typed refusal a second concurrent writer
// receives: the lease is held, unexpired, and its holder is not provably
// dead.
type WriterActiveError struct {
	Incarnation string
	PID         int
	Hostname    string
	ExpiresAt   string
}

func (e *WriterActiveError) Error() string {
	return fmt.Sprintf(
		"store: authority writer already active (incarnation %s, pid %d on %s, lease expires %s)",
		e.Incarnation, e.PID, e.Hostname, e.ExpiresAt)
}

// OpenAuthority opens the store at path and acquires the exclusive
// authority-writer lease, minting a fresh incarnation ID. When a live writer
// already holds the lease it fails with *WriterActiveError. See
// docs/store/decisions.md for the takeover rules.
func OpenAuthority(path string) (*Store, error) {
	s, err := Open(path)
	if err != nil {
		return nil, err
	}
	if err := s.acquireLease(context.Background(), time.Now()); err != nil {
		_ = s.db.Close()
		return nil, err
	}
	return s, nil
}

// acquireLease claims the writer lease inside one immediate transaction.
// Takeover of an existing lease is allowed only when the lease has expired,
// or when its holder ran on this host and the process is provably gone.
func (s *Store) acquireLease(ctx context.Context, now time.Time) error {
	incarnation, err := newIncarnation()
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		return fmt.Errorf("store: lease hostname: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("store: lease acquire: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var held struct {
		incarnation string
		pid         int
		hostname    string
		expiresAt   string
	}
	err = tx.QueryRowContext(ctx,
		`SELECT incarnation, pid, hostname, expires_at FROM writer_lease WHERE id = 1`,
	).Scan(&held.incarnation, &held.pid, &held.hostname, &held.expiresAt)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		// No holder; claim below.
	case err != nil:
		return fmt.Errorf("store: lease acquire: %w", err)
	default:
		expired := held.expiresAt <= timestamp(now)
		holderDead := held.hostname == hostname && !processAlive(held.pid)
		if !expired && !holderDead {
			return &WriterActiveError{
				Incarnation: held.incarnation,
				PID:         held.pid,
				Hostname:    held.hostname,
				ExpiresAt:   held.expiresAt,
			}
		}
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO writer_lease (id, incarnation, pid, hostname, acquired_at, expires_at)
		 VALUES (1, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET
			incarnation = excluded.incarnation,
			pid         = excluded.pid,
			hostname    = excluded.hostname,
			acquired_at = excluded.acquired_at,
			expires_at  = excluded.expires_at`,
		incarnation, os.Getpid(), hostname,
		timestamp(now), timestamp(now.Add(leaseTTL))); err != nil {
		return fmt.Errorf("store: lease acquire: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("store: lease acquire: %w", err)
	}
	s.incarnation = incarnation
	return nil
}

// RenewLease extends this handle's lease by the standard TTL. It fails with
// ErrLeaseLost when another writer has taken over.
func (s *Store) RenewLease(ctx context.Context) error {
	if s.incarnation == "" {
		return ErrNotAuthority
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE writer_lease SET expires_at = ? WHERE id = 1 AND incarnation = ?`,
		timestamp(time.Now().Add(leaseTTL)), s.incarnation)
	if err != nil {
		return fmt.Errorf("store: lease renew: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("store: lease renew: %w", err)
	}
	if n == 0 {
		return ErrLeaseLost
	}
	return nil
}

// processAlive reports whether pid names a live process on this host.
// Signal 0 probes without delivering; EPERM still proves liveness.
func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// newIncarnation mints the per-acquisition writer incarnation ID.
func newIncarnation() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("store: minting incarnation: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
