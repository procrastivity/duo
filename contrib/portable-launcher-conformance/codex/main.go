// Package main provides the thin Codex adapter for the portable-launcher suite.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/procrastivity/duo/internal/conformance/portablelauncher"
)

func main() {
	root := os.Getenv("DUO_CONFORMANCE_RUN_ROOT")
	runOrigin := os.Getenv(portablelauncher.CommandRunOriginEnv)
	spec := portablelauncher.DriverSpec{
		Name: "codex", Executable: filepath.Join(root, "bin", "codex"),
		Arguments: []string{"--ask-for-approval", "never", "--sandbox", "workspace-write", "exec", "--ephemeral", "--ignore-user-config", "--skip-git-repo-check", "--json", portablelauncher.TaskArgument},
	}
	capture, runErr := portablelauncher.RunDriver(context.Background(), spec, portablelauncher.RequestFromRunRoot(root, runOrigin))
	persistErr := portablelauncher.PersistDriverCapture(root, capture)
	err := errors.Join(runErr, persistErr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
