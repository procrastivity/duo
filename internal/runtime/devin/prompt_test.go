package devin_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/devin"
)

type stdioPair struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p *stdioPair) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p *stdioPair) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p *stdioPair) Close() error {
	_ = p.w.Close()
	return p.r.Close()
}

type closeTracker struct {
	io.ReadWriteCloser
	closed *bool
}

func (c *closeTracker) Close() error {
	*c.closed = true
	return c.ReadWriteCloser.Close()
}

func startFakeACP(t *testing.T, handle func(method string) (json.RawMessage, *rpcErr)) io.ReadWriteCloser {
	t.Helper()
	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	go func() {
		defer stdoutW.Close()
		dec := json.NewDecoder(stdinR)
		enc := json.NewEncoder(stdoutW)
		for {
			var req struct {
				ID     any    `json:"id"`
				Method string `json:"method"`
			}
			if err := dec.Decode(&req); err != nil {
				return
			}
			result, ferr := handle(req.Method)
			resp := struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      any             `json:"id"`
				Result  json.RawMessage `json:"result,omitempty"`
				Error   *rpcErr         `json:"error,omitempty"`
			}{JSONRPC: "2.0", ID: req.ID, Result: result, Error: ferr}
			if err := enc.Encode(resp); err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() { _ = stdinW.Close(); _ = stdinR.Close() })
	return &stdioPair{r: stdoutR, w: stdinW}
}

type rpcErr struct {
	Code    int            `json:"code"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
}

func TestPromptPathIsExactNativeNotComposerSafe(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	var dialed bool
	r.ACPDial = func(context.Context) (io.ReadWriteCloser, error) {
		dialed = true
		return nil, errors.New("must not dial")
	}
	got, err := r.PromptPath(context.Background(), runtime.RuntimeBinding{ExternalAgentSessionID: "lace-pegasus"})
	if err != nil {
		t.Fatalf("PromptPath: %v", err)
	}
	if dialed {
		t.Fatal("PromptPath spawned or dialed ACP")
	}
	if got.Quality != "exact" || got.Realization != "native" {
		t.Fatalf("candidate = %+v, want exact/native", got)
	}
	if got.ComposerSafe {
		t.Fatal("ComposerSafe true: ACP takes exclusive hold and collides with a TUI")
	}
}

func TestPromptPathEmptySessionIDErrors(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	if _, err := r.PromptPath(context.Background(), runtime.RuntimeBinding{}); err == nil {
		t.Fatal("expected an error for an empty session id")
	}
}

func TestDeliverPromptEndTurnIsDeliveredAndReleases(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	var closed bool
	r.ACPDial = func(context.Context) (io.ReadWriteCloser, error) {
		conn := startFakeACP(t, func(method string) (json.RawMessage, *rpcErr) {
			switch method {
			case "initialize":
				return json.RawMessage(`{"protocolVersion":1}`), nil
			case "session/load":
				return json.RawMessage(`{"modes":{"currentModeId":"accept-edits"}}`), nil
			case "session/prompt":
				return json.RawMessage(`{"stopReason":"end_turn"}`), nil
			default:
				return nil, &rpcErr{Code: -32601, Message: "unknown method"}
			}
		})
		return &closeTracker{ReadWriteCloser: conn, closed: &closed}, nil
	}

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{
			ExternalAgentSessionID: "brave-muskmelon",
			WorkingDirectory:       "/tmp/duo-ws",
		},
		Text: "Reply with exactly: LOOP-B-OK",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectDelivered {
		t.Fatalf("Effect = %q, want delivered", result.Effect)
	}
	if !closed {
		t.Fatal("ACP child was not closed after delivered")
	}
}

func TestDeliverPromptSessionLocked(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	r.ACPDial = func(context.Context) (io.ReadWriteCloser, error) {
		return startFakeACP(t, func(method string) (json.RawMessage, *rpcErr) {
			switch method {
			case "initialize":
				return json.RawMessage(`{"protocolVersion":1}`), nil
			case "session/load":
				return nil, &rpcErr{
					Code:    -32015,
					Message: "Session 'grass-tangelo' is already open in another process.",
					Data:    map[string]any{"cognition.ai/errorKind": "session_locked", "cognition.ai/retryable": true},
				}
			default:
				return nil, &rpcErr{Code: -32601, Message: "unknown method"}
			}
		}), nil
	}

	_, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "grass-tangelo"},
		Text:    "hello",
	})
	if !errors.Is(err, devin.ErrSessionLocked) {
		t.Fatalf("err = %v, want ErrSessionLocked", err)
	}
}

func TestDeliverPromptSessionNotFound(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	r.ACPDial = func(context.Context) (io.ReadWriteCloser, error) {
		return startFakeACP(t, func(method string) (json.RawMessage, *rpcErr) {
			switch method {
			case "initialize":
				return json.RawMessage(`{"protocolVersion":1}`), nil
			case "session/load":
				return nil, &rpcErr{
					Code:    -32016,
					Message: "Session not found",
					Data:    map[string]any{"cognition.ai/errorKind": "session_not_found", "cognition.ai/retryable": false},
				}
			default:
				return nil, &rpcErr{Code: -32601, Message: "unknown method"}
			}
		}), nil
	}

	_, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "brave-muskmelon"},
		Text:    "hello",
	})
	if !errors.Is(err, devin.ErrSessionNotFound) {
		t.Fatalf("err = %v, want ErrSessionNotFound", err)
	}
}

func writeResumeScript(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake-devin")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	return path
}

func runtimeWithResumeScript(t *testing.T, body string) *devin.Runtime {
	t.Helper()
	r := devin.New(testIntegrationInstanceID)
	r.ResumeCommand = []string{writeResumeScript(t, body)}
	return r
}

func TestDeliverPromptSpawnFailIsNoEffect(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	r.ResumeCommand = []string{filepath.Join(t.TempDir(), "no-such-devin-resume")}

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "lace-pegasus"},
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectNoEffect {
		t.Fatalf("Effect = %q, want no_effect", result.Effect)
	}
}

func TestDeliverPromptResumeLockEvenOnExitZero(t *testing.T) {
	r := runtimeWithResumeScript(t, `echo "Error: failed to start ACP agent session" >&2
exit 0`)

	_, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "dark-carnation"},
		Text:    "hello",
	})
	if !errors.Is(err, devin.ErrSessionLocked) {
		t.Fatalf("err = %v, want ErrSessionLocked", err)
	}
}

func TestDeliverPromptResumePassesExportAndPrint(t *testing.T) {
	argvFile := filepath.Join(t.TempDir(), "argv.txt")
	r := runtimeWithResumeScript(t, `printf '%s\n' "$@" > `+argvFile+`
exit 0`)

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{
			ExternalAgentSessionID: "dark-carnation",
			TranscriptID:           "/tmp/atif.json",
		},
		Text: "hello",
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
		"--resume", "dark-carnation",
		"--export", "/tmp/atif.json",
		"--respect-workspace-trust", "false",
		"-p", "hello",
	}
	for _, w := range want {
		if !slices.Contains(argv, w) {
			t.Fatalf("argv %v missing %q", argv, w)
		}
	}
	if slices.Contains(argv, "--permission-mode") {
		t.Fatalf("argv must not contain --permission-mode: %v", argv)
	}
}
