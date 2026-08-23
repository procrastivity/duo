package herdr

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/adapter"
)

func testFactory(t *testing.T, f *fakeHerdr, digest string) Factory {
	t.Helper()
	return Factory{
		Config: Config{
			IntegrationInstanceID: testInstanceID,
			SocketPath:            f.socketPath(),
			CallTimeout:           time.Second,
			ResolveProcessBirth:   fakeBirth,
		},
		SchemaDigest: func(context.Context) (string, error) { return digest, nil },
	}
}

func TestDescriptorDeclaresTheHostRoleAndPin(t *testing.T) {
	d := Factory{}.Descriptor()
	if d.AdapterID != AdapterID || d.Role != adapter.RoleHost {
		t.Fatalf("descriptor = %+v", d)
	}
	if len(d.SupportedExternalVersions) != 1 || d.SupportedExternalVersions[0] != PinnedVersion {
		t.Fatalf("supported versions = %v, want only the pinned %s", d.SupportedExternalVersions, PinnedVersion)
	}
	if d.ConformanceRecordDigest == "" {
		t.Error("descriptor names no conformance record")
	}
}

// The pinned triple: version, protocol, and a live schema export whose
// digest matches. All three, or the verdict is not "supported".
func TestProbeSupportedOnThePinnedVersionAndDigest(t *testing.T) {
	f := newFakeHerdr(t)
	probe, err := testFactory(t, f, PinnedSchemaDigest).Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilitySupported {
		t.Fatalf("compatibility = %s, want supported", probe.Compatibility)
	}
	if probe.DetectedVersion != PinnedVersion {
		t.Fatalf("detected version = %q", probe.DetectedVersion)
	}
	if probe.ProtocolOrFormatIdentity != "herdr-socket-api/20" {
		t.Fatalf("protocol identity = %q", probe.ProtocolOrFormatIdentity)
	}
	if probe.FixtureOrSchemaDigest != PinnedSchemaDigest {
		t.Fatalf("schema digest = %q", probe.FixtureOrSchemaDigest)
	}
	if probe.ConnectionState != "connected" {
		t.Fatalf("connection state = %q", probe.ConnectionState)
	}
}

// An unknown newer version starts as unverified — the conformance stance,
// and exactly what 0.8.2 itself was against the pinned 0.7.5 record.
func TestProbeUnverifiedOnAnUnknownVersion(t *testing.T) {
	f := newFakeHerdr(t)
	f.setVersion("0.9.0", 21)
	probe, err := testFactory(t, f, "some-other-digest").Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnverified {
		t.Fatalf("compatibility = %s, want unverified", probe.Compatibility)
	}
}

// The sharper case: the version string still says 0.8.2 but the schema
// export has drifted. Digest drift under a stable version number is
// precisely what the 0.7.5 → 0.8.2 gap taught this project to catch.
func TestProbeUnverifiedOnSchemaDrift(t *testing.T) {
	f := newFakeHerdr(t)
	probe, err := testFactory(t, f, "0000000000000000000000000000000000000000000000000000000000000000").
		Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnverified {
		t.Fatalf("compatibility = %s, want unverified on digest drift", probe.Compatibility)
	}
}

// "I could not check" is not "it does not match", but it is also not
// supported: a probe that cannot obtain a digest reports unverified.
func TestProbeUnverifiedWhenNoSchemaExportIsAvailable(t *testing.T) {
	f := newFakeHerdr(t)
	factory := testFactory(t, f, "")
	factory.SchemaDigest = func(context.Context) (string, error) {
		return "", errors.New("herdr binary not found")
	}
	probe, err := factory.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnverified {
		t.Fatalf("compatibility = %s, want unverified", probe.Compatibility)
	}
	if probe.FixtureOrSchemaDigest != "" {
		t.Fatalf("schema digest = %q, want empty", probe.FixtureOrSchemaDigest)
	}
}

func TestProbeUnavailableWhenTheSocketDoesNotAnswer(t *testing.T) {
	factory := Factory{
		Config: Config{
			IntegrationInstanceID: testInstanceID,
			SocketPath:            filepath.Join(shortTempDir(t), "absent.sock"),
			CallTimeout:           200 * time.Millisecond,
		},
		SchemaDigest: func(context.Context) (string, error) { return PinnedSchemaDigest, nil },
	}
	probe, err := factory.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe returned an error instead of a verdict: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityUnavailable {
		t.Fatalf("compatibility = %s, want unavailable", probe.Compatibility)
	}
	if probe.ConnectionState != "unreachable" {
		t.Fatalf("connection state = %q", probe.ConnectionState)
	}
}

func TestProbeIncompatibleBelowTheProtocolFloor(t *testing.T) {
	f := newFakeHerdr(t)
	f.setVersion("0.4.0", MinimumKnownProtocol-1)
	probe, err := testFactory(t, f, PinnedSchemaDigest).Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != adapter.CompatibilityIncompatible {
		t.Fatalf("compatibility = %s, want incompatible", probe.Compatibility)
	}
}

// An unverified integration is one Duo records as unverified, not one it
// hides: New builds on it and carries the verdict.
func TestNewBuildsOnUnverifiedAndCarriesTheProbe(t *testing.T) {
	f := newFakeHerdr(t)
	f.setVersion("0.9.0", 21)
	factory := testFactory(t, f, "drifted")
	probe, err := factory.Probe(context.Background())
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	h, err := factory.New(context.Background(), probe)
	if err != nil {
		t.Fatalf("New on unverified: %v", err)
	}
	if h.Probe().Compatibility != adapter.CompatibilityUnverified {
		t.Fatalf("built host carries %s", h.Probe().Compatibility)
	}
}

func TestNewRefusesIncompatibleAndUnavailable(t *testing.T) {
	f := newFakeHerdr(t)
	factory := testFactory(t, f, PinnedSchemaDigest)
	for _, state := range []adapter.CompatibilityState{
		adapter.CompatibilityIncompatible,
		adapter.CompatibilityUnavailable,
	} {
		if _, err := factory.New(context.Background(), adapter.Probe{Compatibility: state}); err == nil {
			t.Errorf("New built an adapter for a %s integration", state)
		}
	}
}
