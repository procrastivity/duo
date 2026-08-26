package herdr

import (
	"context"
	"errors"

	"github.com/procrastivity/duo/internal/host"
)

// herdrPromptPath is the static HostPromptProvider offer for Herdr
// agent.prompt. Realization is native (Herdr's own complete-turn method,
// not synthesized send-keys). Quality is exact so a min-exact selector
// still has a host fallback when the runtime does not offer a path.
// ComposerSafe
// is false: notes/19 §2 verified that agent.prompt merges into and submits
// a live human composer draft with a success result.
var herdrPromptPath = host.PromptPathCandidate{
	Quality:      "exact",
	Realization:  "native",
	ComposerSafe: false,
}

// provenNoEffectPromptCodes are Herdr agent.prompt refusals that prove the
// pane accepted no input (notes/19 §3; conformance mapping at notes/19:397-402).
// The kernel may retry these; this adapter does not loop.
var provenNoEffectPromptCodes = map[string]struct{}{
	CodeAgentNotReady:     {},
	CodeAgentBlocked:      {},
	CodeEmptyAgentPrompt:  {},
	CodeAgentNotRunning:   {},
	CodeAgentKindMismatch: {},
	CodeAgentNotFound:     {},
	CodeInvalidRequest:    {},
}

// PromptPath implements host.HostPromptProvider. The offer is a property
// of this host, not of one pane's current agent registration, so this
// call does no I/O.
func (h *Host) PromptPath(_ context.Context, attachment host.Attachment) (host.PromptPathCandidate, error) {
	if err := h.requireInstance(attachment.IntegrationInstanceID); err != nil {
		return host.PromptPathCandidate{}, err
	}
	return herdrPromptPath, nil
}

// DeliverPrompt implements host.HostPromptProvider: one agent.prompt call
// with no wait object and no retry loop. Quiet-gate lives in
// internal/delivery; require_ready and kernel retry of no_effect stay
// outside this adapter.
func (h *Host) DeliverPrompt(ctx context.Context, req host.PromptRequest) (host.PromptAttemptResult, error) {
	if err := h.requireInstance(req.Attachment.IntegrationInstanceID); err != nil {
		return host.PromptAttemptResult{}, err
	}
	if req.Attachment.PaneID == "" {
		return host.PromptAttemptResult{}, errors.New("herdr: prompt request has no pane ID")
	}

	name, result, err := h.namedAgentOnPane(ctx, req.Attachment.PaneID)
	if err != nil {
		// agent.list is not pane input. A lookup failure proves this
		// prompt was never written.
		return host.PromptAttemptResult{
			Outcome:  host.PromptOutcomeNoEffect,
			HostCode: ErrorCode(err),
		}, nil
	}
	if result.Outcome != "" {
		return result, nil
	}

	written, err := h.client.callAdmit(ctx, "agent.prompt", agentPromptParams{
		Target: name,
		Text:   req.Text,
	}, nil)
	if err != nil {
		return mapPromptAttempt(err, written), nil
	}
	// agent_prompted is admission into the PTY, not submission and not
	// acknowledgment. False success is verified at 0.8.2: until-matching
	// can return while the text sits as an unsubmitted composer draft
	// (notes/19 §3).
	return host.PromptAttemptResult{
		Outcome:      host.PromptOutcomeDelivered,
		Acknowledged: false,
		HostCode:     CodeAgentPrompted,
	}, nil
}

// namedAgentOnPane resolves agent.prompt's target from the pane. agent.list
// is a name lookup, not an inventory: Discover still enumerates panes from
// session.snapshot. No named agent is the same pre-delivery shape as
// agent_not_ready ("not an active named agent") — proven no-effect, and
// agent.prompt is never called.
func (h *Host) namedAgentOnPane(ctx context.Context, paneID string) (string, host.PromptAttemptResult, error) {
	var listed agentListResult
	if _, err := h.client.callAdmit(ctx, "agent.list", nil, &listed); err != nil {
		return "", host.PromptAttemptResult{}, err
	}
	for _, agent := range listed.Agents {
		if agent.PaneID == paneID && agent.Name != "" {
			return agent.Name, host.PromptAttemptResult{}, nil
		}
	}
	return "", host.PromptAttemptResult{
		Outcome:  host.PromptOutcomeNoEffect,
		HostCode: CodeAgentNotReady,
	}, nil
}

func mapPromptAttempt(err error, written bool) host.PromptAttemptResult {
	code := ErrorCode(err)
	if code != "" {
		if _, ok := provenNoEffectPromptCodes[code]; ok {
			return host.PromptAttemptResult{Outcome: host.PromptOutcomeNoEffect, HostCode: code}
		}
		// agent_pane_busy on agent.start is a pre-delivery refusal.
		// On agent.prompt a write may already have happened, so busy,
		// stall, and timeout are unknown_effect. Do not retry
		// agent_prompt_stalled (decision-03 §7.1, notes/19 §3).
		return host.PromptAttemptResult{Outcome: host.PromptOutcomeUnknownEffect, HostCode: code}
	}
	if !written {
		return host.PromptAttemptResult{Outcome: host.PromptOutcomeNoEffect}
	}
	return host.PromptAttemptResult{Outcome: host.PromptOutcomeUnknownEffect}
}
