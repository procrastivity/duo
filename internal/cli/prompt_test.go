package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/devin"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
	"github.com/procrastivity/duo/internal/store"
)

func TestMapPromptReleaseDevinLockJSON(t *testing.T) {
	for _, tc := range []struct {
		name, title string
	}{
		{name: "without title"},
		{name: "with title", title: "Reply with exactly: LOCKED"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var stderr bytes.Buffer
			streams := &iostreams.Streams{Err: &stderr}
			err := mapPromptReleaseError(streams, "json", "prompt.deliver", domain.PromptCommand{ID: "cmd_lock"}, &devin.SessionLockedError{
				SessionID: "dark-carnation", Title: tc.title,
			})
			if err == nil {
				t.Fatal("expected written failure marker")
			}
			assertValidExternalV1(t, stderr.Bytes())
			var env struct {
				Error struct {
					Code, Message, Effect string
					Retry                 promptRetryAdvice
					Details               map[string]any
				} `json:"error"`
			}
			if json.Unmarshal(stderr.Bytes(), &env) != nil {
				t.Fatalf("invalid JSON: %s", stderr.String())
			}
			if env.Error.Code != "operation.temporarily_unavailable" || env.Error.Effect != "unknown_effect" || !env.Error.Retry.Safe || env.Error.Retry.Action != "retry_after_holder_release" {
				t.Fatalf("error mapping = %+v", env.Error)
			}
			if env.Error.Details["error_kind"] != "session_locked" || env.Error.Details["devin_session_id"] != "dark-carnation" {
				t.Fatalf("details = %#v", env.Error.Details)
			}
			gotTitle, hasTitle := env.Error.Details["devin_session_title"]
			if hasTitle != (tc.title != "") || (hasTitle && gotTitle != tc.title) {
				t.Fatalf("devin_session_title = %#v, present %v", gotTitle, hasTitle)
			}
			if !strings.Contains(env.Error.Message, "dark-carnation") || strings.Contains(strings.ToLower(env.Error.Message), "process") || strings.Contains(strings.ToLower(env.Error.Message), "pane") {
				t.Fatalf("unsafe or incomplete message: %q", env.Error.Message)
			}
		})
	}
}

func TestPromptSendAndShow_FakePairQueuedThenInspect(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()

	const (
		hostIntegration   = "fake-host"
		agentIntegration  = "fake-runtime"
		externalSessionID = "external-agent-prompt-1"
	)

	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSessionID, runtime.ConditionObservation{
		Value:      runtime.ConditionIdle,
		Confidence: runtime.ConditionConfidenceInferred,
		Freshness:  runtime.ConditionFreshnessFresh,
	})
	RegisterAgentRuntime(agentIntegration, rt)
	t.Cleanup(func() { UnregisterAgentRuntime(agentIntegration) })

	hostAd := hostfake.New(hostIntegration)
	RegisterHostPromptProvider(hostIntegration, hostAd)
	t.Cleanup(func() { UnregisterHostPromptProvider(hostIntegration) })

	code, out, errOut := runSession(t,
		"session", "enroll", "--output", "json",
		"--root-path", root,
		"--integration-instance", hostIntegration,
		"--epoch-kind", "fake.epoch",
		"--epoch-value", "epoch-prompt-1",
		"--epoch-scope", "pane",
		"--container", "pane-prompt-1",
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

	code, out, errOut = runSession(t,
		"prompt", "send", sessionID,
		"--text", "Run the focused checks.",
		"--idempotency-key", "key-queued-1",
		"--output", "json",
	)
	if code != exitcode.Success {
		t.Fatalf("prompt send: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var sent struct {
		Operation string              `json:"operation"`
		Result    promptDeliverResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &sent); err != nil {
		t.Fatalf("send JSON: %v", err)
	}
	if sent.Operation != registeredOpByCLI("prompt", "send") {
		t.Errorf("operation = %q, want prompt.deliver", sent.Operation)
	}
	if sent.Result.ResponsibilityState != string(domain.ResponsibilityQueued) {
		t.Fatalf("state = %q, want queued (enroll is not Duo-created)", sent.Result.ResponsibilityState)
	}
	if sent.Result.Hold == nil || sent.Result.Hold.Code != "prompt.human_priority_hold" {
		t.Fatalf("hold = %+v, want prompt.human_priority_hold", sent.Result.Hold)
	}
	if sent.Result.QueuePolicy != string(domain.QueueUntilSafe) {
		t.Errorf("queue_policy = %q", sent.Result.QueuePolicy)
	}
	if sent.Result.Acknowledged {
		t.Error("acknowledged must stay false")
	}
	commandID := sent.Result.CommandID

	code, out, errOut = runSession(t, "prompt", "show", commandID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("prompt show: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var shown struct {
		Operation string               `json:"operation"`
		Result    commandInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("show JSON: %v", err)
	}
	if shown.Operation != registeredOpByCLI("prompt", "show") {
		t.Errorf("operation = %q, want command.inspect", shown.Operation)
	}
	if shown.Result.CommandID != commandID {
		t.Errorf("command_id = %q, want %q", shown.Result.CommandID, commandID)
	}
	if shown.Result.ResponsibilityState != string(domain.ResponsibilityQueued) {
		t.Errorf("inspect state = %q, want queued", shown.Result.ResponsibilityState)
	}
}

func TestPromptSend_DeliveredFakePair(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	const (
		hostIntegration   = "fake-host"
		agentIntegration  = "fake-runtime"
		externalSessionID = "external-agent-prompt-2"
	)

	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSessionID, runtime.ConditionObservation{
		Value:      runtime.ConditionIdle,
		Confidence: runtime.ConditionConfidenceInferred,
		Freshness:  runtime.ConditionFreshnessFresh,
	})
	RegisterAgentRuntime(agentIntegration, rt)
	t.Cleanup(func() { UnregisterAgentRuntime(agentIntegration) })

	hostAd := hostfake.New(hostIntegration)
	RegisterHostPromptProvider(hostIntegration, hostAd)
	t.Cleanup(func() { UnregisterHostPromptProvider(hostIntegration) })

	sessionID := seedDuoCreatedSession(t, hostIntegration, agentIntegration, externalSessionID, "w1:p-deliv", "term-deliv", 5151)

	code, out, errOut := runSession(t,
		"prompt", "send", sessionID,
		"--text", "Run the focused checks.",
		"--idempotency-key", "key-delivered-1",
		"--output", "json",
	)
	if code != exitcode.Success {
		t.Fatalf("prompt send: exit code = %d (stderr: %s)\nstdout: %s", code, errOut, out)
	}
	assertValidExternalV1(t, []byte(out))
	var sent struct {
		Result promptDeliverResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &sent); err != nil {
		t.Fatalf("send JSON: %v", err)
	}
	if sent.Result.ResponsibilityState != string(domain.ResponsibilityDelivered) {
		t.Fatalf("state = %q, want delivered", sent.Result.ResponsibilityState)
	}
	if sent.Result.Hold != nil {
		t.Fatalf("hold present on delivered: %+v", sent.Result.Hold)
	}
	if sent.Result.Acknowledged {
		t.Error("acknowledged must stay false")
	}

	code, out, errOut = runSession(t, "prompt", "show", sent.Result.CommandID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("prompt show: exit code = %d (stderr: %s)", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var shown struct {
		Result commandInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("show JSON: %v", err)
	}
	if shown.Result.ResponsibilityState != string(domain.ResponsibilityDelivered) {
		t.Errorf("inspect state = %q, want delivered", shown.Result.ResponsibilityState)
	}
	if len(shown.Result.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1", len(shown.Result.Attempts))
	}
	if shown.Result.Attempts[0].Realization != "native" {
		t.Errorf("realization = %q, want native", shown.Result.Attempts[0].Realization)
	}
	if shown.Result.Attempts[0].RecordedResult != string(domain.ResponsibilityDelivered) {
		t.Errorf("recorded_result = %q", shown.Result.Attempts[0].RecordedResult)
	}
	if shown.Result.Milestones.DeliveredAt == "" {
		t.Error("milestones.delivered_at missing")
	}
}

func TestPromptSend_IdempotencyConflict(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	const (
		hostIntegration   = "fake-host"
		agentIntegration  = "fake-runtime"
		externalSessionID = "external-agent-prompt-3"
	)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSessionID, runtime.ConditionObservation{
		Value: runtime.ConditionIdle, Confidence: runtime.ConditionConfidenceInferred, Freshness: runtime.ConditionFreshnessFresh,
	})
	RegisterAgentRuntime(agentIntegration, rt)
	t.Cleanup(func() { UnregisterAgentRuntime(agentIntegration) })
	RegisterHostPromptProvider(hostIntegration, hostfake.New(hostIntegration))
	t.Cleanup(func() { UnregisterHostPromptProvider(hostIntegration) })

	sessionID := seedDuoCreatedSession(t, hostIntegration, agentIntegration, externalSessionID, "w1:p-idem", "term-idem", 5252)

	code, _, errOut := runSession(t,
		"prompt", "send", sessionID,
		"--text", "first text",
		"--idempotency-key", "same-key",
		"--output", "json",
	)
	if code != exitcode.Success {
		t.Fatalf("first send: exit = %d (stderr: %s)", code, errOut)
	}

	code, out, errOut := runSession(t,
		"prompt", "send", sessionID,
		"--text", "different text",
		"--idempotency-key", "same-key",
		"--output", "json",
	)
	if code != exitcode.UserFail {
		t.Fatalf("conflict exit = %d, want %d (stderr: %s stdout: %s)", code, exitcode.UserFail, errOut, out)
	}
	assertValidExternalV1(t, []byte(errOut))
	var env struct {
		Operation string `json:"operation"`
		Error     struct {
			Class   string         `json:"class"`
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(errOut), &env); err != nil {
		t.Fatalf("conflict JSON: %v\n%s", err, errOut)
	}
	if env.Operation != registeredOpByCLI("prompt", "send") {
		t.Errorf("operation = %q", env.Operation)
	}
	if env.Error.Code != "command.idempotency_conflict" {
		t.Errorf("code = %q", env.Error.Code)
	}
	if env.Error.Class != "conflict" {
		t.Errorf("class = %q", env.Error.Class)
	}
}

func TestPromptSend_ReplaySameDigest(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	const (
		hostIntegration   = "fake-host"
		agentIntegration  = "fake-runtime"
		externalSessionID = "external-agent-prompt-4"
	)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSessionID, runtime.ConditionObservation{
		Value: runtime.ConditionIdle, Confidence: runtime.ConditionConfidenceInferred, Freshness: runtime.ConditionFreshnessFresh,
	})
	RegisterAgentRuntime(agentIntegration, rt)
	t.Cleanup(func() { UnregisterAgentRuntime(agentIntegration) })
	RegisterHostPromptProvider(hostIntegration, hostfake.New(hostIntegration))
	t.Cleanup(func() { UnregisterHostPromptProvider(hostIntegration) })

	sessionID := seedDuoCreatedSession(t, hostIntegration, agentIntegration, externalSessionID, "w1:p-replay", "term-replay", 5353)

	code, out, errOut := runSession(t,
		"prompt", "send", sessionID,
		"--text", "same text",
		"--idempotency-key", "replay-key",
		"--output", "json",
	)
	if code != exitcode.Success {
		t.Fatalf("first send: %d (%s)", code, errOut)
	}
	var first struct {
		Result promptDeliverResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatal(err)
	}

	code, out, errOut = runSession(t,
		"prompt", "send", sessionID,
		"--text", "same text",
		"--idempotency-key", "replay-key",
		"--output", "json",
	)
	if code != exitcode.Success {
		t.Fatalf("replay send: %d (%s)", code, errOut)
	}
	var second struct {
		Result promptDeliverResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &second); err != nil {
		t.Fatal(err)
	}
	if second.Result.CommandID != first.Result.CommandID {
		t.Errorf("replay minted %s, want existing %s", second.Result.CommandID, first.Result.CommandID)
	}
	if second.Result.ResponsibilityState != string(domain.ResponsibilityDelivered) {
		t.Errorf("replay state = %q", second.Result.ResponsibilityState)
	}
}

func TestPromptSend_TextPointsAtInspect(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := t.TempDir()

	const (
		hostIntegration   = "fake-host"
		agentIntegration  = "fake-runtime"
		externalSessionID = "external-agent-prompt-5"
	)
	rt := runtimefake.New(agentIntegration)
	RegisterAgentRuntime(agentIntegration, rt)
	t.Cleanup(func() { UnregisterAgentRuntime(agentIntegration) })

	code, out, errOut := runSession(t,
		"session", "enroll", "--output", "json",
		"--root-path", root,
		"--integration-instance", hostIntegration,
		"--epoch-kind", "fake.epoch",
		"--epoch-value", "epoch-text",
		"--epoch-scope", "pane",
		"--container", "pane-text",
		"--process-pid", "4242",
		"--process-started-at", "2026-08-13T12:00:00.000Z",
		"--agent-integration-instance", agentIntegration,
		"--agent-session-id", externalSessionID,
		"--transcript", "/tmp/fake-transcript.jsonl",
	)
	if code != exitcode.Success {
		t.Fatalf("enroll: %d (%s)", code, errOut)
	}
	var enrolled enrollEnvelope
	_ = json.Unmarshal([]byte(out), &enrolled)

	code, out, errOut = runSession(t,
		"prompt", "send", enrolled.Result.SessionID,
		"--text", "hello",
		"--idempotency-key", "text-key",
	)
	if code != exitcode.Success {
		t.Fatalf("send: %d (%s)", code, errOut)
	}
	if !strings.Contains(out, "duo prompt show ") {
		t.Errorf("text output must point at prompt show:\n%s", out)
	}
}

func TestSessionLaunchHelpHasNoPromptFlag(t *testing.T) {
	code, out, errOut := runSession(t, "session", "launch", "--help")
	if code != exitcode.Success {
		t.Fatalf("help exit = %d (stderr: %s)", code, errOut)
	}
	combined := out + errOut
	if strings.Contains(combined, "--prompt") {
		t.Fatal("session launch --help must not mention --prompt")
	}
}

func TestPromptSend_DelayedIdentityEndsDelivered(t *testing.T) {
	const ident = "sess-delay-prompt-1"
	pair := launchStartingClaudePair(t, ident)
	pair.host.ScriptPromptOutcome(host.PromptOutcomeNoEffect)

	go func() {
		time.Sleep(80 * time.Millisecond)
		pair.host.SetPaneAgentBind(pair.pane, host.AgentBindState{
			Session: &host.AgentSessionIdentity{
				Source: "herdr:claude",
				Agent:  "claude",
				Kind:   host.AgentSessionKindID,
				Value:  ident,
			},
			LaunchPending:    false,
			InteractiveReady: true,
		})
	}()
	pair.harness.close()

	expires := time.Now().UTC().Add(15 * time.Second).Format(time.RFC3339Nano)
	code, out, errOut := runSession(t,
		"prompt", "send", pair.session,
		"--text", "Run the focused checks.",
		"--idempotency-key", "key-delay-1",
		"--expires-at", expires,
		"--output", "json",
	)
	if code != exitcode.Success {
		t.Fatalf("prompt send: exit code = %d (stderr: %s)\nstdout: %s", code, errOut, out)
	}
	assertValidExternalV1(t, []byte(out))
	var sent struct {
		Result promptDeliverResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &sent); err != nil {
		t.Fatalf("send JSON: %v", err)
	}
	if sent.Result.ResponsibilityState != string(domain.ResponsibilityDelivered) {
		t.Fatalf("state = %q, want delivered (identity appeared after a short delay)", sent.Result.ResponsibilityState)
	}
	if sent.Result.Hold != nil {
		t.Fatalf("hold present on delivered: %+v", sent.Result.Hold)
	}

	code, out, errOut = runSession(t, "prompt", "show", sent.Result.CommandID, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("prompt show: exit code = %d (stderr: %s)", code, errOut)
	}
	var shown struct {
		Result commandInspectResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &shown); err != nil {
		t.Fatalf("show JSON: %v", err)
	}
	if shown.Result.ResponsibilityState != string(domain.ResponsibilityDelivered) {
		t.Errorf("inspect state = %q, want delivered", shown.Result.ResponsibilityState)
	}
	if len(shown.Result.Attempts) != 1 {
		t.Fatalf("attempts = %d, want 1 (runtime path once bound)", len(shown.Result.Attempts))
	}
}

func TestPromptSend_NeverBindsExpiresLoudly(t *testing.T) {
	pair := launchStartingClaudePair(t, "sess-never-prompt-1")
	sessionID := pair.session
	pair.harness.close()

	expires := time.Now().UTC().Add(800 * time.Millisecond).Format(time.RFC3339Nano)
	code, out, errOut := runSession(t,
		"prompt", "send", sessionID,
		"--text", "Run the focused checks.",
		"--idempotency-key", "key-never-1",
		"--expires-at", expires,
		"--output", "json",
	)
	if code == exitcode.Success {
		t.Fatalf("never-binds send succeeded (stdout: %s stderr: %s)", out, errOut)
	}
	body := errOut
	if body == "" {
		body = out
	}
	assertValidExternalV1(t, []byte(body))
	var env struct {
		Error struct {
			Code    string         `json:"code"`
			Effect  string         `json:"effect"`
			Details map[string]any `json:"details"`
		} `json:"error"`
		Result *promptDeliverResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("failure JSON: %v\n%s", err, body)
	}
	if env.Result != nil && env.Result.ResponsibilityState == string(domain.ResponsibilityDelivered) {
		t.Fatalf("never-binds looked like success: delivered %+v", env.Result)
	}
	if env.Error.Code != "command.expired" && env.Error.Code != "operation.temporarily_unavailable" {
		t.Errorf("code = %q, want command.expired (or a loud unavailable), stdout=%s", env.Error.Code, out)
	}
	if state, _ := env.Error.Details["responsibility_state"].(string); state == string(domain.ResponsibilityDelivered) {
		t.Fatalf("expired details claim delivered: %+v", env.Error.Details)
	}
	if state, _ := env.Error.Details["responsibility_state"].(string); state == string(domain.ResponsibilityQueued) {
		t.Fatalf("never-binds expired as queued hold, which looks like success: %+v", env.Error.Details)
	}

	a, closer, err := openReadAuthority(context.Background())
	if err != nil {
		t.Fatalf("reopen authority: %v", err)
	}
	defer func() { _ = closer.Close() }()
	sess, ok := a.Session(domain.SessionID(sessionID))
	if !ok {
		t.Fatalf("no session %s after never-binds send", sessionID)
	}
	inst, ok := a.Instance(sess.Current)
	if !ok {
		t.Fatal("no current instance")
	}
	if inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %s, want starting (send must not MarkLive)", inst.State)
	}
	if _, ok := agentBindingsFor(a, sess); ok {
		t.Fatal("agent.session correlation written although identity never appeared")
	}
}

// launchStartingClaudePair launches a Duo-created Claude leaf with no
// host identity at spawn. The instance stays starting; the cached fake
// host is registered so a later in-process prompt send sees the same pane.
func launchStartingClaudePair(t *testing.T, ident string) struct {
	harness *bindHarness
	session string
	pane    string
	host    *hostfake.Host
} {
	t.Helper()
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	rt := runtimefake.New("claude-code")
	rt.SeedCondition(ident, runtime.ConditionObservation{
		Value:      runtime.ConditionIdle,
		Confidence: runtime.ConditionConfidenceInferred,
		Freshness:  runtime.ConditionFreshnessFresh,
	})
	RegisterAgentRuntime("claude-code", rt)
	t.Cleanup(func() { UnregisterAgentRuntime("claude-code") })

	hosts := newIdentityHosts(nil)
	report, err := h.launch(mat, hosts, false)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(hosts.hosts) != 1 {
		t.Fatalf("cached hosts = %d, want 1", len(hosts.hosts))
	}
	var (
		hostID   string
		fakeHost *hostfake.Host
	)
	for id, fh := range hosts.hosts {
		hostID, fakeHost = id, fh
	}
	RegisterHostPromptProvider(hostID, fakeHost)
	t.Cleanup(func() { UnregisterHostPromptProvider(hostID) })

	sess, ok := h.authority.Session(domain.SessionID(report.SessionID))
	if !ok {
		t.Fatalf("no session %s", report.SessionID)
	}
	inst, ok := h.authority.Instance(sess.Current)
	if !ok || inst.State != domain.InstanceStarting {
		t.Fatalf("instance state = %+v, want starting", inst)
	}
	if _, ok := agentBindingsFor(h.authority, sess); ok {
		t.Fatal("identity appeared at launch; want a delayed reveal")
	}
	att, ok := h.authority.Attachment(sess.Attachment)
	if !ok || att.Container == "" {
		t.Fatal("launch attachment has no pane id")
	}
	return struct {
		harness *bindHarness
		session string
		pane    string
		host    *hostfake.Host
	}{h, report.SessionID, att.Container, fakeHost}
}

func TestPromptCLIPathsMatchRegistry(t *testing.T) {
	root := NewRootCommand(iostreams.System(), buildinfo.Info{})
	for _, want := range [][]string{
		{"prompt", "send"},
		{"prompt", "show"},
	} {
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
			t.Fatalf("%v is not registered", want)
		}
		cmd, _, err := root.Find(d.CLI)
		if err != nil {
			t.Fatalf("%s: root.Find(%v): %v", d.Name, d.CLI, err)
		}
		if cmd.Name() != want[len(want)-1] {
			t.Errorf("%s resolved %q, want %q", d.Name, cmd.Name(), want[len(want)-1])
		}
	}
}

func TestPromptLeaseNotInCLI(t *testing.T) {
	root := NewRootCommand(iostreams.System(), buildinfo.Info{})
	parent, _, err := root.Find([]string{"prompt"})
	if err != nil {
		t.Fatalf("prompt parent missing: %v", err)
	}
	for _, child := range parent.Commands() {
		if child.Name() == "lease" {
			t.Fatal("lease verb must not be registered under prompt")
		}
	}
	cliDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	pattern := "prompt" + " " + "lease"
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

func TestPromptCanonicalDigestStable(t *testing.T) {
	a := promptCanonicalDigest("Run the focused checks.")
	b := promptCanonicalDigest("Run the focused checks.")
	c := promptCanonicalDigest("different")
	if a != b {
		t.Fatalf("same text produced different digests: %q vs %q", a, b)
	}
	if !strings.HasPrefix(a, "sha256:") || len(a) != len("sha256:")+64 {
		t.Fatalf("digest shape = %q, want sha256:<64 hex>", a)
	}
	if a == c {
		t.Fatal("different text produced the same digest")
	}
}

// seedDuoCreatedSession writes a launch-plan-bound session with StartedAt in
// the past so the D3 settle window has elapsed under the wall clock when
// prompt send runs.
func seedDuoCreatedSession(t *testing.T, hostIntegration, agentIntegration, externalSession, pane, terminal string, pid int) string {
	t.Helper()
	ctx := context.Background()
	path, err := authorityStorePath()
	if err != nil {
		t.Fatalf("store path: %v", err)
	}
	s, err := store.OpenAuthority(path)
	if err != nil {
		t.Fatalf("OpenAuthority: %v", err)
	}
	past := time.Now().UTC().Add(-1 * time.Minute)
	a, err := domain.Open(ctx, storerepo.New(s), domain.WithClock(func() time.Time { return past }))
	if err != nil {
		_ = s.Close()
		t.Fatalf("domain.Open: %v", err)
	}
	launched, err := a.Launch(ctx, domain.LaunchRequest{
		RootPath: t.TempDir(), Actor: "cli", Reason: "step-14 duo-created seed",
	})
	if err != nil {
		_ = s.Close()
		t.Fatalf("Launch: %v", err)
	}
	fp := domain.Fingerprint{
		IntegrationInstance: hostIntegration,
		Epoch: domain.HostEpoch{
			Kind:  "fake.epoch",
			Value: terminal,
			Scope: domain.EpochScopePane,
		},
		Container: pane,
		Process: domain.ProcessBirth{
			PID:       pid,
			StartedAt: past.Format("2006-01-02T15:04:05.000Z"),
		},
	}
	if err := a.Bind(ctx, domain.BindRequest{
		Session:     launched.Session,
		Actor:       "host",
		Attestation: domain.Attestation{Source: domain.SourceLaunchPlan},
		Fingerprint: &fp,
		AgentSession: domain.AgentSessionRef{
			IntegrationInstance: agentIntegration,
			SessionID:           externalSession,
		},
		Transcript: "/tmp/fake-transcript.jsonl",
	}); err != nil {
		_ = s.Close()
		t.Fatalf("Bind: %v", err)
	}
	if err := a.MarkLive(ctx, launched.Instance, "host", "process is live"); err != nil {
		_ = s.Close()
		t.Fatalf("MarkLive: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}
	return string(launched.Session)
}
