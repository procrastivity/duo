package materialize

import (
	"context"
	"sort"

	"github.com/procrastivity/duo/internal/domain"
)

// CaptureTimeLayout is the stamp format every ambient capture carries. It
// is the domain's own layout (internal/domain/authority.go) verbatim —
// fixed width, UTC, millisecond — so a capture recorded here and a fact
// stamped by the kernel sort against each other as plain strings, and both
// satisfy duo.external/v1's `timestamp` (RFC 3339 date-time).
const CaptureTimeLayout = "2006-01-02T15:04:05.000Z"

// The `session_hosts.deduce` source names, which are the closed set the v3
// schema fixes. Each names one rung of the fixed ranking *below* the
// explicit flag, which has no deduce key: a flag the operator typed is not
// a deduction, and installed policy never switches it off.
const (
	// DeduceWorkspace enables the workspace↔host-correlation rung.
	DeduceWorkspace = "workspace"
	// DeduceEnv enables the ambient-environment rung.
	DeduceEnv = "env"
	// DeduceDefault enables the policy-default rung.
	DeduceDefault = "default"
)

// Rungs is the fixed deduction ranking, in rank order. It is a constant of
// this package, not a configurable: `session_hosts.deduce` decides which of
// these are consulted and never in what order (notes/42 §3.2).
var Rungs = []domain.HostSource{
	domain.HostSourceExplicitFlag,
	domain.HostSourceWorkspaceCorrelation,
	domain.HostSourceAmbientEnv,
	domain.HostSourcePolicyDefault,
}

// DeduceSourceFor returns the `session_hosts.deduce` key that switches a
// rung on or off, and "" for the explicit-flag rung, which has none.
func DeduceSourceFor(s domain.HostSource) string {
	switch s {
	case domain.HostSourceWorkspaceCorrelation:
		return DeduceWorkspace
	case domain.HostSourceAmbientEnv:
		return DeduceEnv
	case domain.HostSourcePolicyDefault:
		return DeduceDefault
	case domain.HostSourceExplicitFlag:
		return ""
	default:
		return ""
	}
}

// --- read models, as narrow interfaces -----------------------------------

// CorrelationSource is the workspace↔host read model M1 consults at the
// `workspace-correlation` rung, narrowed to the two methods this package
// calls. *domain.Authority satisfies it; a test fake satisfies it without a
// store.
type CorrelationSource interface {
	// WorkspaceForRoot maps a normalized root path to its workspace.
	WorkspaceForRoot(root string) (domain.Workspace, bool)
	// HostCorrelation returns one workspace's current binding.
	HostCorrelation(domain.WorkspaceID) (domain.HostCorrelation, bool)
}

// ProviderSource is the standing-provider read model M2 snapshots, narrowed
// to the one method this package calls. *domain.Authority satisfies it.
type ProviderSource interface {
	// StandingProviderFacts maps a provider name to its latest standing.
	StandingProviderFacts() map[string]domain.ProviderStanding
}

// Instance is one discovered session-host instance of a known kind.
//
// Locator is the value that addresses the instance — for Herdr, the API
// socket path — and is what `<kind>:<instance>` renders. InstanceID is the
// Duo integration-instance ID for the same host when the discoverer has
// one; it is optional, because the locator is what addresses the host and
// the ID is only what evidence is scoped by.
type Instance struct {
	Locator    string
	InstanceID string
}

// InstanceDiscovery enumerates the live instances of one session-host kind.
//
// This is deliberately *not* host.HostDiscovery. That contract (§5.2)
// discovers transient runtime candidates *inside* one already-addressed
// host — building a herdr.Host needs a socket path before Discover can be
// called at all — so it cannot answer "which instances of kind K exist",
// which is the only question the policy-default rung asks. The
// implementation is injected: this package holds no adapter knowledge.
//
// An implementation must never dial or otherwise prove an instance is
// reachable (I-3); enumerating is not attesting.
type InstanceDiscovery interface {
	DiscoverInstances(ctx context.Context, kind string) ([]Instance, error)
}

// --- ambient environment --------------------------------------------------

// AmbientSource is one host kind's ambient-environment signature: the
// variables a host server publishes into the panes it owns, which is how a
// Duo running *inside* a session host can tell which host that is.
//
// LocatorVar carries the instance locator and is what makes the rung yield:
// with no locator there is no instance to address. InstanceVar carries the
// host's own session identity, which is folded into an integration-instance
// ID with InstanceIDPrefix.
type AmbientSource struct {
	// Kind is the session-host kind these variables belong to.
	Kind string
	// LocatorVar is the variable holding the instance locator.
	LocatorVar string
	// InstanceVar is the variable holding the host's session identity, or
	// "" for a kind that publishes none.
	InstanceVar string
	// InstanceIDPrefix is prepended to InstanceVar's value to build the
	// integration-instance ID.
	InstanceIDPrefix string
}

// DefaultAmbientSources is the ambient signature of every session-host kind
// this build knows. Herdr is the only one: Stage 1 has no Solo or tmux
// adapter, and nothing in this repository publishes a Solo ambient variable
// (the name would be invented here, not observed, so none is listed).
//
// The Herdr row is `herdr.InstanceIDForSession`'s convention spelled out
// rather than imported: a launch-layer package that reaches into an adapter
// for a string constant would make every future host kind an import edge.
// TestAmbientHerdrRowMatchesTheAdapterConvention pins the two together.
var DefaultAmbientSources = []AmbientSource{
	{
		Kind:             "herdr",
		LocatorVar:       "HERDR_SOCKET_PATH",
		InstanceVar:      "HERDR_SESSION",
		InstanceIDPrefix: "herdr:",
	},
}

// AmbientVariableNames lists, in read order and without repeats, every
// variable the ambient rung reads for sources. It is the exactly-once read
// order made inspectable, and what `duo doctor` names when it explains what
// M1 would look at.
func AmbientVariableNames(sources []AmbientSource) []string {
	seen := map[string]bool{}
	out := make([]string, 0, 2*len(sources))
	for _, s := range sources {
		for _, name := range []string{s.LocatorVar, s.InstanceVar} {
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	return out
}

// AmbientCapture is one ambient variable read once at materialization and
// recorded whether or not its rung won. duo.external/v1's
// `launch_ambient_capture`.
type AmbientCapture struct {
	Name       string
	Value      string
	CapturedAt string
}

// --- deduction output -----------------------------------------------------

// DeducedHost is the single host instance M1 deduced, with the rung that
// produced it. duo.external/v1's `launch_deduced_host`: Kind is `kind`,
// Instance is `instance_label`, InstanceID is `instance_id`, Source is
// `host_source`, Workspace is `workspace_id`.
type DeducedHost struct {
	// Kind is the session-host kind, never an instance.
	Kind string
	// Instance is the instance locator.
	Instance string
	// InstanceID is the integration-instance ID, when one is known.
	InstanceID string
	// Source is the rung that produced this instance.
	Source domain.HostSource
	// Workspace is the workspace the deduction was made for, "" when the
	// path is not enrolled.
	Workspace domain.WorkspaceID
	// Fingerprint is the correlation's evidence set, carried only when the
	// `workspace-correlation` rung won. Nothing else here has one:
	// fingerprints are proven, not deduced.
	Fingerprint domain.HostFingerprint
}

// Locator renders `<kind>:<instance>` — the same form the audited rebind
// verb's target flag accepts and `--host` parses, so what an operator reads
// back is what they would type.
func (h DeducedHost) Locator() string { return h.Kind + ":" + h.Instance }

// Present reports whether a host was deduced at all.
func (h DeducedHost) Present() bool { return h.Kind != "" && h.Instance != "" }

// OutrankedEvidence is host evidence that was captured and then beaten by a
// higher rung. duo.external/v1's `launch_outranked_evidence`.
//
// It exists so a wrong binding is visible rather than silent: a stale
// correlation that outranks the pane the operator is standing in shows up
// here, next to the pointer at the rebind verb (workplan Risk 2).
type OutrankedEvidence struct {
	Source   domain.HostSource
	Kind     string
	Instance string
	FactID   domain.FactID
	Captures []AmbientCapture
	Detail   string
}

func (e OutrankedEvidence) clone() OutrankedEvidence {
	e.Captures = cloneCaptures(e.Captures)
	return e
}

// DeductionRung is one rung's outcome. duo.external/v1's
// `launch_host_deduction_rung`.
//
// Consulted is false for a rung that was skipped: no flag was supplied, the
// rung is switched off in `session_hosts.deduce`, or a higher rung already
// produced the host and this rung would have cost I/O to ask.
type DeductionRung struct {
	Source      domain.HostSource
	Consulted   bool
	YieldedHost bool
	Kind        string
	Instance    string
	Detail      string
}

// EvidenceBundle is the immutable M1+M2 output the resolver reads.
//
// Immutable is the point, not a nicety: the resolver's purity (I-3) rests
// on it being handed facts that cannot change under it, and the record
// cites the same fact IDs afterwards. Every field is unexported and every
// accessor returns a copy, so a caller that mutates what it got back
// mutates its own copy.
type EvidenceBundle struct {
	correlationFactID      domain.FactID
	correlationFingerprint domain.HostFingerprint
	hasCorrelation         bool
	ambient                []AmbientCapture
	providers              map[string]domain.ProviderStanding
}

// CorrelationFactID returns the fact that recorded the workspace↔host
// correlation consulted at materialization, and whether there was one. The
// ID is present whenever a correlation existed, including when a higher
// rung outranked it — the record cites what was consulted, not only what
// won.
func (b EvidenceBundle) CorrelationFactID() (domain.FactID, bool) {
	return b.correlationFactID, b.hasCorrelation
}

// CorrelationFingerprint returns the correlated instance's evidence set,
// and whether there was a correlation.
func (b EvidenceBundle) CorrelationFingerprint() (domain.HostFingerprint, bool) {
	return b.correlationFingerprint, b.hasCorrelation
}

// AmbientCaptures returns every ambient variable read at materialization,
// in read order, as a copy.
func (b EvidenceBundle) AmbientCaptures() []AmbientCapture {
	return cloneCaptures(b.ambient)
}

// ProviderStanding returns one provider's standing fact, and whether the
// snapshot holds one. A name absent from the snapshot has no standing fact
// at all, which by the kernel's rule means enabled — the default is applied
// by the reader, here as in domain.Authority.StandingProviderFacts.
func (b EvidenceBundle) ProviderStanding(name string) (domain.ProviderStanding, bool) {
	st, ok := b.providers[name]
	return st, ok
}

// ProviderDisabled reports whether name is standing-disabled, with the fact
// ID that disabled it. It is the exact question step 3 of resolution asks
// before raising the `provider_disabled` elimination, answered against the
// snapshot rather than against a live read.
func (b EvidenceBundle) ProviderDisabled(name string) (domain.FactID, bool) {
	st, ok := b.providers[name]
	if !ok || st.Enabled {
		return "", false
	}
	return st.FactID, true
}

// ProviderStandings returns the whole snapshot as a copy.
func (b EvidenceBundle) ProviderStandings() map[string]domain.ProviderStanding {
	out := make(map[string]domain.ProviderStanding, len(b.providers))
	for name, st := range b.providers {
		out[name] = st
	}
	return out
}

// ProviderFactIDs returns every snapshotted fact ID, sorted, as a copy.
// This is `launch_evidence_bundle.provider_fact_ids`: what the launch
// record cites so a later reader can replay the exact provider state the
// resolution rested on.
func (b EvidenceBundle) ProviderFactIDs() []domain.FactID {
	out := make([]domain.FactID, 0, len(b.providers))
	for _, st := range b.providers {
		if st.FactID != "" {
			out = append(out, st.FactID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Result is one materialization: what M1 deduced, what it outranked, the
// trail it walked, and the bundle M2 completed. Like EvidenceBundle it is
// read-only through its accessors.
type Result struct {
	workspacePath string
	workspaceID   domain.WorkspaceID
	host          DeducedHost
	outranked     []OutrankedEvidence
	trail         []DeductionRung
	bundle        EvidenceBundle
}

// WorkspacePath returns the resolved workspace path: --workspace when it
// was given, else the working directory.
func (r Result) WorkspacePath() string { return r.workspacePath }

// WorkspaceID returns the enrolled workspace for that path, or "" when the
// path is not enrolled. A launch into an unenrolled path is normal — the
// launch record's own commit is what enrolls it.
func (r Result) WorkspaceID() domain.WorkspaceID { return r.workspaceID }

// Host returns the deduced host instance.
func (r Result) Host() DeducedHost { return r.host }

// OutrankedEvidence returns every captured-but-beaten piece of evidence, in
// rank order, as a copy.
func (r Result) OutrankedEvidence() []OutrankedEvidence {
	out := make([]OutrankedEvidence, 0, len(r.outranked))
	for _, e := range r.outranked {
		out = append(out, e.clone())
	}
	return out
}

// Trail returns all four rungs in rank order, as a copy. Every rung is
// present on every materialization, successful or not: the trail is the
// explanation, and an explanation with holes in it is not one.
func (r Result) Trail() []DeductionRung {
	return append([]DeductionRung(nil), r.trail...)
}

// Bundle returns the immutable evidence bundle handed to the resolver.
func (r Result) Bundle() EvidenceBundle { return r.bundle }

func cloneCaptures(in []AmbientCapture) []AmbientCapture {
	if in == nil {
		return nil
	}
	return append([]AmbientCapture(nil), in...)
}
