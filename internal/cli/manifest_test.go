package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/registry"
)

// TestManifestCommand_JSON runs `duo manifest --json` through the same
// Execute path main.go uses, and checks the emitted document's shape and
// exit code — a through-the-built-root-command exercise, not just a direct
// call into internal/manifest.Build.
func TestManifestCommand_JSON(t *testing.T) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"manifest", "--json"})

	code := Execute(root, streams)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut.String())
	}

	var doc struct {
		Schema         string `json:"schema"`
		ManifestDigest string `json:"manifest_digest"`
		Operations     []struct {
			Name string `json:"name"`
		} `json:"operations"`
	}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	if doc.Schema != "duo.manifest/v1" {
		t.Errorf("schema = %q, want duo.manifest/v1", doc.Schema)
	}
	if doc.ManifestDigest == "" {
		t.Error("manifest_digest is empty")
	}
	if len(doc.Operations) == 0 {
		t.Error("operations is empty")
	}
}

// TestManifestCommand_Human runs `duo manifest` (no --json) and checks the
// human-mode line lands on stdout with no error.
func TestManifestCommand_Human(t *testing.T) {
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"manifest"})

	code := Execute(root, streams)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut.String())
	}
	if out.Len() == 0 {
		t.Error("human-mode output is empty")
	}
}

// TestManifestCommand_CLIPathMatchesRegistry pins that `duo manifest` is
// registered at exactly the CLI path internal/registry's "manifest.show"
// row declares — the registration this row's metadata describes.
func TestManifestCommand_CLIPathMatchesRegistry(t *testing.T) {
	d, ok := registry.Lookup("manifest.show")
	if !ok {
		t.Fatal(`"manifest.show" is not registered`)
	}

	root := NewRootCommand(iostreams.System(), buildinfo.Info{})
	cmd, _, err := root.Find(d.CLI)
	if err != nil {
		t.Fatalf("root.Find(%v): %v", d.CLI, err)
	}
	if cmd.Name() != "manifest" {
		t.Errorf("resolved command %q, want %q", cmd.Name(), "manifest")
	}
}
