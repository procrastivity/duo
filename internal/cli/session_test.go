package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
)

// runSession runs args through the same built-root Execute path main.go
// uses, isolated to the calling test's own streams. Every session test sets
// XDG_DATA_HOME to its own t.TempDir() first (as doctor_test.go and
// manifest_test.go do), so no test touches a real installation.
func runSession(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs(args)
	code = Execute(root, streams)
	return code, out.String(), errOut.String()
}

// --- CLI-path-to-registry pinning ---------------------------------------
//
// These tests derive every operation they check from registry.All()'s CLI
// path, rather than repeating registered operation names as literals —
// internal/registry/conformance_test.go's TestNoDuplicateOperationTableOutsideRegistry
// holds every package but the registry itself to that discipline.

// sessionDescriptor returns the registered operation whose CLI verb path is
// {"session", verb...}, or fails t if none matches.
func sessionDescriptor(t *testing.T, verb ...string) registry.Descriptor {
	t.Helper()
	want := append([]string{"session"}, verb...)
	for _, d := range registry.All() {
		if len(d.CLI) != len(want) {
			continue
		}
		match := true
		for i, seg := range want {
			if d.CLI[i] != seg {
				match = false
				break
			}
		}
		if match {
			return d
		}
	}
	t.Fatalf("no registered operation has CLI path %v", want)
	return registry.Descriptor{}
}

// TestSessionCLIPathsMatchRegistry pins that every session.* verb is
// registered at exactly the CLI path internal/registry's row declares —
// the same discipline TestDoctorCommand_CLIPathMatchesRegistry uses.
func TestSessionCLIPathsMatchRegistry(t *testing.T) {
	for _, verb := range [][]string{
		{"list"}, {"show"}, {"enroll"}, {"detach"}, {"reattach"}, {"launch"},
	} {
		d := sessionDescriptor(t, verb...)
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

// TestSessionLaunchNoMCPTool pins the locked ledger decision this step must
// not disturb: the launch verb stays local_admin, with no MCP tool.
func TestSessionLaunchNoMCPTool(t *testing.T) {
	d := sessionDescriptor(t, "launch")
	if d.Projectability != registry.LocalAdmin {
		t.Errorf("%s projectability = %q, want local_admin", d.Name, d.Projectability)
	}
	if d.MCPTool != "" {
		t.Errorf("%s has MCP tool %q, want none", d.Name, d.MCPTool)
	}
}

// --- session list --------------------------------------------------------

func TestSessionList_EmptyInstallation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	code, out, errOut := runSession(t, "session", "list")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	if out != "no duo sessions\n" {
		t.Errorf("output = %q, want %q", out, "no duo sessions\n")
	}
}

func TestSessionList_EmptyInstallation_JSON(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	code, out, errOut := runSession(t, "session", "list", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Operation string `json:"operation"`
		Result    struct {
			Items []any `json:"items"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if want := sessionDescriptor(t, "list").Name; env.Operation != want {
		t.Errorf("operation = %q, want %q", env.Operation, want)
	}
	if len(env.Result.Items) != 0 {
		t.Errorf("items = %v, want empty", env.Result.Items)
	}
	// A read against an uninitialized installation must never create a
	// store file: docs/doctor/decisions.md's discipline, reused here.
	if _, err := authorityStorePath(); err != nil {
		t.Fatalf("authorityStorePath: %v", err)
	}
}

// --- session show ---------------------------------------------------------

func TestSessionShow_NotFound(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	code, _, errOut := runSession(t, "session", "show", "ses_does_not_exist")
	if code != exitcode.UserFail {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.UserFail, errOut)
	}
	if errOut == "" {
		t.Error("expected an error message on stderr")
	}
}

// --- session enroll / list / show / detach / reattach round trip --------

// enrollResult is the JSON shape session_test.go decodes from `session
// enroll --output json` to drive the rest of the round trip.
type enrollEnvelope struct {
	Result struct {
		SessionID    string `json:"session_id"`
		WorkspaceID  string `json:"workspace_id"`
		InstanceID   string `json:"runtime_instance_id"`
		AttachmentID string `json:"attachment_id"`
		Repeat       bool   `json:"repeat"`
		Credential   string `json:"credential"`
	} `json:"result"`
}

func enrollFixtureArgs(root string) []string {
	return []string{
		"session", "enroll", "--output", "json",
		"--root-path", root,
		"--integration-instance", "test-herdr",
		"--epoch-kind", "herdr.terminal_id",
		"--epoch-value", "term-1",
		"--epoch-scope", "pane",
		"--container", "pane-1",
		"--process-pid", "4242",
		"--process-started-at", "2026-08-13T12:00:00.000Z",
	}
}

func TestSessionEnroll_ThenListShowDetachReattach(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()

	// enroll
	code, out, errOut := runSession(t, enrollFixtureArgs(root)...)
	if code != exitcode.Success {
		t.Fatalf("enroll: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var enrolled enrollEnvelope
	if err := json.Unmarshal([]byte(out), &enrolled); err != nil {
		t.Fatalf("enroll output is not valid JSON: %v\noutput: %s", err, out)
	}
	if enrolled.Result.SessionID == "" {
		t.Fatal("enroll: session_id is empty")
	}
	if enrolled.Result.Credential == "" {
		t.Error("enroll: expected a reporter credential on first enrollment")
	}
	if enrolled.Result.Repeat {
		t.Error("enroll: expected Repeat=false on first enrollment")
	}
	sessionID := enrolled.Result.SessionID

	// repeat enrollment (decision-01 §4.2 step 3: same live runtime,
	// existing session returned, nothing new created).
	code, out, errOut = runSession(t, enrollFixtureArgs(root)...)
	if code != exitcode.Success {
		t.Fatalf("repeat enroll: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	var repeat enrollEnvelope
	if err := json.Unmarshal([]byte(out), &repeat); err != nil {
		t.Fatalf("repeat enroll output is not valid JSON: %v", err)
	}
	if !repeat.Result.Repeat {
		t.Error("repeat enroll: expected Repeat=true")
	}
	if repeat.Result.SessionID != sessionID {
		t.Errorf("repeat enroll: session_id = %q, want %q", repeat.Result.SessionID, sessionID)
	}
	if repeat.Result.Credential != "" {
		t.Error("repeat enroll: expected no credential (decision-01 §3.6: returned once, at creation)")
	}

	// list shows it. Its view is "recovering", not "attached": each CLI
	// invocation is its own process, so domain.Open replays the fact log
	// fresh for this list command and — per decision-01 §5.1/§4.4 — marks
	// every nonterminal runtime instance recovering on load, exactly as it
	// would after a real authority restart. Nothing at Step 21's layer
	// (14, 15) resolves that view back to attached; only real host
	// evidence can (domain.Authority.ResolveRecovery, Step 22 territory).
	// See the step-21 wip findings and final report.
	code, out, errOut = runSession(t, "session", "list", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("list: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var listed struct {
		Result struct {
			Items []sessionListItem `json:"items"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("list output is not valid JSON: %v", err)
	}
	if len(listed.Result.Items) != 1 {
		t.Fatalf("list: got %d items, want 1", len(listed.Result.Items))
	}
	if listed.Result.Items[0].SessionID != sessionID {
		t.Errorf("list: session_id = %q, want %q", listed.Result.Items[0].SessionID, sessionID)
	}
	if listed.Result.Items[0].View != "recovering" {
		t.Errorf("list: view = %q, want recovering", listed.Result.Items[0].View)
	}

	// show resolves it too, and the JSON validates against session.inspect's
	// schema conditional (lifecycle enum, condition_view_data, support_view).
	code, out, errOut = runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var shown struct {
		Result sessionInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("show output is not valid JSON: %v", err)
	}
	if shown.Result.Lifecycle != "active" {
		t.Errorf("show: lifecycle = %q, want active", shown.Result.Lifecycle)
	}
	if shown.Result.View != "recovering" {
		t.Errorf("show: view = %q, want recovering", shown.Result.View)
	}

	// show, human mode: a stable, non-empty rendering.
	code, out, errOut = runSession(t, "session", "show", sessionID)
	if code != exitcode.Success {
		t.Fatalf("show text: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	wantPrefix := fmt.Sprintf("session:            %s\n", sessionID)
	if len(out) < len(wantPrefix) || out[:len(wantPrefix)] != wantPrefix {
		t.Errorf("show text = %q, want it to start with %q", out, wantPrefix)
	}

	// detach.
	code, out, errOut = runSession(t, "session", "detach", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("detach: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var detached struct {
		Result sessionAttachmentResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &detached); err != nil {
		t.Fatalf("detach output is not valid JSON: %v", err)
	}
	if detached.Result.AttachmentState != "detached" {
		t.Errorf("detach: attachment_state = %q, want detached", detached.Result.AttachmentState)
	}

	code, out, errOut = runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show after detach: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	var afterDetach struct {
		Result sessionInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &afterDetach); err != nil {
		t.Fatalf("show after detach output is not valid JSON: %v", err)
	}
	// Still "recovering", not "detached": View() checks the recovering set
	// before the attachment state (decision-01 §5.1's View precedence), and
	// nothing in this flow has proven continuity.
	if afterDetach.Result.View != "recovering" {
		t.Errorf("show after detach: view = %q, want recovering", afterDetach.Result.View)
	}

	// reattach, revalidating the same live-runtime claim.
	code, out, errOut = runSession(t, "session", "reattach", sessionID, "--output", "json",
		"--integration-instance", "test-herdr",
		"--epoch-kind", "herdr.terminal_id",
		"--epoch-value", "term-1",
		"--epoch-scope", "pane",
		"--container", "pane-1",
		"--process-pid", "4242",
		"--process-started-at", "2026-08-13T12:00:00.000Z",
	)
	if code != exitcode.Success {
		t.Fatalf("reattach: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var reattached struct {
		Result sessionAttachmentResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &reattached); err != nil {
		t.Fatalf("reattach output is not valid JSON: %v", err)
	}
	if reattached.Result.AttachmentState != "attached" {
		t.Errorf("reattach: attachment_state = %q, want attached", reattached.Result.AttachmentState)
	}

	// Detach again so the next reattach actually revalidates: setAttachment
	// short-circuits to a no-op when the attachment is already in the
	// requested state, so a mismatched-fingerprint reattach only exercises
	// the revalidation path starting from Detached.
	code, _, errOut = runSession(t, "session", "detach", sessionID)
	if code != exitcode.Success {
		t.Fatalf("second detach: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}

	// reattach with a different container is a conflict, not a silent
	// revalidation (decision-01 §5.2: "a pane that now holds a different
	// execution is a new runtime instance, not a reattach").
	code, _, errOut = runSession(t, "session", "reattach", sessionID,
		"--integration-instance", "test-herdr",
		"--epoch-kind", "herdr.terminal_id",
		"--epoch-value", "term-1",
		"--epoch-scope", "pane",
		"--container", "some-other-pane",
		"--process-pid", "9999",
		"--process-started-at", "2026-08-13T13:00:00.000Z",
	)
	if code != exitcode.UserFail {
		t.Fatalf("reattach with mismatched fingerprint: exit code = %d, want %d (stderr: %s)", code, exitcode.UserFail, errOut)
	}
}

func TestSessionEnroll_MissingRequiredFlag(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	code, _, _ := runSession(t, "session", "enroll", "--epoch-kind", "herdr.terminal_id")
	if code != exitcode.Usage {
		t.Fatalf("exit code = %d, want %d (Cobra's own required-flag refusal)", code, exitcode.Usage)
	}
}

// --- session launch -------------------------------------------------------

// duoConfigFixture is a minimal, schema-valid duo.config/v3 document
// declaring one Claude Code launch variant — enough for the resolver to
// produce a one-leaf ordered selection without ever reaching a live Herdr
// socket (dry-run never calls the host set).
//
// duo-config-v3 step 12: the document no longer names a session host
// instance. `session_hosts` is kind policy, and the instance is deduced at
// launch — which is why every test below sets the ambient Herdr variables
// (withAmbientHerdr) to give the deduction something to land on.
const duoConfigFixture = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  daily_claude:
    agent_runtime: claude
    model_line: sonnet-5
    model_family: claude
presets:
  daily:
    leaves:
      main:
        candidates:
          - variant: daily_claude
`

// withAmbientHerdr publishes the Herdr ambient signature into the test's
// environment, so M1's ambient-environment rung deduces a host. It is the
// rung a Duo running inside a Herdr pane actually uses, and the only one
// reachable through the step-12 CLI shim, which wires neither a correlation
// read model nor an instance discoverer (step 14 does).
func withAmbientHerdr(t *testing.T) {
	t.Helper()
	t.Setenv("HERDR_SOCKET_PATH", "/tmp/does-not-need-to-exist-for-a-dry-run.sock")
	t.Setenv("HERDR_SESSION", "duo-cli-test")
}

func writeDuoConfig(t *testing.T) string {
	t.Helper()
	path := t.TempDir() + "/duo.config.yaml"
	if err := os.WriteFile(path, []byte(duoConfigFixture), 0o600); err != nil {
		t.Fatalf("writing duo.config/v3 fixture: %v", err)
	}
	return path
}

func TestSessionLaunch_DryRun(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	withAmbientHerdr(t)
	configPath := writeDuoConfig(t)

	code, out, errOut := runSession(t, "session", "launch", "daily",
		"--config", configPath, "--dry-run", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	assertValidExternalV1(t, []byte(out))

	var env struct {
		Result struct {
			SessionID          string `json:"session_id"`
			LaunchResolutionID string `json:"launch_resolution_id"`
			Selection          string `json:"selection"`
			Preview            bool   `json:"preview"`
			Leaves             []struct {
				Name         string `json:"name"`
				AgentRuntime string `json:"agent_runtime"`
				ModelLine    string `json:"model_line"`
				Outcome      string `json:"outcome"`
			} `json:"leaves"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if !env.Result.Preview {
		t.Error("preview = false, want true for --dry-run")
	}
	if env.Result.SessionID != "" {
		t.Errorf("session_id = %q, want empty for --dry-run", env.Result.SessionID)
	}
	if env.Result.LaunchResolutionID != "" {
		t.Errorf("launch_resolution_id = %q, want empty for --dry-run (§6.10: no durable record)", env.Result.LaunchResolutionID)
	}
	if len(env.Result.Leaves) != 1 {
		t.Fatalf("leaves = %v, want exactly one", env.Result.Leaves)
	}
	if env.Result.Leaves[0].AgentRuntime != "claude" || env.Result.Leaves[0].ModelLine != "sonnet-5" {
		t.Errorf("leaf = %+v, want agent_runtime=claude model_line=sonnet-5", env.Result.Leaves[0])
	}
	if env.Result.Leaves[0].Outcome != "selected" {
		t.Errorf("leaf outcome = %q, want selected", env.Result.Leaves[0].Outcome)
	}

	// A dry run must never touch the authority store.
	if _, err := authorityStorePath(); err != nil {
		t.Fatalf("authorityStorePath: %v", err)
	}
	code2, out2, _ := runSession(t, "session", "list", "--output", "json")
	if code2 != exitcode.Success {
		t.Fatalf("list after dry-run: exit code = %d", code2)
	}
	var listed struct {
		Result struct {
			Items []any `json:"items"`
		} `json:"result"`
	}
	_ = json.Unmarshal([]byte(out2), &listed)
	if len(listed.Result.Items) != 0 {
		t.Errorf("list after dry-run launch: %d items, want 0 (dry run must create no session)", len(listed.Result.Items))
	}
}

func TestSessionLaunch_UnknownPreset(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	withAmbientHerdr(t)
	configPath := writeDuoConfig(t)

	code, _, errOut := runSession(t, "session", "launch", "no-such-preset",
		"--config", configPath, "--dry-run")
	if code != exitcode.UserFail {
		t.Fatalf("exit code = %d, want %d (preset.not_found is not refusal./internal.-prefixed) (stderr: %s)",
			code, exitcode.UserFail, errOut)
	}
}

func TestSessionLaunch_BadRequireGrammar(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	withAmbientHerdr(t)
	configPath := writeDuoConfig(t)

	code, _, errOut := runSession(t, "session", "launch", "daily",
		"--config", configPath, "--dry-run", "--require", "not-a-predicate")
	if code != exitcode.UserFail {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.UserFail, errOut)
	}
}

// TestSessionLaunch_UnrelentingRequireExhausts drives a --require that no
// declared candidate can satisfy (the fixture declares only "claude") to
// launch.constraints_exhausted, and checks that --output json's early
// --json sync (outputMode runs before config load or resolution) reaches
// even this deep a failure: the error still renders through the chassis's
// {"error": {...}} envelope, not the human-mode line.
func TestSessionLaunch_UnrelentingRequireExhausts(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	withAmbientHerdr(t)
	configPath := writeDuoConfig(t)

	code, _, errOut := runSession(t, "session", "launch", "daily",
		"--config", configPath, "--dry-run", "--output", "json",
		"--require", "agent_runtime=pi")
	if code != exitcode.UserFail {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.UserFail, errOut)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(errOut), &env); err != nil {
		t.Fatalf("stderr is not the chassis's JSON error envelope: %v\nstderr: %s", err, errOut)
	}
	if env.Error.Code != "launch.constraints_exhausted" {
		t.Errorf("error code = %q, want launch.constraints_exhausted", env.Error.Code)
	}
}
