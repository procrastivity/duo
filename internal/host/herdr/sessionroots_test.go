package herdr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/host/herdr"
)

// writeSessionFile drops a session.json at
// $XDG_CONFIG_HOME/herdr/sessions/<session>/session.json, the same layout
// seedSession builds the socket in.
func writeSessionFile(t *testing.T, root, session, body string) {
	t.Helper()
	dir := filepath.Join(root, herdr.ConfigDirName, herdr.SessionsDirName, session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	path := filepath.Join(dir, herdr.SessionFileName)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
}

// TestSessionRootsReturnsTheProjectDirectory pins the source the cwd
// correlation rung reads: version-3 session.json, one workspace, one
// project identity_cwd.
func TestSessionRootsReturnsTheProjectDirectory(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", home)

	proj := filepath.Join(home, "Code", "proj")
	writeSessionFile(t, root, "work", `{"version":3,"workspaces":[{"identity_cwd":"`+proj+`"}]}`)

	got, err := herdr.SessionRoots("work")
	if err != nil {
		t.Fatalf("SessionRoots: %v", err)
	}
	if len(got) != 1 || got[0] != proj {
		t.Fatalf("roots = %v, want [%s]", got, proj)
	}
}

// TestSessionRootsFiltersGenericRoots covers Herdr's no-project fallback:
// a session started without a project directory claims the filesystem
// root, the home directory, or an ancestor of it, none of which is
// identity — left in, any of them would claim every directory under home.
func TestSessionRootsFiltersGenericRoots(t *testing.T) {
	root := t.TempDir()
	homeParent := t.TempDir()
	home := filepath.Join(homeParent, "dev")
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", home)

	writeSessionFile(t, root, "generic", `{"version":3,"workspaces":[
		{"identity_cwd":"/"},
		{"identity_cwd":"`+home+`"},
		{"identity_cwd":"`+homeParent+`"}
	]}`)

	got, err := herdr.SessionRoots("generic")
	if err != nil {
		t.Fatalf("SessionRoots: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("roots = %v, want none", got)
	}
}

// TestSessionRootsSortsAndDedupesMultipleWorkspaces: several workspaces in
// one session can name the same project directory twice (a second pane
// opened into it); the reader dedupes and returns the distinct roots
// sorted.
func TestSessionRootsSortsAndDedupesMultipleWorkspaces(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", home)

	first := filepath.Join(home, "Code", "zeta")
	second := filepath.Join(home, "Code", "alpha")
	writeSessionFile(t, root, "multi", `{"version":3,"workspaces":[
		{"identity_cwd":"`+first+`"},
		{"identity_cwd":"`+second+`"},
		{"identity_cwd":"`+first+`"}
	]}`)

	got, err := herdr.SessionRoots("multi")
	if err != nil {
		t.Fatalf("SessionRoots: %v", err)
	}
	want := []string{second, first}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("roots = %v, want %v (sorted, deduped)", got, want)
	}
}

// TestSessionRootsIsBestEffort: a missing, malformed, or foreign-version
// session.json is (nil, nil) — no roots for that session is an answer, and
// a broken metadata file must not fail a materialization.
func TestSessionRootsIsBestEffort(t *testing.T) {
	root := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("HOME", home)

	writeSessionFile(t, root, "malformed", `not json`)
	writeSessionFile(t, root, "old-version", `{"version":2,"workspaces":[{"identity_cwd":"`+filepath.Join(home, "Code", "proj")+`"}]}`)

	cases := []string{"missing", "malformed", "old-version"}
	for _, session := range cases {
		t.Run(session, func(t *testing.T) {
			got, err := herdr.SessionRoots(session)
			if err != nil {
				t.Fatalf("SessionRoots(%q): %v", session, err)
			}
			if got != nil {
				t.Errorf("roots = %v, want nil", got)
			}
		})
	}
}
