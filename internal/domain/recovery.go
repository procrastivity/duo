package domain

import (
	"context"
	"fmt"
)

// RecoveryOutcome is what an integration proved about a runtime instance
// that was live when the Duo authority stopped. The five values are
// decision-01 §4.4's five recovery rules, one each.
type RecoveryOutcome string

// The recovery outcomes.
const (
	// RecoverySameLive: the host proves the same host object and process
	// birth remain live. The runtime-instance ID is kept and its
	// correlations are reactivated (rule 1).
	RecoverySameLive RecoveryOutcome = "same-live"
	// RecoveryExited: the host proves exit. The instance becomes final
	// (rule 2).
	RecoveryExited RecoveryOutcome = "exited"
	// RecoveryReplaced: the host object remains but process birth changed.
	// The old instance exits; only an explicit restart or resume may then
	// create a new one (rule 3).
	RecoveryReplaced RecoveryOutcome = "replaced"
	// RecoveryUnreachable: the host cannot be reached. The claim
	// reservation is kept and exit is not inferred (rule 4).
	RecoveryUnreachable RecoveryOutcome = "unreachable"
	// RecoveryConflicted: recovery found a conflicting live claim. Both
	// sessions are quarantined from automatic control until an owner
	// resolves it (rule 5).
	RecoveryConflicted RecoveryOutcome = "conflicted"
)

// RecoveryEvidence is what an integration reports about one recovering
// runtime instance.
type RecoveryEvidence struct {
	Outcome RecoveryOutcome
	Actor   string
	// Fingerprint is the live-runtime evidence observed now. RecoverySameLive
	// requires it, and requires it to carry process birth.
	Fingerprint *Fingerprint
	// Conflicting names the other Duo session found holding a live claim,
	// for RecoveryConflicted.
	Conflicting SessionID
	Detail      string
}

// ResolveRecovery applies decision-01 §4.4's recovery rules to one
// recovering runtime instance.
//
// Nothing here infers: an unreachable host leaves the instance recovering
// with its claim reserved, and only proof of the same process birth keeps a
// runtime-instance ID alive across the authority restart.
func (a *Authority) ResolveRecovery(ctx context.Context, id InstanceID, ev RecoveryEvidence) error {
	instance, err := a.requireInstance(id)
	if err != nil {
		return err
	}
	actor := ev.Actor
	if actor == "" {
		actor = "authority"
	}
	if instance.State.Terminal() {
		return fmt.Errorf("%w: instance %s", ErrInstanceExited, id)
	}

	session, err := a.requireSession(instance.Session)
	if err != nil {
		return err
	}

	b := a.change(actor)
	switch ev.Outcome {
	case RecoverySameLive:
		if err := a.proveContinuity(instance, ev); err != nil {
			// The host answered and the answer did not prove that this
			// execution is the one Duo claimed. That is the conformance
			// matrix's "missing identity or continuity evidence" row, not a
			// bare refusal: record the degradation durably, then return the
			// refusal. Nothing is resolved — the instance stays in the
			// recovering view.
			return a.degrade(ctx, actor, session, instance, err)
		}
		// Rule 1: keep the runtime-instance ID and reactivate its
		// correlations.
		for _, c := range a.Correlations(TargetInstance, string(id)) {
			if c.Status == CorrelationStale {
				b.fact(FactCorrelationStatus, Fact{
					CorrelationID: c.ID, State: string(CorrelationActive),
					Reason: "revalidated during recovery",
				})
			}
		}
		// Rule 1 is also the re-proof this kernel recognizes: same host
		// object, same process birth. A session that had degraded to
		// unverified continuity leaves that state here and nowhere else.
		a.markContinuity(b, session, instance.ID, ContinuityVerified,
			"host proved the same host object and process birth")

	case RecoveryExited:
		// Rule 2: the host proves exit.
		a.exitInstance(b, instance, "recovery: "+ev.Detail)

	case RecoveryReplaced:
		// Rule 3: the container survived but the execution did not. The old
		// instance ends here; §6.4 leaves creating the replacement to an
		// explicit restart or resume, because pane-ID reuse is not
		// continuity.
		a.exitInstance(b, instance, "recovery: process birth changed behind a surviving host container")
		a.markContinuity(b, session, instance.ID, ContinuityUnverified,
			"process birth changed behind a surviving host container")

	case RecoveryUnreachable:
		// Rule 4: keep the reservation, report disconnected, infer nothing.
		// Inferring nothing is not the same as carrying on: an unreachable
		// host is precisely a host that has not proven continuity, so the
		// attachment degrades even though the claim and the instance state
		// stay exactly where they were.
		a.markContinuity(b, session, instance.ID, ContinuityUnverified,
			"host unreachable during recovery; continuity not proven")

	case RecoveryConflicted:
		// Rule 5: quarantine both claims from automatic control.
		//
		// Two judgments here. The quarantine is recorded per *session*
		// rather than per claim: a claim is not a control target, and every
		// verb §4.4 withholds ("automatic control") is addressed to a
		// session. And no claim is released — releasing one would let the
		// contested runtime be enrolled again, which is the automatic merge
		// §4.2 forbids. The claims stay held and inert until an owner
		// resolves the conflict.
		b.fact(FactSessionQuarantined, Fact{
			SessionID: instance.Session, State: "true",
			Reason: "recovery found a conflicting live claim", Detail: string(ev.Conflicting),
		})
		a.markContinuity(b, session, instance.ID, ContinuityUnverified,
			"a conflicting live claim contests this attachment")
		if ev.Conflicting != "" {
			other, err := a.requireSession(ev.Conflicting)
			if err != nil {
				return err
			}
			b.fact(FactSessionQuarantined, Fact{
				SessionID: ev.Conflicting, State: "true",
				Reason: "recovery found a conflicting live claim", Detail: string(instance.Session),
			})
			a.markContinuity(b, other, other.Current, ContinuityUnverified,
				"a conflicting live claim contests this attachment")
		}

	default:
		return fmt.Errorf("%w: unknown recovery outcome %q", ErrIllegalTransition, ev.Outcome)
	}

	b.fact(FactRecoveryDecision, Fact{
		SessionID: instance.Session, InstanceID: id,
		State: string(ev.Outcome), Detail: ev.Detail,
		Evidence: describeRecovery(ev),
	}).auditEntry(AuditEntry{
		Target: string(id), Reason: "recovery " + string(ev.Outcome), Detail: ev.Detail,
	})

	change, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitObservation, change)
}

// proveContinuity enforces §4.4 rule 1's precondition: the *same* host
// object and process birth. A fingerprint with no process-birth identity
// cannot satisfy it — the Herdr 0.8.2 probe (notes/19 §5) shows a restarted
// server restoring a pane under its old pane_id, so container coordinates
// alone would happily "prove" continuity for a process that no longer
// exists.
func (a *Authority) proveContinuity(instance *RuntimeInstance, ev RecoveryEvidence) error {
	if ev.Fingerprint == nil {
		return fmt.Errorf("%w: same-live recovery needs an observed fingerprint", ErrIntentRequired)
	}
	if err := ev.Fingerprint.Validate(); err != nil {
		return err
	}
	if !ev.Fingerprint.Process.Present() {
		return fmt.Errorf("%w: same-live recovery needs process-birth evidence; container coordinates alone do not prove continuity",
			ErrIntentRequired)
	}
	ref := ev.Fingerprint.ClaimRef()
	held, ok := a.claims[ref]
	if !ok || held.Instance != instance.ID {
		return &ConflictError{
			Reason: "observed live runtime is not the one this instance claimed; a changed process birth is a new runtime instance",
			Ref:    ref,
		}
	}
	return nil
}

// describeRecovery renders recovery evidence for the fact log.
func describeRecovery(ev RecoveryEvidence) string {
	if ev.Fingerprint == nil {
		return "outcome=" + string(ev.Outcome)
	}
	return "outcome=" + string(ev.Outcome) + " " + describeFingerprint(*ev.Fingerprint)
}
