package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/manifest"
	"github.com/procrastivity/duo/internal/surface"
)

// manifestCommand constructs the `duo manifest` verb: internal/registry's
// "manifest.show" operation, CLI path {"manifest"}. It emits the
// duo.manifest/v1 document internal/manifest.Build derives from the built
// root command and the registry — no verb, asset, or operation inventory is
// restated here.
//
// root is the *cobra.Command being assembled by NewRootCommand at the
// moment this constructor runs — its subcommand list is still growing (this
// very command has not been added to it yet). That is safe: RunE only reads
// root.Commands() when `duo manifest` actually executes, by which point
// NewRootCommand has finished every AddCommand call and returned. Build's
// own doc comment requires exactly this — "the fully-constructed root
// command, not one still being assembled" — satisfied at call time, not at
// construction time.
func manifestCommand(streams *iostreams.Streams, build buildinfo.Info, root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "print duo's machine-readable capability manifest (duo.manifest/v1)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cliflags.FromContext(cmd.Context())

			if flags.Verbose {
				if _, err := fmt.Fprintln(streams.Err, "manifest: walking the built command tree and shipped asset list"); err != nil {
					return err
				}
			}

			m, err := manifest.Build(root, build)
			if err != nil {
				return duoerr.New("internal.manifest_build_failed", fmt.Sprintf("building the manifest: %v", err))
			}

			if flags.JSON {
				b, err := json.Marshal(m)
				if err != nil {
					return duoerr.New("internal.manifest_encode_failed", fmt.Sprintf("encoding the manifest: %v", err))
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}

			_, err = fmt.Fprintf(streams.Out,
				"duo %s (commit %s, built %s)\nschema:     %s\ndigest:     %s\nverbs:      %d\noperations: %d\nadapters:   %d\nassets:     %d\n",
				m.Product.Version, m.Tool.Commit, m.Tool.Date,
				m.Schema, m.ManifestDigest,
				len(m.Verbs), len(m.Operations), len(m.Adapters), len(m.Assets),
			)
			return err
		},
	}
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}
