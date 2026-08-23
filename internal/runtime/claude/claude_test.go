package claude_test

import (
	"context"
	"testing"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime/claude"
)

func TestFactoryDescriptorAndProbeReachable(t *testing.T) {
	ctx := context.Background()
	f := claude.Factory{IntegrationInstanceID: "integration-1", ClaudeDir: "testdata"}

	d := f.Descriptor()
	if d.AdapterID == "" || d.Role != adapter.RoleRuntime {
		t.Fatalf("descriptor missing AdapterID or wrong Role: %+v", d)
	}

	probe, err := f.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnverified {
		t.Fatalf("Compatibility = %s, want Unverified (Probe never confirms a live version this step)", probe.Compatibility)
	}

	r, err := f.New(ctx, probe)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r == nil {
		t.Fatalf("New returned nil runtime")
	}
}

func TestFactoryProbeUnreachableRefusesNew(t *testing.T) {
	ctx := context.Background()
	f := claude.Factory{IntegrationInstanceID: "integration-1", ClaudeDir: "testdata/does-not-exist"}

	probe, err := f.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnavailable {
		t.Fatalf("Compatibility = %s, want Unavailable for a missing claude dir", probe.Compatibility)
	}

	if _, err := f.New(ctx, probe); err == nil {
		t.Fatalf("New with an Unavailable probe: want an error, got none")
	}
}
