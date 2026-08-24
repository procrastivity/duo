package cli

import (
	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// providerEnableOperation is the operation this verb implements: the one
// registered operation name this file may name as a literal (see
// provider.go's file-split note).
const providerEnableOperation = "provider.enable"

// providerEnableCommand constructs `duo provider enable`, projecting
// internal/registry's provider.enable operation at CLI path
// {"provider", "enable"}: a standing decision that the named provider is
// eligible again, recorded through domain.Authority.EnableProvider. It
// still writes even for a provider with no standing fact at all — already
// enabled by default — because the write is durable evidence of the
// explicit decision.
func providerEnableCommand(streams *iostreams.Streams) *cobra.Command {
	var configPath, actor, reason string
	cmd := &cobra.Command{
		Use:   "enable <name>",
		Short: "record a standing decision that this provider is eligible again",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProviderToggle(cmd, streams, providerToggleParams{
				operation:  providerEnableOperation,
				name:       args[0],
				enable:     true,
				configPath: configPath,
				actor:      actor,
				reason:     reason,
			})
		},
	}
	providerConfigFlag(cmd, &configPath)
	providerActorFlags(cmd, &actor, &reason)
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}
