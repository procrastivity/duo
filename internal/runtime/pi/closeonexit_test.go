package pi_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

// The tests below guard the embedded close-on-exit TypeScript asset the
// same way extension_test.go guards the reporter's: string assertions on
// purpose, because the asset is source Duo generates and never executes,
// and each load-bearing line is one careless edit away from a silently
// wrong pane close.

// TestCloseOnExitExtensionReadsGuardsWithoutScrubbing pins the four
// module-load env reads and confirms none of them are scrubbed — unlike
// the reporter's SOCKET_PATH/TOKEN, none of these four name a secret.
func TestCloseOnExitExtensionReadsGuardsWithoutScrubbing(t *testing.T) {
	src := runtimepi.CloseOnExitExtensionSource

	for _, want := range []string{
		`const DUO_CLOSE_PANE_ON_EXIT = process.env["DUO_CLOSE_PANE_ON_EXIT"] === "1";`,
		`const HERDR_ENV = process.env["HERDR_ENV"] === "1";`,
		`const HERDR_PANE_ID = process.env["HERDR_PANE_ID"] ?? "";`,
		`const HERDR_BIN_PATH = process.env["HERDR_BIN_PATH"] ?? "";`,
	} {
		if !strings.Contains(src, want) {
			t.Errorf("asset lost close-on-exit guard line: %s", want)
		}
	}

	for _, scrubbed := range []string{
		`delete process.env["DUO_CLOSE_PANE_ON_EXIT"]`,
		`delete process.env["HERDR_ENV"]`,
		`delete process.env["HERDR_PANE_ID"]`,
		`delete process.env["HERDR_BIN_PATH"]`,
	} {
		if strings.Contains(src, scrubbed) {
			t.Errorf("asset scrubs %s: none of the four close-on-exit guards name a secret, "+
				"so they are meant to stay readable for pi's whole lifetime", scrubbed)
		}
	}
}

// TestCloseOnExitExtensionDocumentsSecondPiEffect pins notes/51 record 9c:
// DUO_CLOSE_PANE_ON_EXIT lasts the pane's life, so a second pi started by
// hand in that pane also closes it on quit.
func TestCloseOnExitExtensionDocumentsSecondPiEffect(t *testing.T) {
	src := runtimepi.CloseOnExitExtensionSource
	want := "DUO_CLOSE_PANE_ON_EXIT lasts the pane's life, so a second pi started by"
	if !strings.Contains(src, want) {
		t.Errorf("asset lost the notes/51 record 9c doc line containing %q", want)
	}
}

// TestCloseOnExitExtensionActsOnlyOnQuit pins the terminality guard: only
// session_shutdown reason "quit" closes the pane. /reload, /new, /resume,
// and /fork rebind the extension runtime in-process (the same trap
// duo-pi-reporter.ts documents) and must not trip this.
func TestCloseOnExitExtensionActsOnlyOnQuit(t *testing.T) {
	src := runtimepi.CloseOnExitExtensionSource

	if !strings.Contains(src, `pi.on("session_shutdown"`) {
		t.Fatal("asset does not subscribe to session_shutdown")
	}
	if !strings.Contains(src, `if (event?.reason !== "quit") return;`) {
		t.Errorf("asset lost its quit-only session_shutdown guard")
	}
}

// TestCloseOnExitExtensionClosesSynchronouslyAndSwallowsErrors pins the
// close call itself: exact herdr invocation, never awaited, and wrapped in
// a try/catch that swallows everything — pi signal-kills this extension's
// child process on a quit shutdown, so a failed close (or an untrustworthy
// local exit status) must never surface.
func TestCloseOnExitExtensionClosesSynchronouslyAndSwallowsErrors(t *testing.T) {
	src := runtimepi.CloseOnExitExtensionSource

	if !strings.Contains(src, `execFileSync(HERDR_BIN_PATH, ["pane", "close", HERDR_PANE_ID], { stdio: "ignore" });`) {
		t.Errorf("asset lost its close-on-exit execFileSync call")
	}
	if strings.Contains(src, "await execFileSync") {
		t.Errorf("execFileSync must not be awaited: the pane close must stay synchronous")
	}
	if !strings.Contains(src, "} catch {") {
		t.Errorf("asset lost its error-swallowing catch around the close call")
	}
}

// TestCloseOnExitExtensionGuardsAllFourBeforeClosing pins the full guard:
// the extension must check the activation flag, the Herdr-pane markers, and
// the terminal reason before ever calling execFileSync, in the same
// function.
func TestCloseOnExitExtensionGuardsAllFourBeforeClosing(t *testing.T) {
	src := runtimepi.CloseOnExitExtensionSource

	shutdown := strings.Index(src, `pi.on("session_shutdown"`)
	closeCall := strings.Index(src, "execFileSync(HERDR_BIN_PATH")
	if shutdown < 0 || closeCall < 0 {
		t.Fatalf("could not locate session_shutdown handler or its close call")
	}
	body := src[shutdown:closeCall]
	for _, guard := range []string{
		"DUO_CLOSE_PANE_ON_EXIT",
		"HERDR_ENV",
		"HERDR_PANE_ID",
		"HERDR_BIN_PATH",
	} {
		if !strings.Contains(body, guard) {
			t.Errorf("close call at offset %d is not preceded by a %s check", closeCall, guard)
		}
	}
}

// TestCloseOnExitExtensionHasNoModeGate documents the deliberate absence of
// a ctx.mode gate on this asset: unlike the globally-installed reporter,
// this file is only ever loaded by the one pi process Duo launched it for
// (pi's own `-e <path>`), so there is no second invocation it could be
// mistaken for. The session_shutdown handler itself must take no ctx
// parameter at all — that is what makes a mode gate structurally
// unavailable here, not just unneeded — even though ctx.mode may still
// appear in the asset's own comments explaining that absence.
func TestCloseOnExitExtensionHasNoModeGate(t *testing.T) {
	src := runtimepi.CloseOnExitExtensionSource
	if !strings.Contains(src, `pi.on("session_shutdown", (event: any) => {`) {
		t.Errorf("session_shutdown handler takes more than just event: any — " +
			"it should take no ctx parameter, since this asset needs no mode gate")
	}
}

// TestDefaultHarnessDirHonorsXDGDataHome mirrors
// internal/doctor.TestDefaultStorePath_HonorsXDGDataHome's XDG pattern for
// pi's close-on-exit harness directory.
func TestDefaultHarnessDirHonorsXDGDataHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)

	got, err := runtimepi.DefaultHarnessDir("lrr_1", "primary")
	if err != nil {
		t.Fatalf("DefaultHarnessDir: %v", err)
	}
	want := filepath.Join(dir, "duo", "harness", "lrr_1", "primary")
	if got != want {
		t.Errorf("DefaultHarnessDir() = %q, want %q", got, want)
	}
}

// TestDefaultHarnessDirRequiresALaunchResolutionID pins the same guard
// claude's DefaultHarnessDir carries: a per-launch harness directory needs
// a launch-resolution ID to be per-launch at all.
func TestDefaultHarnessDirRequiresALaunchResolutionID(t *testing.T) {
	if _, err := runtimepi.DefaultHarnessDir("", "primary"); err == nil {
		t.Error("DefaultHarnessDir accepted an empty launch-resolution ID")
	}
}

// TestMaterializeCloseOnExit writes the extension into a fresh directory
// and pins its permissions and content, mirroring claude's
// materializeHook helper in closeonexit_test.go.
func TestMaterializeCloseOnExit(t *testing.T) {
	dir := t.TempDir()

	path, err := runtimepi.MaterializeCloseOnExit(dir)
	if err != nil {
		t.Fatalf("MaterializeCloseOnExit: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("MaterializeCloseOnExit path %q is not absolute", path)
	}
	wantPath := filepath.Join(dir, runtimepi.CloseOnExitExtensionFileName)
	if path != wantPath {
		t.Errorf("MaterializeCloseOnExit path = %q, want %q", path, wantPath)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat extension file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("extension file perm = %o, want 0600", info.Mode().Perm())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading materialized extension: %v", err)
	}
	if string(got) != runtimepi.CloseOnExitExtensionSource {
		t.Errorf("materialized extension does not match the embedded source")
	}
}
