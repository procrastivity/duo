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
}

// PromptPathCandidate is one runtime prompt path the composer can select
// for a prompt.deliver attempt. Step 10 names the type; step 12 (Claude
// messaging socket) fills it. Pi does not gain a runtime prompt path.
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

// RuntimePromptProvider reports a runtime-native prompt path for one
// bound instance. §5.3. This package defines the interface; it does not
// invoke it and does not dial a messaging socket.
//
//nolint:revive // name is §5.3's Go code block verbatim; see package doc comment.
type RuntimePromptProvider interface {
	PromptPath(context.Context, RuntimeBinding) (PromptPathCandidate, error)
}
