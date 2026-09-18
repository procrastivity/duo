package portablelauncher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/store"
)

// AuthoritySnapshotLoader returns the complete durable fact log in commit
// order. An injected loader keeps observer tests independent of SQLite and of
// every live host or runtime process.
type AuthoritySnapshotLoader func(context.Context) ([]domain.Fact, error)

// MonotonicClock returns an offset from the beginning of the run. It must be
// backed by a monotonic clock; authority fact wall timestamps are never used
// for controller ordering.
type MonotonicClock func() time.Duration

// CheckpointObserverConfig is the complete trusted input to the authority
// observer. Integration values are exact configured instances, not kinds.
type CheckpointObserverConfig struct {
	Load             AuthoritySnapshotLoader
	Monotonic        MonotonicClock
	PollInterval     time.Duration
	FixtureWorkspace string
	HerdrIntegration string
	PiIntegration    string
}

// CheckpointObserver projects durable authority facts into the two raw facts
// RunCommon understands. It owns no controller actions and no verdicts.
type CheckpointObserver struct {
	config  CheckpointObserverConfig
	phase   int
	targets map[RunnerCase]ProcessIdentity
}

// NewCheckpointObserver validates and constructs an authority-fact observer.
func NewCheckpointObserver(config CheckpointObserverConfig) (*CheckpointObserver, error) {
	switch {
	case config.Load == nil:
		return nil, fmt.Errorf("checkpoint observer requires a snapshot loader")
	case config.Monotonic == nil:
		return nil, fmt.Errorf("checkpoint observer requires a monotonic clock")
	case config.PollInterval <= 0:
		return nil, fmt.Errorf("checkpoint observer requires a positive poll interval")
	case config.FixtureWorkspace == "":
		return nil, fmt.Errorf("checkpoint observer requires the fixture workspace")
	case config.HerdrIntegration == "":
		return nil, fmt.Errorf("checkpoint observer requires the exact Herdr integration identity")
	case config.PiIntegration == "":
		return nil, fmt.Errorf("checkpoint observer requires the exact Pi integration identity")
	}
	return &CheckpointObserver{config: config, targets: make(map[RunnerCase]ProcessIdentity, 2)}, nil
}

// NewAuthoritySnapshotLoader builds the production loader. Every snapshot is
// opened mode=ro/query_only, replayed, and closed; it never acquires the
// authority lease, invokes a domain mutator, or shells out to a CLI.
func NewAuthoritySnapshotLoader(path string) AuthoritySnapshotLoader {
	return func(ctx context.Context) ([]domain.Fact, error) {
		if _, err := os.Stat(path); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, nil
			}
			return nil, fmt.Errorf("checkpoint observer inspecting authority store: %w", err)
		}
		s, err := store.OpenReadOnly(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = s.Close() }()
		return storerepo.New(s).Load(ctx)
	}
}

// Observe polls until all four raw checkpoints have been emitted or ctx is
// canceled. It emits at most one checkpoint from any one snapshot, which
// gives RunCommon a control boundary before the next authority observation.
// No goroutine is created here; cancellation stops the ticker and every send.
func (o *CheckpointObserver) Observe(ctx context.Context, output chan<- RunnerCheckpoint) error {
	ticker := time.NewTicker(o.config.PollInterval)
	defer ticker.Stop()

	for {
		facts, err := o.config.Load(ctx)
		if err != nil {
			return fmt.Errorf("checkpoint observer loading authority snapshot: %w", err)
		}
		checkpoint, err := o.next(facts)
		if err != nil {
			return err
		}
		if checkpoint != nil {
			select {
			case output <- *checkpoint:
				o.phase++
				if o.phase == len(orderedControlCheckpoints) {
					return nil
				}
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (o *CheckpointObserver) next(facts []domain.Fact) (*RunnerCheckpoint, error) {
	if o.phase >= len(orderedControlCheckpoints) {
		return nil, nil
	}
	projection, err := projectSnapshot(facts)
	if err != nil {
		return nil, fmt.Errorf("checkpoint observer projecting authority snapshot: %w", err)
	}
	caseName := orderedControlCheckpoints[o.phase].caseName
	session, ready, err := o.sessionForCase(projection, caseName)
	if err != nil || !ready {
		return nil, err
	}
	target, ready, err := o.boundTarget(projection, session)
	if err != nil || !ready {
		return nil, err
	}

	want := orderedControlCheckpoints[o.phase]
	if want.kind == CheckpointCommandDelivered {
		if target != o.targets[caseName] {
			return nil, fmt.Errorf("checkpoint observer: %s process identity changed after bind", caseName)
		}
		delivered, err := commandDelivered(projection, session, PromptDigest, CanonicalScenario().Fixture.IdempotencyKey)
		if err != nil || !delivered {
			return nil, err
		}
	} else {
		o.targets[caseName] = target
	}

	offset := o.config.Monotonic()
	if offset < 0 {
		return nil, fmt.Errorf("checkpoint observer monotonic offset is negative")
	}
	return &RunnerCheckpoint{
		Kind: want.kind, Case: caseName, Target: target, MonotonicMS: offset.Milliseconds(),
	}, nil
}

type snapshotProjection struct {
	facts     []domain.Fact
	authority *domain.Authority
}

func projectSnapshot(facts []domain.Fact) (snapshotProjection, error) {
	repo := snapshotRepository{facts: facts}
	authority, err := domain.Open(context.Background(), repo)
	if err != nil {
		return snapshotProjection{}, err
	}
	return snapshotProjection{facts: facts, authority: authority}, nil
}

type snapshotRepository struct {
	facts []domain.Fact
}

func (r snapshotRepository) Load(context.Context) ([]domain.Fact, error) {
	return r.facts, nil
}

func (snapshotRepository) CommitIdentity(context.Context, domain.Change) error {
	return errors.New("checkpoint snapshot repository is read-only")
}

func (snapshotRepository) CommitLaunchResolution(context.Context, domain.Change) error {
	return errors.New("checkpoint snapshot repository is read-only")
}

func (snapshotRepository) CommitCommandAcceptance(context.Context, domain.Change) error {
	return errors.New("checkpoint snapshot repository is read-only")
}

func (snapshotRepository) CommitCommandTransition(context.Context, domain.Change) error {
	return errors.New("checkpoint snapshot repository is read-only")
}

func (snapshotRepository) CommitObservation(context.Context, domain.Change) error {
	return errors.New("checkpoint snapshot repository is read-only")
}

type launchFact struct {
	session  domain.SessionID
	instance domain.InstanceID
	index    int
}

func (o *CheckpointObserver) sessionForCase(projection snapshotProjection, caseName RunnerCase) (domain.SessionID, bool, error) {
	ordinal, ok := canonicalLaunchOrdinal(caseName)
	if !ok {
		return "", false, fmt.Errorf("checkpoint observer has no canonical launch ordinal for %s", caseName)
	}
	launches, complete, err := o.orderedFixtureLaunches(projection)
	if err != nil {
		return "", false, err
	}
	if !complete || len(launches) <= ordinal {
		return "", false, nil
	}
	return launches[ordinal].session, true, nil
}

// canonicalLaunchOrdinal is the single intentional ordinal bridge between
// durable sessions and scenario cases. The authority has no durable case
// field, so opaque IDs must never be sorted or interpreted: the nth valid
// fixture launch is mapped to the nth canonical runnable launch. A case whose
// prerequisite is explicitly unavailable cannot create a session and does
// not consume an ordinal. A future durable case field should replace only
// this function and its caller.
func canonicalLaunchOrdinal(caseName RunnerCase) (int, bool) {
	ordinal := 0
	for _, group := range caseMatrix {
		hasLaunch := false
		for _, stage := range group.Stages {
			if stage == "launch" {
				hasLaunch = true
				break
			}
		}
		if !hasLaunch {
			continue
		}
		if group.Case == string(RunnerCaseBlocked) && blockedPrerequisiteUnavailable() {
			continue
		}
		if group.Case == string(caseName) {
			return ordinal, true
		}
		ordinal++
	}
	return 0, false
}

func canonicalLaunchCount() int {
	count := 0
	for _, group := range caseMatrix {
		if group.Case == string(RunnerCaseBlocked) && blockedPrerequisiteUnavailable() {
			continue
		}
		for _, stage := range group.Stages {
			if stage == "launch" {
				count++
				break
			}
		}
	}
	return count
}

func blockedPrerequisiteUnavailable() bool {
	for _, prerequisite := range CanonicalScenario().Prerequisites {
		if prerequisite.Name == CanonicalOracle().BlockedPrerequisite {
			return prerequisite.Status == "unavailable"
		}
	}
	return false
}

func (o *CheckpointObserver) orderedFixtureLaunches(projection snapshotProjection) ([]launchFact, bool, error) {
	seen := make(map[domain.SessionID]struct{})
	workspaceIDs := make(map[domain.WorkspaceID]struct{})
	var launches []launchFact
	for index, fact := range projection.facts {
		if fact.Kind != domain.FactSessionCreated || fact.Session == nil {
			continue
		}
		id := fact.Session.ID
		if _, duplicate := seen[id]; duplicate {
			return nil, false, fmt.Errorf("checkpoint observer: duplicate session.created for %s", id)
		}
		seen[id] = struct{}{}
		workspace, ok := projection.authority.Workspace(fact.Session.Workspace)
		if !ok || workspace.RootPath != o.config.FixtureWorkspace {
			continue
		}
		if fact.Reason != "launch" || fact.Session.Current == "" {
			return nil, false, fmt.Errorf("checkpoint observer: ambiguous non-launch session %s in fixture workspace", id)
		}
		workspaceIDs[fact.Session.Workspace] = struct{}{}
		launches = append(launches, launchFact{session: id, instance: fact.Session.Current, index: index})
	}
	if len(workspaceIDs) > 1 {
		return nil, false, fmt.Errorf("checkpoint observer: fixture workspace resolves to multiple durable workspace IDs")
	}
	if len(launches) > canonicalLaunchCount() {
		return nil, false, fmt.Errorf("checkpoint observer: ambiguous extra fixture launch")
	}
	for _, launch := range launches {
		instanceAt, launchedAt := -1, -1
		instanceCount, launchedCount := 0, 0
		for index, fact := range projection.facts {
			switch {
			case fact.Kind == domain.FactInstanceStarted && fact.Instance != nil && fact.Instance.ID == launch.instance:
				if fact.Instance.Session != launch.session || fact.SessionID != launch.session {
					return nil, false, fmt.Errorf("checkpoint observer: launch instance %s has inconsistent session", launch.instance)
				}
				instanceAt, instanceCount = index, instanceCount+1
			case fact.Kind == domain.FactSessionLaunched && fact.SessionID == launch.session:
				if fact.InstanceID != launch.instance {
					return nil, false, fmt.Errorf("checkpoint observer: session %s launch names the wrong instance", launch.session)
				}
				launchedAt, launchedCount = index, launchedCount+1
			}
		}
		if instanceCount == 0 || launchedCount == 0 {
			return launches, false, nil
		}
		if instanceCount != 1 || launchedCount != 1 {
			return nil, false, fmt.Errorf("checkpoint observer: duplicated launch prerequisites for %s", launch.session)
		}
		if launch.index >= instanceAt || instanceAt >= launchedAt {
			return nil, false, fmt.Errorf("checkpoint observer: reordered launch prerequisites for %s", launch.session)
		}
	}
	return launches, true, nil
}

func (o *CheckpointObserver) boundTarget(projection snapshotProjection, sessionID domain.SessionID) (ProcessIdentity, bool, error) {
	session, ok := projection.authority.Session(sessionID)
	if !ok || session.Current == "" {
		return ProcessIdentity{}, false, nil
	}
	live := 0
	seenInstances := make(map[domain.InstanceID]struct{})
	for _, fact := range projection.facts {
		if fact.Kind != domain.FactInstanceStarted || fact.Instance == nil || fact.Instance.Session != sessionID {
			continue
		}
		if _, duplicate := seenInstances[fact.Instance.ID]; duplicate {
			return ProcessIdentity{}, false, fmt.Errorf("checkpoint observer: duplicate instance.started for %s", fact.Instance.ID)
		}
		seenInstances[fact.Instance.ID] = struct{}{}
		instance, exists := projection.authority.Instance(fact.Instance.ID)
		if exists && instance.State == domain.InstanceLive {
			live++
			if instance.ID != session.Current {
				return ProcessIdentity{}, false, nil
			}
		}
	}
	current, ok := projection.authority.Instance(session.Current)
	if !ok || current.Session != sessionID || current.State != domain.InstanceLive || live != 1 {
		return ProcessIdentity{}, false, nil
	}

	attachments := projection.authority.Attachments(sessionID)
	if len(attachments) != 1 {
		return ProcessIdentity{}, false, nil
	}
	attachment := attachments[0]
	if session.Attachment != attachment.ID || attachment.State != domain.Attached ||
		attachment.Continuity != domain.ContinuityVerified ||
		attachment.IntegrationInstance != o.config.HerdrIntegration ||
		attachment.Epoch.Kind != "herdr.terminal_id" || attachment.Epoch.Scope != domain.EpochScopePane ||
		attachment.Epoch.Value == "" || attachment.Container == "" || !attachment.Process.Present() {
		return ProcessIdentity{}, false, nil
	}
	hostFingerprint := domain.Fingerprint{
		IntegrationInstance: attachment.IntegrationInstance,
		Epoch:               attachment.Epoch,
		Container:           attachment.Container,
		Process:             attachment.Process,
	}
	hostClaim, held := projection.authority.ActiveClaim(hostFingerprint.ClaimRef())
	if !held || hostClaim.Session != sessionID || hostClaim.Instance != current.ID {
		return ProcessIdentity{}, false, nil
	}

	var agent *domain.Correlation
	for _, correlation := range projection.authority.Correlations(domain.TargetInstance, string(current.ID)) {
		if correlation.ExternalKind != "agent.session" || correlation.Status != domain.CorrelationActive {
			continue
		}
		if agent != nil {
			return ProcessIdentity{}, false, nil
		}
		correlationCopy := correlation
		agent = &correlationCopy
	}
	if agent == nil || agent.Scope != o.config.PiIntegration || agent.ExternalValue == "" {
		return ProcessIdentity{}, false, nil
	}
	agentRef := domain.AgentSessionRef{IntegrationInstance: agent.Scope, SessionID: agent.ExternalValue}
	agentClaim, held := projection.authority.ActiveClaim(agentRef.ClaimRef())
	if !held || agentClaim.Session != sessionID || agentClaim.Instance != current.ID {
		return ProcessIdentity{}, false, nil
	}

	return ProcessIdentity{
		PID: attachment.Process.PID, StartTime: attachment.Process.StartedAt,
		TerminalID: attachment.Epoch.Value, AttachmentID: string(attachment.ID),
	}, true, nil
}

func commandDelivered(
	projection snapshotProjection,
	sessionID domain.SessionID,
	digest, key string,
) (bool, error) {
	var matches []domain.PromptCommand
	seen := make(map[domain.CommandID]struct{})
	for _, fact := range projection.facts {
		if fact.Kind != domain.FactCommandAccepted || fact.Command == nil {
			continue
		}
		id := fact.Command.ID
		if _, duplicate := seen[id]; duplicate {
			return false, fmt.Errorf("checkpoint observer: duplicate command.accepted for %s", id)
		}
		seen[id] = struct{}{}
		command, ok := projection.authority.Command(id)
		if !ok || command.Session != sessionID || command.Instance != fact.Command.Instance {
			continue
		}
		if command.Operation == domain.PromptDeliverOperation && command.IdempotencyKey == key && command.CanonicalDigest == digest {
			matches = append(matches, command)
		}
	}
	if len(matches) == 0 {
		return false, nil
	}
	if len(matches) != 1 {
		return false, fmt.Errorf("checkpoint observer: ambiguous canonical commands for session %s", sessionID)
	}
	command := matches[0]
	session, ok := projection.authority.Session(sessionID)
	if !ok || command.Instance != session.Current || command.State != domain.ResponsibilityDelivered ||
		len(command.Attempts) != 1 || command.Attempts[0].PathKind != domain.PromptPathRuntime ||
		command.Attempts[0].RecordedResult != string(domain.ResponsibilityDelivered) {
		return false, nil
	}
	acceptedAt, attemptAt, deliveredAt := -1, -1, -1
	acceptedCount, attemptCount, deliveredCount := 0, 0, 0
	for index, fact := range projection.facts {
		if fact.Command == nil || fact.Command.ID != command.ID {
			continue
		}
		if fact.SessionID != sessionID || fact.InstanceID != command.Instance {
			return false, fmt.Errorf("checkpoint observer: command %s fact target changed", command.ID)
		}
		switch fact.Kind {
		case domain.FactCommandAccepted:
			acceptedAt, acceptedCount = index, acceptedCount+1
		case domain.FactCommandAttemptCreated:
			if fact.Command.Attempt == nil || fact.Command.Attempt.ID != command.Attempts[0].ID ||
				fact.Command.Attempt.PathKind != domain.PromptPathRuntime {
				return false, nil
			}
			attemptAt, attemptCount = index, attemptCount+1
		case domain.FactCommandDelivered:
			if fact.Command.State != domain.ResponsibilityDelivered || fact.Command.Attempt == nil ||
				fact.Command.Attempt.ID != command.Attempts[0].ID ||
				fact.Command.Attempt.PathKind != domain.PromptPathRuntime ||
				fact.Command.Attempt.RecordedResult != string(domain.ResponsibilityDelivered) {
				return false, nil
			}
			deliveredAt, deliveredCount = index, deliveredCount+1
		}
	}
	if acceptedCount != 1 || attemptCount != 1 || deliveredCount != 1 {
		if acceptedCount > 1 || attemptCount > 1 || deliveredCount > 1 {
			return false, fmt.Errorf("checkpoint observer: duplicated command lifecycle for %s", command.ID)
		}
		return false, nil
	}
	if acceptedAt >= attemptAt || attemptAt >= deliveredAt {
		return false, fmt.Errorf("checkpoint observer: reordered command lifecycle for %s", command.ID)
	}
	return true, nil
}
