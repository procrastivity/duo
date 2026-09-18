// Package main provides the thin Amp adapter for the portable-launcher suite.
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
		Name: "amp", Executable: filepath.Join(root, "bin", "amp"),
		Arguments: []string{"-x", portablelauncher.TaskArgument, "--stream-json", "--no-ide", "--settings-file", filepath.Join(root, "xdg", "config", "amp", "settings.json")},
	}
	capture, runErr := portablelauncher.RunDriver(context.Background(), spec, portablelauncher.RequestFromRunRoot(root, runOrigin))
	persistErr := portablelauncher.PersistDriverCapture(root, capture)
	err := errors.Join(runErr, persistErr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
