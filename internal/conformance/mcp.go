package conformance

import (
	"encoding/json"
	"fmt"
)

// mcpToolResult is the minimal shape of an MCP CallToolResult carrying a
// JSON envelope as its sole content item: {"content":[{"type":"text",
// "text":"<json>"}]}. This is the harness's own encoding choice, not a
// contract citation — Stage 0 has no MCP server yet
// (docs/conformance/decisions.md records why this shape and not
// "structuredContent"). It is exercised as a real wrap/unwrap, unlike the
// CLI and presentation envelope codecs, which are Stage 0 identity.
type mcpToolResult struct {
	Content []mcpContentItem `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

type mcpContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// EncodeMCP wraps canonical as a single-item MCP tool result. isError is
// set when the envelope carries a top-level "error" key, mirroring how a
// real MCP server would report the operation's outcome without changing
// the wrapped domain value.
func EncodeMCP(v Canonical) []byte {
	inner, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("conformance: EncodeMCP: marshal inner: %v", err))
	}
	_, isErr := v["error"]
	wrapper := mcpToolResult{
		Content: []mcpContentItem{{Type: "text", Text: string(inner)}},
		IsError: isErr,
	}
	data, err := json.Marshal(wrapper)
	if err != nil {
		panic(fmt.Sprintf("conformance: EncodeMCP: marshal wrapper: %v", err))
	}
	return data
}

// DecodeMCP unwraps an MCP tool result back to Canonical.
func DecodeMCP(data []byte) (Canonical, error) {
	var wrapper mcpToolResult
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return nil, fmt.Errorf("conformance: DecodeMCP: parsing wrapper: %w", err)
	}
	if len(wrapper.Content) != 1 {
		return nil, fmt.Errorf("conformance: DecodeMCP: expected exactly one content item, got %d", len(wrapper.Content))
	}
	if wrapper.Content[0].Type != "text" {
		return nil, fmt.Errorf("conformance: DecodeMCP: expected content type %q, got %q", "text", wrapper.Content[0].Type)
	}
	return decodeCanonicalObject([]byte(wrapper.Content[0].Text))
}

// mcpDispatchKeys names the fields an MCP tool call carries to select among
// a shared tool's sub-behaviors — the stream family for
// duo_subscription_open, the sub-action for duo_lease_manage — rather than
// to carry request data. registry's decisions.md documents both shares.
// They have no counterpart in semantic.input and are dropped, not compared.
var mcpDispatchKeys = []string{"stream", "operation"}

// DecodeMCPRequestArgs converts a projection-cases.json MCP arguments object
// to Canonical, dropping the shared-tool dispatch keys.
func DecodeMCPRequestArgs(args map[string]any) Canonical {
	out := make(Canonical, len(args))
	for k, v := range args {
		drop := false
		for _, dk := range mcpDispatchKeys {
			if k == dk {
				drop = true
				break
			}
		}
		if !drop {
			out[k] = v
		}
	}
	return out
}
