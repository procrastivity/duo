package manifest

import (
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/registry"
	"github.com/procrastivity/duo/internal/surface"
)

// TestBuildOperationsComeFromRegistry pins the wiring: the manifest's
// operations array is internal/registry's derived view, byte for byte —
// Build maintains no operation table of its own.
func TestBuildOperationsComeFromRegistry(t *testing.T) {
	root := &cobra.Command{Use: "duo"}
	verb := &cobra.Command{Use: "noop", Short: "test verb", RunE: func(*cobra.Command, []string) error { return nil }}
	surface.Annotate(verb, surface.Plumbing)
	root.AddCommand(verb)

	m, err := Build(root, buildinfo.Info{Version: "test", Commit: "test", Date: "test"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !reflect.DeepEqual(m.Operations, registry.ManifestOperations()) {
		t.Error("manifest operations differ from registry.ManifestOperations() — the manifest must not keep its own operation table")
	}
	if len(m.Operations) == 0 {
		t.Error("manifest carries no operations; the registry is not empty")
	}
}
