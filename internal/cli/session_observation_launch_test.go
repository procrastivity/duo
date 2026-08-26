package cli

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/runtime"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
)

// TestLaunchInspectAndConversationList_FakePair exercises session.show and
// conversation.list after a launch that reaches live through step 04's bind
// helper — the test body never calls MarkLive or hand-seeds Authority
// correlations. Fake host reports agent_session identity; fake runtime
// correlates on ExternalAgentSessionID (D6).
func TestLaunchInspectAndConversationList_FakePair(t *testing.T) {
	const externalID = "external-agent-launch-1"

	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	at := time.Date(2026, 8, 13, 12, 0, 1, 0, time.UTC)
	rt := runtimefake.New("claude-code")
	rt.SeedTranscript(externalID,
		runtime.ConversationTurn{ID: "cvr_1", Role: "user", Text: "hello", At: at},
		runtime.ConversationTurn{ID: "cvr_2", Role: "agent", Text: "The focused checks pass.", At: at.Add(time.Second)},
	)
	rt.SeedCondition(externalID, runtime.ConditionObservation{
		ObservationID: "obs_1",
		Value:         runtime.ConditionIdle,
		Confidence:    runtime.ConditionConfidenceInferred,
		Freshness:     runtime.ConditionFreshnessFresh,
		EffectiveAt:   at,
		ComputedAt:    at,
	})
	RegisterAgentRuntime("claude-code", rt)
	t.Cleanup(func() { UnregisterAgentRuntime("claude-code") })

	ident := host.AgentSessionIdentity{
		Source: "herdr:claude",
		Agent:  "claude",
		Kind:   host.AgentSessionKindID,
		Value:  externalID,
	}
	report, err := h.launch(mat, newIdentityHosts(&host.AgentBindState{
		Session:          &ident,
		LaunchPending:    false,
		InteractiveReady: true,
	}), false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok {
		t.Fatal("no current runtime instance")
	}
	if inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live (test body did not call MarkLive)", inst.State)
	}
	bindings, ok := agentBindingsFor(h.authority, sess)
	if !ok {
		t.Fatal("agentBindingsFor failed after launch bind")
	}
	if bindings.ExternalAgentSessionID != externalID {
		t.Errorf("external agent session = %q, want %q", bindings.ExternalAgentSessionID, externalID)
	}

	sessionID := report.SessionID
	instanceID := string(sess.Current)

	h.close()

	code, out, errOut := runSession(t, "session", "show", sessionID, "--output", "json")
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
	if shown.Result.InstanceState != string(domain.InstanceLive) {
		t.Errorf("runtime_instance_state = %q, want live", shown.Result.InstanceState)
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

	code, out, errOut = runSession(t, "conversation", "list", sessionID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("conversation list: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	conversationOp := registeredOpByCLI("conversation", "list")
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
}
