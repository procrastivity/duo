// Package cli is the one registration point for every verb: it builds the
// root Cobra command, binds the two global flags once, and maps whatever
// Execute returns to the process exit code. cmd/duo/main.go does nothing
// beyond calling into this package.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/duoerr"
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
		// --output json modes come from one code path; Cobra's own
		// printing would double up or bypass the JSON envelope.
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			output, err := outputMode(cmd)
			if err != nil {
				return err
			}
			verbose, err := cmd.Flags().GetBool("verbose")
			if err != nil {
				return err
			}
			cmd.SetContext(cliflags.WithFlags(cmd.Context(), cliflags.Flags{Output: output, Verbose: verbose}))
			return nil
		},
	}
	root.SetOut(streams.Out)
	root.SetErr(streams.Err)

	root.PersistentFlags().String(cliflags.OutputFlag, cliflags.OutputText, `result format: "text" or "json"`)
	root.PersistentFlags().BoolP("verbose", "v", false, "extra diagnostic lines on stderr")

	root.AddCommand(versionCommand(streams, build))
	root.AddCommand(manifestCommand(streams, build, root))
	root.AddCommand(doctorCommand(streams))
	root.AddCommand(sessionCommand(streams))
	root.AddCommand(conversationCommand(streams))
	root.AddCommand(promptCommand(streams))
	root.AddCommand(configCommand(streams))
	root.AddCommand(workspaceCommand(streams))
	root.AddCommand(providerCommand(streams))

	// DUO_SELFTEST-gated fixture command: an end-to-end, through-the-built-
	// binary exercise of the code-3/--output json refusal path before any real
	// refusal-raising verb exists. Gating it behind an env var keeps it out
	// of the real binary's command tree — and so out of any future manifest
	// walk — while still being reachable for that test.
	if os.Getenv("DUO_SELFTEST") == "1" {
		root.AddCommand(selftest.RefusalCommand())
	}

	return root
}

// outputMode reads and validates the root persistent --output flag off cmd.
// Cobra merges root's persistent flags into every descendant's Flags(), so
// the same read works from root's own PersistentPreRunE and from Execute's
// error path, whichever command parsing stopped on.
//
// Validation happens here, once, in the one hook every verb passes through:
// no verb re-validates, and no verb has to. An unrecognized value is a
// caller mistake, not a verb failure — invalid.request, exit 1.
func outputMode(cmd *cobra.Command) (string, error) {
	v, err := cmd.Flags().GetString(cliflags.OutputFlag)
	if err != nil {
		return "", err
	}
	switch v {
	case "", cliflags.OutputText:
		return cliflags.OutputText, nil
	case cliflags.OutputJSON:
		return cliflags.OutputJSON, nil
	default:
		return "", duoerr.New("invalid.request",
			fmt.Sprintf("--output must be \"text\" or \"json\", not %q.", v))
	}
}
