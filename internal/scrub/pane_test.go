package scrub

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPaneCommandShape(t *testing.T) {
	line := PaneCommand("claude", "-p", "hello world")
	if !strings.HasPrefix(line, "env ") {
		t.Fatalf("PaneCommand line does not start with %q: %s", "env ", line)
	}
	for _, m := range Markers {
		if !strings.Contains(line, "-u "+m) {
			t.Errorf("PaneCommand line does not pass -u %s: %s", m, line)
		}
	}
	if !strings.Contains(line, "sh -c ") {
		t.Errorf("PaneCommand line does not invoke sh -c: %s", line)
	}
	// The multi-word argument must survive as ONE quoted token, not be
	// split on its internal space: this is the exact defect a naive
	// argv-join (space-separated, unquoted) would reproduce, and the one
	// TestPaneCommandActuallyScrubsAtExecTime below proves does not
	// happen at exec time.
	if !strings.Contains(line, shellQuote("hello world")) {
		t.Errorf("PaneCommand line does not carry the multi-word argument as one quoted token: %s", line)
	}
}

func TestShellQuoteRoundTripsThroughSh(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	cases := []string{
		"claude",
		"hello world",
		`it's got a quote`,
		"multiple   spaces",
		`$(echo injected)`,
		"; rm -rf /",
		"back`tick`",
	}
	for _, c := range cases {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		script := "printf '%s' " + shellQuote(c)
		out, err := exec.CommandContext(ctx, "sh", "-c", script).Output()
		cancel()
		if err != nil {
			t.Fatalf("shellQuote(%q) produced a token sh rejected: %v", c, err)
		}
		if string(out) != c {
			t.Errorf("shellQuote(%q) round-tripped as %q", c, string(out))
		}
	}
}

// TestPaneCommandScriptIsWellFormedShell requires /bin/sh (present on
// every platform this project targets) and checks the generated line
// parses with `sh -n`, which parses without executing — the "well-formed
// shell" requirement, exercised on exactly the shape a pane's shell
// receives (one line on stdin), not a synthetic decomposition of it.
func TestPaneCommandScriptIsWellFormedShell(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not on PATH")
	}
	line := PaneCommand("claude", "-p", "hello world")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-n")
	cmd.Stdin = strings.NewReader(line)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("sh -n rejected the generated line: %v\nline: %s\noutput: %s", err, line, out)
	}
}

// TestPaneCommandActuallyScrubsAtExecTime is the strongest form of the
// well-formedness check: it runs the generated line for real through
// `sh -c <line>` — the same evaluation a pane's shell performs on a typed
// line — with markers injected only into the *inherited* environment
// (the shape a Herdr pane's shell sees, since Herdr's env map cannot
// remove them), and asserts the executed command saw none of them and
// that its multi-word argument arrived intact.
func TestPaneCommandActuallyScrubsAtExecTime(t *testing.T) {
	for _, bin := range []string{"sh", "env", "sed"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s not on PATH", bin)
		}
	}
	line := PaneCommand("sh", "-c", `printf 'ARG=[%s]\n' "$1"; env`, "arg0-placeholder", "hello world")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", line)
	cmd.Env = []string{
		"PATH=" + mustLookPathDir(t, "env") + ":" + mustLookPathDir(t, "sh") + ":" + mustLookPathDir(t, "sed"),
		"CLAUDECODE=1",
		"AI_AGENT=1",
		"CLAUDE_CODE_CHILD_SESSION=abc",
		"CLAUDE_CODE_ENTRYPOINT=duo",
		"CLAUDE_CONFIG_DIR=/x",
		"HARMLESS=keep-me",
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("running PaneCommand's line failed: %v\noutput: %s", err, out)
	}
	got := string(out)
	if !strings.Contains(got, "ARG=[hello world]") {
		t.Errorf("the multi-word argument did not arrive as one token; output:\n%s", got)
	}
	if !strings.Contains(got, "HARMLESS=keep-me") {
		t.Errorf("the wrapped command lost an unrelated variable; output:\n%s", got)
	}
	for _, marker := range []string{"CLAUDECODE", "AI_AGENT", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_ENTRYPOINT", "CLAUDE_CONFIG_DIR"} {
		if strings.Contains(got, marker+"=") {
			t.Errorf("marker %s survived into the wrapped command's environment; output:\n%s", marker, got)
		}
	}
}

func mustLookPathDir(t *testing.T, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("LookPath(%s): %v", name, err)
	}
	return strings.TrimSuffix(p, "/"+name)
}
