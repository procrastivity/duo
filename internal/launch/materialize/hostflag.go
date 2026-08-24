package materialize

import (
	"fmt"
	"strings"

	"github.com/procrastivity/duo/internal/duoerr"
)

// HostFlag is a parsed `--host` value.
//
// Two shapes are accepted, and the grammar is the audited rebind verb's
// target grammar on purpose (domain.HostBinding.Locator): what `duo
// workspace host` prints back is what `--host` takes, so an operator never
// has to translate between two spellings of one instance.
//
//   - `--host <kind>:<instance>` names the instance outright. This is the
//     complete form, and the only one that needs nothing else to resolve.
//   - `--host <kind>` names the kind and leaves the instance to discovery.
//     It is a convenience for the ordinary single-instance case, and it
//     fails loudly rather than picking when discovery is ambiguous.
type HostFlag struct {
	// Kind is the session-host kind. Always set on a present flag.
	Kind string
	// Instance is the instance locator, "" for the kind-only form.
	Instance string
}

// Present reports whether a flag value was supplied at all.
func (f HostFlag) Present() bool { return f.Kind != "" }

// Complete reports whether the flag names one instance outright.
func (f HostFlag) Complete() bool { return f.Kind != "" && f.Instance != "" }

// String renders the flag back in the form it was typed.
func (f HostFlag) String() string {
	if !f.Present() {
		return ""
	}
	if f.Instance == "" {
		return f.Kind
	}
	return f.Kind + ":" + f.Instance
}

// ParseHostFlag parses a raw `--host` value. An empty or whitespace-only
// value is an absent flag, not an error.
//
// The value is cut at the *first* colon, which is what makes the grammar a
// true inverse of Locator(): an instance locator is a socket path and may
// itself contain a colon, while a host kind never does.
func ParseHostFlag(raw string) (HostFlag, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return HostFlag{}, nil
	}
	kind, instance, hasColon := strings.Cut(value, ":")
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return HostFlag{}, duoerr.New("invalid.request",
			fmt.Sprintf("Host %q names no session-host kind; use --host <kind> or --host <kind>:<instance>.", raw))
	}
	if strings.ContainsAny(kind, " \t") {
		return HostFlag{}, duoerr.New("invalid.request",
			fmt.Sprintf("Host kind %q contains whitespace; use --host <kind> or --host <kind>:<instance>.", kind))
	}
	if hasColon && strings.TrimSpace(instance) == "" {
		return HostFlag{}, duoerr.New("invalid.request",
			fmt.Sprintf("Host %q names a kind with an empty instance; drop the colon to let discovery resolve it, or name the instance.", raw))
	}
	return HostFlag{Kind: kind, Instance: strings.TrimSpace(instance)}, nil
}
