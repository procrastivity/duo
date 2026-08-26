package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/surface"
)

// sessionInspectResult is session.inspect's result. Condition and operations
// are populated from the session's correlated agent runtime when available
// (delegation-loop step 09). Attachments stay the locked multi-leaf show
// projection (notes/48 §1): an array, never a singular "attachment" field.
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
	Condition         *conditionViewData         `json:"condition,omitempty"`
	Operations        []operationSupportView     `json:"operations,omitempty"`
	Attachments       []sessionAttachmentInspect `json:"attachments,omitempty"`
}

// conditionViewData is $defs.condition_view_data plus the inspect extras the
// schema permits as additional properties (runtime_instance_id, times,
// determining_observations, reasons). Revision is omitted: this milestone
// has no condition-view revision store.
type conditionViewData struct {
	Value                   string   `json:"value"`
	Confidence              string   `json:"confidence,omitempty"`
	Freshness               string   `json:"freshness,omitempty"`
	RuntimeInstanceID       string   `json:"runtime_instance_id,omitempty"`
	EffectiveAt             string   `json:"effective_at,omitempty"`
	ComputedAt              string   `json:"computed_at,omitempty"`
	DeterminingObservations []string `json:"determining_observations,omitempty"`
	Reasons                 []string `json:"reasons,omitempty"`
}

// operationSupportView is one $defs.support_view row plus the operation name
// the fixture carries. Only prompt.deliver and conversation.list are listed;
// terminal.read, leases, and streams are never fabricated.
type operationSupportView struct {
	Operation    string `json:"operation"`
	Availability string `json:"availability"`
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

// sessionShowCommand constructs `duo session show`: the registry operation
// whose CLI path is session/show.
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
			result := inspectResultFor(cmd.Context(), a, s)

			if mode == "json" {
				b, err := json.Marshal(newEnvelope(registeredOpByCLI("session", "show"), result))
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

func inspectResultFor(ctx context.Context, a *domain.Authority, s domain.Session) sessionInspectResult {
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
	var inst domain.RuntimeInstance
	var haveInst bool
	if s.Current != "" {
		result.RuntimeInstanceID = string(s.Current)
		if i, ok := a.Instance(s.Current); ok {
			inst = i
			haveInst = true
			result.InstanceState = string(inst.State)
		}
	}
	for _, att := range a.Attachments(s.ID) {
		result.Attachments = append(result.Attachments, attachmentInspectFor(a, s.ID, att))
	}
	result.Condition = conditionForInspect(ctx, a, s, haveInst, inst)
	result.Operations = operationsForInspect(a, s, haveInst, inst)
	return result
}

// conditionForInspect chooses the session.inspect condition view.
//
// Completion (I-5): step 08 adapters never emit exited or done. When the
// store instance is terminal (InstanceExited / Terminal()), condition is
// exited and final — inspect does not dial HostLifecycleSource (I-3: read
// path, no streams). I-8: while starting (or otherwise not live), omit
// condition even if agentBindingsFor succeeds — identity may be named while
// launch_pending keeps the instance starting; do not invent a live
// condition. Only live instances SnapshotCondition on a ConditionProvider.
func conditionForInspect(ctx context.Context, a *domain.Authority, s domain.Session, haveInst bool, inst domain.RuntimeInstance) *conditionViewData {
	if !haveInst {
		return nil
	}
	if inst.State.Terminal() {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		return &conditionViewData{
			Value:             string(runtime.ConditionExited),
			Confidence:        string(runtime.ConditionConfidenceReported),
			Freshness:         string(runtime.ConditionFreshnessFresh),
			RuntimeInstanceID: string(inst.ID),
			ComputedAt:        now,
			Reasons:           []string{"runtime instance state is exited"},
		}
	}
	if inst.State != domain.InstanceLive {
		return nil
	}
	bindings, ok := agentBindingsFor(a, s)
	if !ok {
		return nil
	}
	rt, err := openAgentRuntime(bindings.IntegrationInstance)
	if err != nil {
		return nil
	}
	provider, ok := rt.(runtime.ConditionProvider)
	if !ok {
		return nil
	}
	obs, err := runtime.SnapshotCondition(ctx, provider, conditionObservationRequest(bindings))
	if err != nil {
		return nil
	}
	return conditionViewFromObservation(obs, string(inst.ID))
}

func conditionViewFromObservation(obs runtime.ConditionObservation, runtimeInstanceID string) *conditionViewData {
	view := &conditionViewData{
		Value:             string(obs.Value),
		Confidence:        string(obs.Confidence),
		Freshness:         string(obs.Freshness),
		RuntimeInstanceID: runtimeInstanceID,
		Reasons:           append([]string(nil), obs.Reasons...),
	}
	if !obs.EffectiveAt.IsZero() {
		view.EffectiveAt = obs.EffectiveAt.UTC().Format(time.RFC3339Nano)
	}
	if !obs.ComputedAt.IsZero() {
		view.ComputedAt = obs.ComputedAt.UTC().Format(time.RFC3339Nano)
	}
	if obs.ObservationID != "" {
		view.DeterminingObservations = []string{obs.ObservationID}
	}
	return view
}

// operationsForInspect lists the prompt-send and conversation-list support
// rows only. Availability is available when the runtime implements the
// provider and the instance is live; temporarily_unavailable when it
// implements but is not live; unsupported when it does not implement.
func operationsForInspect(a *domain.Authority, s domain.Session, haveInst bool, inst domain.RuntimeInstance) []operationSupportView {
	live := haveInst && inst.State == domain.InstanceLive
	var (
		hasConversation bool
		hasPrompt       bool
	)
	if bindings, ok := agentBindingsFor(a, s); ok {
		if rt, err := openAgentRuntime(bindings.IntegrationInstance); err == nil {
			_, hasConversation = rt.(runtime.ConversationProvider)
			_, hasPrompt = rt.(runtime.RuntimePromptProvider)
		}
	}
	return []operationSupportView{
		{Operation: registeredOpByCLI("prompt", "send"), Availability: supportAvailability(hasPrompt, live)},
		{Operation: registeredOpByCLI("conversation", "list"), Availability: supportAvailability(hasConversation, live)},
	}
}

func supportAvailability(implements, live bool) string {
	if !implements {
		return "unsupported"
	}
	if live {
		return "available"
	}
	return "temporarily_unavailable"
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
	if r.Condition != nil {
		if _, err := fmt.Fprintf(streams.Out, "condition:          %s\n", r.Condition.Value); err != nil {
			return err
		}
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
