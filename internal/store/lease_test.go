package store

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"testing"
)

// expireLease backdates the lease row so takeover rules see it as lapsed.
func expireLease(t *testing.T, path string) {
	t.Helper()
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reader): %v", err)
	}
	defer func() { _ = r.Close() }()
	if _, err := r.db.ExecContext(context.Background(),
		`UPDATE writer_lease SET expires_at = '2000-01-01T00:00:00.000Z' WHERE id = 1`); err != nil {
		t.Fatalf("expiring lease: %v", err)
	}
}

func TestSecondWriterRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	s1, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(first): %v", err)
	}
	defer func() { _ = s1.Close() }()

	s2, err := OpenAuthority(path)
	if err == nil {
		_ = s2.Close()
		t.Fatal("second OpenAuthority succeeded, want WriterActiveError")
	}
	var active *WriterActiveError
	if !errors.As(err, &active) {
		t.Fatalf("second OpenAuthority error = %v, want *WriterActiveError", err)
	}
	if active.Incarnation != s1.Incarnation() {
		t.Fatalf("refusal names incarnation %s, want holder %s", active.Incarnation, s1.Incarnation())
	}
}

func TestExpiredLeaseTakeover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	s1, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(first): %v", err)
	}
	first := s1.Incarnation()
	// Crash simulation: the process dies without releasing the lease.
	_ = s1.db.Close()

	expireLease(t, path)

	s2, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(after expiry): %v", err)
	}
	defer func() { _ = s2.Close() }()
	if s2.Incarnation() == first || s2.Incarnation() == "" {
		t.Fatalf("takeover incarnation %q not freshly minted (was %q)", s2.Incarnation(), first)
	}
}

func TestDeadHolderTakeover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	s1, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(first): %v", err)
	}
	_ = s1.db.Close()

	// Point the unexpired lease at a same-host pid that is provably dead.
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Fatalf("starting throwaway process: %v", err)
	}
	_ = cmd.Wait()
	r, err := Open(path)
	if err != nil {
		t.Fatalf("Open(reader): %v", err)
	}
	if _, err := r.db.ExecContext(context.Background(),
		`UPDATE writer_lease SET pid = ? WHERE id = 1`, cmd.Process.Pid); err != nil {
		t.Fatalf("repointing lease pid: %v", err)
	}
	_ = r.Close()

	s2, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(dead holder): %v", err)
	}
	_ = s2.Close()
}

func TestUsurpedWriterCannotCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	s1, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(first): %v", err)
	}
	defer func() { _ = s1.Close() }()

	expireLease(t, path)
	s2, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(takeover): %v", err)
	}
	defer func() { _ = s2.Close() }()

	// The usurped handle is still open; the lease fence must stop it.
	err = s1.ObservationTx(context.Background(), func(tx *Tx) error {
		return tx.AppendAudit(Audit{Actor: "test"})
	})
	if !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("usurped ObservationTx error = %v, want ErrLeaseLost", err)
	}
	if err := s1.RenewLease(context.Background()); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("usurped RenewLease error = %v, want ErrLeaseLost", err)
	}
}

func TestReadHandleIsNotAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()

	err = s.EnrollmentTx(context.Background(), func(*Tx) error { return nil })
	if !errors.Is(err, ErrNotAuthority) {
		t.Fatalf("EnrollmentTx on read handle = %v, want ErrNotAuthority", err)
	}
}
