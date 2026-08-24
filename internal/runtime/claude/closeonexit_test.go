package claude_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	runtimeclaude "github.com/procrastivity/duo/internal/runtime/claude"
)

// materializeHook writes the embedded hook script (and its settings
// sidecar, unused by these tests but exercised for coverage of the whole
// materialization path) into a fresh temp directory and returns the hook
// script's path.
func materializeHook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := runtimeclaude.MaterializeCloseOnExit(dir); err != nil {
		t.Fatalf("MaterializeCloseOnExit: %v", err)
	}
	hookPath := filepath.Join(dir, runtimeclaude.CloseOnExitHookFileName)
	if info, err := os.Stat(hookPath); err != nil {
		t.Fatalf("stat hook script: %v", err)
	} else if info.Mode().Perm() != 0o700 {
		t.Errorf("hook script perm = %o, want 0700", info.Mode().Perm())
	}
	return hookPath
}

// fakeHerdrBin writes a recorder script that stands in for HERDR_BIN_PATH:
// it appends its own argv, space-joined, as one line to logPath. A test
// reads that log to assert exactly what (if anything) the hook invoked.
func fakeHerdrBin(t *testing.T) (binPath, logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "invocations.log")
	binPath = filepath.Join(dir, "herdr")
	script := "#!/bin/sh\necho \"$*\" >> " + shQuote(logPath) + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o700); err != nil {
		t.Fatalf("writing fake herdr recorder: %v", err)
	}
	return binPath, logPath
}

func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// herdrStrippedEnviron returns the current process environment with every
// HERDR_* variable removed. Needed because this repository's own
// development environment runs inside a Herdr pane, so os.Environ()
// already carries HERDR_ENV, HERDR_PANE_ID, and HERDR_BIN_PATH — exactly
// the values a "not a Herdr pane" test case needs to be genuinely absent.
func herdrStrippedEnviron() []string {
	env := os.Environ()
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if strings.HasPrefix(kv, "HERDR_") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// runHook runs the hook script with sh, the given extra environment on top
// of a HERDR_*-stripped copy of the test process's own (so PATH, HOME, etc.
// are intact for `cat`, `sed`, and `sh` itself, but a test that wants "no
// Herdr environment" gets exactly that even when `go test` itself runs
// inside a Herdr pane — as this package's own development environment
// does), and stdin as the SessionEnd payload. It fails t if the script
// does not exit 0 — every path through it must, per the script's own doc
// comment.
func runHook(t *testing.T, hookPath string, extraEnv []string, stdin string) {
	t.Helper()
	cmd := exec.Command("sh", hookPath)
	cmd.Env = append(herdrStrippedEnviron(), extraEnv...)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("hook script did not exit 0: %v\nstdout: %s\nstderr: %s", err, out.String(), errOut.String())
	}
}

func readLog(t *testing.T, logPath string) (contents string, exists bool) {
	t.Helper()
	b, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		t.Fatalf("reading recorder log: %v", err)
	}
	return string(b), true
}

// TestCloseOnExitHookClosesOnATerminalReason is case (a): a herdr-shaped
// environment plus reason "prompt_input_exit" (the observed /exit reason,
// see notes/45 and the hook script's own TERMINAL_REASONS comment) calls
// the recorder with exactly "pane close <id>".
func TestCloseOnExitHookClosesOnATerminalReason(t *testing.T) {
	hookPath := materializeHook(t)
	binPath, logPath := fakeHerdrBin(t)

	runHook(t, hookPath, []string{
		"HERDR_ENV=1",
		"HERDR_PANE_ID=pane-7",
		"HERDR_BIN_PATH=" + binPath,
	}, `{"hook_event_name":"SessionEnd","reason":"prompt_input_exit"}`)

	got, ok := readLog(t, logPath)
	if !ok {
		t.Fatal("the recorder was never invoked for a terminal reason")
	}
	if want := "pane close pane-7"; strings.TrimSpace(got) != want {
		t.Errorf("recorder invoked with %q, want %q", strings.TrimSpace(got), want)
	}
}

// TestCloseOnExitHookLeavesTheLogoutReasonAlone pins "logout", the other
// name in TERMINAL_REASONS, the same way.
func TestCloseOnExitHookClosesOnLogout(t *testing.T) {
	hookPath := materializeHook(t)
	binPath, logPath := fakeHerdrBin(t)

	runHook(t, hookPath, []string{
		"HERDR_ENV=1",
		"HERDR_PANE_ID=pane-9",
		"HERDR_BIN_PATH=" + binPath,
	}, `{"hook_event_name":"SessionEnd","reason":"logout"}`)

	got, ok := readLog(t, logPath)
	if !ok {
		t.Fatal("the recorder was never invoked for reason \"logout\"")
	}
	if want := "pane close pane-9"; strings.TrimSpace(got) != want {
		t.Errorf("recorder invoked with %q, want %q", strings.TrimSpace(got), want)
	}
}

// TestCloseOnExitHookDoesNotCloseOnANonTerminalReason is case (b): "clear"
// fires SessionEnd without the process exiting, so the hook must not close
// the pane out from under a still-live agent.
func TestCloseOnExitHookDoesNotCloseOnANonTerminalReason(t *testing.T) {
	hookPath := materializeHook(t)
	binPath, logPath := fakeHerdrBin(t)

	runHook(t, hookPath, []string{
		"HERDR_ENV=1",
		"HERDR_PANE_ID=pane-7",
		"HERDR_BIN_PATH=" + binPath,
	}, `{"hook_event_name":"SessionEnd","reason":"clear"}`)

	if _, ok := readLog(t, logPath); ok {
		t.Error("the recorder was invoked for reason \"clear\", a known non-terminal SessionEnd")
	}
}

// TestCloseOnExitHookDoesNotCloseOnResume is "resume", the other known
// non-terminal reason.
func TestCloseOnExitHookDoesNotCloseOnResume(t *testing.T) {
	hookPath := materializeHook(t)
	binPath, logPath := fakeHerdrBin(t)

	runHook(t, hookPath, []string{
		"HERDR_ENV=1",
		"HERDR_PANE_ID=pane-7",
		"HERDR_BIN_PATH=" + binPath,
	}, `{"hook_event_name":"SessionEnd","reason":"resume"}`)

	if _, ok := readLog(t, logPath); ok {
		t.Error("the recorder was invoked for reason \"resume\", a known non-terminal SessionEnd")
	}
}

// TestCloseOnExitHookDoesNothingOutsideHerdr is case (c): without the
// HERDR_ENV/HERDR_PANE_ID guard vars, the hook must exit 0 and touch
// nothing, even with a terminal reason and a working recorder available.
func TestCloseOnExitHookDoesNothingOutsideHerdr(t *testing.T) {
	hookPath := materializeHook(t)
	binPath, logPath := fakeHerdrBin(t)

	// Deliberately no HERDR_ENV and no HERDR_PANE_ID: HERDR_BIN_PATH alone
	// (pointed at a working recorder) must not be enough to make the hook
	// act.
	runHook(t, hookPath, []string{
		"HERDR_BIN_PATH=" + binPath,
	}, `{"hook_event_name":"SessionEnd","reason":"prompt_input_exit"}`)

	if _, ok := readLog(t, logPath); ok {
		t.Error("the recorder was invoked outside a Herdr-shaped environment")
	}
}

// TestCloseOnExitHookDoesNothingWithoutAnExecutableBinPath rounds out the
// guard: HERDR_ENV and HERDR_PANE_ID alone, with HERDR_BIN_PATH pointing
// at something that is not executable, must also do nothing.
func TestCloseOnExitHookDoesNothingWithoutAnExecutableBinPath(t *testing.T) {
	hookPath := materializeHook(t)
	notExecutable := filepath.Join(t.TempDir(), "not-herdr")
	if err := os.WriteFile(notExecutable, []byte("#!/bin/sh\nexit 0\n"), 0o600); err != nil {
		t.Fatalf("writing non-executable stand-in: %v", err)
	}

	runHook(t, hookPath, []string{
		"HERDR_ENV=1",
		"HERDR_PANE_ID=pane-7",
		"HERDR_BIN_PATH=" + notExecutable,
	}, `{"hook_event_name":"SessionEnd","reason":"prompt_input_exit"}`)
	// No recorder log to check here (HERDR_BIN_PATH is not the recorder);
	// the guard tripping the exit is what runHook already asserted (exit
	// 0), and the point of this case is that the guard *does* trip
	// rather than the script trying to exec a non-executable file.
}

// TestRenderCloseOnExitSettingsPointsAtTheHookAbsolutePath guards the
// generated settings document's shape: one SessionEnd hook, "type":
// "command", and the exact absolute path handed in.
func TestRenderCloseOnExitSettingsPointsAtTheHookAbsolutePath(t *testing.T) {
	hookPath := filepath.Join(t.TempDir(), runtimeclaude.CloseOnExitHookFileName)
	b, err := runtimeclaude.RenderCloseOnExitSettings(hookPath)
	if err != nil {
		t.Fatalf("RenderCloseOnExitSettings: %v", err)
	}
	got := string(b)
	for _, want := range []string{`"SessionEnd"`, `"type": "command"`, hookPath} {
		if !strings.Contains(got, want) {
			t.Errorf("rendered settings does not contain %q:\n%s", want, got)
		}
	}
}

// TestRenderCloseOnExitSettingsRejectsARelativePath: the settings document
// outlives the working directory it was generated from, so a relative hook
// path would resolve against whatever directory Claude Code happens to run
// in, not this one.
func TestRenderCloseOnExitSettingsRejectsARelativePath(t *testing.T) {
	if _, err := runtimeclaude.RenderCloseOnExitSettings("relative/hook.sh"); err == nil {
		t.Fatal("RenderCloseOnExitSettings accepted a relative path")
	}
}
