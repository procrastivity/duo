package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// --- the evidence bridge ---------------------------------------------------

// TestLiveRuntimeFingerprintCrossesTheContainerAndTheEpoch pins the one
// thing this bridge can get wrong silently: which half of the host evidence
// is the container and which is the incarnation. The Herdr adapter's
// HostContainerID is terminal_id — the epoch-equivalent — and its PaneID is
// the stable coordinate, which is the opposite of what the two field names
// suggest. Getting it backwards would still validate, still claim, and
// still refuse every reattach an operator typed from `duo session enroll`'s
// documented flag order.
func TestLiveRuntimeFingerprintCrossesTheContainerAndTheEpoch(t *testing.T) {
	started := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	evidence := host.Evidence{
		IntegrationInstanceID: herdr.InstanceIDForSession(bindSession),
		HostServerEpoch:       herdr.NoServerEpoch,
		HostContainerID:       "term_7",
		PaneID:                "pane_3",
		ProcessBirth:          host.ProcessBirthEvidence{PID: 4242, StartTime: started},
	}

	fp := liveRuntimeFingerprint(herdr.AdapterID, evidence)
	if err := fp.Validate(); err != nil {
		t.Fatalf("the fingerprint a real spawn produced does not validate: %v", err)
	}
	if fp.IntegrationInstance != evidence.IntegrationInstanceID {
		t.Errorf("integration instance = %q, want %q", fp.IntegrationInstance, evidence.IntegrationInstanceID)
	}
	if fp.Epoch.Kind != herdrEpochKind {
		t.Errorf("epoch kind = %q, want %q (the string `duo session reattach --epoch-kind` takes)", fp.Epoch.Kind, herdrEpochKind)
	}
	if fp.Epoch.Value != "term_7" {
		t.Errorf("epoch value = %q, want the terminal_id term_7", fp.Epoch.Value)
	}
	if fp.Epoch.Scope != domain.EpochScopePane {
		t.Errorf("epoch scope = %q, want %q (herdr has no server-scoped epoch)", fp.Epoch.Scope, domain.EpochScopePane)
	}
	if fp.Container != "pane_3" {
		t.Errorf("container = %q, want the pane_id pane_3", fp.Container)
	}
	if fp.Process.PID != 4242 || fp.Process.StartedAt != started.Format(materialize.CaptureTimeLayout) {
		t.Errorf("process birth = %+v, want pid 4242 at %s", fp.Process, started.Format(materialize.CaptureTimeLayout))
	}
	if fp.Degraded() {
		t.Error("a spawn that reported process birth produced a degraded fingerprint")
	}
}

// TestLiveRuntimeFingerprintRefusesAnUnknownHostKind is the other half of
// the same rule: a host whose incarnation evidence nobody has probed gets
// no invented epoch kind, and a fingerprint nothing can validate is
// refused rather than recorded.
func TestLiveRuntimeFingerprintRefusesAnUnknownHostKind(t *testing.T) {
	fp := liveRuntimeFingerprint("tmux", host.Evidence{
		IntegrationInstanceID: "tmux:default",
		HostServerEpoch:       "epoch-1",
		HostContainerID:       "%7",
		PaneID:                "%7",
	})
	if err := fp.Validate(); err == nil {
		t.Fatalf("an unknown host kind produced a claimable fingerprint: %+v", fp)
	}
}

// TestLiveRuntimeFingerprintOmitsAHalfProcessBirth pins §3.6's floor: a PID
// with no start time is not process-birth evidence, so it is left out
// entirely rather than recorded as half a tuple.
func TestLiveRuntimeFingerprintOmitsAHalfProcessBirth(t *testing.T) {
	fp := liveRuntimeFingerprint(herdr.AdapterID, host.Evidence{
		IntegrationInstanceID: herdr.InstanceIDForSession(bindSession),
		HostContainerID:       "term_7",
		PaneID:                "pane_3",
		ProcessBirth:          host.ProcessBirthEvidence{PID: 4242, StartTimeSource: herdr.StartTimeSourceUnavailable},
	})
	if fp.Process.Present() {
		t.Errorf("process birth = %+v, want it absent when no start time was proven", fp.Process)
	}
	if err := fp.Validate(); err != nil {
		t.Fatalf("a claimable but degraded fingerprint was refused: %v", err)
	}
	if !fp.Degraded() {
		t.Error("a fingerprint with no process birth does not report itself degraded")
	}
}

// --- host-set doubles that report what they spawned ------------------------

// recordingHosts is spawningHosts plus a tap on Start: every leaf's spawn
// evidence lands in seen, in spawn order, so a test can build the reattach
// flags an operator would type from the same values duo recorded.
//
// It keeps one fake host per integration instance rather than building one
// per leaf, because the fake mints its pane and PID from its own counter:
// two fresh fakes would hand two leaves of one launch the same pane, which
// no real session host does.
type recordingHosts struct {
	hosts map[string]*hostfake.Host
	seen  []host.Evidence
}

func newRecordingHosts() *recordingHosts {
	return &recordingHosts{hosts: map[string]*hostfake.Host{}}
}

func (h *recordingHosts) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	fake, ok := h.hosts[t.IntegrationInstanceID]
	if !ok {
		fake = hostfake.New(t.IntegrationInstanceID)
		h.hosts[t.IntegrationInstanceID] = fake
	}
	return &recordingLauncher{Host: fake, hosts: h}, nil
}

type recordingLauncher struct {
	*hostfake.Host
	hosts *recordingHosts
}

func (l *recordingLauncher) Start(ctx context.Context, prepared host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	evidence, err := l.Host.Start(ctx, prepared)
	if err == nil {
		l.hosts.seen = append(l.hosts.seen, evidence.Evidence)
	}
	return evidence, err
}

// reattachArgsFromPrintedCommand splits a show-projected `duo session reattach …`
// line into argv after the leading "duo" token. strings.Fields is safe here
// because reattach flag values never contain spaces.
func reattachArgsFromPrintedCommand(t *testing.T, printed string) []string {
	t.Helper()
	fields := strings.Fields(printed)
	if len(fields) < 2 || fields[0] != "duo" {
		t.Fatalf("reattach_command = %q, want it to start with duo", printed)
	}
	return fields[1:]
}

// attachmentOf returns the host attachment a session currently holds.
func attachmentOf(t *testing.T, a *domain.Authority, id string) (domain.HostAttachment, bool) {
	t.Helper()
	session, ok := a.Session(domain.SessionID(id))
	if !ok {
		t.Fatalf("no session %s", id)
	}
	if session.Attachment == "" {
		return domain.HostAttachment{}, false
	}
	return a.Attachment(session.Attachment)
}

// --- the launch-path attachment -------------------------------------------

// TestLaunchRecordsTheHostAttachmentFromItsOwnSpawn is the fix's core:
// duo opened the pane, so duo records the attachment. Before this, only
// `duo session enroll` wrote one, and every launched session answered
// detach and reattach with "session <id> has no host attachment".
func TestLaunchRecordsTheHostAttachmentFromItsOwnSpawn(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	hosts := newRecordingHosts()
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(hosts.seen) != 1 {
		t.Fatalf("the launch spawned %d leaves, want 1", len(hosts.seen))
	}

	attachment, ok := attachmentOf(t, h.authority, report.SessionID)
	if !ok {
		t.Fatalf("the launched session holds no host attachment:\n%s", h.err.String())
	}
	if attachment.State != domain.Attached {
		t.Errorf("attachment state = %q, want %q (duo spawned the pane, so it observes it)", attachment.State, domain.Attached)
	}
	if attachment.Epoch.Value != hosts.seen[0].HostContainerID {
		t.Errorf("epoch value = %q, want the spawn's own %q", attachment.Epoch.Value, hosts.seen[0].HostContainerID)
	}
	if attachment.Container != hosts.seen[0].PaneID {
		t.Errorf("container = %q, want the spawn's own %q", attachment.Container, hosts.seen[0].PaneID)
	}
	if attachment.IntegrationInstance != hosts.seen[0].IntegrationInstanceID {
		t.Errorf("integration instance = %q, want %q", attachment.IntegrationInstance, hosts.seen[0].IntegrationInstanceID)
	}

	// The claim is what makes reattach possible at all: reattach revalidates
	// the fingerprint against the claim the session already holds.
	claim, held := h.authority.ActiveClaim(liveRuntimeFingerprint(herdr.AdapterID, hosts.seen[0]).ClaimRef())
	if !held {
		t.Fatal("the launch recorded an attachment but seized no live-runtime claim")
	}
	if string(claim.Session) != report.SessionID {
		t.Errorf("the live-runtime claim is held by %s, want the launched session %s", claim.Session, report.SessionID)
	}
}

// TestShowPrintsReattachCommandAfterLaunch is the step-05 smoke: after a
// launched bind, session show JSON carries attachments[] with a pasteable
// reattach_command built from the stored fingerprint — no manual flag
// discovery. Full detach→printed-command reattach acceptance is step 06.
func TestShowPrintsReattachCommandAfterLaunch(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	hosts := newRecordingHosts()
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(hosts.seen) != 1 {
		t.Fatalf("the launch spawned %d leaves, want 1", len(hosts.seen))
	}
	sessionID := report.SessionID
	evidence := hosts.seen[0]
	wantStarted := evidence.ProcessBirth.StartTime.UTC().Format(materialize.CaptureTimeLayout)
	wantCommand := reattachCommand(sessionID, liveRuntimeFingerprint(herdr.AdapterID, evidence))
	h.close()

	code, out, errOut := runSession(t, "session", "show", sessionID, "--output", "json")
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
	if len(shown.Result.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(shown.Result.Attachments))
	}
	att := shown.Result.Attachments[0]
	if !att.ClaimHeld {
		t.Error("claim_held = false, want true for the launched session's own claim")
	}
	if att.ProcessBirth == nil {
		t.Fatal("process_birth omitted, want the spawn's PID and start time")
	}
	if att.ProcessBirth.PID != evidence.ProcessBirth.PID || att.ProcessBirth.StartedAt != wantStarted {
		t.Errorf("process_birth = %+v, want pid %d started %s",
			att.ProcessBirth, evidence.ProcessBirth.PID, wantStarted)
	}
	if att.ReattachCommand != wantCommand {
		t.Errorf("reattach_command = %q\nwant %q", att.ReattachCommand, wantCommand)
	}
	if strings.Contains(att.ReattachCommand, "--process-host") ||
		strings.Contains(att.ReattachCommand, "--process-executable") {
		t.Errorf("reattach_command must not emit host/executable flags: %q", att.ReattachCommand)
	}
	if !strings.Contains(att.ReattachCommand, "--process-pid") ||
		!strings.Contains(att.ReattachCommand, "--process-started-at") {
		t.Errorf("reattach_command missing process flags: %q", att.ReattachCommand)
	}

	code, out, errOut = runSession(t, "session", "show", sessionID)
	if code != exitcode.Success {
		t.Fatalf("show text: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	wantLine := "reattach with: " + wantCommand + "\n"
	if !strings.Contains(out, wantLine) {
		t.Errorf("show text missing one-line reattach with:\n%s", out)
	}
	if strings.Count(out, "reattach with:") != 1 {
		t.Errorf("want exactly one reattach with: line, got:\n%s", out)
	}
}

// TestNoAttachmentIsRecordedWhenStartFails is the ordering, one object down
// from TestBindIsWrittenOnlyAfterStartSucceeds: an attachment attests where
// a *running* execution is, so a spawn that never started records none —
// even though the session and the launch-resolution record are already
// durable (invariant I-1, §7.4).
func TestNoAttachmentIsRecordedWhenStartFails(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	if _, err := h.launch(mat, spawningHosts{failStart: true}, false); err == nil {
		t.Fatal("launch succeeded, want the host's Start refusal")
	}
	for _, session := range h.authority.Sessions() {
		if session.Attachment != "" {
			t.Fatalf("session %s holds attachment %s after a failed Start", session.ID, session.Attachment)
		}
	}
}

// TestADryRunRecordsNoAttachment keeps §6.10 whole: a preview spawns
// nothing, so there is nothing to attach to and nothing to record.
func TestADryRunRecordsNoAttachment(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	if _, err := h.launch(mat, spawningHosts{}, true); err != nil {
		t.Fatalf("launch --dry-run: %v", err)
	}
	if sessions := h.authority.Sessions(); len(sessions) != 0 {
		t.Fatalf("a dry run created %d sessions", len(sessions))
	}
	if h.authority.ActiveClaims() != 0 {
		t.Errorf("a dry run seized %d live-runtime claims", h.authority.ActiveClaims())
	}
}

// twoLeafScenarioYAML is bindScenarioYAML with a second leaf, so one launch
// spawns two panes.
const twoLeafScenarioYAML = `
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
      secondary:
        candidates:
          - variant: daily
`

// TestEveryLeafOfOneLaunchIsAttached pins the per-leaf rule and the limit
// it runs into. Each spawned pane gets its own attachment and its own live
// claim — two panes are two live runtimes and may never share one. What the
// kernel carries per session is a single *current* attachment
// (decision-01 §4.1 puts the container on the attachment, and a session
// points at one), so the last leaf spawned is the one detach and reattach
// act on. A per-leaf attachment surface is a kernel change, not a CLI one.
func TestEveryLeafOfOneLaunchIsAttached(t *testing.T) {
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
		t.Fatalf("the launch spawned %d leaves, want 2", len(hosts.seen))
	}
	if h.authority.ActiveClaims() != 2 {
		t.Errorf("active live-runtime claims = %d, want one per spawned pane", h.authority.ActiveClaims())
	}
	for i, evidence := range hosts.seen {
		if _, held := h.authority.ActiveClaim(liveRuntimeFingerprint(herdr.AdapterID, evidence).ClaimRef()); !held {
			t.Errorf("leaf %d's pane %s holds no live-runtime claim", i, evidence.PaneID)
		}
	}

	h.close()
	code, out, errOut := runSession(t, "session", "show", report.SessionID, "--output", "json")
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
	if len(shown.Result.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2 (one per spawned leaf)", len(shown.Result.Attachments))
	}
	paneSet := map[string]bool{hosts.seen[0].PaneID: true, hosts.seen[1].PaneID: true}
	for _, att := range shown.Result.Attachments {
		if !paneSet[att.Container] {
			t.Errorf("attachment container = %q, want one of the spawned panes", att.Container)
		}
		if att.ProcessBirth == nil {
			t.Errorf("attachment %s: process_birth omitted, want spawn PID and start time", att.AttachmentID)
		}
		if att.ReattachCommand == "" {
			t.Errorf("attachment %s: reattach_command empty, want a pasteable command", att.AttachmentID)
		}
		if !att.ClaimHeld && att.ReattachCommand != "" {
			t.Errorf("attachment %s: claim_held=false but reattach_command=%q", att.AttachmentID, att.ReattachCommand)
		}
		if att.ClaimHeld && !strings.Contains(att.ReattachCommand, "--process-pid") {
			t.Errorf("attachment %s: reattach_command missing process flags: %q", att.AttachmentID, att.ReattachCommand)
		}
	}

	attachment, ok := attachmentOf(t, h.authority, report.SessionID)
	if !ok {
		t.Fatal("a two-leaf launch recorded no attachment at all")
	}
	if attachment.Container != hosts.seen[len(hosts.seen)-1].PaneID {
		t.Errorf("current attachment container = %q, want the last spawned pane %q",
			attachment.Container, hosts.seen[len(hosts.seen)-1].PaneID)
	}
}

// TestAttachmentFailureIsLoudAndNeverFailsTheLaunch is the failure stance
// (docs/cli/decisions.md, "Launch records the host attachment"): the pane is
// live and nothing can un-spawn it, so a refused attachment write is
// reported, not raised. Here the deduced host is a kind this build has no
// epoch convention for, so the fingerprint cannot validate.
func TestAttachmentFailureIsLoudAndNeverFailsTheLaunch(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("tmux:/tmp/tmux-1000/default", nil)
	if mat.Host().Kind == herdr.AdapterID {
		t.Fatalf("host kind = %q, want a non-herdr kind for this case", mat.Host().Kind)
	}

	report, err := h.launch(mat, spawningHosts{}, false)
	if err != nil {
		t.Fatalf("a refused attachment failed the launch: %v", err)
	}
	if report.SessionID == "" {
		t.Fatal("the launch reported no session")
	}
	if _, ok := attachmentOf(t, h.authority, report.SessionID); ok {
		t.Error("an unvalidatable fingerprint was recorded as an attachment anyway")
	}

	said := h.err.String()
	for _, want := range []string{"host attachment not recorded", "the launch itself succeeded", "duo session detach"} {
		if !strings.Contains(said, want) {
			t.Errorf("the skipped-attachment output does not say %q:\n%s", want, said)
		}
	}
}

// --- the CLI round trip ----------------------------------------------------

// TestDetachAndReattachSucceedOnALaunchedSession is the live-found gap,
// end to end and through the real verbs: `duo session detach <id>` and
// `duo session reattach <id> --epoch-kind …` both refused a launched
// session with "domain: unknown duo object: session <id> has no host
// attachment" (2026-08-24 dogfood). The launch runs through the same
// launchAndBind the command handler calls; detach and reattach then run as
// separate `duo` invocations against the same installation, which is what
// the operator actually did.
func TestDetachAndReattachSucceedOnALaunchedSession(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	hosts := newRecordingHosts()
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(hosts.seen) != 1 {
		t.Fatalf("the launch spawned %d leaves, want 1", len(hosts.seen))
	}
	sessionID := report.SessionID
	// Release the authority-writer lease: every verb below is its own
	// process in real use.
	h.close()

	code, out, errOut := runSession(t, "session", "show", sessionID, "--output", "json")
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
	if len(shown.Result.Attachments) != 1 {
		t.Fatalf("attachments = %d, want 1", len(shown.Result.Attachments))
	}
	reattachCmd := shown.Result.Attachments[0].ReattachCommand
	if reattachCmd == "" {
		t.Fatal("reattach_command empty before detach, want the pasteable success path")
	}

	code, out, errOut = runSession(t, "session", "show", sessionID)
	if code != exitcode.Success {
		t.Fatalf("show text: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	wantLine := "reattach with: " + reattachCmd + "\n"
	if !strings.Contains(out, wantLine) {
		t.Errorf("show text missing one-line reattach with matching JSON:\n%s", out)
	}

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
	if detached.Result.AttachmentState != string(domain.Detached) {
		t.Errorf("detach: attachment_state = %q, want detached", detached.Result.AttachmentState)
	}

	reattachArgs := append(reattachArgsFromPrintedCommand(t, reattachCmd), "--output", "json")
	code, out, errOut = runSession(t, reattachArgs...)
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
	if reattached.Result.AttachmentState != string(domain.Attached) {
		t.Errorf("reattach: attachment_state = %q, want attached", reattached.Result.AttachmentState)
	}

	// A pane that now holds a different execution is a new runtime instance,
	// not a reattach: the revalidation is real, not a formality.
	evidence := hosts.seen[0]
	code, _, errOut = runSession(t, "session", "detach", sessionID)
	if code != exitcode.Success {
		t.Fatalf("second detach: exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut)
	}
	code, _, errOut = runSession(t, "session", "reattach", sessionID,
		"--integration-instance", evidence.IntegrationInstanceID,
		"--epoch-kind", herdrEpochKind,
		"--epoch-value", evidence.HostContainerID,
		"--epoch-scope", string(domain.EpochScopePane),
		"--container", "some-other-pane",
	)
	if code != exitcode.UserFail {
		t.Fatalf("reattach with a mismatched fingerprint: exit code = %d, want %d (stderr: %s)",
			code, exitcode.UserFail, errOut)
	}
}
