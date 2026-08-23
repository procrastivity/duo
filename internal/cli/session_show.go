package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

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
type sessionInspectResult struct {
	SessionID         string `json:"session_id"`
	WorkspaceID       string `json:"workspace_id"`
	Lifecycle         string `json:"lifecycle"`
	View              string `json:"view"`
	RuntimeInstanceID string `json:"runtime_instance_id,omitempty"`
	InstanceState     string `json:"runtime_instance_state,omitempty"`
	Quarantined       bool   `json:"quarantined,omitempty"`
	Owner             string `json:"owner,omitempty"`
	Creator           string `json:"creator,omitempty"`
}

// sessionShowCommand constructs `duo session show`: internal/registry's
// "session.inspect" operation, CLI path {"session", "show"}.
func sessionShowCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "show one duo session's identity, lifecycle, and derived view",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}

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
	return result
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
	_, err := fmt.Fprintf(streams.Out,
		"session:            %s\nworkspace:          %s\nlifecycle:          %s\nview:               %s\nruntime instance:   %s\ninstance state:     %s\nowner:              %s\nquarantined:        %t\n",
		r.SessionID, r.WorkspaceID, r.Lifecycle, r.View, instance, instanceState, r.Owner, r.Quarantined,
	)
	return err
}
