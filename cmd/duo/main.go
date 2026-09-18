// Command duo is the entrypoint for the duo CLI. Apart from the explicitly
// configured portable-launcher command recorder, it only constructs the root
// command, calls Execute, and maps the result to an exit code.
package main

import (
	"os"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/cli"
	"github.com/procrastivity/duo/internal/conformance/portablelauncher"
	"github.com/procrastivity/duo/internal/iostreams"
)

// version, commit, and date are set via -ldflags at build time. Both the
// Makefile's build/cross-compile targets and the Nix buildGoModule package
// target this exact package path and these exact var names — a locally-built
// binary and a Nix-built one carry identical labels.
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

func main() {
	if recorded, exitCode := portablelauncher.RecordCommandIfConfigured(); recorded {
		os.Exit(exitCode)
	}
	// Keep the ordinary execution path unchanged when recording is disabled.
	streams := iostreams.System()
	build := buildinfo.Info{Version: version, Commit: commit, Date: date}
	root := cli.NewRootCommand(streams, build)
	os.Exit(cli.Execute(root, streams))
}
