package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// injectTextFrame is the NDJSON object notes/18 §5 documented on the
// inject socket: {"text": "<prompt>"}, one object per write, terminated
// by a newline. deliverAs and abort are out of this stage.
type injectTextFrame struct {
	Text string `json:"text"`
}

// connectLineDrain is how long DeliverPrompt waits for the extension's
// connect-line greeting before writing the prompt. Tens of milliseconds,
// not a second: a missing greeting is ignored (the extension may not
// have written yet) and must not stall delivery.
const connectLineDrain = 50 * time.Millisecond

// PromptPath implements runtime.RuntimePromptProvider. It offers the
// native inject-socket path when InjectSocketPath can name a conventional
// socket from the bound Pi session id. It does not dial, does not stat,
// and does not read DUO_PI_SOCK (that override is extension-only).
// ComposerSafe is true: input.source is "extension" (notes/18). Quality
// is exact / native.
func (r *Runtime) PromptPath(_ context.Context, binding runtime.RuntimeBinding) (runtime.PromptPathCandidate, error) {
	if _, err := InjectSocketPath(binding.ExternalAgentSessionID); err != nil {
		return runtime.PromptPathCandidate{}, err
	}
	return runtime.PromptPathCandidate{
		Quality:      "exact",
		Realization:  "native",
		ComposerSafe: true,
	}, nil
}

// DeliverPrompt implements runtime.RuntimePromptProvider. It writes one
// {"text": …} NDJSON frame to the bound instance's inject socket and no
// other. Admission (the socket accepts the frame and the peer is still
// there) is delivered at native quality. Connection loss after a write is
// unknown_effect. A dial that never starts a write is no_effect.
//
// The extension writes a correlation line on connect (idle is a byproduct
// for Stage B). This method drains at most one newline-terminated line
// with connectLineDrain and does not parse it — idle is not a delivery
// effect. A missing greeting is not a delivery failure.
func (r *Runtime) DeliverPrompt(ctx context.Context, req runtime.PromptDeliveryRequest) (runtime.PromptDeliveryResult, error) {
	path, err := InjectSocketPath(req.Binding.ExternalAgentSessionID)
	if err != nil {
		return runtime.PromptDeliveryResult{}, err
	}

	frame, err := encodeInjectFrame(req.Text)
	if err != nil {
		return runtime.PromptDeliveryResult{}, fmt.Errorf("pi runtime %s: encoding prompt frame: %w", r.integrationInstanceID, err)
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return runtime.PromptDeliveryResult{}, err
		}
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectNoEffect}, nil
	}
	defer func() { _ = conn.Close() }()

	drainConnectLine(conn)
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	n, writeErr := conn.Write(frame)
	if n == len(frame) && writeErr == nil && !peerGone(conn) {
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectDelivered}, nil
	}
	return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
}

func encodeInjectFrame(text string) ([]byte, error) {
	b, err := json.Marshal(injectTextFrame{Text: text})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// drainConnectLine reads at most one newline-terminated greeting so the
// subsequent peerGone check is not looking at the extension's connect-line.
// Timeout, EOF, and parse-irrelevant bytes are ignored.
func drainConnectLine(conn net.Conn) {
	_ = conn.SetReadDeadline(time.Now().Add(connectLineDrain))
	_, _ = bufio.NewReader(conn).ReadBytes('\n')
	_ = conn.SetReadDeadline(time.Time{})
}

// peerGone reports whether the peer has already closed. Copied from the
// Claude messaging-socket adapter: a live server sends nothing unsolicited
// after the greeting and waits, so a short read timeout means the peer is
// still there. EOF or reset after our write is connection loss.
func peerGone(conn net.Conn) bool {
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Millisecond))
	var buf [1]byte
	_, err := conn.Read(buf[:])
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return false
	}
	return true
}
