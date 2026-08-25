package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
)

func reconcileExited(t *testing.T) (sessionID string) {
	t.Helper()
	h, hosts, sessionID, _ := launchRecovering(t)
	hosts.hosts[hosts.seen[0].IntegrationInstanceID].Kill(hostAttachmentOf(hosts.seen[0]))
	installReconcileHosts(t, hosts)
	h.close()

	code, out, errOut := runSession(t, "session", "reconcile", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("reconcile: exit %d (stderr: %s)", code, errOut)
	}
	result := parseReconcileJSON(t, out)
	if len(result.Items) != 1 || result.Items[0].Outcome != string(domain.RecoveryExited) {
		t.Fatalf("reconcile result = %+v, want one exited item", result.Items)
	}
	view, lifecycle := showView(t, sessionID)
	if view != string(domain.ViewInactive) {
		t.Errorf("view = %q, want inactive", view)
	}
	if lifecycle != "active" {
		t.Errorf("lifecycle = %q, want active (inactive is view-only)", lifecycle)
	}
	return sessionID
}

func listSessionIDs(t *testing.T) []string {
	t.Helper()
	code, out, errOut := runSession(t, "session", "list", "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("list: exit %d (stderr: %s)", code, errOut)
	}
	var env struct {
		Result struct {
			Items []sessionListItem `json:"items"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("list JSON: %v\n%s", err, out)
	}
	ids := make([]string, len(env.Result.Items))
	for i, it := range env.Result.Items {
		ids[i] = it.SessionID
	}
	return ids
}

func assertSessionGuardRefusal(t *testing.T, code int, errOut, wantLadder string) {
	t.Helper()
	if code != exitcode.Refusal {
		t.Fatalf("exit = %d, want refusal %d (stderr: %s)", code, exitcode.Refusal, errOut)
	}
	if !strings.Contains(errOut, "refusal.session_guard") && !strings.Contains(errOut, wantLadder) {
		t.Errorf("stderr missing session guard or ladder %q:\n%s", wantLadder, errOut)
	}
	if wantLadder != "" && !strings.Contains(errOut, wantLadder) {
		t.Errorf("stderr missing transition ladder %q:\n%s", wantLadder, errOut)
	}
}

// TestArchiveRemoveLifecycleLadder is the enforced §5.2 path: reconcile to
// inactive, archive, remove; list omits the tombstone while show still
// answers.
func TestArchiveRemoveLifecycleLadder(t *testing.T) {
	sessionID := reconcileExited(t)

	code, out, errOut := runSession(t, "session", "archive", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("archive: exit %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var archived struct {
		Operation string                 `json:"operation"`
		Result    sessionLifecycleResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &archived); err != nil {
		t.Fatalf("archive JSON: %v\n%s", err, out)
	}
	if archived.Operation != "session.archive" {
		t.Errorf("operation = %q, want session.archive", archived.Operation)
	}
	if archived.Result.Lifecycle != "archived" {
		t.Errorf("lifecycle = %q, want archived", archived.Result.Lifecycle)
	}
	view, lifecycle := showView(t, sessionID)
	if view != string(domain.ViewArchived) {
		t.Errorf("view after archive = %q, want archived", view)
	}
	if lifecycle != "archived" {
		t.Errorf("lifecycle after archive = %q, want archived", lifecycle)
	}

	code, out, errOut = runSession(t, "session", "remove", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("remove: exit %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var removed struct {
		Operation string                 `json:"operation"`
		Result    sessionLifecycleResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &removed); err != nil {
		t.Fatalf("remove JSON: %v\n%s", err, out)
	}
	if removed.Operation != "session.remove" {
		t.Errorf("operation = %q, want session.remove", removed.Operation)
	}
	if removed.Result.Lifecycle != "removed" {
		t.Errorf("lifecycle = %q, want removed", removed.Result.Lifecycle)
	}

	for _, id := range listSessionIDs(t) {
		if id == sessionID {
			t.Errorf("list still includes removed session %s", sessionID)
		}
	}

	code, out, errOut = runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show removed tombstone: exit %d (stderr: %s)", code, errOut)
	}
	var shown struct {
		Result sessionInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("show JSON: %v\n%s", err, out)
	}
	if shown.Result.Lifecycle != "removed" {
		t.Errorf("show lifecycle = %q, want removed", shown.Result.Lifecycle)
	}
	if shown.Result.View != string(domain.ViewRemoved) {
		t.Errorf("show view = %q, want removed", shown.Result.View)
	}
}

// TestArchiveRefusesActiveSession refuses archive on a launched session
// that has not been reconciled to inactive.
func TestArchiveRefusesActiveSession(t *testing.T) {
	h, _, sessionID, _ := launchRecovering(t)
	_ = h
	h.close()

	code, _, errOut := runSession(t, "session", "archive", sessionID)
	assertSessionGuardRefusal(t, code, errOut, "active -> archived")
}

// TestRemoveRefusesNonArchivedSession refuses remove before archive.
func TestRemoveRefusesNonArchivedSession(t *testing.T) {
	sessionID := reconcileExited(t)

	code, _, errOut := runSession(t, "session", "remove", sessionID)
	assertSessionGuardRefusal(t, code, errOut, "inactive -> removed")
}

// TestArchiveTwiceRefusesSecondCall: domain Archive is not idempotent from
// archived — the second call is an illegal transition.
func TestArchiveTwiceRefusesSecondCall(t *testing.T) {
	sessionID := reconcileExited(t)

	code, _, errOut := runSession(t, "session", "archive", sessionID)
	if code != exitcode.Success {
		t.Fatalf("first archive: exit %d (stderr: %s)", code, errOut)
	}

	code, _, errOut = runSession(t, "session", "archive", sessionID)
	assertSessionGuardRefusal(t, code, errOut, "archived -> archived")
}

// TestArchiveRemoveTextOutput checks the stable one-line text renderings.
func TestArchiveRemoveTextOutput(t *testing.T) {
	sessionID := reconcileExited(t)

	code, out, errOut := runSession(t, "session", "archive", sessionID)
	if code != exitcode.Success {
		t.Fatalf("archive text: exit %d (stderr: %s)", code, errOut)
	}
	want := "session " + sessionID + ": archived\n"
	if out != want {
		t.Errorf("archive text = %q, want %q", out, want)
	}

	code, out, errOut = runSession(t, "session", "remove", sessionID)
	if code != exitcode.Success {
		t.Fatalf("remove text: exit %d (stderr: %s)", code, errOut)
	}
	want = "session " + sessionID + ": removed\n"
	if out != want {
		t.Errorf("remove text = %q, want %q", out, want)
	}
}
