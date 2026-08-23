package doctor

import (
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/store"
)

func TestRun_MissingStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	report := Run(path)

	if report.Store.Present {
		t.Error("Present = true for a store file that was never created")
	}
	if !report.Store.Healthy {
		t.Error("Healthy = false for a merely-missing store; a missing store is not an error")
	}
	if report.Store.Error != "" {
		t.Errorf("Error = %q, want empty", report.Store.Error)
	}
	if report.Store.Writer != nil {
		t.Errorf("Writer = %+v, want nil (no probe on a missing store)", report.Store.Writer)
	}
	if report.Adapters.Registered == nil || len(report.Adapters.Registered) != 0 {
		t.Errorf("Adapters.Registered = %+v, want a non-nil empty slice", report.Adapters.Registered)
	}
}

func TestRun_PresentNoWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	// Create the store, then release it fully (no lease held) before doctor
	// probes it — the common case: duo has run before, nothing is writing
	// right now.
	s, err := store.Open(path)
	if err != nil {
		t.Fatalf("store.Open (setup): %v", err)
	}
	wantVersion := s.Version()
	if err := s.Close(); err != nil {
		t.Fatalf("Close (setup): %v", err)
	}

	report := Run(path)

	if !report.Store.Present {
		t.Error("Present = false for a store file that exists")
	}
	if !report.Store.Healthy {
		t.Errorf("Healthy = false, Error = %q", report.Store.Error)
	}
	if report.Store.SchemaVersion != wantVersion {
		t.Errorf("SchemaVersion = %d, want %d", report.Store.SchemaVersion, wantVersion)
	}
	if report.Store.Writer == nil {
		t.Fatal("Writer is nil, want a probe result")
	}
	if report.Store.Writer.Active {
		t.Errorf("Writer.Active = true, want false: %+v", report.Store.Writer)
	}

	// The probe must not have left a lingering lease: a second probe must
	// see the same "no writer" result, not "active" because a lease row
	// leaked.
	again := Run(path)
	if again.Store.Writer == nil || again.Store.Writer.Active {
		t.Errorf("second probe saw a lingering lease: %+v", again.Store.Writer)
	}
}

func TestRun_ActiveWriter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "duo.db")

	authority, err := store.OpenAuthority(path)
	if err != nil {
		t.Fatalf("store.OpenAuthority (setup): %v", err)
	}
	t.Cleanup(func() { _ = authority.Close() })

	report := Run(path)

	if !report.Store.Present {
		t.Error("Present = false for a store file that exists")
	}
	if !report.Store.Healthy {
		t.Errorf("Healthy = false, Error = %q", report.Store.Error)
	}
	if report.Store.Writer == nil {
		t.Fatal("Writer is nil, want a probe result")
	}
	if !report.Store.Writer.Active {
		t.Fatalf("Writer.Active = false, want true: %+v", report.Store.Writer)
	}
	if report.Store.Writer.Incarnation != authority.Incarnation() {
		t.Errorf("Writer.Incarnation = %q, want %q", report.Store.Writer.Incarnation, authority.Incarnation())
	}
	if report.Store.Writer.ExpiresAt == "" {
		t.Error("Writer.ExpiresAt is empty for an active lease")
	}
}

func TestDefaultStorePath_HonorsXDGDataHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	path, err := DefaultStorePath()
	if err != nil {
		t.Fatalf("DefaultStorePath: %v", err)
	}
	want := filepath.Join(dir, "duo", "duo.db")
	if path != want {
		t.Errorf("DefaultStorePath() = %q, want %q", path, want)
	}
}
