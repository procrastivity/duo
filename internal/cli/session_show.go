package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// sessionInspectResult is session.inspect's result. Only the fields the
// domain kernel can answer today are populated: "condition" and
// "operations" (duo-external-v1's condition_view_data / support_view) are
// presentation-service usage and operation-availability projections
// (duo-vnext-projection-contracts.md) that no component built through Step
// 21's dependencies (14, 15) produces evidence for, so this result omits
// them rather than fabricate a value. Both are optional in the schema's
// session.inspect conditional (no "required" list under either $ref).
//
// Attachments is the locked multi-leaf show projection (notes/48 §1): an
// array, never a singular "attachment" field.
type sessionInspectResult struct {
	SessionID         string                     `json:"session_id"`
	WorkspaceID       string                     `json:"workspace_id"`
	Lifecycle         string                     `json:"lifecycle"`
	View              string                     `json:"view"`
	RuntimeInstanceID string                     `json:"runtime_instance_id,omitempty"`
	InstanceState     string                     `json:"runtime_instance_state,omitempty"`
	Quarantined       bool                       `json:"quarantined,omitempty"`
	Owner             string                     `json:"owner,omitempty"`
	Creator           string                     `json:"creator,omitempty"`
	Attachments       []sessionAttachmentInspect `json:"attachments,omitempty"`
}

// sessionAttachmentInspect is one element of session.inspect's attachments
// array. Shape is locked by notes/48 §1.
type sessionAttachmentInspect struct {
	AttachmentID        string                         `json:"attachment_id"`
	State               string                         `json:"state"`
	IntegrationInstance string                         `json:"integration_instance"`
	Epoch               sessionAttachmentEpochInspect  `json:"epoch"`
	Container           string                         `json:"container"`
	ProcessBirth        *sessionAttachmentProcessBirth `json:"process_birth,omitempty"`
	ClaimHeld           bool                           `json:"claim_held"`
	ReattachCommand     string                         `json:"reattach_command,omitempty"`
}

type sessionAttachmentEpochInspect struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Scope string `json:"scope"`
}

// sessionAttachmentProcessBirth is the show-facing process birth: PID and
// start time only. Host/executable are never projected from launch evidence.
type sessionAttachmentProcessBirth struct {
	PID       int    `json:"pid"`
	StartedAt string `json:"started_at"`
}

// sessionShowCommand constructs `duo session show`: internal/registry's
// "session.inspect" operation, CLI path {"session", "show"}.
func sessionShowCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "show one duo session's identity, lifecycle, and derived view",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output

			a, closer, err := openReadAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()

			id := domain.SessionID(args[0])
			s, ok := a.Session(id)
			if !ok {
				return duoerr.New("object.not_found", fmt.Sprintf("No session named %q is known.", args[0]))
			}
			result := inspectResultFor(a, s)

			if mode == "json" {
				b, err := json.Marshal(newEnvelope("session.inspect", result))
				if err != nil {
					return duoerr.New("internal.session_show_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			return renderSessionShowText(streams, result)
		},
	}
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func inspectResultFor(a *domain.Authority, s domain.Session) sessionInspectResult {
	view, _ := a.View(s.ID)
	result := sessionInspectResult{
		SessionID:   string(s.ID),
		WorkspaceID: string(s.Workspace),
		Lifecycle:   lifecycle(s.State),
		View:        string(view),
		Quarantined: s.Quarantined,
		Owner:       s.Owner,
		Creator:     s.Creator,
	}
	if s.Current != "" {
		result.RuntimeInstanceID = string(s.Current)
		if inst, ok := a.Instance(s.Current); ok {
			result.InstanceState = string(inst.State)
		}
	}
	for _, att := range a.Attachments(s.ID) {
		result.Attachments = append(result.Attachments, attachmentInspectFor(a, s.ID, att))
	}
	return result
}

// attachmentInspectFor rebuilds the attachment fingerprint and projects
// one show record. reattach_command is present only when this session
// holds the rebuilt claim (notes/48 §1, §6).
func attachmentInspectFor(a *domain.Authority, session domain.SessionID, att domain.HostAttachment) sessionAttachmentInspect {
	fp := domain.Fingerprint{
		IntegrationInstance: att.IntegrationInstance,
		Epoch:               att.Epoch,
		Container:           att.Container,
		Process:             att.Process,
	}
	claim, held := a.ActiveClaim(fp.ClaimRef())
	claimHeld := held && claim.Session == session

	item := sessionAttachmentInspect{
		AttachmentID:        string(att.ID),
		State:               string(att.State),
		IntegrationInstance: att.IntegrationInstance,
		Epoch: sessionAttachmentEpochInspect{
			Kind:  att.Epoch.Kind,
			Value: att.Epoch.Value,
			Scope: string(att.Epoch.Scope),
		},
		Container: att.Container,
		ClaimHeld: claimHeld,
	}
	if att.Process.Present() {
		item.ProcessBirth = &sessionAttachmentProcessBirth{
			PID:       att.Process.PID,
			StartedAt: att.Process.StartedAt,
		}
	}
	if claimHeld {
		item.ReattachCommand = reattachCommand(string(session), fp)
	}
	return item
}

func renderSessionShowText(streams *iostreams.Streams, r sessionInspectResult) error {
	instance := r.RuntimeInstanceID
	if instance == "" {
		instance = "-"
	}
	instanceState := r.InstanceState
	if instanceState == "" {
		instanceState = "-"
	}
	if _, err := fmt.Fprintf(streams.Out,
		"session:            %s\nworkspace:          %s\nlifecycle:          %s\nview:               %s\nruntime instance:   %s\ninstance state:     %s\nowner:              %s\nquarantined:        %t\n",
		r.SessionID, r.WorkspaceID, r.Lifecycle, r.View, instance, instanceState, r.Owner, r.Quarantined,
	); err != nil {
		return err
	}
	for _, att := range r.Attachments {
		if err := renderAttachmentShowText(streams, att); err != nil {
			return err
		}
	}
	return nil
}

func renderAttachmentShowText(streams *iostreams.Streams, att sessionAttachmentInspect) error {
	if _, err := fmt.Fprintf(streams.Out,
		"\nattachment:         %s\nstate:              %s\nintegration:        %s\nepoch:              %s %s (%s)\ncontainer:          %s\n",
		att.AttachmentID, att.State, att.IntegrationInstance,
		att.Epoch.Kind, att.Epoch.Value, att.Epoch.Scope, att.Container,
	); err != nil {
		return err
	}
	if att.ProcessBirth != nil {
		if _, err := fmt.Fprintf(streams.Out,
			"process birth:      pid %d started %s\n",
			att.ProcessBirth.PID, att.ProcessBirth.StartedAt,
		); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(streams.Out, "claim held:         %t\n", att.ClaimHeld); err != nil {
		return err
	}
	if att.ReattachCommand != "" {
		// One physical line: the pasteable command itself.
		_, err := fmt.Fprintf(streams.Out, "reattach with: %s\n", att.ReattachCommand)
		return err
	}
	_, err := fmt.Fprintf(streams.Out, "reattach with: omitted — %s\n", reattachOmitReason(att))
	return err
}

// reattachOmitReason explains why show withheld a pasteable command
// (notes/48 §6). Never invent a command that will not work.
func reattachOmitReason(att sessionAttachmentInspect) string {
	if att.ProcessBirth == nil {
		return "process birth unknown; cannot be paired to this attachment"
	}
	return "rebuilt claim is not held by this session"
}
