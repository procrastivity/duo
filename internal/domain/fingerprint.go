package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// EpochScope says how far a host's epoch-equivalent identity reaches.
//
// decision-01 §4.2 asks a terminal-hosted fingerprint for "the host server
// epoch or equivalent server identity". The Herdr 0.8.2 probe (notes/19 §5,
// review/05-close-report.md §1) refuted the literal reading: Herdr exposes
// no server-scoped epoch on any surface, and its epoch-equivalent is the
// per-pane `terminal_id`, which changes when a restored pane keeps its
// `pane_id`. So the domain requires *an* epoch-equivalent and records the
// scope it was proven at, instead of requiring a server-scoped one.
type EpochScope string

// The epoch scopes an integration can prove.
const (
	// EpochScopeServer is a host-server-wide epoch, such as a tmux server
	// start identity: one value covers every container on that server.
	EpochScopeServer EpochScope = "server"
	// EpochScopePane is a per-container epoch that distinguishes
	// incarnations of one container, such as Herdr's terminal_id. It is
	// weaker in reach but equally distinguishing for one container, which
	// is all the claim key needs.
	EpochScopePane EpochScope = "pane"
)

// HostEpoch is a host-specific epoch-equivalent identity: whatever value the
// session-host integration can prove changes when the thing behind a stable
// container coordinate is a new incarnation.
type HostEpoch struct {
	// Kind names the surface the value came from, scoped by integration —
	// "tmux.server-epoch", "herdr.terminal_id". It keeps two integrations'
	// values from colliding in the claim key.
	Kind string
	// Value is the opaque epoch value.
	Value string
	// Scope is the reach Kind was proven at.
	Scope EpochScope
}

// Valid reports whether the epoch carries enough to distinguish incarnations.
func (e HostEpoch) Valid() bool {
	return e.Kind != "" && e.Value != "" && (e.Scope == EpochScopeServer || e.Scope == EpochScopePane)
}

// ProcessBirth is the process-birth identity of one agent execution.
// decision-01 §3.6: "a process-birth tuple, such as boot or host epoch, PID,
// and start time. PID alone is insufficient" — so a PID with no start time
// does not count as present, and the type has no way to say otherwise.
type ProcessBirth struct {
	// Host scopes the PID namespace (a boot ID, a container ID, a host
	// name). Optional: the integration instance already scopes the
	// fingerprint, and no host surface probed so far reports a boot ID.
	Host string
	// PID is the process ID.
	PID int
	// StartedAt is the kernel-reported process start time. It is what makes
	// a reused PID a different process.
	StartedAt string
	// Executable is the resolved program, when the host reports it.
	Executable string
}

// Present reports whether process-birth evidence is complete enough to be
// part of a claim. decision-01 §4.2 includes it "when it is available"; this
// is the test for available.
func (p ProcessBirth) Present() bool { return p.PID > 0 && p.StartedAt != "" }

// Fingerprint is the strongest available live-runtime identity for a
// terminal-hosted candidate (decision-01 §4.2).
//
// It deliberately has no working-directory, command-name, agent-name, or
// transcript-modification-time field. Those are "never sufficient" there, so
// the type refuses to carry them at all: they live on Candidate.Hints, which
// nothing in the claim path reads.
type Fingerprint struct {
	// IntegrationInstance is the configured session-host integration
	// instance — not the integration *kind*. Two configured Herdr servers
	// are two instances and never share a claim.
	IntegrationInstance string
	// Epoch is the host's epoch-equivalent identity.
	Epoch HostEpoch
	// Container is the exact host container, such as a pane ID.
	Container string
	// Process is the process-birth identity, when the host reports it.
	Process ProcessBirth
}

// ErrWeakFingerprint reports evidence too weak to claim a live runtime.
// decision-01 §4.2's floor: without an integration instance, an
// epoch-equivalent, and an exact container, Duo cannot tell two live
// runtimes apart, and §4.3 forbids guessing.
var ErrWeakFingerprint = errors.New("domain: fingerprint cannot distinguish a live runtime")

// Validate reports whether the fingerprint may be claimed.
func (f Fingerprint) Validate() error {
	var missing []string
	if f.IntegrationInstance == "" {
		missing = append(missing, "session-host integration instance")
	}
	if !f.Epoch.Valid() {
		missing = append(missing, "host server epoch or equivalent")
	}
	if f.Container == "" {
		missing = append(missing, "exact host container")
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf("%w: missing %s", ErrWeakFingerprint, strings.Join(missing, ", "))
}

// Degraded reports a fingerprint that is claimable but carries no
// process-birth identity. Such a claim still separates two live runtimes —
// one container holds one foreground execution — but it cannot prove
// *continuity* of an execution across a Duo restart, so recovery treats it
// as unproven rather than as proof (see Authority.ResolveRecovery).
func (f Fingerprint) Degraded() bool { return !f.Process.Present() }

// ClaimKey is the stable digest of one exclusive claim's evidence.
type ClaimKey string

// ClaimKind separates the claim namespaces the active-claim index holds.
type ClaimKind string

// The claim kinds.
const (
	// ClaimLiveRuntime is the terminal-hosted live-runtime fingerprint from
	// decision-01 §4.2 — the primary claim.
	ClaimLiveRuntime ClaimKind = "live-runtime"
	// ClaimAgentSession is the agent-runtime session identity an
	// authenticated reporter claim adds. It is exclusive for the same
	// reason: §6.5 requires two same-directory sessions to hold distinct
	// agent-session correlations, and an overlap is a conflict, never a
	// merge.
	ClaimAgentSession ClaimKind = "agent-session"
)

// ClaimRef is one entry in the active-claim index: a kind plus its key. It is
// comparable, so it is usable directly as a map key.
type ClaimRef struct {
	Kind ClaimKind
	Key  ClaimKey
}

// String renders a claim reference for diagnostics and durable token IDs.
func (r ClaimRef) String() string { return string(r.Kind) + "/" + string(r.Key) }

// claimKeyVersion prefixes every digest. A future change to what a claim
// covers must change this, so old and new keys can never be confused for the
// same claim.
const claimKeyVersion = "v1"

// ClaimRef returns the primary live-runtime claim this fingerprint holds.
//
// The digest covers the integration instance, the epoch-equivalent (kind,
// scope, and value), the container, and the process birth when present. It
// deliberately does *not* cover an agent-runtime session ID or transcript:
// decision-01 §4.2 lets an authenticated report "strengthen the fingerprint"
// later, and a strengthening that moved the primary key would orphan the
// claim it is strengthening. Strengthening adds a second claim instead.
func (f Fingerprint) ClaimRef() ClaimRef {
	parts := []string{
		claimKeyVersion,
		string(ClaimLiveRuntime),
		f.IntegrationInstance,
		f.Epoch.Kind,
		string(f.Epoch.Scope),
		f.Epoch.Value,
		f.Container,
	}
	if f.Process.Present() {
		parts = append(parts,
			f.Process.Host,
			strconv.Itoa(f.Process.PID),
			f.Process.StartedAt,
			f.Process.Executable,
		)
	}
	return ClaimRef{Kind: ClaimLiveRuntime, Key: digest(parts)}
}

// AgentSessionRef is an agent-runtime session identity, scoped to the
// agent-runtime integration instance that reported it.
type AgentSessionRef struct {
	// IntegrationInstance is the configured agent-runtime integration
	// instance. decision-01 §3.6: an agent-runtime session ID is "scoped by
	// runtime integration and installation or profile".
	IntegrationInstance string
	// SessionID is the runtime's own session identifier.
	SessionID string
}

// Valid reports whether both halves of the scoped identity are present.
func (a AgentSessionRef) Valid() bool {
	return a.IntegrationInstance != "" && a.SessionID != ""
}

// ClaimRef returns the exclusive claim for this agent-runtime session.
func (a AgentSessionRef) ClaimRef() ClaimRef {
	return ClaimRef{Kind: ClaimAgentSession, Key: digest([]string{
		claimKeyVersion,
		string(ClaimAgentSession),
		a.IntegrationInstance,
		a.SessionID,
	})}
}

// digest hashes a claim's evidence into its key. The parts are joined with a
// separator that cannot occur in any of them, so no two different part lists
// can produce the same input string.
func digest(parts []string) ClaimKey {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x1f")))
	return ClaimKey(hex.EncodeToString(sum[:]))
}
