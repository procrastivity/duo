package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/host"
)

// installReconcileHosts wires reconcileValidatorFor to the fake hosts used
// during launch so unit tests never dial a live Herdr socket.
func installReconcileHosts(t *testing.T, hosts *recordingHosts) {
	t.Helper()
	orig := reconcileValidatorFor
	t.Cleanup(func() { reconcileValidatorFor = orig })
	reconcileValidatorFor = func(integrationInstance string) (host.HostAttachmentValidator, error) {
		if h, ok := hosts.hosts[integrationInstance]; ok {
			return h, nil
		}
		return nil, host.Unreachable(nil)
	}
}

func hostAttachmentOf(e host.Evidence) host.Attachment {
	return host.Attachment{
		IntegrationInstanceID: e.IntegrationInstanceID,
		PaneID:                e.PaneID,
		HostContainerID:       e.HostContainerID,
	}
}

func launchRecovering(t *testing.T) (*bindHarness, *recordingHosts, string, string) {
	t.Helper()
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)
	hosts := newRecordingHosts()
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(hosts.seen) != 1 {
		t.Fatalf("spawned %d leaves, want 1", len(hosts.seen))
	}
	session, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok || session.Current == "" {
		t.Fatalf("launched session %s has no current instance", report.SessionID)
	}
	return h, hosts, report.SessionID, string(session.Current)
}

func parseReconcileJSON(t *testing.T, out string) sessionReconcileResult {
	t.Helper()
	assertValidExternalV1(t, []byte(out))
	var env struct {
		Operation string                 `json:"operation"`
		Result    sessionReconcileResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("reconcile JSON: %v\n%s", err, out)
	}
	if env.Operation != "session.reconcile" {
		t.Fatalf("operation = %q, want session.reconcile", env.Operation)
	}
	return env.Result
}

func showView(t *testing.T, sessionID string) (view, lifecycle string) {
	t.Helper()
	code, out, errOut := runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show: exit %d (stderr: %s)", code, errOut)
	}
	var env struct {
		Result sessionInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("show JSON: %v", err)
	}
	return env.Result.View, env.Result.Lifecycle
}

// TestReconcilePaneAbsentExits reports exited when the fake host Kill'd the
// pane, and the session view becomes inactive while lifecycle stays active.
func TestReconcilePaneAbsentExits(t *testing.T) {
	h, hosts, sessionID, instanceID := launchRecovering(t)
	hosts.hosts[hosts.seen[0].IntegrationInstanceID].Kill(hostAttachmentOf(hosts.seen[0]))
	installReconcileHosts(t, hosts)
	h.close()

	code, out, errOut := runSession(t, "session", "reconcile", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	it := result.Items[0]
	if it.Outcome != string(domain.RecoveryExited) {
		t.Errorf("outcome = %q, want exited", it.Outcome)
	}
	if it.SessionID != sessionID || it.InstanceID != instanceID {
		t.Errorf("ids = %s %s, want %s %s", it.SessionID, it.InstanceID, sessionID, instanceID)
	}

	view, lifecycle := showView(t, sessionID)
	if view != string(domain.ViewInactive) {
		t.Errorf("view = %q, want inactive", view)
	}
	if lifecycle != "active" {
		t.Errorf("lifecycle = %q, want active (inactive is view-only)", lifecycle)
	}
}

// TestReconcileReplaceProcessReportsReplaced covers ContinuityProcessReplaced.
func TestReconcileReplaceProcessReportsReplaced(t *testing.T) {
	h, hosts, sessionID, _ := launchRecovering(t)
	fake := hosts.hosts[hosts.seen[0].IntegrationInstanceID]
	if live := fake.ReplaceProcess(hostAttachmentOf(hosts.seen[0])); live.ProcessBirth.PID == 0 {
		t.Fatal("ReplaceProcess returned empty evidence")
	}
	installReconcileHosts(t, hosts)
	h.close()

	code, out, errOut := runSession(t, "session", "reconcile", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 || result.Items[0].Outcome != string(domain.RecoveryReplaced) {
		t.Fatalf("result = %+v, want replaced", result.Items)
	}
	view, _ := showView(t, sessionID)
	if view != string(domain.ViewInactive) {
		t.Errorf("view = %q, want inactive after replaced", view)
	}
}

// TestReconcileReplaceTerminalReportsReplaced covers ContinuityTerminalReplaced.
func TestReconcileReplaceTerminalReportsReplaced(t *testing.T) {
	h, hosts, sessionID, _ := launchRecovering(t)
	fake := hosts.hosts[hosts.seen[0].IntegrationInstanceID]
	if live := fake.ReplaceTerminal(hostAttachmentOf(hosts.seen[0])); live.HostContainerID == "" {
		t.Fatal("ReplaceTerminal returned empty evidence")
	}
	installReconcileHosts(t, hosts)
	h.close()

	code, out, errOut := runSession(t, "session", "reconcile", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 || result.Items[0].Outcome != string(domain.RecoveryReplaced) {
		t.Fatalf("result = %+v, want replaced", result.Items)
	}
}

// TestReconcileSameLiveKeepsContinuity proves same pane + same birth →
// same-live. View leaves recovering only inside the writer that called
// ResolveRecovery; the next duo process Open remarks every live instance
// recovering (decision-01 §4.4), so a subsequent show may still say
// recovering — that is not a reconcile failure.
func TestReconcileSameLiveKeepsContinuity(t *testing.T) {
	h, hosts, sessionID, instanceID := launchRecovering(t)
	installReconcileHosts(t, hosts)
	h.close()

	code, out, errOut := runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show before: exit %d (stderr: %s)", code, errOut)
	}
	var before struct {
		Result sessionInspectResult `json:"result"`
	}
	_ = json.Unmarshal([]byte(out), &before)
	if before.Result.View != string(domain.ViewRecovering) {
		t.Fatalf("view before reconcile = %q, want recovering", before.Result.View)
	}

	code, out, errOut = runSession(t, "session", "reconcile", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}
	if result.Items[0].Outcome != string(domain.RecoverySameLive) {
		t.Errorf("outcome = %q, want same-live", result.Items[0].Outcome)
	}
	if result.Items[0].InstanceID != instanceID {
		t.Errorf("instance = %q, want %q", result.Items[0].InstanceID, instanceID)
	}

	// Instance identity is kept: show still names the same runtime instance.
	view, lifecycle := showView(t, sessionID)
	if lifecycle != "active" {
		t.Errorf("lifecycle = %q, want active", lifecycle)
	}
	code, out, errOut = runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show after: exit %d (stderr: %s)", code, errOut)
	}
	var after struct {
		Result sessionInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &after); err != nil {
		t.Fatalf("show after JSON: %v", err)
	}
	if after.Result.RuntimeInstanceID != instanceID {
		t.Errorf("runtime_instance_id = %q, want preserved %q", after.Result.RuntimeInstanceID, instanceID)
	}
	_ = view
}

// TestReconcileDisconnectIsUnreachable keeps lifecycle/instance state when
// the host cannot be reached.
func TestReconcileDisconnectIsUnreachable(t *testing.T) {
	h, hosts, sessionID, instanceID := launchRecovering(t)
	hosts.hosts[hosts.seen[0].IntegrationInstanceID].Disconnect()
	installReconcileHosts(t, hosts)
	h.close()

	code, out, errOut := runSession(t, "session", "reconcile", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 || result.Items[0].Outcome != string(domain.RecoveryUnreachable) {
		t.Fatalf("result = %+v, want unreachable", result.Items)
	}

	view, lifecycle := showView(t, sessionID)
	if view != string(domain.ViewRecovering) {
		t.Errorf("view = %q, want recovering (unreachable must not resolve)", view)
	}
	if lifecycle != "active" {
		t.Errorf("lifecycle = %q, want active", lifecycle)
	}

	// Instance must still be in Recovering on a fresh open — reopen via list
	// recovering hint / doctor count covered elsewhere; here re-reconcile
	// without IDs still finds it.
	code, out, errOut = runSession(t, "session", "reconcile", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("second reconcile: exit %d (stderr: %s)", code, errOut)
	}
	again := parseReconcileJSON(t, out)
	if len(again.Items) != 1 || again.Items[0].InstanceID != instanceID {
		t.Fatalf("second reconcile = %+v, want still-recovering %s", again.Items, instanceID)
	}
}

// TestReconcileMissingValidatorIsUnreachable covers a validator that cannot
// be resolved for the attachment's integration instance.
func TestReconcileMissingValidatorIsUnreachable(t *testing.T) {
	h, hosts, sessionID, _ := launchRecovering(t)
	orig := reconcileValidatorFor
	t.Cleanup(func() { reconcileValidatorFor = orig })
	reconcileValidatorFor = func(string) (host.HostAttachmentValidator, error) {
		return nil, nil
	}
	_ = hosts
	h.close()

	code, out, errOut := runSession(t, "session", "reconcile", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 || result.Items[0].Outcome != string(domain.RecoveryUnreachable) {
		t.Fatalf("result = %+v, want unreachable", result.Items)
	}
	view, _ := showView(t, sessionID)
	if view != string(domain.ViewRecovering) {
		t.Errorf("view = %q, want recovering", view)
	}
}

// TestReconcileMixedMultiLeafIsReplaced: Kill one leaf of a two-leaf launch,
// leave the other → Replaced, not SameLive.
func TestReconcileMixedMultiLeafIsReplaced(t *testing.T) {
	h := newBindHarness(t, nil)
	doc, err := config.ParseV3([]byte(twoLeafScenarioYAML))
	if err != nil {
		t.Fatalf("ParseV3: %v", err)
	}
	h.doc = doc
	mat := h.materializeWith("herdr:"+bindSocket, nil)
	hosts := newRecordingHosts()
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(hosts.seen) != 2 {
		t.Fatalf("spawned %d leaves, want 2", len(hosts.seen))
	}
	// Both leaves share one fake Host (same integration instance).
	fake := hosts.hosts[hosts.seen[0].IntegrationInstanceID]
	fake.Kill(hostAttachmentOf(hosts.seen[0]))
	installReconcileHosts(t, hosts)
	h.close()

	code, out, errOut := runSession(t, "session", "reconcile", report.SessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1 (one ResolveRecovery per instance)", len(result.Items))
	}
	if result.Items[0].Outcome != string(domain.RecoveryReplaced) {
		t.Errorf("outcome = %q, want replaced (mixed exited+same-live)", result.Items[0].Outcome)
	}
}

// TestReconcileConcurrentWriterRefused: holding openWriteAuthority blocks
// a concurrent reconcile with refusal.authority_writer_active.
func TestReconcileConcurrentWriterRefused(t *testing.T) {
	h, hosts, sessionID, _ := launchRecovering(t)
	installReconcileHosts(t, hosts)
	// Deliberately do not close — the harness still holds the writer lease.
	if h.authority == nil {
		t.Fatal("harness lost its authority")
	}

	code, _, errOut := runSession(t, "session", "reconcile", sessionID)
	if code != exitcode.Refusal {
		t.Fatalf("exit = %d, want refusal %d (stderr: %s)", code, exitcode.Refusal, errOut)
	}
	if !strings.Contains(errOut, "authority_writer_active") && !strings.Contains(errOut, "authority-writer") {
		t.Errorf("stderr missing writer-active refusal:\n%s", errOut)
	}
}

// TestSessionListHintWhenRecovering prints the reconcile hint in text mode.
func TestSessionListHintWhenRecovering(t *testing.T) {
	h, _, sessionID, _ := launchRecovering(t)
	_ = sessionID
	h.close()

	code, out, errOut := runSession(t, "session", "list")
	if code != exitcode.Success {
		t.Fatalf("list: exit %d (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, "await reconciliation") || !strings.Contains(out, "duo session reconcile") {
		t.Errorf("list text missing reconcile hint:\n%s", out)
	}

	// JSON item shape unchanged — no recovering field required on items.
	code, out, errOut = runSession(t, "session", "list", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("list json: exit %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	if strings.Contains(out, "await reconciliation") {
		t.Error("JSON list must not embed the text hint string")
	}
}

// TestDoctorReportsRecoveringInstancesCount is additive on doctorReport.
func TestDoctorReportsRecoveringInstancesCount(t *testing.T) {
	h, _, _, _ := launchRecovering(t)
	root := h.root
	h.close()

	code, out, errOut := runSession(t, "doctor", "--workspace", root, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("doctor json: exit %d (stderr: %s)", code, errOut)
	}
	var report struct {
		RecoveringInstances int `json:"recovering_instances"`
	}
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("doctor JSON: %v\n%s", err, out)
	}
	if report.RecoveringInstances < 1 {
		t.Errorf("recovering_instances = %d, want >= 1", report.RecoveringInstances)
	}

	code, out, errOut = runSession(t, "doctor", "--workspace", root)
	if code != exitcode.Success {
		t.Fatalf("doctor text: exit %d (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, "recovering:") || !strings.Contains(out, "duo session reconcile") {
		t.Errorf("doctor text missing recovering line:\n%s", out)
	}
}

// TestReconcileUnknownSessionIsNotFound follows other session verbs.
func TestReconcileUnknownSessionIsNotFound(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	code, _, errOut := runSession(t, "session", "reconcile", "ses_does_not_exist")
	if code == exitcode.Success {
		t.Fatal("reconcile succeeded for an unknown session")
	}
	if !strings.Contains(errOut, "not") && !strings.Contains(errOut, "known") {
		t.Errorf("stderr = %q, want object.not_found style message", errOut)
	}
}

// TestReconcileTextModeFormatsOneLinePerInstance covers the human render.
func TestReconcileTextModeFormatsOneLinePerInstance(t *testing.T) {
	h, hosts, sessionID, instanceID := launchRecovering(t)
	hosts.hosts[hosts.seen[0].IntegrationInstanceID].Kill(hostAttachmentOf(hosts.seen[0]))
	installReconcileHosts(t, hosts)
	h.close()

	code, out, errOut := runSession(t, "session", "reconcile", sessionID)
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	if !strings.Contains(out, sessionID) || !strings.Contains(out, instanceID) || !strings.Contains(out, "exited") {
		t.Errorf("text = %q, want session, instance, exited", out)
	}
	if !strings.Contains(out, "->") {
		t.Errorf("text missing arrow form: %q", out)
	}
}

// TestHerdrSocketForIntegrationAcceptsLocatorPaths is the live-gate
// finding: launch --host herdr:<socket> records that locator as the
// integration instance. SessionForInstanceID rejects a remainder with a
// path separator, so reconcile must use the path itself when the file
// exists, and Unreachable when it does not.
func TestHerdrSocketForIntegrationAcceptsLocatorPaths(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	socket := filepath.Join(dir, "herdr.sock")
	if err := os.WriteFile(socket, []byte{}, 0o600); err != nil {
		t.Fatalf("write socket file: %v", err)
	}
	id := "herdr:" + socket
	got, err := herdrSocketForIntegration(id)
	if err != nil {
		t.Fatalf("existing socket path: %v", err)
	}
	if got != socket {
		t.Errorf("socket = %q, want %q", got, socket)
	}

	missing := "herdr:" + filepath.Join(dir, "missing", "herdr.sock")
	_, err = herdrSocketForIntegration(missing)
	if !errors.Is(err, host.ErrUnreachable) {
		t.Errorf("missing socket path error = %v, want ErrUnreachable", err)
	}

	_, err = herdrSocketForIntegration("herdr:not-a-session-or-path")
	if !errors.Is(err, host.ErrUnreachable) {
		t.Errorf("bare name without SessionsDir layout error = %v, want ErrUnreachable", err)
	}
}
