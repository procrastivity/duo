package scrub

import (
	"reflect"
	"sort"
	"testing"
)

func TestEnvironRemovesAllMarkersIncludingWildcard(t *testing.T) {
	in := []string{
		"PATH=/usr/bin",
		"CLAUDECODE=1",
		"AI_AGENT=1",
		"CLAUDE_CODE_CHILD_SESSION=abc123",
		"CLAUDE_CODE_ENTRYPOINT=duo", // wildcard
		"CLAUDE_CONFIG_DIR=/x",       // wildcard
		"HOME=/home/dev",
		"DUO_SESSION=s1",
	}
	got := Environ(in)
	want := []string{
		"PATH=/usr/bin",
		"HOME=/home/dev",
		"DUO_SESSION=s1",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Environ(%v) = %v, want %v", in, got, want)
	}
}

func TestEnvironPreservesOrderAndDoesNotMutateInput(t *testing.T) {
	in := []string{"FOO=1", "CLAUDECODE=1", "BAR=2"}
	inCopy := append([]string(nil), in...)
	got := Environ(in)
	if !reflect.DeepEqual(in, inCopy) {
		t.Fatalf("Environ mutated its input: got %v, want unchanged %v", in, inCopy)
	}
	want := []string{"FOO=1", "BAR=2"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Environ(%v) = %v, want %v", in, got, want)
	}
}

func TestEnvironOnEmptyAndAllMarkers(t *testing.T) {
	if got := Environ(nil); len(got) != 0 {
		t.Errorf("Environ(nil) = %v, want empty", got)
	}
	all := []string{"CLAUDECODE=1", "AI_AGENT=1", "CLAUDE_CODE_CHILD_SESSION=1", "CLAUDE_FOO=1"}
	if got := Environ(all); len(got) != 0 {
		t.Errorf("Environ(%v) = %v, want empty", all, got)
	}
}

func TestSurvivingMarkersSortedAndDeduplicated(t *testing.T) {
	in := []string{
		"CLAUDECODE=1",
		"CLAUDE_FOO=1",
		"CLAUDECODE=2", // duplicate name, different value
		"AI_AGENT=1",
		"PATH=/bin",
	}
	got := SurvivingMarkers(in)
	want := []string{"AI_AGENT", "CLAUDECODE", "CLAUDE_FOO"}
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SurvivingMarkers(%v) = %v, want %v", in, got, want)
	}
}

func TestSurvivingMarkersNilWhenClean(t *testing.T) {
	if got := SurvivingMarkers([]string{"PATH=/bin", "HOME=/x"}); got != nil {
		t.Errorf("SurvivingMarkers on a clean environ = %v, want nil", got)
	}
}

// TestVerifyFailsClosedWhenAMarkerSurvives is the fail-closed refusal
// test the launcher's hard Stage-1 gate depends on: Verify must return a
// non-nil error the moment any marker is present, never a warning-shaped
// nil-with-a-log-line. This exercises Verify directly against an environ
// scrubbing did not touch, which is exactly the shape a caller sees if
// its own environment construction has a bug — the case the fail-closed
// contract exists for.
func TestVerifyFailsClosedWhenAMarkerSurvives(t *testing.T) {
	cases := [][]string{
		{"CLAUDECODE=1"},
		{"AI_AGENT=1"},
		{"CLAUDE_CODE_CHILD_SESSION=x"},
		{"CLAUDE_ANYTHING=x"}, // wildcard survivor
		{"PATH=/bin", "CLAUDECODE=1"},
	}
	for _, environ := range cases {
		err := Verify(environ)
		if err == nil {
			t.Errorf("Verify(%v) = nil, want a refusal error", environ)
			continue
		}
		if err.Error() == "" {
			t.Errorf("Verify(%v) returned an empty error message", environ)
		}
	}
}

func TestVerifyCleanEnvironPasses(t *testing.T) {
	if err := Verify([]string{"PATH=/bin", "HOME=/x", "DUO_SESSION=s1"}); err != nil {
		t.Errorf("Verify on a clean environ = %v, want nil", err)
	}
	if err := Verify(nil); err != nil {
		t.Errorf("Verify(nil) = %v, want nil", err)
	}
}

func TestGuardScrubsAndPasses(t *testing.T) {
	in := []string{"PATH=/bin", "CLAUDECODE=1", "CLAUDE_FOO=1", "AI_AGENT=1"}
	got, err := Guard(in)
	if err != nil {
		t.Fatalf("Guard(%v) returned error %v, want a scrubbed environ", in, err)
	}
	if err := Verify(got); err != nil {
		t.Errorf("Guard's own output failed its own Verify: %v (output %v)", err, got)
	}
	want := []string{"PATH=/bin"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Guard(%v) = %v, want %v", in, got, want)
	}
}

// TestGuardRefusesWhenItsOwnScrubStepIsIncomplete proves Guard's
// fail-closed re-check is load-bearing, not a no-op. Environ is
// exhaustive by construction — it and Verify share IsMarker as their
// only source of truth, so they can never actually disagree — which
// means the only way to exercise the path this defense exists for is to
// swap in a deliberately broken scrub step and confirm Guard refuses
// rather than handing back whatever the broken step produced.
func TestGuardRefusesWhenItsOwnScrubStepIsIncomplete(t *testing.T) {
	orig := environFunc
	t.Cleanup(func() { environFunc = orig })
	environFunc = func(environ []string) []string { return environ } // a no-op "scrubber"

	in := []string{"PATH=/bin", "CLAUDECODE=1"}
	got, err := Guard(in)
	if err == nil {
		t.Fatalf("Guard(%v) with a broken scrub step returned (%v, nil); "+
			"the fail-closed re-verification did not fire", in, got)
	}
	if got != nil {
		t.Errorf("Guard returned a non-nil environ (%v) alongside its refusal error", got)
	}
}
