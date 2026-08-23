// Package launchrecord is the glue between the launch resolver and the
// identity and lifecycle kernel: a launch.Recorder that commits the
// launch-resolution record and the Duo identities it explains in one
// domain transaction.
//
// It exists as its own package to keep the dependency direction the
// architecture's §3 table fixes. internal/launch is a pure decision
// component and must not import the kernel; internal/domain owns no
// component's schema and must not import the resolver. This package imports
// both and neither imports it, so the seam stays a seam.
//
// What it wires:
//
//	launch.Launcher --Recorder--> launchrecord.Recorder --> domain.Authority.Launch
//	                                                        --> Repository.CommitLaunchResolution
//	                                                        --> store.LaunchResolutionTx
//
// which is duo-vnext-go-architecture.md §4.2's launch-resolution boundary,
// entered exactly once per launch, before anything spawns.
package launchrecord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/launch"
)

// Options configure a Recorder.
//
// WorkspacePath is here rather than on the call because launch.Recorder's
// method takes the record and nothing else: the record is a decision about
// *what* to launch, and placement is not part of it (decision-01 §3.3 — a
// path is a placement input, never an identity). One Recorder therefore
// belongs to one workspace root, which is also the granularity a
// launch.Launcher is built at.
type Options struct {
	// WorkspacePath is the workspace root the launched session belongs to.
	// Required: the kernel refuses a launch without one.
	WorkspacePath string
	// Actor is the responsible actor or subsystem recorded on every fact.
	// It defaults to the record's Caller, and then to the kernel's own
	// default.
	Actor string
	// Owner is the session's owning subject; it defaults to Actor.
	Owner string
	// BindActor optionally binds a durable agent actor at launch. A launch
	// plan is one of §3.4's admissible binding sources, so no separate
	// attestation is needed.
	BindActor domain.ActorID
	// OnLaunch, when set, receives the kernel's whole LaunchResult after a
	// successful commit.
	//
	// It exists because launch.Commit — a fixed contract this package may
	// not change — carries only the session and instance IDs, while
	// domain.Launch also returns the instance-scoped reporter credential,
	// which the kernel returns exactly once and never stores in the clear.
	// A caller that needs it (anything that will later accept a
	// SessionStart report from the launched execution) takes it here. A
	// caller that does not may leave this nil; the credential is then
	// unrecoverable, which is the intended behaviour of a secret nobody
	// kept.
	OnLaunch func(domain.LaunchResult)
}

// Recorder commits launch-resolution records through the domain kernel.
//
// It satisfies launch.Recorder, so a launch.Launcher built with one gets the
// pre-spawn guarantee the launcher already enforces (record, then spawn)
// backed by a durable, atomic transaction: the record, the Duo session, and
// its starting runtime instance commit together or not at all.
type Recorder struct {
	authority *domain.Authority
	opts      Options
}

var _ launch.Recorder = (*Recorder)(nil)

// New builds a Recorder over an open authority.
func New(authority *domain.Authority, opts Options) (*Recorder, error) {
	switch {
	case authority == nil:
		return nil, errors.New("launchrecord: New needs an open domain.Authority")
	case opts.WorkspacePath == "":
		return nil, errors.New("launchrecord: New needs a workspace path; the kernel refuses a launch without one")
	}
	return &Recorder{authority: authority, opts: opts}, nil
}

// CommitLaunchResolution implements launch.Recorder.
//
// It serializes the record, hands the bytes to the kernel as an opaque body,
// and returns the identities the same transaction created. The kernel does
// not decode the body — this package is the only side that knows the
// record's schema — so the bytes that come back after an authority restart
// are the bytes committed here.
//
// The record commits *before* those identities exist as values, which is why
// its serialized body carries no session or instance link: the links are on
// the launch.resolved fact itself (Fact.SessionID, Fact.InstanceID) and are
// queryable through Authority.SessionLaunchResolution. launch.Launcher fills
// the same links into its in-memory copy of the record from the Commit this
// method returns, which is what reaches the caller and the ordinary result.
func (r *Recorder) CommitLaunchResolution(ctx context.Context, rec launch.Record) (launch.Commit, error) {
	if rec.ID == "" {
		return launch.Commit{}, errors.New("launchrecord: the launch-resolution record has no id")
	}
	body, err := json.Marshal(rec)
	if err != nil {
		return launch.Commit{}, fmt.Errorf("launchrecord: serializing launch-resolution record %s: %w", rec.ID, err)
	}

	actor := r.opts.Actor
	if actor == "" {
		actor = rec.Caller
	}
	result, err := r.authority.Launch(ctx, domain.LaunchRequest{
		RootPath:  r.opts.WorkspacePath,
		Actor:     actor,
		Owner:     r.opts.Owner,
		BindActor: r.opts.BindActor,
		Reason:    reason(rec),
		Resolution: &domain.LaunchResolution{
			ID:   domain.LaunchResolutionID(rec.ID),
			Body: body,
		},
	})
	if err != nil {
		// Nothing committed: the boundary is atomic, so there is no session,
		// no runtime instance, and no record to clean up (§7.4).
		return launch.Commit{}, fmt.Errorf("launchrecord: committing launch-resolution record %s: %w", rec.ID, err)
	}
	if r.opts.OnLaunch != nil {
		r.opts.OnLaunch(result)
	}
	return launch.Commit{
		SessionID: string(result.Session),
		// The kernel's launch verb mints one starting runtime instance per
		// session, so a multi-leaf resolution reports one instance here
		// today; the per-leaf executions correlate to it as they report in.
		InstanceIDs: []string{string(result.Instance)},
	}, nil
}

// reason renders the one phrase the launch facts and the audit row carry.
// Everything that explains the choice is in the record; this only has to say
// which request it was.
func reason(rec launch.Record) string {
	if rec.RequestedPreset == "" {
		return "launch"
	}
	return "launch of preset " + rec.RequestedPreset
}
