package pi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tests below guard the inject TypeScript asset the same way
// closeonexit_test.go guards close-on-exit: string assertions on purpose,
// because the asset is source Duo generates and never executes, and each
// load-bearing line is one careless edit away from a silently wrong
// prompt delivery path.

func readInjectAsset(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("inject", "duo-inject.ts"))
	if err != nil {
		t.Fatalf("reading inject asset: %v", err)
	}
	return string(src)
}

// TestInjectAssetReadsEnvWithoutScrubbing pins the module-load env reads
// and confirms DUO_PI_SOCK is not scrubbed — unlike the reporter's
// SOCKET_PATH/TOKEN, the socket path override names no secret.
func TestInjectAssetReadsEnvWithoutScrubbing(t *testing.T) {
	src := readInjectAsset(t)

	for _, want := range []string{
		`const DUO_PI_SOCK = process.env["DUO_PI_SOCK"] ?? "";`,
		`const XDG_RUNTIME_DIR = process.env["XDG_RUNTIME_DIR"] ?? "";`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("asset lost inject env line: %s", want)
		}
	}

	if strings.Contains(src, `delete process.env["DUO_PI_SOCK"]`) {
		t.Errorf("asset scrubs DUO_PI_SOCK: the path override names no secret, " +
			"so it is meant to stay readable for pi's whole lifetime")
	}
}

// TestInjectAssetSessionStartHandler pins the session_start signature,
// dedupe guard, and socket path convention.
func TestInjectAssetSessionStartHandler(t *testing.T) {
	src := readInjectAsset(t)

	if !strings.Contains(src, `pi.on("session_start", (event: any, ctx: any) => {`) {
		t.Errorf("asset lost session_start handler signature")
	}
	if !strings.Contains(src, "sessionId === servedSessionId") {
		t.Errorf("asset lost servedSessionId dedupe guard")
	}
	if !strings.Contains(src, "duo/pi-inject/") {
		t.Errorf("asset lost duo/pi-inject/ socket path convention")
	}
}

// TestInjectAssetWritesClaimLineOnConnect pins that the connect handler
// writes the claim line and keeps the connection open for prompt delivery.
func TestInjectAssetWritesClaimLineOnConnect(t *testing.T) {
	src := readInjectAsset(t)

	if !strings.Contains(src, "socket.write(claimLine(ctx))") {
		t.Errorf("asset must socket.write(claimLine(ctx)) on connect")
	}
	if strings.Contains(src, "socket.end(claimLine") {
		t.Errorf("asset must not socket.end(claimLine): the connection stays open for delivery")
	}
}

// TestInjectAssetDeliversPrompts pins prompt delivery via sendUserMessage.
func TestInjectAssetDeliversPrompts(t *testing.T) {
	src := readInjectAsset(t)

	if !strings.Contains(src, "pi.sendUserMessage(parsed.text)") {
		t.Errorf("asset must call pi.sendUserMessage(parsed.text) for prompt frames")
	}
}

// TestInjectAssetClaimLineFields pins the connect-line NDJSON shape.
func TestInjectAssetClaimLineFields(t *testing.T) {
	src := readInjectAsset(t)

	if !strings.Contains(src, "JSON.stringify({") {
		t.Fatal("asset has no JSON.stringify claim line")
	}
	for _, key := range []string{
		"sessionId",
		"sessionFile",
		"cwd",
		"hasUI",
		"mode",
		"idle",
	} {
		if !strings.Contains(src, key) {
			t.Errorf("claim line lost key %q", key)
		}
	}
	if !strings.Contains(src, "ctx.isIdle()") {
		t.Errorf("claim line must read idle from ctx.isIdle()")
	}
}

// TestInjectAssetActsOnlyOnQuit pins the terminality guard on session_shutdown.
func TestInjectAssetActsOnlyOnQuit(t *testing.T) {
	src := readInjectAsset(t)

	if !strings.Contains(src, `pi.on("session_shutdown"`) {
		t.Fatal("asset does not subscribe to session_shutdown")
	}
	if !strings.Contains(src, `if (event?.reason !== "quit") return;`) {
		t.Errorf("asset lost its quit-only session_shutdown guard")
	}
}

// TestInjectAssetCreatesSocketDirSecurely pins mkdirSync with mode 0o700.
func TestInjectAssetCreatesSocketDirSecurely(t *testing.T) {
	src := readInjectAsset(t)

	if !strings.Contains(src, "mkdirSync") {
		t.Fatal("asset does not mkdir the socket directory")
	}
	if !strings.Contains(src, "0o700") {
		t.Errorf("asset lost mkdirSync mode 0o700")
	}
}

// TestInjectAssetHasNoModeGate documents the deliberate absence of a
// ctx.mode gate: unlike the globally-installed reporter, this file is only
// ever loaded by the one pi process Duo launched it for (pi's own
// `-e <path>`), so there is no second invocation it could be mistaken for.
func TestInjectAssetHasNoModeGate(t *testing.T) {
	src := readInjectAsset(t)

	if !strings.Contains(src, "no ctx.mode gate") && !strings.Contains(src, "No ctx.mode gate") {
		t.Errorf("asset must document no ctx.mode gate in its file comment")
	}
	for _, forbidden := range []string{
		`ctx.mode !== "tui"`,
		`ctx.mode === "tui"`,
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("asset must not contain mode gate %q", forbidden)
		}
	}
}

// TestInjectAssetOmitsForbiddenDeliveryAndAbort pins absence of abort
// handling, deliverAs, agent_settled subscription, and ctx.abort calls.
func TestInjectAssetOmitsForbiddenDeliveryAndAbort(t *testing.T) {
	src := readInjectAsset(t)

	for _, forbidden := range []string{
		"ctx.abort(",
		"deliverAs",
		`pi.on("agent_settled"`,
		`{"abort"`,
	} {
		if strings.Contains(src, forbidden) {
			t.Errorf("asset must not contain %q", forbidden)
		}
	}
}
