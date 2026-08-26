package host

import "context"

// PromptPathCandidate is one host prompt path the composer can select for
// a prompt.deliver attempt. PromptPath reports the path; it does not send
// input. DeliverPrompt (same interface) is the send: architecture §5.2
// only names PromptPath, but the delivery call has to live here because a
// semantic attempt result cannot be produced without I/O.
//
// Quality is exact | degraded | heuristic. Realization is native |
// adapted | synthesized. ComposerSafe is true when the path does not
// clobber a TUI composer.
type PromptPathCandidate struct {
	Quality      string
	Realization  string
	ComposerSafe bool
}

// PromptRequest is DeliverPrompt's input: one complete-turn text aimed at
// one host attachment. The adapter never sees a Duo session ID.
type PromptRequest struct {
	Attachment Attachment
	Text       string
}

// PromptOutcome is the adapter-side attempt result. delivered means the
// host accepted the call; no_effect and unknown_effect use the same
// spellings as domain.EffectCertainty so a composer can copy them. This
// package owns the type so adapters never import internal/domain.
type PromptOutcome string

// Closed prompt-attempt outcomes a HostPromptProvider can report.
const (
	PromptOutcomeDelivered     PromptOutcome = "delivered"
	PromptOutcomeNoEffect      PromptOutcome = "no_effect"
	PromptOutcomeUnknownEffect PromptOutcome = "unknown_effect"
)

// PromptAttemptResult is what DeliverPrompt returns. A host prompt path
// supplies a semantic attempt result; it never changes host evidence into
// an agent acknowledgment (§5.2). Acknowledged is therefore independent
// of Outcome: a delivered result may still have Acknowledged false.
type PromptAttemptResult struct {
	Outcome PromptOutcome
	// Acknowledged is true only when a qualified target protocol returns
	// a receipt with acknowledgment semantics. Transport acceptance,
	// condition waits, and agent_prompted success are not acknowledgments.
	Acknowledged bool
	// HostCode is the host's typed success or refusal code when one
	// exists (agent_prompted, agent_not_ready, …). Empty on a transport
	// failure that never produced a protocol answer.
	HostCode string
}

// HostPromptProvider reports a host-side prompt path for one attachment
// and, when selected, delivers one complete-turn prompt on that path.
// §5.2 names PromptPath; DeliverPrompt is the send this milestone needs.
// This package defines the interface; it does not invoke it.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostPromptProvider interface {
	PromptPath(context.Context, Attachment) (PromptPathCandidate, error)
	DeliverPrompt(context.Context, PromptRequest) (PromptAttemptResult, error)
}
