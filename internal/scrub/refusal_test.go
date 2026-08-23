package scrub

import (
	"errors"
	"strings"
	"testing"
)

// Gate passes an environment with nothing to refuse, and returns a nil
// error interface rather than a typed nil pointer — a caller's `if err !=
// nil` has to mean what it says.
func TestGatePassesACleanEnvironment(t *testing.T) {
	err := Gate("the test environment", "Do nothing.", []string{"PATH=/usr/bin", "HOME=/home/tester"})
	if err != nil {
		t.Fatalf("Gate on a clean environment = %v, want nil", err)
	}
}

// The refusal names every surviving marker, so an operator learns what to
// remove rather than that "something" was wrong.
func TestGateRefusesAndNamesEverySurvivingMarker(t *testing.T) {
	environ := []string{"PATH=/usr/bin"}
	for _, m := range Markers {
		environ = append(environ, m+"=1")
	}
	for _, p := range WildcardPrefixes {
		environ = append(environ, p+"SOMETHING=x")
	}

	err := Gate("the pane environment", "Restart the server.", environ)
	if err == nil {
		t.Fatal("Gate accepted a marker-carrying environment")
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("Gate returned %T, want *RefusalError", err)
	}
	want := len(Markers) + len(WildcardPrefixes)
	if len(refusal.Survivors) != want {
		t.Fatalf("Survivors = %v, want %d entries", refusal.Survivors, want)
	}
	for _, m := range Markers {
		if !strings.Contains(err.Error(), m) {
			t.Errorf("refusal message does not name %s: %s", m, err)
		}
	}
	if !strings.Contains(err.Error(), "Restart the server.") {
		t.Errorf("refusal message drops the remedy: %s", err)
	}
}

// The gate reports names, never values: §6.8 keeps environment values
// behind diagnostics.read and conformance §8 requires marker reporting
// "without printing secret values".
func TestGateRefusalNeverPrintsAValue(t *testing.T) {
	const secret = "sk-do-not-print-me"
	environ := []string{Markers[0] + "=" + secret}
	err := Gate("the pane environment", "Restart the server.", environ)
	if err == nil {
		t.Fatal("Gate accepted a marker-carrying environment")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("refusal message leaked a variable's value: %s", err)
	}
}

// Fail-closed on an environment that cannot be observed at all: "could not
// check" is not "checked and clean".
func TestUnreadableEnvironmentIsARefusalNotAPass(t *testing.T) {
	cause := errors.New("permission denied")
	err := Unreadable("the pane environment", "Run Duo as the pane's owner.", cause)
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("Unreadable returned %T, want *RefusalError", err)
	}
	if len(refusal.Survivors) != 0 {
		t.Errorf("Survivors = %v, want none: nothing was observed", refusal.Survivors)
	}
	if !errors.Is(err, cause) {
		t.Error("the read failure is not reachable through errors.Is")
	}
	if !strings.Contains(err.Error(), "refused launch, not a warning") {
		t.Errorf("message does not say the launch is refused: %s", err)
	}
}

// A teardown failure after the refusal does not soften the refusal, but it
// does reach the operator: a pane still running is theirs to close.
func TestRefusalCarriesACleanupFailure(t *testing.T) {
	refusal := &RefusalError{
		Subject:   "the pane environment",
		Survivors: []string{Markers[0]},
		Remedy:    "Restart the server.",
		Cleanup:   errors.New("pane p1 could not be closed"),
	}
	if !strings.Contains(refusal.Error(), "pane p1 could not be closed") {
		t.Fatalf("message hides the cleanup failure: %s", refusal)
	}
}

func TestRefusalSubjectKeepsTheIdentifier(t *testing.T) {
	if got := RefusalSubject("the pane environment", "w1:p3"); !strings.Contains(got, "w1:p3") {
		t.Fatalf("RefusalSubject = %q, want it to name the pane", got)
	}
	if got := RefusalSubject("the pane environment", ""); got != "the pane environment" {
		t.Fatalf("RefusalSubject with no id = %q", got)
	}
}
