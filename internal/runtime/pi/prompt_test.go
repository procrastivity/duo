package pi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

const injectSessionID = "01a02c19-65e1-7346-b418-82ab0d32942c"

func TestPromptPathOffersNativeComposerSafeWithoutDialing(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	r := runtimepi.New("pi-test")

	got, err := r.PromptPath(context.Background(), runtime.RuntimeBinding{ExternalAgentSessionID: injectSessionID})
	if err != nil {
		t.Fatalf("PromptPath: %v", err)
	}
	if got.Quality != "exact" || got.Realization != "native" {
		t.Fatalf("candidate = %+v, want exact/native", got)
	}
	if !got.ComposerSafe {
		t.Fatal("ComposerSafe = false, want true")
	}
}

func TestPromptPathEmptySessionErrors(t *testing.T) {
	r := runtimepi.New("pi-test")
	_, err := r.PromptPath(context.Background(), runtime.RuntimeBinding{})
	if err == nil {
		t.Fatal("PromptPath: want an error for an empty session id")
	}
}

func TestPromptPathPathShapedIdentityOffers(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	r := runtimepi.New("pi-test")

	transcript := "/tmp/sessions/--cwd--/2026-08-23T00-50-57-121Z_" + injectSessionID + ".jsonl"
	got, err := r.PromptPath(context.Background(), runtime.RuntimeBinding{ExternalAgentSessionID: transcript})
	if err != nil {
		t.Fatalf("PromptPath path-shaped identity: %v", err)
	}
	if got.Quality != "exact" {
		t.Fatalf("candidate = %+v, want exact", got)
	}
}

func TestDeliverPromptAdmitIsDelivered(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	sockPath, err := runtimepi.InjectSocketPath(injectSessionID)
	if err != nil {
		t.Fatalf("InjectSocketPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir inject socket dir: %v", err)
	}
	received := startAdmitStandIn(t, sockPath, true)

	decoy := filepath.Join(dir, "decoy.sock")
	decoyHit := startDecoyStandIn(t, decoy)
	t.Setenv("DUO_PI_SOCK", decoy)

	r := runtimepi.New("pi-test")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := r.DeliverPrompt(ctx, runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: injectSessionID},
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
		want := []byte(`{"text":"hello"}` + "\n")
		if !bytes.Equal(frame, want) {
			t.Fatalf("frame = %q, want %q", frame, want)
		}
		if err := json.Unmarshal(bytes.TrimSpace(frame), new(map[string]any)); err != nil {
			t.Fatalf("frame is not JSON: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("stand-in received no frame")
	}

	select {
	case <-decoyHit:
		t.Fatal("dialed DUO_PI_SOCK; Go locate must ignore the test-process env")
	default:
	}
}

func TestDeliverPromptWriteThenCloseIsUnknownEffect(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	sockPath, err := runtimepi.InjectSocketPath(injectSessionID)
	if err != nil {
		t.Fatalf("InjectSocketPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir inject socket dir: %v", err)
	}
	startReadThenCloseStandIn(t, sockPath)

	r := runtimepi.New("pi-test")
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	result, err := r.DeliverPrompt(ctx, runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: injectSessionID},
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectUnknownEffect {
		t.Fatalf("Effect = %q, want unknown_effect", result.Effect)
	}
}

func TestDeliverPromptNoListenerIsNoEffect(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	r := runtimepi.New("pi-test")

	result, err := r.DeliverPrompt(context.Background(), runtime.PromptDeliveryRequest{
		Binding: runtime.RuntimeBinding{ExternalAgentSessionID: injectSessionID},
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if result.Effect != runtime.PromptEffectNoEffect {
		t.Fatalf("Effect = %q, want no_effect", result.Effect)
	}
}

// startAdmitStandIn listens and, if greet is true, writes one NDJSON
// connect-line (the extension's greeting) before reading the prompt frame.
func startAdmitStandIn(t *testing.T, sockPath string, greet bool) <-chan []byte {
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
		if greet {
			_, _ = conn.Write([]byte(`{"sessionId":"x","idle":true}` + "\n"))
		}
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
		_, _ = conn.Write([]byte(`{"idle":true}` + "\n"))
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
