package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
)

// TestExitIsFinal is §5.3. Exit releases the active claim, retires the
// instance's live correlations, and closes the instance permanently: no
// later verb and no late report can move it again.
func TestExitIsFinal(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))

	if err := h.a.Exit(ctx, res.Instance, "host", "herdr reported pane process exit"); err != nil {
		t.Fatalf("Exit: %v", err)
	}

	inst, _ := h.a.Instance(res.Instance)
	if inst.State != domain.InstanceExited {
		t.Fatalf("instance state = %s, want exited", inst.State)
	}
	if _, held := h.a.ActiveClaim(fp.ClaimRef()); held {
		t.Fatal("exit did not release the active claim")
	}
	session, _ := h.a.Session(res.Session)
	if session.State != domain.SessionInactive {
		t.Fatalf("session state = %s, want inactive", session.State)
	}
	for _, c := range h.a.Correlations(domain.TargetInstance, string(res.Instance)) {
		if c.Status != domain.CorrelationRetired {
			t.Fatalf("correlation %s (%s) is %s after exit, want retired",
				c.ID, c.ExternalKind, c.Status)
		}
	}

	// Every path back to a live state is refused.
	if err := h.a.Stop(ctx, res.Instance, "user:beau", "too late"); !errors.Is(err, domain.ErrInstanceExited) {
		t.Fatalf("Stop after exit returned %v, want ErrInstanceExited", err)
	}
	if err := h.a.MarkLive(ctx, res.Instance, "hook", "late SessionStart"); !errors.Is(err, domain.ErrInstanceExited) {
		t.Fatalf("MarkLive after exit returned %v, want ErrInstanceExited", err)
	}
	if err := h.a.Exit(ctx, res.Instance, "host", "again"); !errors.Is(err, domain.ErrInstanceExited) {
		t.Fatalf("second Exit returned %v, want ErrInstanceExited", err)
	}

	// §6.7: a queued hook arriving after exit enriches history and changes
	// nothing else.
	err := h.a.Bind(ctx, domain.BindRequest{
		Session: res.Session,
		Actor:   "hook",
		Attestation: domain.Attestation{
			Source: domain.SourceInstanceClaim, Credential: res.Credential, Instance: res.Instance,
		},
		AgentSession: domain.AgentSessionRef{IntegrationInstance: "claude@default", SessionID: "agent-abc"},
	})
	if !errors.Is(err, domain.ErrInstanceExited) {
		t.Fatalf("late bind returned %v, want ErrInstanceExited", err)
	}
	if inst, _ := h.a.Instance(res.Instance); inst.State != domain.InstanceExited {
		t.Fatalf("late report moved the instance to %s", inst.State)
	}

	// The finality survives a restart.
	h.reopen()
	if inst, _ := h.a.Instance(res.Instance); inst.State != domain.InstanceExited {
		t.Fatalf("after reopen, instance state = %s, want exited", inst.State)
	}
	if n := len(h.a.Recovering()); n != 0 {
		t.Fatalf("an exited instance entered the recovering view (%d recovering)", n)
	}
}

// TestNewProcessInTheSamePaneIsANewInstance is §6.4 and §5.3's last bullet.
// The released claim key can be taken again — by a new Duo session with a new
// runtime instance, never by reopening the exited one.
func TestNewProcessInTheSamePaneIsANewInstance(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	// A degraded (process-birth-free) fingerprint is the hard case: the
	// claim key is identical across the two executions, so only the
	// generation separates them.
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	fp.Process = domain.ProcessBirth{}

	first := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	if err := h.a.Exit(ctx, first.Instance, "host", "process exited"); err != nil {
		t.Fatalf("Exit: %v", err)
	}
	second := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))

	if second.Instance == first.Instance {
		t.Fatal("a replacement process reused the exited runtime-instance id")
	}
	if second.Session == first.Session {
		t.Fatal("pane-id reuse alone continued the duo session")
	}
	claim, held := h.a.ActiveClaim(fp.ClaimRef())
	if !held || claim.Instance != second.Instance {
		t.Fatalf("claim holder = %+v, want instance %s", claim, second.Instance)
	}
	if claim.Generation != 1 {
		t.Fatalf("re-taken claim generation = %d, want 1", claim.Generation)
	}
}

// TestStopIsARequest is §5.2: "Do not report exit until process-lifecycle
// evidence proves it."
func TestStopIsARequest(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))

	if err := h.a.Stop(ctx, res.Instance, "user:beau", "user asked"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	inst, _ := h.a.Instance(res.Instance)
	if inst.State != domain.InstanceStopRequested {
		t.Fatalf("instance state = %s, want stop_requested", inst.State)
	}
	if _, held := h.a.ActiveClaim(fp.ClaimRef()); !held {
		t.Fatal("a stop request released the active claim")
	}
	if s, _ := h.a.Session(res.Session); s.State != domain.SessionActive {
		t.Fatalf("session state = %s after a stop request, want active", s.State)
	}
}

// TestDetachKeepsTheRuntime is §5.2: detach disables Duo's attachment while
// the runtime continues; the claim reservation stays.
func TestDetachKeepsTheRuntime(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))

	if err := h.a.Detach(ctx, res.Session, "user:beau", "detach"); err != nil {
		t.Fatalf("Detach: %v", err)
	}
	if view, _ := h.a.View(res.Session); view != domain.ViewDetached {
		t.Fatalf("view = %s, want detached", view)
	}
	if inst, _ := h.a.Instance(res.Instance); inst.State != domain.InstanceLive {
		t.Fatalf("detach changed the instance state to %s", inst.State)
	}
	if _, held := h.a.ActiveClaim(fp.ClaimRef()); !held {
		t.Fatal("detach released the active claim reservation")
	}

	// Reattach revalidates: the same execution restores the attachment
	// without a new runtime-instance ID.
	if err := h.a.Reattach(ctx, res.Session, "user:beau", fp); err != nil {
		t.Fatalf("Reattach: %v", err)
	}
	if view, _ := h.a.View(res.Session); view != domain.ViewAttached {
		t.Fatalf("view = %s, want attached", view)
	}
	if s, _ := h.a.Session(res.Session); s.Current != res.Instance {
		t.Fatalf("reattach changed the current instance to %s", s.Current)
	}
}

// TestReattachRefusesADifferentExecution is §6.4: a pane that now holds a
// different process is not the session Duo detached from.
func TestReattachRefusesADifferentExecution(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	if err := h.a.Detach(ctx, res.Session, "user:beau", "detach"); err != nil {
		t.Fatalf("Detach: %v", err)
	}

	replacement := herdrFingerprint("w1:p1", "term_a", 9999) // same pane, new process birth
	err := h.a.Reattach(ctx, res.Session, "user:beau", replacement)

	var conflict *domain.ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("Reattach returned %v, want a ConflictError", err)
	}
	if view, _ := h.a.View(res.Session); view != domain.ViewDetached {
		t.Fatalf("view = %s after a refused reattach, want detached", view)
	}
}

// TestRestartKeepsTheSessionAndReplacesTheInstance is §5.2 and §6.4:
// "Runtime-instance IDs never carry across the boundary."
func TestRestartKeepsTheSessionAndReplacesTheInstance(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))

	if _, err := h.a.Restart(ctx, domain.RestartRequest{
		Session: res.Session, Actor: "user:beau", Reason: "restart policy",
	}); !errors.Is(err, domain.ErrIntentRequired) {
		t.Fatal("restart without explicit intent was accepted")
	}

	next, err := h.a.Restart(ctx, domain.RestartRequest{
		Session:      res.Session,
		Actor:        "user:beau",
		Attestation:  domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
		ExitEvidence: "herdr reported the pane process exited",
		Reason:       "restart policy",
	})
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if next == res.Instance {
		t.Fatal("restart reused the runtime-instance id")
	}
	if old, _ := h.a.Instance(res.Instance); old.State != domain.InstanceExited {
		t.Fatalf("old instance state = %s, want exited", old.State)
	}
	s, _ := h.a.Session(res.Session)
	if s.Current != next {
		t.Fatalf("current instance = %s, want %s", s.Current, next)
	}
	if s.State != domain.SessionActive {
		t.Fatalf("session state = %s after restart, want active", s.State)
	}
	if _, held := h.a.ActiveClaim(fp.ClaimRef()); held {
		t.Fatal("the old execution's claim survived the restart")
	}
}

// TestResumeNeedsIntentAndKeepsTheActor is §5.2 and §6.6.
func TestResumeNeedsIntentAndKeepsTheActor(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	actor := domain.ActorID("actor_reviewer")
	owner := domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"}

	res, err := h.a.Enroll(ctx, domain.EnrollRequest{
		Candidate:   candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)),
		Actor:       "user:beau",
		BindActor:   actor,
		Attestation: owner,
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if err := h.a.Exit(ctx, res.Instance, "host", "process exited"); err != nil {
		t.Fatalf("Exit: %v", err)
	}

	if _, err := h.a.Resume(ctx, domain.ResumeRequest{Session: res.Session, Actor: "hook"}); !errors.Is(err, domain.ErrIntentRequired) {
		t.Fatalf("Resume without intent returned %v, want ErrIntentRequired", err)
	}
	next, err := h.a.Resume(ctx, domain.ResumeRequest{
		Session: res.Session, Actor: "user:beau", Attestation: owner, Reason: "continue the review",
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if next == res.Instance {
		t.Fatal("resume reused the exited runtime-instance id")
	}
	s, _ := h.a.Session(res.Session)
	if s.BoundActor != actor {
		t.Fatalf("bound actor = %q after resume, want %s", s.BoundActor, actor)
	}
	if s.State != domain.SessionActive {
		t.Fatalf("session state = %s after resume, want active", s.State)
	}
	if inst, _ := h.a.Instance(next); inst.State != domain.InstanceStarting {
		t.Fatalf("resumed instance state = %s, want starting", inst.State)
	}
}

// TestAgentActorHasOneLiveBinding is §3.4's "one live runtime binding for an
// agent actor at a time".
func TestAgentActorHasOneLiveBinding(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	actor := domain.ActorID("actor_reviewer")
	owner := domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"}

	if _, err := h.a.Enroll(ctx, domain.EnrollRequest{
		Candidate:   candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)),
		Actor:       "user:beau",
		BindActor:   actor,
		Attestation: owner,
	}); err != nil {
		t.Fatalf("first Enroll: %v", err)
	}
	_, err := h.a.Enroll(ctx, domain.EnrollRequest{
		Candidate:   candidate("/home/dev/Code/duo", herdrFingerprint("w1:p2", "term_b", 5353)),
		Actor:       "user:beau",
		BindActor:   actor,
		Attestation: owner,
	})
	if !errors.Is(err, domain.ErrActorBound) {
		t.Fatalf("second binding returned %v, want ErrActorBound", err)
	}
}

// TestBindNeedsTheInstanceCredential is §6.1 and §6.2's normal path: Duo
// enrolls or launches, hands out an instance-scoped credential, and only a
// report carrying it can attach the agent-runtime session to that instance.
func TestBindNeedsTheInstanceCredential(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	agent := domain.AgentSessionRef{IntegrationInstance: "claude@default", SessionID: "agent-abc"}

	bad := domain.BindRequest{
		Session:      res.Session,
		Actor:        "hook",
		Attestation:  domain.Attestation{Source: domain.SourceInstanceClaim, Credential: "duocred_wrong", Instance: res.Instance},
		AgentSession: agent,
	}
	if err := h.a.Bind(ctx, bad); !errors.Is(err, domain.ErrNotAttested) {
		t.Fatalf("bind with a wrong credential returned %v, want ErrNotAttested", err)
	}
	if _, held := h.a.ActiveClaim(agent.ClaimRef()); held {
		t.Fatal("a rejected report seized the agent-session claim")
	}

	good := bad
	good.Attestation.Credential = res.Credential
	good.Transcript = "/home/dev/.claude/projects/duo/agent-abc.jsonl"
	if err := h.a.Bind(ctx, good); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	claim, held := h.a.ActiveClaim(agent.ClaimRef())
	if !held || claim.Instance != res.Instance {
		t.Fatalf("agent-session claim = %+v, want instance %s", claim, res.Instance)
	}
	var kinds []string
	for _, c := range h.a.Correlations(domain.TargetInstance, string(res.Instance)) {
		kinds = append(kinds, c.ExternalKind)
	}
	if !contains(kinds, "agent.session") || !contains(kinds, "transcript") {
		t.Fatalf("instance correlations = %v, want agent.session and transcript", kinds)
	}
}

// TestArchiveRefusesALiveSession is §5.2: "A session with a live instance
// cannot be archived."
func TestArchiveRefusesALiveSession(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	res := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))

	if err := h.a.Archive(ctx, res.Session, "user:beau", "tidy up"); err == nil {
		t.Fatal("archived a session with a live runtime instance")
	}
	if err := h.a.Exit(ctx, res.Instance, "host", "process exited"); err != nil {
		t.Fatalf("Exit: %v", err)
	}
	if err := h.a.Archive(ctx, res.Session, "user:beau", "tidy up"); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if view, _ := h.a.View(res.Session); view != domain.ViewArchived {
		t.Fatalf("view = %s, want archived", view)
	}
	if err := h.a.Remove(ctx, res.Session, "user:beau", "done"); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Removal keeps the tombstone: the ID stays known so a dangling
	// reference can be explained, and it is never reissued.
	h.reopen()
	s, ok := h.a.Session(res.Session)
	if !ok {
		t.Fatal("a removed session left no tombstone")
	}
	if s.State != domain.SessionRemoved {
		t.Fatalf("tombstone state = %s, want removed", s.State)
	}
}

// TestLaunchCreatesIdentityBeforeTheProcess is §6.2: the Duo IDs exist
// before the integration is called, so the returned external identifiers can
// be recorded as correlations on an object that already has an identity.
func TestLaunchCreatesIdentityBeforeTheProcess(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	launched, err := h.a.Launch(ctx, domain.LaunchRequest{
		RootPath: "/home/dev/Code/duo", Actor: "user:beau", Reason: "composition A",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if launched.Session == "" || launched.Instance == "" || launched.Credential == "" {
		t.Fatalf("launch returned %+v, want a session, an instance, and a credential", launched)
	}
	inst, _ := h.a.Instance(launched.Instance)
	if inst.State != domain.InstanceStarting {
		t.Fatalf("launched instance state = %s, want starting", inst.State)
	}
	if h.a.ActiveClaims() != 0 {
		t.Fatal("launch claimed a live-runtime fingerprint before the process existed")
	}

	// The host reports back: bind the container and its epoch, then mark it
	// live.
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	if err := h.a.Bind(ctx, domain.BindRequest{
		Session:     launched.Session,
		Actor:       "host",
		Attestation: domain.Attestation{Source: domain.SourceLaunchPlan},
		Fingerprint: &fp,
	}); err != nil {
		t.Fatalf("Bind: %v", err)
	}
	if err := h.a.MarkLive(ctx, launched.Instance, "host", "process is live"); err != nil {
		t.Fatalf("MarkLive: %v", err)
	}
	if inst, _ := h.a.Instance(launched.Instance); inst.State != domain.InstanceLive {
		t.Fatalf("instance state = %s, want live", inst.State)
	}
	if view, _ := h.a.View(launched.Session); view != domain.ViewAttached {
		t.Fatalf("view = %s, want attached", view)
	}

	// And a second enrollment of that same live runtime finds it already
	// enrolled rather than creating a second session.
	repeat := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	if !repeat.Repeat || repeat.Session != launched.Session {
		t.Fatalf("enrolling the launched runtime returned %+v, want a repeat of %s",
			repeat, launched.Session)
	}
}

// TestFailedLaunchLeavesAnInactiveSession is §5.2: "A failed launch exits
// that instance and leaves an inactive session with failure history."
func TestFailedLaunchLeavesAnInactiveSession(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	launched, err := h.a.Launch(ctx, domain.LaunchRequest{
		RootPath: "/home/dev/Code/duo", Actor: "user:beau",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if err := h.a.LaunchFailed(ctx, launched.Instance, "host", "exec: no such file"); err != nil {
		t.Fatalf("LaunchFailed: %v", err)
	}
	if inst, _ := h.a.Instance(launched.Instance); inst.State != domain.InstanceExited {
		t.Fatalf("instance state = %s, want exited", inst.State)
	}
	if s, _ := h.a.Session(launched.Session); s.State != domain.SessionInactive {
		t.Fatalf("session state = %s, want inactive", s.State)
	}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
