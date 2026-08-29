package devin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"

	"github.com/procrastivity/duo/internal/runtime"
)

// ErrSessionLocked is the typed ACP first-holder collision (notes/27 §9,
// notes/59 §5). It is not PromptEffectNoEffect (that auto-retries) and
// not delivered. Callers may errors.Is this value.
var ErrSessionLocked = errors.New("devin: session_locked")

// ErrSessionNotFound is ACP session/load -32016: the id is not in the
// CLI store. A Herdr-named TUI pane can bind before a store row exists
// (live finding, duo-devin-loop/prompt). Not retryable.
var ErrSessionNotFound = errors.New("devin: session_not_found")

const (
	acpJSONRPC      = "2.0"
	acpInitializeID = 1
	acpLoadID       = 2
	acpPromptID     = 3
	acpLockedCode   = -32015
	acpNotFoundCode = -32016
	acpLockedKind   = "session_locked"
	acpNotFoundKind = "session_not_found"
	acpStopEndTurn  = "end_turn"
)

// PromptPath implements runtime.RuntimePromptProvider. It offers the ACP
// stdio path when a session id is bound. It does not spawn or dial.
// ComposerSafe is false: this path takes exclusive hold and collides with
// a TUI holder (notes/27 §9).
func (r *Runtime) PromptPath(_ context.Context, binding runtime.RuntimeBinding) (runtime.PromptPathCandidate, error) {
	if binding.ExternalAgentSessionID == "" {
		return runtime.PromptPathCandidate{}, fmt.Errorf("devin runtime %s: prompt path requires an external agent-session id", r.integrationInstanceID)
	}
	return runtime.PromptPathCandidate{
		Quality:      "exact",
		Realization:  "native",
		ComposerSafe: false,
	}, nil
}

// DeliverPrompt implements runtime.RuntimePromptProvider. Production
// spawns `devin --resume [--export] -p` per prompt so ATIF updates.
// When ACPDial is set, the ACP stdio JSON-RPC path is used instead
// (tests). ComposerSafe stays false.
func (r *Runtime) DeliverPrompt(ctx context.Context, req runtime.PromptDeliveryRequest) (runtime.PromptDeliveryResult, error) {
	if req.Binding.ExternalAgentSessionID == "" {
		return runtime.PromptDeliveryResult{}, fmt.Errorf("devin runtime %s: prompt requires an external agent-session id", r.integrationInstanceID)
	}

	if r.ACPDial == nil {
		return r.deliverPromptResume(ctx, req)
	}

	conn, stop, err := r.openACP(ctx)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return runtime.PromptDeliveryResult{}, err
		}
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectNoEffect}, nil
	}
	defer stop()

	sess := req.Binding.ExternalAgentSessionID
	if err := r.rpcCall(ctx, conn, acpInitializeID, "initialize", initializeParams()); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return runtime.PromptDeliveryResult{}, err
		}
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
	}

	loadParams := map[string]any{
		"sessionId":  sess,
		"cwd":        req.Binding.WorkingDirectory,
		"mcpServers": []any{},
	}
	if err := r.rpcCall(ctx, conn, acpLoadID, "session/load", loadParams); err != nil {
		if errors.Is(err, ErrSessionLocked) || errors.Is(err, ErrSessionNotFound) {
			return runtime.PromptDeliveryResult{}, err
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return runtime.PromptDeliveryResult{}, err
		}
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
	}

	promptParams := map[string]any{
		"sessionId": sess,
		"prompt":    []map[string]string{{"type": "text", "text": req.Text}},
	}
	raw, err := r.rpcCallResult(ctx, conn, acpPromptID, "session/prompt", promptParams)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return runtime.PromptDeliveryResult{}, err
		}
		if isWriteBeforePrompt(err) {
			return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectNoEffect}, nil
		}
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
	}

	var result struct {
		StopReason string `json:"stopReason"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
	}
	if result.StopReason == acpStopEndTurn {
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectDelivered}, nil
	}
	return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
}

func (r *Runtime) deliverPromptResume(ctx context.Context, req runtime.PromptDeliveryRequest) (runtime.PromptDeliveryResult, error) {
	argv := r.ResumeCommand
	if len(argv) == 0 {
		argv = []string{"devin"}
	}
	sess := req.Binding.ExternalAgentSessionID
	args := append([]string(nil), argv...)
	args = append(args, "--resume", sess)
	if req.Binding.TranscriptID != "" {
		args = append(args, "--export", req.Binding.TranscriptID)
	}
	args = append(args, "--respect-workspace-trust", "false", "-p", req.Text)

	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	if req.Binding.WorkingDirectory != "" {
		cmd.Dir = req.Binding.WorkingDirectory
	}
	out, err := cmd.CombinedOutput()
	outStr := string(out)

	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return runtime.PromptDeliveryResult{}, err
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectNoEffect}, nil
		}
	}

	if resumeOutputSessionLocked(outStr) {
		return runtime.PromptDeliveryResult{}, ErrSessionLocked
	}
	if resumeOutputSessionNotFound(outStr) {
		return runtime.PromptDeliveryResult{}, ErrSessionNotFound
	}
	if err == nil {
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectDelivered}, nil
	}
	return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
}

func resumeOutputSessionLocked(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "failed to start acp agent session") ||
		strings.Contains(lower, "already open in another process")
}

func resumeOutputSessionNotFound(out string) bool {
	lower := strings.ToLower(out)
	return strings.Contains(lower, "session_not_found") ||
		strings.Contains(lower, "session not found")
}

func initializeParams() map[string]any {
	return map[string]any{
		"protocolVersion": 1,
		"clientCapabilities": map[string]any{
			"fs":       map[string]bool{"readTextFile": false, "writeTextFile": false},
			"terminal": false,
		},
	}
}

func (r *Runtime) rpcCall(ctx context.Context, conn io.ReadWriter, id int, method string, params any) error {
	_, err := r.rpcCallResult(ctx, conn, id, method, params)
	return err
}

func (r *Runtime) rpcCallResult(ctx context.Context, conn io.ReadWriter, id int, method string, params any) (json.RawMessage, error) {
	req := map[string]any{
		"jsonrpc": acpJSONRPC,
		"id":      id,
		"method":  method,
		"params":  params,
	}
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	b = append(b, '\n')
	if _, err := conn.Write(b); err != nil {
		if method == "session/prompt" {
			return nil, &promptWriteError{err: err}
		}
		return nil, err
	}

	dec := json.NewDecoder(conn)
	want := strconv.Itoa(id)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var env rpcEnvelope
		if err := dec.Decode(&env); err != nil {
			return nil, err
		}
		if rpcIDString(env.ID) != want {
			continue
		}
		if env.Error != nil {
			if env.Error.sessionLocked() {
				return nil, ErrSessionLocked
			}
			if env.Error.sessionNotFound() {
				return nil, ErrSessionNotFound
			}
			return nil, fmt.Errorf("devin acp %s: %s", method, env.Error.Message)
		}
		return env.Result, nil
	}
}

type promptWriteError struct{ err error }

func (e *promptWriteError) Error() string { return e.err.Error() }
func (e *promptWriteError) Unwrap() error { return e.err }

func isWriteBeforePrompt(err error) bool {
	var w *promptWriteError
	return errors.As(err, &w)
}

type rpcEnvelope struct {
	ID     any             `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) sessionLocked() bool {
	if e == nil {
		return false
	}
	if e.Code == acpLockedCode {
		return true
	}
	var data map[string]any
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return false
	}
	kind, _ := data["cognition.ai/errorKind"].(string)
	return kind == acpLockedKind
}

func (e *rpcError) sessionNotFound() bool {
	if e == nil {
		return false
	}
	if e.Code == acpNotFoundCode {
		return true
	}
	var data map[string]any
	if err := json.Unmarshal(e.Data, &data); err != nil {
		return false
	}
	kind, _ := data["cognition.ai/errorKind"].(string)
	return kind == acpNotFoundKind
}

func rpcIDString(id any) string {
	switch v := id.(type) {
	case nil:
		return ""
	case float64:
		return strconv.Itoa(int(v))
	case json.Number:
		return v.String()
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func (r *Runtime) openACP(ctx context.Context) (io.ReadWriteCloser, func(), error) {
	if r.ACPDial != nil {
		conn, err := r.ACPDial(ctx)
		if err != nil {
			return nil, nil, err
		}
		return conn, func() { _ = conn.Close() }, nil
	}
	argv := r.ACPCommand
	if len(argv) == 0 {
		argv = []string{"devin", "acp"}
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	conn := &stdioConn{Reader: stdout, WriteCloser: stdin}
	stop := func() {
		_ = conn.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}
	return conn, stop, nil
}

type stdioConn struct {
	io.Reader
	io.WriteCloser
}
