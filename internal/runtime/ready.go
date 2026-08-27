package runtime

import "context"

// RuntimeReadyProvider reports whether a bound instance is
// interactively ready even when the host still reports
// launch_pending. Adapters that do not implement it leave D3 as
// host !LaunchPending only. Ready does not send input.
//
//nolint:revive // name matches RuntimeCorrelator / RuntimePromptProvider.
type RuntimeReadyProvider interface {
	Ready(context.Context, RuntimeBinding) (bool, error)
}
