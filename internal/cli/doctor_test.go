package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
)

// TestDoctorCommand_JSON runs `duo doctor --json` through the same Execute
// path main.go uses, against a store path isolated to a temp directory via
// XDG_DATA_HOME so the test never touches a real installation.
func TestDoctorCommand_JSON(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // Step 15: no duo.config.yaml written under it
	clearAmbientHerdrEnv(t)                  // Step 15: this repo dogfoods a live Herdr session

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"doctor", "--json"})

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
				Name   string `json:"name"`
				Kind   string `json:"kind"`
				Status string `json:"status"`
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
	if len(report.Adapters.Registered) != 2 {
		t.Fatalf("Adapters.Registered has %d rows, want the fake pair", len(report.Adapters.Registered))
	}
	want := map[string]string{"fake-host": "session_host", "fake-runtime": "agent_runtime"}
	for _, a := range report.Adapters.Registered {
		if want[a.Name] != a.Kind {
			t.Errorf("adapter %q has kind %q, want %q", a.Name, a.Kind, want[a.Name])
		}
		if a.Status != "supported" {
			t.Errorf("adapter %q has status %q, want \"supported\"", a.Name, a.Status)
		}
	}
}

// TestDoctorCommand_Human runs `duo doctor` (no --json) and checks the
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
