package amp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime/amp"
)

func TestFactoryDescriptor(t *testing.T) {
	d := amp.Factory{IntegrationInstanceID: "integration-1"}.Descriptor()
	if d.AdapterID != amp.AdapterID || d.Role != adapter.RoleRuntime {
		t.Fatalf("descriptor = %+v, want adapter id %q in the runtime role", d, amp.AdapterID)
	}
	if d.ConformanceRecordDigest != "amp-exclusive-writer-0.0.1789142434-g4f3b4d" {
		t.Fatalf("ConformanceRecordDigest = %q, want amp-exclusive-writer-0.0.1789142434-g4f3b4d", d.ConformanceRecordDigest)
	}
	if len(d.SupportedExternalVersions) != 1 || d.SupportedExternalVersions[0] != amp.PinnedExternalVersion {
		t.Fatalf("SupportedExternalVersions = %v, want [%s]", d.SupportedExternalVersions, amp.PinnedExternalVersion)
	}
	if d.DiagnosticRedactionPolicy == "" {
		t.Fatal("descriptor missing DiagnosticRedactionPolicy")
	}
}

func TestFactoryProbeMissingBinaryUnavailable(t *testing.T) {
	ctx := context.Background()
	f := amp.Factory{
		IntegrationInstanceID: "integration-1",
		Binary:                filepath.Join(t.TempDir(), "no-such-amp"),
	}
	probe, err := f.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnavailable {
		t.Fatalf("Compatibility = %s, want Unavailable for a missing binary", probe.Compatibility)
	}
	if _, err := f.New(ctx, probe); err == nil {
		t.Fatal("New with an Unavailable probe: want an error, got none")
	}
}

func TestFactoryProbeExistingBinaryUnverified(t *testing.T) {
	ctx := context.Background()
	f := amp.Factory{
		IntegrationInstanceID: "integration-1",
		Binary:                os.Args[0],
	}
	probe, err := f.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnverified {
		t.Fatalf("Compatibility = %s, want Unverified (Probe never execs --version)", probe.Compatibility)
	}
	if probe.ProtocolOrFormatIdentity != amp.ThreadIDFormatIdentity {
		t.Fatalf("ProtocolOrFormatIdentity = %q, want %q", probe.ProtocolOrFormatIdentity, amp.ThreadIDFormatIdentity)
	}
	r, err := f.New(ctx, probe)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r == nil {
		t.Fatal("New returned nil runtime")
	}
}
