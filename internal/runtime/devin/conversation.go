package devin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// atifDoc is the ATIF-v1.7 export envelope (notes/59). The file is one
// JSON document, not JSONL. ConversationProvider projects user and
// assistant text plus tool calls. ATIF has no tool-result steps; results are
// enriched separately from Devin's read-only session store.
type atifDoc struct {
	SchemaVersion string     `json:"schema_version"`
	SessionID     string     `json:"session_id"`
	Steps         []atifStep `json:"steps"`
}

type atifStep struct {
	StepID           int            `json:"step_id"`
	Timestamp        string         `json:"timestamp"`
	Source           string         `json:"source"` // system | user | agent
	Message          string         `json:"message"`
	ReasoningContent string         `json:"reasoning_content"`
	ToolCalls        []atifToolCall `json:"tool_calls"`
}

type atifToolCall struct {
	ToolCallID   string          `json:"tool_call_id"`
	FunctionName string          `json:"function_name"`
	Arguments    json.RawMessage `json:"arguments"`
}

// ReadConversation implements runtime.ConversationProvider.
//
// req.TranscriptID is an ATIF document path. This adapter does not
// resolve a session id to a path and does not spawn `devin --export`
// (that flag is turn-side-effect, not a dump; `--resume` opens the TUI).
// Empty TranscriptID is a hard error. Pagination matches Claude: After is
// a decimal offset into the projected turn slice.
func (r *Runtime) ReadConversation(ctx context.Context, req runtime.ConversationReadRequest) (runtime.ConversationBatch, error) {
	if req.TranscriptID == "" {
		return runtime.ConversationBatch{}, fmt.Errorf("devin runtime: opening transcript: missing transcript")
	}
	all, err := parseATIFTurns(req.TranscriptID)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}
	if store, ok := r.readStoreConversation(ctx, req.ExternalAgentSessionID); ok {
		all = mergeStoreResults(all, store)
	}

	offset, err := cursorOffset(req.After)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}
	if offset > len(all) {
		offset = len(all)
	}

	remaining := all[offset:]
	limit := req.Limit
	if limit <= 0 || limit > len(remaining) {
		limit = len(remaining)
	}
	batch := append([]runtime.ConversationTurn(nil), remaining[:limit]...)
	nextOffset := offset + limit

	return runtime.ConversationBatch{
		Turns:      batch,
		NextCursor: strconv.Itoa(nextOffset),
		Complete:   nextOffset >= len(all),
	}, nil
}

func cursorOffset(after string) (int, error) {
	if after == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(after)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("devin runtime: invalid cursor %q", after)
	}
	return n, nil
}

func parseATIFTurns(path string) ([]runtime.ConversationTurn, error) {
	doc, err := readATIF(path)
	if err != nil {
		return nil, err
	}
	var turns []runtime.ConversationTurn
	for _, step := range doc.Steps {
		turns = append(turns, projectATIFStep(step)...)
	}
	return turns, nil
}

func readATIF(path string) (atifDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return atifDoc{}, fmt.Errorf("devin runtime: opening transcript %s: %w", path, err)
	}
	var doc atifDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return atifDoc{}, fmt.Errorf("devin runtime: reading transcript %s: %w", path, err)
	}
	if doc.SchemaVersion == "" && doc.SessionID == "" {
		return atifDoc{}, fmt.Errorf("devin runtime: opening transcript %s: unreadable ATIF document", path)
	}
	return doc, nil
}

func projectATIFStep(step atifStep) []runtime.ConversationTurn {
	var role string
	switch step.Source {
	case "user":
		role = "user"
	case "agent":
		role = "assistant"
	default:
		return nil
	}
	at, _ := parseATIFTime(step.Timestamp)
	var turns []runtime.ConversationTurn
	if step.Message != "" {
		turns = append(turns, runtime.ConversationTurn{
			ID: strconv.Itoa(step.StepID), Role: role, Text: step.Message, At: at,
		})
	}
	if step.Source != "agent" {
		return turns
	}
	for i, call := range step.ToolCalls {
		turns = append(turns, runtime.ConversationTurn{
			ID:         fmt.Sprintf("%d.tool.%d", step.StepID, i),
			Role:       "assistant",
			At:         at,
			Kind:       "tool_call",
			ToolCallID: call.ToolCallID,
			ToolName:   call.FunctionName,
			Arguments:  append([]byte(nil), call.Arguments...),
		})
	}
	return turns
}

func parseATIFTime(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty timestamp")
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
