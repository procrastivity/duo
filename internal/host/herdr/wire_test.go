package herdr

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// The wire contract: Herdr closes the connection after every non-streaming
// response, so a client that reuses one gets a broken pipe. Three calls
// must therefore cost three connections.
func TestClientUsesOneConnectionPerRequest(t *testing.T) {
	f := newFakeHerdr(t)
	c := newClient(f.socketPath(), time.Second)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		var pong pongResult
		if err := c.call(ctx, "ping", nil, &pong); err != nil {
			t.Fatalf("ping %d: %v", i, err)
		}
		if pong.Version != PinnedVersion {
			t.Fatalf("ping %d: version %q", i, pong.Version)
		}
	}
	if got := f.connCount(); got != 3 {
		t.Fatalf("connections = %d, want 3 (one per request)", got)
	}
}

// Every request carries an id: 0.8.2 rejects an id-less request with
// invalid_request, so the envelope this client writes is not optional.
func TestClientAlwaysSendsRequestID(t *testing.T) {
	f := newFakeHerdr(t)
	c := newClient(f.socketPath(), time.Second)
	if err := c.call(context.Background(), "ping", nil, nil); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if got := f.lastID(); got == "" {
		t.Fatal("request carried no id; 0.8.2 answers invalid_request to that")
	}
	if got := ErrorCode(errors.New("not protocol")); got != "" {
		t.Fatalf("ErrorCode of a plain error = %q", got)
	}
}

// A refusal from the server is a typed ProtocolError, distinguishable from
// a transport failure. The whole retry and absence logic keys on that
// distinction.
func TestClientReturnsTypedProtocolError(t *testing.T) {
	f := newFakeHerdr(t)
	c := newClient(f.socketPath(), time.Second)

	err := c.call(context.Background(), "pane.get", paneTargetParams{PaneID: "w9:p9"}, nil)
	if err == nil {
		t.Fatal("pane.get on a missing pane returned no error")
	}
	if got := ErrorCode(err); got != CodePaneNotFound {
		t.Fatalf("ErrorCode = %q, want %q", got, CodePaneNotFound)
	}
	var pe *ProtocolError
	if !errors.As(err, &pe) {
		t.Fatalf("error %v is not a *ProtocolError", err)
	}
	if pe.Method != "pane.get" {
		t.Fatalf("ProtocolError.Method = %q", pe.Method)
	}
}

func TestClientTransportFailureIsNotAProtocolError(t *testing.T) {
	c := newClient(filepath.Join(shortTempDir(t), "absent.sock"), 200*time.Millisecond)
	err := c.call(context.Background(), "ping", nil, nil)
	if err == nil {
		t.Fatal("ping against an absent socket returned no error")
	}
	if got := ErrorCode(err); got != "" {
		t.Fatalf("ErrorCode = %q, want empty for a transport failure", got)
	}
}

// events.subscribe keeps its connection: the ack arrives first, then
// events stream on the same connection until it is closed.
func TestSubscribeStreamsOnOneConnection(t *testing.T) {
	f := newFakeHerdr(t)
	c := newClient(f.socketPath(), time.Second)

	stream, err := c.subscribe(context.Background(), []subscription{{Type: "pane.created"}})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer func() { _ = stream.Close() }()

	f.emitEvent("pane_closed", map[string]any{
		"type": "pane_closed", "pane_id": "w1:p1", "workspace_id": "w1",
	})
	ev := nextStreamEvent(t, stream)
	if ev.Event != "pane_closed" {
		t.Fatalf("event = %q, want pane_closed", ev.Event)
	}
}

func nextStreamEvent(t *testing.T, stream *eventStream) eventEnvelope {
	t.Helper()
	type result struct {
		ev  eventEnvelope
		err error
	}
	ch := make(chan result, 1)
	go func() {
		ev, err := stream.next()
		ch <- result{ev, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatalf("next: %v", r.err)
		}
		return r.ev
	case <-time.After(3 * time.Second):
		_ = stream.Close()
		t.Fatal("timed out waiting for a streamed event")
		return eventEnvelope{}
	}
}
