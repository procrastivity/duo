package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func openAuthorityT(t *testing.T) *Store {
	t.Helper()
	return openAuthorityAtT(t, filepath.Join(t.TempDir(), "duo.db"))
}

// openAuthorityAtT is openAuthorityT at a caller-chosen path, for tests that
// reopen the same database after simulating a crash.
func openAuthorityAtT(t *testing.T, path string) *Store {
	t.Helper()
	s, err := OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority(%s): %v", path, err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// boundaries enumerates every §4.2 named transaction helper, so the crash
// tests cannot silently skip one.
func boundaries(s *Store) map[string]func(context.Context, func(*Tx) error) error {
	return map[string]func(context.Context, func(*Tx) error) error{
		"EnrollmentTx":        s.EnrollmentTx,
		"CommandAcceptTx":     s.CommandAcceptTx,
		"CommandTransitionTx": s.CommandTransitionTx,
		"ObservationTx":       s.ObservationTx,
		"CollaborationTx":     s.CollaborationTx,
		"NotificationTx":      s.NotificationTx,
		"ProjectionInstallTx": s.ProjectionInstallTx,
		"LaunchResolutionTx":  s.LaunchResolutionTx,
	}
}

func rowCount(t *testing.T, s *Store, table string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRowContext(context.Background(),
		"SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
		t.Fatalf("counting %s: %v", table, err)
	}
	return n
}

// writeAll touches every substrate table one boundary can write, so a crash
// leaving any partial row behind is visible.
func writeAll(t *testing.T, tx *Tx, tag string) {
	t.Helper()
	if err := tx.AppendAudit(Audit{Actor: "test", Target: tag}); err != nil {
		t.Fatalf("AppendAudit: %v", err)
	}
	if _, err := tx.EnqueueWork("test.kind", tag); err != nil {
		t.Fatalf("EnqueueWork: %v", err)
	}
	if _, _, err := tx.AppendStreamItem("test-stream", tag, tag); err != nil {
		t.Fatalf("AppendStreamItem: %v", err)
	}
}

func assertCounts(t *testing.T, s *Store, want int, when string) {
	t.Helper()
	for _, table := range []string{"audit_log", "work_queue", "stream_log"} {
		if got := rowCount(t, s, table); got != want {
			t.Fatalf("%s: %s has %d rows, want %d", when, table, got, want)
		}
	}
}

func TestBoundaryCrashInjection(t *testing.T) {
	ctx := context.Background()
	injected := errors.New("injected crash")

	for name := range boundaries(nil) {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "duo.db")
			s := openAuthorityAtT(t, path)
			run := boundaries(s)[name]

			// Crash mid-body, after every write.
			err := run(ctx, func(tx *Tx) error {
				writeAll(t, tx, "mid-body")
				return injected
			})
			if !errors.Is(err, injected) {
				t.Fatalf("mid-body crash: err = %v, want injected", err)
			}
			assertCounts(t, s, 0, "after mid-body crash")

			// Crash after the body, before commit.
			s.beforeCommit = func(string) error { return injected }
			err = run(ctx, func(tx *Tx) error {
				writeAll(t, tx, "pre-commit")
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatalf("pre-commit crash: err = %v, want injected", err)
			}
			s.beforeCommit = nil
			assertCounts(t, s, 0, "after pre-commit crash")

			// Panic mid-body.
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("panic did not propagate")
					}
				}()
				_ = run(ctx, func(tx *Tx) error {
					writeAll(t, tx, "panic")
					panic("injected panic")
				})
			}()
			assertCounts(t, s, 0, "after panic")

			// The clean run commits everything together.
			if err := run(ctx, func(tx *Tx) error {
				writeAll(t, tx, "clean")
				return nil
			}); err != nil {
				t.Fatalf("clean run: %v", err)
			}
			assertCounts(t, s, 1, "after clean run")

			// The other half of the boundary: what committed must survive
			// the writer's death. Drop the handle without Close, so the
			// lease is never released, exactly as a killed process leaves
			// it, then recover the database from a new incarnation.
			_ = s.db.Close()
			expireLease(t, path)
			recovered := openAuthorityAtT(t, path)
			assertCounts(t, recovered, 1, "after crash following commit")
		})
	}
}

func TestAuditStamping(t *testing.T) {
	s := openAuthorityT(t)
	ctx := context.Background()

	if err := s.EnrollmentTx(ctx, func(tx *Tx) error {
		return tx.AppendAudit(Audit{Actor: "user:beau", Target: "session:x", Reason: "why"})
	}); err != nil {
		t.Fatalf("EnrollmentTx: %v", err)
	}

	var incarnation, operation, actor string
	if err := s.db.QueryRowContext(ctx,
		`SELECT incarnation, operation, actor FROM audit_log`).Scan(&incarnation, &operation, &actor); err != nil {
		t.Fatalf("reading audit row: %v", err)
	}
	if incarnation != s.Incarnation() {
		t.Fatalf("audit incarnation = %q, want %q", incarnation, s.Incarnation())
	}
	if operation != "enrollment" {
		t.Fatalf("audit operation = %q, want enrollment", operation)
	}
	if actor != "user:beau" {
		t.Fatalf("audit actor = %q", actor)
	}
}

// TestBoundaryListMatchesArchitecture pins the helper count to the eight
// §4.2 boundaries; a ninth helper or a dropped one must fail loudly here.
func TestBoundaryListMatchesArchitecture(t *testing.T) {
	if n := len(boundaries(nil)); n != 8 {
		t.Fatalf("store exposes %d boundary helpers, architecture §4.2 names 8", n)
	}
}
