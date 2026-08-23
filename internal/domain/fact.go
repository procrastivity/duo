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
}
