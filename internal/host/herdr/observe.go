package herdr

import (
	"context"
	"sync"
	"time"

	"github.com/procrastivity/duo/internal/host"
)

// observationBuffer is the depth of one stream's event channel. A full
// channel drops the oldest-arriving event rather than blocking the
// adapter's own reconcile loop (§9: adapter backpressure cannot block the
// authority writer indefinitely). Dropping is safe here in a way it would
// not be for an event-derived design: every emission is derived from a
// snapshot, so a later reconcile re-reports the current truth.
const observationBuffer = 64

// presence is what the last successful snapshot said about the observed
// pane. It starts unknown so the first reconcile always emits something.
type presence int

const (
	presenceUnknown presence = iota
	presencePresent
	presenceGone
)

// observationStream implements host.HostObservationStream over Herdr.
//
// Two goroutines. readEvents holds the events.subscribe connection and
// does exactly one thing with what it reads: wake the other goroutine.
// reconcileLoop owns all state and derives every emission from a fresh
// session.snapshot. That split is the encoded probe finding — pane.created
// backfill is partial and shape-identical to a real creation, and events
// between subscriptions are lost with no cursor to recover them, so an
// event may signal "look again" and nothing more.
type observationStream struct {
	host       *Host
	attachment host.Attachment
	events     chan host.LifecycleEvent
	wake       chan struct{}
	done       chan struct{}
	cancel     context.CancelFunc
	closeOnce  sync.Once
	wg         sync.WaitGroup

	// Owned by reconcileLoop only.
	presence  presence
	reachable bool
	last      host.Evidence
}

func (s *observationStream) Events() <-chan host.LifecycleEvent { return s.events }

// Close ends the stream and waits for its goroutines to finish. The event
// channel is closed afterwards, so a caller ranging over Events sees the
// range end.
func (s *observationStream) Close() error {
	s.closeOnce.Do(func() { s.cancel() })
	<-s.done
	return nil
}

// start launches the stream's goroutines. It is called once, by
// ObserveHostLifecycle.
func (s *observationStream) start(ctx context.Context) {
	s.reachable = true
	s.wg.Add(2)
	go func() {
		defer s.wg.Done()
		s.readEvents(ctx)
	}()
	go func() {
		defer s.wg.Done()
		s.reconcileLoop(ctx)
	}()
	go func() {
		s.wg.Wait()
		close(s.events)
		close(s.done)
	}()
}

// subscriptions are the pane-lifecycle events worth waking for. They are
// all global (no pane_id), because a subscription scoped to a pane cannot
// tell us about the pane's disappearance any better than the snapshot can,
// and because resubscribing on pane-set changes would add gaps rather than
// remove them.
func (s *observationStream) subscriptions() []subscription {
	return []subscription{
		{Type: "pane.created"},
		{Type: "pane.closed"},
		{Type: "pane.exited"},
		{Type: "pane.updated"},
	}
}

func (s *observationStream) readEvents(ctx context.Context) {
	for ctx.Err() == nil {
		stream, err := s.host.client.subscribe(ctx, s.subscriptions())
		if err != nil {
			s.signal()
			sleepCtx(ctx, s.host.cfg.SnapshotInterval)
			continue
		}
		// Every (re)subscribe opens a gap: events that happened while no
		// subscription was live are gone, and Herdr has no replay token.
		// So the first act after subscribing is to re-diff, not to trust
		// whatever the server chooses to backfill.
		s.signal()

		closed := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
			case <-closed:
			}
			_ = stream.Close()
		}()

		for {
			if _, err := stream.next(); err != nil {
				break
			}
			s.signal()
		}
		close(closed)
		s.signal()
		sleepCtx(ctx, s.host.cfg.SnapshotInterval)
	}
}

func (s *observationStream) reconcileLoop(ctx context.Context) {
	ticker := time.NewTicker(s.host.cfg.SnapshotInterval)
	defer ticker.Stop()
	s.reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
			s.reconcile(ctx)
		case <-ticker.C:
			s.reconcile(ctx)
		}
	}
}

// signal is a non-blocking wake. Coalescing is intended: several events
// arriving together need one re-diff, not several.
func (s *observationStream) signal() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// reconcile is the only place lifecycle state changes. It reads the
// inventory (session.snapshot), compares it with what it last saw, and
// emits the difference.
func (s *observationStream) reconcile(ctx context.Context) {
	snap, err := s.host.snapshot(ctx)
	if err != nil {
		s.markUnreachable()
		return
	}
	pane, found := findPane(snap, s.attachment.PaneID)

	var birth host.ProcessBirthEvidence
	if found {
		b, birthErr := s.host.processBirth(ctx, pane.PaneID)
		switch {
		case birthErr == nil:
			birth = b
		case ErrorCode(birthErr) == CodePaneNotFound:
			// The pane went away between the snapshot and this lookup.
			found = false
		default:
			s.markUnreachable()
			return
		}
	}
	s.reachable = true

	if !found {
		if s.presence != presenceGone {
			s.emit(host.LifecycleExited, s.lastKnownEvidence())
			s.presence = presenceGone
		}
		return
	}

	evidence := evidenceFor(s.host.cfg.IntegrationInstanceID, pane, birth)
	switch {
	case s.presence != presencePresent:
		s.presence = presencePresent
		s.last = evidence
		s.emit(host.LifecycleAttached, evidence)
	case s.incarnationChanged(pane, birth):
		// Same pane coordinates, different terminal or different process:
		// the thing we were observing is gone and something else is here.
		s.emit(host.LifecycleExited, s.last)
		s.last = evidence
		s.emit(host.LifecycleAttached, evidence)
	}
}

// incarnationChanged reports whether the pane still holds what it held at
// the last reconcile. terminal_id is the authoritative signal (a restart
// or a new terminal always changes it); a proven change of process birth
// under a stable terminal_id catches a runtime that exited and was
// replaced inside the same shell.
func (s *observationStream) incarnationChanged(pane paneInfo, birth host.ProcessBirthEvidence) bool {
	if pane.TerminalID != s.last.HostContainerID {
		return true
	}
	if birthProven(birth) && birthProven(s.last.ProcessBirth) {
		return !sameBirth(birth, s.last.ProcessBirth)
	}
	return false
}

// markUnreachable reports that observation itself was lost. It is
// deliberately not an exit claim: an unreachable server says nothing about
// whether the process is still running. The next successful reconcile
// re-derives presence from the snapshot and emits attached or exited
// accordingly.
func (s *observationStream) markUnreachable() {
	if !s.reachable {
		return
	}
	s.reachable = false
	s.presence = presenceUnknown
	s.emit(host.LifecycleDetached, s.lastKnownEvidence())
}

// lastKnownEvidence is the evidence to attach to an exit or detach: what
// was last observed, or — before anything was observed — the claim itself,
// so the event still names what it is about.
func (s *observationStream) lastKnownEvidence() host.Evidence {
	if s.last.PaneID != "" {
		return s.last
	}
	return host.Evidence{
		IntegrationInstanceID: s.attachment.IntegrationInstanceID,
		HostServerEpoch:       NoServerEpoch,
		HostContainerID:       s.attachment.HostContainerID,
		PaneID:                s.attachment.PaneID,
	}
}

func (s *observationStream) emit(kind host.LifecycleEventKind, evidence host.Evidence) {
	event := host.LifecycleEvent{
		Kind:       kind,
		Evidence:   evidence,
		ObservedAt: s.host.cfg.Now(),
	}
	select {
	case s.events <- event:
	default:
	}
}

func findPane(snap sessionSnapshot, paneID string) (paneInfo, bool) {
	for _, pane := range snap.Panes {
		if pane.PaneID == paneID {
			return pane, true
		}
	}
	return paneInfo{}, false
}

func sleepCtx(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
