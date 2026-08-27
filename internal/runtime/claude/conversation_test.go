package claude_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/claude"
)

func newConversationRuntime(t *testing.T) *claude.Runtime {
	t.Helper()
	return newTestRuntime(t, "duo-reporter-secret")
}

// TestReadConversationParsesEveryFixture is the spec's "every fixture
// parses" requirement: no ported or constructed testdata/claude/*.jsonl
// file should make ReadConversation return an error.
func TestReadConversationParsesEveryFixture(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "claude", "*.jsonl"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(files) == 0 {
		t.Fatalf("no fixtures found under testdata/claude")
	}

	r := newConversationRuntime(t)
	ctx := context.Background()
	for _, f := range files {
		f := f
		t.Run(filepath.Base(f), func(t *testing.T) {
			if _, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{TranscriptID: f}); err != nil {
				t.Fatalf("ReadConversation(%s): %v", f, err)
			}
		})
	}
}

// TestReadConversationPrintToolUseResumeQuirks exercises the ported
// adapter_claude.go quirks against a real captured session
// (print-tool-use-resume.jsonl): assistant entries split one content
// block per JSONL line sharing message.id, a user tool_result entry with
// no text block, and thinking/tool_use blocks — none of which should
// surface as a ConversationTurn, only the four human/assistant text
// turns should.
func TestReadConversationPrintToolUseResumeQuirks(t *testing.T) {
	r := newConversationRuntime(t)
	ctx := context.Background()

	batch, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		TranscriptID: filepath.Join("testdata", "claude", "print-tool-use-resume.jsonl"),
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if !batch.Complete {
		t.Fatalf("expected Complete true reading the whole file in one batch")
	}

	want := []struct {
		role, text string
	}{
		{"user", "Run this bash command and tell me its output: echo turn-end-test"},
		{"assistant", "The output is:\n\n```\nturn-end-test\n```"},
		{"user", "What was the output of the bash command you ran earlier? Answer in one line."},
		{"assistant", "The output was `turn-end-test`."},
	}
	if len(batch.Turns) != len(want) {
		t.Fatalf("got %d turns, want %d: %+v", len(batch.Turns), len(want), batch.Turns)
	}
	for i, w := range want {
		got := batch.Turns[i]
		if got.Role != w.role || got.Text != w.text {
			t.Fatalf("turn %d = {Role:%q Text:%q}, want {Role:%q Text:%q}", i, got.Role, got.Text, w.role, w.text)
		}
		if got.ID == "" {
			t.Fatalf("turn %d has an empty ID", i)
		}
		if got.At.IsZero() {
			t.Fatalf("turn %d has a zero timestamp", i)
		}
	}
}

// TestReadConversationDropsSubagentSidechain proves the isSidechain
// filter: every line in this fixture carries isSidechain: true, so
// reading it must yield zero turns even though it is full of ordinary
// user/assistant text entries.
func TestReadConversationDropsSubagentSidechain(t *testing.T) {
	r := newConversationRuntime(t)
	ctx := context.Background()

	batch, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		TranscriptID: filepath.Join("testdata", "claude", "subagent-sidechain-truncated.jsonl"),
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(batch.Turns) != 0 {
		t.Fatalf("got %d turns from an all-sidechain fixture, want 0: %+v", len(batch.Turns), batch.Turns)
	}
}

// TestReadConversationDropsNewBookkeepingEntryTypes covers the 2.1.240
// churn notes/16 §1 found (atis-latch, bridge-session,
// total_tokens_reminder): none of the three should produce a turn or an
// error, since they fall through parseLine's type switch like any other
// bookkeeping entry. The fixture's two interrupt-shape user entries
// (notes/16 §2, "[Request interrupted by user]" /
// "[Request interrupted by user for tool use]") are ordinary human-typed
// user turns to this adapter — ConversationProvider does not special-case
// them; ConditionProvider treats those exact texts as turn-close.
func TestReadConversationDropsNewBookkeepingEntryTypes(t *testing.T) {
	r := newConversationRuntime(t)
	ctx := context.Background()

	batch, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		TranscriptID: filepath.Join("testdata", "claude", "new-entry-types.jsonl"),
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	want := []string{"[Request interrupted by user]", "[Request interrupted by user for tool use]"}
	if len(batch.Turns) != len(want) {
		t.Fatalf("got %d turns, want %d (atis-latch/bridge-session/total_tokens_reminder must not turn into turns): %+v", len(batch.Turns), len(want), batch.Turns)
	}
	for i, text := range want {
		if batch.Turns[i].Role != "user" || batch.Turns[i].Text != text {
			t.Fatalf("turn %d = %+v, want Role=user Text=%q", i, batch.Turns[i], text)
		}
	}
}

// TestReadConversationProjectsPeerInject expects peer-injected isMeta user
// lines (origin.kind:"peer") to project as peer-role turns alongside the
// assistant reply. Ordinary isMeta bookkeeping (meta-drop-1) stays dropped.
func TestReadConversationProjectsPeerInject(t *testing.T) {
	r := newConversationRuntime(t)
	ctx := context.Background()

	batch, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		TranscriptID: filepath.Join("testdata", "claude", "peer-inject.jsonl"),
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(batch.Turns) != 2 {
		t.Fatalf("got %d turns, want 2: %+v", len(batch.Turns), batch.Turns)
	}

	peer := batch.Turns[0]
	if peer.ID != "peer-user-1" {
		t.Fatalf("Turns[0].ID = %q, want peer-user-1", peer.ID)
	}
	if peer.Role != "peer" {
		t.Fatalf("Turns[0].Role = %q, want peer", peer.Role)
	}
	if peer.OriginKind != "peer" {
		t.Fatalf("Turns[0].OriginKind = %q, want peer", peer.OriginKind)
	}
	if !strings.Contains(peer.Text, "Another Claude session sent a message") || !strings.Contains(peer.Text, "Reply with the single word pong.") {
		t.Fatalf("Turns[0].Text = %q, want peer inject preamble and prompt", peer.Text)
	}

	asst := batch.Turns[1]
	if asst.ID != "peer-asst-1" {
		t.Fatalf("Turns[1].ID = %q, want peer-asst-1", asst.ID)
	}
	if asst.Role != "assistant" {
		t.Fatalf("Turns[1].Role = %q, want assistant", asst.Role)
	}
	if asst.OriginKind != "" {
		t.Fatalf("Turns[1].OriginKind = %q, want empty", asst.OriginKind)
	}
	if asst.Text != "pong" {
		t.Fatalf("Turns[1].Text = %q, want pong", asst.Text)
	}

	for _, turn := range batch.Turns {
		if turn.ID == "meta-drop-1" {
			t.Fatalf("meta-drop-1 must not appear as a turn: %+v", batch.Turns)
		}
	}
}

func TestReadConversationPaginatesWithCursor(t *testing.T) {
	r := newConversationRuntime(t)
	ctx := context.Background()
	path := filepath.Join("testdata", "claude", "print-tool-use-resume.jsonl")

	first, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{TranscriptID: path, Limit: 2})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(first.Turns) != 2 || first.Complete {
		t.Fatalf("first batch = %+v, want 2 turns and Complete=false", first)
	}

	second, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{TranscriptID: path, After: first.NextCursor, Limit: 2})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(second.Turns) != 2 || !second.Complete {
		t.Fatalf("second batch = %+v, want 2 turns and Complete=true", second)
	}
	if second.Turns[0].Text != "What was the output of the bash command you ran earlier? Answer in one line." {
		t.Fatalf("second.Turns[0] = %+v, unexpected", second.Turns[0])
	}
}

func TestReadConversationUnknownTranscriptIDErrors(t *testing.T) {
	r := newConversationRuntime(t)
	ctx := context.Background()

	if _, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{TranscriptID: filepath.Join("testdata", "claude", "does-not-exist.jsonl")}); err == nil {
		t.Fatalf("expected an error reading a nonexistent transcript path")
	}
}
