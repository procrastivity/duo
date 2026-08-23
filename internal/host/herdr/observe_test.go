package herdr

import (
	"context"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/host"
)

func observe(t *testing.T, h *Host, pane fakePaneState) host.HostObservationStream {
	t.Helper()
	stream, err := h.ObserveHostLifecycle(context.Background(), host.HostObservationRequest{
		Attachment: host.Attachment{
			IntegrationInstanceID: testInstanceID,
			HostServerEpoch:       NoServerEpoch,
			HostContainerID:       pane.terminalID,
			PaneID:                pane.paneID,
		},
	})
	if err != nil {
		t.Fatalf("ObserveHostLifecycle: %v", err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	return stream
}

func nextEvent(t *testing.T, stream host.HostObservationStream) host.LifecycleEvent {
	t.Helper()
	select {
	case ev, ok := <-stream.Events():
		if !ok {
			t.Fatal("event stream closed")
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a lifecycle event")
		return host.LifecycleEvent{}
	}
}

func expectNoEvent(t *testing.T, stream host.HostObservationStream, within time.Duration) {
	t.Helper()
	select {
	case ev := <-stream.Events():
		t.Fatalf("unexpected lifecycle event %+v", ev)
	case <-time.After(within):
	}
}

// The first observation is derived from session.snapshot, not from an
// event: a pane restored from session persistence never replays a
// pane.created, so an event-driven first observation would report nothing
// at all for the most common case.
func TestObserveDerivesAttachmentFromTheSnapshot(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	stream := observe(t, h, pane)
	ev := nextEvent(t, stream)
	if ev.Kind != host.LifecycleAttached {
		t.Fatalf("first event = %s, want attached", ev.Kind)
	}
	if ev.Evidence.PaneID != pane.paneID || ev.Evidence.HostContainerID != pane.terminalID {
		t.Fatalf("attached evidence = %+v", ev.Evidence)
	}
	if ev.Evidence.HostServerEpoch != "" {
		t.Fatalf("attached evidence claims a server epoch: %q", ev.Evidence.HostServerEpoch)
	}
}

// pane.created backfill is partial at 0.8.2 and shape-identical to a real
// creation, so an event must never become inventory. Here the server
// replays a pane that does not exist; nothing may be emitted for it.
func TestObserveTreatsBackfillAsAWakeUpNotInventory(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.setBackfill("w1:p99", "w1:p100")
	h := testHost(t, f)

	stream := observe(t, h, pane)
	ev := nextEvent(t, stream)
	if ev.Kind != host.LifecycleAttached || ev.Evidence.PaneID != pane.paneID {
		t.Fatalf("first event = %+v, want attached for %s", ev, pane.paneID)
	}
	// The two backfilled phantom panes must produce nothing, and the
	// observed pane must not be re-announced because an event arrived.
	expectNoEvent(t, stream, 200*time.Millisecond)
}

// The pane an observation watches can be absent from the very first
// snapshot — the attachment claim is stale. That is an exit, reported from
// the inventory rather than from a missed event.
func TestObserveReportsAnAlreadyGonePane(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.removePane(pane.paneID)
	h := testHost(t, f)

	stream := observe(t, h, pane)
	ev := nextEvent(t, stream)
	if ev.Kind != host.LifecycleExited {
		t.Fatalf("event = %s, want exited", ev.Kind)
	}
	if ev.Evidence.PaneID != pane.paneID {
		t.Fatalf("exit evidence = %+v, want the claimed pane", ev.Evidence)
	}
}

// No pane.closed event is emitted by the fake here: the disappearance is
// found by the snapshot poll alone, which is what has to work when an
// event is lost in a resubscribe gap.
func TestObserveDetectsDisappearanceWithoutAnyEvent(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	stream := observe(t, h, pane)
	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleAttached {
		t.Fatalf("first event = %s, want attached", ev.Kind)
	}
	f.removePane(pane.paneID)
	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleExited {
		t.Fatalf("event = %s, want exited", ev.Kind)
	}
}

// Same pane_id, new terminal_id: the server restarted and restored the
// pane. The old incarnation exited and a new one is present; reporting
// only "still attached" would silently re-use a dead runtime instance.
func TestObserveReportsANewIncarnationOnTheSamePaneID(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	stream := observe(t, h, pane)
	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleAttached {
		t.Fatalf("first event = %s, want attached", ev.Kind)
	}

	f.mutatePane(pane.paneID, func(p *fakePaneState) {
		p.terminalID = "term_restored"
		p.fgPID = 9001
	})

	exited := nextEvent(t, stream)
	if exited.Kind != host.LifecycleExited {
		t.Fatalf("event = %s, want exited for the old incarnation", exited.Kind)
	}
	if exited.Evidence.HostContainerID != pane.terminalID {
		t.Fatalf("exit evidence = %q, want the old terminal %q",
			exited.Evidence.HostContainerID, pane.terminalID)
	}
	attached := nextEvent(t, stream)
	if attached.Kind != host.LifecycleAttached {
		t.Fatalf("event = %s, want attached for the new incarnation", attached.Kind)
	}
	if attached.Evidence.HostContainerID != "term_restored" {
		t.Fatalf("attach evidence = %q, want the new terminal", attached.Evidence.HostContainerID)
	}
}

// A process replaced inside a stable terminal (the runtime exited and the
// shell ran something else) is also a new incarnation, caught by the birth
// fingerprint rather than by terminal_id.
func TestObserveReportsAReplacedProcessUnderAStableTerminal(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	stream := observe(t, h, pane)
	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleAttached {
		t.Fatalf("first event = %s, want attached", ev.Kind)
	}
	f.mutatePane(pane.paneID, func(p *fakePaneState) { p.fgPID = 6001 })

	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleExited {
		t.Fatalf("event = %s, want exited", ev.Kind)
	}
	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleAttached {
		t.Fatalf("event = %s, want attached", ev.Kind)
	}
}

// A dropped subscription is a gap: events that happened while no
// subscription was live are unrecoverable (no cursor, no replay token).
// The stream must resubscribe and re-diff rather than wait for an event
// that will never come.
func TestObserveResubscribesAndRediffsAfterAStreamDrop(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	stream := observe(t, h, pane)
	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleAttached {
		t.Fatalf("first event = %s, want attached", ev.Kind)
	}
	waitFor(t, "the first subscription", func() bool { return f.callCount("events.subscribe") >= 1 })

	subscriptions := f.callCount("events.subscribe")
	f.dropSubscribers()
	// The change happens while no subscription is live, so no event will
	// ever report it.
	f.removePane(pane.paneID)

	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleExited {
		t.Fatalf("event = %s, want exited found by the post-resubscribe diff", ev.Kind)
	}
	waitFor(t, "a resubscription", func() bool { return f.callCount("events.subscribe") > subscriptions })
}

// An unreachable server is not an exit claim: it says nothing about
// whether the process is running. It reports detached, and the first
// successful snapshot afterwards re-derives the truth.
func TestObserveReportsDetachedWhenTheServerGoesAway(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	stream := observe(t, h, pane)
	if ev := nextEvent(t, stream); ev.Kind != host.LifecycleAttached {
		t.Fatalf("first event = %s, want attached", ev.Kind)
	}
	f.stop()

	ev := nextEvent(t, stream)
	if ev.Kind != host.LifecycleDetached {
		t.Fatalf("event = %s, want detached (observation lost, not an exit)", ev.Kind)
	}
	if ev.Evidence.PaneID != pane.paneID {
		t.Fatalf("detach evidence = %+v", ev.Evidence)
	}
	expectNoEvent(t, stream, 150*time.Millisecond)
}

func TestObserveCloseEndsTheStream(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	stream, err := h.ObserveHostLifecycle(context.Background(), host.HostObservationRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
	})
	if err != nil {
		t.Fatalf("ObserveHostLifecycle: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	deadline := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-stream.Events():
			if !ok {
				if err := stream.Close(); err != nil {
					t.Fatalf("second Close: %v", err)
				}
				return
			}
		case <-deadline:
			t.Fatal("event channel never closed")
		}
	}
}

func TestObserveRequiresAPaneID(t *testing.T) {
	f := newFakeHerdr(t)
	h := testHost(t, f)
	if _, err := h.ObserveHostLifecycle(context.Background(), host.HostObservationRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID},
	}); err == nil {
		t.Fatal("ObserveHostLifecycle accepted an attachment with no pane ID")
	}
}
