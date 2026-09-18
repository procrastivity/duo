package registry_test

import (
	"slices"
	"testing"

	"github.com/procrastivity/duo/internal/registry"
)

func TestPortableInstallDescriptor(t *testing.T) {
	d, ok := registry.Lookup("projection.install")
	if !ok {
		t.Fatal("projection.install is not registered")
	}
	if d.Projectability != registry.LocalAdmin || !slices.Equal(d.CLI, []string{"install", "portable-launchers"}) {
		t.Fatalf("descriptor = %#v", d)
	}
	if !slices.Equal(d.Permissions, []string{"harness.manage"}) || d.MCPTool != "" || d.Route != nil {
		t.Fatalf("descriptor projections = %#v", d)
	}
	if d.Milestone {
		t.Fatal("projection.install must not expand the sealed dogfood-milestone operation set")
	}
	for _, code := range []string{"invalid.precondition", "projection.modified", "projection.incompatible", "projection.user_file_conflict"} {
		if !slices.Contains(d.ErrorCodes, code) {
			t.Errorf("descriptor missing %s", code)
		}
	}
}
