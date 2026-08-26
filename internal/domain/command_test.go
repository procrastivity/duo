package domain_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/store"
)

func TestAcceptPromptReplaysSameKey(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))

	first := mustAcceptPrompt(t, h.a, enrolled, "key-1", "digest_a")
	if first.Replay {
		t.Fatal("first AcceptPrompt reported itself as a replay")
	}
	if first.Command.State != domain.ResponsibilityQueued {
		t.Fatalf("state = %s, want queued", first.Command.State)
	}
	if first.Command.QueuePolicy != domain.QueueUntilSafe {
		t.Fatalf("queue_policy = %s, want queue_until_safe", first.Command.QueuePolicy)
	}
	if first.Command.Instance != enrolled.Instance {
		t.Fatalf("bound instance = %s, want %s (I-5)", first.Command.Instance, enrolled.Instance)
	}
	if first.Command.ExpiresAt == "" {
		t.Fatal("expires_at empty; no unlimited queue entry")
	}

	second, err := h.a.AcceptPrompt(ctx, promptReq(enrolled, "key-1", "digest_a"))
	if err != nil {
		t.Fatalf("replay AcceptPrompt: %v", err)
	}
	if !second.Replay {
		t.Fatal("same key + same digest did not replay")
	}
	if second.Command.ID != first.Command.ID {
		t.Fatalf("replay id = %s, want %s", second.Command.ID, first.Command.ID)
	}

	h.reopen()
	again, err := h.a.AcceptPrompt(ctx, promptReq(enrolled, "key-1", "digest_a"))
	if err != nil {
		t.Fatalf("replay after reopen: %v", err)
	}
	if !again.Replay || again.Command.ID != first.Command.ID {
		t.Fatalf("after reopen, replay = %+v, want command %s", again, first.Command.ID)
	}
}

func TestAcceptPromptConflictsOnDigestChange(t *testing.T) {
	h := newHarness(t)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	first := mustAcceptPrompt(t, h.a, enrolled, "key-1", "digest_a")

	_, err := h.a.AcceptPrompt(context.Background(), promptReq(enrolled, "key-1", "digest_b"))
	var conflict *domain.IdempotencyConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v, want IdempotencyConflictError", err)
	}
	if conflict.ExistingCommand != first.Command.ID {
		t.Fatalf("conflict command = %s, want %s", conflict.ExistingCommand, first.Command.ID)
	}
	if conflict.ExistingDigest != "digest_a" || conflict.RequestDigest != "digest_b" {
		t.Fatalf("digests = %s vs %s", conflict.ExistingDigest, conflict.RequestDigest)
	}
	if _, ok := h.a.Command(first.Command.ID); !ok {
		t.Fatal("conflict dropped the existing command")
	}
}

func TestExpireBeforeAttempt(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	clock := now
	h := newClockHarness(t, &clock)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))

	accepted, err := h.a.AcceptPrompt(ctx, domain.AcceptPromptRequest{
		Session:         enrolled.Session,
		Actor:           "user:beau",
		IdempotencyKey:  "key-exp",
		CanonicalDigest: "digest_exp",
		ExpiresAt:       now.Add(5 * time.Minute),
	})
	if err != nil {
		t.Fatalf("AcceptPrompt: %v", err)
	}

	clock = now.Add(6 * time.Minute)
	expired, err := h.a.ExpireIfDue(ctx, accepted.Command.ID, "authority")
	if err != nil {
		t.Fatalf("ExpireIfDue: %v", err)
	}
	if !expired {
		t.Fatal("ExpireIfDue returned false; want expired")
	}
	cmd, _ := h.a.Command(accepted.Command.ID)
	if cmd.State != domain.ResponsibilityExpired {
		t.Fatalf("state = %s, want expired", cmd.State)
	}

	_, err = h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathHost)
	if !errors.Is(err, domain.ErrCommandTerminal) {
		t.Fatalf("CreateAttempt after expiry: %v, want ErrCommandTerminal", err)
	}
}

func TestCreateAttemptExpiresDueCommand(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	clock := now
	h := newClockHarness(t, &clock)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	accepted, err := h.a.AcceptPrompt(ctx, domain.AcceptPromptRequest{
		Session:         enrolled.Session,
		Actor:           "user:beau",
		IdempotencyKey:  "key-exp2",
		CanonicalDigest: "digest_exp2",
		ExpiresAt:       now.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("AcceptPrompt: %v", err)
	}
	clock = now.Add(2 * time.Minute)
	_, err = h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathRuntime)
	if !errors.Is(err, domain.ErrCommandExpired) {
		t.Fatalf("CreateAttempt: %v, want ErrCommandExpired", err)
	}
	cmd, _ := h.a.Command(accepted.Command.ID)
	if cmd.State != domain.ResponsibilityExpired {
		t.Fatalf("state = %s, want expired", cmd.State)
	}
	if len(cmd.Attempts) != 0 {
		t.Fatalf("expired command recorded %d attempts, want 0", len(cmd.Attempts))
	}
}

func TestCrashAfterAttemptCreateDoesNotCreateSecondAttempt(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	accepted := mustAcceptPrompt(t, h.a, enrolled, "key-crash-attempt", "digest_crash_a")

	attemptID, err := h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathRuntime)
	if err != nil {
		t.Fatalf("CreateAttempt: %v", err)
	}

	h.reopen()
	cmd, ok := h.a.Command(accepted.Command.ID)
	if !ok {
		t.Fatal("command did not survive reopen after attempt create")
	}
	if cmd.State != domain.ResponsibilityAttempting {
		t.Fatalf("after reopen, state = %s, want attempting", cmd.State)
	}
	if len(cmd.Attempts) != 1 || cmd.Attempts[0].ID != attemptID {
		t.Fatalf("attempts = %+v, want one %s", cmd.Attempts, attemptID)
	}

	_, err = h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathHost)
	if !errors.Is(err, domain.ErrCommandAttempting) {
		t.Fatalf("second CreateAttempt: %v, want ErrCommandAttempting", err)
	}
	cmd, _ = h.a.Command(accepted.Command.ID)
	if len(cmd.Attempts) != 1 {
		t.Fatalf("crash recovery minted a second attempt (%d)", len(cmd.Attempts))
	}
}

func TestCrashBeforeTerminalCommitReconcilesUnknownEffect(t *testing.T) {
	ctx := context.Background()
	injected := errors.New("injected crash")
	h := newHarness(t)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	accepted := mustAcceptPrompt(t, h.a, enrolled, "key-crash-term", "digest_crash_t")
	attemptID, err := h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathHost)
	if err != nil {
		t.Fatalf("CreateAttempt: %v", err)
	}

	h.repo.InjectBeforeCommit(func() error { return injected })
	if err := h.a.CommitDelivered(ctx, accepted.Command.ID, attemptID, "authority"); !errors.Is(err, injected) {
		t.Fatalf("CommitDelivered crash: %v, want injected", err)
	}
	h.repo.InjectBeforeCommit(nil)

	live, _ := h.a.Command(accepted.Command.ID)
	if live.State != domain.ResponsibilityAttempting {
		t.Fatalf("live kernel after crashed terminal commit: state = %s, want attempting", live.State)
	}

	h.reopen()
	reloaded, _ := h.a.Command(accepted.Command.ID)
	if reloaded.State != domain.ResponsibilityAttempting {
		t.Fatalf("reloaded state = %s, want attempting", reloaded.State)
	}

	if err := h.a.ReconcileAttempt(ctx, accepted.Command.ID, attemptID, "authority", false); err != nil {
		t.Fatalf("ReconcileAttempt: %v", err)
	}
	failed, _ := h.a.Command(accepted.Command.ID)
	if failed.State != domain.ResponsibilityFailed {
		t.Fatalf("state = %s, want failed", failed.State)
	}
	if len(failed.Attempts) != 1 || failed.Attempts[0].EffectCertainty != domain.EffectUnknownEffect {
		t.Fatalf("attempt = %+v, want unknown_effect", failed.Attempts)
	}

	_, err = h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathHost)
	if !errors.Is(err, domain.ErrCommandTerminal) {
		t.Fatalf("CreateAttempt on failed command: %v, want ErrCommandTerminal", err)
	}
}

func TestCrashBeforeTerminalCommitProvedNoEffectRequeues(t *testing.T) {
	ctx := context.Background()
	injected := errors.New("injected crash")
	h := newHarness(t)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	accepted := mustAcceptPrompt(t, h.a, enrolled, "key-crash-ne", "digest_crash_ne")
	attemptID, err := h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathHost)
	if err != nil {
		t.Fatalf("CreateAttempt: %v", err)
	}

	h.repo.InjectBeforeCommit(func() error { return injected })
	if err := h.a.CommitDelivered(ctx, accepted.Command.ID, attemptID, "authority"); !errors.Is(err, injected) {
		t.Fatalf("CommitDelivered crash: %v, want injected", err)
	}
	h.repo.InjectBeforeCommit(nil)
	h.reopen()

	if err := h.a.ReconcileAttempt(ctx, accepted.Command.ID, attemptID, "authority", true); err != nil {
		t.Fatalf("ReconcileAttempt(no_effect): %v", err)
	}
	cmd, _ := h.a.Command(accepted.Command.ID)
	if cmd.State != domain.ResponsibilityQueued {
		t.Fatalf("state = %s, want queued", cmd.State)
	}
	if len(cmd.Attempts) != 1 || cmd.Attempts[0].EffectCertainty != domain.EffectNoEffect {
		t.Fatalf("attempt = %+v, want no_effect", cmd.Attempts)
	}

	second, err := h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathHost)
	if err != nil {
		t.Fatalf("retry CreateAttempt after no_effect: %v", err)
	}
	if second == attemptID {
		t.Fatal("retry reused the reconciled attempt id")
	}
	cmd, _ = h.a.Command(accepted.Command.ID)
	if len(cmd.Attempts) != 2 {
		t.Fatalf("after retry, %d attempts, want 2", len(cmd.Attempts))
	}
}

func TestAcceptanceCrashLeavesNoCommand(t *testing.T) {
	ctx := context.Background()
	injected := errors.New("injected crash")
	h := newHarness(t)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))

	h.repo.InjectBeforeCommit(func() error { return injected })
	_, err := h.a.AcceptPrompt(ctx, promptReq(enrolled, "key-crash-acc", "digest_crash_acc"))
	if !errors.Is(err, injected) {
		t.Fatalf("AcceptPrompt crash: %v, want injected", err)
	}
	h.repo.InjectBeforeCommit(nil)
	if _, ok := h.a.CommandByIdempotency("user:beau", string(enrolled.Session), "key-crash-acc"); ok {
		t.Fatal("crashed acceptance left an idempotency record in the kernel")
	}

	h.reopen()
	if _, ok := h.a.CommandByIdempotency("user:beau", string(enrolled.Session), "key-crash-acc"); ok {
		t.Fatal("crashed acceptance left an idempotency record after reopen")
	}
	accepted := mustAcceptPrompt(t, h.a, enrolled, "key-crash-acc", "digest_crash_acc")
	if accepted.Replay {
		t.Fatal("clean accept after crashed acceptance reported a replay")
	}
}

func TestCommandDoesNotMigrateToReplacementInstance(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	fp := herdrFingerprint("w1:p1", "term_a", 4242)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", fp))
	accepted := mustAcceptPrompt(t, h.a, enrolled, "key-i5", "digest_i5")
	bound := accepted.Command.Instance

	if err := h.a.Exit(ctx, bound, "host", "process exited"); err != nil {
		t.Fatalf("Exit: %v", err)
	}
	replacement, err := h.a.Resume(ctx, domain.ResumeRequest{
		Session:     enrolled.Session,
		Actor:       "user:beau",
		Attestation: domain.Attestation{Source: domain.SourceOwner, Subject: "user:beau"},
		Reason:      "replacement process",
	})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if replacement == bound {
		t.Fatal("resume reused the exited instance id")
	}

	_, err = h.a.CreateAttempt(ctx, accepted.Command.ID, "authority", domain.PromptPathHost)
	if !errors.Is(err, domain.ErrInstanceExited) {
		t.Fatalf("CreateAttempt against exited bound instance: %v, want ErrInstanceExited", err)
	}
	cmd, _ := h.a.Command(accepted.Command.ID)
	if cmd.Instance != bound {
		t.Fatalf("command rebound from %s to %s; I-5 forbids migration", bound, cmd.Instance)
	}
	if cmd.State != domain.ResponsibilityFailed {
		t.Fatalf("state = %s, want failed (target exit)", cmd.State)
	}
}

func TestQueuePolicyOtherThanQueueUntilSafeIsRefused(t *testing.T) {
	h := newHarness(t)
	enrolled := mustEnroll(t, h.a, candidate("/home/dev/Code/duo", herdrFingerprint("w1:p1", "term_a", 4242)))
	req := promptReq(enrolled, "key-pol", "digest_pol")
	req.QueuePolicy = "require_ready"
	_, err := h.a.AcceptPrompt(context.Background(), req)
	if !errors.Is(err, domain.ErrQueuePolicyUnsupported) {
		t.Fatalf("err = %v, want ErrQueuePolicyUnsupported", err)
	}
}

func promptReq(enrolled domain.EnrollResult, key, digest string) domain.AcceptPromptRequest {
	return domain.AcceptPromptRequest{
		Session:         enrolled.Session,
		Actor:           "user:beau",
		IdempotencyKey:  key,
		CanonicalDigest: digest,
	}
}

func mustAcceptPrompt(t *testing.T, a *domain.Authority, enrolled domain.EnrollResult, key, digest string) domain.AcceptPromptResult {
	t.Helper()
	res, err := a.AcceptPrompt(context.Background(), promptReq(enrolled, key, digest))
	if err != nil {
		t.Fatalf("AcceptPrompt: %v", err)
	}
	return res
}

type clockHarness struct {
	t     *testing.T
	path  string
	s     *store.Store
	repo  *storerepo.Repo
	a     *domain.Authority
	clock *time.Time
}

func newClockHarness(t *testing.T, clock *time.Time) *clockHarness {
	t.Helper()
	h := &clockHarness{t: t, path: filepath.Join(t.TempDir(), "duo.db"), clock: clock}
	s, err := store.OpenAuthority(h.path)
	if err != nil {
		t.Fatalf("OpenAuthority: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	repo := storerepo.New(s)
	a, err := domain.Open(context.Background(), repo, domain.WithClock(func() time.Time { return *h.clock }))
	if err != nil {
		t.Fatalf("domain.Open: %v", err)
	}
	h.s, h.repo, h.a = s, repo, a
	return h
}
