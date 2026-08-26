package delivery_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/delivery"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/promptpath"
	"github.com/procrastivity/duo/internal/runtime"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
	"github.com/procrastivity/duo/internal/store"
)

const (
	hostIntegration  = "fake-host"
	agentIntegration = "fake-runtime"
	externalSession  = "ext-agent-1"
)

func TestDuoCreatedSettledIdleSelectsRuntimePath(t *testing.T) {
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSession, idleObs())
	hostAd := hostfake.New(hostIntegration)

	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-runtime", "digest-runtime")

	res := mustRelease(t, composer(h.a, rt, hostAd, h.now.Add(11*time.Second)), cmd.ID)
	if res.Held {
		t.Fatalf("held: %+v", res.Hold)
	}
	if res.Path.Kind != promptpath.KindRuntime {
		t.Fatalf("path = %s, want runtime (Claude socket when present)", res.Path.Kind)
	}
	if res.Command.State != domain.ResponsibilityDelivered {
		t.Fatalf("state = %s, want delivered", res.Command.State)
	}
	if len(res.Command.Attempts) != 1 || res.Command.Attempts[0].PathKind != domain.PromptPathRuntime {
		t.Fatalf("attempts = %+v, want one runtime attempt", res.Command.Attempts)
	}
}

func TestAttachedPaneHolds(t *testing.T) {
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSession, idleObs())
	hostAd := hostfake.New(hostIntegration)

	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-att", "digest-att")

	res := mustRelease(t, composer(h.a, rt, hostAd, h.now.Add(11*time.Second)), cmd.ID, withEvidence(delivery.Evidence{HumanAttached: true}))
	if !res.Held {
		t.Fatal("attached pane auto-released; want hold")
	}
	if res.Hold.Code != delivery.HoldCode {
		t.Fatalf("hold code = %q, want %s", res.Hold.Code, delivery.HoldCode)
	}
	if res.Command.State != domain.ResponsibilityQueued {
		t.Fatalf("state = %s, want queued", res.Command.State)
	}
	if len(res.Command.Attempts) != 0 {
		t.Fatalf("held command recorded %d attempts", len(res.Command.Attempts))
	}
}

func TestUnknownOriginPaneHolds(t *testing.T) {
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSession, idleObs())
	hostAd := hostfake.New(hostIntegration)

	enrolled := mustEnrollUnknown(t, h.a, "w1:p2", "term_b", 5353)
	cmd := mustAccept(t, h.a, enrolled.Session, enrolled.Instance, "key-unk", "digest-unk")

	if delivery.DuoCreated(h.a, enrolled.Session) {
		t.Fatal("enrolled pane stamped Duo-created; enroll writes host-report, not launch-plan")
	}

	res := mustRelease(t, composer(h.a, rt, hostAd, h.now.Add(11*time.Second)), cmd.ID)
	if !res.Held {
		t.Fatal("unknown-origin pane auto-released; want hold")
	}
	if res.Hold.Code != delivery.HoldCode {
		t.Fatalf("hold code = %q, want %s", res.Hold.Code, delivery.HoldCode)
	}
}

func TestDraftHoldsEvenOnRuntimePath(t *testing.T) {
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSession, idleObs())
	hostAd := hostfake.New(hostIntegration)

	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-draft", "digest-draft")

	res := mustRelease(t, composer(h.a, rt, hostAd, h.now.Add(11*time.Second)), cmd.ID, withEvidence(delivery.Evidence{Draft: true}))
	if !res.Held {
		t.Fatal("draft evidence did not hold on the socket path")
	}
	if res.Hold.Code != delivery.HoldCode {
		t.Fatalf("hold code = %q, want %s", res.Hold.Code, delivery.HoldCode)
	}
	if len(res.Command.Attempts) != 0 {
		t.Fatal("draft hold invoked an adapter")
	}
}

func TestConditionOnlyRuntimeSelectsHostPath(t *testing.T) {
	h := newHarness(t)
	pi := &conditionOnlyRuntime{obs: idleObs()}
	hostAd := hostfake.New(hostIntegration)

	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-pi", "digest-pi")

	res := mustRelease(t, composer(h.a, pi, hostAd, h.now.Add(11*time.Second)), cmd.ID)
	if res.Held {
		t.Fatalf("held: %+v", res.Hold)
	}
	if res.Path.Kind != promptpath.KindHost {
		t.Fatalf("path = %s, want host (no runtime offer)", res.Path.Kind)
	}
	if res.Command.State != domain.ResponsibilityDelivered {
		t.Fatalf("state = %s, want delivered", res.Command.State)
	}
	if len(res.Command.Attempts) != 1 || res.Command.Attempts[0].PathKind != domain.PromptPathHost {
		t.Fatalf("attempts = %+v, want one host attempt", res.Command.Attempts)
	}
}

func TestPiSelectsRuntimePath(t *testing.T) {
	h := newHarness(t)
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	sockPath, err := runtimepi.InjectSocketPath(externalSession)
	if err != nil {
		t.Fatalf("InjectSocketPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir inject socket dir: %v", err)
	}
	startAdmitStandIn(t, sockPath, true)

	rt := runtimepi.New(agentIntegration)
	hostAd := hostfake.New(hostIntegration)

	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-pi-rt", "digest-pi-rt")

	res := mustRelease(t, composer(h.a, rt, hostAd, h.now.Add(11*time.Second)), cmd.ID)
	if res.Held {
		t.Fatalf("held: %+v", res.Hold)
	}
	if res.Path.Kind != promptpath.KindRuntime {
		t.Fatalf("path = %s, want runtime (Pi inject socket)", res.Path.Kind)
	}
	if res.Command.State != domain.ResponsibilityDelivered {
		t.Fatalf("state = %s, want delivered", res.Command.State)
	}
	if len(res.Command.Attempts) != 1 || res.Command.Attempts[0].PathKind != domain.PromptPathRuntime {
		t.Fatalf("attempts = %+v, want one runtime attempt", res.Command.Attempts)
	}
}

func TestWorkingHolds(t *testing.T) {
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSession, runtime.ConditionObservation{
		Value:      runtime.ConditionWorking,
		Confidence: runtime.ConditionConfidenceInferred,
		Freshness:  runtime.ConditionFreshnessFresh,
	})
	hostAd := hostfake.New(hostIntegration)
	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-work", "digest-work")

	res := mustRelease(t, composer(h.a, rt, hostAd, h.now.Add(11*time.Second)), cmd.ID)
	if !res.Held {
		t.Fatal("working condition auto-released")
	}
}

func TestUnknownSettledDuoCreatedAutoReleases(t *testing.T) {
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	// No SeedCondition: snapshot is unknown. Carve-out still applies.
	hostAd := hostfake.New(hostIntegration)
	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-unkidle", "digest-unkidle")

	res := mustRelease(t, composer(h.a, rt, hostAd, h.now.Add(11*time.Second)), cmd.ID)
	if res.Held {
		t.Fatalf("unknown-but-settled Duo-created held: %+v", res.Hold)
	}
	if res.Path.Kind != promptpath.KindRuntime {
		t.Fatalf("path = %s, want runtime", res.Path.Kind)
	}
}

func TestAttributedQuietPeriodHolds(t *testing.T) {
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSession, idleObs())
	hostAd := hostfake.New(hostIntegration)
	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-quiet", "digest-quiet")

	now := h.now.Add(11 * time.Second)
	res := mustRelease(t, composer(h.a, rt, hostAd, now), cmd.ID, withEvidence(delivery.Evidence{
		LastHumanInput: now.Add(-5 * time.Second),
	}))
	if !res.Held {
		t.Fatal("attributed quiet period did not hold")
	}
}

func TestHerdrQuietPeriodDoesNotFireWithoutAttribution(t *testing.T) {
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSession, idleObs())
	hostAd := hostfake.New(hostIntegration)
	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-noattr", "digest-noattr")

	res := mustRelease(t, composer(h.a, rt, hostAd, h.now.Add(11*time.Second)), cmd.ID)
	if res.Held {
		t.Fatalf("zero LastHumanInput held via quiet-gate: %+v", res.Hold)
	}
}

func TestCommandDoesNotRebind(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	rt := runtimefake.New(agentIntegration)
	rt.SeedCondition(externalSession, idleObs())
	hostAd := hostfake.New(hostIntegration)
	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-i5", "digest-i5")
	bound := cmd.Instance

	if err := h.a.Exit(ctx, bound, "host", "process exited"); err != nil {
		t.Fatalf("Exit: %v", err)
	}
	replacement, err := h.a.Resume(ctx, domain.ResumeRequest{
		Session:     launched.Session,
		Actor:       "user:beau",
		Attestation: domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
		Reason:      "replacement",
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if replacement == bound {
		t.Fatal("resume reused the bound instance")
	}

	_, err = composer(h.a, rt, hostAd, h.now.Add(11*time.Second)).Release(ctx, delivery.Request{
		Command: cmd.ID,
		Text:    "go",
		Actor:   "user:beau",
	})
	if err == nil {
		t.Fatal("Release against exited bound instance succeeded")
	}
	got, _ := h.a.Command(cmd.ID)
	if got.Instance != bound {
		t.Fatalf("command rebound from %s to %s", bound, got.Instance)
	}
}

func TestHostNoEffectRequeues(t *testing.T) {
	h := newHarness(t)
	pi := &conditionOnlyRuntime{obs: idleObs()}
	hostAd := hostfake.New(hostIntegration)
	hostAd.ScriptPromptOutcome(host.PromptOutcomeNoEffect)

	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	cmd := mustAccept(t, h.a, launched.Session, launched.Instance, "key-ne", "digest-ne")

	res := mustRelease(t, composer(h.a, pi, hostAd, h.now.Add(11*time.Second)), cmd.ID)
	if res.Held {
		t.Fatalf("held: %+v", res.Hold)
	}
	if res.Command.State != domain.ResponsibilityQueued {
		t.Fatalf("state = %s, want queued after no_effect", res.Command.State)
	}
	if len(res.Command.Attempts) != 1 || res.Command.Attempts[0].EffectCertainty != domain.EffectNoEffect {
		t.Fatalf("attempt = %+v, want no_effect", res.Command.Attempts)
	}
}

func TestDuoCreatedStampIsLaunchPlan(t *testing.T) {
	h := newHarness(t)
	launched := mustLaunchDuoCreated(t, h.a, "w1:p1", "term_a", 4242)
	if !delivery.DuoCreated(h.a, launched.Session) {
		t.Fatal("launch-plan Bind did not stamp Duo-created")
	}
	cand := domain.Candidate{
		Fingerprint: herdrFingerprint("w1:p9", "term_z", 9999),
		RootPath:    "/home/dev/Code/duo",
	}
	enrolled, err := h.a.Enroll(context.Background(), domain.EnrollRequest{
		Candidate: cand, Actor: "user:beau",
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	if delivery.DuoCreated(h.a, enrolled.Session) {
		t.Fatal("enrolled attachment must not count as Duo-created")
	}
}

type harness struct {
	t    *testing.T
	a    *domain.Authority
	now  time.Time
	path string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	now := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	h := &harness{t: t, now: now, path: filepath.Join(t.TempDir(), "duo.db")}
	s, err := store.OpenAuthority(h.path)
	if err != nil {
		t.Fatalf("OpenAuthority: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	a, err := domain.Open(context.Background(), storerepo.New(s), domain.WithClock(func() time.Time { return h.now }))
	if err != nil {
		t.Fatalf("domain.Open: %v", err)
	}
	h.a = a
	return h
}

func composer(a *domain.Authority, rt any, hostAd host.HostPromptProvider, now time.Time) *delivery.Composer {
	return &delivery.Composer{
		Authority: a,
		Runtime:   rt,
		Host:      hostAd,
		Now:       func() time.Time { return now },
	}
}

type releaseOpt func(*delivery.Request)

func withEvidence(ev delivery.Evidence) releaseOpt {
	return func(r *delivery.Request) { r.Evidence = ev }
}

func mustRelease(t *testing.T, c *delivery.Composer, id domain.CommandID, opts ...releaseOpt) delivery.Result {
	t.Helper()
	req := delivery.Request{Command: id, Text: "do the work", Actor: "user:beau"}
	for _, opt := range opts {
		opt(&req)
	}
	res, err := c.Release(context.Background(), req)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	return res
}

func mustAccept(t *testing.T, a *domain.Authority, session domain.SessionID, instance domain.InstanceID, key, digest string) domain.PromptCommand {
	t.Helper()
	res, err := a.AcceptPrompt(context.Background(), domain.AcceptPromptRequest{
		Session:         session,
		Instance:        instance,
		Actor:           "user:beau",
		IdempotencyKey:  key,
		CanonicalDigest: digest,
	})
	if err != nil {
		t.Fatalf("AcceptPrompt: %v", err)
	}
	return res.Command
}

func mustLaunchDuoCreated(t *testing.T, a *domain.Authority, pane, terminal string, pid int) domain.LaunchResult {
	t.Helper()
	ctx := context.Background()
	launched, err := a.Launch(ctx, domain.LaunchRequest{
		RootPath: "/home/dev/Code/duo", Actor: "user:beau", Reason: "test launch",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	fp := herdrFingerprint(pane, terminal, pid)
	if err := a.Bind(ctx, domain.BindRequest{
		Session:      launched.Session,
		Actor:        "host",
		Attestation:  domain.Attestation{Source: domain.SourceLaunchPlan},
		Fingerprint:  &fp,
		AgentSession: domain.AgentSessionRef{IntegrationInstance: agentIntegration, SessionID: externalSession},
		Transcript:   "/tmp/fake-transcript.jsonl",
	}); err != nil {
		t.Fatalf("Bind launch-plan: %v", err)
	}
	if err := a.MarkLive(ctx, launched.Instance, "host", "process is live"); err != nil {
		t.Fatalf("MarkLive: %v", err)
	}
	return launched
}

func mustEnrollUnknown(t *testing.T, a *domain.Authority, pane, terminal string, pid int) domain.EnrollResult {
	t.Helper()
	cand := domain.Candidate{
		Fingerprint:  herdrFingerprint(pane, terminal, pid),
		RootPath:     "/home/dev/Code/duo",
		AgentSession: domain.AgentSessionRef{IntegrationInstance: agentIntegration, SessionID: externalSession},
		Transcript:   "/tmp/fake-transcript.jsonl",
	}
	res, err := a.Enroll(context.Background(), domain.EnrollRequest{
		Candidate:   cand,
		Actor:       "user:beau",
		Attestation: domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
	})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	return res
}

func herdrFingerprint(pane, terminalID string, pid int) domain.Fingerprint {
	return domain.Fingerprint{
		IntegrationInstance: hostIntegration,
		Epoch: domain.HostEpoch{
			Kind:  "herdr.terminal_id",
			Value: terminalID,
			Scope: domain.EpochScopePane,
		},
		Container: pane,
		Process: domain.ProcessBirth{
			PID:        pid,
			StartedAt:  "2026-08-26T12:00:0" + strconv.Itoa(pid%10) + ".000Z",
			Executable: "/usr/bin/claude",
		},
	}
}

func idleObs() runtime.ConditionObservation {
	return runtime.ConditionObservation{
		Value:      runtime.ConditionIdle,
		Confidence: runtime.ConditionConfidenceInferred,
		Freshness:  runtime.ConditionFreshnessFresh,
		ComputedAt: time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC),
	}
}

// conditionOnlyRuntime implements ConditionProvider but not
// RuntimePromptProvider. collectOffers skips it for runtime path selection.
type conditionOnlyRuntime struct {
	obs runtime.ConditionObservation
}

var _ runtime.ConditionProvider = (*conditionOnlyRuntime)(nil)

func (r *conditionOnlyRuntime) ObserveCondition(context.Context, runtime.ConditionObservationRequest) (runtime.ConditionObservationStream, error) {
	return runtime.NewStaticConditionStream(r.obs), nil
}

func shortRuntimeDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pi")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// startAdmitStandIn listens and, if greet is true, writes one NDJSON
// connect-line before reading the prompt frame. The connection stays open
// after read so peerGone sees the peer still there.
func startAdmitStandIn(t *testing.T, sockPath string, greet bool) {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen %s: %v", sockPath, err)
	}
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		if greet {
			_, _ = conn.Write([]byte(`{"sessionId":"x","idle":true}` + "\n"))
		}
		buf := make([]byte, 64*1024)
		_, _ = conn.Read(buf)
		<-done
	}()
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
		wg.Wait()
	})
}
