package pi_test

import (
	"bytes"
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"

	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

func TestReadInjectIdleTrue(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	sockPath := setupIdleSocket(t, dir, injectSessionID)

	clientData := startIdleGreetStandIn(t, sockPath, `{"idle":true}`)

	got, err := runtimepi.ReadInjectIdle(context.Background(), injectSessionID)
	if err != nil {
		t.Fatalf("ReadInjectIdle: %v", err)
	}
	if !got {
		t.Fatal("ReadInjectIdle = false, want true")
	}
	assertNoClientWrite(t, clientData)
}

func TestReadInjectIdleFalse(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	sockPath := setupIdleSocket(t, dir, injectSessionID)

	clientData := startIdleGreetStandIn(t, sockPath, `{"idle":false}`)

	got, err := runtimepi.ReadInjectIdle(context.Background(), injectSessionID)
	if err != nil {
		t.Fatalf("ReadInjectIdle: %v", err)
	}
	if got {
		t.Fatal("ReadInjectIdle = true, want false")
	}
	assertNoClientWrite(t, clientData)
}

func TestReadInjectIdleRealShapedClaimLine(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	sockPath := setupIdleSocket(t, dir, injectSessionID)

	line := `{"sessionId":"x","sessionFile":"","cwd":"","hasUI":false,"mode":"tui","idle":true}`
	clientData := startIdleGreetStandIn(t, sockPath, line)

	got, err := runtimepi.ReadInjectIdle(context.Background(), injectSessionID)
	if err != nil {
		t.Fatalf("ReadInjectIdle: %v", err)
	}
	if !got {
		t.Fatal("ReadInjectIdle = false, want true")
	}
	assertNoClientWrite(t, clientData)
}

func TestReadInjectIdleNoListener(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)

	got, err := runtimepi.ReadInjectIdle(context.Background(), injectSessionID)
	if err != nil {
		t.Fatalf("ReadInjectIdle: %v, want nil", err)
	}
	if got {
		t.Fatal("ReadInjectIdle = true, want false with no listener")
	}
}

func TestReadInjectIdlePathShapedIdentity(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	transcript := "/tmp/sessions/--cwd--/2026-08-23T00-50-57-121Z_" + injectSessionID + ".jsonl"
	sockPath := setupIdleSocket(t, dir, injectSessionID)

	clientData := startIdleGreetStandIn(t, sockPath, `{"idle":true}`)

	got, err := runtimepi.ReadInjectIdle(context.Background(), transcript)
	if err != nil {
		t.Fatalf("ReadInjectIdle: %v", err)
	}
	if !got {
		t.Fatal("ReadInjectIdle = false, want true for path-shaped identity")
	}
	assertNoClientWrite(t, clientData)
}

func TestReadInjectIdleIgnoresDUOPISock(t *testing.T) {
	dir := shortRuntimeDir(t)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	sockPath := setupIdleSocket(t, dir, injectSessionID)

	decoy := filepath.Join(dir, "decoy.sock")
	decoyHit := startDecoyStandIn(t, decoy)
	t.Setenv("DUO_PI_SOCK", decoy)

	clientData := startIdleGreetStandIn(t, sockPath, `{"idle":true}`)

	got, err := runtimepi.ReadInjectIdle(context.Background(), injectSessionID)
	if err != nil {
		t.Fatalf("ReadInjectIdle: %v", err)
	}
	if !got {
		t.Fatal("ReadInjectIdle = false, want true")
	}
	assertNoClientWrite(t, clientData)

	select {
	case <-decoyHit:
		t.Fatal("dialed DUO_PI_SOCK; Go locate must ignore the test-process env")
	default:
	}
}

func TestReadInjectIdleEmptySessionErrors(t *testing.T) {
	_, err := runtimepi.ReadInjectIdle(context.Background(), "")
	if err == nil {
		t.Fatal("ReadInjectIdle: want an error for an empty session id")
	}
}

func setupIdleSocket(t *testing.T, dir, sessionID string) string {
	t.Helper()
	t.Setenv("XDG_RUNTIME_DIR", dir)
	sockPath, err := runtimepi.InjectSocketPath(sessionID)
	if err != nil {
		t.Fatalf("InjectSocketPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(sockPath), 0o700); err != nil {
		t.Fatalf("mkdir inject socket dir: %v", err)
	}
	return sockPath
}

// startIdleGreetStandIn accepts one connection, writes one NDJSON greeting
// line, and reports any bytes the client sent (ReadInjectIdle must not write).
func startIdleGreetStandIn(t *testing.T, sockPath, greetLine string) <-chan []byte {
	t.Helper()
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen %s: %v", sockPath, err)
	}
	clientData := make(chan []byte, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			buf := make([]byte, 64*1024)
			n, _ := conn.Read(buf)
			if n > 0 {
				clientData <- append([]byte(nil), buf[:n]...)
			}
		}()

		if _, err := conn.Write([]byte(greetLine + "\n")); err != nil {
			return
		}
		<-readDone
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		wg.Wait()
	})
	return clientData
}

func assertNoClientWrite(t *testing.T, clientData <-chan []byte) {
	t.Helper()
	select {
	case data := <-clientData:
		if len(data) > 0 {
			if bytes.Contains(data, []byte(`{"text":`)) {
				t.Fatalf("client sent prompt frame %q; ReadInjectIdle must not write", data)
			}
			t.Fatalf("client sent unexpected data %q", data)
		}
	default:
	}
}
