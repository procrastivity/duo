package cli

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
	"github.com/procrastivity/duo/internal/runtime"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
)

// TestSessionInspectAndConversationList_FakePair covers both step-09 verbs
// through the built root with the permanent fake-host + fake-runtime pair
// (D6): session.inspect carries condition/operations, conversation.list
// projects seeded turns, and both JSON envelopes validate.
// Bound-live condition + turns on the enroll path; launch-path live inspect
// is step 05's fake-pair coverage — do not duplicate it here.
func TestSessionInspectAndConversationList_FakePair(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()

	const (
		hostIntegration   = "fake-host"
		agentIntegration  = "fake-runtime"
		externalSessionID = "external-agent-1"
	)

	rt := runtimefake.New(agentIntegration)
	at := time.Date(2026, 8, 13, 12, 0, 1, 0, time.UTC)
	rt.SeedTranscript(externalSessionID,
		runtime.ConversationTurn{ID: "cvr_1", Role: "user", Text: "hello", At: at},
		runtime.ConversationTurn{ID: "cvr_2", Role: "agent", Text: "The focused checks pass.", At: at.Add(time.Second)},
	)
	rt.SeedCondition(externalSessionID, runtime.ConditionObservation{
		ObservationID: "obs_1",
		Value:         runtime.ConditionIdle,
		Confidence:    runtime.ConditionConfidenceInferred,
		Freshness:     runtime.ConditionFreshnessFresh,
		EffectiveAt:   at,
		ComputedAt:    at,
	})
	RegisterAgentRuntime(agentIntegration, rt)
	t.Cleanup(func() { UnregisterAgentRuntime(agentIntegration) })

	code, out, errOut := runSession(t,
		"session", "enroll", "--output", "json",
		"--root-path", root,
		"--integration-instance", hostIntegration,
		"--epoch-kind", "fake.epoch",
		"--epoch-value", "epoch-1",
		"--epoch-scope", "pane",
		"--container", "pane-1",
		"--process-pid", "4242",
		"--process-started-at", "2026-08-13T12:00:00.000Z",
		"--agent-integration-instance", agentIntegration,
		"--agent-session-id", externalSessionID,
		"--transcript", "/tmp/fake-transcript.jsonl",
	)
	if code != exitcode.Success {
		t.Fatalf("enroll: exit code = %d (stderr: %s)", code, errOut)
	}
	var enrolled enrollEnvelope
	if err := json.Unmarshal([]byte(out), &enrolled); err != nil {
		t.Fatalf("enroll JSON: %v", err)
	}
	sessionID := enrolled.Result.SessionID
	instanceID := enrolled.Result.InstanceID

	code, out, errOut = runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var shown struct {
		Result sessionInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("show JSON: %v", err)
	}
	if shown.Result.Condition == nil {
		t.Fatal("show: condition missing")
	}
	if shown.Result.Condition.Value != string(runtime.ConditionIdle) {
		t.Errorf("condition.value = %q, want idle", shown.Result.Condition.Value)
	}
	if shown.Result.Condition.RuntimeInstanceID != instanceID {
		t.Errorf("condition.runtime_instance_id = %q, want %q", shown.Result.Condition.RuntimeInstanceID, instanceID)
	}
	if len(shown.Result.Operations) != 2 {
		t.Fatalf("operations = %d, want 2", len(shown.Result.Operations))
	}
	promptOp := registeredOpByCLI("prompt", "send")
	conversationOp := registeredOpByCLI("conversation", "list")
	byOp := map[string]string{}
	for _, op := range shown.Result.Operations {
		byOp[op.Operation] = op.Availability
	}
	if byOp[promptOp] != "available" {
		t.Errorf("%s availability = %q, want available", promptOp, byOp[promptOp])
	}
	if byOp[conversationOp] != "available" {
		t.Errorf("%s availability = %q, want available", conversationOp, byOp[conversationOp])
	}
	for _, op := range shown.Result.Operations {
		if op.Operation != promptOp && op.Operation != conversationOp {
			t.Errorf("unexpected operation support row %q", op.Operation)
		}
	}

	code, out, errOut = runSession(t, "conversation", "list", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("conversation list: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var listed struct {
		Operation string                 `json:"operation"`
		Result    conversationListResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &listed); err != nil {
		t.Fatalf("conversation list JSON: %v", err)
	}
	if listed.Operation != conversationOp {
		t.Errorf("operation = %q, want %q", listed.Operation, conversationOp)
	}
	if listed.Result.SessionID != sessionID {
		t.Errorf("session_id = %q, want %q", listed.Result.SessionID, sessionID)
	}
	if len(listed.Result.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(listed.Result.Items))
	}
	if listed.Result.Items[1].AuthorRole != "agent" {
		t.Errorf("author_role = %q, want agent", listed.Result.Items[1].AuthorRole)
	}
	if listed.Result.Items[1].RecordID != "cvr_2" {
		t.Errorf("record_id = %q, want cvr_2", listed.Result.Items[1].RecordID)
	}
	if len(listed.Result.Items[1].Blocks) != 1 || listed.Result.Items[1].Blocks[0].Type != "text" {
		t.Errorf("blocks = %+v, want one text block", listed.Result.Items[1].Blocks)
	}
	if listed.Result.Items[1].Completion != "source_record_complete" {
		t.Errorf("completion = %q, want source_record_complete", listed.Result.Items[1].Completion)
	}
	if _, hasBarrier := conversationResultField(out, "barrier"); hasBarrier {
		t.Error("conversation.list must omit barrier")
	}

	// I-5: store terminal → exited without dialing HostLifecycleSource.
	exitStoreInstance(t, domain.InstanceID(instanceID))
	code, out, errOut = runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show after exit: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("show after exit JSON: %v", err)
	}
	if shown.Result.Condition == nil || shown.Result.Condition.Value != string(runtime.ConditionExited) {
		t.Fatalf("after exit: condition = %+v, want exited", shown.Result.Condition)
	}
	byOp = map[string]string{}
	for _, op := range shown.Result.Operations {
		byOp[op.Operation] = op.Availability
	}
	// Exit retires instance correlations, so the provider binding is gone
	// and availability falls to unsupported (not a fabricated live probe).
	if byOp[conversationOp] != "unsupported" {
		t.Errorf("after exit: %s = %q, want unsupported", conversationOp, byOp[conversationOp])
	}
}

// TestSessionShow_StartingOmitsCondition matches step-01 fixture honesty
// (session-inspect-starting.json): runtime_instance_state is starting and
// the condition key is absent — identity never appeared, no MarkLive.
func TestSessionShow_StartingOmitsCondition(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	report, err := h.launch(mat, newIdentityHosts(nil), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok || inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %+v, want starting", inst)
	}
	sessionID := report.SessionID
	h.close()

	code, out, errOut := runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	result := inspectResultJSON(t, out)
	if got, _ := result["runtime_instance_state"].(string); got != string(domain.InstanceStarting) {
		t.Errorf("runtime_instance_state = %v, want starting", result["runtime_instance_state"])
	}
	if _, has := result["condition"]; has {
		t.Fatalf("condition key present while starting: %v", result["condition"])
	}
}

// TestSessionShow_StartingOmitsCondition_LaunchPendingBound is I-8: identity
// is bound and correlations exist, but launch_pending left the instance
// starting — show must still omit condition (not SnapshotCondition a live view).
func TestSessionShow_StartingOmitsCondition_LaunchPendingBound(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	const agentIntegration = "claude-code"
	const externalSessionID = "sess-pending-show-1"
	rt := runtimefake.New(agentIntegration)
	at := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	rt.SeedCondition(externalSessionID, runtime.ConditionObservation{
		ObservationID: "obs_pending",
		Value:         runtime.ConditionIdle,
		Confidence:    runtime.ConditionConfidenceInferred,
		Freshness:     runtime.ConditionFreshnessFresh,
		EffectiveAt:   at,
		ComputedAt:    at,
	})
	RegisterAgentRuntime(agentIntegration, rt)
	t.Cleanup(func() { UnregisterAgentRuntime(agentIntegration) })

	ident := host.AgentSessionIdentity{
		Kind:  host.AgentSessionKindID,
		Value: externalSessionID,
	}
	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:       &ident,
		LaunchPending: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, _ := h.authority.Instance(sess.Current)
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting while launch_pending", inst.State)
	}
	if _, ok := agentBindingsFor(h.authority, sess); !ok {
		t.Fatal("want agentBindingsFor success while still starting (I-8 regression setup)")
	}
	sessionID := report.SessionID
	h.close()

	code, out, errOut := runSession(t, "session", "show", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("show: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	result := inspectResultJSON(t, out)
	if got, _ := result["runtime_instance_state"].(string); got != string(domain.InstanceStarting) {
		t.Errorf("runtime_instance_state = %v, want starting", result["runtime_instance_state"])
	}
	if _, has := result["condition"]; has {
		t.Fatalf("condition key present while starting with bindings: %v", result["condition"])
	}
}

func TestConversationList_NoBoundTranscript(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()

	code, out, errOut := runSession(t, enrollFixtureArgs(root)...)
	if code != exitcode.Success {
		t.Fatalf("enroll: exit code = %d (stderr: %s)", code, errOut)
	}
	var enrolled enrollEnvelope
	if err := json.Unmarshal([]byte(out), &enrolled); err != nil {
		t.Fatalf("enroll JSON: %v", err)
	}

	code, _, errOut = runSession(t, "conversation", "list", enrolled.Result.SessionID, "--output", "json")
	if code != exitcode.UserFail {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.UserFail, errOut)
	}
	if !strings.Contains(errOut, "object.not_found") && !strings.Contains(errOut, "No bound agent transcript") {
		t.Errorf("stderr = %q, want object.not_found / no bound transcript", errOut)
	}
}

func TestConversationSubscribeNotInCLI(t *testing.T) {
	root := NewRootCommand(iostreams.System(), buildinfo.Info{})
	parent, _, err := root.Find([]string{"conversation"})
	if err != nil {
		t.Fatalf("conversation parent missing: %v", err)
	}
	for _, child := range parent.Commands() {
		if child.Name() == "subscribe" {
			t.Fatal("subscribe verb must not be registered under conversation")
		}
	}

	cliDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	// Seal phrase split so this test source does not itself match the grep.
	pattern := "conversation" + " " + "subscribe"
	cmd := exec.Command("grep", "-R", "--include=*.go", pattern, cliDir)
	out, err := cmd.CombinedOutput()
	if err == nil && len(out) > 0 {
		t.Fatalf("internal/cli must not contain %q:\n%s", pattern, out)
	}
	if err != nil {
		if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 1 {
			t.Fatalf("grep: %v\n%s", err, out)
		}
	}
}

func TestConversationList_CLIPathMatchesRegistry(t *testing.T) {
	want := []string{"conversation", "list"}
	var d registry.Descriptor
	found := false
	for _, row := range registry.All() {
		if len(row.CLI) != len(want) {
			continue
		}
		match := true
		for i := range want {
			if row.CLI[i] != want[i] {
				match = false
				break
			}
		}
		if match {
			d = row
			found = true
			break
		}
	}
	if !found {
		t.Fatal("conversation.list is not registered")
	}
	root := NewRootCommand(iostreams.System(), buildinfo.Info{})
	cmd, _, err := root.Find(d.CLI)
	if err != nil {
		t.Fatalf("%s: root.Find(%v): %v", d.Name, d.CLI, err)
	}
	if cmd.Name() != "list" {
		t.Errorf("resolved %q, want list", cmd.Name())
	}
}

func exitStoreInstance(t *testing.T, id domain.InstanceID) {
	t.Helper()
	ctx := context.Background()
	a, s, err := openWriteAuthority(ctx)
	if err != nil {
		t.Fatalf("openWriteAuthority: %v", err)
	}
	defer func() { _ = s.Close() }()
	if err := a.Exit(ctx, id, "test", "step-09 exited mapping"); err != nil {
		t.Fatalf("Exit: %v", err)
	}
}

func conversationResultField(raw, field string) (any, bool) {
	var doc map[string]any
	if json.Unmarshal([]byte(raw), &doc) != nil {
		return nil, false
	}
	result, _ := doc["result"].(map[string]any)
	if result == nil {
		return nil, false
	}
	v, ok := result[field]
	return v, ok
}

func inspectResultJSON(t *testing.T, raw string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("show JSON: %v", err)
	}
	result, _ := doc["result"].(map[string]any)
	if result == nil {
		t.Fatal("show JSON missing result object")
	}
	return result
}
