package scrub

import (
	"fmt"
	"sort"
	"strings"
)

// varName extracts the variable name from one os.Environ()-shaped
// "KEY=VALUE" entry. An entry with no "=" is returned whole; os.Environ
// never produces one, but a caller-built slice might, and treating the
// whole malformed entry as its own name still lets IsMarker catch it if
// it happens to equal or start with a marker.
func varName(entry string) string {
	if i := strings.IndexByte(entry, '='); i >= 0 {
		return entry[:i]
	}
	return entry
}

// Environ returns a copy of environ (os.Environ() shape: "KEY=VALUE"
// entries) with every marker variable removed. The order of surviving
// entries is preserved. It never mutates environ.
func Environ(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, e := range environ {
		if IsMarker(varName(e)) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// SurvivingMarkers returns the sorted, de-duplicated marker names still
// present in environ, or nil when none remain.
func SurvivingMarkers(environ []string) []string {
	seen := make(map[string]struct{})
	for _, e := range environ {
		n := varName(e)
		if IsMarker(n) {
			seen[n] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Verify is the fail-closed gate. It returns a non-nil error naming every
// marker still present in environ, or nil when none remain.
//
// A launcher calls this on the exact environment it is about to hand to
// a spawned process (or, when it cannot observe that directly — a Herdr
// pane's inherited environment — the best evidence it has). Per
// conformance §8's hard Stage-1 gate, a non-nil result means the launch
// is refused, not logged and continued.
func Verify(environ []string) error {
	survivors := SurvivingMarkers(environ)
	if len(survivors) == 0 {
		return nil
	}
	return fmt.Errorf("scrub: environment still carries marker(s): %s", strings.Join(survivors, ", "))
}

// Guard scrubs environ and immediately re-verifies its own output before
// returning it. It is shape (a)'s fail-closed entry point: the one
// function a caller should use to build the environment for a direct
// child spawn, or for launching a Duo-owned Herdr server (whose panes
// then inherit an already-clean environment; see docs/scrub/decisions.md).
//
// A caller should never hand exec.Cmd.Env a plain call to Environ; Guard
// is what turns "the scrub logic has a bug" into a refused launch
// instead of a silent leak — the fail-closed property this package
// exists to provide.
func Guard(environ []string) ([]string, error) {
	scrubbed := environFunc(environ)
	if err := Verify(scrubbed); err != nil {
		return nil, err
	}
	return scrubbed, nil
}

// environFunc is the scrub step Guard calls before re-verifying its own
// output. It defaults to Environ and is a package-level variable — not a
// parameter — purely so a test can swap in a deliberately incomplete
// scrubber and prove the re-verification is load-bearing rather than a
// no-op: Environ and Verify share IsMarker as their only source of
// truth, so they can never actually disagree, and the only way to
// exercise Guard's fail-closed path is to break that agreement on
// purpose (see TestGuardRefusesWhenItsOwnScrubStepIsIncomplete).
var environFunc = Environ
