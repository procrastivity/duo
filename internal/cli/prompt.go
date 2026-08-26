package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/delivery"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
	"github.com/procrastivity/duo/internal/surface"
)

// promptDeliverResult is prompt.deliver's success result. Shape follows
// fixtures/duo-external-v1/prompt-queued.json and prompt-delivered.json.
type promptDeliverResult struct {
	CommandID           string            `json:"command_id"`
	Revision            string            `json:"revision"`
	Target              promptTarget      `json:"target"`
	ResponsibilityState string            `json:"responsibility_state"`
	QueuePolicy         string            `json:"queue_policy"`
	ExpiresAt           string            `json:"expires_at"`
	Hold                *promptHoldView   `json:"hold,omitempty"`
	ActivityObserved    bool              `json:"activity_observed"`
	Acknowledged        bool              `json:"acknowledged"`
	Retry               promptRetryAdvice `json:"retry"`
}

type promptTarget struct {
	SessionID         string `json:"session_id"`
	RuntimeInstanceID string `json:"runtime_instance_id"`
}

type promptHoldView struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type promptRetryAdvice struct {
	Safe   bool   `json:"safe"`
	Action string `json:"action"`
}

// commandInspectResult is command.inspect's result (fixture command-inspect.json).
type commandInspectResult struct {
	CommandID           string                `json:"command_id"`
	Revision            string                `json:"revision"`
	Operation           string                `json:"operation"`
	Target              promptTarget          `json:"target"`
	ResponsibilityState string                `json:"responsibility_state"`
	QueuePolicy         string                `json:"queue_policy"`
	ExpiresAt           string                `json:"expires_at"`
	Attempts            []commandAttemptView  `json:"attempts"`
	Milestones          commandMilestonesView `json:"milestones"`
	Retry               promptRetryAdvice     `json:"retry"`
}

type commandAttemptView struct {
	AttemptID      string `json:"attempt_id"`
	Realization    string `json:"realization"`
	StartedAt      string `json:"started_at"`
	RecordedResult string `json:"recorded_result"`
}

type commandMilestonesView struct {
	AcceptedAt       string `json:"accepted_at,omitempty"`
	DeliveredAt      string `json:"delivered_at,omitempty"`
	ActivityObserved bool   `json:"activity_observed"`
	Acknowledged     bool   `json:"acknowledged"`
}

// failureWrittenError means the verb already wrote a complete duo.external/v1
// failure envelope to stderr under --output json; Execute must not re-render.
type failureWrittenError struct {
	code string
}

func (e *failureWrittenError) Error() string { return e.code }

// promptCommand builds the `duo prompt` parent verb.
func promptCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prompt",
		Short: "deliver and inspect prompt commands",
	}
	cmd.AddCommand(promptSendCommand(streams))
	cmd.AddCommand(promptShowCommand(streams))
	return cmd
}

// promptSendCommand constructs `duo prompt send`: registry prompt.deliver,
// CLI path {"prompt","send"}.
func promptSendCommand(streams *iostreams.Streams) *cobra.Command {
	var (
		text            string
		idempotencyKey  string
		expiresAtRaw    string
		runtimeInstance string
		actor           string
	)
	cmd := &cobra.Command{
		Use:   "send <session-id>",
		Short: "accept and release one prompt.deliver command (queue_until_safe)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output
			op := registeredOpByCLI("prompt", "send")

			if text == "" {
				return duoerr.New("invalid.request", "Prompt text is required (--text).")
			}
			if idempotencyKey == "" {
				return duoerr.New("invalid.request", "--idempotency-key is required.")
			}

			var expiresAt time.Time
			if expiresAtRaw != "" {
				t, err := time.Parse(time.RFC3339Nano, expiresAtRaw)
				if err != nil {
					t, err = time.Parse(time.RFC3339, expiresAtRaw)
				}
				if err != nil {
					return duoerr.New("invalid.request",
						fmt.Sprintf("--expires-at must be RFC3339, not %q.", expiresAtRaw))
				}
				expiresAt = t.UTC()
			}

			a, store, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()

			sessionID := domain.SessionID(args[0])
			if _, ok := a.Session(sessionID); !ok {
				return duoerr.New("object.not_found", fmt.Sprintf("No session named %q is known.", args[0]))
			}

			digest := promptCanonicalDigest(text)
			accepted, err := a.AcceptPrompt(cmd.Context(), domain.AcceptPromptRequest{
				Session:         sessionID,
				Instance:        domain.InstanceID(runtimeInstance),
				Actor:           actor,
				IdempotencyKey:  idempotencyKey,
				CanonicalDigest: digest,
				ExpiresAt:       expiresAt,
				QueuePolicy:     domain.QueueUntilSafe,
			})
			if err != nil {
				return mapPromptAcceptError(streams, mode, op, err)
			}

			var (
				cmdState domain.PromptCommand
				held     bool
				hold     delivery.Hold
			)
			if accepted.Command.State.Terminal() {
				cmdState = accepted.Command
			} else {
				if err := waitPromptReady(cmd.Context(), streams, mode, op, a, accepted.Command, actor); err != nil {
					return err
				}
				rt, hostAd, openErr := openPromptAdapters(a, accepted.Command.Session)
				if openErr != nil {
					return openErr
				}
				composer := &delivery.Composer{
					Authority: a,
					Runtime:   rt,
					Host:      hostAd,
				}
				res, relErr := composer.Release(cmd.Context(), delivery.Request{
					Command: accepted.Command.ID,
					Text:    text,
					Actor:   actor,
				})
				if relErr != nil {
					return mapPromptReleaseError(streams, mode, op, res.Command, relErr)
				}
				cmdState = res.Command
				held = res.Held
				hold = res.Hold
			}

			result := promptDeliverResultFrom(cmdState, held, hold)
			if mode == cliflags.OutputJSON {
				b, err := json.Marshal(newEnvelope(op, result))
				if err != nil {
					return duoerr.New("internal.prompt_deliver_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			return renderPromptDeliverText(streams, result)
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "prompt text to deliver")
	cmd.Flags().StringVar(&idempotencyKey, "idempotency-key", "", "caller idempotency key (required)")
	cmd.Flags().StringVar(&expiresAtRaw, "expires-at", "",
		fmt.Sprintf("RFC3339 expiry (default: %s from now)", domain.DefaultPromptExpiry))
	cmd.Flags().StringVar(&runtimeInstance, "runtime-instance", "",
		"expected runtime instance ID (defaults to the session's current instance)")
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on every fact")
	_ = cmd.MarkFlagRequired("text")
	_ = cmd.MarkFlagRequired("idempotency-key")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// promptShowCommand constructs `duo prompt show`: registry command.inspect,
// CLI path {"prompt","show"} (projection-cases / table.go; not duo command).
func promptShowCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <command-id>",
		Short: "inspect one prompt command's responsibility state and attempts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output
			op := registeredOpByCLI("prompt", "show")

			a, closer, err := openReadAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()

			id := domain.CommandID(args[0])
			c, ok := a.Command(id)
			if !ok {
				return duoerr.New("object.not_found", fmt.Sprintf("No command named %q is known.", args[0]))
			}
			result := commandInspectResultFrom(c)
			if mode == cliflags.OutputJSON {
				b, err := json.Marshal(newEnvelope(op, result))
				if err != nil {
					return duoerr.New("internal.command_inspect_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			return renderCommandInspectText(streams, result)
		},
	}
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// promptCanonicalDigest is the CLI idempotency digest: SHA-256 of the
// prompt text alone, spelled sha256:<hex>. Same text → same digest →
// AcceptPrompt replay; a changed text with the same key conflicts.
func promptCanonicalDigest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// promptStampLayout matches domain/store timestamps so command ExpiresAt
// and instance StartedAt parse. Copied: internal/domain does not export it.
const promptStampLayout = "2006-01-02T15:04:05.000Z"

// waitPromptReady is the implicit send wait (D4): bindStartingIdentity
// until live, using the command's expires_at as the only deadline, then
// — for Duo-created sessions — the existing delivery launch-settle
// window so Release can auto-deliver instead of returning a queued hold.
// No duo session settle verb.
func waitPromptReady(
	ctx context.Context,
	streams *iostreams.Streams,
	mode, op string,
	a *domain.Authority,
	cmd domain.PromptCommand,
	actor string,
) error {
	sess, ok := a.Session(cmd.Session)
	if !ok {
		return duoerr.New("object.not_found", fmt.Sprintf("No session named %q is known.", cmd.Session))
	}
	deadline := promptWaitDeadline(ctx, cmd)

	hostAd, err := openHostPromptProvider(a, sess)
	if err != nil {
		return err
	}
	out := waitPromptIdentity(ctx, streams, a, hostAd, sess, actor, deadline)
	if !out.Live {
		return expireUnboundPrompt(ctx, streams, mode, op, a, cmd, actor, deadline)
	}

	if !delivery.DuoCreated(a, sess.ID) {
		return nil
	}
	inst, ok := a.Instance(sess.Current)
	if !ok {
		return expireUnboundPrompt(ctx, streams, mode, op, a, cmd, actor, deadline)
	}
	if promptLaunchSettled(inst, time.Now()) {
		return nil
	}
	if waitPromptLaunchSettled(ctx, a, sess.Current, deadline) {
		return nil
	}
	return expireUnboundPrompt(ctx, streams, mode, op, a, cmd, actor, deadline)
}

func promptWaitDeadline(ctx context.Context, cmd domain.PromptCommand) time.Time {
	deadline := time.Now().Add(domain.DefaultPromptExpiry)
	if t, ok := parsePromptStamp(cmd.ExpiresAt); ok {
		deadline = t
	}
	if ctx != nil {
		if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
			return d
		}
	}
	return deadline
}

func parsePromptStamp(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{promptStampLayout, time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func promptLaunchSettled(inst domain.RuntimeInstance, now time.Time) bool {
	started, ok := parsePromptStamp(inst.StartedAt)
	if !ok {
		return false
	}
	return !now.UTC().Before(started.Add(delivery.DefaultLaunchSettleTimeout))
}

func waitPromptLaunchSettled(ctx context.Context, a *domain.Authority, instance domain.InstanceID, deadline time.Time) bool {
	poll := identityBindPoll
	if poll <= 0 {
		poll = defaultIdentityBindPoll
	}
	for {
		inst, ok := a.Instance(instance)
		if ok && promptLaunchSettled(inst, time.Now()) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(poll):
		}
	}
}

func expireUnboundPrompt(
	ctx context.Context,
	streams *iostreams.Streams,
	mode, op string,
	a *domain.Authority,
	cmd domain.PromptCommand,
	actor string,
	deadline time.Time,
) error {
	if remain := time.Until(deadline); remain > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(remain + time.Millisecond):
		}
	}
	expired, err := a.ExpireIfDue(ctx, cmd.ID, actor)
	if err != nil {
		return err
	}
	latest, _ := a.Command(cmd.ID)
	if latest.ID == "" {
		latest = cmd
	}
	if expired || latest.State == domain.ResponsibilityExpired {
		return mapPromptReleaseError(streams, mode, op, latest, domain.ErrCommandExpired)
	}
	return duoerr.New("operation.temporarily_unavailable",
		fmt.Sprintf("session %s is still starting; no live agent-session bind before expires_at.", cmd.Session))
}

func openPromptAdapters(a *domain.Authority, session domain.SessionID) (any, host.HostPromptProvider, error) {
	s, ok := a.Session(session)
	if !ok {
		return nil, nil, duoerr.New("object.not_found", fmt.Sprintf("No session named %q is known.", session))
	}
	var rt any
	if bindings, ok := agentBindingsFor(a, s); ok {
		opened, err := openAgentRuntime(bindings.IntegrationInstance)
		if err != nil {
			return nil, nil, duoerr.New("operation.temporarily_unavailable",
				fmt.Sprintf("No agent-runtime adapter is available for %q.", bindings.IntegrationInstance))
		}
		rt = opened
	}
	hostAd, err := openHostPromptProvider(a, s)
	if err != nil {
		return nil, nil, err
	}
	return rt, hostAd, nil
}

func openHostPromptProvider(a *domain.Authority, s domain.Session) (host.HostPromptProvider, error) {
	if s.Attachment == "" {
		return nil, nil
	}
	att, ok := a.Attachment(s.Attachment)
	if !ok {
		return nil, nil
	}
	return openHostPromptProviderFor(att.IntegrationInstance)
}

func promptDeliverResultFrom(cmd domain.PromptCommand, held bool, hold delivery.Hold) promptDeliverResult {
	out := promptDeliverResult{
		CommandID: string(cmd.ID),
		Revision:  strconv.Itoa(cmd.Revision),
		Target: promptTarget{
			SessionID:         string(cmd.Session),
			RuntimeInstanceID: string(cmd.Instance),
		},
		ResponsibilityState: string(cmd.State),
		QueuePolicy:         string(cmd.QueuePolicy),
		ExpiresAt:           cmd.ExpiresAt,
		ActivityObserved:    false,
		Acknowledged:        false,
		Retry: promptRetryAdvice{
			Safe:   false,
			Action: "observe_existing_command",
		},
	}
	if held {
		out.Hold = &promptHoldView{
			Code:    hold.Code,
			Message: hold.Message,
		}
		if out.Hold.Code == "" {
			out.Hold.Code = delivery.HoldCode
		}
		if out.Hold.Message == "" {
			out.Hold.Message = "Attributed human input has priority."
		}
	}
	return out
}

func commandInspectResultFrom(cmd domain.PromptCommand) commandInspectResult {
	attempts := make([]commandAttemptView, 0, len(cmd.Attempts))
	for _, at := range cmd.Attempts {
		attempts = append(attempts, commandAttemptView{
			AttemptID:      string(at.ID),
			Realization:    "native", // Stage-1 adapters only offer native
			StartedAt:      at.StartedAt,
			RecordedResult: at.RecordedResult,
		})
	}
	milestones := commandMilestonesView{
		AcceptedAt:       cmd.AcceptedAt,
		ActivityObserved: false,
		Acknowledged:     false,
	}
	if cmd.State == domain.ResponsibilityDelivered {
		milestones.DeliveredAt = cmd.TerminalAt
	}
	return commandInspectResult{
		CommandID: string(cmd.ID),
		Revision:  strconv.Itoa(cmd.Revision),
		Operation: cmd.Operation,
		Target: promptTarget{
			SessionID:         string(cmd.Session),
			RuntimeInstanceID: string(cmd.Instance),
		},
		ResponsibilityState: string(cmd.State),
		QueuePolicy:         string(cmd.QueuePolicy),
		ExpiresAt:           cmd.ExpiresAt,
		Attempts:            attempts,
		Milestones:          milestones,
		Retry: promptRetryAdvice{
			Safe:   false,
			Action: "observe_existing_command",
		},
	}
}

func renderPromptDeliverText(streams *iostreams.Streams, r promptDeliverResult) error {
	if _, err := fmt.Fprintf(streams.Out, "command:  %s\n", r.CommandID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(streams.Out, "state:    %s\n", r.ResponsibilityState); err != nil {
		return err
	}
	if r.Hold != nil {
		if _, err := fmt.Fprintf(streams.Out, "hold:     %s\n", r.Hold.Code); err != nil {
			return err
		}
	}
	_, err := fmt.Fprintf(streams.Out, "inspect:  duo prompt show %s\n", r.CommandID)
	return err
}

func renderCommandInspectText(streams *iostreams.Streams, r commandInspectResult) error {
	if _, err := fmt.Fprintf(streams.Out, "command:  %s\n", r.CommandID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(streams.Out, "state:    %s\n", r.ResponsibilityState); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(streams.Out, "attempts: %d\n", len(r.Attempts)); err != nil {
		return err
	}
	return nil
}

func mapPromptAcceptError(streams *iostreams.Streams, mode, op string, err error) error {
	var conflict *domain.IdempotencyConflictError
	if errors.As(err, &conflict) {
		details := map[string]any{
			"idempotency_key": conflict.Key,
			"existing_digest": conflict.ExistingDigest,
			"request_digest":  conflict.RequestDigest,
		}
		return writePromptFailure(streams, mode, op, promptFailure{
			Code:    "command.idempotency_conflict",
			Message: "The idempotency key already binds a different canonical request.",
			Target:  map[string]string{"kind": "prompt_command", "id": string(conflict.ExistingCommand)},
			Retry:   promptRetryAdvice{Safe: false, Action: "use_new_idempotency_key"},
			Effect:  "no_effect",
			Details: details,
		})
	}
	switch {
	case errors.Is(err, domain.ErrIdempotencyKeyRequired),
		errors.Is(err, domain.ErrCanonicalDigestRequired),
		errors.Is(err, domain.ErrExpiryInPast),
		errors.Is(err, domain.ErrQueuePolicyUnsupported):
		return duoerr.New("invalid.request", err.Error())
	case errors.Is(err, domain.ErrUnknownObject), errors.Is(err, domain.ErrNoCurrentInstance):
		return duoerr.New("object.not_found", err.Error())
	case errors.Is(err, domain.ErrInstanceExited):
		return duoerr.New("session.target_exited", err.Error())
	default:
		return duoerrFromDomain(err)
	}
}

func mapPromptReleaseError(streams *iostreams.Streams, mode, op string, cmd domain.PromptCommand, err error) error {
	if errors.Is(err, domain.ErrCommandExpired) {
		details := map[string]any{
			"command_id":           string(cmd.ID),
			"responsibility_state": string(cmd.State),
			"expires_at":           cmd.ExpiresAt,
			"queue_policy":         string(cmd.QueuePolicy),
		}
		return writePromptFailure(streams, mode, op, promptFailure{
			Code:    "command.expired",
			Message: "The queued command passed its expires_at without an attempt.",
			Target:  map[string]string{"kind": "prompt_command", "id": string(cmd.ID)},
			Retry:   promptRetryAdvice{Safe: false, Action: "submit_new_command"},
			Effect:  "no_effect",
			Details: details,
		})
	}
	return duoerr.New("internal.prompt_deliver_failed", err.Error())
}

type promptFailure struct {
	Code    string
	Message string
	Target  map[string]string
	Retry   promptRetryAdvice
	Effect  string
	Details any
}

func writePromptFailure(streams *iostreams.Streams, mode, op string, f promptFailure) error {
	if mode != cliflags.OutputJSON {
		return duoerr.New(f.Code, f.Message).WithDetails(f.Details)
	}
	class := "internal"
	if c, ok := registry.StableErrorCodes()[f.Code]; ok {
		class = string(c)
	}
	envelope := struct {
		Schema    string `json:"schema"`
		RequestID string `json:"request_id"`
		Operation string `json:"operation"`
		Error     struct {
			Class   string            `json:"class"`
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Target  map[string]string `json:"target,omitempty"`
			Retry   promptRetryAdvice `json:"retry"`
			Effect  string            `json:"effect"`
			Details any               `json:"details,omitempty"`
		} `json:"error"`
	}{
		Schema:    "duo.external/v1",
		RequestID: requestID(),
		Operation: op,
	}
	envelope.Error.Class = class
	envelope.Error.Code = f.Code
	envelope.Error.Message = f.Message
	envelope.Error.Target = f.Target
	envelope.Error.Retry = f.Retry
	envelope.Error.Effect = f.Effect
	envelope.Error.Details = f.Details
	b, err := json.Marshal(envelope)
	if err != nil {
		return duoerr.New("internal.prompt_deliver_encode_failed", err.Error())
	}
	if _, err := fmt.Fprintln(streams.Err, string(b)); err != nil {
		return err
	}
	return &failureWrittenError{code: f.Code}
}
