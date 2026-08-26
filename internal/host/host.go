// Package host defines the architecture's §5.2 session-host adapter
// contract: independent interfaces a session-host adapter can implement,
// plus the evidence and request/result types they share.
//
// This package implements HostDiscovery, HostLauncher,
// HostAttachmentValidator, HostLifecycleSource, and HostPromptProvider.
// HostPromptProvider is named here (delegation-loop step 10) and implemented
// by Herdr (step 11) over `agent.prompt`; this package does not invoke it.
// HostTerminalProvider stays out of scope — no empty interface, no TODO type. See
// docs/adapters/decisions.md.
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
	"errors"
	"fmt"
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

// LaunchTarget names where in the session host's containment model a
// launched execution's container is created. Empty on a ResolvedLaunchTuple
// asks the host for its built-in default. The launcher resolves the
// effective value (explicit flag, config-default, or built-in) before
// PrepareLaunch and records it as target / target_source on the
// launch-resolution report (notes/51 records 1–3).
type LaunchTarget string

const (
	// LaunchTargetTab creates the container as a new tab of the host's
	// current workspace (Herdr tab.create; tmux new-window would map here).
	LaunchTargetTab LaunchTarget = "tab"
	// LaunchTargetPane splits the container from an existing pane.
	LaunchTargetPane LaunchTarget = "pane"
)

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
	// Target is the requested placement inside the host's containment
	// model. Empty asks for the host's built-in default. See LaunchTarget.
	Target LaunchTarget
	// CloseOnExit requests that the launched execution's host-side
	// container close itself once the launched agent exits cleanly.
	// Product default is true (notes/51 record 7 stop-gate edit);
	// --remain-on-exit or config close_on_exit: false opts out. Crash
	// paths still leave the pane (no watcher).
	//
	// A HostLauncher implementation carries no obligation from this field
	// alone: the closing action Duo has verified — a Herdr pane closing
	// itself synchronously from inside the launched agent's own
	// SessionEnd hook — never runs as host-adapter behavior (no watcher,
	// no send-keys, no shell injection from outside the pane). CloseOnExit
	// exists on the tuple only so a runtime-specific launch.LeafAugmenter
	// can see the request when it decides what extra arguments or env a
	// leaf's launch needs (a `--settings <path>` pointing at a generated
	// hook, or a pane-creation env marker like DUO_CLOSE_PANE_ON_EXIT); a
	// HostLauncher that has nothing to add for it may ignore it entirely.
	CloseOnExit bool
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

// ContinuityClass is ValidateAttachment's typed host-side verdict. The
// composition root maps these onto domain recovery outcomes; adapters
// never import the domain to do that mapping themselves. Unreachable is
// not a class: it is a call error (see ErrUnreachable).
type ContinuityClass string

// Host continuity classes a successful ValidateAttachment can report.
// A surviving pane is never ContinuityPaneAbsent — absence requires the
// host to prove the pane is gone. Empty-foreground is not a class: Herdr
// falls back to ShellPID, and a PID of zero is unproven birth, not a
// proven empty pane.
const (
	ContinuityPaneAbsent       ContinuityClass = "pane_absent"
	ContinuityTerminalReplaced ContinuityClass = "terminal_replaced"
	ContinuityProcessReplaced  ContinuityClass = "process_replaced"
	ContinuityUnproven         ContinuityClass = "unproven"
	ContinuitySameLive         ContinuityClass = "same_live"
)

// ErrUnreachable marks a ValidateAttachment call that never completed
// because the host could not be reached (dial failure, timeout, missing
// socket). It is not a ContinuityClass and must not be inferred from
// pane absence. DiscoverInstances omitting a socket is the same
// condition, decided before this adapter is built; herdr.New does not
// dial.
var ErrUnreachable = errors.New("host unreachable")

// Unreachable wraps a transport failure so callers can classify it with
// errors.Is without reading the error text. A nil argument is
// ErrUnreachable itself.
func Unreachable(err error) error {
	if err == nil {
		return ErrUnreachable
	}
	if errors.Is(err, ErrUnreachable) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrUnreachable, err)
}

// HostContinuityEvidence is ValidateAttachment's result. §5.2: a host
// prompt path "supplies a semantic attempt result. It never changes host
// evidence into an agent acknowledgment" — by the same principle,
// SameProcess is a process-continuity verdict only, never a claim about
// agent state. Class is the typed reason a composition root switches on;
// SameProcess stays true only for ContinuitySameLive so existing callers
// keep compiling.
//
//nolint:revive // name is §5.2's Go code block verbatim; see package doc comment.
type HostContinuityEvidence struct {
	Evidence Evidence
	Class    ContinuityClass
	// SameProcess is true only when Class is ContinuitySameLive.
	// decision-01 §5.3: "A new process in the same pane always needs a
	// new runtime-instance ID."
	SameProcess bool
}

// ContinuityEvidence builds a ValidateAttachment result. SameProcess is
// set from Class so adapters cannot report same-live and a different
// class at once.
func ContinuityEvidence(class ContinuityClass, evidence Evidence) HostContinuityEvidence {
	return HostContinuityEvidence{
		Evidence:    evidence,
		Class:       class,
		SameProcess: class == ContinuitySameLive,
	}
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
