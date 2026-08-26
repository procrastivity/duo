package herdr

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/host"
)

func TestPromptPathIsNativeNotComposerSafe(t *testing.T) {
	f := newFakeHerdr(t)
	h := testHost(t, f)

	got, err := h.PromptPath(context.Background(), host.Attachment{
		IntegrationInstanceID: testInstanceID,
		PaneID:                "w1:p1",
	})
	if err != nil {
		t.Fatalf("PromptPath: %v", err)
	}
	if got.Quality != "exact" || got.Realization != "native" {
		t.Fatalf("path = %+v, want exact/native", got)
	}
	if got.ComposerSafe {
		t.Fatal("ComposerSafe true: notes/19 §2 verified agent.prompt clobbers a live composer draft")
	}
	if f.callCount("agent.prompt") != 0 {
		t.Fatal("PromptPath must not send input")
	}
}

func TestDeliverPromptCleanAdmissionIsDeliveredNotAcknowledged(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.addAgent("duo-probe", pane.paneID)
	h := testHost(t, f)

	got, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
		Text:       "Reply with exactly the word OK",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if got.Outcome != host.PromptOutcomeDelivered {
		t.Fatalf("Outcome = %q, want delivered", got.Outcome)
	}
	if got.Acknowledged {
		t.Fatal("Acknowledged true: Herdr wait is condition evidence, not acknowledgment (notes/19 §3)")
	}
	if got.HostCode != CodeAgentPrompted {
		t.Fatalf("HostCode = %q, want %q", got.HostCode, CodeAgentPrompted)
	}
	if f.callCount("agent.prompt") != 1 {
		t.Fatalf("agent.prompt calls = %d, want 1", f.callCount("agent.prompt"))
	}
	sent := f.lastAgentPromptParams()
	if sent.Target != "duo-probe" || sent.Text != "Reply with exactly the word OK" {
		t.Fatalf("agent.prompt params = %+v", sent)
	}
}

func TestDeliverPromptBusyIsUnknownEffectAndNotRetried(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.addAgent("duo-probe", pane.paneID)
	f.setAgentPromptError(CodeAgentPaneBusy, "pane is not at an interactive prompt")
	h := testHost(t, f)

	got, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
		Text:       "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if got.Outcome != host.PromptOutcomeUnknownEffect {
		t.Fatalf("Outcome = %q, want unknown_effect: on agent.prompt a write may already have happened", got.Outcome)
	}
	if got.HostCode != CodeAgentPaneBusy {
		t.Fatalf("HostCode = %q, want %q", got.HostCode, CodeAgentPaneBusy)
	}
	if f.callCount("agent.prompt") != 1 {
		t.Fatalf("agent.prompt calls = %d, want 1 (no retry on the prompt path)", f.callCount("agent.prompt"))
	}
}

func TestDeliverPromptMapsProvenNoEffectCodes(t *testing.T) {
	codes := []string{CodeAgentNotReady, CodeAgentBlocked, CodeEmptyAgentPrompt, CodeAgentNotRunning, CodeAgentKindMismatch}
	for _, code := range codes {
		t.Run(code, func(t *testing.T) {
			f := newFakeHerdr(t)
			pane := f.addPane("w1")
			f.addAgent("duo-probe", pane.paneID)
			f.setAgentPromptError(code, "refused before input")
			h := testHost(t, f)

			got, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
				Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
				Text:       "hello",
			})
			if err != nil {
				t.Fatalf("DeliverPrompt: %v", err)
			}
			if got.Outcome != host.PromptOutcomeNoEffect {
				t.Fatalf("Outcome = %q, want no_effect for %s (notes/19 §3)", got.Outcome, code)
			}
			if got.HostCode != code {
				t.Fatalf("HostCode = %q, want %q", got.HostCode, code)
			}
			if f.callCount("agent.prompt") != 1 {
				t.Fatalf("retries on proven-no-effect code %s: %d calls", code, f.callCount("agent.prompt"))
			}
		})
	}
}

func TestDeliverPromptStalledIsUnknownEffectAndNotRetried(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.addAgent("duo-probe", pane.paneID)
	f.setAgentPromptError(CodeAgentPromptStalled, "no lifecycle change within five seconds")
	h := testHost(t, f)

	got, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
		Text:       "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if got.Outcome != host.PromptOutcomeUnknownEffect {
		t.Fatalf("Outcome = %q, want unknown_effect (notes/19 §3; decision-03 §7.1)", got.Outcome)
	}
	if f.callCount("agent.prompt") != 1 {
		t.Fatalf("agent.prompt retried after stall: %d calls", f.callCount("agent.prompt"))
	}
}

func TestDeliverPromptTimeoutIsUnknownEffect(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.addAgent("duo-probe", pane.paneID)
	f.setAgentPromptError(CodeTimeout, "wait timed out")
	h := testHost(t, f)

	got, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
		Text:       "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if got.Outcome != host.PromptOutcomeUnknownEffect {
		t.Fatalf("Outcome = %q, want unknown_effect", got.Outcome)
	}
}

func TestDeliverPromptDialFailureIsNoEffect(t *testing.T) {
	h, err := New(Config{
		IntegrationInstanceID: testInstanceID,
		SocketPath:            filepath.Join(shortTempDir(t), "absent.sock"),
		CallTimeout:           200 * time.Millisecond,
		ResolveProcessBirth:   fakeBirth,
		ResolvePaneEnviron:    cleanPaneEnviron,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: "w1:p1"},
		Text:       "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if got.Outcome != host.PromptOutcomeNoEffect {
		t.Fatalf("Outcome = %q, want no_effect: the pane accepted no input", got.Outcome)
	}
}

func TestDeliverPromptWriteThenDropIsUnknownEffect(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.addAgent("duo-probe", pane.paneID)
	f.setDropPrompt(true)
	h := testHost(t, f)

	got, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
		Text:       "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if got.Outcome != host.PromptOutcomeUnknownEffect {
		t.Fatalf("Outcome = %q, want unknown_effect after a possible write", got.Outcome)
	}
	if f.callCount("agent.prompt") != 1 {
		t.Fatalf("agent.prompt calls = %d, want 1", f.callCount("agent.prompt"))
	}
}

func TestDeliverPromptMissingAgentIsNoEffectWithoutPrompt(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	got, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
		Text:       "hello",
	})
	if err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	if got.Outcome != host.PromptOutcomeNoEffect {
		t.Fatalf("Outcome = %q, want no_effect (not an active named agent)", got.Outcome)
	}
	if got.HostCode != CodeAgentNotReady {
		t.Fatalf("HostCode = %q, want %q", got.HostCode, CodeAgentNotReady)
	}
	if f.callCount("agent.prompt") != 0 {
		t.Fatal("agent.prompt must not run when no named agent is registered")
	}
}

func TestDeliverPromptDoesNotUseWait(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.addAgent("duo-probe", pane.paneID)
	h := testHost(t, f)

	if _, err := h.DeliverPrompt(context.Background(), host.PromptRequest{
		Attachment: host.Attachment{IntegrationInstanceID: testInstanceID, PaneID: pane.paneID},
		Text:       "hello",
	}); err != nil {
		t.Fatalf("DeliverPrompt: %v", err)
	}
	// agentPromptParams has no wait field, so the wire object cannot carry
	// until-matching. The fake records the decoded params; an accidental
	// wait would not decode onto this struct, but the call still must be
	// the one admission request.
	if f.callCount("agent.prompt") != 1 {
		t.Fatalf("agent.prompt calls = %d, want 1", f.callCount("agent.prompt"))
	}
}
