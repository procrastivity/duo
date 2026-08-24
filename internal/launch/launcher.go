package launch

import (
	"context"
	"fmt"

	"github.com/procrastivity/duo/internal/host"
)

// Recorder durably commits one launch-resolution record.
//
// §4.2's launch-resolution boundary and §7.4 together fix what an
// implementation owes: the record, the session it belongs to, and the
// session's starting runtime instances commit together or not at all, and
// Commit returns only once that transaction is durable. A Commit that
// returned early would turn the whole pre-spawn ordering guarantee into a
// race, because the caller's next step is to spawn a process.
//
// A *failed* resolution never reaches here at all: Launcher.Launch returns
// before commit, so there is no session, no record, and nothing to roll
// back. That is §6.8's "effect: no_effect" and §7.4's "a failed resolution
// creates no session and therefore no launch-resolution record", enforced
// by control flow rather than by cleanup.
type Recorder interface {
	CommitLaunchResolution(ctx context.Context, rec Record) (Commit, error)
}

// Commit is what the launch-resolution transaction hands back: the Duo
// identities it created in the same transaction as the record. They become
// §6.9's "eventual session/runtime-instance links" on the record and the
// session_id of the ordinary result.
type Commit struct {
	SessionID   string
	InstanceIDs []string
}

// HostSet resolves the session-host launcher for one materialized tuple.
// One declared session host is one integration instance, so an
// implementation keys on Tuple.IntegrationInstanceID.
type HostSet interface {
	LauncherFor(t Tuple) (host.HostLauncher, error)
}

// Launcher is the ordering guarantee: it resolves, commits the
// launch-resolution record, and only then lets a session host prepare a
// spawn.
//
// duo-vnext-go-architecture.md §5.2: "A launch resolver completes launch
// resolution and records the launch-resolution record before any
// HostLauncher.PrepareLaunch call. PrepareLaunch receives the
// already-resolved launch tuple."
//
// The ordering here is not a convention about the order of statements. The
// only value spawn accepts is a *committed, and the only way to obtain one
// is to call commit and have it succeed — so a future edit that tried to
// prepare a launch before (or without) the commit would not compile. The
// test TestRecordCommitsBeforePrepareLaunch pins the observable half of the
// same property.
type Launcher struct {
	resolver *Resolver
	recorder Recorder
	hosts    HostSet
}

// NewLauncher builds a launcher over a resolver, a recorder, and a host
// set.
func NewLauncher(resolver *Resolver, recorder Recorder, hosts HostSet) (*Launcher, error) {
	switch {
	case resolver == nil:
		return nil, fmt.Errorf("launch: NewLauncher needs a resolver")
	case recorder == nil:
		return nil, fmt.Errorf("launch: NewLauncher needs a recorder; the launch-resolution record must be durable before anything spawns")
	case hosts == nil:
		return nil, fmt.Errorf("launch: NewLauncher needs a host set")
	}
	return &Launcher{resolver: resolver, recorder: recorder, hosts: hosts}, nil
}

// SpawnRequest is one session.launch request: the resolution request plus
// the placement inputs a host launcher needs. A dry run is the same request
// with DryRun set — same resolver, same static inputs, nothing recorded and
// nothing spawned.
type SpawnRequest struct {
	Request
	// WorkspacePath is the working directory the launched execution starts
	// in. It is a placement input, never an identity (decision-01 §3.3).
	WorkspacePath string
	// Env is the environment the host launcher adds to the execution.
	Env map[string]string
	// Target is the requested container placement inside the deduced
	// host's containment model — a placement input like WorkspacePath,
	// never a constraint axis. Empty means the host's built-in default.
	Target host.LaunchTarget
	// DryRun previews the resolution: same resolver, same static inputs,
	// no durable record, no session, and no spawn (§6.10). In random mode
	// its draw is preview-only and is not promised for a later launch.
	DryRun bool
}

// Result is one launch's outcome.
type Result struct {
	// Report is the ordinary safe result.
	Report Report
	// Record is the launch-resolution record as committed, including the
	// session and runtime-instance links the commit added.
	Record Record
	// Leaves carries each leaf's host-side launch evidence, in the
	// resolution's leaf order. It is empty for a dry run, and partial when
	// a spawn failed partway: the leaves before the failure did start, and
	// the caller owns what to do about them.
	Leaves []LeafLaunch
}

// LeafLaunch is one leaf's host-side spawn.
type LeafLaunch struct {
	Leaf     string
	Tuple    Tuple
	Prepared host.PreparedHostLaunch
	Evidence host.HostLaunchEvidence
}

// committed is proof that a launch-resolution record is durable.
//
// It is unexported and has no exported constructor on purpose: it is the
// token that makes "record before spawn" a type-level property of this
// package rather than a comment. commit is the only thing that builds one,
// and it builds one only after Recorder.CommitLaunchResolution returned
// without error.
type committed struct {
	record Record
}

// Launch resolves, records, and spawns — §6.7's step 10 and the pre-spawn
// gate, in that order and no other.
//
// A resolution failure returns before anything is committed or spawned; a
// commit failure returns before anything is spawned. A spawn failure after
// the commit leaves the record standing: §7.4's "once a resolution succeeds
// and its record is durable, a later spawn failure does not erase that
// evidence". The partial Result comes back with the error so the caller can
// record the failure against the session the commit created.
func (l *Launcher) Launch(ctx context.Context, req SpawnRequest) (*Result, error) {
	resolution, err := l.resolver.Resolve(req.Request)
	if err != nil {
		// Nothing was recorded and nothing was prepared: the pre-spawn
		// gate is that this return happens before the commit.
		return nil, err
	}

	if req.DryRun {
		return dryRunResult(resolution), nil
	}
	if req.WorkspacePath == "" {
		return nil, invalidRequest("A launch needs a workspace path.")
	}

	c, err := l.commit(ctx, resolution)
	if err != nil {
		return nil, err
	}
	return l.spawn(ctx, c, req)
}

// Resolve exposes the pure resolution for a caller that only wants the
// decision — a preview, an explanation, a validation pass. It records
// nothing and spawns nothing.
func (l *Launcher) Resolve(req Request) (*Resolution, error) { return l.resolver.Resolve(req) }

// dryRunResult builds the preview result. It carries no
// launch_resolution_id, because a dry run commits no record and an ID that
// references nothing is worse than no ID: §6.10 makes a dry run create
// "no session or durable launch-resolution record".
func dryRunResult(resolution *Resolution) *Result {
	report := resolution.Report()
	report.LaunchResolutionID = ""
	report.Preview = true
	return &Result{Report: report, Record: resolution.Record}
}

// commit durably records the resolution and returns the proof token spawn
// needs.
func (l *Launcher) commit(ctx context.Context, resolution *Resolution) (*committed, error) {
	record := resolution.Record
	result, err := l.recorder.CommitLaunchResolution(ctx, record)
	if err != nil {
		return nil, fmt.Errorf("launch: committing launch-resolution record %s: %w", record.ID, err)
	}
	record.SessionID = result.SessionID
	record.InstanceIDs = result.InstanceIDs
	return &committed{record: record}, nil
}

// spawn prepares and starts every leaf of an already-recorded resolution.
// Taking *committed rather than *Resolution is what makes the ordering
// unforgeable.
func (l *Launcher) spawn(ctx context.Context, c *committed, req SpawnRequest) (*Result, error) {
	resolved := &Resolution{Record: c.record}
	out := &Result{Report: resolved.Report(), Record: c.record}

	for _, assignment := range c.record.Assignment {
		launcher, err := l.hosts.LauncherFor(assignment.Tuple)
		if err != nil {
			return out, fmt.Errorf("launch: leaf %s: %w", assignment.Leaf, err)
		}
		prepared, err := launcher.PrepareLaunch(ctx, host.HostLaunchRequest{
			ResolvedLaunchTuple: host.ResolvedLaunchTuple{
				LaunchResolutionID:    c.record.ID,
				Leaf:                  assignment.Leaf,
				IntegrationInstanceID: assignment.Tuple.IntegrationInstanceID,
				WorkspacePath:         req.WorkspacePath,
				Command:               assignment.Tuple.Executable,
				Args:                  assignment.Tuple.Arguments,
				Env:                   req.Env,
				Target:                req.Target,
			},
		})
		if err != nil {
			return out, fmt.Errorf("launch: leaf %s: preparing launch: %w", assignment.Leaf, err)
		}
		evidence, err := launcher.Start(ctx, prepared)
		if err != nil {
			return out, fmt.Errorf("launch: leaf %s: starting launch: %w", assignment.Leaf, err)
		}
		out.Leaves = append(out.Leaves, LeafLaunch{
			Leaf:     assignment.Leaf,
			Tuple:    assignment.Tuple,
			Prepared: prepared,
			Evidence: evidence,
		})
	}
	return out, nil
}
