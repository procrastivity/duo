package opencode

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
)

// EventStream is the package-owned, read-only /event observer. It never
// reconnects or replays; State becomes unknown when the process disappears
// and stale on a transport or framing failure.
type EventStream interface {
	Events() <-chan Event
	State() string
	Close() error
}

type eventStream struct {
	events chan Event
	done   chan struct{}
	state  atomic.Value
	cancel context.CancelFunc
	once   sync.Once
}

func (s *eventStream) Events() <-chan Event { return s.events }

func (s *eventStream) State() string     { return s.state.Load().(string) }
func (s *eventStream) setState(v string) { s.state.Store(v) }
func (s *eventStream) Close() error {
	s.once.Do(func() {
		close(s.done)
		s.cancel()
	})
	return nil
}

// ObserveEvents opens the bound read-only SSE event stream without retrying.
func (r *Runtime) ObserveEvents(ctx context.Context) (EventStream, error) {
	if err := r.ensureAdmitted(); err != nil {
		return nil, err
	}
	if err := r.binding.validate(); err != nil {
		return nil, err
	}
	if r.credential == "" {
		return nil, fmt.Errorf("opencode: credentials required")
	}
	requestCtx, cancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, r.binding.Endpoint+"/event", nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opencode: event unavailable")
	}
	req.Header.Set("Authorization", r.credential)
	resp, err := r.client.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("opencode: event unavailable")
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		cancel()
		return nil, fmt.Errorf("opencode: event rejected")
	}
	s := &eventStream{events: make(chan Event, 8), done: make(chan struct{}), cancel: cancel}
	s.state.Store("fresh")
	go func() { <-requestCtx.Done(); _ = resp.Body.Close() }()
	go func() {
		defer cancel()
		defer close(s.events)
		defer func() { _ = resp.Body.Close() }()
		f, _ := NewFramer(r.binding.ProcessEpoch, r.binding.SessionID)
		read := make([]byte, 4096)
		for {
			select {
			case <-ctx.Done():
				s.setState("unknown")
				return
			case <-s.done:
				s.setState("unknown")
				return
			default:
			}
			n, er := resp.Body.Read(read)
			if n > 0 {
				es, fe := f.Feed(read[:n])
				for _, e := range es {
					select {
					case s.events <- e:
					case <-s.done:
						s.setState("unknown")
						return
					}
				}
				if fe != nil {
					s.setState("stale")
					return
				}
			}
			if er != nil {
				f.Finalize()
				if f.Truncated() {
					s.setState("stale")
				} else {
					s.setState("unknown")
				}
				return
			}
		}
	}()
	return s, nil
}

// ObserveEventsFromReader is useful for deterministic transport tests. A
// context cannot interrupt an arbitrary blocking io.Reader; callers needing
// cancellation while Read is blocked must use ObserveEventsFromReaderWithCloser
// and provide the transport's close operation.
func ObserveEventsFromReader(ctx context.Context, epoch, session string, rd io.ReadCloser) ([]Event, string, error) {
	return ObserveEventsFromReaderWithCloser(ctx, epoch, session, rd)
}

// ObserveEventsFromReaderWithCloser adds the required cancellation seam for
// readers whose Close interrupts Read. It never reconnects or replays.
func ObserveEventsFromReaderWithCloser(ctx context.Context, epoch, session string, rd io.ReadCloser) ([]Event, string, error) {
	f, err := NewFramer(epoch, session)
	if err != nil {
		return nil, "stale", err
	}
	var es []Event
	chunk := make([]byte, 4096)
	for {
		select {
		case <-ctx.Done():
			f.Finalize()
			return es, "unknown", ctx.Err()
		default:
		}
		type result struct {
			n   int
			err error
		}
		reads := make(chan result, 1)
		go func() { n, err := rd.Read(chunk); reads <- result{n, err} }()
		var n int
		var readErr error
		select {
		case <-ctx.Done():
			_ = rd.Close()
			f.Finalize()
			return es, "unknown", ctx.Err()
		case got := <-reads:
			n, readErr = got.n, got.err
		}
		if n > 0 {
			got, feedErr := f.Feed(chunk[:n])
			es = append(es, got...)
			if feedErr != nil {
				return es, "stale", feedErr
			}
		}
		if readErr != nil {
			f.Finalize()
			if f.Truncated() {
				return es, "stale", nil
			}
			return es, "unknown", nil
		}
	}
}
