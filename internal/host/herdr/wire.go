package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"time"
)

// Herdr error codes this adapter reasons about by name. The full set is
// larger; these are the ones a control-flow decision depends on, so
// spelling them out keeps that decision greppable.
const (
	// CodePaneNotFound answers a pane.get or pane.process_info for a pane
	// that is gone. It is an absence, not a failure.
	CodePaneNotFound = "pane_not_found"
	// CodeAgentPaneBusy means the target pane's shell is not at its
	// interactive prompt yet. On agent.start it is a pre-delivery
	// refusal (proven no-effect, retried). On agent.prompt a write may
	// already have happened, so DeliverPrompt maps it to unknown_effect.
	CodeAgentPaneBusy = "agent_pane_busy"
	// CodeAgentNotFound answers agent lookups for a pane whose agent
	// registration dropped — including the spontaneous durable
	// deregistration on foreground loss. Never treated as pane absence.
	CodeAgentNotFound = "agent_not_found"
	// CodeInvalidRequest is the envelope-level rejection (e.g. a missing
	// id field, which 0.8.2 requires).
	CodeInvalidRequest = "invalid_request"
	// CodeTimeout is the typed timeout result of a blocking wait.
	CodeTimeout = "timeout"
	// CodeAgentNotReady is a pre-delivery agent.prompt refusal: the named
	// agent is not interactive yet, or is no longer the pane foreground.
	// Proven no-effect at 0.8.2 (notes/19 §3).
	CodeAgentNotReady = "agent_not_ready"
	// CodeAgentBlocked is a pre-delivery refusal: a prompt to an already
	// blocked agent is rejected before any input is sent (notes/19 §3).
	CodeAgentBlocked = "agent_blocked"
	// CodeEmptyAgentPrompt is a pre-delivery refusal for empty text.
	CodeEmptyAgentPrompt = "empty_agent_prompt"
	// CodeAgentNotRunning and CodeAgentKindMismatch complete the 0.8.2
	// pre-delivery refusal family notes/19 lists as safe to retry.
	CodeAgentNotRunning   = "agent_not_running"
	CodeAgentKindMismatch = "agent_kind_mismatch"
	// CodeAgentPromptStalled means Herdr accepted the submission and then
	// saw no lifecycle change. unknown_effect; never auto-retried
	// (notes/19 §3, decision-03 §7.1).
	CodeAgentPromptStalled = "agent_prompt_stalled"
	// CodeAgentPrompted is agent.prompt's success discriminator. It is
	// not effect certainty and is not an acknowledgment (notes/19 §2–§3).
	CodeAgentPrompted = "agent_prompted"
)

// maxResponseBytes caps one NDJSON line. A session.snapshot on a large
// session is the biggest thing this adapter reads; 32 MiB is far beyond
// it, and the cap exists so a wedged or hostile socket cannot exhaust
// memory.
const maxResponseBytes = 32 << 20

// ProtocolError is a typed refusal from the Herdr server: the server
// answered, and this is what it said. Transport failures are plain errors
// instead, so a caller can tell "Herdr said no" from "Herdr is not there".
type ProtocolError struct {
	Method  string
	Code    string
	Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("herdr %s: %s: %s", e.Method, e.Code, e.Message)
}

// ErrorCode returns the Herdr error code carried by err, or "" when err is
// not a ProtocolError.
func ErrorCode(err error) string {
	var pe *ProtocolError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

// client speaks Herdr's NDJSON socket API.
//
// One connection per request is not a style choice: the server closes the
// connection after every non-streaming response, and a second request on
// the same connection gets a broken pipe (verified at 0.7.5 and again at
// 0.8.2). Only events.subscribe and blocking waits hold a connection open,
// which is why subscribe returns its own connection rather than borrowing
// one.
type client struct {
	socketPath string
	timeout    time.Duration
	seq        atomic.Uint64
}

func newClient(socketPath string, timeout time.Duration) *client {
	return &client{socketPath: socketPath, timeout: timeout}
}

func (c *client) nextID() string {
	return "duo-" + strconv.FormatUint(c.seq.Add(1), 10)
}

func (c *client) dial(ctx context.Context) (net.Conn, error) {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "unix", c.socketPath)
	if err != nil {
		return nil, fmt.Errorf("herdr: dial %s: %w", c.socketPath, err)
	}
	return conn, nil
}

type requestEnvelope struct {
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params"`
}

type wireError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type responseEnvelope struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *wireError      `json:"error"`
}

// call performs one request/response round trip on its own connection and
// decodes result into out (which may be nil to discard it).
func (c *client) call(ctx context.Context, method string, params any, out any) error {
	_, err := c.callAdmit(ctx, method, params, out)
	return err
}

// callAdmit is call plus whether a request line may have reached the
// server. Dial and pre-write failures report written=false (proven
// no-effect). A failed write after dial reports written=true: the server
// may have read a partial or complete request.
func (c *client) callAdmit(ctx context.Context, method string, params any, out any) (written bool, err error) {
	deadline := time.Now().Add(c.timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	dialCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	conn, err := c.dial(dialCtx)
	if err != nil {
		return false, err
	}
	defer func() { _ = conn.Close() }()
	if err := conn.SetDeadline(deadline); err != nil {
		return false, fmt.Errorf("herdr %s: set deadline: %w", method, err)
	}

	if err := writeRequest(conn, requestEnvelope{ID: c.nextID(), Method: method, Params: orEmptyParams(params)}); err != nil {
		return true, fmt.Errorf("herdr %s: %w", method, err)
	}
	resp, err := readResponse(conn)
	if err != nil {
		return true, fmt.Errorf("herdr %s: %w", method, err)
	}
	if resp.Error != nil {
		return true, &ProtocolError{Method: method, Code: resp.Error.Code, Message: resp.Error.Message}
	}
	if out == nil {
		return true, nil
	}
	if err := json.Unmarshal(resp.Result, out); err != nil {
		return true, fmt.Errorf("herdr %s: decode result: %w", method, err)
	}
	return true, nil
}

// orEmptyParams keeps the params field present and object-shaped. The
// 0.8.2 request schema requires both id and params on every request.
func orEmptyParams(params any) any {
	if params == nil {
		return struct{}{}
	}
	return params
}

func writeRequest(w io.Writer, req requestEnvelope) error {
	line, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode request: %w", err)
	}
	if _, err := w.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write request: %w", err)
	}
	return nil
}

func readResponse(r io.Reader) (responseEnvelope, error) {
	line, err := readLine(bufio.NewReader(io.LimitReader(r, maxResponseBytes)))
	if err != nil {
		return responseEnvelope{}, err
	}
	var resp responseEnvelope
	if err := json.Unmarshal(line, &resp); err != nil {
		return responseEnvelope{}, fmt.Errorf("decode response: %w", err)
	}
	return resp, nil
}

func readLine(r *bufio.Reader) ([]byte, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		if len(line) == 0 {
			return nil, fmt.Errorf("read response: %w", err)
		}
		// A final line without its newline is still a complete response.
		if !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("read response: %w", err)
		}
	}
	return line, nil
}

// subscription is one entry of an events.subscribe request. Global
// subscriptions carry only a type; pane.agent_status_changed and friends
// require a pane_id, which is why PaneID is present but omitted when
// empty.
type subscription struct {
	Type   string `json:"type"`
	PaneID string `json:"pane_id,omitempty"`
}

// eventEnvelope is one streamed event. The event name is keyed as "event"
// (not "type"); the payload's own discriminator lives inside data.
type eventEnvelope struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

// eventStream is an open events.subscribe connection.
//
// This adapter reads events for one purpose only: to learn that *something*
// changed, so it can re-diff a session.snapshot. It never decodes an event
// payload into observed state, because pane.created backfill is partial at
// 0.8.2 (restored panes never replay) and backfill is indistinguishable
// from a fresh creation.
type eventStream struct {
	conn   net.Conn
	reader *bufio.Reader
}

func (c *client) subscribe(ctx context.Context, subs []subscription) (*eventStream, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	if err := conn.SetWriteDeadline(time.Now().Add(c.timeout)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("herdr events.subscribe: set deadline: %w", err)
	}
	req := requestEnvelope{
		ID:     c.nextID(),
		Method: "events.subscribe",
		Params: map[string]any{"subscriptions": subs},
	}
	if err := writeRequest(conn, req); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("herdr events.subscribe: %w", err)
	}

	reader := bufio.NewReader(conn)
	if err := conn.SetReadDeadline(time.Now().Add(c.timeout)); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("herdr events.subscribe: set deadline: %w", err)
	}
	ackLine, err := readLine(reader)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("herdr events.subscribe: %w", err)
	}
	var ack responseEnvelope
	if err := json.Unmarshal(ackLine, &ack); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("herdr events.subscribe: decode ack: %w", err)
	}
	if ack.Error != nil {
		_ = conn.Close()
		return nil, &ProtocolError{Method: "events.subscribe", Code: ack.Error.Code, Message: ack.Error.Message}
	}
	// The stream has no server-side keepalive and no cursor, so the read
	// side must block indefinitely rather than time out into a false gap.
	if err := conn.SetReadDeadline(time.Time{}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("herdr events.subscribe: clear deadline: %w", err)
	}
	return &eventStream{conn: conn, reader: reader}, nil
}

// next blocks until the next event arrives or the stream ends. Closing the
// stream from another goroutine unblocks it with an error.
func (s *eventStream) next() (eventEnvelope, error) {
	line, err := readLine(s.reader)
	if err != nil {
		return eventEnvelope{}, err
	}
	var ev eventEnvelope
	if err := json.Unmarshal(line, &ev); err != nil {
		return eventEnvelope{}, fmt.Errorf("herdr event: decode: %w", err)
	}
	return ev, nil
}

func (s *eventStream) Close() error {
	return s.conn.Close()
}
