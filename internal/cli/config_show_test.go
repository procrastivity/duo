package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
)

// configShowFixture is a valid duo.config/v3 document with unsorted preset
// and agent_runtime keys so roster ordering is proven by the tests.
const configShowFixture = `
schema: duo.config/v3
agent_runtimes:
  pi:
    kind: pi
    executable: pi
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
    model_line: sonnet-5
    model_family: claude
  pi_gpt56:
    agent_runtime: pi
    model_line: gpt-5.6
    model_family: gpt
presets:
  zebra:
    leaves:
      main:
        candidates:
          - variant: pi_gpt56
  alpha:
    leaves:
      reviewer:
        candidates:
          - variant: pi_gpt56
      builder:
        candidates:
          - variant: claude_sonnet
`

// configShowEmptyPresetsFixture is a valid duo.config/v3 with runtimes and
// variants but no presets key.
const configShowEmptyPresetsFixture = `
schema: duo.config/v3
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
    model_line: sonnet-5
    model_family: claude
`

// writeConfigShowFixture writes configShowFixture to a temp duo.config.yaml
// and returns its path.
func writeConfigShowFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "duo.config.yaml")
	if err := os.WriteFile(path, []byte(configShowFixture), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

// writeConfigShowEmptyPresetsFixture writes configShowEmptyPresetsFixture to
// a temp duo.config.yaml and returns its path.
func writeConfigShowEmptyPresetsFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "duo.config.yaml")
	if err := os.WriteFile(path, []byte(configShowEmptyPresetsFixture), 0o600); err != nil {
		t.Fatalf("writing empty-presets test config: %v", err)
	}
	return path
}

// configShowDescriptor returns the registered operation whose CLI path is
// {"config", "show"}.
func configShowDescriptor(t *testing.T) registry.Descriptor {
	t.Helper()
	for _, d := range registry.All() {
		if len(d.CLI) == 2 && d.CLI[0] == "config" && d.CLI[1] == "show" {
			return d
		}
	}
	t.Fatal("no registered operation has CLI path [config show]")
	return registry.Descriptor{}
}

// --- CLI-path-to-registry pinning ---------------------------------------

func TestConfigShowCLIPathMatchRegistry(t *testing.T) {
	d := configShowDescriptor(t)
	root := NewRootCommand(iostreams.System(), buildinfo.Info{})
	cmd, _, err := root.Find(d.CLI)
	if err != nil {
		t.Fatalf("%s: root.Find(%v): %v", d.Name, d.CLI, err)
	}
	if cmd.Name() != "show" {
		t.Errorf("%s: resolved command %q, want %q", d.Name, cmd.Name(), "show")
	}
}

func TestConfigShowNoMCPTool(t *testing.T) {
	d := configShowDescriptor(t)
	if d.Projectability != registry.LocalAdmin {
		t.Errorf("%s projectability = %q, want local_admin", d.Name, d.Projectability)
	}
	if d.MCPTool != "" {
		t.Errorf("%s has MCP tool %q, want none", d.Name, d.MCPTool)
	}
	if d.Route != nil {
		t.Errorf("%s has route %+v, want none", d.Name, d.Route)
	}
}

// --- config show roster ---------------------------------------------------

func TestConfigShow_JSONRoster(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	configPath := writeConfigShowFixture(t)
	d := configShowDescriptor(t)

	code, out, errOut := runSession(t, "config", "show", "--config", configPath, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Operation string `json:"operation"`
		Result    struct {
			Schema         string              `json:"schema"`
			Path           string              `json:"path"`
			Presets        []configShowPreset  `json:"presets"`
			AgentRuntimes  []configShowRuntime `json:"agent_runtimes"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if env.Operation != d.Name {
		t.Errorf("operation = %q, want %q", env.Operation, d.Name)
	}
	if env.Result.Schema != "duo.config/v3" {
		t.Errorf("result.schema = %q, want duo.config/v3", env.Result.Schema)
	}
	if env.Result.Path != configPath {
		t.Errorf("result.path = %q, want %q", env.Result.Path, configPath)
	}
	wantPresets := []configShowPreset{
		{Name: "alpha", Leaves: []string{"builder", "reviewer"}},
		{Name: "zebra", Leaves: []string{"main"}},
	}
	if len(env.Result.Presets) != len(wantPresets) {
		t.Fatalf("presets = %+v, want %+v", env.Result.Presets, wantPresets)
	}
	for i, want := range wantPresets {
		got := env.Result.Presets[i]
		if got.Name != want.Name {
			t.Errorf("presets[%d].name = %q, want %q", i, got.Name, want.Name)
		}
		if len(got.Leaves) != len(want.Leaves) {
			t.Errorf("presets[%d].leaves = %v, want %v", i, got.Leaves, want.Leaves)
			continue
		}
		for j, leaf := range want.Leaves {
			if got.Leaves[j] != leaf {
				t.Errorf("presets[%d].leaves[%d] = %q, want %q", i, j, got.Leaves[j], leaf)
			}
		}
	}
	wantRuntimes := []configShowRuntime{
		{Name: "claude", Kind: "claude"},
		{Name: "pi", Kind: "pi"},
	}
	if len(env.Result.AgentRuntimes) != len(wantRuntimes) {
		t.Fatalf("agent_runtimes = %+v, want %+v", env.Result.AgentRuntimes, wantRuntimes)
	}
	for i, want := range wantRuntimes {
		got := env.Result.AgentRuntimes[i]
		if got.Name != want.Name || got.Kind != want.Kind {
			t.Errorf("agent_runtimes[%d] = %+v, want %+v", i, got, want)
		}
	}
}

func TestConfigShow_TextRoster(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	configPath := writeConfigShowFixture(t)

	code, out, errOut := runSession(t, "config", "show", "--config", configPath)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	if out != "alpha\nzebra\n" {
		t.Errorf("output = %q, want %q", out, "alpha\nzebra\n")
	}
}

func TestConfigShow_EmptyPresets(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	configPath := writeConfigShowEmptyPresetsFixture(t)

	code, out, errOut := runSession(t, "config", "show", "--config", configPath, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var raw struct {
		Result struct {
			Presets json.RawMessage `json:"presets"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &raw); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if string(raw.Result.Presets) != "[]" {
		t.Errorf("result.presets = %s, want []", raw.Result.Presets)
	}

	code, out, errOut = runSession(t, "config", "show", "--config", configPath)
	if code != exitcode.Success {
		t.Fatalf("text: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	if out != "no presets\n" {
		t.Errorf("text output = %q, want %q", out, "no presets\n")
	}
}

func TestConfigShow_MissingFile(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	missing := filepath.Join(t.TempDir(), "no-such-duo.config.yaml")

	code, _, errOut := runSession(t, "config", "show", "--config", missing, "--output", "json")
	if code == exitcode.Success {
		t.Fatalf("exit code = %d, want non-zero for a missing config file", code)
	}
	if !strings.Contains(errOut, missing) {
		t.Errorf("stderr = %q, want it to name the missing path %q", errOut, missing)
	}
}

func TestConfigShow_DefaultPath(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	duoDir := filepath.Join(configHome, "duo")
	if err := os.MkdirAll(duoDir, 0o700); err != nil {
		t.Fatalf("creating default config dir: %v", err)
	}
	defaultPath := filepath.Join(duoDir, "duo.config.yaml")
	if err := os.WriteFile(defaultPath, []byte(configShowFixture), 0o600); err != nil {
		t.Fatalf("writing default config: %v", err)
	}

	code, out, errOut := runSession(t, "config", "show", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Result struct {
			Path    string             `json:"path"`
			Presets []configShowPreset `json:"presets"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if !strings.HasSuffix(env.Result.Path, "duo.config.yaml") {
		t.Errorf("result.path = %q, want suffix duo.config.yaml", env.Result.Path)
	}
	if len(env.Result.Presets) != 2 || env.Result.Presets[0].Name != "alpha" || env.Result.Presets[1].Name != "zebra" {
		t.Errorf("presets = %+v, want alpha then zebra", env.Result.Presets)
	}
}
