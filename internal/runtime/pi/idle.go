package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// Ready implements runtime.RuntimeReadyProvider by reading inject
// connect-line idle. It does not send input.
func (r *Runtime) Ready(ctx context.Context, binding runtime.RuntimeBinding) (bool, error) {
	return ReadInjectIdle(ctx, binding.ExternalAgentSessionID)
}

// ReadInjectIdle dials the inject socket for sessionID, reads one
// connect-line greeting, and reports whether idle is JSON-boolean true.
// It does not send a prompt frame. A missing listener or unreadable greeting
// is not ready (false, nil), not a caller failure.
func ReadInjectIdle(ctx context.Context, sessionID string) (bool, error) {
	path, err := InjectSocketPath(sessionID)
	if err != nil {
		return false, err
	}

	ctx, cancel := context.WithTimeout(ctx, connectLineDrain)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, err
		}
		return false, nil
	}
	defer func() { _ = conn.Close() }()

	_ = conn.SetReadDeadline(time.Now().Add(connectLineDrain))
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil {
		return false, nil
	}

	var payload struct {
		Idle *bool `json:"idle"`
	}
	if err := json.Unmarshal(line, &payload); err != nil {
		return false, nil
	}
	if payload.Idle == nil || !*payload.Idle {
		return false, nil
	}
	return true, nil
}
