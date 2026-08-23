// Package duoerr is the structured error type every verb raises for a
// user-facing failure, and the renderer that turns one into both the
// human-mode line and the --json envelope from the exact same value — no
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
}

// New constructs a verb-raised structured error.
func New(code, message string) *Error {
	return &Error{Code: code, Message: message}
}

func (e *Error) Error() string {
	return e.Message
}

// Render writes err to w in the chassis's fixed shape: one
// "duo: <verb>: <message>" line in human mode, or the {"error": {"code",
// "message"}} envelope under --json. verb is empty when the failure occurred
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
		} `json:"error"`
	}{}
	envelope.Error.Code = err.Code
	envelope.Error.Message = err.Message

	// Two plain strings cannot fail to encode; no fallback path needed.
	b, _ := json.Marshal(envelope)
	_, _ = fmt.Fprintln(w, string(b))
}
