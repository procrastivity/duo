package fake_test

import (
	"context"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
)

func TestFactoryDescriptorAndProbe(t *testing.T) {
	ctx := context.Background()
	f := hostfake.Factory{IntegrationInstanceID: "integration-1"}

	d := f.Descriptor()
	if d.AdapterID == "" || d.Role == "" {
		t.Fatalf("descriptor missing AdapterID or Role: %+v", d)
	}

	probe, err := f.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if probe.Compatibility != "supported" {
		t.Fatalf("Probe.Compatibility = %s, want supported", probe.Compatibility)
	}

	h, err := f.New(ctx, probe)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if h == nil {
		t.Fatalf("New returned nil host")
	}
}

func TestDiscoverReturnsSeededCandidates(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New("integration-1")

	h.SeedCandidate(host.HostCandidate{
		Evidence: host.Evidence{
			IntegrationInstanceID: "integration-1",
			HostServerEpoch:       "epoch-1",
		},
		DetectedAt: time.Now(),
	})
	h.SeedCandidate(host.HostCandidate{
		Evidence: host.Evidence{
			IntegrationInstanceID: "other-integration",
		},
		DetectedAt: time.Now(),
	})

	got, err := h.Discover(ctx, host.DiscoveryRequest{IntegrationInstanceID: "integration-1"})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Discover returned %d candidates, want 1 (scoped by integration instance)", len(got))
	}
}

func TestLaunchAttachAndKill(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New("integration-1")

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{
		ResolvedLaunchTuple: host.ResolvedLaunchTuple{
			LaunchResolutionID:    "lr-1",
			IntegrationInstanceID: "integration-1",
			WorkspacePath:         "/workspace",
			Command:               "true",
		},
	})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}

	launched, err := h.Start(ctx, prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if launched.Evidence.ProcessBirth.PID == 0 {
		t.Fatalf("Start returned zero PID")
	}

	attachment := host.Attachment{
		IntegrationInstanceID: launched.Evidence.IntegrationInstanceID,
		HostServerEpoch:       launched.Evidence.HostServerEpoch,
		HostContainerID:       launched.Evidence.HostContainerID,
		PaneID:                launched.Evidence.PaneID,
	}

	continuity, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
		Attachment:            attachment,
		LastKnownProcessBirth: launched.Evidence.ProcessBirth,
	})
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if !continuity.SameProcess {
		t.Fatalf("expected SameProcess true immediately after Start")
	}

	stream, err := h.ObserveHostLifecycle(ctx, host.HostObservationRequest{Attachment: attachment})
	if err != nil {
		t.Fatalf("ObserveHostLifecycle: %v", err)
	}
	defer func() { _ = stream.Close() }()

	h.Kill(attachment)

	select {
	case event := <-stream.Events():
		if event.Kind != host.LifecycleExited {
			t.Fatalf("event.Kind = %s, want exited", event.Kind)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for exited event")
	}

	continuity, err = h.ValidateAttachment(ctx, host.HostAttachmentClaim{
		Attachment:            attachment,
		LastKnownProcessBirth: launched.Evidence.ProcessBirth,
	})
	if err != nil {
		t.Fatalf("ValidateAttachment after Kill: %v", err)
	}
	if continuity.SameProcess {
		t.Fatalf("expected SameProcess false after Kill")
	}
}
