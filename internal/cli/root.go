// Package cli is the one registration point for every verb: it builds the
// root Cobra command, binds the two global flags once, and maps whatever
// Execute returns to the process exit code. cmd/duo/main.go does nothing
// beyond calling into this package.
package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/selftest"
)

// NewRootCommand builds the duo root command with both global flags bound
// and every verb registered. It is the only place any verb gets registered —
// no ad hoc init() side effects live anywhere else.
func NewRootCommand(streams *iostreams.Streams, build buildinfo.Info) *cobra.Command {
	root := &cobra.Command{
		Use:   "duo",
		Short: "duo — attach to and observe agent terminal sessions",
		// Setting Version wires Cobra's built-in --version flag, so
		// `duo --version` works alongside the richer `duo version` verb
		// (which also carries commit and build date). wip's chassis has
		// only the verb; duo's bootstrap contract asks for both.
		Version: build.Version,
		// We render every error ourselves (see Execute) so human and
		// --json modes come from one code path; Cobra's own printing
		// would double up or bypass the --json envelope.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			jsonOut, err := cmd.Flags().GetBool("json")
			if err != nil {
				return err
			}
			verbose, err := cmd.Flags().GetBool("verbose")
			if err != nil {
				return err
			}
			cmd.SetContext(cliflags.WithFlags(cmd.Context(), cliflags.Flags{JSON: jsonOut, Verbose: verbose}))
			return nil
		},
	}
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)

	root.PersistentFlags().Bool("json", false, "emit the success payload as one JSON value")
	root.PersistentFlags().BoolP("verbose", "v", false, "extra diagnostic lines on stderr")

	root.AddCommand(versionCommand(streams, build))
	root.AddCommand(manifestCommand(streams, build, root))
	root.AddCommand(doctorCommand(streams))

	// DUO_SELFTEST-gated fixture command: an end-to-end, through-the-built-
	// binary exercise of the code-3/--json refusal path before any real
	// refusal-raising verb exists. Gating it behind an env var keeps it out
	// of the real binary's command tree — and so out of any future manifest
	// walk — while still being reachable for that test.
	if os.Getenv("DUO_SELFTEST") == "1" {
		root.AddCommand(selftest.RefusalCommand())
	}

	return root
}
