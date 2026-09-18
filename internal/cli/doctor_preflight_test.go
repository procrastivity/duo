package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/doctor"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/manifest"
	"github.com/procrastivity/duo/internal/store"
)

const doctorPreflightConfig = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
agent_runtimes:
  pi_default:
    kind: pi
    executable: pi
launch_variants:
  builder:
    agent_runtime: pi_default
    model_line: openai/gpt-5.3-codex
    model_family: gpt
presets:
  zeta:
    leaves:
      main:
        candidates: [{variant: builder}]
  builder:
    leaves:
      main:
        candidates: [{variant: builder}]
`

func prepareDoctorPreflight(t *testing.T) (workspace, configPath, socket string) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	clearAmbientHerdrEnv(t)
	workspace = t.TempDir()
	configPath = filepath.Join(t.TempDir(), "duo.config.yaml")
	if err := os.WriteFile(configPath, []byte(doctorPreflightConfig), 0o600); err != nil {
		t.Fatalf("WriteFile config: %v", err)
	}
	code, _, stderr := runSession(t, "install", "portable-launchers", "--workspace", workspace)
	if code != exitcode.Success {
		t.Fatalf("install portable projection: exit %d: %s", code, stderr)
	}
	socket = filepath.Join(t.TempDir(), "herdr.sock")
	return workspace, configPath, socket
}

func useDoctorProbe(t *testing.T, probe adapter.Probe, err error) {
	t.Helper()
	previous := doctorProbeHerdr
	doctorProbeHerdr = func(context.Context, herdr.Config) (adapter.Probe, error) { return probe, err }
	t.Cleanup(func() { doctorProbeHerdr = previous })
}

func runLauncherPreflight(t *testing.T, args ...string) (doctor.LauncherPreflight, string) {
	t.Helper()
	code, stdout, stderr := runSession(t, append([]string{"doctor", "--output", "json"}, args...)...)
	if code != exitcode.Success {
		t.Fatalf("doctor exit = %d, want 0 (stderr: %s)", code, stderr)
	}
	var report struct {
		Preflight doctor.LauncherPreflight `json:"launcher_preflight"`
	}
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("decode doctor JSON: %v\n%s", err, stdout)
	}
	return report.Preflight, stdout
}

func preflightCheck(t *testing.T, p doctor.LauncherPreflight, id string) doctor.Check {
	t.Helper()
	for _, check := range p.Checks {
		if check.ID == id {
			return check
		}
	}
	t.Fatalf("preflight has no %s check: %+v", id, p.Checks)
	return doctor.Check{}
}

func TestDoctorLauncherPreflightReadyIsClosedDeterministicAndReadOnly(t *testing.T) {
	workspace, configPath, socket := prepareDoctorPreflight(t)
	useDoctorProbe(t, adapter.Probe{
		DetectedVersion:          herdr.PinnedVersion,
		ProtocolOrFormatIdentity: "herdr-socket-api/20",
		FixtureOrSchemaDigest:    herdr.PinnedSchemaDigest,
		ConnectionState:          "connected",
		Compatibility:            adapter.CompatibilitySupported,
	}, nil)
	before := snapshotTree(t, workspace)
	dataHome := os.Getenv("XDG_DATA_HOME")

	p, _ := runLauncherPreflight(t, "--workspace", workspace, "--config", configPath, "--host", "herdr:"+socket)

	if p.Schema != doctor.LauncherPreflightSchema || p.Status != "ready" || p.AuthorityScope != "local" {
		t.Fatalf("preflight identity/status = %q/%q/%q, want schema/ready/local", p.Schema, p.Status, p.AuthorityScope)
	}
	wantIDs := []string{
		doctor.CheckDuoExecutable, doctor.CheckEffectiveConfig, doctor.CheckAuthorityStore,
		doctor.CheckWorkspace, doctor.CheckHostSelection, doctor.CheckHostReachability,
		doctor.CheckSkillProjection, doctor.CheckHostCompatibility,
	}
	if len(p.Checks) != len(wantIDs) {
		t.Fatalf("checks = %d, want exactly 8", len(p.Checks))
	}
	for i, id := range wantIDs {
		if p.Checks[i].ID != id {
			t.Errorf("checks[%d].id = %q, want %q", i, p.Checks[i].ID, id)
		}
		if p.Checks[i].Status != "pass" || p.Checks[i].Code != "ok" {
			t.Errorf("check %s = %s/%s, want pass/ok", id, p.Checks[i].Status, p.Checks[i].Code)
		}
	}
	if p.Duo.Version != "v0.1.0-test" || p.Duo.Commit != "abcdef0" || p.Duo.BuildDate != "2026-08-23T00:00:00Z" || !filepath.IsAbs(p.Duo.ExecutablePath) {
		t.Errorf("duo identity = %+v", p.Duo)
	}
	doc, err := config.LoadV3(configPath)
	if err != nil {
		t.Fatalf("LoadV3: %v", err)
	}
	wantConfigDigest, err := launch.ConfigurationDigest(doc)
	if err != nil {
		t.Fatalf("ConfigurationDigest: %v", err)
	}
	if p.Config.Path != configPath || p.Config.Schema != "duo.config/v3" || !p.Config.Valid ||
		p.Config.EffectiveDigest != wantConfigDigest || strings.Join(p.Config.Presets, ",") != "builder,zeta" {
		t.Errorf("config identity = %+v", p.Config)
	}
	if p.Authority.State != "initializable" || p.Authority.Present {
		t.Errorf("authority = %+v, want absent initializable", p.Authority)
	}
	if p.Workspace.SelectedPath != workspace || p.Workspace.Source != "flag" || !p.Workspace.Exists || !p.Workspace.Directory {
		t.Errorf("workspace = %+v", p.Workspace)
	}
	if !p.Host.Selected || !p.Host.Reachable || p.Host.InstanceLabel != socket || p.Host.HostSource != "explicit-flag" || p.Host.Compatibility != "supported" {
		t.Errorf("host = %+v", p.Host)
	}
	if p.SkillProjection.State != "current" || p.SkillProjection.Name != "duo-delegation-loop" ||
		p.SkillProjection.File != filepath.Join(workspace, filepath.FromSlash(manifest.PortableProjectionRoot), manifest.PortableSkillFile) {
		t.Errorf("skill projection = %+v", p.SkillProjection)
	}
	if after := snapshotTree(t, workspace); !bytes.Equal(after, before) {
		t.Fatalf("doctor changed workspace\nbefore: %s\nafter:  %s", before, after)
	}
	if _, err := os.Stat(dataHome); !os.IsNotExist(err) {
		t.Fatalf("doctor created the missing XDG data root: %v", err)
	}
}

func TestDoctorLauncherPreflightUnreachableAndModifiedAreNotReadyButExitZero(t *testing.T) {
	workspace, configPath, socket := prepareDoctorPreflight(t)
	useDoctorProbe(t, adapter.Probe{
		ConnectionState: "unreachable", Compatibility: adapter.CompatibilityUnavailable,
	}, nil)

	p, _ := runLauncherPreflight(t, "--workspace", workspace, "--config", configPath, "--host", "herdr:"+socket)
	if p.Status != "not_ready" || p.AuthorityScope != "unavailable" {
		t.Fatalf("status/scope = %s/%s, want not_ready/unavailable", p.Status, p.AuthorityScope)
	}
	if check := preflightCheck(t, p, doctor.CheckHostReachability); check.Status != "fail" || check.Code != "host.unreachable" || check.Action == "" {
		t.Errorf("host reachability check = %+v", check)
	}
	if check := preflightCheck(t, p, doctor.CheckHostCompatibility); check.Status != "not_checked" {
		t.Errorf("host compatibility check = %+v", check)
	}

	skill := filepath.Join(workspace, filepath.FromSlash(manifest.PortableProjectionRoot), manifest.PortableSkillFile)
	if err := os.WriteFile(skill, []byte("locally modified\n"), 0o644); err != nil {
		t.Fatalf("modify skill: %v", err)
	}
	before := snapshotTree(t, workspace)
	p, _ = runLauncherPreflight(t, "--workspace", workspace, "--config", configPath, "--host", "herdr:"+socket)
	if p.SkillProjection.State != "modified" {
		t.Fatalf("projection state = %q, want modified", p.SkillProjection.State)
	}
	if check := preflightCheck(t, p, doctor.CheckSkillProjection); check.Status != "fail" || check.Code != "projection.modified" {
		t.Errorf("projection check = %+v", check)
	}
	if after := snapshotTree(t, workspace); !bytes.Equal(after, before) {
		t.Fatalf("doctor repaired modified projection\nbefore: %s\nafter:  %s", before, after)
	}
}

func TestDoctorLauncherPreflightFreshMissingProjectionIsNotReady(t *testing.T) {
	workspace, configPath, socket := prepareDoctorPreflight(t)
	projectionRoot := filepath.Join(workspace, filepath.FromSlash(manifest.PortableProjectionRoot))
	if err := os.RemoveAll(projectionRoot); err != nil {
		t.Fatalf("remove installed projection for fresh-workspace case: %v", err)
	}
	useDoctorProbe(t, adapter.Probe{
		DetectedVersion:          herdr.PinnedVersion,
		ProtocolOrFormatIdentity: "herdr-socket-api/20",
		ConnectionState:          "connected",
		Compatibility:            adapter.CompatibilitySupported,
	}, nil)

	p, _ := runLauncherPreflight(t, "--workspace", workspace, "--config", configPath, "--host", "herdr:"+socket)
	if p.Status != "not_ready" || p.Authority.State != "initializable" || p.SkillProjection.State != "missing" {
		t.Fatalf("status/authority/projection = %s/%s/%s", p.Status, p.Authority.State, p.SkillProjection.State)
	}
	if check := preflightCheck(t, p, doctor.CheckSkillProjection); check.Status != "fail" || check.Code != "projection.missing" {
		t.Errorf("projection check = %+v", check)
	}
	if _, err := os.Stat(projectionRoot); !os.IsNotExist(err) {
		t.Fatalf("doctor created missing projection: %v", err)
	}
}

func TestDoctorLauncherPreflightUnverifiedCompatibilityWarnsButRemainsReady(t *testing.T) {
	workspace, configPath, socket := prepareDoctorPreflight(t)
	useDoctorProbe(t, adapter.Probe{
		DetectedVersion:          "0.9.0",
		ProtocolOrFormatIdentity: "herdr-socket-api/21",
		ConnectionState:          "connected",
		Compatibility:            adapter.CompatibilityUnverified,
	}, nil)

	p, _ := runLauncherPreflight(t, "--workspace", workspace, "--config", configPath, "--host", "herdr:"+socket)
	if p.Status != "ready" || p.AuthorityScope != "local" {
		t.Fatalf("status/scope = %s/%s, want ready/local", p.Status, p.AuthorityScope)
	}
	if check := preflightCheck(t, p, doctor.CheckHostCompatibility); check.Status != "warning" || check.Code != "host.compatibility_unverified" || check.Required {
		t.Errorf("host compatibility = %+v, want informational warning", check)
	}
}

func TestDoctorLauncherPreflightUnavailableResourcesUseDependentNotChecked(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "absent-data")
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "absent-config"))
	clearAmbientHerdrEnv(t)
	missingWorkspace := filepath.Join(t.TempDir(), "missing-workspace")
	missingConfig := filepath.Join(t.TempDir(), "missing-config.yaml")

	p, _ := runLauncherPreflight(t, "--workspace", missingWorkspace, "--config", missingConfig, "--host", "herdr:/missing/herdr.sock")
	if p.Status != "not_ready" || p.AuthorityScope != "unavailable" {
		t.Fatalf("status/scope = %s/%s", p.Status, p.AuthorityScope)
	}
	if check := preflightCheck(t, p, doctor.CheckEffectiveConfig); check.Code != "config.missing" || check.Action == "" {
		t.Errorf("config check = %+v", check)
	}
	if check := preflightCheck(t, p, doctor.CheckAuthorityStore); check.Status != "pass" || p.Authority.State != "initializable" {
		t.Errorf("authority check/state = %+v/%s", check, p.Authority.State)
	}
	if check := preflightCheck(t, p, doctor.CheckWorkspace); check.Code != "workspace.invalid" || check.Action == "" {
		t.Errorf("workspace check = %+v", check)
	}
	for _, id := range []string{doctor.CheckHostSelection, doctor.CheckHostReachability, doctor.CheckSkillProjection, doctor.CheckHostCompatibility} {
		if check := preflightCheck(t, p, id); check.Status != "not_checked" {
			t.Errorf("%s = %+v, want not_checked", id, check)
		}
	}
	if _, err := os.Stat(dataHome); !os.IsNotExist(err) {
		t.Fatalf("doctor created unavailable local authority root: %v", err)
	}
}

func TestDoctorLauncherPreflightReportsWriterWithoutChangingLease(t *testing.T) {
	workspace, configPath, socket := prepareDoctorPreflight(t)
	useDoctorProbe(t, adapter.Probe{ConnectionState: "connected", Compatibility: adapter.CompatibilitySupported}, nil)
	storePath, err := doctor.DefaultStorePath()
	if err != nil {
		t.Fatalf("DefaultStorePath: %v", err)
	}
	writer, err := store.OpenAuthority(storePath)
	if err != nil {
		t.Fatalf("OpenAuthority: %v", err)
	}
	t.Cleanup(func() { _ = writer.Close() })
	incarnation := writer.Incarnation()

	p, _ := runLauncherPreflight(t, "--workspace", workspace, "--config", configPath, "--host", "herdr:"+socket)
	if p.Authority.State != "writer_active" || !p.Authority.WriterActive {
		t.Fatalf("authority = %+v, want writer_active", p.Authority)
	}
	if p.Status != "not_ready" || p.AuthorityScope != "local" {
		t.Errorf("status/scope = %s/%s, want not_ready/local for a reachable local store with an active writer", p.Status, p.AuthorityScope)
	}
	if check := preflightCheck(t, p, doctor.CheckAuthorityStore); check.Code != "authority.writer_active" || check.Status != "fail" {
		t.Errorf("authority check = %+v", check)
	}
	if writer.Incarnation() != incarnation {
		t.Errorf("writer incarnation changed: %q != %q", writer.Incarnation(), incarnation)
	}
	if err := writer.RenewLease(context.Background()); err != nil {
		t.Fatalf("doctor disrupted active writer lease: %v", err)
	}
}

func TestDoctorLauncherPreflightStillChecksExplicitHostWhenAuthorityUnavailable(t *testing.T) {
	workspace, configPath, socket := prepareDoctorPreflight(t)
	dataHome := os.Getenv("XDG_DATA_HOME")
	if err := os.WriteFile(dataHome, []byte("not a directory\n"), 0o600); err != nil {
		t.Fatalf("block authority data root: %v", err)
	}
	useDoctorProbe(t, adapter.Probe{
		DetectedVersion:          herdr.PinnedVersion,
		ProtocolOrFormatIdentity: "herdr-socket-api/20",
		FixtureOrSchemaDigest:    herdr.PinnedSchemaDigest,
		ConnectionState:          "connected",
		Compatibility:            adapter.CompatibilitySupported,
	}, nil)

	p, _ := runLauncherPreflight(t, "--workspace", workspace, "--config", configPath, "--host", "herdr:"+socket)
	if p.Status != "not_ready" || p.AuthorityScope != "unavailable" || p.Authority.State != "unavailable" {
		t.Fatalf("status/scope/authority = %s/%s/%s", p.Status, p.AuthorityScope, p.Authority.State)
	}
	for _, id := range []string{doctor.CheckHostSelection, doctor.CheckHostReachability, doctor.CheckHostCompatibility, doctor.CheckSkillProjection} {
		if check := preflightCheck(t, p, id); check.Status != "pass" {
			t.Errorf("independent check %s = %+v, want pass", id, check)
		}
	}
}

func TestDoctorLauncherPreflightCorruptAuthorityIsTrustworthyNotReadyAndUnchanged(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	clearAmbientHerdrEnv(t)
	storePath, err := doctor.DefaultStorePath()
	if err != nil {
		t.Fatalf("DefaultStorePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(storePath), 0o700); err != nil {
		t.Fatalf("MkdirAll store parent: %v", err)
	}
	before := []byte("not a sqlite authority database\n")
	if err := os.WriteFile(storePath, before, 0o600); err != nil {
		t.Fatalf("WriteFile corrupt store: %v", err)
	}

	p, _ := runLauncherPreflight(t, "--workspace", t.TempDir(), "--config", filepath.Join(t.TempDir(), "missing.yaml"))
	if p.Status != "not_ready" || p.Authority.State != "unhealthy" {
		t.Fatalf("status/authority = %s/%s, want not_ready/unhealthy", p.Status, p.Authority.State)
	}
	if check := preflightCheck(t, p, doctor.CheckAuthorityStore); check.Status != "fail" || check.Code != "authority.unhealthy" {
		t.Errorf("authority check = %+v", check)
	}
	after, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("ReadFile corrupt store after doctor: %v", err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("doctor changed corrupt authority bytes: %q", after)
	}
}

func TestDoctorLauncherPreflightHumanRendersHeadingChecksAndActions(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	clearAmbientHerdrEnv(t)
	workspace := t.TempDir()
	code, stdout, stderr := runSession(t, "doctor", "--workspace", workspace)
	if code != exitcode.Success {
		t.Fatalf("doctor exit = %d: %s", code, stderr)
	}
	for _, want := range []string{
		"launcher preflight: not ready",
		"[pass] duo_executable:",
		"[fail] effective_config:",
		"[not checked] host_selection:",
		"[fail] skill_projection:",
		"[not checked] host_compatibility:",
		"action: Config stage:",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("human output missing %q:\n%s", want, stdout)
		}
	}
}
