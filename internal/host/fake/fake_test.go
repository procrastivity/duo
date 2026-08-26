package fake_test

import (
	"context"
	"errors"
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
	if continuity.Class != host.ContinuitySameLive {
		t.Fatalf("Class = %q, want %q", continuity.Class, host.ContinuitySameLive)
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
	if continuity.Class != host.ContinuityPaneAbsent {
		t.Fatalf("Class after Kill = %q, want %q", continuity.Class, host.ContinuityPaneAbsent)
	}
}

func TestFakeHostImplementsPromptProvider(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New("integration-1")
	attachment, _ := startFake(t, h)

	path, err := h.PromptPath(ctx, attachment)
	if err != nil {
		t.Fatalf("PromptPath: %v", err)
	}
	if path.Quality != "exact" || path.Realization != "native" {
		t.Fatalf("path = %+v, want exact/native", path)
	}

	delivered, err := h.DeliverPrompt(ctx, host.PromptRequest{Attachment: attachment, Text: "hello"})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if delivered.Outcome != host.PromptOutcomeDelivered {
		t.Fatalf("default Outcome = %q, want delivered", delivered.Outcome)
	}
	if delivered.Acknowledged {
		t.Fatal("fake delivered result must not claim acknowledgment")
	}

	h.ScriptPromptOutcome(host.PromptOutcomeNoEffect)
	none, err := h.DeliverPrompt(ctx, host.PromptRequest{Attachment: attachment, Text: "hello"})
	if err != nil {
		t.Fatalf("DeliverPrompt scripted no_effect: %v", err)
	}
	if none.Outcome != host.PromptOutcomeNoEffect {
		t.Fatalf("scripted Outcome = %q, want no_effect", none.Outcome)
	}

	h.ScriptPromptOutcome("")
	h.Disconnect()
	lost, err := h.DeliverPrompt(ctx, host.PromptRequest{Attachment: attachment, Text: "hello"})
	if err != nil {
		t.Fatalf("DeliverPrompt after Disconnect: %v", err)
	}
	if lost.Outcome != host.PromptOutcomeNoEffect {
		t.Fatalf("unreachable Outcome = %q, want no_effect", lost.Outcome)
	}
}

func TestValidateAttachmentClasses(t *testing.T) {
	ctx := context.Background()

	t.Run("same-live", func(t *testing.T) {
		h := hostfake.New("integration-1")
		attachment, birth := startFake(t, h)
		got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
			Attachment:            attachment,
			LastKnownProcessBirth: birth,
		})
		if err != nil {
			t.Fatalf("ValidateAttachment: %v", err)
		}
		if got.Class != host.ContinuitySameLive || !got.SameProcess {
			t.Fatalf("got Class=%q SameProcess=%v", got.Class, got.SameProcess)
		}
	})

	t.Run("never-started pane is absent", func(t *testing.T) {
		h := hostfake.New("integration-1")
		got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
			Attachment: host.Attachment{
				IntegrationInstanceID: "integration-1",
				PaneID:                "fake-pane-missing",
			},
		})
		if err != nil {
			t.Fatalf("ValidateAttachment: %v", err)
		}
		if got.Class != host.ContinuityPaneAbsent {
			t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityPaneAbsent)
		}
		if got.SameProcess {
			t.Fatal("SameProcess true for a pane that was never started")
		}
	})

	t.Run("process replaced", func(t *testing.T) {
		h := hostfake.New("integration-1")
		attachment, birth := startFake(t, h)
		live := h.ReplaceProcess(attachment)
		if live.ProcessBirth.PID == birth.PID {
			t.Fatal("ReplaceProcess kept the original PID")
		}
		got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
			Attachment:            attachment,
			LastKnownProcessBirth: birth,
		})
		if err != nil {
			t.Fatalf("ValidateAttachment: %v", err)
		}
		if got.Class != host.ContinuityProcessReplaced {
			t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityProcessReplaced)
		}
		if got.SameProcess {
			t.Fatal("SameProcess true after ReplaceProcess")
		}
		if got.Evidence.PaneID != attachment.PaneID {
			t.Fatalf("pane vanished on process replace: %q", got.Evidence.PaneID)
		}
		if got.Evidence.HostContainerID != attachment.HostContainerID {
			t.Fatalf("terminal changed on process replace: %q", got.Evidence.HostContainerID)
		}
	})

	t.Run("terminal replaced", func(t *testing.T) {
		h := hostfake.New("integration-1")
		attachment, birth := startFake(t, h)
		live := h.ReplaceTerminal(attachment)
		if live.HostContainerID == attachment.HostContainerID {
			t.Fatal("ReplaceTerminal kept the original container id")
		}
		got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
			Attachment:            attachment,
			LastKnownProcessBirth: birth,
		})
		if err != nil {
			t.Fatalf("ValidateAttachment: %v", err)
		}
		if got.Class != host.ContinuityTerminalReplaced {
			t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityTerminalReplaced)
		}
		if got.Evidence.HostContainerID != live.HostContainerID {
			t.Fatalf("live terminal = %q, want %q", got.Evidence.HostContainerID, live.HostContainerID)
		}
		if got.Evidence.PaneID != attachment.PaneID {
			t.Fatalf("pane vanished on terminal replace: %q", got.Evidence.PaneID)
		}
	})
}

func TestValidateAttachmentUnprovenLiveBirth(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New("integration-1")
	attachment, birth := startFake(t, h)
	h.UnproveProcess(attachment)

	got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
		Attachment:            attachment,
		LastKnownProcessBirth: birth,
	})
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.Class != host.ContinuityUnproven {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityUnproven)
	}
	if got.SameProcess {
		t.Fatal("SameProcess true for unproven live birth")
	}
	if got.Evidence.PaneID != attachment.PaneID {
		t.Fatalf("unproven birth must keep the pane; PaneID = %q", got.Evidence.PaneID)
	}
}

func TestValidateAttachmentUnprovenClaim(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New("integration-1")
	attachment, _ := startFake(t, h)

	got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
		Attachment:            attachment,
		LastKnownProcessBirth: host.ProcessBirthEvidence{},
	})
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.Class != host.ContinuityUnproven {
		t.Fatalf("Class = %q, want %q for a zero claimed birth", got.Class, host.ContinuityUnproven)
	}
}

func TestValidateAttachmentDisconnectIsUnreachable(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New("integration-1")
	attachment, birth := startFake(t, h)
	h.Disconnect()

	_, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
		Attachment:            attachment,
		LastKnownProcessBirth: birth,
	})
	if err == nil {
		t.Fatal("ValidateAttachment succeeded after Disconnect")
	}
	if !errors.Is(err, host.ErrUnreachable) {
		t.Fatalf("error = %v, want host.ErrUnreachable", err)
	}
}

func TestKillIsPaneAbsentNotProcessReplaced(t *testing.T) {
	ctx := context.Background()
	h := hostfake.New("integration-1")
	attachment, birth := startFake(t, h)
	h.Kill(attachment)

	got, err := h.ValidateAttachment(ctx, host.HostAttachmentClaim{
		Attachment:            attachment,
		LastKnownProcessBirth: birth,
	})
	if err != nil {
		t.Fatalf("ValidateAttachment: %v", err)
	}
	if got.Class != host.ContinuityPaneAbsent {
		t.Fatalf("Class = %q, want %q", got.Class, host.ContinuityPaneAbsent)
	}
}

func startFake(t *testing.T, h *hostfake.Host) (host.Attachment, host.ProcessBirthEvidence) {
	t.Helper()
	prepared, err := h.PrepareLaunch(context.Background(), host.HostLaunchRequest{
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
	launched, err := h.Start(context.Background(), prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return host.Attachment{
		IntegrationInstanceID: launched.Evidence.IntegrationInstanceID,
		HostServerEpoch:       launched.Evidence.HostServerEpoch,
		HostContainerID:       launched.Evidence.HostContainerID,
		PaneID:                launched.Evidence.PaneID,
	}, launched.Evidence.ProcessBirth
}
