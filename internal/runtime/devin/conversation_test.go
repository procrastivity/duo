package devin_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/devin"
)

func testdataATIF(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadConversationProjectsUserAndAssistant(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	batch, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		TranscriptID: testdataATIF(t, "atif-user-assistant.json"),
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(batch.Turns) != 2 {
		t.Fatalf("turns = %d, want 2 (system/thinking/tools dropped): %+v", len(batch.Turns), batch.Turns)
	}
	if batch.Turns[0].Role != "user" || batch.Turns[0].Text != "Reply with exactly: LOOP-C-OK" {
		t.Fatalf("user turn: %+v", batch.Turns[0])
	}
	if batch.Turns[1].Role != "assistant" || batch.Turns[1].Text != "LOOP-C-OK" {
		t.Fatalf("assistant turn: %+v", batch.Turns[1])
	}
	if batch.Turns[1].Text == "thinking must not become a turn" {
		t.Fatal("reasoning_content leaked as assistant text")
	}
	for _, turn := range batch.Turns {
		if turn.Text == "system prompt must not become a turn" {
			t.Fatal("system step leaked as a turn")
		}
		if turn.Text == "thinking must not become a turn" {
			t.Fatal("thinking leaked as a turn")
		}
	}
	if !batch.Complete {
		t.Fatal("want Complete true on a full read")
	}
}

func TestReadConversationEmptyTranscriptIDErrors(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	_, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{})
	if err == nil {
		t.Fatal("empty TranscriptID: want an error")
	}
	if !strings.Contains(err.Error(), "missing transcript") && !strings.Contains(err.Error(), "opening transcript") {
		t.Fatalf("error %q: want missing transcript or opening transcript", err)
	}
}

func TestReadConversationMissingFileErrors(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	_, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		TranscriptID: filepath.Join(t.TempDir(), "no-such-atif.json"),
	})
	if err == nil {
		t.Fatal("missing file: want an error")
	}
	if !strings.Contains(err.Error(), "opening transcript") {
		t.Fatalf("error %q: want opening transcript", err)
	}
}

func TestReadConversationPaginates(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	path := testdataATIF(t, "atif-user-assistant.json")
	first, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		TranscriptID: path,
		Limit:        1,
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(first.Turns) != 1 || first.Turns[0].Role != "user" {
		t.Fatalf("first page: %+v", first.Turns)
	}
	if first.Complete {
		t.Fatal("first page of 1: want Complete false")
	}
	second, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		TranscriptID: path,
		After:        first.NextCursor,
		Limit:        1,
	})
	if err != nil {
		t.Fatalf("ReadConversation after: %v", err)
	}
	if len(second.Turns) != 1 || second.Turns[0].Role != "assistant" {
		t.Fatalf("second page: %+v", second.Turns)
	}
	if !second.Complete {
		t.Fatal("second page: want Complete true")
	}
}

func TestReadConversationUnreadableDocumentErrors(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	path := filepath.Join(t.TempDir(), "empty.json")
	if err := os.WriteFile(path, []byte(`{"unrelated": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{TranscriptID: path})
	if err == nil {
		t.Fatal("unreadable ATIF: want an error")
	}
}
