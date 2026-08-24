package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/exitcode"
)

// TestOutputFlagIsGlobal pins the chassis-wide result-format spelling: one
// --output flag bound at root, honored by a verb from every family — the
// plumbing verbs that used to read the retired global --json bool
// (version, doctor, manifest, config migrate) and the session-family verbs
// that used to redeclare --output on their own command tree.
//
// The retired --json is checked here too: it must be an unknown flag (exit
// 2), not a quiet alias. The 2026-08-24 chassis call retired it outright.
func TestOutputFlagIsGlobal(t *testing.T) {
	for _, args := range [][]string{
		{"version", "--output", "json"},
		{"manifest", "--output", "json"},
		{"doctor", "--output", "json"},
		{"session", "list", "--output", "json"},
	} {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		code, out, errOut := runSession(t, args...)
		if code != exitcode.Success {
			t.Fatalf("%v: exit code = %d, want %d (stderr: %s)", args, code, exitcode.Success, errOut)
		}
		if !json.Valid([]byte(out)) {
			t.Errorf("%v: stdout is not JSON: %s", args, out)
		}
	}

	for _, args := range [][]string{
		{"version", "--json"},
		{"doctor", "--json"},
	} {
		code, _, errOut := runSession(t, args...)
		if code != exitcode.Usage {
			t.Errorf("%v: exit code = %d, want %d — --json is retired, with no alias", args, code, exitcode.Usage)
		}
		if !strings.Contains(errOut, "unknown flag: --json") {
			t.Errorf("%v: stderr = %q, want an unknown-flag report", args, errOut)
		}
	}
}

// TestOutputFlagRejectsUnknownFormat pins that the one validation of
// --output lives at root, so a verb that never looks at the flag itself
// still refuses an unrecognized format — and that it is a caller mistake
// (invalid.request, exit 1), not a usage error or a verb failure.
func TestOutputFlagRejectsUnknownFormat(t *testing.T) {
	code, _, errOut := runSession(t, "version", "--output", "yaml")
	if code != exitcode.UserFail {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.UserFail, errOut)
	}
	if !strings.Contains(errOut, `--output must be "text" or "json", not "yaml".`) {
		t.Errorf("stderr = %q, want the --output validation message", errOut)
	}
}
