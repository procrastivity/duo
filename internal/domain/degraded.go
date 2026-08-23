package domain

import (
	"context"
	"fmt"
)

// Degraded continuity: the third recovery outcome shape.
//
// decision-01 §4.4 names five recovery rules but leaves one consequence
// implicit: what a session may still *do* while the host has not proven that
// the execution behind its container is the one Duo claimed. The locked
// decision-ledger row "Identity-evidence degradation" and
// duo-vnext-integration-conformance.md §10 make it explicit, and they are one
// triple, not three independent switches:
//
//	Mark attachment continuity unverified, park instance-scoped reports as
//	unresolved evidence, and disable write paths that need an exact live
//	target. A pane ID or PID alone never proves continuity.
//
// All three are implemented here. The first is durable state on the host
// attachment; the second is a durable fact that is deliberately never applied
// to identity state; the third is derived from both, so no code path can
// enable an exact-target write without a fact that says continuity was
// proven.

// ContinuityState is whether a host attachment's link to a live execution is
// still proven.
type ContinuityState string

// The continuity states. Verified is the zero value on purpose: an
// attachment is created from evidence that was just revalidated, and a
// migration or a replayed log that predates this field must not read as
// degraded.
const (
	ContinuityVerified   ContinuityState = ""
	ContinuityUnverified ContinuityState = "unverified"
)

// String renders a continuity state, naming the zero value.
func (c ContinuityState) String() string {
	if c == ContinuityVerified {
		return "verified"
	}
	return string(c)
}

// ParkedReport is one instance report that arrived while its session's
// attachment continuity was unverified: durably recorded, never applied.
//
// decision-02's 2026-08-14 G-03 amendment gives parked evidence a retention
// window and a retroactive-binding rule. Neither is implemented here — both
// are Stage 2 ingestion concerns, and this kernel owns only the identity
// half: the report is kept, attributable, and inert. What it keeps is chosen
// so that the later exact-match rule has what it needs: the scoped
// agent-runtime session identity structurally, and everything else as
// evidence text.
type ParkedReport struct {
	ID      ParkedReportID
	Session SessionID
	// Instance is the runtime instance the report claimed to target.
	Instance InstanceID
	// Source is the attestation source the report presented. A parked
	// report is not an unattested one: it was refused for want of *host*
	// continuity, not for want of a binding source.
	Source BindingSource
	// AgentSession and Transcript are the agent-runtime correlations the
	// report offered. They are kept structurally because the retroactive
	// exact-match rule keys on them.
	AgentSession AgentSessionRef
	Transcript   string
	// Evidence renders whatever else the report carried, such as a
	// live-runtime fingerprint.
	Evidence   string
	Reason     string
	ReceivedAt string
}

// WriteRefusal names why exact-target writes are unavailable. It is a closed
// vocabulary so a caller can map a refusal to an error code or a presentation
// state without matching on prose.
type WriteRefusal string

// The write refusals.
const (
	// WriteRefusalNone is the allowed case.
	WriteRefusalNone WriteRefusal = ""
	// WriteRefusalUnknownSession: no such Duo session.
	WriteRefusalUnknownSession WriteRefusal = "unknown-session"
	// WriteRefusalSessionState: the session is inactive, archived, or
	// removed.
	WriteRefusalSessionState WriteRefusal = "session-state"
	// WriteRefusalQuarantined: recovery found a conflicting live claim and
	// §4.4 rule 5 withholds the session from automatic control.
	WriteRefusalQuarantined WriteRefusal = "quarantined"
	// WriteRefusalNoLiveInstance: the session has no current runtime
	// instance, or its current one has exited.
	WriteRefusalNoLiveInstance WriteRefusal = "no-live-instance"
	// WriteRefusalRecovering: the authority restarted and no integration has
	// validated this instance yet (§4.4's recovering view).
	WriteRefusalRecovering WriteRefusal = "recovering"
	// WriteRefusalContinuityUnverified: continuity to the live execution was
	// not proven.
	WriteRefusalContinuityUnverified WriteRefusal = "continuity-unverified"
)

// WriteGate is the answer to "may Duo write to this session's exact live
// target right now".
type WriteGate struct {
	Allowed bool
	Refusal WriteRefusal
	Reason  string
}

// ExactTargetWrites reports whether write paths that need an exact live
// target are available for one session.
//
// It is a pure function of durable facts plus the startup recovering view,
// and it is the only place that decides. The refusals are ordered from the
// broadest to the narrowest, so the reason a caller sees is the first thing
// an owner would have to fix.
//
// What it deliberately does not gate on: a detached host attachment. Detach
// disables Duo's attachment while "the external runtime can continue" (§5.2),
// and choosing a delivery path — host-mediated, agent-native, or none — is
// the control and delivery domain's decision, not an identity one. This gate
// answers only whether identity evidence permits an exact-target write at
// all.
func (a *Authority) ExactTargetWrites(id SessionID) WriteGate {
	refuse := func(r WriteRefusal, reason string) WriteGate {
		return WriteGate{Refusal: r, Reason: reason}
	}
	session, ok := a.sessions[id]
	if !ok {
		return refuse(WriteRefusalUnknownSession, "no duo session "+string(id))
	}
	if session.State != SessionActive {
		return refuse(WriteRefusalSessionState, "session is "+string(session.State))
	}
	if session.Quarantined {
		return refuse(WriteRefusalQuarantined,
			"recovery found a conflicting live claim; an owner must resolve it before automatic control resumes")
	}
	if session.Current == "" {
		return refuse(WriteRefusalNoLiveInstance, "session has no current runtime instance")
	}
	instance, ok := a.instances[session.Current]
	switch {
	case !ok || instance.State.Terminal():
		return refuse(WriteRefusalNoLiveInstance, "the current runtime instance has exited")
	case instance.State == InstanceStarting:
		// §5.1's Starting is "launched, not yet live". There is a Duo ID but
		// no proven execution behind it, and an exact-target write needs the
		// target to exist.
		return refuse(WriteRefusalNoLiveInstance, "the current runtime instance has not become live yet")
	}
	// Continuity outranks the recovering view, and the order carries
	// meaning: an unverified attachment is a durable finding about evidence
	// that was examined, while recovering is only "no integration has
	// answered yet". A host that answered "unreachable" leaves both true,
	// and the caller deserves the finding rather than the waiting state.
	if att, ok := a.attachments[session.Attachment]; ok &&
		att.Continuity == ContinuityUnverified && att.ContinuityInstance == session.Current {
		return refuse(WriteRefusalContinuityUnverified,
			"host continuity to this runtime instance is unverified; a pane id or pid alone never proves it")
	}
	if a.recovering[session.Current] {
		return refuse(WriteRefusalRecovering,
			"the authority restarted and no integration has validated this runtime instance yet")
	}
	return WriteGate{Allowed: true}
}

// Continuity reports one session's attachment continuity. A session with no
// host attachment at all — the hostless case the architecture keeps open —
// has no host continuity to lose and reports verified.
func (a *Authority) Continuity(id SessionID) (ContinuityState, bool) {
	session, ok := a.sessions[id]
	if !ok {
		return "", false
	}
	att, ok := a.attachments[session.Attachment]
	if !ok {
		return ContinuityVerified, true
	}
	return att.Continuity, true
}

// ParkedReports lists the parked reports held for one session, oldest first.
func (a *Authority) ParkedReports(id SessionID) []ParkedReport {
	out := make([]ParkedReport, 0, len(a.parked[id]))
	for _, p := range a.parked[id] {
		out = append(out, *p)
	}
	return out
}

// degraded reports whether an instance report aimed at instance must be
// parked rather than applied.
//
// The check is instance-scoped, not session-scoped, and that is load-bearing.
// A degraded episode belongs to one execution generation: §6.4 leaves an
// explicit restart or resume as the way forward, and a session-scoped check
// would park the new instance's evidence too, leaving no path back to a
// verified attachment for a host that cannot report process birth.
func (a *Authority) degraded(session *Session, instance InstanceID) bool {
	att, ok := a.attachments[session.Attachment]
	if !ok {
		return false
	}
	return att.Continuity == ContinuityUnverified && att.ContinuityInstance == instance
}

// markContinuity adds the fact that moves one session's attachment
// continuity. It is a no-op for a session with no host attachment, and for a
// state the attachment already holds.
func (a *Authority) markContinuity(b *changeBuilder, session *Session, instance InstanceID, want ContinuityState, reason string) {
	att, ok := a.attachments[session.Attachment]
	if !ok {
		return
	}
	if att.Continuity == want && att.ContinuityInstance == instance {
		return
	}
	if want == ContinuityVerified {
		instance = ""
	}
	b.fact(FactAttachmentContinuity, Fact{
		SessionID: session.ID, AttachmentID: att.ID, InstanceID: instance,
		State: string(want), Reason: reason,
	})
}

// degrade records a failed continuity proof and returns the refusal that
// caused it. It resolves nothing: a degraded instance stays in the recovering
// view, keeps its claim reservation, and keeps its state, exactly as §4.4
// rule 4 requires of evidence that proves nothing.
func (a *Authority) degrade(ctx context.Context, actor string, session *Session, instance *RuntimeInstance, cause error) error {
	b := a.change(actor)
	a.markContinuity(b, session, instance.ID, ContinuityUnverified,
		"continuity proof refused: "+cause.Error())
	b.auditEntry(AuditEntry{
		Target: string(instance.ID), Reason: "continuity unverified", Detail: cause.Error(),
	})
	change, err := b.build()
	if err != nil {
		return err
	}
	if len(change.Facts) == 0 {
		// Nothing to record: the attachment is already degraded for this
		// instance, or the session is hostless and has no attachment whose
		// continuity could degrade.
		return cause
	}
	if err := a.commit(ctx, a.repo.CommitObservation, change); err != nil {
		return err
	}
	return cause
}

// park records one report as unresolved evidence and applies nothing else.
// It commits through the observation boundary: a parked report is accepted
// evidence about a runtime, it just does not move identity state.
func (a *Authority) park(ctx context.Context, actor string, session *Session, report ParkedReport) error {
	b := a.change(actor)
	id := b.mint(parkedPrefix)
	report.ID = ParkedReportID(id)
	report.Session = session.ID
	report.ReceivedAt = b.at
	b.fact(FactReportParked, Fact{
		Parked: &report, SessionID: session.ID, InstanceID: report.Instance,
		Reason: report.Reason, Evidence: report.Evidence,
	}).auditEntry(AuditEntry{
		Target: string(session.ID), Reason: "report parked as unresolved evidence",
		Detail: report.Reason,
	})
	change, err := b.build()
	if err != nil {
		return err
	}
	if err := a.commit(ctx, a.repo.CommitObservation, change); err != nil {
		return err
	}
	return fmt.Errorf("%w: parked as %s", ErrContinuityUnverified, report.ID)
}

// ResolveQuarantine records an owner's resolution of the conflicting live
// claims that §4.4 rule 5 quarantined a session for.
//
// Rule 5 withholds a session "until an owner resolves the conflict", so the
// resolution needs a verb, and §3.4 makes an explicit owner action the only
// admissible source for one. Resolving the quarantine does not prove
// anything about the runtime: continuity stays where the recovery decision
// left it, so a resolved session still cannot take exact-target writes until
// the host proves the same live execution. Nothing merges, and no claim is
// released — an automatic merge is exactly what §4.2 forbids.
func (a *Authority) ResolveQuarantine(ctx context.Context, id SessionID, actor string, att Attestation, reason string) error {
	session, err := a.requireSession(id)
	if err != nil {
		return err
	}
	if att.Source != SourceOwner {
		return fmt.Errorf("%w: resolving a quarantine is an owner action", ErrNotAttested)
	}
	if !session.Quarantined {
		return nil
	}
	b := a.change(actor)
	b.fact(FactSessionQuarantined, Fact{
		SessionID: id, State: "false", Reason: reason, Detail: att.Subject,
	}).auditEntry(AuditEntry{
		Target: string(id), Reason: "quarantine resolved by owner", Detail: reason,
	})
	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitIdentity, change)
}
