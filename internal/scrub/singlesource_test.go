package scrub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// moduleRoot walks up from the package directory to the directory holding
// go.mod. Mirrors internal/registry/conformance_test.go's helper of the
// same name.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the package directory")
		}
		dir = parent
	}
}

// singleSourceLiterals is every quoted-string form that would duplicate
// this package's scrub policy if it appeared in another package: the
// three exact marker names, and the bare wildcard prefix itself (the
// literal a HasPrefix-style reimplementation would need). Built from
// Markers and WildcardPrefixes rather than retyped, so this test cannot
// drift from markers.go.
func singleSourceLiterals() []string {
	lits := make([]string, 0, len(Markers)+len(WildcardPrefixes))
	lits = append(lits, Markers...)
	lits = append(lits, WildcardPrefixes...)
	return lits
}

// TestNoDuplicateMarkerLiteralsOutsideScrub is the repo-wide half of the
// single-source rule Step 20 requires: no package other than
// internal/scrub may hardcode one of the scrub marker names or the
// wildcard prefix as a Go string literal. It mirrors
// internal/registry/conformance_test.go's
// TestNoDuplicateOperationTableOutsideRegistry, adapted from "N-or-more
// mentions is an inventory" (registry's operation names have legitimate
// one-off call sites) to zero tolerance: nothing outside this package
// legitimately needs to spell CLAUDECODE, AI_AGENT,
// CLAUDE_CODE_CHILD_SESSION, or CLAUDE_ as a string literal — a consumer
// that needs to test or act on the deny list calls scrub.Markers,
// scrub.WildcardPrefixes, or scrub.IsMarker instead.
func TestNoDuplicateMarkerLiteralsOutsideScrub(t *testing.T) {
	root := moduleRoot(t)
	// go test runs with the working directory set to the package under
	// test, so this is internal/scrub's own directory — the one this
	// walk must skip.
	scrubDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	literals := singleSourceLiterals()
	quoted := make([]string, len(literals))
	for i, lit := range literals {
		quoted[i] = `"` + lit + `"`
	}

	err = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			switch {
			case d.Name() == ".git", d.Name() == "bin", d.Name() == "dist":
				return filepath.SkipDir
			case path == scrubDir:
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		source := string(data)

		var found []string
		for i, q := range quoted {
			if strings.Contains(source, q) {
				found = append(found, literals[i])
			}
		}
		if len(found) > 0 {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s hardcodes scrub marker literal(s) %v: "+
				"the scrub deny list lives in exactly one place (internal/scrub); "+
				"reference scrub.Markers, scrub.WildcardPrefixes, or scrub.IsMarker instead",
				rel, found)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
}
