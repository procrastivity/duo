package cli

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
)

// providerV3Config is a minimal duo.config/v3 document naming one provider
// ("anthropic") on one launch_variant — enough for "a provider exists when a
// variant names it" (workplan Step 08) without pulling in the rest of the
// v3 fixture.
const providerV3Config = `
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
    provider: anthropic
`

// writeProviderConfig writes providerV3Config to a temp file and returns its
// path.
func writeProviderConfig(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/duo.config.yaml"
	if err := os.WriteFile(path, []byte(providerV3Config), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

// --- CLI-path-to-registry pinning ---------------------------------------

func providerDescriptor(t *testing.T, verb string) registry.Descriptor {
	t.Helper()
	for _, d := range registry.All() {
		if len(d.CLI) == 2 && d.CLI[0] == "provider" && d.CLI[1] == verb {
			return d
		}
	}
	t.Fatalf("no registered operation has CLI path [provider %s]", verb)
	return registry.Descriptor{}
}

// TestProviderCLIPathsMatchRegistry pins that every provider.* verb is
// registered at exactly the CLI path internal/registry's row declares, the
// same discipline TestSessionCLIPathsMatchRegistry uses.
func TestProviderCLIPathsMatchRegistry(t *testing.T) {
	for _, verb := range []string{"disable", "enable", "list"} {
		d := providerDescriptor(t, verb)
		root := NewRootCommand(iostreams.System(), buildinfo.Info{})
		cmd, _, err := root.Find(d.CLI)
		if err != nil {
			t.Fatalf("%s: root.Find(%v): %v", d.Name, d.CLI, err)
		}
		if cmd.Name() != d.CLI[len(d.CLI)-1] {
			t.Errorf("%s: resolved command %q, want %q", d.Name, cmd.Name(), d.CLI[len(d.CLI)-1])
		}
	}
}

// TestProviderNoMCPTool pins that every provider.* verb stays local_admin,
// CLI-only: no MCP tool, no presentation route.
func TestProviderNoMCPTool(t *testing.T) {
	for _, verb := range []string{"disable", "enable", "list"} {
		d := providerDescriptor(t, verb)
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
}

// --- provider list --------------------------------------------------------

func TestProviderList_DefaultEnabled(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	configPath := writeProviderConfig(t)

	code, out, errOut := runSession(t, "provider", "list", "--config", configPath, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Operation string `json:"operation"`
		Result    struct {
			Providers []providerListItem `json:"providers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if want := providerDescriptor(t, "list").Name; env.Operation != want {
		t.Errorf("operation = %q, want %q", env.Operation, want)
	}
	if len(env.Result.Providers) != 1 {
		t.Fatalf("providers = %+v, want exactly one entry (anthropic)", env.Result.Providers)
	}
	p := env.Result.Providers[0]
	if p.Name != "anthropic" || !p.Enabled || !p.Known || p.FactID != "" {
		t.Errorf("providers[0] = %+v, want {anthropic enabled=true known=true fact_id=\"\"}", p)
	}
}

// --- provider disable / enable ---------------------------------------------

func TestProviderDisable_KnownProvider(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	configPath := writeProviderConfig(t)

	code, out, errOut := runSession(t, "provider", "disable", "anthropic", "--config", configPath, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Operation string               `json:"operation"`
		Result    providerToggleResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if want := providerDescriptor(t, "disable").Name; env.Operation != want {
		t.Errorf("operation = %q, want %q", env.Operation, want)
	}
	if env.Result.Enabled {
		t.Error("result.enabled = true right after disable")
	}
	if !env.Result.Known {
		t.Error("result.known = false for a provider a launch_variant names")
	}
	if env.Result.FactID == "" {
		t.Error("result.fact_id is empty")
	}
	disableFactID := env.Result.FactID

	// list now shows it disabled, with the same fact id.
	code, out, errOut = runSession(t, "provider", "list", "--config", configPath, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("list: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	var listed struct {
		Result struct {
			Providers []providerListItem `json:"providers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("list output is not valid JSON: %v", err)
	}
	if len(listed.Result.Providers) != 1 {
		t.Fatalf("providers = %+v, want exactly one entry", listed.Result.Providers)
	}
	got := listed.Result.Providers[0]
	if got.Enabled {
		t.Error("list after disable: enabled = true")
	}
	if got.FactID != disableFactID {
		t.Errorf("list after disable: fact_id = %q, want %q", got.FactID, disableFactID)
	}

	// enable supersedes it.
	code, out, errOut = runSession(t, "provider", "enable", "anthropic", "--config", configPath, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("enable: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var enabled struct {
		Operation string               `json:"operation"`
		Result    providerToggleResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &enabled); err != nil {
		t.Fatalf("enable output is not valid JSON: %v", err)
	}
	if want := providerDescriptor(t, "enable").Name; enabled.Operation != want {
		t.Errorf("operation = %q, want %q", enabled.Operation, want)
	}
	if !enabled.Result.Enabled {
		t.Error("result.enabled = false right after enable")
	}
	if enabled.Result.FactID == disableFactID {
		t.Error("enable minted the same fact id as disable")
	}
}

// TestProviderDisable_UnknownName proves the boundary the workplan names
// explicitly: disabling a name no launch_variant declares is allowed, and
// the text-mode render says so.
func TestProviderDisable_UnknownName(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	configPath := writeProviderConfig(t)

	code, out, errOut := runSession(t, "provider", "disable", "no-such-provider", "--config", configPath)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	if !strings.Contains(out, "no variant names this provider") {
		t.Errorf("output = %q, want it to say \"no variant names this provider\"", out)
	}

	// JSON mode carries the same fact as a field, not just prose.
	code, out, errOut = runSession(t, "provider", "disable", "no-such-provider", "--config", configPath, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("json: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var env struct {
		Result providerToggleResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if env.Result.Known {
		t.Error("result.known = true for a name no launch_variant declares")
	}
}

// TestProviderList_NoConfig proves a missing config file is not an error —
// mirroring session list's "empty installation" discipline — and a
// previously-recorded standing fact still surfaces.
func TestProviderList_NoConfig(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no duo.config.yaml written under it

	code, out, errOut := runSession(t, "provider", "list", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var env struct {
		Result struct {
			Providers []providerListItem `json:"providers"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if len(env.Result.Providers) != 0 {
		t.Fatalf("providers = %+v, want none (no config, no standing facts)", env.Result.Providers)
	}

	code, out, errOut = runSession(t, "provider", "disable", "anthropic")
	if code != exitcode.Success {
		t.Fatalf("disable: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	if !strings.Contains(out, "no variant names this provider") {
		t.Errorf("disable with no config output = %q, want the unknown-provider note", out)
	}

	code, out, errOut = runSession(t, "provider", "list", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("list after disable: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if len(env.Result.Providers) != 1 || env.Result.Providers[0].Name != "anthropic" {
		t.Fatalf("providers = %+v, want the standing fact for anthropic to surface even with no config", env.Result.Providers)
	}
	if env.Result.Providers[0].Known {
		t.Error("providers[0].known = true; no config was loaded to know it")
	}
}
