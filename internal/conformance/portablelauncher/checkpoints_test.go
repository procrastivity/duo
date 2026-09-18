package portablelauncher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
)

const (
	checkpointWorkspace = "/fixture/workspace"
	checkpointHerdr     = "herdr@/fixture/herdr.sock"
	checkpointPi        = "pi@fixture"
)

var checkpointLaunches = []struct {
	session  domain.SessionID
	instance domain.InstanceID
}{
	// IDs are deliberately reverse-lexical. The observer must use durable
	// creation order, never opaque-ID order.
	{session: "ses_z_happy", instance: "ins_z_happy"},
	{session: "ses_b_exited", instance: "ins_b_exited"},
	{session: "ses_a_timeout", instance: "ins_a_timeout"},
}

func TestCheckpointObserverUsesFactOrderAndMonotonicOffsets(t *testing.T) {
	clock := &offsetClock{offsets: []time.Duration{
		11*time.Millisecond + 900*time.Microsecond,
		22 * time.Millisecond,
		33 * time.Millisecond,
		44 * time.Millisecond,
	}}
	observer := newTestObserver(t, checkpointWorkspace, clock.Now)

	exitedFacts := fixtureLaunchFacts(2)
	exitedTarget := addCompleteBind(&exitedFacts, 1, bindOptions{})
	checkpoint := requireNext(t, observer, exitedFacts)
	assertCheckpoint(t, checkpoint, CheckpointAttachmentBound, RunnerCaseExited, exitedTarget, 11)
	observer.phase++

	// The same snapshot cannot repeat the bind checkpoint, and it has no
	// delivered command with which to advance.
	if got := nextCheckpoint(t, observer, exitedFacts); got != nil {
		t.Fatalf("duplicate snapshot emitted %+v", *got)
	}
	addCommand(&exitedFacts, 1, commandOptions{})
	checkpoint = requireNext(t, observer, exitedFacts)
	assertCheckpoint(t, checkpoint, CheckpointCommandDelivered, RunnerCaseExited, exitedTarget, 22)
	observer.phase++
	if got := nextCheckpoint(t, observer, exitedFacts); got != nil {
		t.Fatalf("repeated delivered snapshot emitted %+v", *got)
	}

	timeoutFacts := fixtureLaunchFacts(3)
	addCompleteBind(&timeoutFacts, 1, bindOptions{})
	addCommand(&timeoutFacts, 1, commandOptions{})
	timeoutTarget := addCompleteBind(&timeoutFacts, 2, bindOptions{})
	checkpoint = requireNext(t, observer, timeoutFacts)
	assertCheckpoint(t, checkpoint, CheckpointAttachmentBound, RunnerCaseTimeout, timeoutTarget, 33)
	observer.phase++
	addCommand(&timeoutFacts, 2, commandOptions{})
	checkpoint = requireNext(t, observer, timeoutFacts)
	assertCheckpoint(t, checkpoint, CheckpointCommandDelivered, RunnerCaseTimeout, timeoutTarget, 44)
}

func TestCheckpointObserverRefusesIncompleteOrMismatchedBind(t *testing.T) {
	tests := []struct {
		name      string
		workspace string
		options   bindOptions
		mutate    func(*[]domain.Fact)
	}{
		{name: "incomplete process", workspace: checkpointWorkspace, options: bindOptions{missingStart: true}},
		{name: "wrong workspace", workspace: "/other/workspace"},
		{name: "wrong Herdr integration", workspace: checkpointWorkspace, options: bindOptions{herdr: "herdr@other"}},
		{name: "wrong Pi correlation", workspace: checkpointWorkspace, options: bindOptions{pi: "pi@other"}},
		{name: "inactive Pi correlation", workspace: checkpointWorkspace, options: bindOptions{correlationStatus: domain.CorrelationStale}},
		{name: "host claim missing", workspace: checkpointWorkspace, options: bindOptions{omitHostClaim: true}},
		{name: "agent claim missing", workspace: checkpointWorkspace, options: bindOptions{omitAgentClaim: true}},
		{name: "multiple attachments", workspace: checkpointWorkspace, mutate: addSecondAttachment},
		{name: "multiple live instances", workspace: checkpointWorkspace, mutate: addSecondLiveInstance},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := fixtureLaunchFacts(2)
			addCompleteBind(&facts, 1, test.options)
			if test.mutate != nil {
				test.mutate(&facts)
			}
			observer := newTestObserver(t, test.workspace, func() time.Duration { return time.Second })
			if got := nextCheckpoint(t, observer, facts); got != nil {
				t.Fatalf("invalid bind emitted %+v", *got)
			}
		})
	}
}

func TestCheckpointObserverRequiresExactDurableDelivery(t *testing.T) {
	tests := []struct {
		name    string
		options commandOptions
	}{
		{name: "accepted only", options: commandOptions{acceptedOnly: true}},
		{name: "attempted only", options: commandOptions{attemptedOnly: true}},
		{name: "wrong key", options: commandOptions{key: "other-key"}},
		{name: "wrong digest", options: commandOptions{digest: ConflictDigest}},
		{name: "wrong instance", options: commandOptions{instance: checkpointLaunches[0].instance}},
		{name: "host path", options: commandOptions{path: domain.PromptPathHost}},
		{name: "multiple attempts", options: commandOptions{secondAttempt: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := fixtureLaunchFacts(2)
			target := addCompleteBind(&facts, 1, bindOptions{})
			observer := newTestObserver(t, checkpointWorkspace, func() time.Duration { return 7 * time.Millisecond })
			bind := requireNext(t, observer, facts)
			if bind.Target != target {
				t.Fatalf("bind target = %+v, want %+v", bind.Target, target)
			}
			observer.phase++
			addCommand(&facts, 1, test.options)
			if got := nextCheckpoint(t, observer, facts); got != nil {
				t.Fatalf("invalid command emitted %+v", *got)
			}
		})
	}
}

func TestCheckpointObserverFailsClosedOnLaunchPrerequisiteDrift(t *testing.T) {
	t.Run("missing prior launch", func(t *testing.T) {
		facts := fixtureLaunchFacts(1)
		addCompleteBind(&facts, 0, bindOptions{})
		observer := newTestObserver(t, checkpointWorkspace, func() time.Duration { return 0 })
		if got := nextCheckpoint(t, observer, facts); got != nil {
			t.Fatalf("missing exited ordinal emitted %+v", *got)
		}
	})

	t.Run("reordered launch facts", func(t *testing.T) {
		facts := fixtureLaunchFacts(2)
		created := factIndex(facts, domain.FactSessionCreated, checkpointLaunches[1].session)
		// The generated trio is session.created, instance.started,
		// session.launched. Swap the latter two.
		facts[created+1], facts[created+2] = facts[created+2], facts[created+1]
		observer := newTestObserver(t, checkpointWorkspace, func() time.Duration { return 0 })
		if _, err := observer.next(facts); err == nil || !strings.Contains(err.Error(), "reordered launch prerequisites") {
			t.Fatalf("reordered facts error = %v", err)
		}
	})

	t.Run("duplicate creation", func(t *testing.T) {
		facts := fixtureLaunchFacts(2)
		index := factIndex(facts, domain.FactSessionCreated, checkpointLaunches[1].session)
		facts = append(facts, facts[index])
		observer := newTestObserver(t, checkpointWorkspace, func() time.Duration { return 0 })
		if _, err := observer.next(facts); err == nil || !strings.Contains(err.Error(), "duplicate session.created") {
			t.Fatalf("duplicate facts error = %v", err)
		}
	})

	t.Run("ambiguous extra launch", func(t *testing.T) {
		facts := fixtureLaunchFacts(3)
		appendLaunch(&facts, "ses_extra", "ins_extra")
		observer := newTestObserver(t, checkpointWorkspace, func() time.Duration { return 0 })
		if _, err := observer.next(facts); err == nil || !strings.Contains(err.Error(), "ambiguous extra fixture launch") {
			t.Fatalf("extra launch error = %v", err)
		}
	})
}

func TestCheckpointObserverCancellationDoesNotBlock(t *testing.T) {
	observer := newTestObserver(t, checkpointWorkspace, func() time.Duration { return 0 })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- observer.Observe(ctx, make(chan RunnerCheckpoint))
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Observe cancellation error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Observe did not return after cancellation")
	}
}

func TestAuthoritySnapshotLoaderTreatsMissingStoreAsNoFacts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "duo.db")
	facts, err := NewAuthoritySnapshotLoader(path)(context.Background())
	if err != nil {
		t.Fatalf("missing authority store: %v", err)
	}
	if facts != nil {
		t.Fatalf("missing authority facts = %v, want nil", facts)
	}
	if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot loader created missing parent: %v", err)
	}
}

type offsetClock struct {
	offsets []time.Duration
	next    int
}

func (c *offsetClock) Now() time.Duration {
	value := c.offsets[c.next]
	c.next++
	return value
}

func newTestObserver(t *testing.T, workspace string, clock MonotonicClock) *CheckpointObserver {
	t.Helper()
	observer, err := NewCheckpointObserver(CheckpointObserverConfig{
		Load: func(context.Context) ([]domain.Fact, error) {
			return nil, nil
		},
		Monotonic:        clock,
		PollInterval:     time.Hour,
		FixtureWorkspace: workspace,
		HerdrIntegration: checkpointHerdr,
		PiIntegration:    checkpointPi,
	})
	if err != nil {
		t.Fatalf("NewCheckpointObserver: %v", err)
	}
	return observer
}

func nextCheckpoint(t *testing.T, observer *CheckpointObserver, facts []domain.Fact) *RunnerCheckpoint {
	t.Helper()
	checkpoint, err := observer.next(facts)
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	return checkpoint
}

func requireNext(t *testing.T, observer *CheckpointObserver, facts []domain.Fact) *RunnerCheckpoint {
	t.Helper()
	checkpoint := nextCheckpoint(t, observer, facts)
	if checkpoint == nil {
		t.Fatal("next returned no checkpoint")
	}
	return checkpoint
}

func assertCheckpoint(
	t *testing.T,
	got *RunnerCheckpoint,
	kind RunnerCheckpointKind,
	caseName RunnerCase,
	target ProcessIdentity,
	offset int64,
) {
	t.Helper()
	if got.Kind != kind || got.Case != caseName || got.Target != target || got.MonotonicMS != offset {
		t.Fatalf("checkpoint = %+v, want kind=%s case=%s target=%+v offset=%d", got, kind, caseName, target, offset)
	}
}

func fixtureLaunchFacts(count int) []domain.Fact {
	workspace := domain.Workspace{ID: "ws_fixture", RootPath: checkpointWorkspace, CreatedAt: "2099-01-01T00:00:00.000Z"}
	facts := []domain.Fact{{
		ID: "fact_workspace", Kind: domain.FactWorkspaceCreated, At: workspace.CreatedAt, Workspace: &workspace,
	}}
	for index := 0; index < count; index++ {
		appendLaunch(&facts, checkpointLaunches[index].session, checkpointLaunches[index].instance)
	}
	return facts
}

func appendLaunch(facts *[]domain.Fact, sessionID domain.SessionID, instanceID domain.InstanceID) {
	session := domain.Session{
		ID: sessionID, Workspace: "ws_fixture", State: domain.SessionActive,
		Current: instanceID, CreatedAt: "2099-01-01T00:00:00.000Z",
	}
	instance := domain.RuntimeInstance{
		ID: instanceID, Session: sessionID, State: domain.InstanceStarting,
		StartedAt: "2099-01-01T00:00:00.000Z",
	}
	*facts = append(*facts,
		domain.Fact{ID: domain.FactID("fact_create_" + sessionID), Kind: domain.FactSessionCreated, At: session.CreatedAt, Reason: "launch", Session: &session},
		domain.Fact{ID: domain.FactID("fact_start_" + instanceID), Kind: domain.FactInstanceStarted, At: instance.StartedAt, SessionID: sessionID, Instance: &instance},
		domain.Fact{ID: domain.FactID("fact_launch_" + sessionID), Kind: domain.FactSessionLaunched, At: instance.StartedAt, SessionID: sessionID, InstanceID: instanceID},
		domain.Fact{ID: domain.FactID("fact_live_" + instanceID), Kind: domain.FactInstanceState, At: instance.StartedAt, InstanceID: instanceID, State: string(domain.InstanceLive)},
	)
}

type bindOptions struct {
	missingStart      bool
	herdr             string
	pi                string
	correlationStatus domain.CorrelationStatus
	omitHostClaim     bool
	omitAgentClaim    bool
}

func addCompleteBind(facts *[]domain.Fact, launch int, options bindOptions) ProcessIdentity {
	sessionID := checkpointLaunches[launch].session
	instanceID := checkpointLaunches[launch].instance
	herdr := options.herdr
	if herdr == "" {
		herdr = checkpointHerdr
	}
	pi := options.pi
	if pi == "" {
		pi = checkpointPi
	}
	status := options.correlationStatus
	if status == "" {
		status = domain.CorrelationActive
	}
	started := "birth-" + string(sessionID)
	if options.missingStart {
		started = ""
	}
	attachment := domain.HostAttachment{
		ID: domain.AttachmentID("att_" + sessionID), Session: sessionID,
		IntegrationInstance: herdr,
		Epoch: domain.HostEpoch{
			Kind: "herdr.terminal_id", Value: "terminal-" + string(sessionID), Scope: domain.EpochScopePane,
		},
		Container: "pane-" + string(sessionID),
		Process: domain.ProcessBirth{
			PID: 1000 + launch, StartedAt: started, Executable: "/fixture/bin/pi",
		},
		State: domain.Attached,
	}
	*facts = append(*facts, domain.Fact{
		ID: domain.FactID("fact_attachment_" + sessionID), Kind: domain.FactAttachmentCreated,
		SessionID: sessionID, Attachment: &attachment,
	})
	fingerprint := domain.Fingerprint{
		IntegrationInstance: attachment.IntegrationInstance,
		Epoch:               attachment.Epoch,
		Container:           attachment.Container,
		Process:             attachment.Process,
	}
	if !options.omitHostClaim {
		claim := domain.Claim{Ref: fingerprint.ClaimRef(), Session: sessionID, Instance: instanceID}
		*facts = append(*facts, domain.Fact{Kind: domain.FactClaimHeld, Claim: &claim})
	}
	correlation := domain.Correlation{
		ID:         "cor_agent_" + domain.CorrelationID(sessionID),
		TargetKind: domain.TargetInstance, TargetID: string(instanceID),
		ExternalKind: "agent.session", ExternalValue: "pi-session-" + string(sessionID),
		Scope: pi, Status: status,
	}
	*facts = append(*facts, domain.Fact{Kind: domain.FactCorrelationClaimed, Correlation: &correlation})
	if !options.omitAgentClaim {
		agentRef := domain.AgentSessionRef{IntegrationInstance: pi, SessionID: correlation.ExternalValue}
		claim := domain.Claim{Ref: agentRef.ClaimRef(), Session: sessionID, Instance: instanceID}
		*facts = append(*facts, domain.Fact{Kind: domain.FactClaimHeld, Claim: &claim})
	}
	return ProcessIdentity{
		PID: attachment.Process.PID, StartTime: attachment.Process.StartedAt,
		TerminalID: attachment.Epoch.Value, AttachmentID: string(attachment.ID),
	}
}

func addSecondAttachment(facts *[]domain.Fact) {
	sessionID := checkpointLaunches[1].session
	attachment := domain.HostAttachment{
		ID:                  "att_second",
		Session:             sessionID,
		IntegrationInstance: checkpointHerdr,
		Epoch: domain.HostEpoch{
			Kind: "herdr.terminal_id", Value: "terminal-second", Scope: domain.EpochScopePane,
		},
		Container: "pane-second",
		Process: domain.ProcessBirth{
			PID: 2002, StartedAt: "birth-second", Executable: "/fixture/bin/pi",
		},
		State: domain.Attached,
	}
	*facts = append(*facts, domain.Fact{
		Kind: domain.FactAttachmentCreated, SessionID: sessionID, Attachment: &attachment,
	})
}

func addSecondLiveInstance(facts *[]domain.Fact) {
	sessionID := checkpointLaunches[1].session
	instanceID := domain.InstanceID("ins_second_live")
	instance := domain.RuntimeInstance{ID: instanceID, Session: sessionID, State: domain.InstanceLive}
	*facts = append(*facts, domain.Fact{
		Kind: domain.FactInstanceStarted, SessionID: sessionID, Instance: &instance,
	})
}

type commandOptions struct {
	acceptedOnly  bool
	attemptedOnly bool
	key           string
	digest        string
	instance      domain.InstanceID
	path          domain.PromptPathKind
	secondAttempt bool
}

func addCommand(facts *[]domain.Fact, launch int, options commandOptions) {
	sessionID := checkpointLaunches[launch].session
	instanceID := options.instance
	if instanceID == "" {
		instanceID = checkpointLaunches[launch].instance
	}
	key := options.key
	if key == "" {
		key = CanonicalScenario().Fixture.IdempotencyKey
	}
	digest := options.digest
	if digest == "" {
		digest = PromptDigest
	}
	path := options.path
	if path == "" {
		path = domain.PromptPathRuntime
	}
	commandID := domain.CommandID("cmd_" + sessionID)
	attemptID := domain.AttemptID("try_" + sessionID)
	accepted := domain.CommandFact{
		ID: commandID, Revision: 1, Operation: domain.PromptDeliverOperation,
		Session: sessionID, Instance: instanceID, Caller: "outer-agent",
		IdempotencyKey: key, CanonicalDigest: digest,
		QueuePolicy: domain.QueueUntilSafe, ExpiresAt: "2099-01-01T00:15:00.000Z",
		State: domain.ResponsibilityQueued, AcceptedAt: "2099-01-01T00:00:00.000Z",
	}
	*facts = append(*facts, domain.Fact{
		Kind: domain.FactCommandAccepted, SessionID: sessionID, InstanceID: instanceID, Command: &accepted,
	})
	if options.acceptedOnly {
		return
	}
	attempt := domain.CommandAttempt{ID: attemptID, PathKind: path, StartedAt: "2099-01-01T00:00:01.000Z"}
	attempted := accepted
	attempted.Revision = 2
	attempted.State = domain.ResponsibilityAttempting
	attempted.PathKind = path
	attempted.Attempt = &attempt
	*facts = append(*facts, domain.Fact{
		Kind: domain.FactCommandAttemptCreated, SessionID: sessionID, InstanceID: instanceID, Command: &attempted,
	})
	if options.secondAttempt {
		second := attempt
		second.ID = "try_second"
		secondFact := attempted
		secondFact.Revision = 3
		secondFact.Attempt = &second
		*facts = append(*facts, domain.Fact{
			Kind: domain.FactCommandAttemptCreated, SessionID: sessionID, InstanceID: instanceID, Command: &secondFact,
		})
	}
	if options.attemptedOnly {
		return
	}
	deliveredAttempt := attempt
	deliveredAttempt.RecordedResult = string(domain.ResponsibilityDelivered)
	delivered := accepted
	delivered.Revision = 3
	delivered.State = domain.ResponsibilityDelivered
	delivered.TerminalAt = "2099-01-01T00:00:02.000Z"
	delivered.Attempt = &deliveredAttempt
	*facts = append(*facts, domain.Fact{
		Kind: domain.FactCommandDelivered, SessionID: sessionID, InstanceID: instanceID, Command: &delivered,
	})
}

func factIndex(facts []domain.Fact, kind domain.FactKind, session domain.SessionID) int {
	for index, fact := range facts {
		if fact.Kind == kind && fact.Session != nil && fact.Session.ID == session {
			return index
		}
	}
	return -1
}
