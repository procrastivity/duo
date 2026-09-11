package cli

import (
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
)

// TestOpenKnownAgentRuntimeAmp mirrors TestOpenKnownAgentRuntimeDevin
// (session_runtime_devin_test.go) for the Amp candidate. Unlike Devin, Amp
// does not implement ConversationProvider or ConditionProvider yet — those
// belong to the lag-aware sibling matter (internal/runtime/amp/amp.go's
// package doc comment) and are not scaffolded here.
func TestOpenKnownAgentRuntimeAmp(t *testing.T) {
	if got := agentRuntimeIntegrationID("amp"); got != "amp" {
		t.Fatalf("agentRuntimeIntegrationID(amp) = %q, want amp", got)
	}

	rt, err := openKnownAgentRuntime("amp")
	if err != nil {
		t.Fatalf("openKnownAgentRuntime(amp): %v", err)
	}
	if _, ok := rt.(runtime.RuntimeCorrelator); !ok {
		t.Fatal("amp runtime does not implement RuntimeCorrelator")
	}
	if _, ok := rt.(runtime.RuntimePromptProvider); !ok {
		t.Fatal("amp runtime does not implement RuntimePromptProvider")
	}
}
