package claude_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/claude"
)

const fixtureSessionID = "89dc3e6a-1609-44ed-8515-64dbef6f3726"

func TestPromptPathOffersNativeComposerSafeWithoutDialing(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	got, err := r.PromptPath(ctx, runtime.RuntimeBinding{ExternalAgentSessionID: fixtureSessionID})
	if err != nil {
		t.Fatalf("PromptPath: %v", err)
	}
	if got.Quality != "exact" || got.Realization != "native" {
		t.Fatalf("candidate = %+v, want exact/native", got)
	}
	if !got.ComposerSafe {
		t.Fatalf("ComposerSafe = false, want true (peer queue, notes/13)")
	}
}

func TestPromptPathMissingSessionErrors(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	_, err := r.PromptPath(context.Background(), runtime.RuntimeBinding{ExternalAgentSessionID: "no-such-session"})
	if err == nil {
		t.Fatalf("PromptPath: want an error for a session with no registry entry")
	}
}

func TestDeliverPromptAdmitIsDelivered(t *testing.T) {
	claudeDir := t.TempDir()
	sockPath := filepath.Join(t.TempDir(), "target.sock")
	received := startAdmitStandIn(t, sockPath)
	writeRegistry(t, claudeDir, "sess-admit", 4242, sockPath)

	r, err := claude.New(testIntegrationInstanceID, "duo-reporter-MUST-NOT-APPEAR-on-socket", claudeDir)
	if err != nil {
		t.Fatalf("claude.New: %v", err)
	}

	decoyDir := t.TempDir()
	decoySock := filepath.Join(decoyDir, "decoy.sock")
	decoyHit := startDecoyStandIn(t, decoySock)
	t.Setenv("CLAUDE_CODE_MESSAGING_SOCKET", decoySock)
	t.Setenv("CLAUDE_CODE_MESSAGING_TOKEN", "decoy-token-MUST-NOT-APPEAR")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := r.DeliverPrompt(ctx, runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "sess-admit"},
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectDelivered {
		t.Fatalf("Effect = %q, want delivered", result.Effect)
	}

	select {
	case frame := <-received:
		want := []byte(`{"type":"user","message":{"role":"user","content":"hello"}}` + "\n")
		if !bytes.Equal(frame, want) {
			t.Fatalf("frame = %q, want %q", frame, want)
		}
		if bytes.Contains(frame, []byte("duo-reporter-MUST-NOT-APPEAR-on-socket")) {
			t.Fatalf("frame carried ReporterCredential")
		}
		if bytes.Contains(frame, []byte("decoy-token-MUST-NOT-APPEAR")) {
			t.Fatalf("frame carried CLAUDE_CODE_MESSAGING_TOKEN from Duo's environment")
		}
		if err := json.Unmarshal(bytes.TrimSpace(frame), new(map[string]any)); err != nil {
			t.Fatalf("frame is not JSON: %v", err)
		}
	case <-ctx.Done():
		t.Fatalf("stand-in received no frame")
	}

	select {
	case <-decoyHit:
		t.Fatalf("dialed CLAUDE_CODE_MESSAGING_SOCKET; send only to the target instance")
	default:
	}
}

func TestDeliverPromptWriteThenCloseIsUnknownEffect(t *testing.T) {
	claudeDir := t.TempDir()
	sockPath := filepath.Join(t.TempDir(), "close.sock")
	startReadThenCloseStandIn(t, sockPath)
	writeRegistry(t, claudeDir, "sess-close", 4343, sockPath)

	r, err := claude.New(testIntegrationInstanceID, "duo-reporter-secret", claudeDir)
	if err != nil {
		t.Fatalf("claude.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := r.DeliverPrompt(ctx, runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "sess-close"},
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptDerivedSocketFromPID(t *testing.T) {
	runtimeDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	pid := 5151
	sockDir := filepath.Join(runtimeDir, "cc-socks")
	if err := os.MkdirAll(sockDir, 0o700); err != nil {
		t.Fatalf("mkdir cc-socks: %v", err)
	}
	sockPath := filepath.Join(sockDir, strconv.Itoa(pid)+".sock")
	received := startAdmitStandIn(t, sockPath)

	claudeDir := t.TempDir()
	writeRegistry(t, claudeDir, "sess-derived", pid, "") // path omitted: derive from pid

	r, err := claude.New(testIntegrationInstanceID, "", claudeDir)
	if err != nil {
		t.Fatalf("claude.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := r.DeliverPrompt(ctx, runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: "sess-derived"},
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectDelivered {
		t.Fatalf("Effect = %q, want delivered via derived cc-socks/<pid>.sock", result.Effect)
	}
	select {
	case <-received:
	case <-ctx.Done():
		t.Fatalf("derived socket received no frame")
	}
}

func writeRegistry(t *testing.T, claudeDir, sessionID string, pid int, socketPath string) {
	t.Helper()
	dir := filepath.Join(claudeDir, "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	entry := map[string]any{
		"pid":       pid,
		"sessionId": sessionID,
	}
	if socketPath != "" {
		entry["messagingSocketPath"] = socketPath
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	name := strconv.Itoa(pid) + ".json"
	if err := os.WriteFile(filepath.Join(dir, name), raw, 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

func startAdmitStandIn(t *testing.T, sockPath string) <-chan []byte {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen %s: %v", sockPath, err)
	}
	received := make(chan []byte, 1)
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
		buf := make([]byte, 64*1024)
		n, err := conn.Read(buf)
		if n > 0 {
			received <- append([]byte(nil), buf[:n]...)
		}
		if err != nil && n == 0 {
			return
		}
		<-done
	}()
	t.Cleanup(func() {
		close(done)
		_ = ln.Close()
		wg.Wait()
	})
	return received
}

func startReadThenCloseStandIn(t *testing.T, sockPath string) {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen %s: %v", sockPath, err)
	}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		buf := make([]byte, 64*1024)
		_, _ = conn.Read(buf)
		_ = conn.Close()
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
}

func startDecoyStandIn(t *testing.T, sockPath string) <-chan struct{} {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen decoy %s: %v", sockPath, err)
	}
	hit := make(chan struct{}, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
		select {
		case hit <- struct{}{}:
		default:
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	return hit
}
