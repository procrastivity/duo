package domain

import (
	"errors"
	"fmt"
	"strings"
)

// The refusals the kernel raises. Each one names a rule in decision-01 §4–§5
// rather than a mechanism, because the caller's recovery depends on the rule.
var (
	// ErrUnknownObject reports a Duo ID this authority never issued or has
	// forgotten.
	ErrUnknownObject = errors.New("domain: unknown duo object")
	// ErrIllegalTransition reports a lifecycle transition §5.1 does not
	// allow.
	ErrIllegalTransition = errors.New("domain: illegal lifecycle transition")
	// ErrInstanceExited reports an operation on a runtime instance that has
	// already exited. §5.3: an accepted exit fact is final; nothing returns
	// an exited instance to a live state.
	ErrInstanceExited = errors.New("domain: runtime instance has exited; exit is final")
	// ErrNoCurrentInstance reports a verb that needs a current runtime
	// instance on a session that has none.
	ErrNoCurrentInstance = errors.New("domain: session has no current runtime instance")
	// ErrNotAttested reports a binding or correlation offered without one
	// of §3.4's three admissible sources.
	ErrNotAttested = errors.New("domain: binding requires an owner action, a launch plan, or an instance-scoped claim")
	// ErrActorBound reports a second concurrent live binding for one agent
	// actor. §3.4 permits one at a time.
	ErrActorBound = errors.New("domain: agent actor already has a live runtime binding")
	// ErrIntentRequired reports a resume or restart without the explicit
	// intent §5.2 demands.
	ErrIntentRequired = errors.New("domain: resume or restart requires explicit intent or equivalent proof")
	// ErrSessionState reports a verb refused by the session's durable state
	// — archiving a session with a live instance, for instance.
	ErrSessionState = errors.New("domain: session state does not permit this verb")
	// ErrRootPathRequired reports an enrollment or launch with no workspace
	// root path. The path is not identity, but it is the placement input.
	ErrRootPathRequired = errors.New("domain: workspace root path is required")
	// ErrLaunchResolutionIncomplete reports a launch that offered half a
	// launch-resolution record. §6.9's record is an ID and a body together;
	// either alone is evidence that cannot be resolved back to the choice
	// it explains.
	ErrLaunchResolutionIncomplete = errors.New(
		"domain: a launch-resolution record requires both an id and a body")
	// ErrContinuityUnverified reports a session whose host attachment
	// continuity is not proven. Its instance reports are parked and its
	// exact-target writes are disabled until the host proves the same live
	// execution again (§4.4 rule 1), or until an explicit restart or resume
	// starts a new runtime instance (§6.4). See degraded.go.
	ErrContinuityUnverified = errors.New(
		"domain: host attachment continuity is unverified; the report was parked and exact-target writes are disabled")
	// ErrProviderNameRequired reports a provider.disabled or
	// provider.enabled write with no provider name (step 08).
	ErrProviderNameRequired = errors.New("domain: provider name is required")
	// ErrIdempotencyKeyRequired reports a prompt command with no caller
	// idempotency key.
	ErrIdempotencyKeyRequired = errors.New("domain: prompt command requires an idempotency key")
	// ErrCanonicalDigestRequired reports a prompt command with no canonical
	// request digest.
	ErrCanonicalDigestRequired = errors.New("domain: prompt command requires a canonical digest")
	// ErrExpiryInPast reports an expires_at that is not later than now.
	ErrExpiryInPast = errors.New("domain: prompt command expires_at must be later than now")
	// ErrQueuePolicyUnsupported reports a queue policy other than
	// queue_until_safe (require_ready and hold_for_release are out of this
	// slice).
	ErrQueuePolicyUnsupported = errors.New("domain: prompt command queue policy must be queue_until_safe")
	// ErrCommandExpired reports a command whose expires_at passed before an
	// attempt, now terminal expired.
	ErrCommandExpired = errors.New("domain: prompt command expired")
	// ErrCommandTerminal reports a verb that needs a nonterminal command.
	ErrCommandTerminal = errors.New("domain: prompt command is terminal")
	// ErrCommandAttempting reports a second attempt while one is already in
	// flight (including after a crash that left the command attempting).
	ErrCommandAttempting = errors.New("domain: prompt command already has an in-flight attempt")
	// ErrUnknownCommand reports a command ID this authority never accepted.
	ErrUnknownCommand = errors.New("domain: unknown prompt command")
	// ErrPromptPathKind reports a CreateAttempt with an empty or unknown
	// path kind. The kernel records the selected kind; it does not choose it.
	ErrPromptPathKind = errors.New("domain: prompt attempt requires a path kind runtime or host")
	// ErrCommandNotAttempting reports ReconcileAttempt on a command that is
	// not in the attempting state.
	ErrCommandNotAttempting = errors.New("domain: prompt command is not attempting")
)

// ConflictError is §4.2 step 5's outcome: evidence that overlaps two Duo
// sessions, or that cannot distinguish live runtimes. It enrolls nothing and
// never merges anything; an owner resolves it.
type ConflictError struct {
	// Reason is the human-facing explanation.
	Reason string
	// Ref is the contested claim, zero when the refusal was that the
	// evidence could not distinguish a live runtime at all.
	Ref ClaimRef
	// Holders lists the Duo sessions the evidence overlapped.
	Holders []SessionID
	// Cause is the underlying refusal, such as ErrWeakFingerprint.
	Cause error
}

func (e *ConflictError) Error() string {
	var b strings.Builder
	b.WriteString("domain: enrollment conflict: ")
	b.WriteString(e.Reason)
	if len(e.Holders) > 0 {
		holders := make([]string, 0, len(e.Holders))
		for _, h := range e.Holders {
			holders = append(holders, string(h))
		}
		fmt.Fprintf(&b, " (held by %s)", strings.Join(holders, ", "))
	}
	return b.String()
}

// Unwrap exposes the underlying refusal to errors.Is.
func (e *ConflictError) Unwrap() error { return e.Cause }

// IdempotencyConflictError is command.idempotency_conflict: the same caller
// key already binds a different canonical digest (decision-03 §3.2).
type IdempotencyConflictError struct {
	Key             string
	ExistingCommand CommandID
	ExistingDigest  string
	RequestDigest   string
}

func (e *IdempotencyConflictError) Error() string {
	return fmt.Sprintf("domain: command.idempotency_conflict: key %q already binds digest %s, not %s (command %s)",
		e.Key, e.ExistingDigest, e.RequestDigest, e.ExistingCommand)
}
