//go:build linux

package portablelauncher

import "golang.org/x/sys/unix"

func publishBundle(stage, destination string) error {
	// RENAME_NOREPLACE is the publication boundary. Unlike os.Rename for
	// directories, it cannot replace an empty destination created by a racer.
	return unix.Renameat2(unix.AT_FDCWD, stage, unix.AT_FDCWD, destination, unix.RENAME_NOREPLACE)
}
