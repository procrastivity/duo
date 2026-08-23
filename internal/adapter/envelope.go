// Package adapter holds the architecture's §5.1 shared adapter envelope:
// the declaration and probe shapes every session-host and agent-runtime
// adapter factory carries, independent of which of the two contract
// families (§5.2, §5.3) the factory's product implements.
//
// This package is deliberately neutral. internal/host and internal/runtime
// both import it; it imports neither, and neither of them imports the
// other. The §5.4 mechanical proof of that last rule lives here too
// (roleseparation_test.go), because it has to sit somewhere that is not
// itself "host" or "runtime" to check both without becoming a false
// positive for the very rule it enforces.
package adapter

import "context"

// Role names which of the two adapter contract families (§5.2 session-host,
// §5.3 agent-runtime) a factory's product implements. A given AdapterID
// never changes Role across builds.
type Role string

// The two adapter roles the architecture currently defines. A future
// protocol-owned role (§7) is a third integration role but not a
// session-host or agent-runtime adapter, so it does not get a Role value
// here; it is out of this step's scope.
const (
	RoleHost    Role = "host"
	RoleRuntime Role = "runtime"
)

// Descriptor is the §5.1 shared adapter envelope: what every adapter
// factory declares about itself before it is asked to probe or build
// anything.
type Descriptor struct {
	// AdapterID names the integration this factory builds adapters for
	// (e.g. "herdr", "fake-host"). Stable across builds.
	AdapterID string
	// Role is host or runtime; see the Role doc comment.
	Role Role
	// BuildVersion is this adapter implementation's own version, distinct
	// from the external integration's version, which is a Probe's
	// DetectedVersion.
	BuildVersion string
	// SupportedExternalVersions lists the version-rule strings this build
	// declares compatibility with. §5.1 requires the envelope to carry
	// "supported external-version rules" but does not fix a rule syntax;
	// this package carries the declared strings opaquely and leaves
	// interpretation to the adapter composer's future version-rule
	// evaluator.
	SupportedExternalVersions []string
	// ConformanceRecordDigest identifies the exact conformance record this
	// build was validated against.
	ConformanceRecordDigest string
	// DiagnosticRedactionPolicy names which redaction policy this
	// adapter's diagnostics must go through (§9: credentials, prompt
	// content, protected collaboration content, and unapproved paths are
	// never logged in the clear).
	DiagnosticRedactionPolicy string
}

// CompatibilityState is a probe's one compatibility verdict. §5.1: "A probe
// does not publish live Duo operation support by itself" — Supported means
// only that this build and the detected external version are compatible,
// not that any operation is currently reachable.
type CompatibilityState string

// The four compatibility states §5.1 names.
const (
	CompatibilitySupported    CompatibilityState = "supported"
	CompatibilityUnverified   CompatibilityState = "unverified"
	CompatibilityIncompatible CompatibilityState = "incompatible"
	CompatibilityUnavailable  CompatibilityState = "unavailable"
)

// Probe is what a factory returns when asked to examine an integration
// instance, before it creates an active adapter.
type Probe struct {
	// DetectedVersion is the external integration's own reported version,
	// not this adapter build's version.
	DetectedVersion string
	// ProtocolOrFormatIdentity names the wire protocol or on-disk format
	// the probe observed (e.g. a socket protocol name, a transcript
	// format tag).
	ProtocolOrFormatIdentity string
	// ConnectionState is a short adapter-defined status string (e.g.
	// "connected", "unreachable"); its vocabulary is per adapter family.
	ConnectionState string
	// FixtureOrSchemaDigest identifies the fixture or schema the probe
	// matched against, when the integration exposes one. Empty when no
	// such evidence was available.
	FixtureOrSchemaDigest string
	// Compatibility is the probe's one compatibility verdict.
	Compatibility CompatibilityState
}

// Factory is the shared adapter-factory shape from §5.1: declare a
// Descriptor, probe an integration instance, and only then build an active
// adapter of type A. Host and runtime factories share this shape without
// sharing a package — A is internal/host's or internal/runtime's own
// adapter type, so instantiating this generic interface never requires
// either package to import the other.
type Factory[A any] interface {
	// Descriptor returns this factory's fixed §5.1 envelope.
	Descriptor() Descriptor
	// Probe examines one integration instance and reports what it found,
	// without creating an active adapter.
	Probe(context.Context) (Probe, error)
	// New builds an active adapter of type A from a prior Probe result.
	New(context.Context, Probe) (A, error)
}
