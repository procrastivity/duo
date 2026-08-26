package host

import "context"

// PromptPathCandidate is one host prompt path the composer can select for
// a prompt.deliver attempt. Step 10 names the type; step 11 (Herdr
// HostPromptProvider) fills it. PromptPath does not send input.
//
// Quality is exact | degraded | heuristic. Realization is native |
// adapted | synthesized. ComposerSafe is true when the path does not
// clobber a TUI composer.
type PromptPathCandidate struct {
	Quality      string
	Realization  string
	ComposerSafe bool
}

// HostPromptProvider reports a host-side prompt path for one attachment.
// §5.2. This package defines the interface; it does not invoke it and
// does not call Herdr.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostPromptProvider interface {
	PromptPath(context.Context, Attachment) (PromptPathCandidate, error)
}
