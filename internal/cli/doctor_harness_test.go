package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	runtimeclaude "github.com/procrastivity/duo/internal/runtime/claude"
)

// plantHarnessDir materializes a close-on-exit harness directory the way a
// launch's augmenter would, keyed by launch-resolution id and leaf.
func plantHarnessDir(t *testing.T, lrr, leaf string) string {
	t.Helper()
	dir, err := runtimeclaude.DefaultHarnessDir(lrr, leaf)
	if err != nil {
		t.Fatalf("DefaultHarnessDir: %v", err)
	}
	if _, err := runtimeclaude.MaterializeCloseOnExit(dir); err != nil {
		t.Fatalf("MaterializeCloseOnExit: %v", err)
	}
	return dir
}

func doctorHarnessSweepJSON(t *testing.T, extraArgs ...string) (reaped int, out string) {
	t.Helper()
	args := append([]string{"doctor", "--output", "json"}, extraArgs...)
	code, stdout, errOut := runSession(t, args...)
	if code != exitcode.Success {
		t.Fatalf("doctor json: exit %d (stderr: %s)", code, errOut)
	}
	var report struct {
		HarnessSweep struct {
			Reaped int      `json:"reaped"`
			Kept   int      `json:"kept"`
			IDs    []string `json:"ids"`
		} `json:"harness_sweep"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decoding doctor JSON: %v\n%s", err, stdout)
	}
	return report.HarnessSweep.Reaped, stdout
}

// TestDoctorReapsRefusedLaunchHarnessDir is notes/51 9a: a harness directory
// whose launch-resolution id has no durable record (a refused launch, or
// any materialize that never committed) is gone after doctor.
func TestDoctorReapsRefusedLaunchHarnessDir(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clearAmbientHerdrEnv(t)

	dir := plantHarnessDir(t, "lrr_refused_orphan", "primary")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("planted harness dir missing before doctor: %v", err)
	}

	reaped, stdout := doctorHarnessSweepJSON(t)
	if reaped != 1 {
		t.Errorf("harness_sweep.reaped = %d, want 1\n%s", reaped, stdout)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("refused launch harness dir still present after doctor: %v", err)
	}
}

// TestDoctorKeepsLiveSessionHarnessDir: close-on-exit files stay for a
// session whose runtime instance is not terminal. Doctor's recovering view
// on Open must not count as a reap signal.
func TestDoctorKeepsLiveSessionHarnessDir(t *testing.T) {
	h := newBindHarness(t, nil)
	clearAmbientHerdrEnv(t)

	result, _ := launchWithAugmenter(t, h, false)
	lrr := result.Report.LaunchResolutionID
	if lrr == "" {
		t.Fatal("the launch was not recorded")
	}
	dir, err := runtimeclaude.DefaultHarnessDir(lrr, "primary")
	if err != nil {
		t.Fatalf("DefaultHarnessDir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("live launch did not materialize a harness dir: %v", err)
	}
	root := h.root
	h.close()

	reaped, _ := doctorHarnessSweepJSON(t, "--workspace", root)
	if reaped != 0 {
		t.Errorf("harness_sweep.reaped = %d, want 0 for a live session", reaped)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("live session harness dir was reaped: %v", err)
	}
}

// TestDoctorReapsTerminalSessionHarnessDir: once the runtime instance is
// exited, doctor removes the harness directory. Close-on-exit no longer
// needs the files.
func TestDoctorReapsTerminalSessionHarnessDir(t *testing.T) {
	h := newBindHarness(t, nil)
	clearAmbientHerdrEnv(t)

	result, _ := launchWithAugmenter(t, h, false)
	lrr := result.Report.LaunchResolutionID
	if lrr == "" {
		t.Fatal("the launch was not recorded")
	}
	if len(result.Record.InstanceIDs) == 0 {
		t.Fatal("the launch recorded no runtime instance")
	}
	dir, err := runtimeclaude.DefaultHarnessDir(lrr, "primary")
	if err != nil {
		t.Fatalf("DefaultHarnessDir: %v", err)
	}

	if err := h.authority.Exit(context.Background(), domain.InstanceID(result.Record.InstanceIDs[0]), "test", "terminal for harness reaping"); err != nil {
		t.Fatalf("Exit: %v", err)
	}
	root := h.root
	h.close()

	reaped, stdout := doctorHarnessSweepJSON(t, "--workspace", root)
	if reaped != 1 {
		t.Errorf("harness_sweep.reaped = %d, want 1 for a terminal session\n%s", reaped, stdout)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("terminal session harness dir still present after doctor: %v", err)
	}
	parent := filepath.Dir(dir)
	if _, err := os.Stat(parent); !os.IsNotExist(err) {
		t.Errorf("terminal session lrr directory still present after doctor: %v", err)
	}
}
