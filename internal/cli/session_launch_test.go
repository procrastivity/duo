package cli

import (
	"fmt"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/scrub"
)

// The spawn-environment gate is a guard tripping, not an internal failure:
// it must reach the operator as a refusal.* code and exit 3, with the
// adapter's actionable message intact. The launcher wraps a host-side
// failure with fmt.Errorf, so the projection has to find the refusal
// through the wrapping rather than at the top level.
func TestLaunchDuoErrProjectsTheScrubRefusalAsAnExitThreeRefusal(t *testing.T) {
	refusal := &scrub.RefusalError{
		Subject:   "the environment the new session-host pane inherited from its server (w1:p2)",
		Survivors: []string{scrub.Markers[0]},
		Remedy:    "Restart the session host from a clean shell, then launch again.",
	}
	wrapped := fmt.Errorf("launch: leaf %s: starting launch: %w", "primary", refusal)

	de := launchDuoErr(wrapped)
	if !strings.HasPrefix(de.Code, "refusal.") {
		t.Fatalf("code = %q, want a refusal.* code (a guard tripped, not an internal failure)", de.Code)
	}
	if got := exitcode.FromError(de); got != exitcode.Refusal {
		t.Fatalf("exit code = %d, want %d", got, exitcode.Refusal)
	}
	if !strings.Contains(de.Message, scrub.Markers[0]) {
		t.Errorf("message does not name the surviving marker: %s", de.Message)
	}
	if !strings.Contains(de.Message, "Restart the session host") {
		t.Errorf("message drops the remedy the operator has to act on: %s", de.Message)
	}
}

// An unreadable environment is the same refusal, not an internal error: the
// operator can act on it (grant access, or run Duo where it can see the
// pane), so exit 3 is the honest code.
func TestLaunchDuoErrProjectsAnUnreadableEnvironmentAsARefusal(t *testing.T) {
	err := scrub.Unreadable("the pane environment (w1:p2)", "Run Duo as the pane's owner.", fmt.Errorf("permission denied"))
	de := launchDuoErr(fmt.Errorf("launch: leaf primary: starting launch: %w", err))
	if got := exitcode.FromError(de); got != exitcode.Refusal {
		t.Fatalf("exit code = %d, want %d (code %q)", got, exitcode.Refusal, de.Code)
	}
}
