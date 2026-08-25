package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch/materialize"
	"github.com/procrastivity/duo/internal/surface"
)

// reconcileCallTimeout bounds one ValidateAttachment round trip during
// session.reconcile. It is well below herdr.DefaultCallTimeout (10s) so a
// dead socket fails fast instead of blocking the writer lease.
const reconcileCallTimeout = 2 * time.Second

// attachmentClass is one attachment's classification after a host probe,
// before the instance outcome table collapses them.
type attachmentClass string

const (
	attUnreachable attachmentClass = "unreachable"
	attExited      attachmentClass = "exited"
	attSameLive    attachmentClass = "same-live"
	attReplaced    attachmentClass = "replaced"
)

// reconcileValidatorFor builds a HostAttachmentValidator for one
// integration-instance ID. Production uses Herdr (socket under SessionsDir,
// short CallTimeout). Tests replace this with a fake so unit tests never
// need a live Herdr socket.
var reconcileValidatorFor = defaultReconcileValidator

// defaultReconcileValidator resolves a Herdr adapter for one integration
// instance. Missing session name, missing socket file, or herdr.New failure
// is unreachable — never pane-absent. herdr.New does not dial.
func defaultReconcileValidator(integrationInstance string) (host.HostAttachmentValidator, error) {
	socket, err := herdrSocketForIntegration(integrationInstance)
	if err != nil {
		return nil, err
	}
	return herdr.New(herdr.Config{
		IntegrationInstanceID: integrationInstance,
		SocketPath:            socket,
		CallTimeout:           reconcileCallTimeout,
	})
}

// herdrSocketForIntegration maps a recorded integration-instance ID onto
// a Herdr API socket. Two shapes exist in stores:
//
//   - herdr:<session-name> — InstanceIDForSession; socket is
//     SessionsDir/<name>/herdr.sock.
//   - herdr:<socket-path> — the launch --host locator when deduction
//     recorded a path rather than a session name (SessionForInstanceID
//     returns "" because the remainder contains a separator).
//
// Either missing file is unreachable, not pane-absent.
func herdrSocketForIntegration(integrationInstance string) (string, error) {
	session := herdr.SessionForInstanceID(integrationInstance)
	if session != "" {
		dir, err := herdr.SessionsDir()
		if err != nil {
			return "", host.Unreachable(err)
		}
		socket := filepath.Join(dir, session, herdr.SessionSocketName)
		if _, err := os.Stat(socket); err != nil {
			return "", host.Unreachable(err)
		}
		return socket, nil
	}
	rest, ok := strings.CutPrefix(integrationInstance, "herdr:")
	if !ok || !strings.ContainsAny(rest, `/\`) {
		return "", host.Unreachable(fmt.Errorf("no herdr session name or socket path in integration instance %q", integrationInstance))
	}
	if _, err := os.Stat(rest); err != nil {
		return "", host.Unreachable(err)
	}
	return rest, nil
}

// sessionReconcileItem is one instance's reconcile result.
type sessionReconcileItem struct {
	SessionID  string `json:"session_id"`
	InstanceID string `json:"instance_id"`
	Outcome    string `json:"outcome"`
	Reason     string `json:"reason,omitempty"`
}

type sessionReconcileResult struct {
	Items []sessionReconcileItem `json:"items"`
}

// sessionReconcileCommand constructs `duo session reconcile`:
// internal/registry's "session.reconcile" operation, CLI path
// {"session", "reconcile"}.
func sessionReconcileCommand(streams *iostreams.Streams) *cobra.Command {
	var actor, reason string

	cmd := &cobra.Command{
		Use:   "reconcile [session-id...]",
		Short: "probe recovering sessions and apply one recovery decision per instance",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output

			a, s, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			instances, err := reconcileTargets(a, args)
			if err != nil {
				return err
			}

			result := sessionReconcileResult{Items: []sessionReconcileItem{}}
			for _, id := range instances {
				item, err := reconcileOne(cmd.Context(), a, id, actor, reason)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, item)
			}
			return renderSessionReconcile(streams, mode, result)
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on every fact")
	cmd.Flags().StringVar(&reason, "reason", "", "a free-form reason recorded on the audit row")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// reconcileTargets selects recovering instances. No IDs: every recovering
// instance. With IDs: recovering instances whose session is in the list.
// An unknown session ID is object.not_found.
func reconcileTargets(a *domain.Authority, args []string) ([]domain.InstanceID, error) {
	if len(args) == 0 {
		return a.Recovering(), nil
	}
	wanted := make(map[domain.SessionID]bool, len(args))
	for _, raw := range args {
		id := domain.SessionID(raw)
		if _, ok := a.Session(id); !ok {
			return nil, duoerr.New("object.not_found", fmt.Sprintf("No session named %q is known.", raw))
		}
		wanted[id] = true
	}
	var out []domain.InstanceID
	for _, iid := range a.Recovering() {
		inst, ok := a.Instance(iid)
		if !ok {
			continue
		}
		if wanted[inst.Session] {
			out = append(out, iid)
		}
	}
	return out, nil
}

// reconcileOne probes every attachment of one recovering instance, maps
// the locked instance table, and calls ResolveRecovery exactly once.
func reconcileOne(ctx context.Context, a *domain.Authority, id domain.InstanceID, actor, reasonFlag string) (sessionReconcileItem, error) {
	inst, ok := a.Instance(id)
	if !ok {
		return sessionReconcileItem{}, duoerr.New("object.not_found", fmt.Sprintf("No runtime instance named %q is known.", id))
	}
	atts := a.Attachments(inst.Session)
	classes := make([]attachmentClass, 0, len(atts))
	var sameLiveAtt *domain.HostAttachment
	details := make([]string, 0, len(atts))

	for i := range atts {
		att := atts[i]
		class, detail := classifyAttachment(ctx, att)
		classes = append(classes, class)
		details = append(details, detail)
		if class == attSameLive && sameLiveAtt == nil {
			cp := att
			sameLiveAtt = &cp
		}
	}

	outcome, detail := instanceOutcome(classes, details)
	if reasonFlag != "" {
		if detail != "" {
			detail = reasonFlag + "; " + detail
		} else {
			detail = reasonFlag
		}
	}

	ev := domain.RecoveryEvidence{
		Outcome: outcome,
		Actor:   actor,
		Detail:  detail,
	}
	if outcome == domain.RecoverySameLive && sameLiveAtt != nil {
		fp := fingerprintFromAttachment(*sameLiveAtt)
		ev.Fingerprint = &fp
	}

	if err := a.ResolveRecovery(ctx, id, ev); err != nil {
		return sessionReconcileItem{}, duoerrFromDomain(err)
	}
	return sessionReconcileItem{
		SessionID:  string(inst.Session),
		InstanceID: string(id),
		Outcome:    string(outcome),
		Reason:     detail,
	}, nil
}

// classifyAttachment probes one attachment and maps host evidence onto the
// locked per-attachment classes (notes/48 §3).
func classifyAttachment(ctx context.Context, att domain.HostAttachment) (attachmentClass, string) {
	v, err := reconcileValidatorFor(att.IntegrationInstance)
	if err != nil {
		if errors.Is(err, host.ErrUnreachable) {
			return attUnreachable, "host unreachable: " + err.Error()
		}
		return attUnreachable, "host validator unavailable: " + err.Error()
	}
	if v == nil {
		return attUnreachable, "host validator missing for " + att.IntegrationInstance
	}

	claim := attachmentClaim(att)
	got, err := v.ValidateAttachment(ctx, claim)
	if err != nil {
		if errors.Is(err, host.ErrUnreachable) {
			return attUnreachable, "host unreachable: " + err.Error()
		}
		return attUnreachable, "host probe failed: " + err.Error()
	}

	switch got.Class {
	case host.ContinuityPaneAbsent:
		return attExited, "pane absent"
	case host.ContinuitySameLive:
		// same-live requires a stored process birth; without it continuity
		// cannot be proven and the attachment is treated as replaced.
		if !att.Process.Present() {
			return attReplaced, "same pane but process birth unknown on attachment"
		}
		return attSameLive, "same live process"
	case host.ContinuityTerminalReplaced:
		return attReplaced, "terminal identity replaced"
	case host.ContinuityProcessReplaced:
		return attReplaced, "process birth replaced"
	case host.ContinuityUnproven:
		return attReplaced, "process birth unproven"
	default:
		return attReplaced, fmt.Sprintf("host class %q", got.Class)
	}
}

// instanceOutcome collapses per-attachment classes into one RecoveryOutcome
// (notes/48 §5). RecoveryConflicted is never synthesized here.
func instanceOutcome(classes []attachmentClass, details []string) (domain.RecoveryOutcome, string) {
	if len(classes) == 0 {
		return domain.RecoveryReplaced, "no attachments to probe"
	}
	joined := joinDetails(details)

	anyUnreachable := false
	allSameLive := true
	allExited := true
	for _, c := range classes {
		if c == attUnreachable {
			anyUnreachable = true
		}
		if c != attSameLive {
			allSameLive = false
		}
		if c != attExited {
			allExited = false
		}
	}
	if anyUnreachable {
		return domain.RecoveryUnreachable, joined
	}
	if allSameLive {
		return domain.RecoverySameLive, joined
	}
	if allExited {
		return domain.RecoveryExited, joined
	}
	return domain.RecoveryReplaced, joined
}

func joinDetails(details []string) string {
	out := ""
	for i, d := range details {
		if d == "" {
			continue
		}
		if out != "" {
			out += "; "
		}
		if len(details) > 1 {
			out += fmt.Sprintf("[%d] %s", i+1, d)
		} else {
			out += d
		}
	}
	return out
}

// attachmentClaim builds ValidateAttachment's claim from a stored
// HostAttachment. Herdr field crossing: terminal_id → epoch, pane_id →
// container. A Present process birth is tagged StartTimeSourceProcfs so
// Herdr's sameBirth can treat the stored tuple as proven (domain wire
// does not carry the source).
func attachmentClaim(att domain.HostAttachment) host.HostAttachmentClaim {
	claim := host.HostAttachmentClaim{
		Attachment: host.Attachment{
			IntegrationInstanceID: att.IntegrationInstance,
			PaneID:                att.Container,
			HostContainerID:       att.Epoch.Value,
		},
	}
	if att.Process.Present() {
		birth := host.ProcessBirthEvidence{
			PID:             att.Process.PID,
			StartTimeSource: herdr.StartTimeSourceProcfs,
		}
		if t, err := time.Parse(materialize.CaptureTimeLayout, att.Process.StartedAt); err == nil {
			birth.StartTime = t
		}
		claim.LastKnownProcessBirth = birth
	}
	return claim
}

// fingerprintFromAttachment rebuilds the continuity fingerprint from the
// stored attachment fields (notes/48 §1).
func fingerprintFromAttachment(att domain.HostAttachment) domain.Fingerprint {
	return domain.Fingerprint{
		IntegrationInstance: att.IntegrationInstance,
		Epoch:               att.Epoch,
		Container:           att.Container,
		Process:             att.Process,
	}
}

func renderSessionReconcile(streams *iostreams.Streams, mode string, result sessionReconcileResult) error {
	if mode == "json" {
		b, err := json.Marshal(newEnvelope("session.reconcile", result))
		if err != nil {
			return duoerr.New("internal.session_reconcile_encode_failed", err.Error())
		}
		_, err = fmt.Fprintln(streams.Out, string(b))
		return err
	}
	if len(result.Items) == 0 {
		_, err := fmt.Fprintln(streams.Out, "no recovering sessions")
		return err
	}
	for _, it := range result.Items {
		line := fmt.Sprintf("%s %s -> %s", it.SessionID, it.InstanceID, it.Outcome)
		if it.Reason != "" {
			line += " (" + it.Reason + ")"
		}
		if _, err := fmt.Fprintln(streams.Out, line); err != nil {
			return err
		}
	}
	return nil
}
