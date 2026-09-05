package devin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/devin"
	_ "modernc.org/sqlite"
)

type storeTestNode struct {
	id      int64
	parent  *int64
	message map[string]any
}

type storeTestState struct {
	id     string
	call   string
	update string
}

func TestReadConversationMergesActiveStoreResults(t *testing.T) {
	const sessionID = "store-session"
	const callID = "call-active"

	atifPath := writeTestATIF(t, sessionID, []map[string]any{
		{"step_id": 1, "source": "user", "message": "use the tool"},
		{"step_id": 2, "source": "agent", "tool_calls": []any{
			map[string]any{
				"tool_call_id":  callID,
				"function_name": "atif-name",
				"arguments":     map[string]any{"from": "atif"},
			},
		}},
		{"step_id": 3, "source": "agent", "message": "finished"},
	})
	parent1 := int64(1)
	parent2 := int64(2)
	parent3 := int64(3)
	parent10 := int64(1)
	storePath := writeTestStore(t, sessionID, 4,
		[]storeTestNode{
			{id: 1, message: testStoreMessage("m1", "user", "use the tool", "", nil, nil)},
			{id: 2, parent: &parent1, message: testStoreMessage("m2", "assistant", "", "", []any{
				map[string]any{
					"id":        callID,
					"name":      "store-name",
					"arguments": map[string]any{"from": "store"},
				},
			}, nil)},
			{id: 3, parent: &parent2, message: testStoreMessage("m3", "tool", "fallback result", callID, nil, map[string]any{
				"created_at": "2026-09-05T12:00:02Z",
			})},
			{id: 4, parent: &parent3, message: testStoreMessage("m4", "assistant", "finished", "", nil, nil)},
			// This branch is not reachable from main_chain_id and must not leak.
			{id: 10, parent: &parent10, message: testStoreMessage("m10", "assistant", "", "", []any{
				map[string]any{"id": "call-abandoned", "name": "abandoned"},
			}, nil)},
			{id: 11, parent: func() *int64 { value := int64(10); return &value }(), message: testStoreMessage("m11", "tool", "abandoned result", "call-abandoned", nil, nil)},
		},
		[]storeTestState{{
			id:     callID,
			call:   `{"toolCallId":"call-active","rawInput":{"from":"store"},"_meta":{"cognition.ai/inferenceToolName":"store-name"}}`,
			update: `{"toolCallId":"call-active","status":"completed","content":[{"type":"content","content":{"type":"text","text":"canonical result"}}]}`,
		}, {id: "call-abandoned", update: `{"status":"completed","content":[{"type":"content","content":{"type":"text","text":"must not appear"}}]}`}},
	)

	r := devin.New("devin")
	r.SessionsDBPath = storePath
	batch, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: sessionID,
		TranscriptID:           atifPath,
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(batch.Turns) != 4 {
		t.Fatalf("turn count = %d, want 4: %+v", len(batch.Turns), batch.Turns)
	}
	if batch.Turns[0].Role != "user" || batch.Turns[1].Kind != "tool_call" || batch.Turns[3].Text != "finished" {
		t.Fatalf("merged order = %+v", batch.Turns)
	}
	call := batch.Turns[1]
	if call.ToolName != "atif-name" || string(call.Arguments) != `{"from":"atif"}` {
		t.Fatalf("ATIF call precedence was lost: %+v", call)
	}
	result := batch.Turns[2]
	if result.Kind != "tool_result" || result.ToolCallID != callID || result.Status != "completed" || result.Text != "canonical result" {
		t.Fatalf("merged result = %+v", result)
	}
	for _, turn := range batch.Turns {
		if turn.Text == "abandoned result" || turn.Text == "must not appear" {
			t.Fatalf("inactive branch leaked: %+v", batch.Turns)
		}
	}

	first, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: sessionID,
		TranscriptID:           atifPath,
		Limit:                  2,
	})
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if len(first.Turns) != 2 || first.Complete || first.NextCursor != "2" {
		t.Fatalf("first page = %+v, want call boundary and cursor 2", first)
	}
	second, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: sessionID,
		TranscriptID:           atifPath,
		After:                  first.NextCursor,
		Limit:                  2,
	})
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if len(second.Turns) != 2 || !second.Complete || second.Turns[0].Kind != "tool_result" {
		t.Fatalf("second page = %+v, want result then final text", second)
	}
}

func TestReadConversationUsesStoreUpdateWithoutToolNode(t *testing.T) {
	const sessionID = "store-update-only"
	const callID = "call-update-only"

	atifPath := writeTestATIF(t, sessionID, []map[string]any{
		{"step_id": 1, "source": "agent", "tool_calls": []any{
			map[string]any{"tool_call_id": callID, "function_name": "write", "arguments": map[string]any{}},
		}},
	})
	parent := int64(1)
	storePath := writeTestStore(t, sessionID, 2, []storeTestNode{
		{id: 1, message: testStoreMessage("m1", "assistant", "", "", []any{
			map[string]any{"id": callID, "name": "write"},
		}, nil)},
		{id: 2, parent: &parent, message: testStoreMessage("m2", "user", "", "", nil, nil)},
	}, []storeTestState{{
		id:     callID,
		update: `{"toolCallId":"call-update-only","status":"failed","content":[{"type":"content","content":{"type":"text","text":"rejected"}}]}`,
	}})

	r := devin.New("devin")
	r.SessionsDBPath = storePath
	batch, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: sessionID,
		TranscriptID:           atifPath,
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(batch.Turns) != 2 || batch.Turns[1].Kind != "tool_result" || batch.Turns[1].Status != "failed" || batch.Turns[1].Text != "rejected" {
		t.Fatalf("update-only result = %+v, want failed result", batch.Turns)
	}
}

func writeTestATIF(t *testing.T, sessionID string, steps []map[string]any) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.json")
	document := map[string]any{
		"schema_version": "ATIF-v1.7",
		"session_id":     sessionID,
		"steps":          steps,
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func testStoreMessage(id, role, content, toolCallID string, toolCalls []any, metadata map[string]any) map[string]any {
	message := map[string]any{
		"message_id": id,
		"role":       role,
		"content":    content,
	}
	if toolCallID != "" {
		message["tool_call_id"] = toolCallID
	}
	if toolCalls != nil {
		message["tool_calls"] = toolCalls
	}
	if metadata != nil {
		message["metadata"] = metadata
	}
	return message
}

func writeTestStore(t *testing.T, sessionID string, head int64, nodes []storeTestNode, states []storeTestState) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	for _, statement := range []string{
		`CREATE TABLE sessions (id TEXT PRIMARY KEY, main_chain_id INTEGER)`,
		`CREATE TABLE message_nodes (node_id INTEGER, parent_node_id INTEGER, session_id TEXT, chat_message TEXT)`,
		`CREATE TABLE tool_call_state (session_id TEXT, tool_call_id TEXT, tool_call_json TEXT, tool_call_update_json TEXT)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO sessions(id, main_chain_id) VALUES (?, ?)`, sessionID, head); err != nil {
		t.Fatal(err)
	}
	for _, node := range nodes {
		raw, err := json.Marshal(node.message)
		if err != nil {
			t.Fatal(err)
		}
		var parent any
		if node.parent != nil {
			parent = *node.parent
		}
		if _, err := db.Exec(`INSERT INTO message_nodes(node_id, parent_node_id, session_id, chat_message) VALUES (?, ?, ?, ?)`, node.id, parent, sessionID, raw); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range states {
		if _, err := db.Exec(`INSERT INTO tool_call_state(session_id, tool_call_id, tool_call_json, tool_call_update_json) VALUES (?, ?, ?, ?)`, sessionID, state.id, nullableJSON(state.call), nullableJSON(state.update)); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func nullableJSON(value string) any {
	if value == "" {
		return nil
	}
	return value
}
