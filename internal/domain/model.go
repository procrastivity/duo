package domain

// SessionState is a Duo session's durable state. decision-01 §5.1: "The
// durable session states are active with an attached or detached host,
// inactive, archived, and removed." Attached versus detached is a property of
// the host attachment, not a second session state, which is why it is not in
// this enumeration.
type SessionState string

// The durable session states.
const (
	SessionActive   SessionState = "active"
	SessionInactive SessionState = "inactive"
	SessionArchived SessionState = "archived"
	SessionRemoved  SessionState = "removed"
)

// AttachmentState is the state of a session's host attachment.
type AttachmentState string

// The host-attachment states.
const (
	Attached AttachmentState = "attached"
	Detached AttachmentState = "detached"
)

// InstanceState is a runtime instance's state (decision-01 §5.1).
// InstanceExited is the one terminal state.
type InstanceState string

// The runtime-instance states.
const (
	InstanceStarting      InstanceState = "starting"
	InstanceLive          InstanceState = "live"
	InstanceStopRequested InstanceState = "stop_requested"
	InstanceExited        InstanceState = "exited"
)

// Terminal reports whether s is the terminal runtime-instance state.
func (s InstanceState) Terminal() bool { return s == InstanceExited }

// instanceNext is decision-01 §5.1's runtime-instance transition diagram:
//
//	Starting -> Live | Exited
//	Live -> StopRequested | Exited
//	StopRequested -> Exited
//	Exited -> (nothing; exit is final)
var instanceNext = map[InstanceState][]InstanceState{
	InstanceStarting:      {InstanceLive, InstanceExited},
	InstanceLive:          {InstanceStopRequested, InstanceExited},
	InstanceStopRequested: {InstanceExited},
	InstanceExited:        {},
}

// canAdvance reports whether from -> to is a transition §5.1 allows.
func canAdvance(from, to InstanceState) bool {
	for _, allowed := range instanceNext[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

// SessionView is the state a presentation shows. It is derived, never
// stored: decision-01 §5.1 makes Recovering "a current view during authority
// startup", and attached/detached a property of the attachment.
type SessionView string

// The derived session views.
const (
	ViewAttached   SessionView = "attached"
	ViewDetached   SessionView = "detached"
	ViewRecovering SessionView = "recovering"
	ViewInactive   SessionView = "inactive"
	ViewArchived   SessionView = "archived"
	ViewRemoved    SessionView = "removed"
)

// CorrelationStatus is a correlation record's status (decision-01 §4.1).
type CorrelationStatus string

// The correlation statuses.
const (
	CorrelationActive     CorrelationStatus = "active"
	CorrelationStale      CorrelationStatus = "stale"
	CorrelationConflicted CorrelationStatus = "conflicted"
	CorrelationRetired    CorrelationStatus = "retired"
)

// CorrelationTarget names the kind of Duo object a correlation points at.
// decision-01 §4.1 fixes which target each external kind takes: host server
// and pane correlations target the host attachment; process-birth,
// agent-runtime session, and transcript correlations target the runtime
// instance.
type CorrelationTarget string

// The correlation target kinds.
const (
	TargetWorkspace  CorrelationTarget = "workspace"
	TargetSession    CorrelationTarget = "session"
	TargetInstance   CorrelationTarget = "instance"
	TargetAttachment CorrelationTarget = "attachment"
)

// Workspace is a durable Duo-owned local work context. Its root path is a
// current correlation, not its identity (decision-01 §3.3).
type Workspace struct {
	ID        WorkspaceID
	RootPath  string
	CreatedAt string
}

// Session is the primary public runtime object.
type Session struct {
	ID        SessionID
	Workspace WorkspaceID
	State     SessionState
	// Owner is the owning subject; Creator is the attributable creator
	// (decision-01 §3.5). Neither implies ownership of the host process.
	Owner   string
	Creator string
	// Current is the current runtime instance, "" when the session has
	// none. §5.1: "One active session has at most one current runtime
	// instance."
	Current InstanceID
	// Attachment is the current host attachment, "" for a session with no
	// session host at all — the hostless case the architecture keeps open.
	Attachment AttachmentID
	// BoundActor is the bound agent actor, "" when none.
	BoundActor ActorID
	// Quarantined marks a session withheld from automatic control after
	// recovery found conflicting live claims (decision-01 §4.4 rule 5).
	Quarantined bool
	CreatedAt   string
}

// RuntimeInstance is one execution generation of a Duo session.
type RuntimeInstance struct {
	ID      InstanceID
	Session SessionID
	State   InstanceState
	// CredentialFingerprint is the hash of this instance's reporter
	// enrollment credential. The secret itself is returned once, at
	// enrollment or launch, and never stored (decision-01 §3.6).
	CredentialFingerprint string
	StartedAt             string
	ExitedAt              string
}

// AgentActor is a durable Duo identity for one agent participant.
type AgentActor struct {
	ID        ActorID
	Name      string
	CreatedAt string
}

// HostAttachment is the current relationship between a Duo session and a
// session-host container. It can outlive one runtime instance.
type HostAttachment struct {
	ID                  AttachmentID
	Session             SessionID
	IntegrationInstance string
	Epoch               HostEpoch
	Container           string
	State               AttachmentState
}

// Correlation is a Duo-owned, time-bounded claim relating a Duo object to an
// external identifier or path (decision-01 §4.1).
type Correlation struct {
	ID         CorrelationID
	TargetKind CorrelationTarget
	TargetID   string
	// ExternalKind is the external identifier's kind, such as "host.pane",
	// "process.birth", "agent.session", or "transcript".
	ExternalKind  string
	ExternalValue string
	// Scope is the integration instance the external value is scoped by. An
	// external value is meaningless without it.
	Scope string
	// Source and Evidence record who claimed this and on what proof.
	Source   string
	Evidence string
	// FirstVerifiedAt, LastVerifiedAt, and ValidUntil bound the claim in
	// time. A record past ValidUntil is stale, not exited.
	FirstVerifiedAt string
	LastVerifiedAt  string
	ValidUntil      string
	Status          CorrelationStatus
}

// Claim is one held entry of the active-claim index.
type Claim struct {
	Ref ClaimRef
	// Generation counts how many times this claim key has been held and
	// released. It is what lets a genuinely new execution re-present a
	// degraded (process-birth-free) fingerprint after the previous holder
	// exited, without ever reopening the exited instance.
	Generation int
	Session    SessionID
	Instance   InstanceID
	// Degraded records that the claimed fingerprint carried no
	// process-birth identity.
	Degraded bool
	HeldAt   string
}

// Candidate is a transient description of an external runtime that might be
// enrolled (decision-01 §3.2). It has no durable Duo ID and becomes nothing
// until enrollment succeeds.
type Candidate struct {
	// Fingerprint is the claimable evidence.
	Fingerprint Fingerprint
	// RootPath selects or creates the workspace. It is a placement input,
	// never an identity or a uniqueness key: two candidates with the same
	// root path enroll as two distinct sessions in one workspace.
	RootPath string
	// AgentSession and Transcript are agent-runtime correlations. They are
	// only accepted from an attested source; an unattested report can
	// neither claim nor bind (§3.4, §6.5).
	AgentSession AgentSessionRef
	Transcript   string
	// Hints carries working directory, command name, agent name and any
	// other weak evidence, for diagnostics only. Nothing in the claim path
	// reads it (§4.2: those are "never sufficient").
	Hints map[string]string
}

// Attestation is the proof accompanying a claim that binds or correlates.
// decision-01 §3.4 admits exactly three sources.
type Attestation struct {
	Source BindingSource
	// Credential is the instance-scoped enrollment credential, required for
	// SourceInstanceClaim and ignored otherwise.
	Credential string
	// Instance is the runtime instance the credential was issued for.
	Instance InstanceID
	// Subject records the acting owner or administrator, for audit.
	Subject string
}

// BindingSource is where a binding or an attested correlation came from.
type BindingSource string

// The three admissible sources from decision-01 §3.4. Anything else — an
// external agent-session ID, transcript path, working directory, PID, or
// pane ID on its own — is not a source and cannot bind.
const (
	// SourceOwner is an explicit owner or administrator action.
	SourceOwner BindingSource = "owner"
	// SourceLaunchPlan is a launch or resume plan that already names the
	// actor.
	SourceLaunchPlan BindingSource = "launch-plan"
	// SourceInstanceClaim is an authenticated, instance-scoped claim that
	// Duo issued for the launch or enrollment.
	SourceInstanceClaim BindingSource = "instance-claim"
)
