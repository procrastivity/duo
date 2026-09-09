package amp_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/amp"
)

func TestPromptPathIsExactNativeNotComposerSafe(t *testing.T) {
	r := amp.New(testIntegrationInstanceID)
	got, err := r.PromptPath(context.Background(), runtime.RuntimeBinding{ExternalAgentSessionID: "T-loop-c-ok"})
	if err != nil {
		t.Fatalf("PromptPath: %v", err)
	}
	if got.Quality != "exact" || got.Realization != "native" {
		t.Fatalf("candidate = %+v, want exact/native", got)
	}
	if got.ComposerSafe {
		t.Fatal("ComposerSafe true: this path takes the exclusive executor hold and collides with a TUI holder")
	}
}

func TestPromptPathEmptySessionIDErrors(t *testing.T) {
	r := amp.New(testIntegrationInstanceID)
	if _, err := r.PromptPath(context.Background(), runtime.RuntimeBinding{}); err == nil {
		t.Fatal("expected an error for an empty session id")
	}
}

func TestDeliverPromptEmptySessionIDErrors(t *testing.T) {
	r := amp.New(testIntegrationInstanceID)
	if _, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{}); err == nil {
		t.Fatal("expected an error for an empty session id")
	}
}

// writeContinueScript writes a fake `amp` executable that runs body as a
// shell script, mirroring devin's writeResumeScript.
func writeContinueScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-amp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func runtimeWithContinueScript(t *testing.T, body string) *amp.Runtime {
	t.Helper()
	r := amp.New(testIntegrationInstanceID)
	r.ContinueCommand = []string{writeContinueScript(t, body)}
	return r
}

// writeDebugLog writes threadID's per-thread debug log under dir,
// mirroring Amp's own <root>/<threadID>.log naming.
func writeDebugLog(t *testing.T, dir, threadID, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, threadID+".log"), []byte(contents), 0o644); err != nil {
		t.Fatalf("write debug log: %v", err)
	}
}

// handshakeRejectedLine renders one fake debug-log record carrying the
// typed collision cause, timestamped at ts — the JSONL shape the real
// per-thread log writes (terminal-multiplexers
// fixtures/amp/thread-debug-log-excerpt.jsonl).
func handshakeRejectedLine(ts time.Time) string {
	return `{"@timestamp":"` + ts.UTC().Format("2006-01-02T15:04:05.000Z") +
		`","level":"ERROR","message":"[thread-client] ExecutorHandshakeRejectedError: Executor already connected","code":"EXECUTOR_ALREADY_CONNECTED","pid":12345}` + "\n"
}

func TestDeliverPromptSuccessResultLineIsDelivered(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv.txt")
	stdinFile := filepath.Join(t.TempDir(), "stdin.txt")
	r := runtimeWithContinueScript(t, `printf '%s\n' "$@" > `+argvFile+`
cat > `+stdinFile+`
echo '{"type":"result","subtype":"success"}'
exit 0`)
	r.SettingsFile = "/tmp/amp-settings.json"

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-loop-c-ok"},
		Text:    "hello amp",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectDelivered {
		t.Fatalf("Effect = %q, want delivered", result.Effect)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	argv := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{
		"threads", "continue", "T-loop-c-ok",
		"-x", "--no-archive-after-execute", "--stream-json",
		"--settings-file", "/tmp/amp-settings.json",
	}
	for _, w := range want {
		if !slices.Contains(argv, w) {
			t.Fatalf("argv %v missing %q", argv, w)
		}
	}

	stdin, err := os.ReadFile(stdinFile)
	if err != nil {
		t.Fatalf("read stdin file: %v", err)
	}
	if string(stdin) != "hello amp" {
		t.Fatalf("stdin = %q, want %q", stdin, "hello amp")
	}
}

func TestDeliverPromptOmitsSettingsFileFlagWhenUnset(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv.txt")
	r := runtimeWithContinueScript(t, `printf '%s\n' "$@" > `+argvFile+`
echo '{"type":"result","subtype":"success"}'
exit 0`)

	if _, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-loop-c-ok"},
		Text:    "hello",
	}); err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}

	raw, err := os.ReadFile(argvFile)
	if err != nil {
		t.Fatalf("read argv file: %v", err)
	}
	if slices.Contains(strings.Split(strings.TrimSpace(string(raw)), "\n"), "--settings-file") {
		t.Fatalf("argv %q contains --settings-file though SettingsFile was unset", raw)
	}
}

func TestDeliverPromptSpawnFailIsNoEffect(t *testing.T) {
	r := amp.New(testIntegrationInstanceID)
	r.ContinueCommand = []string{filepath.Join(t.TempDir(), "no-such-amp")}

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-loop-c-ok"},
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectNoEffect {
		t.Fatalf("Effect = %q, want no_effect", result.Effect)
	}
}

func TestDeliverPromptGenericErrorWithLockEntryIsThreadLockedEvenOnExitZero(t *testing.T) {
	r := runtimeWithContinueScript(t, `echo "Error: Unexpected error inside Amp CLI." >&2
exit 0`)
	logDir := t.TempDir()
	r.DebugLogRoot = logDir
	writeDebugLog(t, logDir, "T-dark-carnation", handshakeRejectedLine(time.Now().Add(1*time.Minute)))

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-dark-carnation"},
		Text:    "hello",
	})
	if !errors.Is(err, amp.ErrThreadLocked) {
		t.Fatalf("err = %v, want ErrThreadLocked", err)
	}
	var locked *amp.ThreadLockedError
	if !errors.As(err, &locked) || locked.ThreadID != "T-dark-carnation" {
		t.Fatalf("lock = %#v, want thread id T-dark-carnation", locked)
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptGenericErrorWithoutLockEntryIsPlainErrorUnknownEffect(t *testing.T) {
	r := runtimeWithContinueScript(t, `echo "Error: Unexpected error inside Amp CLI." >&2
exit 1`)
	r.DebugLogRoot = t.TempDir() // no log file for this thread at all

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-no-evidence"},
		Text:    "hello",
	})
	if err == nil {
		t.Fatal("expected a plain error")
	}
	if errors.Is(err, amp.ErrThreadLocked) || errors.Is(err, amp.ErrThreadNotFound) || errors.Is(err, amp.ErrThreadArchived) {
		t.Fatalf("err = %v, want a plain (untyped) error", err)
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptContextDeadlineHangWithLockEntryIsThreadLocked(t *testing.T) {
	r := runtimeWithContinueScript(t, `exec sleep 5`)
	logDir := t.TempDir()
	r.DebugLogRoot = logDir
	writeDebugLog(t, logDir, "T-hung-thread", handshakeRejectedLine(time.Now().Add(1*time.Minute)))

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	result, err := r.DeliverPrompt(ctx, runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-hung-thread"},
		Text:    "hello",
	})
	if !errors.Is(err, amp.ErrThreadLocked) {
		t.Fatalf("err = %v, want ErrThreadLocked", err)
	}
	var locked *amp.ThreadLockedError
	if !errors.As(err, &locked) || locked.ThreadID != "T-hung-thread" {
		t.Fatalf("lock = %#v, want thread id T-hung-thread", locked)
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptContextDeadlineHangWithoutLockEntryIsTimeoutUnknownEffect(t *testing.T) {
	r := runtimeWithContinueScript(t, `exec sleep 5`)
	r.DebugLogRoot = t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	result, err := r.DeliverPrompt(ctx, runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-hung-no-evidence"},
		Text:    "hello",
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if errors.Is(err, amp.ErrThreadLocked) {
		t.Fatal("err wraps ErrThreadLocked though the debug log had no collision evidence")
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptDoesNotExistOutputIsThreadNotFound(t *testing.T) {
	r := runtimeWithContinueScript(t, `echo "Error: thread T-ghost does not exist" >&2
exit 1`)

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-ghost"},
		Text:    "hello",
	})
	if !errors.Is(err, amp.ErrThreadNotFound) {
		t.Fatalf("err = %v, want ErrThreadNotFound", err)
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptArchivedOutputIsThreadArchived(t *testing.T) {
	r := runtimeWithContinueScript(t, `echo "Error: thread T-archived is archived and cannot be continued" >&2
exit 1`)

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-archived"},
		Text:    "hello",
	})
	if !errors.Is(err, amp.ErrThreadArchived) {
		t.Fatalf("err = %v, want ErrThreadArchived", err)
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptStaleLockEntryNotTreatedAsThisAttempt(t *testing.T) {
	r := runtimeWithContinueScript(t, `echo "Error: Unexpected error inside Amp CLI." >&2
exit 1`)
	logDir := t.TempDir()
	r.DebugLogRoot = logDir
	// This entry predates the attempt by an hour: an earlier, unrelated
	// collision (or a stale leftover from a previous attempt), not
	// evidence for the collision this call is trying to explain.
	writeDebugLog(t, logDir, "T-stale-lock", handshakeRejectedLine(time.Now().Add(-1*time.Hour)))

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-stale-lock"},
		Text:    "hello",
	})
	if errors.Is(err, amp.ErrThreadLocked) {
		t.Fatalf("err = %v, want a plain error: the only debug-log entry predates this attempt", err)
	}
	if err == nil {
		t.Fatal("expected a plain error")
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptNonRecordLockLineNotTreatedAsEvidence(t *testing.T) {
	r := runtimeWithContinueScript(t, `echo "Error: Unexpected error inside Amp CLI." >&2
exit 1`)
	logDir := t.TempDir()
	r.DebugLogRoot = logDir
	// The marker text outside a parseable debug-log record — free text, no
	// @timestamp — is never evidence, the same discipline the generic CLI
	// error text gets.
	writeDebugLog(t, logDir, "T-free-text",
		"ExecutorHandshakeRejectedError: Executor already connected\n")

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "T-free-text"},
		Text:    "hello",
	})
	if errors.Is(err, amp.ErrThreadLocked) {
		t.Fatalf("err = %v, want a plain error: a non-record marker line is not evidence", err)
	}
	if err == nil {
		t.Fatal("expected a plain error")
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}
