package herdr

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ListenerEnviron returns the exec-time environment of the process that
// listens on socketPath, in os.Environ() shape.
//
// It exists for the doctor scrub-gate warning (notes/51 9d): a Herdr pane
// inherits the *server's* environment, so reading the listener's
// /proc/<pid>/environ is what a new pane would inherit — without dialing
// the API socket (I-3). Enumerating is not attesting; a missing socket or
// an unreadable environ is an error the caller folds into "no warning".
//
// The path is resolved with filepath.Clean so two spellings of the same
// socket still match the /proc/net/unix Path column.
func ListenerEnviron(socketPath string) ([]string, error) {
	pid, err := listenerPID(socketPath)
	if err != nil {
		return nil, err
	}
	return readProcEnviron(pid)
}

// listenerPID finds the process that holds the listening end of a Unix
// socket, by inode: /proc/net/unix names the path and inode, then
// /proc/*/fd/* is scanned for socket:[inode]. No connect, no dial.
func listenerPID(socketPath string) (int, error) {
	want := filepath.Clean(socketPath)
	inode, err := unixSocketInode(want)
	if err != nil {
		return 0, err
	}
	return pidHoldingSocketInode(inode)
}

func unixSocketInode(socketPath string) (uint64, error) {
	raw, err := os.ReadFile("/proc/net/unix")
	if err != nil {
		return 0, fmt.Errorf("herdr: reading /proc/net/unix: %w", err)
	}
	lines := bytes.Split(raw, []byte{'\n'})
	for i, line := range lines {
		if i == 0 || len(line) == 0 {
			continue
		}
		fields := strings.Fields(string(line))
		// Num RefCount Protocol Flags Type St Inode [Path]
		if len(fields) < 8 {
			continue
		}
		path := fields[len(fields)-1]
		if !strings.HasPrefix(path, "/") {
			continue
		}
		if filepath.Clean(path) != socketPath {
			continue
		}
		inode, err := strconv.ParseUint(fields[6], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("herdr: parsing inode for %s: %w", socketPath, err)
		}
		return inode, nil
	}
	return 0, fmt.Errorf("herdr: no listening socket at %s", socketPath)
}

func pidHoldingSocketInode(inode uint64) (int, error) {
	want := "socket:[" + strconv.FormatUint(inode, 10) + "]"
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, fmt.Errorf("herdr: reading /proc: %w", err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", entry.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue
			}
			if target == want {
				return pid, nil
			}
		}
	}
	return 0, fmt.Errorf("herdr: no process holds socket inode %d", inode)
}

func readProcEnviron(pid int) ([]string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return nil, fmt.Errorf("herdr: read listener environment: %w", err)
	}
	parts := bytes.Split(raw, []byte{0})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		out = append(out, string(p))
	}
	return out, nil
}
