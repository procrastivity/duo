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

	// Host-attachment facts (§7 bullet 2: detach and reattach).
	FactAttachmentCreated FactKind = "attachment.created"
	FactAttachmentState   FactKind = "attachment.state"

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
)

// Fact is one durable, attributable lifecycle change.
//
// A fact that creates an object carries the whole object, so replay is a
// switch over kinds with no lookups into the outside world; a fact that
// changes one carries the target's ID and the new value. Every fact records
// the responsible actor, the time, and a reason, per §7's "Each fact records
// the responsible actor or subsystem, target Duo IDs, time, source evidence,
// authority incarnation, and reason" — the authority incarnation is added by
// the store, which is the only layer that knows it.
type Fact struct {
	ID   FactID
	Kind FactKind
	At   string
	// Actor is the responsible actor or subsystem.
	Actor string
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
