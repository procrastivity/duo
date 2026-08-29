// Package delivery is the prompt.deliver composer: arbitration (human
// priority, quiet-gate, the D3 Herdr auto-release carve-out), path
// selection via internal/promptpath, then the step-10 kernel
// (CreateAttempt + terminal commit / reconcile) around one adapter
// DeliverPrompt call.
//
// It is not CLI (step 14). Adapters never import this package.
package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/promptpath"
	"github.com/procrastivity/duo/internal/runtime"
)

// HoldCode is the queued-hold code under queue_until_safe. Registry class
// conflict; fixture contracts/fixtures/duo-external-v1/prompt-queued.json.
const HoldCode = "prompt.human_priority_hold"

// DefaultQuietPeriod is decision-03 §5.3 step 4: 30 seconds after the
// latest attributed human input. It cannot fire on Herdr: that host has
// no attributed-input surface (notes/19, TestNoWriterPresenceSurface).
const DefaultQuietPeriod = 30 * time.Second

// DefaultLaunchSettleTimeout is Herdr Config.LaunchSettleTimeout's
// default (10s), reused as the D3 carve-out settle window. Copied rather
// than imported so this composer does not depend on the Herdr adapter.
const DefaultLaunchSettleTimeout = 10 * time.Second

// timestampLayout matches the domain/store stamp so instance.StartedAt
// parses. Duplicated: internal/domain does not export the constant.
const timestampLayout = "2006-01-02T15:04:05.000Z"

// Evidence is the arbitration evidence the caller already has. Herdr
// cannot fill Draft, LastHumanInput, or HumanAttached (notes/19: writer
// presence refuted; no composer lease). Leave them zero and the
// composer will not invent keystroke attribution. Duo-created is not
// here: it is read from the launch-plan attachment stamp.
type Evidence struct {
	// Draft is positive human-draft evidence. When true, the command
	// holds even on a composer-safe runtime path (decision-03 §5.4).
	Draft bool
	// LastHumanInput is the latest attributed human keystroke. Zero
	// means none: the quiet period does not run.
	LastHumanInput time.Time
	// HumanAttached is true only when the caller has proof a human
	// attached to the pane. Herdr has no such signal; production leaves
	// this false. True always holds.
	HumanAttached bool
}

// Hold is the queued-hold payload. The command stays nonterminal.
type Hold struct {
	Code    string
	Message string
	Reason  string
}

// Request is one attempt to release a previously accepted prompt command.
type Request struct {
	Command  domain.CommandID
	Text     string
	Actor    string
	Evidence Evidence
}

// Result is what Release returns. Held means arbitration refused an
// attempt; the command remains queued. Path is empty on hold.
type Result struct {
	Command domain.PromptCommand
	Held    bool
	Hold    Hold
	Path    promptpath.Offer
}

// Composer evaluates arbitration, selects a path, and invokes one
// delivery attempt. Authority is required. Runtime may be nil (host-only)
// or a ConditionProvider and/or RuntimePromptProvider. Host may be nil
// when a runtime path is guaranteed; Pi needs it.
type Composer struct {
	Authority *domain.Authority
	Runtime   any
	Host      host.HostPromptProvider

	QuietPeriod         time.Duration
	LaunchSettleTimeout time.Duration
	MinimumQuality      promptpath.Quality
	Now                 func() time.Time
}

func (c *Composer) now() time.Time {
	if c.Now != nil {
		return c.Now().UTC()
	}
	return time.Now().UTC()
}

func (c *Composer) quietPeriod() time.Duration {
	if c.QuietPeriod > 0 {
		return c.QuietPeriod
	}
	return DefaultQuietPeriod
}

func (c *Composer) settleTimeout() time.Duration {
	if c.LaunchSettleTimeout > 0 {
		return c.LaunchSettleTimeout
	}
	return DefaultLaunchSettleTimeout
}

func (c *Composer) minimumQuality() promptpath.Quality {
	if c.MinimumQuality != "" {
		return c.MinimumQuality
	}
	return promptpath.QualityExact
}

// Release revalidates the bound instance, applies human-priority /
// quiet-gate / ready-boundary arbitration, selects a path, and on
// auto-release invokes CreateAttempt + DeliverPrompt + terminal commit.
func (c *Composer) Release(ctx context.Context, req Request) (Result, error) {
	if c == nil || c.Authority == nil {
		return Result{}, fmt.Errorf("delivery: composer needs an authority")
	}
	actor := req.Actor
	if actor == "" {
		actor = "authority"
	}

	cmd, ok := c.Authority.Command(req.Command)
	if !ok {
		return Result{}, fmt.Errorf("%w: %s", domain.ErrUnknownCommand, req.Command)
	}
	if cmd.State.Terminal() {
		return Result{Command: cmd}, fmt.Errorf("%w: %s is %s", domain.ErrCommandTerminal, cmd.ID, cmd.State)
	}
	if cmd.State == domain.ResponsibilityAttempting {
		return Result{Command: cmd}, fmt.Errorf("%w: %s", domain.ErrCommandAttempting, cmd.ID)
	}

	expired, err := c.Authority.ExpireIfDue(ctx, cmd.ID, actor)
	if err != nil {
		return Result{}, err
	}
	if expired {
		cmd, _ = c.Authority.Command(cmd.ID)
		return Result{Command: cmd}, fmt.Errorf("%w: %s", domain.ErrCommandExpired, cmd.ID)
	}

	instance, ok := c.Authority.Instance(cmd.Instance)
	if !ok {
		return Result{}, fmt.Errorf("%w: instance %s", domain.ErrUnknownObject, cmd.Instance)
	}
	if instance.State.Terminal() {
		// I-5: CreateAttempt records the failure against the bound
		// instance; the command never rebinds to session.Current.
		_, err := c.Authority.CreateAttempt(ctx, cmd.ID, actor, domain.PromptPathHost)
		cmd, _ = c.Authority.Command(cmd.ID)
		return Result{Command: cmd}, err
	}

	if hold, ok := c.arbitrate(ctx, cmd, instance, req.Evidence); ok {
		return Result{Command: cmd, Held: true, Hold: hold}, nil
	}

	binding := runtimeBinding(c.Authority, cmd.Session)
	attachment := hostAttachment(c.Authority, cmd.Session)
	runtimeOffer, hostOffer := c.collectOffers(ctx, binding, attachment)
	selected, err := (promptpath.Selector{}).Select(runtimeOffer, hostOffer, c.minimumQuality())
	if err != nil {
		return Result{Command: cmd}, err
	}

	if req.Text == "" {
		return Result{Command: cmd, Path: selected}, fmt.Errorf("delivery: prompt text is required")
	}

	pathKind := domain.PromptPathKind(selected.Kind)
	attemptID, err := c.Authority.CreateAttempt(ctx, cmd.ID, actor, pathKind)
	if err != nil {
		cmd, _ = c.Authority.Command(cmd.ID)
		return Result{Command: cmd, Path: selected}, err
	}

	delivered, provedNoEffect, deliverErr := c.deliver(ctx, selected, binding, attachment, req.Text)
	if deliverErr != nil {
		// Adapter I/O failed after attempt create: cannot prove no
		// effect, so reconcile as unknown_effect (decision-03 §4.3).
		_ = c.Authority.ReconcileAttempt(ctx, cmd.ID, attemptID, actor, false)
		cmd, _ = c.Authority.Command(cmd.ID)
		return Result{Command: cmd, Path: selected}, deliverErr
	}
	if delivered {
		if err := c.Authority.CommitDelivered(ctx, cmd.ID, attemptID, actor); err != nil {
			cmd, _ = c.Authority.Command(cmd.ID)
			return Result{Command: cmd, Path: selected}, err
		}
		cmd, _ = c.Authority.Command(cmd.ID)
		return Result{Command: cmd, Path: selected}, nil
	}
	if err := c.Authority.ReconcileAttempt(ctx, cmd.ID, attemptID, actor, provedNoEffect); err != nil {
		cmd, _ = c.Authority.Command(cmd.ID)
		return Result{Command: cmd, Path: selected}, err
	}
	cmd, _ = c.Authority.Command(cmd.ID)
	return Result{Command: cmd, Path: selected}, nil
}

func (c *Composer) arbitrate(ctx context.Context, cmd domain.PromptCommand, instance domain.RuntimeInstance, ev Evidence) (Hold, bool) {
	now := c.now()

	if ev.Draft {
		return hold("draft evidence has priority; a native path cannot bypass it"), true
	}
	if !ev.LastHumanInput.IsZero() && now.Sub(ev.LastHumanInput.UTC()) < c.quietPeriod() {
		return hold("quiet period after attributed human input"), true
	}
	if ev.HumanAttached {
		return hold("a human is attached to the pane"), true
	}

	condition := c.snapshotCondition(ctx, cmd.Session)
	switch condition {
	case runtime.ConditionWorking, runtime.ConditionBlocked:
		return hold("agent condition " + string(condition) + " holds automation"), true
	case runtime.ConditionDone:
		return hold("done does not prove the composer is ready"), true
	case runtime.ConditionExited:
		return hold("condition exited; automatic release is not permitted"), true
	}

	duoCreated := DuoCreated(c.Authority, cmd.Session)
	settled := launchSettled(instance, now, c.settleTimeout())

	switch condition {
	case runtime.ConditionIdle:
		if duoCreated && settled {
			return Hold{}, false
		}
		if !duoCreated {
			return hold("pane origin is unknown; Herdr cannot prove it is unattached"), true
		}
		return hold("launch settle window has not elapsed"), true
	case runtime.ConditionUnknown, "":
		// unknown does not make prompt unsupported. It prevents
		// automatic release unless the D3 carve-out applies:
		// Duo-created, unattached, launch-settled.
		if duoCreated && settled {
			return Hold{}, false
		}
		return hold("condition unknown; automatic release requires the Duo-created launch-settled carve-out"), true
	default:
		return hold("condition " + string(condition) + " does not permit automatic release"), true
	}
}

func hold(reason string) Hold {
	return Hold{
		Code:    HoldCode,
		Message: "Attributed human input has priority.",
		Reason:  reason,
	}
}

func (c *Composer) snapshotCondition(ctx context.Context, session domain.SessionID) runtime.ConditionValue {
	provider, ok := c.Runtime.(runtime.ConditionProvider)
	if !ok {
		return runtime.ConditionUnknown
	}
	binding := runtimeBinding(c.Authority, session)
	obs, err := runtime.SnapshotCondition(ctx, provider, runtime.ConditionObservationRequest{
		ExternalAgentSessionID: binding.ExternalAgentSessionID,
		TranscriptID:           binding.TranscriptID,
	})
	if err != nil {
		return runtime.ConditionUnknown
	}
	return obs.Value
}

func (c *Composer) collectOffers(ctx context.Context, binding runtime.RuntimeBinding, attachment host.Attachment) (runtimeOffer, hostOffer *promptpath.Offer) {
	if rp, ok := c.Runtime.(runtime.RuntimePromptProvider); ok {
		if cand, err := rp.PromptPath(ctx, binding); err == nil {
			o := promptpath.Offer{
				Kind:        promptpath.KindRuntime,
				Quality:     promptpath.Quality(cand.Quality),
				Realization: promptpath.Realization(cand.Realization),
			}
			runtimeOffer = &o
		}
	}
	if c.Host != nil {
		if cand, err := c.Host.PromptPath(ctx, attachment); err == nil {
			o := promptpath.Offer{
				Kind:        promptpath.KindHost,
				Quality:     promptpath.Quality(cand.Quality),
				Realization: promptpath.Realization(cand.Realization),
			}
			hostOffer = &o
		}
	}
	return runtimeOffer, hostOffer
}

func (c *Composer) deliver(
	ctx context.Context,
	selected promptpath.Offer,
	binding runtime.RuntimeBinding,
	attachment host.Attachment,
	text string,
) (delivered, provedNoEffect bool, err error) {
	switch selected.Kind {
	case promptpath.KindRuntime:
		rp, ok := c.Runtime.(runtime.RuntimePromptProvider)
		if !ok {
			return false, false, fmt.Errorf("delivery: selected runtime path but runtime is not a RuntimePromptProvider")
		}
		res, derr := rp.DeliverPrompt(ctx, runtime.PromptDeliveryRequest{Binding: binding, Text: text})
		if derr != nil {
			return false, false, derr
		}
		return mapEffect(string(res.Effect))
	case promptpath.KindHost:
		if c.Host == nil {
			return false, false, fmt.Errorf("delivery: selected host path but composer has no HostPromptProvider")
		}
		res, derr := c.Host.DeliverPrompt(ctx, host.PromptRequest{Attachment: attachment, Text: text})
		if derr != nil {
			return false, false, derr
		}
		return mapEffect(string(res.Outcome))
	default:
		return false, false, fmt.Errorf("delivery: unknown path kind %s", selected.Kind)
	}
}

// mapEffect copies the adapter effect string onto kernel actions. Adapters
// must not import internal/domain; the composer copies the spelling.
func mapEffect(effect string) (delivered, provedNoEffect bool, err error) {
	switch effect {
	case "delivered":
		return true, false, nil
	case "no_effect":
		return false, true, nil
	case "unknown_effect":
		return false, false, nil
	default:
		return false, false, nil
	}
}

func runtimeBinding(a *domain.Authority, session domain.SessionID) runtime.RuntimeBinding {
	s, ok := a.Session(session)
	if !ok || s.Current == "" {
		return runtime.RuntimeBinding{}
	}
	var out runtime.RuntimeBinding
	if ws, ok := a.Workspace(s.Workspace); ok {
		out.WorkingDirectory = ws.RootPath
	}
	for _, c := range a.Correlations(domain.TargetInstance, string(s.Current)) {
		if c.Status != domain.CorrelationActive {
			continue
		}
		switch c.ExternalKind {
		case "agent.session":
			out.ExternalAgentSessionID = c.ExternalValue
		case "transcript":
			out.TranscriptID = c.ExternalValue
		}
	}
	return out
}

func hostAttachment(a *domain.Authority, session domain.SessionID) host.Attachment {
	s, ok := a.Session(session)
	if !ok || s.Attachment == "" {
		return host.Attachment{}
	}
	att, ok := a.Attachment(s.Attachment)
	if !ok {
		return host.Attachment{}
	}
	// Herdr field crossing, same as session.reconcile: pane_id →
	// container, terminal_id → epoch value.
	return host.Attachment{
		IntegrationInstanceID: att.IntegrationInstance,
		HostServerEpoch:       att.Epoch.Value,
		HostContainerID:       att.Epoch.Value,
		PaneID:                att.Container,
	}
}

func launchSettled(instance domain.RuntimeInstance, now time.Time, window time.Duration) bool {
	started, err := time.Parse(timestampLayout, instance.StartedAt)
	if err != nil {
		return false
	}
	return !now.Before(started.Add(window))
}
