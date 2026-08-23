package store

import (
	"context"
	"path/filepath"
	"testing"
)

// fixtureRegister stands in for the (currently empty) real register so the
// migrate machinery is exercised end-to-end: apply, idempotent reopen, and
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

func TestOpenFreshWithEmptyRegister(t *testing.T) {
	// The real register is empty until duo's first schema lands; a fresh
	// open must still succeed at v0.
	path := filepath.Join(t.TempDir(), "duo.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if s.Version() != 0 {
		t.Fatalf("empty-register open reached v%d, want v0", s.Version())
	}
	_ = s.Close()
}
