package cli

import (
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
)

func TestOpenKnownAgentRuntimeDevin(t *testing.T) {
	if got := agentRuntimeIntegrationID("devin"); got != "devin" {
		t.Fatalf("agentRuntimeIntegrationID(devin) = %q, want devin", got)
	}

	rt, err := openKnownAgentRuntime("devin")
	if err != nil {
		t.Fatalf("openKnownAgentRuntime(devin): %v", err)
	}
	if _, ok := rt.(runtime.RuntimeCorrelator); !ok {
		t.Fatal("devin runtime does not implement RuntimeCorrelator")
	}
	if _, ok := rt.(runtime.ConversationProvider); !ok {
		t.Fatal("devin runtime does not implement ConversationProvider")
	}
	if _, ok := rt.(runtime.ConditionProvider); !ok {
		t.Fatal("devin runtime does not implement ConditionProvider")
	}
	if _, ok := rt.(runtime.RuntimePromptProvider); !ok {
		t.Fatal("devin runtime does not implement RuntimePromptProvider")
	}
}
