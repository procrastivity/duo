package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
	runtimedevin "github.com/procrastivity/duo/internal/runtime/devin"
)

// TestDoctorCommand_JSON runs `duo doctor --output json` through the same Execute
// path main.go uses, against a store path isolated to a temp directory via
// XDG_DATA_HOME so the test never touches a real installation.
func TestDoctorCommand_JSON(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // Step 15: no duo.config.yaml written under it
	clearAmbientHerdrEnv(t)                  // Step 15: this repo dogfoods a live Herdr session

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"doctor", "--output", "json"})

	code := Execute(root, streams)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut.String())
	}

	var report struct {
		Store struct {
			Present bool `json:"present"`
			Healthy bool `json:"healthy"`
		} `json:"store"`
		Adapters struct {
			Registered []struct {
				Name                      string   `json:"name"`
				Kind                      string   `json:"kind"`
				Status                    string   `json:"status"`
				SupportedExternalVersions []string `json:"supportedExternalVersions"`
				DetectedExternalVersion   string   `json:"detectedExternalVersion"`
				PinnedExternalVersion     string   `json:"pinnedExternalVersion"`
			} `json:"registered"`
		} `json:"adapters"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	if report.Store.Present {
		t.Error("Store.Present = true for a freshly isolated XDG_DATA_HOME")
	}
	if !report.Store.Healthy {
		t.Error("Store.Healthy = false for a merely-missing store")
	}
	if len(report.Adapters.Registered) != 3 {
		t.Fatalf("Adapters.Registered has %d rows, want the fake pair plus Devin", len(report.Adapters.Registered))
	}
	want := map[string]string{"fake-host": "session_host", "fake-runtime": "agent_runtime", "devin": "agent_runtime"}
	for _, a := range report.Adapters.Registered {
		if want[a.Name] != a.Kind {
			t.Errorf("adapter %q has kind %q, want %q", a.Name, a.Kind, want[a.Name])
		}
		if a.Name != "devin" && a.Status != "supported" {
			t.Errorf("adapter %q has status %q, want \"supported\"", a.Name, a.Status)
		}
		if a.Name == "devin" {
			if a.Status != "unverified" && a.Status != "unavailable" {
				t.Errorf("Devin status = %q, want unverified or unavailable", a.Status)
			}
			if a.DetectedExternalVersion != "" {
				t.Errorf("Devin detected version = %q, want empty because Probe does not execute --version", a.DetectedExternalVersion)
			}
			if a.PinnedExternalVersion != "3000.6.7" {
				t.Errorf("Devin pinned version = %q, want 3000.6.7", a.PinnedExternalVersion)
			}
			if len(a.SupportedExternalVersions) != 2 || a.SupportedExternalVersions[0] != "3000.6.2" || a.SupportedExternalVersions[1] != "3000.6.7" {
				t.Errorf("Devin supported versions = %v, want [3000.6.2 3000.6.7]", a.SupportedExternalVersions)
			}
		}
	}
}

// TestDoctorCommand_Human runs `duo doctor` (default --output text) and checks the
// human-mode report lands on stdout with no error.
func TestDoctorCommand_Human(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // Step 15: no duo.config.yaml written under it
	clearAmbientHerdrEnv(t)                  // Step 15: this repo dogfoods a live Herdr session

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"doctor"})

	code := Execute(root, streams)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut.String())
	}
	if out.Len() == 0 {
		t.Error("human-mode output is empty")
	}
	for _, want := range []string{
		"devin (agent_runtime):",
		"external version: detected=not probed",
		"pinned=3000.6.7",
		"supported=3000.6.2, 3000.6.7",
	} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("human-mode output missing %q:\n%s", want, out.String())
		}
	}
}

// TestDoctorCommand_ReportsDevinProjection pins the devin projection section
// in both render modes: the JSON report carries status and the posture file
// path, and the human report names both files.
func TestDoctorCommand_ReportsDevinProjection(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clearAmbientHerdrEnv(t)

	workspace := t.TempDir()
	if err := runtimedevin.MaterializeProjection(workspace, "lrr_doctor"); err != nil {
		t.Fatalf("MaterializeProjection: %v", err)
	}

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"doctor", "--workspace", workspace, "--output", "json"})

	if code := Execute(root, streams); code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut.String())
	}
	var report struct {
		DevinProjection struct {
			Status      string `json:"status"`
			PosturePath string `json:"posture_path"`
		} `json:"devin_projection"`
	}
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	if report.DevinProjection.Status != "current" {
		t.Errorf("devin_projection.status = %q, want current", report.DevinProjection.Status)
	}
	wantPosture := filepath.Join(workspace, ".devin", "config.local.json")
	if report.DevinProjection.PosturePath != wantPosture {
		t.Errorf("devin_projection.posture_path = %q, want %q", report.DevinProjection.PosturePath, wantPosture)
	}

	out.Reset()
	root = NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"doctor", "--workspace", workspace})
	if code := Execute(root, streams); code != exitcode.Success {
		t.Fatalf("human-mode exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut.String())
	}
	for _, want := range []string{"devin projection: current", "config.local.json", "hooks.v1.json"} {
		if !bytes.Contains(out.Bytes(), []byte(want)) {
			t.Errorf("human-mode output missing %q:\n%s", want, out.String())
		}
	}
}

// TestDoctorCommand_CLIPathMatchesRegistry pins that `duo doctor` is
// registered at exactly the CLI path internal/registry's "doctor.run" row
// declares.
func TestDoctorCommand_CLIPathMatchesRegistry(t *testing.T) {
	d, ok := registry.Lookup("doctor.run")
	if !ok {
		t.Fatal(`"doctor.run" is not registered`)
	}

	root := NewRootCommand(iostreams.System(), buildinfo.Info{})
	cmd, _, err := root.Find(d.CLI)
	if err != nil {
		t.Fatalf("root.Find(%v): %v", d.CLI, err)
	}
	if cmd.Name() != "doctor" {
		t.Errorf("resolved command %q, want %q", cmd.Name(), "doctor")
	}
}
