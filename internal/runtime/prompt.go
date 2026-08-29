package runtime

import "context"

// RuntimeBinding scopes RuntimePromptProvider.PromptPath to one bound
// runtime instance's external identifiers. Field shapes follow
// ConditionObservationRequest: the adapter layer does not know Duo
// session IDs.
//
//nolint:revive // name is §5.3's Go code block verbatim; see package doc comment.
type RuntimeBinding struct {
	ExternalAgentSessionID string
	TranscriptID           string
	// WorkingDirectory is the Duo session's workspace root. Empty when
	// unknown. Claude and Pi ignore it; Devin ACP session/load needs it
	// (notes/59). It is not a bind key — Correlate still requires an
	// ExternalAgentSessionID.
	WorkingDirectory string
}

// PromptPathCandidate is one runtime prompt path the composer can select
// for a prompt.deliver attempt. Step 10 names the type; Claude (messaging
// socket) and Pi (inject socket, duo-pi-inject Stage A) fill it.
// PromptPath does not send input.
//
// Quality is exact | degraded | heuristic. Realization is native |
// adapted | synthesized. ComposerSafe is true when the path does not
// clobber a TUI composer.
type PromptPathCandidate struct {
	Quality      string
	Realization  string
	ComposerSafe bool
}

// PromptEffect is the adapter attempt result for one DeliverPrompt call.
// These strings live here, not in internal/domain: adapters must not
// import the domain kernel. delivered is native-quality admission of
// the frame. no_effect and unknown_effect match decision-03 §4.3
// (automatic retry only on proved no_effect).
type PromptEffect string

// Closed prompt-attempt effect values.
const (
	PromptEffectDelivered     PromptEffect = "delivered"
	PromptEffectNoEffect      PromptEffect = "no_effect"
	PromptEffectUnknownEffect PromptEffect = "unknown_effect"
)

// PromptDeliveryRequest is one DeliverPrompt call. Text is the semantic
// prompt body; Binding names the target instance. This package does not
// carry a reporter credential or a messaging token on the request —
// those are instance configuration, and they stay distinct.
type PromptDeliveryRequest struct {
	Binding RuntimeBinding
	Text    string
}

// PromptDeliveryResult is what DeliverPrompt returns after the adapter
// has attempted the send. Effect is the attempt result; this package
// does not dial a socket.
type PromptDeliveryResult struct {
	Effect PromptEffect
}

// RuntimePromptProvider reports a runtime-native prompt path for one
// bound instance and, when the composer has selected that path, sends
// one prompt. §5.3 names only PromptPath — that method still does not
// send. DeliverPrompt is the tightly named send on the same interface
// (not a companion): the seal tests need socket I/O and effect
// certainty, which PromptPath cannot express. This package defines the
// interface; it does not invoke it and does not dial a messaging
// socket.
//
//nolint:revive // name is §5.3's Go code block verbatim; see package doc comment.
type RuntimePromptProvider interface {
	PromptPath(context.Context, RuntimeBinding) (PromptPathCandidate, error)
	DeliverPrompt(context.Context, PromptDeliveryRequest) (PromptDeliveryResult, error)
}
