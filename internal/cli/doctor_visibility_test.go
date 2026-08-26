package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/scrub"
)

// doctorJSON is the subset of `duo doctor --output json`'s output these tests
// assert on: the Step 15 visibility-rail sections. Every field name below
// is asserted against its own literal string, never against another
// package's Go constant, so a field-name typo in doctor.go would fail
// these tests instead of passing by coincidence.
type doctorJSON struct {
	HostBinding struct {
		WorkspaceRoot string `json:"workspace_root"`
		WorkspaceID   string `json:"workspace_id"`
		Bound         bool   `json:"bound"`
		Host          *struct {
			Kind          string `json:"kind"`
			InstanceLabel string `json:"instance_label"`
			HostSource    string `json:"host_source"`
			Fingerprint   struct {
				SessionName string `json:"session_name"`
				PaneID      string `json:"pane_id"`
				TerminalID  string `json:"terminal_id"`
			} `json:"fingerprint"`
		} `json:"host"`
		Detail string `json:"detail"`
	} `json:"host_binding"`
	HostDeduction struct {
		Host *struct {
			Kind          string `json:"kind"`
			InstanceLabel string `json:"instance_label"`
			InstanceID    string `json:"instance_id"`
			HostSource    string `json:"host_source"`
		} `json:"host"`
		HostSource        string   `json:"host_source"`
		Ranking           []string `json:"ranking"`
		OutrankedEvidence []struct {
			Source   string `json:"source"`
			FactID   string `json:"fact_id"`
			Captures []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"captures"`
		} `json:"outranked_evidence"`
		DeductionTrail []struct {
			Source      string `json:"source"`
			Consulted   bool   `json:"consulted"`
			YieldedHost bool   `json:"yielded_host"`
		} `json:"deduction_trail"`
	} `json:"host_deduction"`
	Providers []struct {
		Name    string `json:"name"`
		Enabled bool   `json:"enabled"`
		FactID  string `json:"fact_id"`
	} `json:"providers"`
	Config struct {
		Path        string `json:"path"`
		Schema      string `json:"schema"`
		MigrateHint string `json:"migrate_hint"`
		Detail      string `json:"detail"`
	} `json:"config"`
	ScrubGate *struct {
		Host      string   `json:"host"`
		Survivors []string `json:"survivors"`
	} `json:"scrub_gate"`
}

func runDoctorJSON(t *testing.T, args ...string) doctorJSON {
	t.Helper()
	code, out, errOut := runSession(t, append([]string{"doctor", "--output", "json"}, args...)...)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	var report doctorJSON
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("decoding output: %v\n%s", err, out)
	}
	return report
}

// clearAmbientHerdrEnv neutralizes the ambient-environment rung for tests
// that are not exercising it: the calling developer's own shell (this is a
// dogfooding repository, per README.md) may well export HERDR_SOCKET_PATH
// and HERDR_SESSION for a live Herdr session, and t.Setenv only overrides a
// variable for the duration of the test — an unset one still shows through.
// An empty string is what ambientRung's own "unset" check looks for
// (materialize.go: "locator, ok := values[s.LocatorVar]; if !ok ||
// locator == \"\""), so setting it to "" neutralizes the rung exactly the
// way a genuinely-unset variable would.
func clearAmbientHerdrEnv(t *testing.T) {
	t.Helper()
	t.Setenv("HERDR_SOCKET_PATH", "")
	t.Setenv("HERDR_SESSION", "")
}

// hostSourceIsClosed reports whether s is one of the five sealed
// host_source rungs, or "" (no deduction/binding at all) — the closed set
// the goal names.
func hostSourceIsClosed(t *testing.T, s string) {
	t.Helper()
	if s == "" {
		return
	}
	for _, known := range domain.HostSources {
		if s == string(known) {
			return
		}
	}
	t.Errorf("host_source %q is outside the closed vocabulary %v", s, domain.HostSources)
}

// TestDoctorVisibility_BoundWorkspace covers a workspace with an existing
// host correlation: the binding section must agree with what
// `duo workspace host show` prints for the same workspace, and the
// deduction section must land on the same instance (the correlation rung
// outranks env and default).
func TestDoctorVisibility_BoundWorkspace(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no duo.config.yaml written under it
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()
	ws := seedBoundWorkspace(t, dir, domain.HostSourceAmbientEnv, testSocketA)

	report := runDoctorJSON(t, "--workspace", dir)

	if !report.HostBinding.Bound || report.HostBinding.WorkspaceID != string(ws) {
		t.Fatalf("host_binding = %+v, want a bound workspace %s", report.HostBinding, ws)
	}
	if report.HostBinding.Host.Kind != "herdr" || report.HostBinding.Host.InstanceLabel != testSocketA {
		t.Errorf("host_binding.host = %s:%s, want herdr:%s",
			report.HostBinding.Host.Kind, report.HostBinding.Host.InstanceLabel, testSocketA)
	}
	if report.HostBinding.Host.Fingerprint.TerminalID != "term_1" {
		t.Errorf("host_binding.host.fingerprint.terminal_id = %q, want %q",
			report.HostBinding.Host.Fingerprint.TerminalID, "term_1")
	}
	hostSourceIsClosed(t, report.HostBinding.Host.HostSource)

	// The correlation rung outranks the (unset) ambient environment and
	// the (unpolicied) default, so the deduction must land on the bound
	// instance via workspace-correlation, not just report the binding.
	if report.HostDeduction.Host == nil {
		t.Fatal("host_deduction.host is nil, want the bound instance")
	}
	if report.HostDeduction.Host.InstanceLabel != testSocketA {
		t.Errorf("host_deduction.host.instance_label = %q, want %q",
			report.HostDeduction.Host.InstanceLabel, testSocketA)
	}
	if report.HostDeduction.HostSource != string(domain.HostSourceWorkspaceCorrelation) {
		t.Errorf("host_deduction.host_source = %q, want %q",
			report.HostDeduction.HostSource, domain.HostSourceWorkspaceCorrelation)
	}
	hostSourceIsClosed(t, report.HostDeduction.HostSource)
	assertDoctorRanking(t, report.HostDeduction.Ranking)
	if report.ScrubGate != nil {
		t.Errorf("scrub_gate = %+v, want absent (no live listener for testSocketA)", report.ScrubGate)
	}
}

// TestDoctorVisibility_UnboundWithAmbientEnv covers a workspace with no
// correlation, where the ambient environment (HERDR_SOCKET_PATH, read via
// the injected env lookup — os.LookupEnv, reading the process environment
// t.Setenv wrote into) is the only rung that yields, so the deduction
// section reports host_source "ambient-env" and the binding section still
// says "none".
func TestDoctorVisibility_UnboundWithAmbientEnv(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no duo.config.yaml written under it
	dir := t.TempDir()
	t.Setenv("HERDR_SOCKET_PATH", testSocketA)
	t.Setenv("HERDR_SESSION", "ambient-session")

	report := runDoctorJSON(t, "--workspace", dir)

	if report.HostBinding.Bound {
		t.Fatalf("host_binding = %+v, want an unbound workspace", report.HostBinding)
	}
	if report.HostBinding.Detail == "" {
		t.Error("host_binding.detail is empty for an unbound workspace")
	}

	if report.HostDeduction.Host == nil {
		t.Fatal("host_deduction.host is nil, want the ambient instance")
	}
	if report.HostDeduction.Host.Kind != "herdr" || report.HostDeduction.Host.InstanceLabel != testSocketA {
		t.Errorf("host_deduction.host = %s:%s, want herdr:%s",
			report.HostDeduction.Host.Kind, report.HostDeduction.Host.InstanceLabel, testSocketA)
	}
	if report.HostDeduction.HostSource != string(domain.HostSourceAmbientEnv) {
		t.Errorf("host_deduction.host_source = %q, want %q",
			report.HostDeduction.HostSource, domain.HostSourceAmbientEnv)
	}
	hostSourceIsClosed(t, report.HostDeduction.HostSource)

	// A resolved deduction reports no trail (only outranked evidence and
	// the winner) — the trail is for the "nothing resolves" case.
	if len(report.HostDeduction.DeductionTrail) != 0 {
		t.Errorf("deduction_trail = %+v, want empty for a resolved deduction", report.HostDeduction.DeductionTrail)
	}
	assertDoctorRanking(t, report.HostDeduction.Ranking)
}

// TestDoctorVisibility_NoHostResolves covers the case where every rung
// comes up empty: no correlation, no discovered session claims the
// workspace, no ambient environment, and no policy default (this test's
// process environment carries no HERDR_SOCKET_PATH, and the doctor command
// wires no session_hosts.prefer policy without a duo.config/v3 document
// present). The deduction section then carries the full five-rung trail,
// and every host_source in it is still in the closed set.
func TestDoctorVisibility_NoHostResolves(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no duo.config.yaml written under it
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()

	report := runDoctorJSON(t, "--workspace", dir)

	if report.HostDeduction.Host != nil {
		t.Fatalf("host_deduction.host = %+v, want nil (nothing should resolve)", report.HostDeduction.Host)
	}
	if report.HostDeduction.HostSource != "" {
		t.Errorf("host_deduction.host_source = %q, want empty", report.HostDeduction.HostSource)
	}
	if len(report.HostDeduction.DeductionTrail) != 5 {
		t.Fatalf("deduction_trail has %d rungs, want 5", len(report.HostDeduction.DeductionTrail))
	}
	for _, rung := range report.HostDeduction.DeductionTrail {
		hostSourceIsClosed(t, rung.Source)
	}
	assertDoctorRanking(t, report.HostDeduction.Ranking)
}

// TestDoctorVisibility_UnboundWithCwdCorrelation covers a workspace with no
// correlation and no ambient environment, where a discovered Herdr session
// claims the workspace directory as its own identity_cwd: the deduction
// section reports host_source "cwd-correlation" and the claiming session's
// instance ID, even though nothing bound the workspace and nothing is
// exported into this process's environment.
//
// Unlike the other cases in this file, this one needs a duo.config/v3
// document: the cwd rung, like policy-default, only tries the kinds named
// in session_hosts.prefer (materialize.go's cwdCorrelationRung walks
// enabledKinds()), so with no document present it would never look at
// "herdr" at all.
func TestDoctorVisibility_UnboundWithCwdCorrelation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	configHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configHome)
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()

	duoConfigDir := filepath.Join(configHome, "duo")
	if err := os.MkdirAll(duoConfigDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", duoConfigDir, err)
	}
	const minimalV3 = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
agent_runtimes:
  claude_default:
    kind: claude
    executable: claude
launch_variants:
  daily:
    agent_runtime: claude_default
    model_line: claude-opus-4
    model_family: claude
presets:
  daily:
    selection: ordered
    leaves:
      primary:
        candidates:
          - variant: daily
`
	if err := os.WriteFile(filepath.Join(duoConfigDir, "duo.config.yaml"), []byte(minimalV3), 0o600); err != nil {
		t.Fatalf("writing duo.config.yaml: %v", err)
	}

	sessionDir := filepath.Join(configHome, herdr.ConfigDirName, herdr.SessionsDirName, "proj")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", sessionDir, err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, herdr.SessionSocketName), nil, 0o600); err != nil {
		t.Fatalf("writing session socket: %v", err)
	}
	sessionJSON := `{"version":3,"workspaces":[{"identity_cwd":"` + dir + `"}]}`
	if err := os.WriteFile(filepath.Join(sessionDir, herdr.SessionFileName), []byte(sessionJSON), 0o600); err != nil {
		t.Fatalf("writing session.json: %v", err)
	}

	report := runDoctorJSON(t, "--workspace", dir)

	if report.HostBinding.Bound {
		t.Fatalf("host_binding = %+v, want an unbound workspace", report.HostBinding)
	}
	if report.HostDeduction.Host == nil {
		t.Fatal("host_deduction.host is nil, want the session claiming this directory")
	}
	if report.HostDeduction.HostSource != string(domain.HostSourceCwdCorrelation) {
		t.Errorf("host_deduction.host_source = %q, want %q",
			report.HostDeduction.HostSource, domain.HostSourceCwdCorrelation)
	}
	if report.HostDeduction.Host.InstanceID != "herdr:proj" {
		t.Errorf("host_deduction.host.instance_id = %q, want %q",
			report.HostDeduction.Host.InstanceID, "herdr:proj")
	}
	hostSourceIsClosed(t, report.HostDeduction.HostSource)
}

// TestDoctorVisibility_DisabledProvider covers the standing-provider-facts
// section: a disabled provider reports enabled=false with the fact ID that
// disabled it.
func TestDoctorVisibility_DisabledProvider(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // no duo.config.yaml written under it
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()

	a, s, err := openWriteAuthority(context.Background())
	if err != nil {
		t.Fatalf("openWriteAuthority: %v", err)
	}
	factID, err := a.DisableProvider(context.Background(), "codex", "user:beau", "usage limit")
	if err != nil {
		t.Fatalf("DisableProvider: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	report := runDoctorJSON(t, "--workspace", dir)

	if len(report.Providers) != 1 {
		t.Fatalf("providers = %+v, want exactly one standing fact", report.Providers)
	}
	p := report.Providers[0]
	if p.Name != "codex" {
		t.Errorf("providers[0].name = %q, want %q", p.Name, "codex")
	}
	if p.Enabled {
		t.Error("providers[0].enabled = true, want false")
	}
	if p.FactID != string(factID) {
		t.Errorf("providers[0].fact_id = %q, want %q", p.FactID, factID)
	}
}

// TestDoctorVisibility_ConfigMissing covers the plainly-stated "missing"
// config case: no --config flag exists on `duo doctor`, and this test's
// isolated XDG_CONFIG_HOME never has a duo.config document written into
// it, so the default launch-config path resolves to a file that is not
// there.
func TestDoctorVisibility_ConfigMissing(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()

	report := runDoctorJSON(t, "--workspace", dir)

	if report.Config.Schema != "missing" {
		t.Errorf("config.schema = %q, want %q", report.Config.Schema, "missing")
	}
	if report.Config.MigrateHint != "" {
		t.Errorf("config.migrate_hint = %q, want empty for a missing config", report.Config.MigrateHint)
	}
}

// TestDoctorVisibility_HumanModeIncludesNewSections is a light smoke test
// that the text-mode render (not just --output json) carries the four new
// sections, so an operator who never asks for JSON still sees them.
func TestDoctorVisibility_HumanModeIncludesNewSections(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()

	code, out, errOut := runSession(t, "doctor", "--workspace", dir)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	for _, want := range []string{"workspace:", "host deduction", "ranking:", "providers:", "config:"} {
		if !contains(out, want) {
			t.Errorf("human-mode output does not contain %q:\n%s", want, out)
		}
	}
}

// assertDoctorRanking pins host_deduction.ranking to materialize.Rungs —
// the locked five-rung order planning-foundation's handoff 24 clause names.
func assertDoctorRanking(t *testing.T, ranking []string) {
	t.Helper()
	want := []string{
		string(domain.HostSourceExplicitFlag),
		string(domain.HostSourceWorkspaceCorrelation),
		string(domain.HostSourceCwdCorrelation),
		string(domain.HostSourceAmbientEnv),
		string(domain.HostSourcePolicyDefault),
	}
	if len(ranking) != len(want) {
		t.Fatalf("ranking has %d rungs, want %d: %v", len(ranking), len(want), ranking)
	}
	for i := range want {
		if ranking[i] != want[i] {
			t.Errorf("ranking[%d] = %q, want %q", i, ranking[i], want[i])
		}
	}
}

// TestDoctorVisibility_ScrubGateWarnsWhenSurvivorsNames the notes/51 9d
// golden: a bound workspace whose deduced host would trip scrub.Gate
// surfaces survivors on the visibility rail. Doctor warns; it does not
// refuse. The environ is injected — production reads the herdr listener
// via procfs (herdr.ListenerEnviron) with no dial.
func TestDoctorVisibility_ScrubGateWarnsWhenSurvivors(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()
	seedBoundWorkspace(t, dir, domain.HostSourceAmbientEnv, testSocketA)

	prev := doctorHostEnviron
	doctorHostEnviron = func(socketPath string) ([]string, error) {
		if socketPath != testSocketA {
			t.Fatalf("doctorHostEnviron called with %q, want %q", socketPath, testSocketA)
		}
		// Build from scrub.Markers — never retype a marker literal (single-
		// source tripwire in internal/scrub).
		out := make([]string, 0, len(scrub.Markers)+1)
		for _, m := range scrub.Markers {
			out = append(out, m+"=1")
		}
		out = append(out, "PATH=/usr/bin")
		return out, nil
	}
	t.Cleanup(func() { doctorHostEnviron = prev })

	report := runDoctorJSON(t, "--workspace", dir)
	if report.ScrubGate == nil {
		t.Fatal("scrub_gate is nil, want a warning naming survivors")
	}
	if report.ScrubGate.Host != "herdr:"+testSocketA {
		t.Errorf("scrub_gate.host = %q, want herdr:%s", report.ScrubGate.Host, testSocketA)
	}
	if len(report.ScrubGate.Survivors) != len(scrub.Markers) {
		t.Fatalf("survivors = %v, want every scrub.Markers entry", report.ScrubGate.Survivors)
	}
	for _, m := range scrub.Markers {
		// SurvivingMarkers sorts; Markers order is not the report order.
		found := false
		for _, s := range report.ScrubGate.Survivors {
			if s == m {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("survivors missing %q; got %v", m, report.ScrubGate.Survivors)
		}
	}

	code, out, errOut := runSession(t, "doctor", "--workspace", dir)
	if code != exitcode.Success {
		t.Fatalf("human doctor: exit %d (stderr: %s)", code, errOut)
	}
	if !contains(out, "scrub gate:") || !contains(out, "survivors:") {
		t.Errorf("human output missing scrub-gate warning:\n%s", out)
	}
	for _, m := range scrub.Markers {
		if !contains(out, m) {
			t.Errorf("human output missing survivor %q:\n%s", m, out)
		}
	}
}

// TestDoctorVisibility_ScrubGateSilentWhenClean is the clean-host half of
// the notes/51 9d golden: a deduced host whose listener environ carries no
// markers produces no scrub_gate field and no human warning line.
func TestDoctorVisibility_ScrubGateSilentWhenClean(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()
	seedBoundWorkspace(t, dir, domain.HostSourceAmbientEnv, testSocketA)

	prev := doctorHostEnviron
	doctorHostEnviron = func(string) ([]string, error) {
		return []string{"PATH=/usr/bin", "HOME=/tmp"}, nil
	}
	t.Cleanup(func() { doctorHostEnviron = prev })

	report := runDoctorJSON(t, "--workspace", dir)
	if report.ScrubGate != nil {
		t.Errorf("scrub_gate = %+v, want absent on a clean host", report.ScrubGate)
	}

	code, out, errOut := runSession(t, "doctor", "--workspace", dir)
	if code != exitcode.Success {
		t.Fatalf("human doctor: exit %d (stderr: %s)", code, errOut)
	}
	if contains(out, "scrub gate:") {
		t.Errorf("human output has a scrub-gate section on a clean host:\n%s", out)
	}
	if !contains(out, "winner:") || !contains(out, "ranking:") {
		t.Errorf("human output missing ranking/winner lines:\n%s", out)
	}
}
