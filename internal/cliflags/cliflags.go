// Package cliflags carries the two global flags (--output, -v/--verbose)
// from root's PersistentPreRunE down to every verb via context, per the
// chassis discipline: both bind once at root, read via context by every
// verb. A verb never redeclares either flag.
//
// --output is the chassis's one spelling for "which result format" — there
// is no second global --json boolean, and no alias for one (see
// docs/cli/decisions.md, the 2026-08-24 resolution).
package cliflags

import "context"

// OutputFlag is the name of the root persistent result-format flag. Every
// reader — the verbs via FromContext, internal/cli.Execute's error-envelope
// selection — names it through this constant, so the flag is defined and
// spelled in exactly one place.
const OutputFlag = "output"

// The two values OutputFlag accepts.
const (
	OutputText = "text"
	OutputJSON = "json"
)

type contextKey struct{}

// Flags is the set of global flag values threaded through context.
type Flags struct {
	// Output is the validated result format: OutputText or OutputJSON.
	Output  string
	Verbose bool
}

// JSON reports whether the operator asked for the JSON result format.
func (f Flags) JSON() bool { return f.Output == OutputJSON }

// WithFlags returns a context carrying f, for verbs to read via FromContext.
func WithFlags(ctx context.Context, f Flags) context.Context {
	return context.WithValue(ctx, contextKey{}, f)
}

// FromContext returns the Flags stored by WithFlags, or the zero value if
// none were stored.
func FromContext(ctx context.Context) Flags {
	f, _ := ctx.Value(contextKey{}).(Flags)
	return f
}
