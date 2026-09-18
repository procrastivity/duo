package herdr

import (
	"context"
	"testing"

	"github.com/procrastivity/duo/internal/host"
)

func TestCloseExactAttachmentClosesOnlyTheClaimedPane(t *testing.T) {
	f := newFakeHerdr(t)
	target := f.addPane("w1")
	survivor := f.addPane("w1")
	h := testHost(t, f)

	if err := h.CloseExactAttachment(context.Background(), exactControlClaim(target)); err != nil {
		t.Fatalf("CloseExactAttachment: %v", err)
	}

	if got := f.callCount("pane.close"); got != 1 {
		t.Fatalf("pane.close calls = %d, want 1", got)
	}
	if got := f.paneCount(); got != 1 {
		t.Fatalf("remaining panes = %d, want 1", got)
	}
	f.mu.Lock()
	_, targetFound := f.findLocked(target.paneID)
	_, survivorFound := f.findLocked(survivor.paneID)
	f.mu.Unlock()
	if targetFound || !survivorFound {
		t.Fatalf("pane state after close: target found = %t, survivor found = %t", targetFound, survivorFound)
	}
}

func TestCloseExactAttachmentRejectsIncompleteClaimsBeforeClose(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*host.HostAttachmentClaim)
	}{
		{
			name: "integration instance",
			mutate: func(claim *host.HostAttachmentClaim) {
				claim.Attachment.IntegrationInstanceID = ""
			},
		},
		{
			name: "pane ID",
			mutate: func(claim *host.HostAttachmentClaim) {
				claim.Attachment.PaneID = ""
			},
		},
		{
			name: "terminal container ID",
			mutate: func(claim *host.HostAttachmentClaim) {
				claim.Attachment.HostContainerID = ""
			},
		},
		{
			name: "process birth",
			mutate: func(claim *host.HostAttachmentClaim) {
				claim.LastKnownProcessBirth = host.ProcessBirthEvidence{}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeHerdr(t)
			pane := f.addPane("w1")
			h := testHost(t, f)
			claim := exactControlClaim(pane)
			tt.mutate(&claim)

			if err := h.CloseExactAttachment(context.Background(), claim); err == nil {
				t.Fatal("CloseExactAttachment accepted an incomplete claim")
			}
			if got := f.callCount("pane.close"); got != 0 {
				t.Fatalf("pane.close calls = %d, want 0", got)
			}
		})
	}
}

func TestCloseExactAttachmentRefusesAnythingButExactSameLiveContinuity(t *testing.T) {
	t.Run("wrong integration", func(t *testing.T) {
		f := newFakeHerdr(t)
		pane := f.addPane("w1")
		h := testHost(t, f)
		claim := exactControlClaim(pane)
		claim.Attachment.IntegrationInstanceID = "herdr:other"

		assertExactCloseRefused(t, h, f, claim)
		if got := f.callCount("pane.get"); got != 0 {
			t.Fatalf("pane.get calls = %d, want 0 for the wrong integration", got)
		}
	})

	t.Run("terminal replacement", func(t *testing.T) {
		f := newFakeHerdr(t)
		pane := f.addPane("w1")
		h := testHost(t, f)
		claim := exactControlClaim(pane)
		f.mutatePane(pane.paneID, func(p *fakePaneState) { p.terminalID = "term_replaced" })

		assertExactCloseRefused(t, h, f, claim)
	})

	t.Run("process replacement", func(t *testing.T) {
		f := newFakeHerdr(t)
		pane := f.addPane("w1")
		h := testHost(t, f)
		claim := exactControlClaim(pane)
		f.mutatePane(pane.paneID, func(p *fakePaneState) { p.fgPID++ })

		assertExactCloseRefused(t, h, f, claim)
	})

	t.Run("unproven live birth", func(t *testing.T) {
		f := newFakeHerdr(t)
		pane := f.addPane("w1")
		h := testHost(t, f, func(c *Config) { c.ResolveProcessBirth = unprovenBirth })

		assertExactCloseRefused(t, h, f, exactControlClaim(pane))
	})

	t.Run("already absent pane", func(t *testing.T) {
		f := newFakeHerdr(t)
		pane := f.addPane("w1")
		h := testHost(t, f)
		claim := exactControlClaim(pane)
		f.removePane(pane.paneID)

		assertExactCloseRefused(t, h, f, claim)
	})

	t.Run("nonmatching server epoch", func(t *testing.T) {
		f := newFakeHerdr(t)
		pane := f.addPane("w1")
		h := testHost(t, f)
		claim := exactControlClaim(pane)
		claim.Attachment.HostServerEpoch = "epoch-herdr-does-not-have"

		assertExactCloseRefused(t, h, f, claim)
	})
}

func TestCloseExactAttachmentReturnsCloseProtocolError(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)
	f.setPaneCloseError(CodePaneNotFound, "pane disappeared after validation")

	err := h.CloseExactAttachment(context.Background(), exactControlClaim(pane))
	if err == nil {
		t.Fatal("CloseExactAttachment treated pane_not_found as success")
	}
	if got := ErrorCode(err); got != CodePaneNotFound {
		t.Fatalf("ErrorCode = %q, want %q (error: %v)", got, CodePaneNotFound, err)
	}
	if got := f.callCount("pane.close"); got != 1 {
		t.Fatalf("pane.close calls = %d, want 1", got)
	}
}

func assertExactCloseRefused(t *testing.T, h *Host, f *fakeHerdr, claim host.HostAttachmentClaim) {
	t.Helper()
	if err := h.CloseExactAttachment(context.Background(), claim); err == nil {
		t.Fatal("CloseExactAttachment accepted a claim without exact same-live continuity")
	}
	if got := f.callCount("pane.close"); got != 0 {
		t.Fatalf("pane.close calls = %d, want 0", got)
	}
}

func exactControlClaim(pane fakePaneState) host.HostAttachmentClaim {
	info := processInfo{ShellPID: pane.shellPID}
	if pane.fgPID > 0 {
		info.ForegroundProcesses = []processEntry{{PID: pane.fgPID}}
	}
	return claimFor(pane, fakeBirth(context.Background(), info))
}
