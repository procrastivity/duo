// hostbind.go is the launch path's host wiring: the instance discoverer M1
// consults, the adapter-evidence-to-kernel-fingerprint bridge, and the
// cold-start first bind that follows a successful spawn.
//
// It sits in internal/cli because this is the composition root that owns
// both sides of every seam here. internal/launch/materialize defines
// InstanceDiscovery and holds no adapter knowledge; internal/host/herdr
// owns the Herdr convention and does not import the kernel; internal/domain
// records the correlation it is handed and deduces nothing
// (hostcorrelation.go: "that bridge belongs to the composition layer that
// owns both"). This file is that layer.

package cli

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// --- instance discovery ---------------------------------------------------

// stage1Discovery is materialize.InstanceDiscovery over the session-host
// kinds this build carries an adapter for. Stage 1 has exactly one: Herdr.
//
// It enumerates from the filesystem — $XDG_CONFIG_HOME/herdr/sessions/
// <session>/herdr.sock, Herdr 0.8.2's observed layout (herdr.DiscoverInstances)
// — and never dials. Enumerating is not attesting: a socket that exists is
// an instance, and a dead one fails at spawn, which is where invariant I-3
// puts that failure.
//
// An unknown kind returns no instances and no error. That is what lets the
// materializer's own "discovery found no herdr instance; name one as
// --host herdr:<instance>" trail line stand as the answer, instead of this
// type inventing a second vocabulary for the same fact.
type stage1Discovery struct{}

var _ materialize.InstanceDiscovery = stage1Discovery{}

func (stage1Discovery) DiscoverInstances(_ context.Context, kind string) ([]materialize.Instance, error) {
	if kind != herdr.AdapterID {
		return nil, nil
	}
	found, err := herdr.DiscoverInstances()
	if err != nil {
		return nil, err
	}
	out := make([]materialize.Instance, 0, len(found))
	for _, instance := range found {
		out = append(out, materialize.Instance{
			Locator:    instance.SocketPath,
			InstanceID: instance.InstanceID,
		})
	}
	return out, nil
}

// --- the host.Evidence -> domain.HostFingerprint bridge -------------------

// hostFingerprint maps one adapter-side host.Evidence onto the kernel's
// notes/19 §5 evidence set.
//
// The mapping is one for one with what internal/host/herdr's evidenceFor
// builds, and domain.HostFingerprint's own doc comment names it: TerminalID
// is the epoch-equivalent (host.Evidence.HostContainerID), PaneID is
// addressing only, and Process is the birth tuple. The one field
// host.Evidence does not carry is the session name — §5.2's identity core
// is epoch, container, and process birth — so it is read back out of the
// integration-instance ID, which is where the Herdr session name lives by
// convention (herdr.InstanceIDForSession).
//
// kind selects that read: only Herdr folds a session name into its instance
// ID, and a future host kind that does not would get "" rather than a
// mis-parsed one.
func hostFingerprint(kind string, e host.Evidence) domain.HostFingerprint {
	fp := domain.HostFingerprint{
		PaneID:     e.PaneID,
		TerminalID: e.HostContainerID,
	}
	if kind == herdr.AdapterID {
		fp.SessionName = herdr.SessionForInstanceID(e.IntegrationInstanceID)
	}
	if e.ProcessBirth.PID > 0 {
		fp.Process = domain.ProcessBirth{
			PID: e.ProcessBirth.PID,
		}
		if !e.ProcessBirth.StartTime.IsZero() {
			// materialize.CaptureTimeLayout is the domain's own timestamp
			// layout verbatim (see its doc comment), so a start time
			// stamped here sorts against a kernel-stamped fact as a plain
			// string.
			fp.Process.StartedAt = e.ProcessBirth.StartTime.UTC().Format(materialize.CaptureTimeLayout)
		}
	}
	return fp
}

// firstBindFingerprint is the evidence a first bind rests on: the host-side
// evidence the spawn itself produced, completed from the deduced host where
// the adapter had nothing to say.
//
// The leaf evidence is the strong half — a pane and a terminal_id Duo
// watched a process start in. The deduced host contributes only the session
// name, and only when the spawn evidence carried none: an instance ID is
// what M1's ambient and discovery rungs know, and it is still real evidence
// about which server this is.
func firstBindFingerprint(deduced materialize.DeducedHost, leaves []launch.LeafLaunch) domain.HostFingerprint {
	var fp domain.HostFingerprint
	for _, leaf := range leaves {
		fp = hostFingerprint(deduced.Kind, leaf.Evidence.Evidence)
		break
	}
	if fp.SessionName == "" && deduced.Kind == herdr.AdapterID {
		fp.SessionName = herdr.SessionForInstanceID(deduced.InstanceID)
	}
	return fp
}

// --- the cold-start first bind --------------------------------------------

// bindReason is the phrase the workspace.host_bound fact carries. The
// record explains the deduction; this only has to say which write it was.
const bindReason = "cold-start host correlation from launch deduction"

// bindFirstHost records the workspace↔host correlation after a successful
// spawn into a workspace that had none, applying notes/43 item 13's hybrid
// rule:
//
//   - `ambient-env` asks. It is the ambient-marker hazard class the Stage-1
//     scrub work targeted, so it is confirmed on an interactive terminal
//     and refused outright without one. The launch still succeeded; only
//     the bind is skipped.
//   - `explicit-flag` and `policy-default` write silently — the operator
//     declared the host, or installed policy did — with loud output naming
//     the bind and the rebind path.
//   - `workspace-correlation` cannot reach a first bind: a workspace that
//     has a correlation is already bound.
//
// It never returns an error. Every outcome is reported on stderr, because
// the launch it follows has already succeeded and a failed bind must not
// retroactively turn a live session into a failed command. The bind is
// written only here, only after Start returned, and never on --dry-run,
// which does not reach this path at all.
func bindFirstHost(
	ctx context.Context,
	streams *iostreams.Streams,
	a *domain.Authority,
	mat materialize.Result,
	result *launch.Result,
	actor string,
) {
	deduced := mat.Host()
	if !deduced.Present() {
		return
	}
	ws, ok := a.WorkspaceForRoot(mat.WorkspacePath())
	if !ok {
		// The launch commit enrolls the workspace, so this is unreachable
		// after a successful spawn. Reported rather than ignored: silence
		// would make a real regression here invisible.
		bindSkipped(streams, "duo has no workspace for %s, so there is nothing to correlate", mat.WorkspacePath())
		return
	}
	if _, bound := a.HostCorrelation(ws.ID); bound {
		return
	}

	fingerprint := firstBindFingerprint(deduced, result.Leaves)
	if !fingerprint.Present() {
		bindSkipped(streams,
			"the spawn produced no identifying evidence for %s, and a correlation is recorded with fingerprints or not at all",
			deduced.Locator())
		return
	}

	if deduced.Source == domain.HostSourceAmbientEnv && !confirmAmbientBind(streams, ws.ID, deduced) {
		return
	}

	correlation, err := a.BindWorkspaceHost(ctx, domain.BindHostRequest{
		Workspace:   ws.ID,
		Kind:        deduced.Kind,
		Instance:    deduced.Instance,
		InstanceID:  deduced.InstanceID,
		Source:      deduced.Source,
		Fingerprint: fingerprint,
		Actor:       actor,
		Reason:      bindReason,
		Evidence:    bindEvidence(deduced, result),
	})
	if err != nil {
		bindSkipped(streams, "recording the correlation failed: %v", err)
		return
	}

	_, _ = fmt.Fprintf(streams.Err,
		"duo: host bound: workspace %s -> %s (host_source=%s, fact %s)\n",
		ws.ID, deduced.Locator(), deduced.Source, correlation.FactID)
	_, _ = fmt.Fprintf(streams.Err, "duo: %s\n", rebindPointer(deduced.Locator()))
}

// confirmAmbientBind asks before recording a bind whose provenance is the
// ambient environment, and reports what it decided.
//
// A non-interactive run is a refusal, not a default-yes and not a prompt
// nobody can answer: the whole point of the hybrid rule is that this rung's
// write is never automatic. The message names the way to get the bind
// deliberately — re-launch with --host, whose provenance is explicit-flag
// and writes silently.
func confirmAmbientBind(streams *iostreams.Streams, ws domain.WorkspaceID, deduced materialize.DeducedHost) bool {
	if !streams.Interactive() {
		bindSkipped(streams,
			"%s was deduced from the ambient environment (host_source=%s) and this run is not interactive, "+
				"so duo asked nobody and recorded nothing. Re-launch with --host %s to bind it deliberately",
			deduced.Locator(), domain.HostSourceAmbientEnv, deduced.Locator())
		return false
	}

	_, _ = fmt.Fprintf(streams.Err,
		"duo: %s was deduced from the ambient environment of the pane this command is running in.\n", deduced.Locator())
	_, _ = fmt.Fprintf(streams.Err,
		"duo: bind workspace %s to it, so later launches go to the same host? [y/N] ", ws)

	answer, err := bufio.NewReader(streams.In).ReadString('\n')
	if err != nil && answer == "" {
		bindSkipped(streams, "no answer was read from the terminal, so nothing was recorded")
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		bindSkipped(streams,
			"not confirmed. Re-launch with --host %s to bind it deliberately", deduced.Locator())
		return false
	}
}

// bindSkipped writes one "the launch worked, the bind did not" line. It
// always names both halves, because an operator who reads only the second
// half will think their session failed.
func bindSkipped(streams *iostreams.Streams, format string, args ...any) {
	_, _ = fmt.Fprintf(streams.Err, "duo: host binding skipped: %s.\n", fmt.Sprintf(format, args...))
	_, _ = fmt.Fprintln(streams.Err, "duo: the launch itself succeeded; only the workspace<->host correlation was not recorded.")
}

// bindEvidence is what the fact records as the thing the bind rests on: the
// rung that deduced the host and the launch-resolution record whose spawn
// proved it. Naming the record is what makes the fact checkable later —
// that record carries the whole deduction trail.
func bindEvidence(deduced materialize.DeducedHost, result *launch.Result) string {
	evidence := fmt.Sprintf("host deduced by %s and proven by a successful spawn", deduced.Source)
	if result != nil && result.Record.ID != "" {
		evidence += " (launch-resolution record " + result.Record.ID + ")"
	}
	return evidence
}

// rebindPointer renders the audited verb that changes a correlation, with
// the target already filled in. duo.external/v1's
// `launch_pointer_set.workspace_host_rebind` names the verb; this is that
// verb an operator can actually type.
func rebindPointer(locator string) string {
	return fmt.Sprintf(
		"change it later with: duo workspace host rebind --host %s --evidence \"<what you checked>\"", locator)
}
