//go:build !linux

package portablelauncher

import (
	"fmt"
	"os"
)

const (
	CommandCaptureDirectoryEnv = "DUO_CONFORMANCE_COMMAND_CAPTURE_DIR"
	CommandRunOriginEnv        = "DUO_CONFORMANCE_RUN_ORIGIN_BOOTTIME_NS"
	commandRecorderFail        = 125
)

// RecordCommandIfConfigured leaves ordinary non-Linux Duo execution
// unchanged. A conformance capture request fails closed because the live
// portable-launcher suite requires Linux process and clock evidence.
func RecordCommandIfConfigured() (recorded bool, exitCode int) {
	if os.Getenv(CommandCaptureDirectoryEnv) == "" && os.Getenv(CommandRunOriginEnv) == "" {
		return false, 0
	}
	_, _ = fmt.Fprintln(os.Stderr, "duo command recorder: portable launcher conformance recording requires Linux")
	return true, commandRecorderFail
}
