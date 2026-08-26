package claude

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// peerUserFrame is the NDJSON object notes/10 §3 documented on the
// messaging socket:
//
//	{"type":"user","message":{"role":"user","content":"hello"}}
//
// One object per write, terminated by a newline. Claude admits it as a
// peer message (isMeta, security preamble); that wrapping is accepted
// behavior, not stripped here. Duo does not add hop-chain fields and
// does not write any other session's socket.
type peerUserFrame struct {
	Type    string          `json:"type"`
	Message peerUserMessage `json:"message"`
}

type peerUserMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

const (
	peerFrameType = "user"
	peerFrameRole = "user"
)

// PromptPath implements runtime.RuntimePromptProvider. It offers the
// native peer-queue path when this bound session has a locatable
// messaging socket in the registry (messagingSocketPath, else
// $XDG_RUNTIME_DIR/cc-socks/<pid>.sock). It does not dial — no live
// probe (notes/10, 13, 16). ComposerSafe is true: the socket does not
// clobber the TUI composer (notes/13). Quality is exact / native.
// Quiet-gate remains step 13.
func (r *Runtime) PromptPath(_ context.Context, binding runtime.RuntimeBinding) (runtime.PromptPathCandidate, error) {
	if _, err := r.locateMessagingSocket(binding.ExternalAgentSessionID); err != nil {
		return runtime.PromptPathCandidate{}, err
	}
	return runtime.PromptPathCandidate{
		Quality:      "exact",
		Realization:  "native",
		ComposerSafe: true,
	}, nil
}

// DeliverPrompt implements runtime.RuntimePromptProvider. It writes one
// peer-user NDJSON frame to the bound instance's messaging socket and
// no other. Admission (the socket accepts the frame and the peer is
// still there) is delivered at native quality. Connection loss after a
// write is unknown_effect. A dial that never starts a write is
// no_effect. Fire-and-forget: notes/16 §5 recorded no receipt.
//
// CLAUDE_CODE_MESSAGING_TOKEN is not this instance's ReporterCredential.
// Notes/10 admitted without a token; this method does not send the
// reporter secret as a socket secret, and does not put a token on the
// frame.
func (r *Runtime) DeliverPrompt(ctx context.Context, req runtime.PromptDeliveryRequest) (runtime.PromptDeliveryResult, error) {
	path, err := r.locateMessagingSocket(req.Binding.ExternalAgentSessionID)
	if err != nil {
		return runtime.PromptDeliveryResult{}, err
	}

	frame, err := encodePeerFrame(req.Text)
	if err != nil {
		return runtime.PromptDeliveryResult{}, fmt.Errorf("claude runtime %s: encoding prompt frame: %w", r.integrationInstanceID, err)
	}

	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", path)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return runtime.PromptDeliveryResult{}, err
		}
		// Never wrote: the socket did not accept a connection.
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectNoEffect}, nil
	}
	defer func() { _ = conn.Close() }()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	n, writeErr := conn.Write(frame)
	if n == len(frame) && writeErr == nil && !peerGone(conn) {
		return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectDelivered}, nil
	}
	// Connected, then lost the peer or failed the write: cannot prove
	// the frame was not admitted.
	return runtime.PromptDeliveryResult{Effect: runtime.PromptEffectUnknownEffect}, nil
}

func encodePeerFrame(content string) ([]byte, error) {
	b, err := json.Marshal(peerUserFrame{
		Type: peerFrameType,
		Message: peerUserMessage{
			Role:    peerFrameRole,
			Content: content,
		},
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// locateMessagingSocket returns the bound session's inbox path.
//
// Order, matching notes/10: the registry's messagingSocketPath (the
// same path the process publishes as CLAUDE_CODE_MESSAGING_SOCKET),
// else $XDG_RUNTIME_DIR/cc-socks/<pid>.sock, else
// /run/user/<uid>/cc-socks/<pid>.sock. Duo's own
// CLAUDE_CODE_MESSAGING_SOCKET is some other session's inbox and is
// never used — send only to the target instance (no hop-chain relay).
func (r *Runtime) locateMessagingSocket(sessionID string) (string, error) {
	if sessionID == "" {
		return "", fmt.Errorf("claude runtime %s: prompt path requires an external agent-session id", r.integrationInstanceID)
	}
	entry, ok, err := r.registryLookup(sessionID)
	if err != nil {
		return "", fmt.Errorf("claude runtime %s: registry lookup: %w", r.integrationInstanceID, err)
	}
	if !ok {
		return "", fmt.Errorf("claude runtime %s: no registry entry for session %s", r.integrationInstanceID, sessionID)
	}
	if entry.MessagingSocketPath != "" {
		return entry.MessagingSocketPath, nil
	}
	if entry.PID == 0 {
		return "", fmt.Errorf("claude runtime %s: registry entry for session %s has no messaging socket path or pid", r.integrationInstanceID, sessionID)
	}
	return derivedMessagingSocket(entry.PID), nil
}

func derivedMessagingSocket(pid int) string {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}
	return filepath.Join(dir, "cc-socks", strconv.Itoa(pid)+".sock")
}

// peerGone reports whether the peer has already closed. Notes/10: a
// live messaging server sends nothing unsolicited and waits, so a
// short read timeout means the peer is still there. A deadline in the
// past would time out before EOF is observed, so this uses a brief
// future deadline. EOF or reset after our write is connection loss.
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
