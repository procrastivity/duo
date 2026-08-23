//go:build unix

package scrub

import (
	"os/exec"
	"syscall"
)

// setSysProcAttrDetached makes the disposable herdr server its own
// session leader, so it survives this test process the way a real
// Duo-owned server launch would (and matches the manual `setsid herdr
// server` shape verified in testdata/scrub-live-2026-08-23.md). Herdr
// only ever runs on Unix-like platforms, so this is gated on the `unix`
// build tag rather than duplicated per-GOOS.
func setSysProcAttrDetached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func syscallKill(pid int) error {
	return syscall.Kill(pid, syscall.SIGTERM)
}
