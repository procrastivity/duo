package devin

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/procrastivity/duo/internal/runtime"
	_ "modernc.org/sqlite"
)

type storeConversation struct {
	calls       map[string]storeCall
	results     map[string]runtime.ConversationTurn
	resultOrder []string
}

type storeCall struct {
	name      string
	arguments []byte
}

type storeMessage struct {
	MessageID  string          `json:"message_id"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	ToolCallID string          `json:"tool_call_id"`
	ToolCalls  []storeToolCall `json:"tool_calls"`
	Metadata   struct {
		CreatedAt  string `json:"created_at"`
		Extensions struct {
			Result struct {
				Success *bool `json:"success"`
			} `json:"chisel/tool_result_meta"`
		} `json:"extensions"`
	} `json:"metadata"`
}

type storeToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Index     int             `json:"index"`
}

type acpCall struct {
	RawInput json.RawMessage `json:"rawInput"`
	Meta     struct {
		Name string `json:"cognition.ai/inferenceToolName"`
	} `json:"_meta"`
	Title string `json:"title"`
}

type acpUpdate struct {
	Status  string            `json:"status"`
	Content []json.RawMessage `json:"content"`
}

// readStoreConversation reads only the active Devin message chain. The
// database is Devin-owned, so this reader never creates, migrates, or writes
// its file. An unavailable or incompatible store is an optional enrichment
// failure: the ATIF projection remains usable.
func (r *Runtime) readStoreConversation(ctx context.Context, sessionID string) (storeConversation, bool) {
	if sessionID == "" {
		return storeConversation{}, false
	}

	path := r.SessionsDBPath
	if path == "" {
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return storeConversation{}, false
			}
			base = filepath.Join(home, ".local", "share")
		}
		path = filepath.Join(base, "devin", "cli", "sessions.db")
	}

	dsn := (&url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return storeConversation{}, false
	}
	defer func() { _ = db.Close() }()

	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return storeConversation{}, false
	}
	defer func() { _ = tx.Rollback() }()

	var head sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT main_chain_id FROM sessions WHERE id = ?`, sessionID).Scan(&head); err != nil || !head.Valid {
		return storeConversation{}, false
	}

	type node struct {
		parent sql.NullInt64
		raw    string
	}
	rows, err := tx.QueryContext(ctx,
		`SELECT node_id, parent_node_id, chat_message FROM message_nodes WHERE session_id = ?`, sessionID)
	if err != nil {
		return storeConversation{}, false
	}
	nodes := map[int64]node{}
	for rows.Next() {
		var id int64
		var n node
		if err := rows.Scan(&id, &n.parent, &n.raw); err != nil {
			_ = rows.Close()
			return storeConversation{}, false
		}
		nodes[id] = n
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return storeConversation{}, false
	}
	if err := rows.Close(); err != nil {
		return storeConversation{}, false
	}

	var chain []storeMessage
	seen := map[int64]bool{}
	for id := head.Int64; ; {
		if seen[id] {
			return storeConversation{}, false
		}
		seen[id] = true
		n, ok := nodes[id]
		if !ok {
			return storeConversation{}, false
		}
		var message storeMessage
		if err := json.Unmarshal([]byte(n.raw), &message); err == nil {
			chain = append(chain, message)
		}
		if !n.parent.Valid {
			break
		}
		id = n.parent.Int64
	}
	for left, right := 0, len(chain)-1; left < right; left, right = left+1, right-1 {
		chain[left], chain[right] = chain[right], chain[left]
	}

	out := storeConversation{
		calls:   make(map[string]storeCall),
		results: make(map[string]runtime.ConversationTurn),
	}
	for _, message := range chain {
		if message.Role == "assistant" {
			for _, call := range message.ToolCalls {
				if call.ID == "" {
					continue
				}
				out.calls[call.ID] = storeCall{
					name:      call.Name,
					arguments: append([]byte(nil), call.Arguments...),
				}
			}
		}
		if message.Role == "tool" && message.ToolCallID != "" {
			result := runtime.ConversationTurn{
				ID:         message.MessageID,
				Role:       "tool",
				Text:       message.Content,
				Kind:       "tool_result",
				ToolCallID: message.ToolCallID,
				Status:     "completed",
			}
			if message.Metadata.Extensions.Result.Success != nil && !*message.Metadata.Extensions.Result.Success {
				result.Status = "failed"
			}
			if at, err := parseATIFTime(message.Metadata.CreatedAt); err == nil {
				result.At = at
			}
			putStoreResult(&out, message.ToolCallID, result)
		}
	}

	if err := enrichToolState(ctx, tx, sessionID, &out); err != nil {
		// Individual malformed or unavailable tool-state rows do not make the
		// active message chain unusable.
		_ = err
	}
	if err := tx.Commit(); err != nil {
		return storeConversation{}, false
	}
	return out, true
}

func putStoreResult(out *storeConversation, toolCallID string, result runtime.ConversationTurn) {
	if _, exists := out.results[toolCallID]; exists {
		return
	}
	out.results[toolCallID] = result
	out.resultOrder = append(out.resultOrder, toolCallID)
}

func enrichToolState(ctx context.Context, tx *sql.Tx, sessionID string, out *storeConversation) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT tool_call_id, tool_call_json, tool_call_update_json FROM tool_call_state WHERE session_id = ?`, sessionID)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var id string
		var callJSON, updateJSON sql.NullString
		if err := rows.Scan(&id, &callJSON, &updateJSON); err != nil {
			return err
		}
		call, member := out.calls[id]
		if !member {
			continue
		}
		if callJSON.Valid {
			var ac acpCall
			if err := json.Unmarshal([]byte(callJSON.String), &ac); err == nil {
				if call.name == "" {
					call.name = ac.Meta.Name
				}
				if len(call.arguments) == 0 {
					call.arguments = append([]byte(nil), ac.RawInput...)
				}
				out.calls[id] = call
			}
		}
		if !updateJSON.Valid {
			continue
		}

		var update acpUpdate
		if err := json.Unmarshal([]byte(updateJSON.String), &update); err != nil {
			continue
		}
		if !isTerminalToolStatus(update.Status) {
			continue
		}
		result, exists := out.results[id]
		if !exists {
			result = runtime.ConversationTurn{
				ID:         "tool-result:" + id,
				Role:       "tool",
				Kind:       "tool_result",
				ToolCallID: id,
			}
			putStoreResult(out, id, result)
		}
		if update.Status != "" {
			result.Status = update.Status
		}
		if text := acpContentText(update.Content); text != "" {
			result.Text = text
		}
		out.results[id] = result
	}
	return rows.Err()
}

func isTerminalToolStatus(status string) bool {
	switch strings.ToLower(status) {
	case "", "pending", "running", "started", "in_progress", "in-progress":
		return false
	default:
		return true
	}
}

func acpContentText(items []json.RawMessage) string {
	for _, item := range items {
		var value any
		if json.Unmarshal(item, &value) != nil {
			continue
		}
		if text := jsonValueText(value); text != "" {
			return text
		}
	}
	return ""
}

func jsonValueText(value any) string {
	switch value := value.(type) {
	case map[string]any:
		if text, ok := value["text"].(string); ok && text != "" {
			return text
		}
		for _, key := range []string{"content", "resource"} {
			if nested, ok := value[key]; ok {
				if text := jsonValueText(nested); text != "" {
					return text
				}
			}
		}
	case []any:
		for _, nested := range value {
			if text := jsonValueText(nested); text != "" {
				return text
			}
		}
	}
	return ""
}

// mergeStoreResults keeps ATIF's message and call order, then inserts results
// in their active-chain order as soon as their corresponding call is visible.
// This handles parallel calls without admitting results from abandoned
// message-node branches.
func mergeStoreResults(atif []runtime.ConversationTurn, store storeConversation) []runtime.ConversationTurn {
	out := make([]runtime.ConversationTurn, 0, len(atif)+len(store.results))
	seenCalls := make(map[string]bool)
	emittedResults := make(map[string]bool)
	atifCallIDs := make(map[string]bool)
	for _, turn := range atif {
		if turn.Kind == "tool_call" {
			atifCallIDs[turn.ToolCallID] = true
		}
	}
	orderedResults := make([]string, 0, len(store.resultOrder))
	for _, id := range store.resultOrder {
		if atifCallIDs[id] {
			orderedResults = append(orderedResults, id)
		}
	}

	appendReadyResults := func() {
		for _, id := range orderedResults {
			if emittedResults[id] {
				continue
			}
			if !seenCalls[id] {
				break
			}
			result, ok := store.results[id]
			if !ok {
				continue
			}
			out = append(out, result)
			emittedResults[id] = true
		}
	}

	for _, turn := range atif {
		if turn.Kind == "tool_call" {
			if call, ok := store.calls[turn.ToolCallID]; ok {
				if turn.ToolName == "" {
					turn.ToolName = call.name
				}
				if len(turn.Arguments) == 0 {
					turn.Arguments = append([]byte(nil), call.arguments...)
				}
			}
			out = append(out, turn)
			seenCalls[turn.ToolCallID] = true
			appendReadyResults()
			continue
		}
		out = append(out, turn)
	}
	appendReadyResults()
	return out
}
