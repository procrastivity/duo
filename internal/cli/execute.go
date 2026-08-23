package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
)

// Execute runs root and returns the process exit code, per the chassis's
// exit-code table. It distinguishes a Cobra argument-parsing error (code 2)
// from a verb-raised structured error (codes 1/3/4, read off the error's own
// code field) — two distinct return paths, not one blanket non-zero exit.
func Execute(root *cobra.Command, streams *iostreams.Streams) int {
	cmd, err := root.ExecuteC()
	if err == nil {
		return exitcode.Success
	}

	var derr *duoerr.Error
	if errors.As(err, &derr) {
		jsonOut, _ := cmd.Flags().GetBool("json")
		duoerr.Render(streams.Err, verbPath(root, cmd), derr, jsonOut)
		return exitcode.FromError(derr)
	}

	// Anything that isn't our own structured error type reached here via
	// Cobra's own argument-parsing path (bad flags, unknown command,
	// missing required arg) — a usage error, not a verb failure.
	_, _ = fmt.Fprintf(streams.Err, "duo: %s\n", err)
	return exitcode.Usage
}

// verbPath names the failing verb for the human-mode "duo: <verb>: <message>"
// line — the full subcommand path minus the root command's own name, so
// nested verbs read naturally without redesign later.
func verbPath(root, cmd *cobra.Command) string {
	path := strings.TrimPrefix(cmd.CommandPath(), root.Name())
	return strings.TrimSpace(path)
}
