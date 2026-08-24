// Package selftest holds test-fixture commands that exist only to exercise
// the chassis's own error-envelope machinery end-to-end through the built
// binary. They are wired into the root command only when DUO_SELFTEST=1 is
// set (see internal/cli.NewRootCommand) so they never appear in a real
// install or a future manifest walk.
package selftest

import (
	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/duoerr"
)

// RefusalCommand manufactures a refusal-style *duoerr.Error, unconditionally,
// so the chassis's tests can assert the code-3 exit path and its
// --output json envelope without borrowing a real refusal check from a verb that has not
// been built yet.
func RefusalCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "__selftest-refusal",
		Short:  "chassis test fixture: manufactures a refusal-style error",
		Hidden: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return duoerr.New(
				"refusal.selftest",
				"refused — selftest fixture refusal, exercising the code-3 exit path",
			)
		},
	}
}
