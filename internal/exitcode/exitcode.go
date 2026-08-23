// Package exitcode centralizes the chassis's small, closed exit-code table.
// A fifth code gets added only when a real case doesn't fit one of these
// four — never speculatively.
package exitcode

import (
	"strings"

	"github.com/procrastivity/duo/internal/duoerr"
)

const (
	// Success is the verb's happy path.
	Success = 0
	// UserFail is validation, not-found, any error the verb itself raises
	// that isn't one of the more specific categories below.
	UserFail = 1
	// Usage is a bad-flags/args failure — Cobra's own argument-parsing path.
	Usage = 2
	// Refusal is a guard tripping.
	Refusal = 3
	// Internal is an unexpected internal/store failure — I/O, corruption.
	Internal = 4
)

// FromError reads err's own Code field to pick the exit code: a
// "refusal."-prefixed code is a refusal (3), an "internal."-prefixed code is
// an internal/store error (4), anything else is a plain user-facing failure
// (1). Cobra usage errors (2) never reach this function — main's two return
// paths handle that distinction before FromError is called.
func FromError(err *duoerr.Error) int {
	switch {
	case strings.HasPrefix(err.Code, "refusal."):
		return Refusal
	case strings.HasPrefix(err.Code, "internal."):
		return Internal
	default:
		return UserFail
	}
}
