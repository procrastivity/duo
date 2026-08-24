package cli

import (
	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// providerDisableOperation is the operation this verb implements: the one
// registered operation name this file may name as a literal (see
// provider.go's file-split note).
const providerDisableOperation = "provider.disable"

// providerDisableCommand constructs `duo provider disable`, projecting
// internal/registry's provider.disable operation at CLI path
// {"provider", "disable"}: a standing decision that launch resolution must
// not choose the named provider, recorded through
// domain.Authority.DisableProvider. Disabling a name no launch_variant
// currently declares is allowed — the kernel holds no config and cannot
// refuse on that basis — the result and the text-mode render just say so.
func providerDisableCommand(streams *iostreams.Streams) *cobra.Command {
	var configPath, actor, reason string
	cmd := &cobra.Command{
		Use:   "disable <name>",
		Short: "record a standing decision that launch resolution must not choose this provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runProviderToggle(cmd, streams, providerToggleParams{
				operation:  providerDisableOperation,
				name:       args[0],
				enable:     false,
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
