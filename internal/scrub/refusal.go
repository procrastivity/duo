package scrub

import (
	"fmt"
	"strings"
)

// RefusalError is the typed refusal a launch path raises when a spawn
// environment fails this package's fail-closed gate.
//
// It is deliberately distinct from the plain error Verify and Guard
// return. Those two answer an internal question — "does this slice still
// carry a marker, and did my own scrub step do its job?" — and Guard's
// failure means a bug in this package, not an operator situation.
// RefusalError is the operator-facing half: it names *which* environment
// failed, what the operator can do about it, and (when the gate could not
// run at all) why. A CLI maps it to a refusal.* code and exit 3;
// conformance §8 makes that a hard Stage-1 gate, never a warning.
//
// Nothing here prints a variable's value. §6.8 keeps environment values
// behind diagnostics.read, and conformance §8 requires marker reporting
// "without printing secret values" — names are the reportable half, so
// Survivors carries names only.
type RefusalError struct {
	// Subject names the environment that failed, in a phrase that
	// completes "refusing to launch: <subject> ...". It is the caller's,
	// because only the caller knows what it inspected.
	Subject string
	// Survivors are the marker names still present, sorted, or nil when
	// the environment could not be read at all.
	Survivors []string
	// Remedy is the operator action, one sentence, caller-supplied for
	// the same reason as Subject: the action is host-specific.
	Remedy string
	// Cause is the underlying read failure for an unreadable environment,
	// nil otherwise.
	Cause error
	// Cleanup records a teardown failure that happened *after* the
	// refusal was decided — the launch is refused either way, but a
	// resource the caller could not remove is something the operator has
	// to know about. Callers set it directly.
	Cleanup error
}

func (e *RefusalError) Error() string {
	var b strings.Builder
	b.WriteString("refusing to launch: ")
	b.WriteString(e.Subject)
	if len(e.Survivors) > 0 {
		b.WriteString(" still carries agent-harness marker(s) ")
		b.WriteString(strings.Join(e.Survivors, ", "))
		b.WriteString(". A runtime started there disables its own transcript silently")
	} else {
		b.WriteString(" could not be read")
		if e.Cause != nil {
			b.WriteString(" (")
			b.WriteString(e.Cause.Error())
			b.WriteString(")")
		}
		b.WriteString(". Duo cannot prove the spawn environment is clean, and an unprovable scrub is a refused launch, not a warning")
	}
	if e.Remedy != "" {
		b.WriteString(". ")
		b.WriteString(e.Remedy)
	} else {
		b.WriteString(".")
	}
	if e.Cleanup != nil {
		b.WriteString(" (Cleanup after the refusal also failed: ")
		b.WriteString(e.Cleanup.Error())
		b.WriteString(".)")
	}
	return b.String()
}

func (e *RefusalError) Unwrap() error { return e.Cause }

// Gate is the launch path's fail-closed entry point for an environment the
// caller can observe but cannot change — the case Guard does not cover.
//
// Guard (shape (a)) applies where Duo builds the environment itself: it
// scrubs, re-verifies, and hands back something clean. Gate applies where
// the environment is already fixed by the time Duo can see it — a Herdr
// pane inheriting its server's environment, which Herdr's env map can set
// into but never unset out of (docs/adapters/decisions.md, Herdr section).
// There, the only two outcomes are "provably clean, proceed" and "refused";
// there is no third outcome where Duo fixes it in place.
//
// It returns nil or a *RefusalError. A caller must treat a non-nil result
// as a refused launch: conformance §8 step 7 fails the launch gate when an
// unclassified parent marker remains, and warning-and-continuing is
// exactly the failure mode this package exists to prevent.
func Gate(subject, remedy string, environ []string) error {
	survivors := SurvivingMarkers(environ)
	if len(survivors) == 0 {
		return nil
	}
	return &RefusalError{Subject: subject, Survivors: survivors, Remedy: remedy}
}

// Unreadable is Gate's fail-closed answer for an environment that could not
// be observed at all: no procfs entry, a permission failure, a process that
// exited between one call and the next, a host that reports no process.
//
// Fail-closed here is the same judgment internal/host/herdr already makes
// about process birth ("when birth cannot be proven the verdict is 'not the
// same process', never 'same'") and the one the Step 20 doc states for this
// package: *could not check* is not *checked and clean*. Choosing fail-open
// would make the gate silently vacuous on exactly the hosts where procfs is
// missing, which is the shape of failure conformance §8 calls out — a
// transcript quietly lost with no refusal anywhere.
func Unreadable(subject, remedy string, cause error) error {
	return &RefusalError{Subject: subject, Remedy: remedy, Cause: cause}
}

// RefusalSubject builds the Subject phrase for one named environment plus
// the identifier that distinguishes it, e.g. "the environment pane w1:p3
// inherited from its session host". It exists so a caller composing the
// phrase by hand cannot accidentally drop the identifier.
func RefusalSubject(what, id string) string {
	if id == "" {
		return what
	}
	return fmt.Sprintf("%s (%s)", what, id)
}
