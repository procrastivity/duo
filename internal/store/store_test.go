package store

import (
	"context"
	"path/filepath"
	"testing"
)

// fixtureRegister is a throwaway two-step register — the real one is a
// single substrate migration, so it cannot exercise a multi-step upgrade.
// It drives the migrate machinery end-to-end: apply, idempotent reopen, and
// the forward-only downgrade refusal.
var fixtureRegister = []migration{
	{version: 1, name: "baseline", stmts: []string{
		`CREATE TABLE fixture (id INTEGER PRIMARY KEY, note TEXT NOT NULL) STRICT`,
	}},
	{version: 2, name: "second", stmts: []string{
		`ALTER TABLE fixture ADD COLUMN extra TEXT`,
	}},
}

func TestOpenMigratesAndReopens(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	s, err := openAt(path, fixtureRegister, 2)
	if err != nil {
		t.Fatalf("openAt(fresh): %v", err)
	}
	if s.Version() != 2 {
		t.Fatalf("fresh open reached v%d, want v2", s.Version())
	}
	if _, err := s.db.ExecContext(context.Background(),
		`INSERT INTO fixture (note, extra) VALUES ('hello', 'there')`); err != nil {
		t.Fatalf("writing through migrated schema: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopen at the same version: no pending migrations, same version.
	s, err = openAt(path, fixtureRegister, 2)
	if err != nil {
		t.Fatalf("openAt(reopen): %v", err)
	}
	if s.Version() != 2 {
		t.Fatalf("reopen reached v%d, want v2", s.Version())
	}
	_ = s.Close()
}

func TestOpenRefusesDowngrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	s, err := openAt(path, fixtureRegister, 2)
	if err != nil {
		t.Fatalf("openAt(fresh): %v", err)
	}
	_ = s.Close()

	// A binary that only understands v1 must refuse a v2 database.
	if _, err := openAt(path, fixtureRegister[:1], 1); err == nil {
		t.Fatal("openAt(older binary) succeeded, want forward-only refusal")
	}
}

func TestOpenFreshRealRegister(t *testing.T) {
	// A fresh open applies the real register up to its latest version.
	path := filepath.Join(t.TempDir(), "duo.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if want := latestVersion(register); s.Version() != want {
		t.Fatalf("fresh open reached v%d, want v%d", s.Version(), want)
	}
	_ = s.Close()
}

// TestStartupPragmas pins §4.1's startup requirements. They are set through
// DSN parameters, which fail silently when mistyped or when the driver stops
// honouring them, so assert the effective values rather than the string.
func TestStartupPragmas(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "duo.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = s.Close() }()
	ctx := context.Background()

	for _, want := range []struct{ pragma, value string }{
		{"journal_mode", "wal"},
		{"foreign_keys", "1"},
		{"busy_timeout", "5000"},
	} {
		var got string
		if err := s.db.QueryRowContext(ctx, "PRAGMA "+want.pragma).Scan(&got); err != nil {
			t.Fatalf("PRAGMA %s: %v", want.pragma, err)
		}
		if got != want.value {
			t.Errorf("PRAGMA %s = %q, want %q", want.pragma, got, want.value)
		}
	}

	// foreign_keys=1 has to actually bite: the substrate's only foreign key
	// is work_attempt.work_id, and §4.3 reconciliation reads attempt rows
	// expecting their work item to exist.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO work_attempt (work_id, attempt, incarnation, started_at)
		 VALUES (9999, 1, 'x', '2026-01-01T00:00:00.000Z')`); err == nil {
		t.Fatal("attempt row referencing a missing work item was accepted, want FK refusal")
	}
}
