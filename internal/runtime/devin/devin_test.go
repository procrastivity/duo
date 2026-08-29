package devin_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime/devin"
)

func TestFactoryDescriptor(t *testing.T) {
	d := devin.Factory{IntegrationInstanceID: "integration-1"}.Descriptor()
	if d.AdapterID != "devin" || d.Role != adapter.RoleRuntime {
		t.Fatalf("descriptor = %+v, want adapter id devin in the runtime role", d)
	}
	if d.ConformanceRecordDigest != "notes59-devin-3000.6.7" {
		t.Fatalf("ConformanceRecordDigest = %q, want notes59-devin-3000.6.7", d.ConformanceRecordDigest)
	}
	if d.DiagnosticRedactionPolicy == "" {
		t.Fatal("descriptor missing DiagnosticRedactionPolicy")
	}
}

func TestFactoryProbeMissingBinaryUnavailable(t *testing.T) {
	ctx := context.Background()
	f := devin.Factory{
		IntegrationInstanceID: "integration-1",
		Binary:                filepath.Join(t.TempDir(), "no-such-devin"),
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
	f := devin.Factory{
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
	if probe.ProtocolOrFormatIdentity != devin.SessionIDFormatIdentity {
		t.Fatalf("ProtocolOrFormatIdentity = %q, want %q", probe.ProtocolOrFormatIdentity, devin.SessionIDFormatIdentity)
	}
	r, err := f.New(ctx, probe)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r == nil {
		t.Fatal("New returned nil runtime")
	}
}
