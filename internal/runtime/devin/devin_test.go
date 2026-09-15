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
	if d.ConformanceRecordDigest != devin.ConformanceRecordDigest {
		t.Fatalf("ConformanceRecordDigest = %q, want %s", d.ConformanceRecordDigest, devin.ConformanceRecordDigest)
	}
	if len(d.SupportedExternalVersions) != 1 || d.SupportedExternalVersions[0] != devin.PinnedExternalVersion {
		t.Fatalf("SupportedExternalVersions = %v, want [%s]", d.SupportedExternalVersions, devin.PinnedExternalVersion)
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

func TestFactoryProbeExistingBinarySupported(t *testing.T) {
	ctx := context.Background()
	var gotArgs []string
	f := devin.Factory{
		IntegrationInstanceID: "integration-1",
		Binary:                os.Args[0],
		VersionProbe: func(_ context.Context, binary string, args []string) ([]byte, error) {
			if binary != os.Args[0] {
				t.Fatalf("probe binary = %q, want %q", binary, os.Args[0])
			}
			gotArgs = append([]string(nil), args...)
			return []byte("devin 3000.10.21 (611c1cba)\n"), nil
		},
	}
	probe, err := f.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilitySupported {
		t.Fatalf("Compatibility = %s, want Supported", probe.Compatibility)
	}
	if probe.DetectedVersion != devin.PinnedExternalVersion {
		t.Fatalf("DetectedVersion = %q, want %q", probe.DetectedVersion, devin.PinnedExternalVersion)
	}
	if len(gotArgs) != 3 || gotArgs[0] != "--config" || gotArgs[2] != "--version" {
		t.Fatalf("version args = %v, want --config TEMP --version", gotArgs)
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
