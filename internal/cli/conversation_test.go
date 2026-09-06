package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/runtime"
)

func TestConversationItemsProjectToolCallAndResult(t *testing.T) {
	call := conversationItemFromTurn("call-record", "ses-1", runtime.ConversationTurn{
		ID:         "call-record",
		Role:       "assistant",
		Kind:       "tool_call",
		ToolCallID: "call-1",
		ToolName:   "write",
		Arguments:  json.RawMessage(`{"path":"probe.txt"}`),
	}, false)
	result := conversationItemFromTurn("result-record", "ses-1", runtime.ConversationTurn{
		ID:         "result-record",
		Role:       "tool",
		Kind:       "tool_result",
		ToolCallID: "call-1",
		Text:       "wrote it",
		Status:     "completed",
	}, true)

	if call.Blocks[0].Type != "tool_call" || call.Blocks[0].Content.Name != "write" || string(call.Blocks[0].Content.Arguments) != `{"path":"probe.txt"}` {
		t.Fatalf("call item = %+v", call)
	}
	if result.Blocks[0].Type != "tool_result" || result.Blocks[0].Content.ToolCallID != "call-1" || result.Blocks[0].Content.Status != "completed" || result.Blocks[0].Content.Text != "wrote it" {
		t.Fatalf("result item = %+v", result)
	}

	var out bytes.Buffer
	if err := renderConversationListText(&iostreams.Streams{Out: &out}, conversationListResult{
		SessionID: "ses-1",
		Items:     []conversationListItem{call, result},
	}); err != nil {
		t.Fatalf("render: %v", err)
	}
	text := out.String()
	for _, want := range []string{"call-record\ttool_call\tcall-1\twrite", "result-record\ttool_result\tcall-1\tcompleted\twrote it"} {
		if !strings.Contains(text, want) {
			t.Errorf("rendered text %q does not contain %q", text, want)
		}
	}
}
