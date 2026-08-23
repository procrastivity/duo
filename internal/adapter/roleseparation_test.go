package adapter

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// modulePath is this repository's module path, used to build the package
// patterns handed to `go list`.
const modulePath = "github.com/procrastivity/duo"

// TestRoleSeparation is the mechanical proof §5.4 requires: "Host adapters
// compile without importing an agent-runtime adapter package" and "Runtime
// adapters compile without importing a host adapter package." It shells
// out to `go list -deps` — the mechanism the Step 11 spec names as an
// alternative to a go/packages dependency — rather than trusting a manual
// read of the import graph.
//
// To prove this test actually catches a violation (rather than passing
// vacuously), the verification step for this change temporarily added a
// blank import from internal/host to internal/runtime, confirmed this test
// failed, and reverted it.
func TestRoleSeparation(t *testing.T) {
	t.Run("host does not import runtime", func(t *testing.T) {
		assertNoCrossImport(t, modulePath+"/internal/host/...", modulePath+"/internal/runtime")
	})
	t.Run("runtime does not import host", func(t *testing.T) {
		assertNoCrossImport(t, modulePath+"/internal/runtime/...", modulePath+"/internal/host")
	})
}

// assertNoCrossImport fails t if any package matching pattern transitively
// depends on forbiddenPrefix or any package under it.
func assertNoCrossImport(t *testing.T, pattern, forbiddenPrefix string) {
	t.Helper()
	for _, dep := range goListDeps(t, pattern) {
		if dep == forbiddenPrefix || strings.HasPrefix(dep, forbiddenPrefix+"/") {
			t.Fatalf("%s transitively imports %s", pattern, dep)
		}
	}
}

// goListDeps runs `go list -deps pattern` from the module root and returns
// the reported package paths.
func goListDeps(t *testing.T, pattern string) []string {
	t.Helper()
	cmd := exec.Command("go", "list", "-deps", pattern)
	cmd.Dir = moduleRoot(t)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("go list -deps %s: %v\n%s", pattern, err, stderr.String())
	}
	return strings.Fields(stdout.String())
}

// moduleRoot locates the directory holding go.mod, so goListDeps works
// regardless of the working directory `go test` starts it in.
func moduleRoot(t *testing.T) string {
	t.Helper()
	cmd := exec.Command("go", "env", "GOMOD")
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	if err := cmd.Run(); err != nil {
		t.Fatalf("go env GOMOD: %v", err)
	}
	gomod := strings.TrimSpace(stdout.String())
	if gomod == "" || gomod == "/dev/null" {
		t.Fatalf("go env GOMOD: no module found (got %q)", gomod)
	}
	return filepath.Dir(gomod)
}
