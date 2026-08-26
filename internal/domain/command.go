package domain

import (
	"context"
	"fmt"
	"time"
)

// PromptDeliverOperation is the sole command operation this kernel accepts.
const PromptDeliverOperation = "prompt.deliver"

// DefaultPromptExpiry is the projection default when a caller omits
// expires_at. Every accepted command still has a finite lifetime
// (decision-03 §5.2); there is no unlimited queue entry.
const DefaultPromptExpiry = 15 * time.Minute

// ResponsibilityState is a prompt command's responsibility lifecycle
// (decision-03 §4.1, schema $defs.responsibility_state). canceled is in the
// schema and out of this slice (cancel travels with stop/interrupt).
type ResponsibilityState string

// Nonterminal and terminal responsibility states this kernel records.
const (
	ResponsibilityAccepted   ResponsibilityState = "accepted"
	ResponsibilityQueued     ResponsibilityState = "queued"
	ResponsibilityAttempting ResponsibilityState = "attempting"
	ResponsibilityDelivered  ResponsibilityState = "delivered"
	ResponsibilityExpired    ResponsibilityState = "expired"
	ResponsibilityFailed     ResponsibilityState = "failed"
)

// Terminal reports whether s is a durable terminal responsibility state.
func (s ResponsibilityState) Terminal() bool {
	return s == ResponsibilityDelivered || s == ResponsibilityExpired || s == ResponsibilityFailed
}

// QueuePolicy is the schema $defs.queue_policy vocabulary. This slice
// accepts only queue_until_safe.
type QueuePolicy string

// QueueUntilSafe is the only accepted prompt queue policy in this slice.
const QueueUntilSafe QueuePolicy = "queue_until_safe"

// EffectCertainty is decision-03 §4.3 / schema $defs.effect on unsuccessful
// attempts: no_effect (automatic retry permitted) or unknown_effect
// (automatic retry forbidden).
type EffectCertainty string

// Closed effect-certainty values.
const (
	EffectNoEffect      EffectCertainty = "no_effect"
	EffectUnknownEffect EffectCertainty = "unknown_effect"
)

// PromptPathKind is the selected adapter path recorded on an attempt. The
// kernel records the kind the composer chose; it does not call adapters.
// Prefer runtime (RuntimePromptProvider) when that offer meets quality;
// otherwise host (HostPromptProvider). See internal/promptpath.
type PromptPathKind string

// Closed path kinds this kernel will record.
const (
	PromptPathRuntime PromptPathKind = "runtime"
	PromptPathHost    PromptPathKind = "host"
)

// Valid reports whether k is runtime or host.
func (k PromptPathKind) Valid() bool {
	return k == PromptPathRuntime || k == PromptPathHost
}

// PromptCommand is one accepted prompt.deliver command as the kernel
// replays it. Identity, the bound runtime-instance ID, and the idempotency
// record are immutable after acceptance (I-5).
type PromptCommand struct {
	ID              CommandID
	Revision        int
	Operation       string
	Session         SessionID
	Instance        InstanceID
	Caller          string
	IdempotencyKey  string
	CanonicalDigest string
	QueuePolicy     QueuePolicy
	ExpiresAt       string
	State           ResponsibilityState
	Attempts        []CommandAttempt
	AcceptedAt      string
	TerminalAt      string
}

// CommandAttempt is one delivery attempt (decision-03 §4.1). PathKind is
// recorded at create, before any adapter call. EffectCertainty is set on
// unsuccessful reconciliation; it is empty on a delivered attempt.
type CommandAttempt struct {
	ID              AttemptID
	PathKind        PromptPathKind
	StartedAt       string
	RecordedResult  string
	EffectCertainty EffectCertainty
}

// CommandFact is the payload of every prompt-command fact. Acceptance
// carries the whole command; later facts name the command and, when
// relevant, the attempt they close or reconcile.
type CommandFact struct {
	ID              CommandID
	Revision        int
	Operation       string
	Session         SessionID
	Instance        InstanceID
	Caller          string
	IdempotencyKey  string
	CanonicalDigest string
	QueuePolicy     QueuePolicy
	ExpiresAt       string
	State           ResponsibilityState
	AcceptedAt      string
	TerminalAt      string
	PathKind        PromptPathKind
	Effect          EffectCertainty
	Attempt         *CommandAttempt
}

// AcceptPromptRequest is one prompt.deliver admission.
type AcceptPromptRequest struct {
	Session SessionID
	// Instance is the expected runtime-instance ID. Empty binds the
	// session's current instance. A non-empty value that does not match
	// current is refused; the command never rebinds (I-5, decision-03 G-04).
	Instance        InstanceID
	Actor           string
	IdempotencyKey  string
	CanonicalDigest string
	ExpiresAt       time.Time
	QueuePolicy     QueuePolicy
}

// AcceptPromptResult is what AcceptPrompt returns, including whether this
// call was a replay of an existing command.
type AcceptPromptResult struct {
	Command PromptCommand
	Replay  bool
}

// AcceptPrompt durably accepts one prompt.deliver command. Acceptance and
// the idempotency record commit in one CommandAcceptTx. Same key and same
// digest return the existing command; same key and a different digest is
// command.idempotency_conflict. The command is queued; this kernel does
// not wait for idle and does not invoke an adapter.
func (a *Authority) AcceptPrompt(ctx context.Context, req AcceptPromptRequest) (AcceptPromptResult, error) {
	actor := req.Actor
	if actor == "" {
		actor = "authority"
	}
	if req.IdempotencyKey == "" {
		return AcceptPromptResult{}, ErrIdempotencyKeyRequired
	}
	if req.CanonicalDigest == "" {
		return AcceptPromptResult{}, ErrCanonicalDigestRequired
	}
	policy := req.QueuePolicy
	if policy == "" {
		policy = QueueUntilSafe
	}
	if policy != QueueUntilSafe {
		return AcceptPromptResult{}, fmt.Errorf("%w: %s", ErrQueuePolicyUnsupported, policy)
	}
	session, err := a.requireSession(req.Session)
	if err != nil {
		return AcceptPromptResult{}, err
	}
	instanceID := req.Instance
	if instanceID == "" {
		instanceID = session.Current
	}
	if instanceID == "" {
		return AcceptPromptResult{}, fmt.Errorf("%w: session %s", ErrNoCurrentInstance, session.ID)
	}
	if req.Instance != "" && session.Current != "" && req.Instance != session.Current {
		return AcceptPromptResult{}, &ConflictError{
			Reason:  "expected runtime instance does not match the session's current instance; a prompt command never rebinds",
			Holders: []SessionID{session.ID},
		}
	}
	instance, err := a.requireInstance(instanceID)
	if err != nil {
		return AcceptPromptResult{}, err
	}
	if instance.Session != session.ID {
		return AcceptPromptResult{}, fmt.Errorf("%w: instance %s does not belong to session %s",
			ErrUnknownObject, instance.ID, session.ID)
	}
	if instance.State.Terminal() {
		return AcceptPromptResult{}, fmt.Errorf("%w: instance %s", ErrInstanceExited, instance.ID)
	}

	now := a.now().UTC()
	expiresAt := req.ExpiresAt.UTC()
	if expiresAt.IsZero() {
		expiresAt = now.Add(DefaultPromptExpiry)
	}
	if !expiresAt.After(now) {
		return AcceptPromptResult{}, ErrExpiryInPast
	}

	scope := idempotencyScope(actor, PromptDeliverOperation, string(session.ID), req.IdempotencyKey)
	if existingID, ok := a.idempotency[scope]; ok {
		existing, ok := a.commands[existingID]
		if !ok {
			return AcceptPromptResult{}, fmt.Errorf("%w: %s", ErrUnknownCommand, existingID)
		}
		if existing.CanonicalDigest != req.CanonicalDigest {
			return AcceptPromptResult{}, &IdempotencyConflictError{
				Key:             req.IdempotencyKey,
				ExistingCommand: existing.ID,
				ExistingDigest:  existing.CanonicalDigest,
				RequestDigest:   req.CanonicalDigest,
			}
		}
		return AcceptPromptResult{Command: copyCommand(existing), Replay: true}, nil
	}

	b := a.change(actor)
	id := CommandID(b.mint(commandPrefix))
	cmd := PromptCommand{
		ID:              id,
		Revision:        1,
		Operation:       PromptDeliverOperation,
		Session:         session.ID,
		Instance:        instance.ID,
		Caller:          actor,
		IdempotencyKey:  req.IdempotencyKey,
		CanonicalDigest: req.CanonicalDigest,
		QueuePolicy:     policy,
		ExpiresAt:       expiresAt.Format(timestampLayout),
		State:           ResponsibilityQueued,
		AcceptedAt:      b.at,
	}
	payload := commandFactFrom(cmd)
	b.fact(FactCommandAccepted, Fact{
		Command:    &payload,
		SessionID:  session.ID,
		InstanceID: instance.ID,
		Reason:     "prompt.deliver accepted",
	}).auditEntry(AuditEntry{
		Target: string(id),
		Reason: "prompt.deliver accepted",
		Detail: "idempotency_key=" + req.IdempotencyKey,
	})
	change, err := b.build()
	if err != nil {
		return AcceptPromptResult{}, err
	}
	if err := a.commit(ctx, a.repo.CommitCommandAcceptance, change); err != nil {
		return AcceptPromptResult{}, err
	}
	return AcceptPromptResult{Command: copyCommand(a.commands[id])}, nil
}

// CreateAttempt records one command-attempt in a CommandTransitionTx before
// any adapter is invoked. It refuses a terminal command, an in-flight
// attempt, and a due expiry (which it commits as expired instead). Path
// selection happens outside this kernel; path is recorded, not chosen.
func (a *Authority) CreateAttempt(ctx context.Context, id CommandID, actor string, path PromptPathKind) (AttemptID, error) {
	if actor == "" {
		actor = "authority"
	}
	if !path.Valid() {
		return "", ErrPromptPathKind
	}
	cmd, err := a.requireCommand(id)
	if err != nil {
		return "", err
	}
	if cmd.State.Terminal() {
		return "", fmt.Errorf("%w: %s is %s", ErrCommandTerminal, id, cmd.State)
	}
	if cmd.State == ResponsibilityAttempting {
		return "", fmt.Errorf("%w: %s", ErrCommandAttempting, id)
	}
	if a.expired(cmd) {
		if err := a.commitExpired(ctx, cmd, actor, "expires_at passed before an attempt"); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: %s", ErrCommandExpired, id)
	}
	instance, err := a.requireInstance(cmd.Instance)
	if err != nil {
		return "", err
	}
	if instance.State.Terminal() {
		if err := a.commitFailed(ctx, cmd, nil, actor, EffectNoEffect, "bound runtime instance has exited"); err != nil {
			return "", err
		}
		return "", fmt.Errorf("%w: instance %s", ErrInstanceExited, cmd.Instance)
	}

	b := a.change(actor)
	attemptID := AttemptID(b.mint(attemptPrefix))
	attempt := CommandAttempt{
		ID:        attemptID,
		PathKind:  path,
		StartedAt: b.at,
	}
	next := copyCommand(cmd)
	next.Revision++
	next.State = ResponsibilityAttempting
	payload := commandFactFrom(next)
	payload.PathKind = path
	payload.Attempt = &attempt
	b.fact(FactCommandAttemptCreated, Fact{
		Command:    &payload,
		SessionID:  cmd.Session,
		InstanceID: cmd.Instance,
		Reason:     "attempt created before adapter call",
	}).auditEntry(AuditEntry{
		Target: string(id),
		Reason: "command attempt created",
		Detail: "path=" + string(path),
	})
	change, err := b.build()
	if err != nil {
		return "", err
	}
	if err := a.commit(ctx, a.repo.CommitCommandTransition, change); err != nil {
		return "", err
	}
	return attemptID, nil
}

// CommitDelivered records terminal delivered after the adapter returns and
// before the result is visible. The command must be attempting.
func (a *Authority) CommitDelivered(ctx context.Context, id CommandID, attempt AttemptID, actor string) error {
	if actor == "" {
		actor = "authority"
	}
	cmd, attemptRec, err := a.requireAttempting(id, attempt)
	if err != nil {
		return err
	}
	b := a.change(actor)
	next := copyCommand(cmd)
	next.Revision++
	next.State = ResponsibilityDelivered
	next.TerminalAt = b.at
	closed := *attemptRec
	closed.RecordedResult = string(ResponsibilityDelivered)
	payload := commandFactFrom(next)
	payload.Attempt = &closed
	b.fact(FactCommandDelivered, Fact{
		Command:    &payload,
		SessionID:  cmd.Session,
		InstanceID: cmd.Instance,
		Reason:     "path met complete-turn success",
	}).auditEntry(AuditEntry{Target: string(id), Reason: "command delivered"})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitCommandTransition, change)
}

// ExpireIfDue commits terminal expired when expires_at has passed and no
// attempt is in flight. It is a no-op when the command is still inside its
// deadline or already terminal.
func (a *Authority) ExpireIfDue(ctx context.Context, id CommandID, actor string) (bool, error) {
	if actor == "" {
		actor = "authority"
	}
	cmd, err := a.requireCommand(id)
	if err != nil {
		return false, err
	}
	if cmd.State.Terminal() {
		return cmd.State == ResponsibilityExpired, nil
	}
	if cmd.State == ResponsibilityAttempting {
		return false, nil
	}
	if !a.expired(cmd) {
		return false, nil
	}
	if err := a.commitExpired(ctx, cmd, actor, "expires_at passed before an attempt"); err != nil {
		return false, err
	}
	return true, nil
}

// ReconcileAttempt closes an in-flight attempt after a crash (or an adapter
// return the caller has not yet committed). provedNoEffect records no_effect
// and requeues; otherwise the command fails as unknown_effect and must not
// grow another attempt.
func (a *Authority) ReconcileAttempt(ctx context.Context, id CommandID, attempt AttemptID, actor string, provedNoEffect bool) error {
	if actor == "" {
		actor = "authority"
	}
	cmd, attemptRec, err := a.requireAttempting(id, attempt)
	if err != nil {
		return err
	}
	if provedNoEffect {
		if a.expired(cmd) {
			return a.commitFailed(ctx, cmd, attemptRec, actor, EffectNoEffect, "expired during a proved no-effect attempt")
		}
		return a.commitRequeued(ctx, cmd, attemptRec, actor)
	}
	return a.commitFailed(ctx, cmd, attemptRec, actor, EffectUnknownEffect, "in-flight attempt reconciled without no-effect proof")
}

// Command returns one prompt command by ID.
func (a *Authority) Command(id CommandID) (PromptCommand, bool) {
	c, ok := a.commands[id]
	if !ok {
		return PromptCommand{}, false
	}
	return copyCommand(c), true
}

// CommandByIdempotency returns the command bound to one caller key in the
// (caller, operation, target) scope, if any.
func (a *Authority) CommandByIdempotency(caller, targetSession, key string) (PromptCommand, bool) {
	id, ok := a.idempotency[idempotencyScope(caller, PromptDeliverOperation, targetSession, key)]
	if !ok {
		return PromptCommand{}, false
	}
	return a.Command(id)
}

func (a *Authority) requireCommand(id CommandID) (*PromptCommand, error) {
	c, ok := a.commands[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownCommand, id)
	}
	return c, nil
}

func (a *Authority) requireAttempting(id CommandID, attempt AttemptID) (*PromptCommand, *CommandAttempt, error) {
	cmd, err := a.requireCommand(id)
	if err != nil {
		return nil, nil, err
	}
	if cmd.State != ResponsibilityAttempting {
		return nil, nil, fmt.Errorf("%w: %s is %s", ErrCommandNotAttempting, id, cmd.State)
	}
	for i := range cmd.Attempts {
		if cmd.Attempts[i].ID == attempt {
			return cmd, &cmd.Attempts[i], nil
		}
	}
	return nil, nil, fmt.Errorf("%w: attempt %s is not on command %s", ErrUnknownCommand, attempt, id)
}

func (a *Authority) expired(cmd *PromptCommand) bool {
	expires, err := time.Parse(timestampLayout, cmd.ExpiresAt)
	if err != nil {
		return false
	}
	return !expires.After(a.now().UTC())
}

func (a *Authority) commitExpired(ctx context.Context, cmd *PromptCommand, actor, reason string) error {
	b := a.change(actor)
	next := copyCommand(cmd)
	next.Revision++
	next.State = ResponsibilityExpired
	next.TerminalAt = b.at
	payload := commandFactFrom(next)
	b.fact(FactCommandExpired, Fact{
		Command:    &payload,
		SessionID:  cmd.Session,
		InstanceID: cmd.Instance,
		Reason:     reason,
	}).auditEntry(AuditEntry{Target: string(cmd.ID), Reason: "command expired", Detail: reason})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitCommandTransition, change)
}

func (a *Authority) commitFailed(ctx context.Context, cmd *PromptCommand, attempt *CommandAttempt, actor string, effect EffectCertainty, reason string) error {
	b := a.change(actor)
	next := copyCommand(cmd)
	next.Revision++
	next.State = ResponsibilityFailed
	next.TerminalAt = b.at
	payload := commandFactFrom(next)
	payload.Effect = effect
	if attempt != nil {
		closed := *attempt
		closed.RecordedResult = string(ResponsibilityFailed)
		closed.EffectCertainty = effect
		payload.Attempt = &closed
	}
	b.fact(FactCommandFailed, Fact{
		Command:    &payload,
		SessionID:  cmd.Session,
		InstanceID: cmd.Instance,
		Reason:     reason,
	}).auditEntry(AuditEntry{Target: string(cmd.ID), Reason: "command failed", Detail: reason})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitCommandTransition, change)
}

func (a *Authority) commitRequeued(ctx context.Context, cmd *PromptCommand, attempt *CommandAttempt, actor string) error {
	b := a.change(actor)
	next := copyCommand(cmd)
	next.Revision++
	next.State = ResponsibilityQueued
	payload := commandFactFrom(next)
	payload.Effect = EffectNoEffect
	closed := *attempt
	closed.RecordedResult = string(EffectNoEffect)
	closed.EffectCertainty = EffectNoEffect
	payload.Attempt = &closed
	b.fact(FactCommandRequeued, Fact{
		Command:    &payload,
		SessionID:  cmd.Session,
		InstanceID: cmd.Instance,
		Reason:     "proved no_effect; retry permitted",
	}).auditEntry(AuditEntry{Target: string(cmd.ID), Reason: "command requeued", Detail: "no_effect"})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitCommandTransition, change)
}

func (a *Authority) applyCommand(f Fact) {
	if f.Command == nil {
		return
	}
	cf := f.Command
	switch f.Kind {
	case FactCommandAccepted:
		cmd := promptCommandFrom(*cf)
		a.commands[cmd.ID] = &cmd
		a.idempotency[idempotencyScope(cmd.Caller, cmd.Operation, string(cmd.Session), cmd.IdempotencyKey)] = cmd.ID
	case FactCommandAttemptCreated:
		cmd, ok := a.commands[cf.ID]
		if !ok || cf.Attempt == nil {
			return
		}
		cmd.Revision = cf.Revision
		cmd.State = ResponsibilityAttempting
		cmd.Attempts = append(cmd.Attempts, *cf.Attempt)
	case FactCommandDelivered, FactCommandExpired, FactCommandFailed, FactCommandRequeued:
		cmd, ok := a.commands[cf.ID]
		if !ok {
			return
		}
		cmd.Revision = cf.Revision
		cmd.State = cf.State
		cmd.TerminalAt = cf.TerminalAt
		if cf.Attempt != nil {
			for i := range cmd.Attempts {
				if cmd.Attempts[i].ID == cf.Attempt.ID {
					cmd.Attempts[i] = *cf.Attempt
					break
				}
			}
		}
	}
}

func commandFactFrom(cmd PromptCommand) CommandFact {
	return CommandFact{
		ID:              cmd.ID,
		Revision:        cmd.Revision,
		Operation:       cmd.Operation,
		Session:         cmd.Session,
		Instance:        cmd.Instance,
		Caller:          cmd.Caller,
		IdempotencyKey:  cmd.IdempotencyKey,
		CanonicalDigest: cmd.CanonicalDigest,
		QueuePolicy:     cmd.QueuePolicy,
		ExpiresAt:       cmd.ExpiresAt,
		State:           cmd.State,
		AcceptedAt:      cmd.AcceptedAt,
		TerminalAt:      cmd.TerminalAt,
	}
}

func promptCommandFrom(cf CommandFact) PromptCommand {
	return PromptCommand{
		ID:              cf.ID,
		Revision:        cf.Revision,
		Operation:       cf.Operation,
		Session:         cf.Session,
		Instance:        cf.Instance,
		Caller:          cf.Caller,
		IdempotencyKey:  cf.IdempotencyKey,
		CanonicalDigest: cf.CanonicalDigest,
		QueuePolicy:     cf.QueuePolicy,
		ExpiresAt:       cf.ExpiresAt,
		State:           cf.State,
		AcceptedAt:      cf.AcceptedAt,
		TerminalAt:      cf.TerminalAt,
	}
}

func copyCommand(c *PromptCommand) PromptCommand {
	out := *c
	if c.Attempts != nil {
		out.Attempts = append([]CommandAttempt(nil), c.Attempts...)
	}
	return out
}

func idempotencyScope(caller, operation, target, key string) string {
	return caller + "\x1f" + operation + "\x1f" + target + "\x1f" + key
}
