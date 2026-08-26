package herdr

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestListenerEnvironReadsExecTimeEnvironment covers the doctor scrub-gate
// probe: a process that listens on a Unix socket with a marker in its
// exec-time environment is visible through ListenerEnviron, with no dial.
func TestListenerEnvironReadsExecTimeEnvironment(t *testing.T) {
	if _, err := os.Stat("/proc/self/environ"); err != nil {
		t.Skip("no procfs on this host")
	}

	dir := t.TempDir()
	socket := filepath.Join(dir, "herdr.sock")
	ready := filepath.Join(dir, "ready")

	cmd := exec.Command(os.Args[0], "-test.run=^TestListenerEnvironHelper$", "--", socket, ready)
	cmd.Env = append(os.Environ(),
		"GO_WANT_HELPER_PROCESS=1",
		"DUO_LISTENER_MARKER=present",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("helper did not signal ready")
		}
		time.Sleep(20 * time.Millisecond)
	}

	environ, err := ListenerEnviron(socket)
	if err != nil {
		t.Fatalf("ListenerEnviron: %v", err)
	}
	found := false
	for _, e := range environ {
		if e == "DUO_LISTENER_MARKER=present" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ListenerEnviron missing DUO_LISTENER_MARKER; got %d entries", len(environ))
	}
}

// TestListenerEnvironHelper is the subprocess that listens. It is never a
// real test case; the parent sets GO_WANT_HELPER_PROCESS=1.
func TestListenerEnvironHelper(_ *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" && i+2 < len(args) {
			socket, ready := args[i+1], args[i+2]
			ln, err := net.Listen("unix", socket)
			if err != nil {
				os.Exit(2)
			}
			defer func() { _ = ln.Close() }()
			if err := os.WriteFile(ready, []byte("1"), 0o600); err != nil {
				os.Exit(3)
			}
			// Hold the listening socket until the parent kills us.
			select {}
		}
	}
	os.Exit(1)
}

func TestListenerEnvironMissingSocket(t *testing.T) {
	if _, err := os.Stat("/proc/net/unix"); err != nil {
		t.Skip("no /proc/net/unix on this host")
	}
	_, err := ListenerEnviron(filepath.Join(t.TempDir(), "absent.sock"))
	if err == nil {
		t.Fatal("ListenerEnviron invented an environ for a missing socket")
	}
}
