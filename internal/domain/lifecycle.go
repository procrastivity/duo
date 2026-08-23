package domain

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"
)

// LaunchRequest asks the kernel to create a Duo session and a starting
// runtime instance before an integration is asked to start the execution.
type LaunchRequest struct {
	RootPath string
	Actor    string
	Owner    string
	// BindActor optionally names the agent actor the launch plan carries.
	// A launch plan is one of §3.4's admissible binding sources, so no
	// separate attestation is needed for it.
	BindActor ActorID
	Reason    string
}

// LaunchResult is what launch returns. There is no attachment and no claim
// yet: the live-runtime fingerprint does not exist until the process does,
// and Bind adds it when the integration reports back (§6.2).
type LaunchResult struct {
	Workspace WorkspaceID
	Session   SessionID
	Instance  InstanceID
	// Credential is the instance-scoped reporter enrollment credential,
	// minted before the launch call so a later report can prove which
	// instance it belongs to. Returned once.
	Credential string
}

// Launch creates a Duo session and a starting runtime instance (§5.2).
//
// The IDs exist before the integration is called, which is what lets a
// launch that returns an external process ID record it as a correlation on
// an object that already has a Duo identity.
func (a *Authority) Launch(ctx context.Context, req LaunchRequest) (LaunchResult, error) {
	actor := req.Actor
	if actor == "" {
		actor = "authority"
	}
	root, err := normalizeRoot(req.RootPath)
	if err != nil {
		return LaunchResult{}, err
	}
	owner := req.Owner
	if owner == "" {
		owner = actor
	}
	if req.BindActor != "" {
		if bound, ok := a.actorBinding[req.BindActor]; ok {
			return LaunchResult{}, fmt.Errorf("%w: actor %s is bound to session %s",
				ErrActorBound, req.BindActor, bound)
		}
	}

	b := a.change(actor)
	workspace := a.ensureWorkspace(b, root)
	sessionID := SessionID(b.mint(sessionPrefix))
	instanceID := InstanceID(b.mint(instancePrefix))
	secret, fingerprint, err := mintCredential()
	if err != nil {
		return LaunchResult{}, err
	}

	session := Session{
		ID: sessionID, Workspace: workspace, State: SessionActive,
		Owner: owner, Creator: actor, Current: instanceID,
		BoundActor: req.BindActor, CreatedAt: b.at,
	}
	instance := RuntimeInstance{
		ID: instanceID, Session: sessionID, State: InstanceStarting,
		CredentialFingerprint: fingerprint, StartedAt: b.at,
	}
	b.fact(FactSessionCreated, Fact{Session: &session, Reason: "launch"}).
		fact(FactInstanceStarted, Fact{Instance: &instance, SessionID: sessionID, Reason: "launch"}).
		fact(FactSessionLaunched, Fact{SessionID: sessionID, InstanceID: instanceID, Reason: req.Reason}).
		fact(FactCredentialIssued, Fact{
			SessionID: sessionID, InstanceID: instanceID, Detail: fingerprint,
			Reason: "instance-scoped reporter credential",
		}).
		auditEntry(AuditEntry{Target: string(sessionID), Reason: "launch", Detail: req.Reason})
	if req.BindActor != "" {
		b.fact(FactActorBound, Fact{
			SessionID: sessionID, InstanceID: instanceID, ActorID: req.BindActor,
			Reason: string(SourceLaunchPlan),
		})
	}

	change, err := b.build()
	if err != nil {
		return LaunchResult{}, err
	}
	if err := a.commit(ctx, a.repo.CommitLaunchResolution, change); err != nil {
		return LaunchResult{}, err
	}
	return LaunchResult{Workspace: workspace, Session: sessionID, Instance: instanceID, Credential: secret}, nil
}

// BindRequest adds verified relationships to an existing session and runtime
// instance: a host attachment and its live-runtime claim, an agent-runtime
// session and transcript, or an agent actor.
type BindRequest struct {
	Session SessionID
	// Instance defaults to the session's current runtime instance.
	Instance InstanceID
	Actor    string
	// Attestation is required. §3.4 admits exactly three sources, and an
	// external identifier is not one of them.
	Attestation Attestation
	// Fingerprint, when set, attaches the host container and seizes the
	// live-runtime claim.
	Fingerprint *Fingerprint
	// AgentSession and Transcript, when set, correlate the agent runtime.
	AgentSession AgentSessionRef
	Transcript   string
	// BindActor, when set, binds a durable agent actor.
	BindActor  ActorID
	ValidUntil string
	Reason     string
}

// Bind adds verified relationships without creating or reviving a process
// (§5.2). It is the path a late SessionStart report takes to reach the
// runtime instance it belongs to.
func (a *Authority) Bind(ctx context.Context, req BindRequest) error {
	actor := req.Actor
	if actor == "" {
		actor = "authority"
	}
	session, err := a.requireSession(req.Session)
	if err != nil {
		return err
	}
	instanceID := req.Instance
	if instanceID == "" {
		instanceID = session.Current
	}
	if instanceID == "" {
		return fmt.Errorf("%w: session %s", ErrNoCurrentInstance, session.ID)
	}
	instance, err := a.requireInstance(instanceID)
	if err != nil {
		return err
	}
	if instance.Session != session.ID {
		return fmt.Errorf("%w: instance %s does not belong to session %s",
			ErrUnknownObject, instance.ID, session.ID)
	}
	if instance.State.Terminal() {
		// §5.3: a late report can enrich history but cannot change an
		// exited instance. Record that it arrived, then refuse.
		//
		// This check outranks the degraded-continuity check below on
		// purpose: exit finality is a rule about the *instance* and holds
		// whatever the host can currently prove, and a report about an
		// exited instance must land in history as a late report rather than
		// as evidence something might later act on.
		return a.recordLateReport(ctx, actor, instance, req.Reason)
	}
	if err := a.verifyAttestation(instance, req.Attestation); err != nil {
		return err
	}
	if a.degraded(session, instance.ID) {
		// The whole request is parked, not the agent-runtime half of it.
		// While continuity is unverified Duo may not "bind or keep a live
		// claim on weaker evidence", and a bind that carried a fingerprint
		// would do exactly that.
		return a.park(ctx, actor, session, ParkedReport{
			Instance: instance.ID, Source: req.Attestation.Source,
			AgentSession: req.AgentSession, Transcript: req.Transcript,
			Evidence: describeBindEvidence(req),
			Reason:   "bind arrived while host attachment continuity was unverified",
		})
	}

	b := a.change(actor)
	if fp := req.Fingerprint; fp != nil {
		if err := fp.Validate(); err != nil {
			return err
		}
		ref := fp.ClaimRef()
		if held, ok := a.claims[ref]; ok && held.Instance != instance.ID {
			return &ConflictError{
				Reason:  "live-runtime fingerprint is already claimed by another duo session",
				Ref:     ref,
				Holders: []SessionID{held.Session},
			}
		} else if !ok {
			attachmentID := AttachmentID(b.mint(attachmentPrefix))
			att := HostAttachment{
				ID: attachmentID, Session: session.ID,
				IntegrationInstance: fp.IntegrationInstance,
				Epoch:               fp.Epoch, Container: fp.Container, State: Attached,
			}
			b.fact(FactAttachmentCreated, Fact{Attachment: &att, SessionID: session.ID})
			a.correlateHost(b, attachmentID, *fp, req.ValidUntil, string(req.Attestation.Source))
			if fp.Process.Present() {
				b.correlate(correlationSpec{
					Target: TargetInstance, TargetID: string(instance.ID),
					Kind: "process.birth", Value: processBirthValue(fp.Process),
					Scope: fp.IntegrationInstance, Source: string(req.Attestation.Source),
					Evidence: "process-birth tuple reported at bind",
					Until:    req.ValidUntil,
				})
			}
			b.hold(ref, session.ID, instance.ID, fp.Degraded(), "bind")
		}
	}
	if req.AgentSession.Valid() {
		ref := req.AgentSession.ClaimRef()
		if held, ok := a.claims[ref]; ok && held.Instance != instance.ID {
			return &ConflictError{
				Reason:  "agent-runtime session is already claimed by another duo session",
				Ref:     ref,
				Holders: []SessionID{held.Session},
			}
		} else if !ok {
			a.correlateAgent(b, instance.ID, Candidate{
				AgentSession: req.AgentSession,
				Transcript:   req.Transcript,
			}, req.Attestation, req.ValidUntil)
			b.hold(ref, session.ID, instance.ID, false, "bind")
		}
	}
	if req.BindActor != "" {
		if bound, ok := a.actorBinding[req.BindActor]; ok && bound != session.ID {
			return fmt.Errorf("%w: actor %s is bound to session %s",
				ErrActorBound, req.BindActor, bound)
		}
		if session.BoundActor != req.BindActor {
			b.fact(FactActorBound, Fact{
				SessionID: session.ID, InstanceID: instance.ID, ActorID: req.BindActor,
				Reason: string(req.Attestation.Source),
			})
		}
	}
	b.auditEntry(AuditEntry{Target: string(session.ID), Reason: "bind", Detail: req.Reason})

	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitIdentity, change)
}

// MarkLive records that a starting runtime instance became live (§5.1
// Starting -> Live). It is accepted process evidence, so it commits through
// the observation boundary.
func (a *Authority) MarkLive(ctx context.Context, id InstanceID, actor, evidence string) error {
	return a.advanceInstance(ctx, id, InstanceLive, actor, evidence, a.repo.CommitObservation)
}

// Stop requests that the current runtime instance stop (§5.2). It records a
// request, never an outcome: "Stop is a request, not proof of exit."
func (a *Authority) Stop(ctx context.Context, id InstanceID, actor, reason string) error {
	instance, err := a.requireInstance(id)
	if err != nil {
		return err
	}
	if instance.State.Terminal() {
		return fmt.Errorf("%w: instance %s", ErrInstanceExited, id)
	}
	if !canAdvance(instance.State, InstanceStopRequested) {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, instance.State, InstanceStopRequested)
	}
	b := a.change(actor)
	b.fact(FactStopRequestRecorded, Fact{
		SessionID: instance.Session, InstanceID: id, Reason: reason,
	}).
		fact(FactInstanceState, Fact{
			SessionID: instance.Session, InstanceID: id,
			State: string(InstanceStopRequested), Reason: reason,
		}).
		auditEntry(AuditEntry{Target: string(id), Reason: "stop requested", Detail: reason})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitCommandAcceptance, change)
}

// Exit records the final end of one runtime instance from authoritative
// process or host evidence (§5.2, §5.3).
//
// Exit releases the instance's active claims and retires its live
// correlations, and it leaves the Duo session inactive. It never removes the
// session, and nothing can move the instance again afterwards.
func (a *Authority) Exit(ctx context.Context, id InstanceID, actor, evidence string) error {
	instance, err := a.requireInstance(id)
	if err != nil {
		return err
	}
	if instance.State.Terminal() {
		return fmt.Errorf("%w: instance %s", ErrInstanceExited, id)
	}
	b := a.change(actor)
	a.exitInstance(b, instance, evidence)
	b.auditEntry(AuditEntry{Target: string(id), Reason: "exit", Detail: evidence})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitObservation, change)
}

// exitInstance adds every fact one exit implies. It is shared by Exit,
// Restart, and recovery, so all three release claims and retire correlations
// the same way.
func (a *Authority) exitInstance(b *changeBuilder, instance *RuntimeInstance, evidence string) {
	b.fact(FactInstanceState, Fact{
		SessionID: instance.Session, InstanceID: instance.ID,
		State: string(InstanceExited), Evidence: evidence, Reason: "exit",
	})
	for _, ref := range a.instanceClaims[instance.ID] {
		if held, ok := a.claims[ref]; ok && held.Instance == instance.ID {
			b.release(ref, "runtime instance exited")
		}
	}
	// §4.3: authoritative exit evidence retires the instance's live claims,
	// while the records themselves stay as history.
	for _, c := range a.Correlations(TargetInstance, string(instance.ID)) {
		if c.Status == CorrelationActive {
			b.fact(FactCorrelationStatus, Fact{
				CorrelationID: c.ID, State: string(CorrelationRetired),
				Reason: "runtime instance exited",
			})
		}
	}
	if s, ok := a.sessions[instance.Session]; ok && s.State == SessionActive {
		b.fact(FactSessionState, Fact{
			SessionID: s.ID, State: string(SessionInactive), Reason: "runtime instance exited",
		})
	}
}

// LaunchFailed records a launch that never produced a live process: the
// starting instance exits and the session is left inactive with the failure
// in its history (§5.2).
func (a *Authority) LaunchFailed(ctx context.Context, id InstanceID, actor, reason string) error {
	return a.Exit(ctx, id, actor, "launch failed: "+reason)
}

// Detach disables Duo's active host attachment while the external runtime
// continues (§5.2). The session, runtime instance, claims, and history all
// stay: a detached live runtime keeps its active claim reservation.
func (a *Authority) Detach(ctx context.Context, id SessionID, actor, reason string) error {
	return a.setAttachment(ctx, id, Detached, actor, reason, nil)
}

// Reattach revalidates a detached host and restores observation without
// changing a still-live runtime-instance ID (§5.2).
//
// Revalidation is the whole verb: the caller supplies the fingerprint it
// observes now, and it must be the same claim the session already holds.
// A pane that now holds a different execution is a new runtime instance, not
// a reattach — §6.4's rule that pane-ID reuse alone cannot establish
// continuity.
func (a *Authority) Reattach(ctx context.Context, id SessionID, actor string, fp Fingerprint) error {
	if err := fp.Validate(); err != nil {
		return err
	}
	ref := fp.ClaimRef()
	return a.setAttachment(ctx, id, Attached, actor, "reattach", &ref)
}

// setAttachment moves a session's host attachment, optionally revalidating
// the live-runtime claim first.
func (a *Authority) setAttachment(ctx context.Context, id SessionID, want AttachmentState, actor, reason string, revalidate *ClaimRef) error {
	session, err := a.requireSession(id)
	if err != nil {
		return err
	}
	if session.State != SessionActive {
		return fmt.Errorf("%w: session %s is %s", ErrSessionState, id, session.State)
	}
	attachment, ok := a.attachments[session.Attachment]
	if !ok {
		return fmt.Errorf("%w: session %s has no host attachment", ErrUnknownObject, id)
	}
	if attachment.State == want {
		return nil
	}
	if revalidate != nil {
		held, ok := a.claims[*revalidate]
		if !ok || held.Session != id {
			return &ConflictError{
				Reason: "observed fingerprint is not the claim this session holds; a new execution needs a new runtime instance",
				Ref:    *revalidate,
			}
		}
	}
	b := a.change(actor)
	b.fact(FactAttachmentState, Fact{
		SessionID: id, AttachmentID: attachment.ID, State: string(want), Reason: reason,
	}).auditEntry(AuditEntry{Target: string(id), Reason: reason})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitIdentity, change)
}

// ResumeRequest continues an inactive Duo session in a new runtime instance.
type ResumeRequest struct {
	Session SessionID
	Actor   string
	// Attestation must name explicit owner intent, a launch or resume plan,
	// or an instance-scoped claim. §5.2: resume needs "explicit owner
	// intent, a trusted continuation token, or equally strong integration
	// proof".
	Attestation Attestation
	Reason      string
}

// Resume continues an inactive Duo session in a new runtime instance (§5.2,
// §6.6). The Duo-session ID, owner, history, and actor binding survive; the
// runtime-instance ID does not.
func (a *Authority) Resume(ctx context.Context, req ResumeRequest) (InstanceID, error) {
	actor := req.Actor
	if actor == "" {
		actor = "authority"
	}
	session, err := a.requireSession(req.Session)
	if err != nil {
		return "", err
	}
	if session.State != SessionInactive {
		return "", fmt.Errorf("%w: resume needs an inactive session, %s is %s",
			ErrSessionState, session.ID, session.State)
	}
	if !attests(req.Attestation) {
		return "", fmt.Errorf("%w: resume of %s", ErrIntentRequired, session.ID)
	}
	b := a.change(actor)
	id, err := a.startInstance(b, session, "resume: "+req.Reason)
	if err != nil {
		return "", err
	}
	b.auditEntry(AuditEntry{Target: string(session.ID), Reason: "resume", Detail: req.Reason})
	change, err := b.build()
	if err != nil {
		return "", err
	}
	if err := a.commit(ctx, a.repo.CommitIdentity, change); err != nil {
		return "", err
	}
	return id, nil
}

// RestartRequest ends the current runtime instance and starts a new one in
// the same Duo session.
type RestartRequest struct {
	Session SessionID
	Actor   string
	// Attestation carries the explicit restart policy §5.2 requires.
	Attestation Attestation
	// ExitEvidence is the authoritative evidence that the old execution
	// ended. Restart does not get to assume it: §5.3 makes exit an accepted
	// fact, whatever verb records it.
	ExitEvidence string
	Reason       string
}

// Restart ends the old runtime instance and creates a new one in the same
// Duo session (§5.2, §6.4). The host container can stay attached; the
// runtime-instance ID never carries across the boundary.
func (a *Authority) Restart(ctx context.Context, req RestartRequest) (InstanceID, error) {
	actor := req.Actor
	if actor == "" {
		actor = "authority"
	}
	session, err := a.requireSession(req.Session)
	if err != nil {
		return "", err
	}
	if !attests(req.Attestation) {
		return "", fmt.Errorf("%w: restart of %s", ErrIntentRequired, session.ID)
	}
	if req.ExitEvidence == "" {
		return "", fmt.Errorf("%w: restart needs exit evidence for the old runtime instance", ErrIntentRequired)
	}
	if session.Current == "" {
		return "", fmt.Errorf("%w: session %s", ErrNoCurrentInstance, session.ID)
	}
	old, err := a.requireInstance(session.Current)
	if err != nil {
		return "", err
	}

	b := a.change(actor)
	if !old.State.Terminal() {
		a.exitInstance(b, old, req.ExitEvidence)
	}
	id, err := a.startInstance(b, session, "restart: "+req.Reason)
	if err != nil {
		return "", err
	}
	b.auditEntry(AuditEntry{
		Target: string(session.ID), Reason: "restart",
		Detail: "instance " + string(old.ID) + " ended; " + string(id) + " started",
	})
	change, err := b.build()
	if err != nil {
		return "", err
	}
	if err := a.commit(ctx, a.repo.CommitIdentity, change); err != nil {
		return "", err
	}
	return id, nil
}

// startInstance adds the facts that create a new starting runtime instance
// on an existing session and make it current. Resume and restart share it,
// which is why neither can accidentally reuse an instance ID.
func (a *Authority) startInstance(b *changeBuilder, session *Session, reason string) (InstanceID, error) {
	id := InstanceID(b.mint(instancePrefix))
	_, fingerprint, err := mintCredential()
	if err != nil {
		return "", err
	}
	instance := RuntimeInstance{
		ID: id, Session: session.ID, State: InstanceStarting,
		CredentialFingerprint: fingerprint, StartedAt: b.at,
	}
	b.fact(FactInstanceStarted, Fact{Instance: &instance, SessionID: session.ID, Reason: reason}).
		fact(FactInstanceCurrent, Fact{SessionID: session.ID, InstanceID: id, Reason: reason}).
		fact(FactSessionState, Fact{SessionID: session.ID, State: string(SessionActive), Reason: reason}).
		fact(FactCredentialIssued, Fact{
			SessionID: session.ID, InstanceID: id, Detail: fingerprint,
			Reason: "instance-scoped reporter credential",
		})
	return id, nil
}

// Archive moves an inactive session out of ordinary active use (§5.2).
func (a *Authority) Archive(ctx context.Context, id SessionID, actor, reason string) error {
	return a.setSessionState(ctx, id, SessionArchived, []SessionState{SessionInactive}, actor, reason)
}

// Restore returns an archived session to inactive (§5.2).
func (a *Authority) Restore(ctx context.Context, id SessionID, actor, reason string) error {
	return a.setSessionState(ctx, id, SessionInactive, []SessionState{SessionArchived}, actor, reason)
}

// Remove retires an archived session, keeping the tombstone that prevents ID
// reuse and explains dangling references (§5.2). Removal is terminal.
func (a *Authority) Remove(ctx context.Context, id SessionID, actor, reason string) error {
	return a.setSessionState(ctx, id, SessionRemoved, []SessionState{SessionArchived}, actor, reason)
}

// setSessionState moves a session's durable state, refusing any move §5.1
// does not allow from its current state.
func (a *Authority) setSessionState(ctx context.Context, id SessionID, want SessionState, from []SessionState, actor, reason string) error {
	session, err := a.requireSession(id)
	if err != nil {
		return err
	}
	allowed := false
	for _, s := range from {
		if session.State == s {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, session.State, want)
	}
	if want == SessionArchived && session.Current != "" {
		if inst, ok := a.instances[session.Current]; ok && !inst.State.Terminal() {
			return fmt.Errorf("%w: session %s still has a live runtime instance", ErrSessionState, id)
		}
	}
	b := a.change(actor)
	b.fact(FactSessionState, Fact{SessionID: id, State: string(want), Reason: reason}).
		auditEntry(AuditEntry{Target: string(id), Reason: string(want), Detail: reason})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitIdentity, change)
}

// advanceInstance applies one runtime-instance transition, checking it
// against §5.1's diagram first.
func (a *Authority) advanceInstance(
	ctx context.Context,
	id InstanceID,
	want InstanceState,
	actor, evidence string,
	boundary func(context.Context, Change) error,
) error {
	instance, err := a.requireInstance(id)
	if err != nil {
		return err
	}
	if instance.State.Terminal() {
		return fmt.Errorf("%w: instance %s", ErrInstanceExited, id)
	}
	if !canAdvance(instance.State, want) {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, instance.State, want)
	}
	b := a.change(actor)
	b.fact(FactInstanceState, Fact{
		SessionID: instance.Session, InstanceID: id,
		State: string(want), Evidence: evidence,
	}).auditEntry(AuditEntry{Target: string(id), Reason: string(want), Detail: evidence})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, boundary, change)
}

// recordLateReport stores a report that arrived after its instance exited.
// §5.3 lets it enrich history and forbids it from changing anything else.
func (a *Authority) recordLateReport(ctx context.Context, actor string, instance *RuntimeInstance, detail string) error {
	b := a.change(actor)
	b.fact(FactLateReport, Fact{
		SessionID: instance.Session, InstanceID: instance.ID,
		Reason: "report arrived after exit", Detail: detail,
	}).auditEntry(AuditEntry{
		Target: string(instance.ID), Reason: "late report", Detail: detail,
	})
	change, err := b.build()
	if err != nil {
		return err
	}
	if err := a.commit(ctx, a.repo.CommitObservation, change); err != nil {
		return err
	}
	return fmt.Errorf("%w: instance %s", ErrInstanceExited, instance.ID)
}

// describeBindEvidence renders everything a bind request carried, so a parked
// report explains itself without the reader reconstructing the call.
func describeBindEvidence(req BindRequest) string {
	parts := []string{"source=" + string(req.Attestation.Source)}
	if req.Fingerprint != nil {
		parts = append(parts, describeFingerprint(*req.Fingerprint))
	}
	if req.AgentSession.Valid() {
		parts = append(parts, "agent_session="+req.AgentSession.IntegrationInstance+"/"+req.AgentSession.SessionID)
	}
	if req.Transcript != "" {
		parts = append(parts, "transcript="+req.Transcript)
	}
	if req.BindActor != "" {
		parts = append(parts, "bind_actor="+string(req.BindActor))
	}
	return strings.Join(parts, " ")
}

// verifyAttestation checks a claim's source against §3.4's three admissible
// sources, authenticating the instance-scoped credential when that is what
// is offered.
func (a *Authority) verifyAttestation(instance *RuntimeInstance, att Attestation) error {
	switch att.Source {
	case SourceOwner, SourceLaunchPlan:
		return nil
	case SourceInstanceClaim:
		if att.Instance != instance.ID {
			return fmt.Errorf("%w: credential is scoped to instance %s, not %s",
				ErrNotAttested, att.Instance, instance.ID)
		}
		presented := credentialFingerprint(att.Credential)
		if att.Credential == "" || instance.CredentialFingerprint == "" ||
			subtle.ConstantTimeCompare([]byte(presented), []byte(instance.CredentialFingerprint)) != 1 {
			return fmt.Errorf("%w: credential does not match instance %s", ErrNotAttested, instance.ID)
		}
		return nil
	default:
		return ErrNotAttested
	}
}
