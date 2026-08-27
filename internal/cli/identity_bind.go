package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/runtime"
	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

// identityBindTimeout is how long launch waits for host identity and D3
// readiness (!launch_pending, or a runtime-offered Ready signal).
// Production is 8s: identity is usually already on agent.list
// (AgentOnPane) inside the old 3s window, but launch_pending can stay
// true until ~2s after that window / ~6s from process birth. The cap is
// finite — identity discovery is not the bottleneck, and the wait is
// not unbounded. Prompt send calls the same helper with the command's
// own deadline. Tests zero this so existing launches do one poll and
// return; a live agent that has not reported yet stays starting without
// hanging the CLI.
const defaultIdentityBindTimeout = 8 * time.Second

const defaultIdentityBindPoll = 50 * time.Millisecond

var (
	identityBindTimeout = defaultIdentityBindTimeout
	identityBindPoll    = defaultIdentityBindPoll
)

// identityBindOutcome is what bindStartingIdentity returns so launch and
// later prompt send can share one wait without a `duo session settle`
// verb.
type identityBindOutcome struct {
	Bound bool
	Live  bool
}

const identityBindReason = "host-reported agent-session identity after launch"

// bindLaunchIdentities is the post-spawn agent-session bind: for each
// launched leaf it polls the host for pane identity, Correlates, Binds
// with launch-plan attestation, and MarkLive when D3 holds. D3 is bound
// identity and (!launch_pending or runtime Ready). It never fails the
// launch command — the pane is already running.
func bindLaunchIdentities(
	ctx context.Context,
	streams *iostreams.Streams,
	a *domain.Authority,
	launcher *launch.Launcher,
	result *launch.Result,
	actor string,
) {
	if result == nil || result.Record.SessionID == "" {
		return
	}
	session := domain.SessionID(result.Record.SessionID)
	sess, ok := a.Session(session)
	if !ok || sess.Current == "" {
		return
	}
	deadline := time.Now().Add(identityBindTimeout)
	for _, leaf := range result.Leaves {
		hostLauncher, err := launcher.LauncherFor(leaf.Tuple)
		if err != nil {
			identitySkipped(streams, session, "leaf %s: %v", leaf.Leaf, err)
			continue
		}
		source, ok := hostLauncher.(host.AgentIdentitySource)
		if !ok {
			continue
		}
		runtimeID := agentRuntimeIntegrationID(leaf.Tuple.AgentRuntime)
		paneID := leaf.Evidence.Evidence.PaneID
		bindStartingIdentity(ctx, streams, a, source, sess, sess.Current, paneID, runtimeID, actor, deadline)
	}
}

// bindStartingIdentity polls one pane for host identity, writes
// Authority.Bind (the late SessionStart path in domain.BindRequest) with
// Attestation.Source = launch-plan, and MarkLive only when D3 holds:
// bound identity and (!launch_pending or runtime Ready). Callers that
// need a longer wait (prompt send) pass a later deadline. A timeout
// leaves the instance starting with no invented correlation.
func bindStartingIdentity(
	ctx context.Context,
	streams *iostreams.Streams,
	a *domain.Authority,
	source host.AgentIdentitySource,
	sess domain.Session,
	instance domain.InstanceID,
	paneID, runtimeID, actor string,
	deadline time.Time,
) identityBindOutcome {
	if inst, ok := a.Instance(instance); ok && inst.State == domain.InstanceLive {
		_, bound := agentBindingsFor(a, sess)
		return identityBindOutcome{Bound: bound, Live: true}
	}
	if paneID == "" || source == nil {
		return identityBindOutcome{}
	}

	var last host.AgentBindState
	var sawIdentity bool
	poll := identityBindPoll
	if poll <= 0 {
		poll = defaultIdentityBindPoll
	}

	for {
		state, found, err := source.AgentOnPane(ctx, paneID)
		if err != nil {
			identitySkipped(streams, sess.ID, "%v", err)
			return identityBindOutcome{}
		}
		if found && state.Session != nil && state.Session.Value != "" {
			last = state
			sawIdentity = true
			if identityIsReady(ctx, runtimeID, state) {
				return commitIdentityBind(ctx, streams, a, sess, instance, runtimeID, actor, state)
			}
		}
		if !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			return finishIdentityWait(ctx, streams, a, sess, instance, runtimeID, actor, last, sawIdentity)
		case <-time.After(poll):
		}
	}

	return finishIdentityWait(ctx, streams, a, sess, instance, runtimeID, actor, last, sawIdentity)
}

func finishIdentityWait(
	ctx context.Context,
	streams *iostreams.Streams,
	a *domain.Authority,
	sess domain.Session,
	instance domain.InstanceID,
	runtimeID, actor string,
	last host.AgentBindState,
	sawIdentity bool,
) identityBindOutcome {
	if !sawIdentity {
		return identityBindOutcome{}
	}
	return commitIdentityBind(ctx, streams, a, sess, instance, runtimeID, actor, last)
}

func commitIdentityBind(
	ctx context.Context,
	streams *iostreams.Streams,
	a *domain.Authority,
	sess domain.Session,
	instance domain.InstanceID,
	runtimeID, actor string,
	state host.AgentBindState,
) identityBindOutcome {
	if state.Session == nil || state.Session.Value == "" {
		return identityBindOutcome{}
	}
	workingDirectory := ""
	if ws, ok := a.Workspace(sess.Workspace); ok {
		workingDirectory = ws.RootPath
	}
	claim, ref := claimFromHostIdentity(runtimeID, *state.Session, workingDirectory)
	transcript := ""
	if rt, err := openAgentRuntime(runtimeID); err == nil {
		if correlator, ok := rt.(runtime.RuntimeCorrelator); ok {
			evidence, err := correlator.Correlate(ctx, claim)
			if err == nil && evidence.Bound {
				transcript = evidence.TranscriptID
				// Bound evidence beats the raw host string (I-11).
				if evidence.ExternalAgentSessionID != "" {
					ref.SessionID = evidence.ExternalAgentSessionID
				}
			}
		}
	}
	if transcript == "" && state.Session.Kind == host.AgentSessionKindPath {
		transcript = state.Session.Value
	}

	if _, already := agentBindingsFor(a, sess); !already {
		if err := a.Bind(ctx, domain.BindRequest{
			Session:      sess.ID,
			Instance:     instance,
			Actor:        actor,
			Attestation:  domain.Attestation{Source: domain.SourceLaunchPlan, Subject: actor},
			AgentSession: ref,
			Transcript:   transcript,
			Reason:       identityBindReason,
		}); err != nil {
			identitySkipped(streams, sess.ID, "%v", err)
			return identityBindOutcome{}
		}
	}

	out := identityBindOutcome{Bound: true}
	if !identityIsReady(ctx, runtimeID, state) {
		return out
	}
	if inst, ok := a.Instance(instance); ok && inst.State == domain.InstanceLive {
		out.Live = true
		return out
	}
	evidence := "host reported agent-session identity and the pane is past launch_pending"
	if state.LaunchPending {
		evidence = "host reported agent-session identity and the runtime reports ready"
	}
	if err := a.MarkLive(ctx, instance, actor, evidence); err != nil {
		identitySkipped(streams, sess.ID, "MarkLive: %v", err)
		return out
	}
	out.Live = true
	return out
}

// identityIsReady is D3 readiness: past launch_pending, or the runtime
// reports Ready while still pending. Never dials when already past
// launch_pending.
func identityIsReady(ctx context.Context, runtimeID string, state host.AgentBindState) bool {
	if !state.LaunchPending {
		return true
	}
	return runtimeReportsReady(ctx, runtimeID, state)
}

// runtimeReportsReady asks an optional RuntimeReadyProvider. Missing
// interface, open error, Ready error, or false → not ready. Errors never
// fail the launch command.
func runtimeReportsReady(ctx context.Context, runtimeID string, state host.AgentBindState) bool {
	if state.Session == nil || state.Session.Value == "" {
		return false
	}
	rt, err := openAgentRuntime(runtimeID)
	if err != nil {
		return false
	}
	provider, ok := rt.(runtime.RuntimeReadyProvider)
	if !ok {
		return false
	}
	ready, err := provider.Ready(ctx, runtime.RuntimeBinding{
		ExternalAgentSessionID: state.Session.Value,
	})
	if err != nil || !ready {
		return false
	}
	return true
}

// claimFromHostIdentity maps a host-reported id or path onto a RuntimeClaim
// and domain.AgentSessionRef. Path-shaped identity is still an
// agent-session correlation. For AgentSessionKindPath, peel a UUID from
// the transcript file name via SessionIDFromTranscriptName (I-11); on
// success ExternalAgentSessionID / AgentSessionRef.SessionID are the
// UUID while TranscriptPath stays the host path. Empty peel keeps the
// raw host value. Correlate gets TranscriptPath when kind is path so it
// does not scan a directory (I-6). WorkingDirectory is the launched
// session's workspace root
// (Authority.Workspace(sess.Workspace).RootPath) so Claude can derive the
// project-slug JSONL path from a host-named id (not a path).
func claimFromHostIdentity(runtimeID string, ident host.AgentSessionIdentity, workingDirectory string) (runtime.RuntimeClaim, domain.AgentSessionRef) {
	sessionID := ident.Value
	if ident.Kind == host.AgentSessionKindPath {
		if peeled := runtimepi.SessionIDFromTranscriptName(ident.Value); peeled != "" {
			sessionID = peeled
		}
	}
	ref := domain.AgentSessionRef{
		IntegrationInstance: runtimeID,
		SessionID:           sessionID,
	}
	claim := runtime.RuntimeClaim{
		IntegrationInstanceID:  runtimeID,
		ExternalAgentSessionID: sessionID,
		WorkingDirectory:       workingDirectory,
	}
	if ident.Kind == host.AgentSessionKindPath {
		claim.TranscriptPath = ident.Value
	}
	return claim, ref
}

func identitySkipped(streams *iostreams.Streams, session domain.SessionID, format string, args ...any) {
	if streams == nil {
		return
	}
	_, _ = fmt.Fprintf(streams.Err, "duo: agent identity not bound for session %s: %s.\n",
		session, fmt.Sprintf(format, args...))
	_, _ = fmt.Fprintln(streams.Err,
		"duo: the launch itself succeeded; the instance stays starting until the host reports identity.")
}

// waitPromptIdentity is the send-path wait: type-assert the already-open
// host prompt provider (no Launcher), resolve pane and runtime from the
// attachment and launch tuple, and poll bindStartingIdentity until live
// or deadline. Prompt send passes the command's expires_at, not
// identityBindTimeout — tests zero that for launch, and a delay test
// must still be able to wait.
func waitPromptIdentity(
	ctx context.Context,
	streams *iostreams.Streams,
	a *domain.Authority,
	hostAd host.HostPromptProvider,
	sess domain.Session,
	actor string,
	deadline time.Time,
) identityBindOutcome {
	source, _ := hostAd.(host.AgentIdentitySource)
	return bindStartingIdentity(ctx, streams, a, source, sess, sess.Current,
		paneIDForSession(a, sess), runtimeIDForSession(a, sess), actor, deadline)
}

// paneIDForSession is the launch pane: hostbind records pane_id as the
// attachment container. Prompt send does not scan directories (I-6).
func paneIDForSession(a *domain.Authority, sess domain.Session) string {
	if sess.Attachment == "" {
		return ""
	}
	att, ok := a.Attachment(sess.Attachment)
	if !ok {
		return ""
	}
	return att.Container
}

// runtimeIDForSession is the agent-runtime integration-instance ID for a
// session that may not yet have an agent.session correlation. Prefer the
// bound correlation; otherwise decode the launch-resolution assignment
// (the launch tuple) and map through agentRuntimeIntegrationID. No
// fourth BindingSource.
func runtimeIDForSession(a *domain.Authority, sess domain.Session) string {
	if b, ok := agentBindingsFor(a, sess); ok && b.IntegrationInstance != "" {
		return b.IntegrationInstance
	}
	rec, ok := a.SessionLaunchResolution(sess.ID)
	if !ok {
		return ""
	}
	var body launch.Record
	if err := json.Unmarshal(rec.Body, &body); err != nil {
		return ""
	}
	if len(body.Assignment) == 0 {
		return ""
	}
	return agentRuntimeIntegrationID(body.Assignment[0].Tuple.AgentRuntime)
}
