package cli

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
)

// doctorJSON is the subset of `duo doctor --json`'s output these tests
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
			HostSource    string `json:"host_source"`
		} `json:"host"`
		HostSource        string `json:"host_source"`
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
}

func runDoctorJSON(t *testing.T, args ...string) doctorJSON {
	t.Helper()
	code, out, errOut := runSession(t, append([]string{"doctor", "--json"}, args...)...)
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

// hostSourceIsClosed reports whether s is one of the four sealed
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
}

// TestDoctorVisibility_NoHostResolves covers the case where every rung
// comes up empty: no correlation, no ambient environment, and no policy
// default (this test's process environment carries no HERDR_SOCKET_PATH,
// and the doctor command wires no session_hosts.prefer policy without a
// duo.config/v3 document present). The deduction section then carries the
// full four-rung trail, and every host_source in it is still in the closed
// set.
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
	if len(report.HostDeduction.DeductionTrail) != 4 {
		t.Fatalf("deduction_trail has %d rungs, want 4", len(report.HostDeduction.DeductionTrail))
	}
	for _, rung := range report.HostDeduction.DeductionTrail {
		hostSourceIsClosed(t, rung.Source)
	}
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
// that the text-mode render (not just --json) carries the four new
// sections, so an operator without --json still sees them.
func TestDoctorVisibility_HumanModeIncludesNewSections(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	clearAmbientHerdrEnv(t)
	dir := t.TempDir()

	code, out, errOut := runSession(t, "doctor", "--workspace", dir)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	for _, want := range []string{"workspace:", "host deduction", "providers:", "config:"} {
		if !contains(out, want) {
			t.Errorf("human-mode output does not contain %q:\n%s", want, out)
		}
	}
}
