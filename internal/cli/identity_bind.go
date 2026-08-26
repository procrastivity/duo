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
)

// identityBindTimeout is how long launch waits for host identity and D3
// readiness. Prompt send (step 07) calls the same helper with the
// command's own deadline. Tests zero this so existing launches do one
// poll and return; a live agent that has not reported yet stays
// starting without hanging the CLI.
const defaultIdentityBindTimeout = 3 * time.Second

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
// with launch-plan attestation, and MarkLive when D3 holds. It never
// fails the launch command — the pane is already running.
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
// bound identity and the pane past launch_pending. Callers that need a
// longer wait (prompt send) pass a later deadline. A timeout leaves the
// instance starting with no invented correlation.
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
			if !state.LaunchPending {
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
	if state.LaunchPending {
		return out
	}
	if inst, ok := a.Instance(instance); ok && inst.State == domain.InstanceLive {
		out.Live = true
		return out
	}
	evidence := "host reported agent-session identity and the pane is past launch_pending"
	if err := a.MarkLive(ctx, instance, actor, evidence); err != nil {
		identitySkipped(streams, sess.ID, "MarkLive: %v", err)
		return out
	}
	out.Live = true
	return out
}

// claimFromHostIdentity maps a host-reported id or path onto a RuntimeClaim
// and domain.AgentSessionRef. Path-shaped identity is still an
// agent-session correlation; the value is the id/path. Correlate gets
// TranscriptPath when kind is path so it does not scan a directory (I-6).
// WorkingDirectory is the launched session's workspace root
// (Authority.Workspace(sess.Workspace).RootPath) so Claude can derive the
// project-slug JSONL path from a host-named id (not a path).
func claimFromHostIdentity(runtimeID string, ident host.AgentSessionIdentity, workingDirectory string) (runtime.RuntimeClaim, domain.AgentSessionRef) {
	ref := domain.AgentSessionRef{
		IntegrationInstance: runtimeID,
		SessionID:           ident.Value,
	}
	claim := runtime.RuntimeClaim{
		IntegrationInstanceID:  runtimeID,
		ExternalAgentSessionID: ident.Value,
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
