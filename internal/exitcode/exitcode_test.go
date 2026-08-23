package exitcode_test

import (
	"testing"

	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/exitcode"
)

// TestFromError_ReachesEachStructuredCode covers three of the chassis's four
// exit codes (1, 3, 4) from a synthetic error of the matching kind; code 2
// (Cobra's own argument-parsing path) never reaches FromError — it depends
// on Cobra's own parse failure, not on any *duoerr.Error.
func TestFromError_ReachesEachStructuredCode(t *testing.T) {
	cases := []struct {
		name string
		code string
		want int
	}{
		{name: "plain user-facing failure", code: "validation.missing-arg", want: exitcode.UserFail},
		{name: "refusal", code: "refusal.selftest", want: exitcode.Refusal},
		{name: "internal/store error", code: "internal.corrupt-row", want: exitcode.Internal},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := duoerr.New(tc.code, "synthetic error for exit-code mapping test")
			if got := exitcode.FromError(err); got != tc.want {
				t.Fatalf("FromError(code=%q) = %d, want %d", tc.code, got, tc.want)
			}
		})
	}
}
