package herdr

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/host"
)

// TestLiveHerdr runs the whole Stage-1 host surface against a real Herdr
// server. It is skipped unless DUO_HERDR_LIVE_SOCKET names one, and it is
// only ever safe to point at a **disposable** server: it creates panes,
// registers an agent, and closes what it created.
//
// Environment:
//
//	DUO_HERDR_LIVE_SOCKET  API socket (herdr.sock) of a disposable server.
//	DUO_HERDR_LIVE_KIND    agent kind for the launch leg. Point it at a kind
//	                       whose canonical executable resolves to something
//	                       harmless. Without it the launch leg is skipped
//	                       and the observation legs run against a discovered
//	                       pane.
//	DUO_HERDR_LIVE_ENV     comma-separated K=V pairs to set inside the
//	                       created pane. This is the environment seam under
//	                       test as much as it is test plumbing: a Herdr pane
//	                       inherits the server's environment, so what Duo
//	                       needs has to travel in the launch tuple. Note
//	                       that a pane's interactive shell re-runs its own
//	                       startup files, which can overwrite what is set
//	                       here (PATH in particular).
//	DUO_HERDR_LIVE_BINARY  herdr executable for the schema-digest probe.
//	DUO_HERDR_LIVE_TARGET  placement for the launch leg: "tab", "pane", or
//	                       unset for the host's built-in default.
func TestLiveHerdr(t *testing.T) {
	socket := os.Getenv("DUO_HERDR_LIVE_SOCKET")
	if socket == "" {
		t.Skip("set DUO_HERDR_LIVE_SOCKET to a disposable Herdr server's API socket")
	}
	ctx := context.Background()

	factory := Factory{Config: Config{
		IntegrationInstanceID: InstanceIDForSession("live"),
		SocketPath:            socket,
		Binary:                os.Getenv("DUO_HERDR_LIVE_BINARY"),
		SnapshotInterval:      250 * time.Millisecond,
		StartRetryDelay:       500 * time.Millisecond,
	}}

	probe, err := factory.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	t.Logf("probe: version=%s protocol=%s digest=%s state=%s compatibility=%s",
		probe.DetectedVersion, probe.ProtocolOrFormatIdentity,
		probe.FixtureOrSchemaDigest, probe.ConnectionState, probe.Compatibility)
	if probe.Compatibility == adapter.CompatibilityUnavailable {
		t.Fatalf("no Herdr server answered at %s", socket)
	}
	if probe.DetectedVersion == PinnedVersion && probe.Compatibility != adapter.CompatibilitySupported {
		t.Errorf("live %s probed as %s: schema digest %s does not match the pin",
			PinnedVersion, probe.Compatibility, probe.FixtureOrSchemaDigest)
	}

	h, err := factory.New(ctx, probe)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	subject, launched := liveSubjectPane(ctx, t, h)
	t.Cleanup(func() {
		if err := h.client.call(context.Background(), "pane.close",
			paneTargetParams{PaneID: subject.PaneID}, nil); err != nil {
			t.Logf("cleanup: pane.close %s: %v", subject.PaneID, err)
		}
	})
	t.Logf("subject pane=%s terminal=%s pid=%d start=%s source=%s launched=%v",
		subject.PaneID, subject.HostContainerID, subject.ProcessBirth.PID,
		subject.ProcessBirth.StartTime, subject.ProcessBirth.StartTimeSource, launched)

	if subject.HostServerEpoch != "" {
		t.Errorf("live evidence claims a server epoch %q; Herdr 0.8.2 has none", subject.HostServerEpoch)
	}
	if subject.HostContainerID == "" {
		t.Error("live evidence carries no terminal_id")
	}
	if !birthProven(subject.ProcessBirth) {
		t.Errorf("live process birth %+v is not proven evidence", subject.ProcessBirth)
	}

	t.Run("discovery reports the subject pane", func(t *testing.T) {
		candidates, err := h.Discover(ctx, host.DiscoveryRequest{
			IntegrationInstanceID: h.cfg.IntegrationInstanceID,
		})
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		for _, c := range candidates {
			if c.Evidence.PaneID == subject.PaneID {
				if c.Evidence.HostContainerID != subject.HostContainerID {
					t.Errorf("discovered terminal_id %q, want %q",
						c.Evidence.HostContainerID, subject.HostContainerID)
				}
				return
			}
		}
		t.Fatalf("discovery of %d candidates did not include %s", len(candidates), subject.PaneID)
	})

	t.Run("attachment validates and a stale terminal does not", func(t *testing.T) {
		claim := host.HostAttachmentClaim{
			Attachment:            AttachmentFor(subject),
			LastKnownProcessBirth: subject.ProcessBirth,
		}
		got, err := h.ValidateAttachment(ctx, claim)
		if err != nil {
			t.Fatalf("ValidateAttachment: %v", err)
		}
		if !got.SameProcess {
			t.Fatalf("live attachment did not validate: %+v", got)
		}
		if got.Class != host.ContinuitySameLive {
			t.Fatalf("Class = %q, want %q", got.Class, host.ContinuitySameLive)
		}

		// A restart restores pane_id but always mints a new terminal_id;
		// this is that claim, without restarting the server.
		stale := claim
		stale.Attachment.HostContainerID = "term_from_a_previous_incarnation"
		got, err = h.ValidateAttachment(ctx, stale)
		if err != nil {
			t.Fatalf("ValidateAttachment: %v", err)
		}
		if got.SameProcess {
			t.Fatal("a stale terminal_id validated; pane coordinates are not continuity")
		}
		if got.Class != host.ContinuityTerminalReplaced {
			t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityTerminalReplaced)
		}
	})

	t.Run("lifecycle observation sees the pane and its exit", func(t *testing.T) {
		stream, err := h.ObserveHostLifecycle(ctx, host.HostObservationRequest{
			Attachment: AttachmentFor(subject),
		})
		if err != nil {
			t.Fatalf("ObserveHostLifecycle: %v", err)
		}
		defer func() { _ = stream.Close() }()

		first := liveNextEvent(t, stream)
		if first.Kind != host.LifecycleAttached {
			t.Fatalf("first live event = %s, want attached", first.Kind)
		}
		t.Logf("live event: %s pane=%s terminal=%s", first.Kind,
			first.Evidence.PaneID, first.Evidence.HostContainerID)

		if err := h.client.call(ctx, "pane.close", paneTargetParams{PaneID: subject.PaneID}, nil); err != nil {
			t.Fatalf("pane.close: %v", err)
		}
		exit := liveNextEvent(t, stream)
		if exit.Kind != host.LifecycleExited {
			t.Fatalf("event after pane.close = %s, want exited", exit.Kind)
		}
		t.Logf("live event: %s pane=%s", exit.Kind, exit.Evidence.PaneID)
	})
}

// liveSubjectPane returns the evidence for the pane the live legs work on:
// one this test launched when a harmless agent kind is configured,
// otherwise the first pane discovery reports.
func liveSubjectPane(ctx context.Context, t *testing.T, h *Host) (host.Evidence, bool) {
	t.Helper()
	kind := os.Getenv("DUO_HERDR_LIVE_KIND")
	if kind == "" {
		candidates, err := h.Discover(ctx, host.DiscoveryRequest{
			IntegrationInstanceID: h.cfg.IntegrationInstanceID,
		})
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if len(candidates) == 0 {
			t.Skip("no panes to observe and DUO_HERDR_LIVE_KIND is unset")
		}
		return candidates[0].Evidence, false
	}

	env := map[string]string{"DUO_LIVE_CHECK": "1"}
	for _, pair := range strings.Split(os.Getenv("DUO_HERDR_LIVE_ENV"), ",") {
		key, value, ok := strings.Cut(strings.TrimSpace(pair), "=")
		if ok && key != "" {
			env[key] = value
		}
	}
	tuple := host.ResolvedLaunchTuple{
		LaunchResolutionID:    "live-" + strings.ReplaceAll(time.Now().UTC().Format("150405.000"), ".", ""),
		IntegrationInstanceID: h.cfg.IntegrationInstanceID,
		WorkspacePath:         t.TempDir(),
		Command:               kind,
		Env:                   env,
		// DUO_HERDR_LIVE_TARGET picks the placement leg: "tab", "pane",
		// or unset for the host's built-in default.
		Target: host.LaunchTarget(os.Getenv("DUO_HERDR_LIVE_TARGET")),
	}
	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: tuple})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	evidence, err := h.Start(ctx, prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return evidence.Evidence, true
}

func liveNextEvent(t *testing.T, stream host.HostObservationStream) host.LifecycleEvent {
	t.Helper()
	select {
	case ev, ok := <-stream.Events():
		if !ok {
			t.Fatal("live event stream closed")
		}
		return ev
	case <-time.After(15 * time.Second):
		t.Fatal("timed out waiting for a live lifecycle event")
		return host.LifecycleEvent{}
	}
}
