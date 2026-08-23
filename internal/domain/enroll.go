package domain

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Discovery is the transient result of the discover verb: what Duo can say
// about an external runtime before anyone claims it. It carries no durable
// Duo ID, because §5.2 says discovery "does not claim, control, or assign a
// durable public ID".
type Discovery struct {
	Fingerprint Fingerprint
	Ref         ClaimRef
	// Claimable reports whether the evidence is strong enough to enroll.
	Claimable bool
	// Degraded reports a claimable fingerprint with no process-birth
	// identity.
	Degraded bool
	// Enrolled names the Duo session already holding this fingerprint, ""
	// when none does.
	Enrolled SessionID
	// Reason explains a non-claimable candidate.
	Reason string
}

// Discover observes an external runtime and describes it. It writes nothing.
func (a *Authority) Discover(c Candidate) Discovery {
	d := Discovery{Fingerprint: c.Fingerprint, Degraded: c.Fingerprint.Degraded()}
	if err := c.Fingerprint.Validate(); err != nil {
		d.Reason = err.Error()
		return d
	}
	d.Claimable = true
	d.Ref = c.Fingerprint.ClaimRef()
	if held, ok := a.claims[d.Ref]; ok {
		d.Enrolled = held.Session
	}
	return d
}

// EnrollRequest is one enrollment attempt.
type EnrollRequest struct {
	Candidate Candidate
	// Actor is the responsible actor or subsystem, recorded on every fact.
	Actor string
	// Owner is the session's owning subject; it defaults to Actor.
	Owner string
	// BindActor optionally binds a durable agent actor at enrollment.
	// It requires Attestation, per §3.4.
	BindActor ActorID
	// Attestation proves the source of the agent-runtime evidence and of
	// any actor binding.
	Attestation Attestation
	// ValidUntil bounds the correlations this enrollment creates. Empty
	// leaves them unbounded until an integration verifies or stales them.
	ValidUntil string
}

// EnrollResult is what enrollment returns.
type EnrollResult struct {
	Workspace  WorkspaceID
	Session    SessionID
	Instance   InstanceID
	Attachment AttachmentID
	// Repeat is true when this live runtime was already enrolled and the
	// existing Duo session was returned unchanged.
	Repeat bool
	// Credential is the instance-scoped reporter enrollment credential.
	// It is returned exactly once, at creation, and never stored in the
	// clear; a repeat enrollment returns "".
	Credential string
}

// Enroll atomically adopts one validated live runtime (decision-01 §4.2).
//
// The five steps in §4.2 map one-to-one onto the code below: revalidate the
// candidate, look up active claims, return the existing session for the same
// live runtime, create everything in one durable transaction when no claim
// exists, and record a conflict while enrolling nothing when the evidence
// overlaps two sessions or cannot distinguish live runtimes.
func (a *Authority) Enroll(ctx context.Context, req EnrollRequest) (EnrollResult, error) {
	actor := req.Actor
	if actor == "" {
		actor = "authority"
	}
	cand := req.Candidate

	// Step 1: revalidate the candidate.
	root, err := normalizeRoot(cand.RootPath)
	if err != nil {
		return EnrollResult{}, err
	}
	if err := cand.Fingerprint.Validate(); err != nil {
		conflict := &ConflictError{
			Reason: "candidate evidence cannot distinguish a live runtime",
			Cause:  err,
		}
		return EnrollResult{}, a.recordConflict(ctx, actor, conflict, cand)
	}
	agentClaimed := cand.AgentSession.Valid()
	if agentClaimed && !attests(req.Attestation) {
		// §3.4 and §6.5: an external agent-session ID is not a source. An
		// unattested one is refused rather than guessed at, and the refusal
		// is durable so a spoofing attempt is visible in history.
		return EnrollResult{}, a.recordRejectedReport(ctx, actor, cand,
			"agent-runtime session offered without an owner action, launch plan, or instance-scoped claim")
	}
	if req.BindActor != "" && !attests(req.Attestation) {
		return EnrollResult{}, fmt.Errorf("%w: actor %s", ErrNotAttested, req.BindActor)
	}

	// Step 2: look up active claims.
	primary := cand.Fingerprint.ClaimRef()
	var secondary ClaimRef
	if agentClaimed {
		secondary = cand.AgentSession.ClaimRef()
	}
	primaryHolder, primaryHeld := a.claims[primary]
	secondaryHolder, secondaryHeld := a.claims[secondary]
	if !agentClaimed {
		secondaryHolder, secondaryHeld = nil, false
	}

	switch {
	// Step 3: the same live runtime is already enrolled.
	case primaryHeld:
		if secondaryHeld && secondaryHolder.Session != primaryHolder.Session {
			conflict := &ConflictError{
				Reason:  "candidate evidence overlaps two duo sessions",
				Ref:     secondary,
				Holders: []SessionID{primaryHolder.Session, secondaryHolder.Session},
			}
			return EnrollResult{}, a.recordConflict(ctx, actor, conflict, cand)
		}
		return a.repeatEnrollment(ctx, actor, req, cand, primaryHolder, secondary, secondaryHeld)

	// Step 5: the evidence belongs to a different Duo session.
	case secondaryHeld:
		conflict := &ConflictError{
			Reason:  "agent-runtime session is already claimed by another duo session",
			Ref:     secondary,
			Holders: []SessionID{secondaryHolder.Session},
		}
		return EnrollResult{}, a.recordConflict(ctx, actor, conflict, cand)
	}

	// Step 4: no claim exists — create everything in one transaction.
	return a.createEnrollment(ctx, actor, root, req, primary, secondary, agentClaimed)
}

// createEnrollment builds and commits the whole enrollment: workspace,
// session, runtime instance, host attachment, correlations, active claims,
// the reporter credential, and the audit row — one Change, one transaction.
func (a *Authority) createEnrollment(
	ctx context.Context,
	actor, root string,
	req EnrollRequest,
	primary, secondary ClaimRef,
	agentClaimed bool,
) (EnrollResult, error) {
	cand := req.Candidate
	fp := cand.Fingerprint
	owner := req.Owner
	if owner == "" {
		owner = actor
	}

	b := a.change(actor)
	workspace := a.ensureWorkspace(b, root)
	sessionID := SessionID(b.mint(sessionPrefix))
	instanceID := InstanceID(b.mint(instancePrefix))
	attachmentID := AttachmentID(b.mint(attachmentPrefix))
	secret, fingerprint, err := mintCredential()
	if err != nil {
		return EnrollResult{}, err
	}

	session := Session{
		ID:        sessionID,
		Workspace: workspace,
		State:     SessionActive,
		Owner:     owner,
		Creator:   actor,
		Current:   instanceID,
		CreatedAt: b.at,
	}
	if req.BindActor != "" {
		if bound, ok := a.actorBinding[req.BindActor]; ok {
			return EnrollResult{}, fmt.Errorf("%w: actor %s is bound to session %s",
				ErrActorBound, req.BindActor, bound)
		}
		session.BoundActor = req.BindActor
	}
	// Enrollment adopts a runtime that is already running and validated, so
	// its instance starts live rather than starting (§5.1: Candidate
	// --enroll--> Attached).
	instance := RuntimeInstance{
		ID:                    instanceID,
		Session:               sessionID,
		State:                 InstanceLive,
		CredentialFingerprint: fingerprint,
		StartedAt:             b.at,
	}
	attachment := HostAttachment{
		ID:                  attachmentID,
		Session:             sessionID,
		IntegrationInstance: fp.IntegrationInstance,
		Epoch:               fp.Epoch,
		Container:           fp.Container,
		State:               Attached,
	}

	b.fact(FactSessionCreated, Fact{Session: &session, Reason: "enroll"}).
		fact(FactInstanceStarted, Fact{Instance: &instance, SessionID: sessionID, Reason: "enroll"}).
		fact(FactAttachmentCreated, Fact{Attachment: &attachment, SessionID: sessionID}).
		fact(FactSessionEnrolled, Fact{
			SessionID:  sessionID,
			InstanceID: instanceID,
			Evidence:   describeFingerprint(fp),
			Reason:     "enroll",
		}).
		fact(FactCredentialIssued, Fact{
			SessionID:  sessionID,
			InstanceID: instanceID,
			Detail:     fingerprint,
			Reason:     "instance-scoped reporter credential",
		})

	a.correlateHost(b, attachmentID, fp, req.ValidUntil, "host-report")
	if fp.Process.Present() {
		b.correlate(correlationSpec{
			Target: TargetInstance, TargetID: string(instanceID),
			Kind: "process.birth", Value: processBirthValue(fp.Process),
			Scope: fp.IntegrationInstance, Source: "host-report",
			Evidence: "process-birth tuple reported by the session host",
			Until:    req.ValidUntil,
		})
	}
	if agentClaimed {
		a.correlateAgent(b, instanceID, cand, req.Attestation, req.ValidUntil)
	}

	b.hold(primary, sessionID, instanceID, fp.Degraded(), "enroll")
	if agentClaimed {
		b.hold(secondary, sessionID, instanceID, false, "attested agent-runtime session")
	}
	if req.BindActor != "" {
		b.fact(FactActorBound, Fact{
			SessionID: sessionID, InstanceID: instanceID, ActorID: req.BindActor,
			Reason: string(req.Attestation.Source),
		})
	}
	b.auditEntry(AuditEntry{
		Target: string(sessionID),
		Reason: "enroll",
		Detail: "live runtime adopted as " + string(sessionID) + " / " + string(instanceID),
	})

	change, err := b.build()
	if err != nil {
		return EnrollResult{}, err
	}
	if err := a.commit(ctx, a.repo.CommitIdentity, change); err != nil {
		return EnrollResult{}, err
	}
	return EnrollResult{
		Workspace:  workspace,
		Session:    sessionID,
		Instance:   instanceID,
		Attachment: attachmentID,
		Credential: secret,
	}, nil
}

// repeatEnrollment is §4.2 step 3: the same live runtime is already
// enrolled, so the existing Duo session comes back unchanged. Newly supplied
// attested evidence is added to the existing runtime instance rather than
// creating a second session (§6.1).
func (a *Authority) repeatEnrollment(
	ctx context.Context,
	actor string,
	req EnrollRequest,
	cand Candidate,
	held *Claim,
	secondary ClaimRef,
	secondaryHeld bool,
) (EnrollResult, error) {
	session, err := a.requireSession(held.Session)
	if err != nil {
		return EnrollResult{}, err
	}
	instance, err := a.requireInstance(held.Instance)
	if err != nil {
		return EnrollResult{}, err
	}
	if instance.State.Terminal() {
		// A live claim held by an exited instance would be a contradiction:
		// exit releases claims. Refuse rather than reopen it (§5.3).
		return EnrollResult{}, fmt.Errorf("%w: instance %s", ErrInstanceExited, instance.ID)
	}

	b := a.change(actor)
	b.fact(FactEnrollmentRepeated, Fact{
		SessionID:  session.ID,
		InstanceID: instance.ID,
		Evidence:   describeFingerprint(cand.Fingerprint),
		Reason:     "same live runtime fingerprint already enrolled",
	})
	if cand.AgentSession.Valid() && !secondaryHeld {
		a.correlateAgent(b, instance.ID, cand, req.Attestation, req.ValidUntil)
		b.hold(secondary, session.ID, instance.ID, false, "attested agent-runtime session")
	}
	b.auditEntry(AuditEntry{
		Target: string(session.ID),
		Reason: "repeat enrollment",
		Detail: "returned the existing duo session",
	})

	change, err := b.build()
	if err != nil {
		return EnrollResult{}, err
	}
	if err := a.commit(ctx, a.repo.CommitIdentity, change); err != nil {
		return EnrollResult{}, err
	}
	return EnrollResult{
		Workspace:  session.Workspace,
		Session:    session.ID,
		Instance:   instance.ID,
		Attachment: session.Attachment,
		Repeat:     true,
	}, nil
}

// ensureWorkspace returns the workspace for root, adding a creation fact when
// there is none. §3.3: one normalized current root maps to one active
// workspace, and an enrollment for the same root returns the existing one.
func (a *Authority) ensureWorkspace(b *changeBuilder, root string) WorkspaceID {
	if id, ok := a.rootIndex[root]; ok {
		return id
	}
	id := WorkspaceID(b.mint(workspacePrefix))
	ws := Workspace{ID: id, RootPath: root, CreatedAt: b.at}
	b.fact(FactWorkspaceCreated, Fact{Workspace: &ws, Reason: "root path has no workspace"})
	b.correlate(correlationSpec{
		Target: TargetWorkspace, TargetID: string(id),
		Kind: "workspace.root", Value: root,
		Source:   "caller",
		Evidence: "normalized current root path",
	})
	return id
}

// correlateHost records the host-attachment correlations: the container and
// the epoch-equivalent that distinguishes its incarnation. §4.1 puts both on
// the attachment, not on the session.
func (a *Authority) correlateHost(b *changeBuilder, id AttachmentID, fp Fingerprint, until, source string) {
	b.correlate(correlationSpec{
		Target: TargetAttachment, TargetID: string(id),
		Kind: "host.container", Value: fp.Container,
		Scope: fp.IntegrationInstance, Source: source,
		Evidence: "exact host container reported by the session host",
		Until:    until,
	})
	b.correlate(correlationSpec{
		Target: TargetAttachment, TargetID: string(id),
		Kind: "host.epoch", Value: fp.Epoch.Kind + "=" + fp.Epoch.Value,
		Scope: fp.IntegrationInstance, Source: source,
		Evidence: "epoch-equivalent identity at " + string(fp.Epoch.Scope) + " scope",
		Until:    until,
	})
}

// correlateAgent records the agent-runtime session and transcript
// correlations on the runtime instance (§4.1).
func (a *Authority) correlateAgent(b *changeBuilder, id InstanceID, cand Candidate, att Attestation, until string) {
	b.correlate(correlationSpec{
		Target: TargetInstance, TargetID: string(id),
		Kind: "agent.session", Value: cand.AgentSession.SessionID,
		Scope: cand.AgentSession.IntegrationInstance, Source: string(att.Source),
		Evidence: "attested agent-runtime report",
		Until:    until,
	})
	if cand.Transcript != "" {
		b.correlate(correlationSpec{
			Target: TargetInstance, TargetID: string(id),
			Kind: "transcript", Value: cand.Transcript,
			Scope: cand.AgentSession.IntegrationInstance, Source: string(att.Source),
			// §8 rejects binding "the newest transcript in a directory";
			// this record exists only because a report named it alongside
			// the agent-session identity.
			Evidence: "reported with the attested agent-runtime session",
			Until:    until,
		})
	}
}

// recordConflict commits §4.2 step 5: a durable conflict, and nothing else.
// The candidate stays unenrolled and no merge is ever attempted.
func (a *Authority) recordConflict(ctx context.Context, actor string, conflict *ConflictError, cand Candidate) error {
	b := a.change(actor)
	holders := make([]string, 0, len(conflict.Holders))
	for _, h := range conflict.Holders {
		holders = append(holders, string(h))
	}
	b.fact(FactEnrollmentConflict, Fact{
		Reason:   conflict.Reason,
		Evidence: describeFingerprint(cand.Fingerprint),
		Detail:   strings.Join(holders, ","),
	}).auditEntry(AuditEntry{
		Target: string(conflict.Ref.Key),
		Reason: "enrollment conflict",
		Detail: conflict.Error(),
	})
	change, err := b.build()
	if err != nil {
		return err
	}
	if err := a.commit(ctx, a.repo.CommitIdentity, change); err != nil {
		return err
	}
	return conflict
}

// recordRejectedReport commits a refused, unattested claim (§7's "rejected
// spoofing attempts") and returns the refusal.
func (a *Authority) recordRejectedReport(ctx context.Context, actor string, cand Candidate, reason string) error {
	b := a.change(actor)
	b.fact(FactReportRejected, Fact{
		Reason:   reason,
		Evidence: describeFingerprint(cand.Fingerprint),
	}).auditEntry(AuditEntry{Target: cand.AgentSession.SessionID, Reason: "rejected report", Detail: reason})
	change, err := b.build()
	if err != nil {
		return err
	}
	if err := a.commit(ctx, a.repo.CommitIdentity, change); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrNotAttested, reason)
}

// correlationSpec is one correlation record's inputs.
type correlationSpec struct {
	Target   CorrelationTarget
	TargetID string
	Kind     string
	Value    string
	Scope    string
	Source   string
	Evidence string
	Until    string
}

// correlate appends one correlation record.
func (b *changeBuilder) correlate(spec correlationSpec) CorrelationID {
	id := CorrelationID(b.mint(correlationPrefix))
	c := Correlation{
		ID:              id,
		TargetKind:      spec.Target,
		TargetID:        spec.TargetID,
		ExternalKind:    spec.Kind,
		ExternalValue:   spec.Value,
		Scope:           spec.Scope,
		Source:          spec.Source,
		Evidence:        spec.Evidence,
		FirstVerifiedAt: b.at,
		LastVerifiedAt:  b.at,
		ValidUntil:      spec.Until,
		Status:          CorrelationActive,
	}
	b.fact(FactCorrelationClaimed, Fact{Correlation: &c})
	return id
}

// attests reports whether an attestation names one of §3.4's three sources.
func attests(a Attestation) bool {
	switch a.Source {
	case SourceOwner, SourceLaunchPlan, SourceInstanceClaim:
		return true
	default:
		return false
	}
}

// normalizeRoot cleans a workspace root path. Cleaning and an absolute-path
// requirement are all the kernel does: resolving symlinks or reading
// filesystem identity is I/O, which belongs to the application layer, and
// §3.3 wants an explicit audited rebind rather than a silent one when
// filesystem evidence cannot prove continuity.
func normalizeRoot(p string) (string, error) {
	if p == "" {
		return "", ErrRootPathRequired
	}
	if !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %q is not absolute", ErrRootPathRequired, p)
	}
	return filepath.Clean(p), nil
}

// processBirthValue renders a process-birth tuple for a correlation record.
func processBirthValue(p ProcessBirth) string {
	parts := []string{"pid=" + strconv.Itoa(p.PID), "started=" + p.StartedAt}
	if p.Host != "" {
		parts = append(parts, "host="+p.Host)
	}
	if p.Executable != "" {
		parts = append(parts, "exe="+p.Executable)
	}
	return strings.Join(parts, " ")
}

// describeFingerprint renders a fingerprint as fact evidence. It names the
// components rather than the digest, so history explains why two candidates
// were or were not the same live runtime.
func describeFingerprint(fp Fingerprint) string {
	parts := []string{
		"integration=" + fp.IntegrationInstance,
		"epoch=" + fp.Epoch.Kind + ":" + fp.Epoch.Value + "@" + string(fp.Epoch.Scope),
		"container=" + fp.Container,
	}
	if fp.Process.Present() {
		parts = append(parts, "process="+processBirthValue(fp.Process))
	} else {
		parts = append(parts, "process=absent")
	}
	return strings.Join(parts, " ")
}
