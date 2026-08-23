package pi

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/procrastivity/duo/internal/runtime"
)

// PaneSessionMode is the one ctx.mode value that identifies the session a
// terminal pane is showing. The other three values pi reports — "rpc",
// "json", "print" — are automation surfaces.
const PaneSessionMode = "tui"

// ReportedClaim is the correlation record the generated extension writes to
// its socket: one JSON object per connection, from live ctx accessors inside
// the agent process. Field names match the asset's JSON.stringify literal, so
// a change to either side must change both.
type ReportedClaim struct {
	Protocol string `json:"protocol"`
	// Token is the per-runtime-instance reporter credential the extension
	// read from the environment at module load and then scrubbed.
	Token string `json:"token"`
	// SessionID is ctx.sessionManager.getSessionId(): pi's own session
	// UUIDv7, which is also the transcript file name's second component and
	// the transcript header line's "id".
	SessionID string `json:"sessionId"`
	// SessionFile is ctx.sessionManager.getSessionFile(): the absolute path
	// of the JSONL transcript pi is writing.
	SessionFile string `json:"sessionFile"`
	Cwd         string `json:"cwd"`
	// Mode is ctx.mode. HasUI is ctx.hasUI, carried for diagnostics only —
	// it is true in rpc mode as well, so it is never the gate.
	Mode  string `json:"mode"`
	HasUI bool   `json:"hasUI"`
	Idle  bool   `json:"idle"`
	// StartReason is the session_start reason (startup|reload|new|resume|
	// fork) that led to this record being served.
	StartReason string `json:"startReason"`
	// LastSettledAt is stamped from agent_settled, never agent_end: after
	// agent_end pi may still auto-retry, auto-compact, or run a queued
	// follow-up.
	LastSettledAt string `json:"lastSettledAt"`
	PID           int    `json:"pid"`
}

// DecodeReportedClaim parses one line of the reporter socket's output.
func DecodeReportedClaim(line []byte) (ReportedClaim, error) {
	var c ReportedClaim
	dec := json.NewDecoder(bytes.NewReader(line))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return ReportedClaim{}, fmt.Errorf("pi: decode reporter claim: %w", err)
	}
	return c, nil
}

// Validate rejects a record this adapter must not turn into a RuntimeClaim.
//
// The mode check is the load-bearing one, and it is deliberately duplicated:
// the generated extension gates its own serving on ctx.mode === "tui", and
// Duo re-checks the reported mode here rather than trusting a file on disk
// that a user can edit. At 0.83.0 ctx.hasUI is true in rpc mode too, so a
// hasUI-based gate on either side would let an rpc-driven pi present itself
// as the pane's session. A future rpc-driven launch variant must widen this
// check deliberately.
func (c ReportedClaim) Validate() error {
	if c.Protocol != ReporterProtocol {
		return fmt.Errorf("pi: reporter claim protocol %q, want %q", c.Protocol, ReporterProtocol)
	}
	if c.SessionID == "" {
		return fmt.Errorf("pi: reporter claim carries no session id")
	}
	if c.Mode != PaneSessionMode {
		return fmt.Errorf(
			"pi: reporter claim from mode %q, want %q: only a tui session is the pane's session",
			c.Mode, PaneSessionMode)
	}
	return nil
}

// Claim converts a validated record into the §5.3 claim shape. The reported
// session file becomes the claim's transcript path and the credential becomes
// its reporter credential; Correlate is what decides whether that binds.
func (c ReportedClaim) Claim(integrationInstanceID string) runtime.RuntimeClaim {
	return runtime.RuntimeClaim{
		IntegrationInstanceID:  integrationInstanceID,
		ExternalAgentSessionID: c.SessionID,
		TranscriptPath:         c.SessionFile,
		WorkingDirectory:       c.Cwd,
		ReporterCredential:     c.Token,
	}
}
