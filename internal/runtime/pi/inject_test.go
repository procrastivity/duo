package pi_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// TestMaterializeInject writes the extension into a fresh directory
// and pins its permissions and content, mirroring closeonexit_test.go's
// TestMaterializeCloseOnExit.
func TestMaterializeInject(t *testing.T) {
	dir := t.TempDir()

	path, err := runtimepi.MaterializeInject(dir)
	if err != nil {
		t.Fatalf("MaterializeInject: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("MaterializeInject path %q is not absolute", path)
	}
	wantPath := filepath.Join(dir, runtimepi.InjectExtensionFileName)
	if path != wantPath {
		t.Errorf("MaterializeInject path = %q, want %q", path, wantPath)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat extension file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("extension file perm = %o, want 0600", info.Mode().Perm())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading materialized extension: %v", err)
	}
	if string(got) != runtimepi.InjectExtensionSource {
		t.Errorf("materialized extension does not match the embedded source")
	}
}

// TestInjectExtensionSourceMatchesFile pins the embed against the
// on-disk asset so a broken //go:embed fails loudly.
func TestInjectExtensionSourceMatchesFile(t *testing.T) {
	onDisk, err := os.ReadFile(filepath.Join("inject", "duo-inject.ts"))
	if err != nil {
		t.Fatalf("reading inject asset file: %v", err)
	}
	if runtimepi.InjectExtensionSource != string(onDisk) {
		t.Errorf("InjectExtensionSource does not match inject/duo-inject.ts on disk")
	}
}

// TestInjectSocketPathHonorsXDGRuntimeDir mirrors closeonexit's XDG pattern
// for pi's inject socket directory.
func TestInjectSocketPathHonorsXDGRuntimeDir(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)

	sessionID := "01a02c19-65e1-7346-b418-82ab0d32942c"
	got, err := runtimepi.InjectSocketPath(sessionID)
	if err != nil {
		t.Fatalf("InjectSocketPath: %v", err)
	}
	want := filepath.Join(dir, "duo", "pi-inject", sessionID+".sock")
	if got != want {
		t.Errorf("InjectSocketPath() = %q, want %q", got, want)
	}
}

// TestInjectSocketPathExtractsIDFromTranscriptPath pins Herdr's path-shaped
// Pi identity: a transcript file name reduces to the embedded session uuid
// before the socket path is built.
func TestInjectSocketPathExtractsIDFromTranscriptPath(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)

	transcript := "/tmp/sessions/--cwd--/2026-08-23T00-50-57-121Z_01a02c19-65e1-7346-b418-82ab0d32942c.jsonl"
	sessionID := "01a02c19-65e1-7346-b418-82ab0d32942c"
	got, err := runtimepi.InjectSocketPath(transcript)
	if err != nil {
		t.Fatalf("InjectSocketPath: %v", err)
	}
	want := filepath.Join(dir, "duo", "pi-inject", sessionID+".sock")
	if got != want {
		t.Errorf("InjectSocketPath() = %q, want %q", got, want)
	}
}

// TestInjectSocketPathRejectsEmptyAndRawDirectoryPath pins that locate
// needs a Pi session id, not an arbitrary filesystem path.
func TestInjectSocketPathRejectsEmptyAndRawDirectoryPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", dir)

	if _, err := runtimepi.InjectSocketPath(""); err == nil {
		t.Error(`InjectSocketPath("") accepted empty session id`)
	}

	if _, err := runtimepi.InjectSocketPath("/tmp/sessions/--cwd--"); err == nil {
		t.Error("InjectSocketPath accepted raw directory path without transcript uuid")
	}
}

// TestInjectSocketPathFallsBackToRunUser mirrors the inject extension's own
// runtimeDir fallback when XDG_RUNTIME_DIR is unset.
func TestInjectSocketPathFallsBackToRunUser(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "")

	sessionID := "01a02c19-65e1-7346-b418-82ab0d32942c"
	got, err := runtimepi.InjectSocketPath(sessionID)
	if err != nil {
		t.Fatalf("InjectSocketPath: %v", err)
	}
	prefix := filepath.Join("/run/user", strconv.Itoa(os.Getuid()), "duo", "pi-inject")
	if !strings.HasPrefix(got, prefix) {
		t.Errorf("InjectSocketPath() = %q, want prefix %q", got, prefix)
	}
}
