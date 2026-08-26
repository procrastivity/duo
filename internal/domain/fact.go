package domain

// FactKind names one durable lifecycle fact. decision-01 §7 lists the
// changes Duo must keep "durable, attributable facts" for; this enumeration
// is that list, one constant per change.
type FactKind string

// The lifecycle facts.
const (
	// Workspace facts (§7 bullet 1).
	FactWorkspaceCreated  FactKind = "workspace.created"
	FactWorkspaceRebound  FactKind = "workspace.rebound"
	FactWorkspaceConflict FactKind = "workspace.conflict"

	// Session facts (§7 bullet 2).
	FactSessionCreated  FactKind = "session.created"
	FactSessionEnrolled FactKind = "session.enrolled"
	FactSessionLaunched FactKind = "session.launched"
	FactSessionState    FactKind = "session.state"
	FactSessionOwner    FactKind = "session.owner"

	// FactLaunchResolved records the launch-resolution record a launch was
	// gated on: §6.9's immutable, Duo-authored evidence of the complete
	// choice made before anything spawned. It commits in the same Change as
	// session.created, instance.started, and session.launched, because the
	// record and the identities it explains are one transaction (§4.2's
	// launch-resolution boundary, §7.4).
	FactLaunchResolved FactKind = "launch.resolved"

	// Host-attachment facts (§7 bullet 2: detach and reattach).
	FactAttachmentCreated FactKind = "attachment.created"
	FactAttachmentState   FactKind = "attachment.state"
	// FactAttachmentContinuity records that the attachment's continuity to a
	// live execution became unverified, or was proven again. It is the
	// durable half of the degraded-continuity rule: "Mark attachment
	// continuity unverified, park instance-scoped reports as unresolved
	// evidence, and disable write paths that need an exact live target"
	// (duo-vnext-integration-conformance.md §10).
	FactAttachmentContinuity FactKind = "attachment.continuity"

	// Runtime-instance facts (§7 bullet 3).
	FactInstanceStarted     FactKind = "instance.started"
	FactInstanceState       FactKind = "instance.state"
	FactInstanceCurrent     FactKind = "instance.current"
	FactRecoveryDecision    FactKind = "instance.recovery-decision"
	FactSessionQuarantined  FactKind = "session.quarantined"
	FactStopRequestRecorded FactKind = "instance.stop-requested"

	// Agent-actor facts (§7 bullet 4).
	FactActorCreated FactKind = "actor.created"
	FactActorBound   FactKind = "actor.bound"
	FactActorUnbound FactKind = "actor.unbound"

	// Correlation facts (§7 bullet 5).
	FactCorrelationClaimed FactKind = "correlation.claimed"
	FactCorrelationStatus  FactKind = "correlation.status"

	// Active-claim facts and duplicate-enrollment decisions (§7 bullets
	// 5–6). A claim is held and released, never mutated.
	FactClaimHeld     FactKind = "claim.held"
	FactClaimReleased FactKind = "claim.released"
	// FactEnrollmentRepeated records a repeat enrollment that returned an
	// existing session — a duplicate-enrollment decision, which §7 requires
	// to be durable even though it changes nothing else.
	FactEnrollmentRepeated FactKind = "enrollment.repeated"
	// FactEnrollmentConflict records a refused enrollment. It is the one
	// fact an otherwise-empty enrollment commits: §4.2 step 5 says to
	// "record a conflict and leave the candidate unenrolled".
	FactEnrollmentConflict FactKind = "enrollment.conflict"

	// Reporter-credential facts (§7 bullet 7).
	FactCredentialIssued FactKind = "credential.issued"
	FactLateReport       FactKind = "report.after-exit"
	FactReportRejected   FactKind = "report.rejected"
	// FactReportParked records an instance report that arrived while the
	// session's attachment continuity was unverified. The report is durable
	// evidence; it is deliberately not applied to identity state
	// (decision-02's 2026-08-14 G-03 amendment, "parked unresolved
	// evidence").
	FactReportParked FactKind = "report.parked"

	// --- duo.config/v3 workspace<->host correlation facts -------------
	//
	// Added 2026-08-24 (handoff 22; notes/42 §8, notes/43 item 13). v3
	// stops authoring the session host in configuration, so the host
	// instance a workspace's new spawns go to is state, not intent.
	// These two are the host siblings of FactWorkspaceRebound: one
	// current correlation, established once and changed only by an
	// explicit, audited rebind (decision-01 §3.3's shape, applied to the
	// host instance instead of the root path). See hostcorrelation.go.

	// FactWorkspaceHostBound records the first workspace<->host-instance
	// correlation: the host kind, the instance locator, the HostSource
	// rung that produced it, and the notes/19 §5 fingerprint set.
	FactWorkspaceHostBound FactKind = "workspace.host_bound"
	// FactWorkspaceHostRebound records an audited change of that
	// correlation. It carries both instances with their fingerprints, so
	// the fact alone answers "what was it before, and what is it now".
	FactWorkspaceHostRebound FactKind = "workspace.host_rebound"

	// --- Provider state facts (duo.config/v3 step 08; notes/42 §8, notes/43
	// item 14) ---
	//
	// A provider is config data, not a durable Duo object — a launch_variant
	// names it, and nothing mints an ID for it. These two facts record a
	// standing administrative decision about one provider name: the latest
	// one recorded for a name wins on replay, and the default for a name
	// with no fact at all is enabled. See ProviderFact and
	// Authority.StandingProviderFacts.

	// FactProviderDisabled records that launch resolution must not choose
	// the named provider until a later provider.enabled fact supersedes it.
	FactProviderDisabled FactKind = "provider.disabled"
	// FactProviderEnabled records that the named provider is eligible again,
	// superseding any earlier provider.disabled fact for the same name. It
	// is also the fact a first-ever "enable" writes, even though enabled is
	// already the default for a name with no standing fact — the write is
	// durable evidence of the decision, not merely a state change.
	FactProviderEnabled FactKind = "provider.enabled"

	// --- Prompt-command facts (delegation-loop Stage D step 10; decision-03
	// §3–§4, narrowed: delivered | expired | failed; not canceled) ---
	//
	// These are a recorded vocabulary add on the lifecycle fact log, not an
	// I-2 elimination-reason add: launch/record.go stays closed.

	// FactCommandAccepted records durable acceptance of one prompt.deliver
	// command together with its idempotency record, bound runtime-instance
	// ID, finite expiry, and initial queued responsibility (queue_until_safe
	// only). Acceptance and idempotency commit in one CommandAcceptTx.
	FactCommandAccepted FactKind = "command.accepted"
	// FactCommandAttemptCreated records one command-attempt before any
	// adapter is invoked. It is a CommandTransitionTx of its own, so a
	// crash after this commit reloads the command as attempting and must
	// not mint a second attempt.
	FactCommandAttemptCreated FactKind = "command.attempt_created"
	// FactCommandDelivered is the terminal transition after a path meets
	// its complete-turn success condition. It commits after the adapter
	// returns and before the result is visible.
	FactCommandDelivered FactKind = "command.delivered"
	// FactCommandExpired is the terminal transition for a command whose
	// expires_at passed with no attempt (or no in-flight attempt to
	// reconcile). Expiry during an unresolved attempt is command.failed
	// with unknown_effect, not expired (decision-03 §7.2).
	FactCommandExpired FactKind = "command.expired"
	// FactCommandFailed is the terminal transition for a definite or
	// indeterminate failure, including crash-before-terminal-commit
	// reconciliation as unknown_effect.
	FactCommandFailed FactKind = "command.failed"
	// FactCommandRequeued records a proved no_effect attempt returning the
	// command to queued so a later CreateAttempt may retry. Automatic retry
	// is permitted only on this proof (decision-03 §4.3, §7.1).
	FactCommandRequeued FactKind = "command.requeued"
)

// Fact is one durable, attributable lifecycle change.
//
// A fact that creates an object carries the whole object, so replay is a
// switch over kinds with no lookups into the outside world; a fact that
// changes one carries the target's ID and the new value. Every fact records
// the responsible actor, the time, and a reason, per §7's "Each fact records
// the responsible actor or subsystem, target Duo IDs, time, source evidence,
// authority incarnation, and reason".
//
// The authority incarnation is stamped by the kernel from the writer
// incarnation its repository reports (see IncarnationReporter). The store
// stamps its own audit rows, but the fact log is a stream payload the store
// does not interpret, so a fact that did not carry the incarnation could not
// answer "which authority run recorded this" after a restart — which is
// exactly the question §4.4 recovery raises.
type Fact struct {
	ID   FactID
	Kind FactKind
	At   string
	// Actor is the responsible actor or subsystem.
	Actor string
	// Incarnation is the authority incarnation that recorded the fact. It
	// changes on every authority restart (§4.4: "It assigns a new authority
	// incarnation ID").
	Incarnation string
	// Reason is why the change happened, in one phrase.
	Reason string
	// Evidence is the source evidence the change rests on.
	Evidence string

	// Creation payloads. Exactly one is set on a creation fact.
	Workspace   *Workspace
	Session     *Session
	Instance    *RuntimeInstance
	AgentActor  *AgentActor
	Attachment  *HostAttachment
	Correlation *Correlation
	Claim       *Claim
	Parked      *ParkedReport
	// LaunchResolution is the launch-resolution record on a launch.resolved
	// fact. Its body is opaque: the kernel carries the bytes and never
	// interprets them (see LaunchResolution).
	LaunchResolution *LaunchResolution

	// Provider is the payload of a provider.disabled or provider.enabled
	// fact (see the Provider-facts block in the FactKind enumeration
	// above). It is not a "creation payload" in the sense of the block
	// above — no Duo ID is minted for a provider — but the same
	// exactly-one-pointer-set shape applies, keyed by name instead.
	Provider *ProviderFact

	// Command is the payload of a prompt-command fact (command.accepted,
	// command.attempt_created, the three terminal kinds, and
	// command.requeued). See CommandFact.
	Command *CommandFact

	// Transition targets. A transition fact names the object it changes and
	// the new value.
	WorkspaceID   WorkspaceID
	SessionID     SessionID
	InstanceID    InstanceID
	AttachmentID  AttachmentID
	ActorID       ActorID
	CorrelationID CorrelationID
	State         string
	Detail        string

	// --- duo.config/v3 workspace<->host correlation payloads ----------
	//
	// HostBinding carries the whole correlation on both host_bound and
	// host_rebound, because a correlation fact creates or replaces an
	// object rather than moving one field of it. PreviousHostBinding is
	// set only on host_rebound: notes/42 §11 requires a rebind to record
	// old and new instance with fingerprints, and carrying the old one on
	// the fact is what makes that true of the fact in isolation, not only
	// of a replay that happens to have seen the earlier fact.
	HostBinding         *HostBinding
	PreviousHostBinding *HostBinding
}

// --- Provider state (duo.config/v3 step 08) --------------------------------

// ProviderFact is one provider.disabled or provider.enabled fact's payload:
// the provider name the standing decision targets.
//
// Note is reserved, always empty in this stage. It is thread 4's extension
// point (workplan Risk 3: `duo provider disable --until` / a recorded
// reason payload) — leaving the field present but unused now means thread 4
// adds a value to it later without a wire-shape change.
type ProviderFact struct {
	Name string
	Note string
}

// ProviderStanding is one provider's current standing state, as
// Authority.StandingProviderFacts reports it: the latest provider.disabled
// or provider.enabled fact recorded for the name, replayed in commit order.
// A name with no ProviderStanding entry has no standing fact at all — the
// default-enabled rule applies at the reader, not by seeding this map (see
// Authority.StandingProviderFacts).
type ProviderStanding struct {
	Enabled bool
	// FactID is the ID of the fact that set this standing state — the exact
	// fact step 11's evidence bundle snapshots provider state by.
	FactID FactID
}
