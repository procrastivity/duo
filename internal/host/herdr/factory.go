package herdr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"

	"github.com/procrastivity/duo/internal/adapter"
)

// The version pin this build was written against, from the 2026-08-23 P7
// probe session (notes/19-herdr-probes.md, review/05-close-report.md).
const (
	// PinnedVersion is the Herdr release every behavior in this package
	// was verified against.
	PinnedVersion = "0.8.2"
	// PinnedProtocol is that release's socket-API protocol number.
	PinnedProtocol = 20
	// PinnedSchemaDigest is the SHA-256 of `herdr api schema --json` at
	// 0.8.2, taken live during the probe session and re-verified while
	// this adapter was written. It is the fixture identity for the
	// compatibility verdict.
	PinnedSchemaDigest = "c48f1f54ee0150ca27e11fd44455fe94aeadb20fdf4e4a62393ed822a4e5b150"
	// MinimumKnownProtocol is the oldest Herdr protocol this project has
	// ever observed (0.7.5, notes/05). Below it the wire vocabulary is
	// unknown territory rather than a drifted version, so the verdict is
	// incompatible instead of unverified.
	MinimumKnownProtocol = 17

	// conformanceRecordDigest identifies the evidence record this build
	// was validated against. Herdr has no digested conformance-record
	// artifact in this repository yet (that is a later step's work), so
	// this names the probe record instead of hashing one.
	conformanceRecordDigest = "notes/19-herdr-probes.md@2026-08-23"
)

// AdapterID is this factory's §5.1 adapter identity.
const AdapterID = "herdr"

// BuildVersion is this adapter implementation's own version, distinct from
// the Herdr version it talks to.
const BuildVersion = "stage1"

// Factory is the Herdr host's §5.1 adapter factory.
type Factory struct {
	// Config is the configuration every Host this factory builds gets.
	Config Config
	// SchemaDigest exports Herdr's API schema and returns its SHA-256.
	// Defaults to running the herdr binary. A factory whose digest source
	// fails does not fail the probe: it downgrades the verdict to
	// unverified, because "I could not check" is not "it does not match".
	SchemaDigest func(context.Context) (string, error)
}

var _ adapter.Factory[*Host] = Factory{}

// Descriptor implements adapter.Factory.
func (f Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID:                 AdapterID,
		Role:                      adapter.RoleHost,
		BuildVersion:              BuildVersion,
		SupportedExternalVersions: []string{PinnedVersion},
		ConformanceRecordDigest:   conformanceRecordDigest,
		DiagnosticRedactionPolicy: "host-default",
	}
}

// Probe implements adapter.Factory: reach the socket, read ping, compare a
// live schema export against the pin, and report one verdict.
//
// An unreachable server is a verdict (unavailable), not an error — §5.1
// wants the probe recorded either way. A transport error that is not a
// dial failure is returned as an error, because it means the socket
// answered with something this adapter cannot parse.
func (f Factory) Probe(ctx context.Context) (adapter.Probe, error) {
	h, err := New(f.Config)
	if err != nil {
		return adapter.Probe{}, err
	}
	pong, err := h.ping(ctx)
	if err != nil {
		return adapter.Probe{
			ConnectionState: "unreachable",
			Compatibility:   adapter.CompatibilityUnavailable,
		}, nil
	}

	probe := adapter.Probe{
		DetectedVersion:          pong.Version,
		ProtocolOrFormatIdentity: fmt.Sprintf("herdr-socket-api/%d", pong.Protocol),
		ConnectionState:          "connected",
	}
	digest, digestErr := f.schemaDigest(ctx)
	if digestErr == nil {
		probe.FixtureOrSchemaDigest = digest
	}
	probe.Compatibility = compatibility(pong.Version, pong.Protocol, probe.FixtureOrSchemaDigest)
	return probe, nil
}

// compatibility is the version rule, spelled out rather than encoded in a
// version-range string: every one of version, protocol, and schema digest
// has to match the pin for a supported verdict. A drifted digest on the
// pinned version number is exactly the drift the 0.7.5 → 0.8.2 gap taught
// this project to catch, so it downgrades too.
func compatibility(version string, protocol int, digest string) adapter.CompatibilityState {
	if protocol < MinimumKnownProtocol {
		return adapter.CompatibilityIncompatible
	}
	if version == PinnedVersion && protocol == PinnedProtocol && digest == PinnedSchemaDigest {
		return adapter.CompatibilitySupported
	}
	return adapter.CompatibilityUnverified
}

// New implements adapter.Factory. It refuses to build on a verdict that
// says this adapter cannot speak to the server at all, and builds happily
// on unverified: an unverified integration is one Duo records as
// unverified, not one it hides.
func (f Factory) New(_ context.Context, probe adapter.Probe) (*Host, error) {
	switch probe.Compatibility {
	case adapter.CompatibilityIncompatible:
		return nil, fmt.Errorf("herdr: refusing to build an adapter for incompatible %s (protocol below %d)",
			probe.DetectedVersion, MinimumKnownProtocol)
	case adapter.CompatibilityUnavailable:
		return nil, fmt.Errorf("herdr: refusing to build an adapter for an unreachable server at %s",
			f.Config.SocketPath)
	case adapter.CompatibilitySupported, adapter.CompatibilityUnverified:
	}
	h, err := New(f.Config)
	if err != nil {
		return nil, err
	}
	h.probe = probe
	return h, nil
}

func (f Factory) schemaDigest(ctx context.Context) (string, error) {
	if f.SchemaDigest != nil {
		return f.SchemaDigest(ctx)
	}
	return schemaDigestFromBinary(ctx, f.Config.Binary)
}

// schemaDigestFromBinary runs `herdr api schema --json` and hashes its
// output. The exported schema is the only machine-readable statement of
// what a given Herdr build's API is, and its digest is what the pinned
// record compares against.
func schemaDigestFromBinary(ctx context.Context, binary string) (string, error) {
	if binary == "" {
		binary = "herdr"
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, binary, "api", "schema", "--json")
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("herdr: export api schema: %w", err)
	}
	sum := sha256.Sum256(out.Bytes())
	return hex.EncodeToString(sum[:]), nil
}
