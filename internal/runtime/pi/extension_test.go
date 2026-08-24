package pi_test

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"

	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

// The tests below guard the embedded TypeScript asset. They are string
// assertions on purpose: the asset is source Duo generates and never
// executes, three of its lines encode probe findings that cost a live session
// to establish, and each of those lines is one careless edit away from a
// silently wrong reporter.

// Finding, 0.83.0: rpc mode installs a real extension UI context, so
// ctx.hasUI is true there too. Only ctx.mode === "tui" separates the pane's
// session from an rpc-driven pi (notes/18 §2, conformance §7.4).
func TestExtensionGatesOnModeNotHasUI(t *testing.T) {
	src := runtimepi.ExtensionSource()

	if !strings.Contains(src, `if (ctx.mode !== "tui") return;`) {
		t.Errorf("asset lost its ctx.mode pane-session gate")
	}
	if !strings.Contains(src, runtimepi.PaneSessionMode) {
		t.Errorf("asset does not mention the pane-session mode %q", runtimepi.PaneSessionMode)
	}

	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(line, "ctx.hasUI") || strings.HasPrefix(trimmed, "//") {
			continue
		}
		// The one legitimate use in code is reporting the value for
		// diagnostics; comments may of course explain the refuted gate.
		if !strings.HasPrefix(trimmed, "hasUI:") {
			t.Errorf("line %d uses ctx.hasUI outside the reported claim field, "+
				"which is the exact gate the 0.83.0 probe refuted: %s", i+1, strings.TrimSpace(line))
		}
	}
}

// Finding, 0.83.0: the credential is delivered in the environment, read at
// module load before any turn can run, and deleted from process.env so no
// tool subprocess inherits it (notes/18 §6).
func TestExtensionReadsCredentialThenScrubsIt(t *testing.T) {
	src := runtimepi.ExtensionSource()

	readSocket := strings.Index(src, `const SOCKET_PATH = process.env["__DUO_SOCKET_ENV__"]`)
	readToken := strings.Index(src, `const TOKEN = process.env["__DUO_TOKEN_ENV__"]`)
	scrubSocket := strings.Index(src, `delete process.env["__DUO_SOCKET_ENV__"];`)
	scrubToken := strings.Index(src, `delete process.env["__DUO_TOKEN_ENV__"];`)
	handlers := strings.Index(src, "\nexport default function")

	for name, at := range map[string]int{
		"socket read": readSocket, "token read": readToken,
		"socket scrub": scrubSocket, "token scrub": scrubToken,
		"default export": handlers,
	} {
		if at < 0 {
			t.Fatalf("asset has no %s", name)
		}
	}
	if readSocket > scrubSocket || readToken > scrubToken {
		t.Errorf("asset scrubs an environment variable before reading it")
	}
	if scrubSocket > handlers || scrubToken > handlers {
		t.Errorf("asset scrubs after the default export: the scrub must happen at module load, " +
			"before any handler can run")
	}
	if last := strings.LastIndex(src, "process.env"); last > handlers {
		t.Errorf("asset touches process.env inside a handler, at offset %d: "+
			"the environment is read once at module load and then gone", last)
	}
}

// Findings, 0.83.0: session_shutdown is terminal only on reason "quit"
// (/reload, /new, /resume, /fork rebind the extension runtime while the agent
// lives on); the idle edge is agent_settled, because after agent_end pi may
// still retry, compact, or run a queued follow-up; and an rpc rebind emits
// session_start twice, so serving must dedupe on the session id.
func TestExtensionTerminalityAndLifecycleMarkers(t *testing.T) {
	src := runtimepi.ExtensionSource()

	if !strings.Contains(src, `if (event?.reason !== "quit") return;`) {
		t.Errorf("asset lost its quit-only session_shutdown guard")
	}
	if !strings.Contains(src, `pi.on("agent_settled"`) {
		t.Errorf("asset does not subscribe to agent_settled")
	}
	if strings.Contains(src, `"agent_end"`) {
		t.Errorf("asset subscribes to agent_end: the settled edge is agent_settled, " +
			"because after agent_end pi may still retry, compact, or run a queued follow-up")
	}
	if !strings.Contains(src, "sessionId === servedSessionId") {
		t.Errorf("asset lost its session_start dedupe, which an rpc rebind needs")
	}
}

// Provisional close-on-exit: on a quit shutdown, if Duo launched the pane
// with DUO_CLOSE_PANE_ON_EXIT=1 and the pane is Herdr's (HERDR_ENV=1, a pane
// id, and Herdr's own bin path all present), the extension asks Herdr to
// close its own pane. The activation flag and Herdr's pane identifiers name
// no secret, so — unlike SOCKET_PATH/TOKEN — they are read at module load
// but never scrubbed; closePaneOnExit runs after stop(), because closing the
// pane can tear the process down before anything queued after it would run.
func TestExtensionClosesPaneOnExitWhenActivated(t *testing.T) {
	src := runtimepi.ExtensionSource()

	for _, want := range []string{
		`const CLOSE_PANE_ON_EXIT = process.env["DUO_CLOSE_PANE_ON_EXIT"] === "1";`,
		`const HERDR_ENV = process.env["HERDR_ENV"] === "1";`,
		`const HERDR_PANE_ID = process.env["HERDR_PANE_ID"] ?? "";`,
		`const HERDR_BIN_PATH = process.env["HERDR_BIN_PATH"] ?? "";`,
		"function closePaneOnExit()",
		`execFileSync(HERDR_BIN_PATH, ["pane", "close", HERDR_PANE_ID], { stdio: "ignore" });`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("asset lost close-on-exit line: %s", want)
		}
	}

	// None of the four gates were scrubbed with `delete process.env[...]`:
	// this is the deliberate stay-readable decision, not an oversight.
	for _, scrubbed := range []string{
		`delete process.env["DUO_CLOSE_PANE_ON_EXIT"]`,
		`delete process.env["HERDR_ENV"]`,
		`delete process.env["HERDR_PANE_ID"]`,
		`delete process.env["HERDR_BIN_PATH"]`,
	} {
		if strings.Contains(src, scrubbed) {
			t.Errorf("asset scrubs %s: close-on-exit's flag and Herdr's pane "+
				"identifiers name no secret and are meant to stay readable", scrubbed)
		}
	}

	// Ordering: stop() releases the reporter's own socket server before
	// closePaneOnExit() can tear the process down.
	shutdown := strings.Index(src, `pi.on("session_shutdown"`)
	stopCall := strings.Index(src[shutdown:], "stop();")
	closeCall := strings.Index(src[shutdown:], "closePaneOnExit();")
	if shutdown < 0 || stopCall < 0 || closeCall < 0 {
		t.Fatalf("could not locate session_shutdown's stop()/closePaneOnExit() calls")
	}
	if stopCall > closeCall {
		t.Errorf("asset calls closePaneOnExit() before stop(): closing the pane can end " +
			"the process, so the reporter's own cleanup must run first")
	}

	// Synchronous and error-swallowing: the process is exiting, so async work
	// would race the exit, and a failed close must never break pi shutdown.
	if strings.Contains(src, "execFileSync") && strings.Contains(src, "await execFileSync") {
		t.Errorf("execFileSync must not be awaited: the pane close must stay synchronous")
	}
}

// Native prompt delivery is proven and documented, and deliberately absent
// from the Stage 1 asset: it is Duo's Stage 2+ prompt surface.
func TestExtensionHasNoPromptDeliveryCall(t *testing.T) {
	src := runtimepi.ExtensionSource()
	for _, call := range []string{"sendUserMessage(", "ctx.abort(", "deliverAs"} {
		if strings.Contains(src, call) {
			t.Errorf("asset contains %q: prompt delivery is Stage 2+, not this step", call)
		}
	}
	if !strings.Contains(src, "sendUserMessage") || !strings.Contains(src, "ctx.abort") {
		t.Errorf("asset should still document the proven inject capability it does not implement")
	}
}

// The socket line and the Go decoder are one wire contract; the decoder is
// strict, so a field added on either side alone breaks correlation.
func TestExtensionClaimFieldsMatchDecoder(t *testing.T) {
	src := runtimepi.ExtensionSource()
	start := strings.Index(src, "JSON.stringify({")
	if start < 0 {
		t.Fatalf("asset has no claim literal")
	}
	end := strings.Index(src[start:], "\n    })")
	if end < 0 {
		t.Fatalf("asset claim literal is not terminated as expected")
	}
	literal := src[start : start+end]

	keyPattern := regexp.MustCompile(`(?m)^\s*(\w+):`)
	var assetKeys []string
	for _, m := range keyPattern.FindAllStringSubmatch(literal, -1) {
		assetKeys = append(assetKeys, m[1])
	}

	var goKeys []string
	rt := reflect.TypeOf(runtimepi.ReportedClaim{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		goKeys = append(goKeys, strings.Split(tag, ",")[0])
	}

	sort.Strings(assetKeys)
	sort.Strings(goKeys)
	if strings.Join(assetKeys, ",") != strings.Join(goKeys, ",") {
		t.Fatalf("claim fields drifted:\n asset: %v\n    go: %v", assetKeys, goKeys)
	}
}

func TestRenderExtension(t *testing.T) {
	raw := runtimepi.ExtensionSource()
	for _, placeholder := range []string{"__DUO_PROTOCOL__", "__DUO_SOCKET_ENV__", "__DUO_TOKEN_ENV__"} {
		if !strings.Contains(raw, placeholder) {
			t.Fatalf("asset lost placeholder %s", placeholder)
		}
	}

	rendered, err := runtimepi.RenderExtension(runtimepi.ExtensionConfig{})
	if err != nil {
		t.Fatalf("RenderExtension: %v", err)
	}
	if strings.Contains(rendered, "__DUO_") {
		t.Errorf("rendered extension still holds a placeholder")
	}
	for _, want := range []string{
		runtimepi.ReporterProtocol,
		runtimepi.DefaultSocketEnvVar,
		runtimepi.DefaultTokenEnvVar,
	} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered extension does not mention %q", want)
		}
	}

	custom, err := runtimepi.RenderExtension(runtimepi.ExtensionConfig{
		SocketEnvVar: "DUO_ALT_SOCKET",
		TokenEnvVar:  "DUO_ALT_TOKEN",
	})
	if err != nil {
		t.Fatalf("RenderExtension (custom): %v", err)
	}
	if !strings.Contains(custom, `process.env["DUO_ALT_TOKEN"]`) ||
		!strings.Contains(custom, `delete process.env["DUO_ALT_SOCKET"];`) {
		t.Errorf("custom variable names did not reach both the read and the scrub")
	}

	if _, err := runtimepi.RenderExtension(runtimepi.ExtensionConfig{
		TokenEnvVar: `X"] ?? process.env["PATH`,
	}); err == nil {
		t.Errorf("a name that is not an environment-variable name must be refused, not injected")
	}
}

func TestReporterEnvironment(t *testing.T) {
	cfg := runtimepi.ExtensionConfig{}

	env, err := runtimepi.ReporterEnvironment(cfg, "/run/user/1000/duo/pi-1.sock", "token-1")
	if err != nil {
		t.Fatalf("ReporterEnvironment: %v", err)
	}
	if env[runtimepi.DefaultSocketEnvVar] != "/run/user/1000/duo/pi-1.sock" ||
		env[runtimepi.DefaultTokenEnvVar] != "token-1" {
		t.Fatalf("environment = %v, want both reporter entries", env)
	}

	// The 108-byte sun_path limit is a live failure mode: the 0.83.0 probe's
	// first listen attempt died with EINVAL on a long scratchpad path.
	long := "/tmp/" + strings.Repeat("a", runtimepi.MaxUnixSocketPath) + "/duo.sock"
	if _, err := runtimepi.ReporterEnvironment(cfg, long, "token-1"); err == nil {
		t.Errorf("an over-long socket path must be refused where it is legible, not at listen")
	}
	if _, err := runtimepi.ReporterEnvironment(cfg, "duo.sock", "token-1"); err == nil {
		t.Errorf("a relative socket path must be refused: it would bind against pi's cwd")
	}
	if _, err := runtimepi.ReporterEnvironment(cfg, "/run/duo/pi.sock", ""); err == nil {
		t.Errorf("an empty credential must be refused: the credential names one runtime instance")
	}
}
