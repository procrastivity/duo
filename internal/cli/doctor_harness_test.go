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

func doctorHarnessInspectionJSON(t *testing.T, extraArgs ...string) (orphaned int, out string) {
	t.Helper()
	args := append([]string{"doctor", "--output", "json"}, extraArgs...)
	code, stdout, errOut := runSession(t, args...)
	if code != exitcode.Success {
		t.Fatalf("doctor json: exit %d (stderr: %s)", code, errOut)
	}
	var report struct {
		HarnessSweep struct {
			Reaped   int      `json:"reaped"`
			Kept     int      `json:"kept"`
			Orphaned int      `json:"orphaned"`
			ReadOnly bool     `json:"read_only"`
			IDs      []string `json:"ids"`
		} `json:"harness_sweep"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decoding doctor JSON: %v\n%s", err, stdout)
	}
	if !report.HarnessSweep.ReadOnly || report.HarnessSweep.Reaped != 0 {
		t.Fatalf("doctor mutated harness state: %+v", report.HarnessSweep)
	}
	return report.HarnessSweep.Orphaned, stdout
}

// TestDoctorReportsRefusedLaunchHarnessDirReadOnly proves diagnosis reports
// an orphan but leaves cleanup to an explicit write operation.
func TestDoctorReportsRefusedLaunchHarnessDirReadOnly(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clearAmbientHerdrEnv(t)

	dir := plantHarnessDir(t, "lrr_refused_orphan", "primary")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("planted harness dir missing before doctor: %v", err)
	}

	orphaned, stdout := doctorHarnessInspectionJSON(t)
	if orphaned != 1 {
		t.Errorf("harness_sweep.orphaned = %d, want 1\n%s", orphaned, stdout)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("doctor removed refused-launch harness dir: %v", err)
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

	orphaned, _ := doctorHarnessInspectionJSON(t, "--workspace", root)
	if orphaned != 0 {
		t.Errorf("harness_sweep.orphaned = %d, want 0 for a live session", orphaned)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("live session harness dir was reaped: %v", err)
	}
}

// TestDoctorReportsTerminalSessionHarnessDirReadOnly: a terminal runtime is
// reported as orphaned, but doctor does not perform cleanup.
func TestDoctorReportsTerminalSessionHarnessDirReadOnly(t *testing.T) {
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

	orphaned, stdout := doctorHarnessInspectionJSON(t, "--workspace", root)
	if orphaned != 1 {
		t.Errorf("harness_sweep.orphaned = %d, want 1 for a terminal session\n%s", orphaned, stdout)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("doctor removed terminal-session harness dir: %v", err)
	}
	parent := filepath.Dir(dir)
	if _, err := os.Stat(parent); err != nil {
		t.Errorf("doctor removed terminal-session lrr directory: %v", err)
	}
}
