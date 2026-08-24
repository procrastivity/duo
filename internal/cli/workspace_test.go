package cli

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
)

// These tests exercise `duo workspace host show` and `duo workspace host
// rebind` through the same built-root Execute path main.go uses, against a
// store isolated to each test's own XDG_DATA_HOME. Operation names are
// derived from the registry by CLI path rather than repeated as literals,
// the discipline internal/registry's TestNoDuplicateOperationTableOutsideRegistry
// holds every package outside the registry to.

const (
	testSocketA = "/run/user/1000/herdr/alpha.sock"
	testSocketB = "/run/user/1000/herdr/beta.sock"
)

// workspaceDescriptor returns the registered operation whose CLI verb path
// is {"workspace", "host", verb}.
func workspaceDescriptor(t *testing.T, verb string) registry.Descriptor {
	t.Helper()
	want := []string{"workspace", "host", verb}
	for _, d := range registry.All() {
		if slices.Equal(d.CLI, want) {
			return d
		}
	}
	t.Fatalf("no registered operation has CLI path %v", want)
	return registry.Descriptor{}
}

// seedBoundWorkspace enrolls a session (which is what creates a workspace)
// and, when source is non-empty, writes a first host correlation through the
// domain API step 14 will call after a successful spawn. It leaves no store
// handle open, so the verb under test can take the writer lease.
func seedBoundWorkspace(t *testing.T, root string, source domain.HostSource, instance string) domain.WorkspaceID {
	t.Helper()
	ctx := context.Background()

	a, s, err := openWriteAuthority(ctx)
	if err != nil {
		t.Fatalf("openWriteAuthority: %v", err)
	}
	defer func() { _ = s.Close() }()

	res, err := a.Enroll(ctx, domain.EnrollRequest{
		Candidate: domain.Candidate{
			RootPath: root,
			Fingerprint: domain.Fingerprint{
				IntegrationInstance: "herdr:alpha",
				Epoch:               domain.HostEpoch{Kind: "herdr.terminal_id", Value: "term_1", Scope: domain.EpochScopePane},
				Container:           "w1:p1",
			},
		},
		Actor:       "user:beau",
		Attestation: domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if source == "" {
		return res.Workspace
	}

	if _, err := a.BindWorkspaceHost(ctx, domain.BindHostRequest{
		Workspace:  res.Workspace,
		Kind:       "herdr",
		Instance:   instance,
		InstanceID: "herdr:alpha",
		Source:     source,
		Fingerprint: domain.HostFingerprint{
			SessionName: "alpha",
			PaneID:      "w1:p1",
			TerminalID:  "term_1",
			Process:     domain.ProcessBirth{PID: 4001, StartedAt: "2026-08-24T10:00:00.000Z"},
		},
		Actor:    "user:beau",
		Evidence: "herdr launch evidence",
	}); err != nil {
		t.Fatalf("BindWorkspaceHost: %v", err)
	}
	return res.Workspace
}

// TestWorkspaceHostCLIPathsMatchRegistry pins both verbs at exactly the CLI
// path their registry rows declare.
func TestWorkspaceHostCLIPathsMatchRegistry(t *testing.T) {
	for _, verb := range []string{"show", "rebind"} {
		d := workspaceDescriptor(t, verb)
		root := NewRootCommand(iostreams.System(), buildinfo.Info{})
		cmd, _, err := root.Find(d.CLI)
		if err != nil {
			t.Fatalf("%s: root.Find(%v): %v", d.Name, d.CLI, err)
		}
		if cmd.Name() != verb {
			t.Errorf("%s: resolved command %q, want %q", d.Name, cmd.Name(), verb)
		}
		if d.Projectability != registry.LocalAdmin {
			t.Errorf("%s projectability = %q, want local_admin", d.Name, d.Projectability)
		}
		if d.MCPTool != "" {
			t.Errorf("%s projects MCP tool %q; local_admin rows carry none", d.Name, d.MCPTool)
		}
	}
}

// TestWorkspaceHostShow_Unbound is the "none" answer: an installation that
// has never bound anything reports an unbound workspace and writes nothing.
func TestWorkspaceHostShow_Unbound(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()

	code, out, errOut := runSession(t, "workspace", "host", "show", "--workspace", dir)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	if !contains(out, "host:            none") {
		t.Errorf("output does not report an unbound host:\n%s", out)
	}
}

func TestWorkspaceHostShow_Unbound_JSON(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()

	code, out, errOut := runSession(t, "workspace", "host", "show", "--workspace", dir, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Operation string `json:"operation"`
		Result    struct {
			Bound bool   `json:"bound"`
			Host  any    `json:"host"`
			Root  string `json:"workspace_root"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("decoding output: %v\n%s", err, out)
	}
	if env.Operation != workspaceDescriptor(t, "show").Name {
		t.Errorf("operation = %q, want %q", env.Operation, workspaceDescriptor(t, "show").Name)
	}
	if env.Result.Bound || env.Result.Host != nil {
		t.Errorf("an unbound workspace reported a host: %+v", env.Result)
	}
	if env.Result.Root != dir {
		t.Errorf("workspace_root = %q, want %q", env.Result.Root, dir)
	}
}

// TestWorkspaceHostShow_ReadsFirstBind proves show reads the binding the
// first-bind domain API wrote, with its provenance and its fingerprints.
func TestWorkspaceHostShow_ReadsFirstBind(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	ws := seedBoundWorkspace(t, dir, domain.HostSourceAmbientEnv, testSocketA)

	code, out, errOut := runSession(t, "workspace", "host", "show", "--workspace", dir, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Result struct {
			WorkspaceID string `json:"workspace_id"`
			Bound       bool   `json:"bound"`
			Host        struct {
				Kind          string `json:"kind"`
				InstanceLabel string `json:"instance_label"`
				HostSource    string `json:"host_source"`
				Fingerprint   struct {
					SessionName string `json:"session_name"`
					PaneID      string `json:"pane_id"`
					TerminalID  string `json:"terminal_id"`
					ProcessPID  int    `json:"process_pid"`
				} `json:"fingerprint"`
			} `json:"host"`
			Provenance struct {
				FactID   string `json:"fact_id"`
				FactKind string `json:"fact_kind"`
				At       string `json:"at"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("decoding output: %v\n%s", err, out)
	}
	r := env.Result
	if !r.Bound || r.WorkspaceID != string(ws) {
		t.Fatalf("result = %+v, want a bound workspace %s", r, ws)
	}
	if r.Host.Kind != "herdr" || r.Host.InstanceLabel != testSocketA {
		t.Errorf("host = %s:%s, want herdr:%s", r.Host.Kind, r.Host.InstanceLabel, testSocketA)
	}
	if r.Host.HostSource != string(domain.HostSourceAmbientEnv) {
		t.Errorf("host_source = %q, want %q", r.Host.HostSource, domain.HostSourceAmbientEnv)
	}
	if r.Host.Fingerprint.TerminalID != "term_1" || r.Host.Fingerprint.PaneID != "w1:p1" ||
		r.Host.Fingerprint.SessionName != "alpha" || r.Host.Fingerprint.ProcessPID != 4001 {
		t.Errorf("fingerprint = %+v, want the notes/19 §5 set", r.Host.Fingerprint)
	}
	if r.Provenance.FactID == "" || r.Provenance.FactKind != string(domain.FactWorkspaceHostBound) || r.Provenance.At == "" {
		t.Errorf("provenance = %+v, want the host_bound fact that recorded it", r.Provenance)
	}
}

// TestWorkspaceHostRebind_RecordsBothInstances runs the rebind verb and then
// show, proving the write lands and the read model follows it.
func TestWorkspaceHostRebind_RecordsBothInstances(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	seedBoundWorkspace(t, dir, domain.HostSourcePolicyDefault, testSocketA)

	code, out, errOut := runSession(t,
		"workspace", "host", "rebind",
		"--workspace", dir,
		"--host", "herdr:"+testSocketB,
		"--host-instance-id", "herdr:beta",
		"--session-name", "beta",
		"--pane-id", "w2:p3",
		"--terminal-id", "term_9",
		"--process-pid", "5100",
		"--process-started-at", "2026-08-24T11:00:00.000Z",
		"--evidence", "herdr status server --json on beta",
		"--reason", "the alpha server was retired",
		"--output", "json",
	)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Operation string `json:"operation"`
		Result    struct {
			Previous struct {
				InstanceLabel string `json:"instance_label"`
				Fingerprint   struct {
					TerminalID string `json:"terminal_id"`
				} `json:"fingerprint"`
			} `json:"previous_host"`
			Host struct {
				InstanceLabel string `json:"instance_label"`
				HostSource    string `json:"host_source"`
				Fingerprint   struct {
					TerminalID string `json:"terminal_id"`
				} `json:"fingerprint"`
			} `json:"host"`
			Provenance struct {
				FactKind string `json:"fact_kind"`
				Evidence string `json:"evidence"`
			} `json:"provenance"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("decoding output: %v\n%s", err, out)
	}
	if env.Operation != workspaceDescriptor(t, "rebind").Name {
		t.Errorf("operation = %q, want %q", env.Operation, workspaceDescriptor(t, "rebind").Name)
	}
	r := env.Result
	if r.Previous.InstanceLabel != testSocketA || r.Host.InstanceLabel != testSocketB {
		t.Errorf("rebind reported %q -> %q, want %q -> %q",
			r.Previous.InstanceLabel, r.Host.InstanceLabel, testSocketA, testSocketB)
	}
	if r.Previous.Fingerprint.TerminalID != "term_1" || r.Host.Fingerprint.TerminalID != "term_9" {
		t.Errorf("both instances did not carry their own fingerprints: %+v", r)
	}
	if r.Host.HostSource != string(domain.HostSourceExplicitFlag) {
		t.Errorf("host_source = %q, want %q: a rebind always names an explicit target",
			r.Host.HostSource, domain.HostSourceExplicitFlag)
	}
	if r.Provenance.FactKind != string(domain.FactWorkspaceHostRebound) || r.Provenance.Evidence == "" {
		t.Errorf("provenance = %+v, want a host_rebound fact naming its evidence", r.Provenance)
	}

	// show now reads the new binding, and reports what it replaced.
	code, showOut, errOut := runSession(t, "workspace", "host", "show", "--workspace", dir)
	if code != exitcode.Success {
		t.Fatalf("show exit code = %d (stderr: %s)", code, errOut)
	}
	if !contains(showOut, testSocketB) || !contains(showOut, "replaced:        herdr:"+testSocketA) {
		t.Errorf("show does not reflect the rebind:\n%s", showOut)
	}
}

// TestWorkspaceHostRebind_RequiresExplicitTarget: a bare kind is refused
// rather than resolved to an instance. Deduction is not this verb's job.
func TestWorkspaceHostRebind_RequiresExplicitTarget(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	seedBoundWorkspace(t, dir, domain.HostSourcePolicyDefault, testSocketA)

	code, _, errOut := runSession(t,
		"workspace", "host", "rebind",
		"--workspace", dir, "--host", "herdr", "--evidence", "e", "--terminal-id", "term_9",
	)
	if code == exitcode.Success {
		t.Fatal("a bare --host kind was accepted; a rebind needs an explicit instance")
	}
	if !contains(errOut, "<kind>:<instance>") {
		t.Errorf("stderr does not name the required target form:\n%s", errOut)
	}
}

// TestWorkspaceHostRebind_RequiresEvidence: the verb never runs without
// naming what it rests on. Cobra refuses the missing required flag before
// anything opens the store.
func TestWorkspaceHostRebind_RequiresEvidence(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	seedBoundWorkspace(t, dir, domain.HostSourcePolicyDefault, testSocketA)

	code, _, errOut := runSession(t,
		"workspace", "host", "rebind",
		"--workspace", dir, "--host", "herdr:"+testSocketB, "--terminal-id", "term_9",
	)
	if code == exitcode.Success {
		t.Fatal("a rebind ran with no --evidence")
	}
	if !contains(errOut, "evidence") {
		t.Errorf("stderr does not name the missing evidence flag:\n%s", errOut)
	}
}

// TestWorkspaceHostRebind_RefusesUnboundWorkspace: there is no bind path
// through this verb. A workspace with no correlation is a precondition
// failure, and the message points at what does bind one.
func TestWorkspaceHostRebind_RefusesUnboundWorkspace(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir := t.TempDir()
	seedBoundWorkspace(t, dir, "", "")

	code, _, errOut := runSession(t,
		"workspace", "host", "rebind",
		"--workspace", dir, "--host", "herdr:"+testSocketB,
		"--evidence", "e", "--terminal-id", "term_9",
	)
	if code == exitcode.Success {
		t.Fatal("a rebind succeeded against a workspace with no correlation")
	}
	if !contains(errOut, "no host correlation") {
		t.Errorf("stderr does not explain the refusal:\n%s", errOut)
	}
}

// TestWorkspaceHostShow_NeverWrites: running show against an installation
// with no store leaves none behind, mirroring doctor's probeStore discipline.
func TestWorkspaceHostShow_NeverWrites(t *testing.T) {
	data := t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)

	if code, _, errOut := runSession(t, "workspace", "host", "show", "--workspace", t.TempDir()); code != exitcode.Success {
		t.Fatalf("exit code = %d (stderr: %s)", code, errOut)
	}
	path, err := authorityStorePath()
	if err != nil {
		t.Fatalf("authorityStorePath: %v", err)
	}
	if fileExists(path) {
		t.Errorf("show created the authority store at %s; it never writes", path)
	}
}

// contains is strings.Contains, named for what the assertions read as.
func contains(haystack, needle string) bool { return strings.Contains(haystack, needle) }

// fileExists reports whether path is present on disk.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
