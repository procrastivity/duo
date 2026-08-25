package domain_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/store"
)

const (
	dogfoodExportSQL = "testdata/duo-db-export.sql"
	dogfoodExportMD  = "testdata/duo-db-export.md"
	// Original airgapped export: /home/dev/Downloads/vnext-dogfood/records/
)

// restoreDogfoodExport applies the airgapped iterdump in testdata to a fresh
// database under t.TempDir(). The fixture SQL and the live user store are
// never written.
func restoreDogfoodExport(t *testing.T) string {
	t.Helper()

	sqlPath := dogfoodExportSQL
	if _, err := os.Stat(sqlPath); err != nil {
		t.Fatalf("reading %s (see %s and /home/dev/Downloads/vnext-dogfood/records/): %v", sqlPath, dogfoodExportMD, err)
	}

	dbPath := filepath.Join(t.TempDir(), "duo.db")
	cmd := exec.Command("python3", "-c",
		"import sqlite3,sys; c=sqlite3.connect(sys.argv[1]); c.executescript(open(sys.argv[2]).read()); c.commit()",
		dbPath, sqlPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("restoring dogfood export to %s: %v\n%s", dbPath, err, out)
	}
	return dbPath
}

// openDogfoodAuthority opens the restored export read-only (no writer lease)
// and rebuilds the domain kernel from its durable fact log.
func openDogfoodAuthority(t *testing.T, dbPath string) (*store.Store, *domain.Authority) {
	t.Helper()

	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("store.Open(%s): %v", dbPath, err)
	}
	a, err := domain.Open(context.Background(), storerepo.New(s))
	if err != nil {
		_ = s.Close()
		t.Fatalf("domain.Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, a
}

func countStreamFactsByKind(t *testing.T, s *store.Store, kind string) int {
	t.Helper()

	items, err := s.ReadStream(context.Background(), "duo.domain.fact/v1", 0, 1000)
	if err != nil {
		t.Fatalf("ReadStream(duo.domain.fact/v1): %v", err)
	}
	n := 0
	for _, item := range items {
		var payload struct {
			Kind string `json:"kind"`
		}
		if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
			t.Fatalf("unmarshal stream payload seq %d: %v", item.Seq, err)
		}
		if payload.Kind == kind {
			n++
		}
	}
	return n
}

func attachmentCreatedBySession(t *testing.T, s *store.Store) map[string]int {
	t.Helper()

	items, err := s.ReadStream(context.Background(), "duo.domain.fact/v1", 0, 1000)
	if err != nil {
		t.Fatalf("ReadStream(duo.domain.fact/v1): %v", err)
	}
	counts := make(map[string]int)
	for _, item := range items {
		var payload struct {
			Kind      string `json:"kind"`
			SessionID string `json:"session_id"`
		}
		if err := json.Unmarshal([]byte(item.Payload), &payload); err != nil {
			t.Fatalf("unmarshal stream payload seq %d: %v", item.Seq, err)
		}
		if payload.Kind != "attachment.created" {
			continue
		}
		counts[payload.SessionID]++
	}
	return counts
}

// TestDogfoodExportBaseline locks the airgapped dogfood export as a regression
// fixture: 25 sessions, 24 starting + 1 live instance, no instance.state
// facts, and both single- and multi-leaf launch records.
func TestDogfoodExportBaseline(t *testing.T) {
	dbPath := restoreDogfoodExport(t)
	s, a := openDogfoodAuthority(t, dbPath)

	sessions := a.Sessions()
	if got := len(sessions); got != 25 {
		t.Fatalf("sessions = %d, want 25", got)
	}

	var starting, live int
	for _, ses := range sessions {
		if ses.Current == "" {
			t.Fatalf("session %s has no current instance", ses.ID)
		}
		inst, ok := a.Instance(ses.Current)
		if !ok {
			t.Fatalf("session %s current instance %s not found", ses.ID, ses.Current)
		}
		switch inst.State {
		case domain.InstanceStarting:
			starting++
		case domain.InstanceLive:
			live++
		default:
			t.Fatalf("session %s instance %s state = %q, want %q or %q",
				ses.ID, inst.ID, inst.State, domain.InstanceStarting, domain.InstanceLive)
		}
	}
	if starting != 24 {
		t.Fatalf("instances starting = %d, want 24", starting)
	}
	if live != 1 {
		t.Fatalf("instances live = %d, want 1", live)
	}

	if got := countStreamFactsByKind(t, s, "instance.state"); got != 0 {
		t.Fatalf("instance.state facts in stream = %d, want 0", got)
	}

	bySession := attachmentCreatedBySession(t, s)
	var singleLeaf, multiLeaf int
	for sessionID, n := range bySession {
		switch {
		case n == 1:
			singleLeaf++
		case n >= 2:
			multiLeaf++
		default:
			t.Fatalf("session %s has %d attachment.created facts, want >= 1", sessionID, n)
		}
	}
	if singleLeaf < 1 {
		t.Fatalf("sessions with exactly 1 attachment.created = %d, want >= 1", singleLeaf)
	}
	if multiLeaf < 1 {
		t.Fatalf("sessions with 2+ attachment.created = %d, want >= 1 (e.g. ses_156959299a047c7a3d8693f7a1ddf3fe)", multiLeaf)
	}
}
