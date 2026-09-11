package amp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// ErrThreadLocked is the typed Amp server-side single-executor-lock
// collision (docs/adapters/decisions.md, 2026-09-09 "Amp exclusive-writer
// scope is per-turn, not per-session"; notes/62 §5). A second writer
// connecting to a thread Duo's spawned process already holds is refused,
// first-connect-wins, no queue. The CLI surfaces only a generic
// `Error: Unexpected error inside Amp CLI.`; the typed cause,
// ExecutorHandshakeRejectedError, is confirmed only by reading the
// per-thread debug log — never by trusting that generic text alone. It is
// not PromptEffectNoEffect (a collision may have partially written) and
// not delivered. Callers may errors.Is this value.
var ErrThreadLocked = errors.New("amp: thread_locked")

// ThreadLockedError identifies the exact bound thread Amp refused a
// second writer on.
type ThreadLockedError struct {
	ThreadID string
}

func (e *ThreadLockedError) Error() string {
	return fmt.Sprintf("amp: thread %q is locked by another executor", e.ThreadID)
}

func (e *ThreadLockedError) Unwrap() error { return ErrThreadLocked }

// ErrThreadNotFound is Amp's clean "does not exist" error text: the bound
// thread id is not one Amp knows. Not retryable in place.
var ErrThreadNotFound = errors.New("amp: thread_not_found")

// ErrThreadArchived is Amp's clean "archived and cannot be continued"
// error text: the bound thread exists but `amp threads continue` refuses
// an archived thread. Not retryable in place.
var ErrThreadArchived = errors.New("amp: thread_archived")

const (
	// genericErrorMarker is the only text the Amp CLI prints for a
	// refused-executor collision. On its own it is never sufficient
	// evidence of a lock (docs/adapters/decisions.md, 2026-09-09) — it
	// only decides whether this package goes looking in the debug log.
	genericErrorMarker = "unexpected error inside amp cli"
	// threadNotFoundMarker and threadArchivedMarker are Amp's distinct
	// clean error texts for the other two closed outcomes this package
	// recognizes. Unlike genericErrorMarker, these name their own cause
	// and need no debug-log correlation.
	threadNotFoundMarker = "does not exist"
	threadArchivedMarker = "archived and cannot be continued"

	// executorHandshakeRejectedMarker is the typed cause name Amp writes
	// to the per-thread debug log for a refused executor collision
	// (docs/adapters/decisions.md, 2026-09-09).
	executorHandshakeRejectedMarker = "ExecutorHandshakeRejectedError"

	// debugLogTailBytes bounds how much of a per-thread debug log this
	// package ever reads. The log is Amp's own append-only file and can
	// grow large over a thread's lifetime; only the entries near one
	// delivery attempt are ever relevant, so a full read is never needed
	// (I-6-shaped discipline: a locator and a bounded read, not an
	// unbounded scan).
	debugLogTailBytes = 256 * 1024
)

// PromptPath implements runtime.RuntimePromptProvider. It offers the
// spawn-per-turn `amp threads continue` path when a thread id is bound.
// It does not spawn. ComposerSafe is false: this path takes the
// exclusive executor hold and collides with a TUI holder attached to the
// same thread (docs/adapters/decisions.md, 2026-09-09 "Amp
// exclusive-writer scope is per-turn, not per-session"; same rationale
// shape as Devin's ACP PromptPath, notes/27 §9).
func (r *Runtime) PromptPath(_ context.Context, binding runtime.RuntimeBinding) (runtime.PromptPathCandidate, error) {
	if binding.ExternalAgentSessionID == "" {
		return runtime.PromptPathCandidate{}, fmt.Errorf("amp runtime %s: prompt path requires an external agent-session id", r.integrationInstanceID)
	}
	return runtime.PromptPathCandidate{
		Quality:      "exact",
		Realization:  "native",
		ComposerSafe: false,
	}, nil
}

// DeliverPrompt implements runtime.RuntimePromptProvider. It spawns one
// `amp threads continue <tid> -x --no-archive-after-execute
// --stream-json` process per prompt — Amp has no long-lived Duo process
// to hold a pane against, so the server-side executor lock is held only
// for the lifetime of this spawned process, i.e. only during this turn
// (docs/adapters/decisions.md, 2026-09-09). The prompt text arrives on
// stdin, the same recipe shape the mint wrapper script uses (mint.go,
// materialize.go). The child runs under ctx via exec.CommandContext: this
// is Duo's own spawned child, not a long-lived session Duo attaches to,
// so owning its lifecycle this way mirrors Devin's spawn-per-prompt
// resume path exactly.
func (r *Runtime) DeliverPrompt(ctx context.Context, req runtime.PromptDeliveryRequest) (runtime.PromptDeliveryResult, error) {
	threadID := req.Binding.ExternalAgentSessionID
	if threadID == "" {
		return runtime.PromptDeliveryResult{}, fmt.Errorf("amp runtime %s: prompt requires an external agent-session id", r.integrationInstanceID)
	}

	argv := r.ContinueCommand
	if len(argv) == 0 {
		argv = []string{"amp"}
	}
	args := append([]string(nil), argv...)
	args = append(args, "threads", "continue", threadID, "-x", "--no-archive-after-execute", "--stream-json")
	if r.SettingsFile != "" {
		args = append(args, "--settings-file", r.SettingsFile)
	}

	// Captured before Start: this is the "attempt started" instant the
	// debug-log correlation below measures recency against.
	attemptStart := time.Now()

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if req.Binding.WorkingDirectory != "" {
		cmd.Dir = req.Binding.WorkingDirectory
	}
	cmd.Stdin = strings.NewReader(req.Text)
	out, err := cmd.CombinedOutput()
	outStr := string(out)

	// A killed-by-context child is detected via ctx.Err(), not by
	// errors.Is on the CombinedOutput error: exec.CommandContext's
	// default Cancel (Process.Kill) makes Wait return a plain "signal:
	// killed" error that does not wrap ctx.Err(). Checking ctx.Err()
	// directly is the only reliable signal that the deadline (or an
	// outer cancellation), not Amp itself, ended the child.
	if err != nil && ctx.Err() != nil {
		// A collision against a still-settling executor handshake can
		// hang to full timeout instead of refusing fast
		// (docs/adapters/decisions.md, 2026-09-09, notes/63 §5) — the
		// debug log still needs correlating before this is graded a
		// plain timeout, exactly as for a fast generic-error refusal.
		if r.debugLogHandshakeRejectedSince(threadID, attemptStart) {
			return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, &ThreadLockedError{ThreadID: threadID}
		}
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, ctx.Err()
	}

	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			// The process never started (missing executable, etc): no
			// write reached Amp.
			return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectNoEffect}, nil
		}
	}

	if containsFold(outStr, threadNotFoundMarker) {
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, ErrThreadNotFound
	}
	if containsFold(outStr, threadArchivedMarker) {
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, ErrThreadArchived
	}

	if err != nil || containsFold(outStr, genericErrorMarker) {
		// Checked even on exit 0: the generic Amp CLI error text is not
		// bound to a particular exit code (mirrors devin's
		// resumeOutputSessionLocked, which checks the same way).
		if r.debugLogHandshakeRejectedSince(threadID, attemptStart) {
			return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, &ThreadLockedError{ThreadID: threadID}
		}
		if containsFold(outStr, genericErrorMarker) {
			return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect},
				fmt.Errorf("amp: thread %s: %s", threadID, strings.TrimSpace(outStr))
		}
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, err
	}

	if streamJSONSuccess(outStr) {
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectDelivered}, nil
	}
	return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
}

// containsFold reports whether s contains substr, ASCII-case-insensitive.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// streamJSONResultLine is the narrow slice of Amp's stream-JSON shape
// streamJSONSuccess needs — the same "type"/"subtype" fields
// mintLogLine reads from a tee'd mint log, applied here to a delivery
// attempt's captured combined output instead of a tee file.
type streamJSONResultLine struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
}

// streamJSONSuccess reports whether out — one prompt-delivery attempt's
// captured stdout/stderr — contains a stream-JSON line with type
// "result" and subtype "success". This is the only proof of delivery
// this package trusts (docs/adapters/decisions.md, 2026-09-09): a clean
// exit code alone is not enough, because `amp threads continue` can exit
// 0 without ever reaching a result record.
func streamJSONSuccess(out string) bool {
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec streamJSONResultLine
		if json.Unmarshal(line, &rec) != nil {
			continue
		}
		if rec.Type == "result" && rec.Subtype == "success" {
			return true
		}
	}
	return false
}

// debugLogHandshakeRejectedSince reports whether threadID's per-thread
// debug log carries an ExecutorHandshakeRejectedError entry timestamped
// at or after attemptStart. A missing log, an unreadable log, or a log
// with no such entry all read as "no evidence" (false), never an error:
// this is a best-effort correlation on top of a CLI outcome that is
// already known to be non-success, not the primary source of truth.
func (r *Runtime) debugLogHandshakeRejectedSince(threadID string, attemptStart time.Time) bool {
	path, err := r.debugLogPath(threadID)
	if err != nil {
		return false
	}
	tail, err := readFileTail(path, debugLogTailBytes)
	if err != nil {
		return false
	}
	return handshakeRejectedEntrySince(tail, attemptStart)
}

// debugLogPath returns <DebugLogRoot>/<threadID>.log, defaulting
// DebugLogRoot to Amp's own per-thread debug log directory,
// ~/.cache/amp/logs/threads (docs/adapters/decisions.md, 2026-09-09).
// This is a locator, not a directory-newest scan (I-6), mirroring
// MintLogPath's discipline.
func (r *Runtime) debugLogPath(threadID string) (string, error) {
	root := r.DebugLogRoot
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("amp: resolving default debug log root: %w", err)
		}
		root = filepath.Join(home, ".cache", "amp", "logs", "threads")
	}
	return filepath.Join(root, threadID+".log"), nil
}

// readFileTail returns up to the last maxBytes of the file at path. A
// missing file returns a nil, error-free tail: the debug log directory
// is Amp's own, and this package never creates or requires it to exist.
func readFileTail(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("amp: opening debug log %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("amp: statting debug log %s: %w", path, err)
	}
	var offset int64
	if info.Size() > maxBytes {
		offset = info.Size() - maxBytes
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, fmt.Errorf("amp: seeking debug log %s: %w", path, err)
	}
	tail, err := io.ReadAll(f)
	if err != nil {
		return nil, fmt.Errorf("amp: reading debug log %s: %w", path, err)
	}
	return tail, nil
}

// debugLogLine is the narrow slice of Amp's per-thread debug-log shape
// the collision correlation reads. The log is JSONL — one JSON object per
// line with an RFC3339 "@timestamp" field (the recorded excerpt is
// terminal-multiplexers fixtures/amp/thread-debug-log-excerpt.jsonl); the
// rejection entry's message carries executorHandshakeRejectedMarker and
// its code field is "EXECUTOR_ALREADY_CONNECTED".
type debugLogLine struct {
	Timestamp string `json:"@timestamp"`
}

// handshakeRejectedEntrySince scans tail line by line for
// executorHandshakeRejectedMarker and reports whether any matching line
// is a debug-log record timestamped at or after notBefore.
//
// Recency design: a matching line must parse as a debug-log JSON object
// whose "@timestamp" parses as RFC3339 and is not before notBefore —
// otherwise it is never counted as evidence, the same "text alone is
// never sufficient" discipline the generic CLI error text gets. This is
// what keeps a stale collision from an earlier attempt (or a different
// caller entirely) from being misread as this attempt's collision: only
// an entry timestamped no earlier than the moment this attempt's process
// was about to start counts. The tail cut can leave the first scanned
// line truncated mid-object; that line fails to parse and is skipped
// like any other non-record line.
func handshakeRejectedEntrySince(tail []byte, notBefore time.Time) bool {
	scanner := bufio.NewScanner(bytes.NewReader(tail))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.Contains(line, []byte(executorHandshakeRejectedMarker)) {
			continue
		}
		var rec debugLogLine
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, rec.Timestamp)
		if err != nil {
			continue
		}
		if !ts.Before(notBefore) {
			return true
		}
	}
	return false
}
