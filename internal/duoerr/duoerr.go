// Package duoerr is the structured error type every verb raises for a
// user-facing failure, and the renderer that turns one into both the
// human-mode line and the --output json envelope from the exact same value — no
// separate code paths that could diverge.
package duoerr

import (
	"encoding/json"
	"fmt"
	"io"
)

// Error carries a stable machine token (e.g. "refusal.some-guard") distinct
// from the process exit code, and the human-readable message.
// exitcode.FromError reads the Code prefix to pick the exit code; Render
// drops Message into the fixed envelope unchanged. This package only owns
// the shape, never the vocabulary.
type Error struct {
	Code    string
	Message string
	// Details is the failure's *safe* detail payload, in the shape the
	// duo.external/v1 error object fixes for that code, or nil when the
	// code carries none. It is rendered only under --output json, where the
	// envelope has somewhere to put it; human mode stays one line.
	//
	// It exists because two launch failures owe the caller more than a
	// sentence: `launch.constraints_exhausted` and
	// `launch.no_eligible_candidate` carry the per-reason tallies, the
	// deduced host, the evidence bundle, and the pointer set — the ways
	// out — and a projection that dropped them would make the operator
	// re-run the command in another mode to learn what to do
	// (duo-vnext-projection-contracts.md §2.1, 2026-08-24 handoff 22).
	// Nothing here needs a permission beyond the operation's own; local
	// paths and raw adapter text stay behind diagnostics.read.
	Details any
}

// New constructs a verb-raised structured error.
func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

// WithDetails attaches a safe detail payload to err and returns it, so a
// projection can build the error and its details in one expression.
func (e *Error) WithDetails(details any) *Error {
	e.Details = details
	return e
}

func (e *Error) Error() string {
	return e.Message
}

// Render writes err to w in the chassis's fixed shape: one
// "duo: <verb>: <message>" line in human mode, or the {"error": {"code",
// "message"}} envelope under --output json. verb is empty when the failure occurred
// before a subcommand was identified.
func Render(w io.Writer, verb string, err *Error, jsonMode bool) {
	if jsonMode {
		renderJSON(w, err)
		return
	}
	if verb == "" {
		_, _ = fmt.Fprintf(w, "duo: %s\n", err.Message)
		return
	}
	_, _ = fmt.Fprintf(w, "duo: %s: %s\n", verb, err.Message)
}

func renderJSON(w io.Writer, err *Error) {
	envelope := struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details any    `json:"details,omitempty"`
		} `json:"error"`
	}{}
	envelope.Error.Code = err.Code
	envelope.Error.Message = err.Message
	envelope.Error.Details = err.Details

	b, marshalErr := json.Marshal(envelope)
	if marshalErr != nil {
		// A caller handed us details that will not encode. The code and
		// the message always will, so the envelope still goes out — a
		// failure must never be swallowed by its own detail payload.
		envelope.Error.Details = nil
		b, _ = json.Marshal(envelope)
	}
	_, _ = fmt.Fprintln(w, string(b))
}
