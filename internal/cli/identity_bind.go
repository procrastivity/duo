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
	runtimedevin "github.com/procrastivity/duo/internal/runtime/devin"
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

// defaultMintExitProbeInterval is the continuity-probe cadence
// bindStartingIdentity uses while no AgentOnPane row exists yet for the
// pane: a print-mint launch (`devin ... --export <path> --print <prompt>`)
// mints an agent session, writes the ATIF export, and exits — nothing else
// observes that exit, and a missing AgentOnPane row is not exit evidence by
// itself (internal/host/herdr/agent_bind.go). Only ValidateAttachment is.
// Production probes at most once a second so a live pane's continuity
// check never competes with the tighter identity poll; tests override this
// var for speed.
const defaultMintExitProbeInterval = 1 * time.Second

// mintExitProbeCallTimeout bounds one ValidateAttachment round trip during
// the mint-exit probe, mirroring reconcileCallTimeout's session.reconcile
// budget (session_reconcile.go) so a dead socket fails fast instead of
// stalling the identity wait.
const mintExitProbeCallTimeout = 2 * time.Second

var (
	identityBindTimeout   = defaultIdentityBindTimeout
	identityBindPoll      = defaultIdentityBindPoll
	mintExitProbeInterval = defaultMintExitProbeInterval
)

// identityBindOutcome is what bindStartingIdentity returns so launch and
// later prompt send can share one wait without a `duo session settle`
// verb.
type identityBindOutcome struct {
	Bound bool
	Live  bool
	// Exited is true when bindStartingIdentity itself observed the
	// print-mint launch's process exit through host continuity evidence
	// (never a signal Duo sent) and drove the runtime instance to a
	// terminal outcome: Authority.Exit on the generic leg, or
	// Authority.ReleaseAttachmentClaim after a recovered Devin
	// agent-session bind. Step 4 (typed prompt-send return) reads this;
	// expireUnboundPrompt does not consume it yet.
	Exited bool
	// ExitEvidence names the continuity class that confirmed the exit
	// (e.g. "mint process exit observed: pane_absent"), for a caller that
	// wants to say more than "exited" (bindLaunchIdentities' loud note).
	// Empty unless Exited is true.
	ExitEvidence string
}

// mintExitProbe bundles the optional continuity prober bindStartingIdentity
// uses to notice a print-mint launch's process exiting before it ever
// posted host identity. A nil Validator disables probing entirely —
// behavior is exactly today's identity-only poll.
type mintExitProbe struct {
	Validator host.HostAttachmentValidator
	Claim     host.HostAttachmentClaim
}

func (p mintExitProbe) enabled() bool { return p.Validator != nil }

// mintExitTracker is bindStartingIdentity's per-call continuity-probe
// state. It gates ValidateAttachment to at most once per
// mintExitProbeInterval, and it requires two consecutive *agreeing*
// process_replaced or terminal_replaced probes before treating that class
// as confirmed exit — the print-mint spawn-handover window can briefly
// report the pane's foreground as the shell, and one probe alone cannot
// tell that apart from a genuine replace.
type mintExitTracker struct {
	lastProbe time.Time
	lastClass host.ContinuityClass
	streak    int
}

// due reports whether it is time for another probe: always on the first
// call, then at most once per mintExitProbeInterval.
func (t *mintExitTracker) due(now time.Time) bool {
	return t.lastProbe.IsZero() || now.Sub(t.lastProbe) >= mintExitProbeInterval
}

// observe calls ValidateAttachment once and folds the result into the
// streak. It reports the confirmed class and true only for
// ContinuityPaneAbsent (immediate) or two consecutive agreeing
// ContinuityProcessReplaced / ContinuityTerminalReplaced probes.
// host.ErrUnreachable, any other call error, or any other class is not
// exit and resets the streak. This is observation only — it never sends a
// signal to the launched process.
func (t *mintExitTracker) observe(ctx context.Context, probe mintExitProbe) (host.ContinuityClass, bool) {
	t.lastProbe = time.Now()
	cctx, cancel := context.WithTimeout(ctx, mintExitProbeCallTimeout)
	defer cancel()
	got, err := probe.Validator.ValidateAttachment(cctx, probe.Claim)
	if err != nil {
		t.lastClass, t.streak = "", 0
		return "", false
	}
	switch got.Class {
	case host.ContinuityPaneAbsent:
		return got.Class, true
	case host.ContinuityProcessReplaced, host.ContinuityTerminalReplaced:
		if t.lastClass == got.Class {
			t.streak++
		} else {
			t.lastClass, t.streak = got.Class, 1
		}
		if t.streak >= 2 {
			return got.Class, true
		}
		return "", false
	default:
		t.lastClass, t.streak = "", 0
		return "", false
	}
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
	claim, hasClaim := sessionAttachmentClaim(a, sess)
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
		var probe mintExitProbe
		if hasClaim {
			if validator, ok := hostLauncher.(host.HostAttachmentValidator); ok {
				probe = mintExitProbe{Validator: validator, Claim: claim}
			}
		}
		out := bindStartingIdentity(ctx, streams, a, source, probe, sess, sess.Current, paneID, runtimeID, actor, deadline)
		if out.Exited && !out.Bound {
			mintExitNote(streams, session, out.ExitEvidence)
		}
	}
}

// sessionAttachmentClaim rebuilds the mint-exit prober's claim from the
// session's current host attachment. recordLaunchAttachments records it
// before bindLaunchIdentities runs; prompt send reads whatever the launch
// already recorded. ok is false when the session has no attachment yet, in
// which case probing stays disabled — the same as a nil Validator.
func sessionAttachmentClaim(a *domain.Authority, sess domain.Session) (host.HostAttachmentClaim, bool) {
	if sess.Attachment == "" {
		return host.HostAttachmentClaim{}, false
	}
	att, ok := a.Attachment(sess.Attachment)
	if !ok {
		return host.HostAttachmentClaim{}, false
	}
	return attachmentClaim(att), true
}

// mintExitNote is bindLaunchIdentities' loud note for the generic exit leg:
// identitySkipped's "stays starting" message would be wrong here — the
// print-mint process exited, no agent-session id could be recovered (or
// the runtime is not Devin), and Authority.Exit already ran, so the
// instance is exited, not starting. The launch command still succeeds
// (matches today's leaves-starting behavior for the ordinary timeout).
func mintExitNote(streams *iostreams.Streams, session domain.SessionID, evidence string) {
	if streams == nil {
		return
	}
	_, _ = fmt.Fprintf(streams.Err, "duo: agent identity never appeared for session %s: %s.\n",
		session, evidence)
	_, _ = fmt.Fprintln(streams.Err,
		"duo: the launch itself succeeded; the runtime instance was exited because the launch process ended before Duo could recover anything to track.")
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
	probe mintExitProbe,
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
	var tracker mintExitTracker

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
		// A missing AgentOnPane row alone is not exit evidence
		// (internal/host/herdr/agent_bind.go) — only ValidateAttachment is,
		// so the continuity probe only ever runs while identity has not
		// shown up yet.
		if !found && probe.enabled() && tracker.due(time.Now()) {
			if class, confirmed := tracker.observe(ctx, probe); confirmed {
				return handleMintExit(ctx, streams, a, sess, instance, runtimeID, actor, string(class))
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

// handleMintExit is bindStartingIdentity's confirmed-exit leg: host
// continuity evidence proved the print-mint process is gone (never a
// signal Duo sent). Devin is special-cased because a print-mint launch's
// whole job is to leave behind an agent-session id nothing else observed:
// read it from the ATIF export the process wrote before it exited, and if
// one is there, bind and mark the instance live exactly as an on-time
// AgentOnPane row would have, then release the claim the exited mint
// process held — ReleaseAttachmentClaim, not Exit, because the runtime
// instance is now bound to a live agent-runtime session and must stay
// live. Every other case — no recoverable id, or a non-Devin runtime —
// takes the generic leg: Authority.Exit, which releases every claim
// through exitInstance.
func handleMintExit(
	ctx context.Context,
	streams *iostreams.Streams,
	a *domain.Authority,
	sess domain.Session,
	instance domain.InstanceID,
	runtimeID, actor, class string,
) identityBindOutcome {
	evidence := "mint process exit observed: " + class
	if runtimeID == "devin" {
		if path := devinTranscriptLocator(a, sess); path != "" {
			if id, err := runtimedevin.SessionIDFromExport(path); err == nil && id != "" {
				state := host.AgentBindState{
					Session: &host.AgentSessionIdentity{
						Kind:  host.AgentSessionKindID,
						Value: id,
					},
					LaunchPending: false,
				}
				out := commitIdentityBind(ctx, streams, a, sess, instance, runtimeID, actor, state)
				if out.Bound {
					if relErr := a.ReleaseAttachmentClaim(ctx, sess.ID, actor, evidence); relErr != nil {
						identitySkipped(streams, sess.ID, "ReleaseAttachmentClaim: %v", relErr)
					}
					out.Exited = true
					out.ExitEvidence = evidence
					return out
				}
			}
		}
	}
	if err := a.Exit(ctx, instance, actor, evidence); err != nil {
		identitySkipped(streams, sess.ID, "Exit: %v", err)
	}
	return identityBindOutcome{Exited: true, ExitEvidence: evidence}
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
	if transcript == "" && runtimeID == "devin" {
		transcript = devinTranscriptLocator(a, sess)
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
	var probe mintExitProbe
	if validator, ok := hostAd.(host.HostAttachmentValidator); ok {
		if claim, ok := sessionAttachmentClaim(a, sess); ok {
			probe = mintExitProbe{Validator: validator, Claim: claim}
		}
	}
	return bindStartingIdentity(ctx, streams, a, source, probe, sess, sess.Current,
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

// devinTranscriptLocator is the convention path stage1LeafAugmenter
// passes as `devin --export`. Correlate leaves TranscriptID empty for
// host kind id; bind fills this so conversation.list has a file to open.
// Assignment[0] is Stage-1's single leaf. Missing record or path error
// leaves the locator empty (honest miss, not a directory scan).
func devinTranscriptLocator(a *domain.Authority, sess domain.Session) string {
	rec, ok := a.SessionLaunchResolution(sess.ID)
	if !ok {
		return ""
	}
	var body launch.Record
	if err := json.Unmarshal(rec.Body, &body); err != nil || len(body.Assignment) == 0 {
		return ""
	}
	path, err := runtimedevin.ATIFPath(string(rec.ID), body.Assignment[0].Leaf)
	if err != nil {
		return ""
	}
	return path
}
