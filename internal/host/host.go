// Package host defines the architecture's §5.2 session-host adapter
// contract: independent interfaces a session-host adapter can implement,
// plus the evidence and request/result types they share.
//
// Stage-1 scope, deliberately: this package implements only HostDiscovery,
// HostLauncher, HostAttachmentValidator, and HostLifecycleSource — the four
// interfaces the Step 11 spec names. §5.2 also defines HostTerminalProvider
// and HostPromptProvider; those are out of scope here and are not
// scaffolded (no empty interface, no TODO type). See
// docs/adapters/decisions.md for why that boundary is drawn there and not
// mid-interface.
//
// The interface names and the types they reference in method signatures
// (HostCandidate, HostLaunchRequest, HostLaunchEvidence, HostAttachmentClaim,
// HostContinuityEvidence, HostObservationRequest, HostObservationStream) are
// copied verbatim from §5.2's Go code block, including the "Host" prefix
// that stutters as host.HostX from another package. That stutter is the
// normative contract, not a naming choice this step made, so the lint
// exclusions on those declarations are load-bearing, not suppressions of
// convenience. Types this package had to invent to fill in the fields the
// prose implies (Evidence, Attachment, LifecycleEvent, ...) drop the
// prefix, because nothing in §5.2 fixes their names.
//
// This package must never import internal/runtime; internal/adapter's
// TestRoleSeparation enforces that mechanically.
package host

import (
	"context"
	"time"
)

// ProcessBirthEvidence is the best available proof that a specific
// operating-system process is the one a candidate, attachment, or launch
// claims. §5.2: "A pane ID or PID alone cannot prove runtime continuity" —
// this type exists so no signature can be satisfied with a bare PID.
type ProcessBirthEvidence struct {
	PID       int
	StartTime time.Time
	// StartTimeSource names how StartTime was obtained (e.g. "procfs",
	// "host-reported"), since its trustworthiness varies by host
	// integration.
	StartTimeSource string
}

// Evidence is the identity core every host-side evidence type in this
// package carries. §5.2: "Host evidence uses host-server epochs,
// host-container IDs, and process-birth evidence." PaneID is
// host-addressing detail (e.g. a tmux pane); it corroborates but never
// substitutes for the three continuity fields.
type Evidence struct {
	IntegrationInstanceID string
	HostServerEpoch       string
	HostContainerID       string
	ProcessBirth          ProcessBirthEvidence
	PaneID                string
}

// DiscoveryRequest scopes a HostDiscovery.Discover call to one integration
// instance and, optionally, one workspace.
type DiscoveryRequest struct {
	IntegrationInstanceID string
	// WorkspaceHint narrows discovery to a workspace when the host
	// integration can filter by it. Empty means unscoped discovery.
	WorkspaceHint string
}

// HostCandidate is one transient, unclaimed external runtime a
// HostDiscovery call found. §5.2 decision-01 language: discovery "does not
// claim, control, or assign a durable public ID."
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostCandidate struct {
	Evidence   Evidence
	DetectedAt time.Time
}

// ResolvedLaunchTuple is the launch resolver's finished output: one
// concrete, already-validated assignment the composer hands to
// HostLauncher.PrepareLaunch. §5.2: "A launch resolver completes launch
// resolution and records the launch-resolution record before any
// HostLauncher.PrepareLaunch call. PrepareLaunch receives the
// already-resolved launch tuple." The launch resolver itself is a later
// step; this is the minimal shape PrepareLaunch needs to receive its
// output.
type ResolvedLaunchTuple struct {
	LaunchResolutionID string
	// Leaf is the assignment's leaf name (§6.7's per-leaf spawn loop) —
	// every preset declares one, single-leaf presets included. Real
	// callers always set it; it is empty only in a hand-built test tuple
	// that has no assignment to draw it from. LaunchResolutionID is the
	// same for every leaf of one launch, so Leaf is what a HostLauncher
	// needs to tell two leaves of the same launch apart.
	Leaf                  string
	IntegrationInstanceID string
	WorkspacePath         string
	Command               string
	Args                  []string
	Env                   map[string]string
}

// HostLaunchRequest is HostLauncher.PrepareLaunch's input.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostLaunchRequest struct {
	ResolvedLaunchTuple ResolvedLaunchTuple
}

// PreparedHostLaunch is PrepareLaunch's result, handed unchanged to Start.
// Callers must treat Opaque as adapter-private and never parse it; only
// the adapter that created a given value can interpret it.
type PreparedHostLaunch struct {
	IntegrationInstanceID string
	LaunchResolutionID    string
	Opaque                any
}

// HostLaunchEvidence is Start's result: the evidence proving the launched
// process is now live.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostLaunchEvidence struct {
	Evidence  Evidence
	StartedAt time.Time
}

// Attachment identifies one previously established binding between a Duo
// session and a host-side location (pane, container, window).
type Attachment struct {
	IntegrationInstanceID string
	HostServerEpoch       string
	HostContainerID       string
	PaneID                string
}

// HostAttachmentClaim is HostAttachmentValidator.ValidateAttachment's
// input: a claimed still-live attachment plus the process-birth evidence
// Duo last recorded for it.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostAttachmentClaim struct {
	Attachment            Attachment
	LastKnownProcessBirth ProcessBirthEvidence
}

// HostContinuityEvidence is ValidateAttachment's result. §5.2: a host
// prompt path "supplies a semantic attempt result. It never changes host
// evidence into an agent acknowledgment" — by the same principle,
// SameProcess is a process-continuity verdict only, never a claim about
// agent state.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostContinuityEvidence struct {
	Evidence Evidence
	// SameProcess is false when the host integration proves a different
	// process now occupies the claimed location. decision-01 §5.3: "A new
	// process in the same pane always needs a new runtime-instance ID."
	SameProcess bool
}

// HostObservationRequest scopes ObserveHostLifecycle to one attachment.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostObservationRequest struct {
	Attachment Attachment
}

// LifecycleEventKind names what a host observation stream reports. These
// are host-side signals only — attachment presence and process exit — not
// the durable session or runtime-instance lifecycle states of decision-01
// §5.1, which the application layer derives from this evidence plus other
// sources.
type LifecycleEventKind string

// The host lifecycle event kinds this contract reports.
const (
	LifecycleAttached LifecycleEventKind = "attached"
	LifecycleDetached LifecycleEventKind = "detached"
	LifecycleExited   LifecycleEventKind = "exited"
)

// LifecycleEvent is one observation from a HostObservationStream.
type LifecycleEvent struct {
	Kind       LifecycleEventKind
	Evidence   Evidence
	ObservedAt time.Time
}

// HostObservationStream is an open subscription to host lifecycle events
// for one attachment. Callers must call Close when done with it.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostObservationStream interface {
	Events() <-chan LifecycleEvent
	Close() error
}

// HostDiscovery reports transient external-runtime candidates. §5.2.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostDiscovery interface {
	Discover(context.Context, DiscoveryRequest) ([]HostCandidate, error)
}

// HostLauncher prepares and starts a launch on an already-resolved launch
// tuple. §5.2.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostLauncher interface {
	PrepareLaunch(context.Context, HostLaunchRequest) (PreparedHostLaunch, error)
	Start(context.Context, PreparedHostLaunch) (HostLaunchEvidence, error)
}

// HostAttachmentValidator revalidates a claimed attachment's process
// continuity. §5.2.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostAttachmentValidator interface {
	ValidateAttachment(context.Context, HostAttachmentClaim) (HostContinuityEvidence, error)
}

// HostLifecycleSource opens a stream of host lifecycle events for one
// attachment. §5.2.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostLifecycleSource interface {
	ObserveHostLifecycle(context.Context, HostObservationRequest) (HostObservationStream, error)
}
