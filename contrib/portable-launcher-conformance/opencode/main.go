// Package main provides the thin OpenCode adapter for the portable-launcher suite.
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
		Name: "opencode", Executable: filepath.Join(root, "bin", "opencode"),
		Arguments: []string{"run", "--pure", "--format", "json", portablelauncher.TaskArgument},
	}
	request := portablelauncher.RequestFromRunRoot(root, runOrigin)
	request.Environment = map[string]string{"OPENCODE_DISABLE_MODELS_FETCH": "1"}
	capture, runErr := portablelauncher.RunDriver(context.Background(), spec, request)
	persistErr := portablelauncher.PersistDriverCapture(root, capture)
	err := errors.Join(runErr, persistErr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
