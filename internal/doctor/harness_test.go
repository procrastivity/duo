package doctor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultHarnessRootHonorsXDGDataHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	got, err := DefaultHarnessRoot()
	if err != nil {
		t.Fatalf("DefaultHarnessRoot: %v", err)
	}
	want := filepath.Join(dir, "duo", "harness")
	if got != want {
		t.Errorf("DefaultHarnessRoot() = %q, want %q", got, want)
	}
}

func TestSweepHarnessDirsMissingRootIsNoop(t *testing.T) {
	root := filepath.Join(t.TempDir(), "no-such-harness")
	sweep, err := SweepHarnessDirs(root, func(string) bool { return true })
	if err != nil {
		t.Fatalf("SweepHarnessDirs: %v", err)
	}
	if sweep.Reaped != 0 || sweep.Kept != 0 {
		t.Errorf("sweep = %+v, want a no-op", sweep)
	}
}

func TestSweepHarnessDirsReapsUnkeptAndLeavesKept(t *testing.T) {
	root := t.TempDir()
	mustHarnessDir(t, root, "lrr_live", "primary")
	mustHarnessDir(t, root, "lrr_dead", "primary")
	mustHarnessDir(t, root, "lrr_orphan", "main")
	if err := os.WriteFile(filepath.Join(root, "not-a-dir"), []byte("x"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	sweep, err := SweepHarnessDirs(root, func(id string) bool { return id == "lrr_live" })
	if err != nil {
		t.Fatalf("SweepHarnessDirs: %v", err)
	}
	if sweep.Kept != 1 {
		t.Errorf("Kept = %d, want 1", sweep.Kept)
	}
	if sweep.Reaped != 2 {
		t.Errorf("Reaped = %d, want 2", sweep.Reaped)
	}
	if !containsID(sweep.IDs, "lrr_dead") || !containsID(sweep.IDs, "lrr_orphan") {
		t.Errorf("IDs = %v, want lrr_dead and lrr_orphan", sweep.IDs)
	}

	if _, err := os.Stat(filepath.Join(root, "lrr_live", "primary")); err != nil {
		t.Errorf("live harness dir was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "lrr_dead")); !os.IsNotExist(err) {
		t.Errorf("dead harness dir still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "lrr_orphan")); !os.IsNotExist(err) {
		t.Errorf("orphan harness dir still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "not-a-dir")); err != nil {
		t.Errorf("non-directory at harness root was removed: %v", err)
	}
}

func TestSweepHarnessDirsNilKeepReapsAll(t *testing.T) {
	root := t.TempDir()
	mustHarnessDir(t, root, "lrr_only", "primary")

	sweep, err := SweepHarnessDirs(root, nil)
	if err != nil {
		t.Fatalf("SweepHarnessDirs: %v", err)
	}
	if sweep.Reaped != 1 || sweep.Kept != 0 {
		t.Errorf("sweep = %+v, want reaped 1", sweep)
	}
	if _, err := os.Stat(filepath.Join(root, "lrr_only")); !os.IsNotExist(err) {
		t.Errorf("lrr_only still present after nil-keep sweep: %v", err)
	}
}

func mustHarnessDir(t *testing.T, root, lrr, leaf string) {
	t.Helper()
	dir := filepath.Join(root, lrr, leaf)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), []byte("keep-or-reap"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}
