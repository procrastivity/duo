package fake_test

import (
	"context"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
)

func TestFactoryDescriptorAndProbe(t *testing.T) {
	ctx := context.Background()
	f := runtimefake.Factory{IntegrationInstanceID: "integration-1"}

	d := f.Descriptor()
	if d.AdapterID == "" || d.Role == "" {
		t.Fatalf("descriptor missing AdapterID or Role: %+v", d)
	}

	probe, err := f.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	r, err := f.New(ctx, probe)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if r == nil {
		t.Fatalf("New returned nil runtime")
	}
}

func TestCorrelateRequiresExternalAgentSessionID(t *testing.T) {
	ctx := context.Background()
	r := runtimefake.New("integration-1")

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID: "integration-1",
		WorkingDirectory:      "/some/dir",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if evidence.Bound {
		t.Fatalf("expected Bound false: a working directory alone cannot bind a runtime instance")
	}

	evidence, err = r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  "integration-1",
		ExternalAgentSessionID: "external-1",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatalf("expected Bound true with an external agent-session ID present")
	}
}

func TestReadConversationPaginatesWithCursor(t *testing.T) {
	ctx := context.Background()
	r := runtimefake.New("integration-1")
	r.SeedTranscript("external-1",
		runtime.ConversationTurn{ID: "1", Role: "user", Text: "hi"},
		runtime.ConversationTurn{ID: "2", Role: "assistant", Text: "hello"},
		runtime.ConversationTurn{ID: "3", Role: "user", Text: "bye"},
	)

	first, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		ExternalAgentSessionID: "external-1",
		Limit:                  2,
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(first.Turns) != 2 || first.Complete {
		t.Fatalf("first batch = %+v, want 2 turns and Complete=false", first)
	}

	second, err := r.ReadConversation(ctx, runtime.ConversationReadRequest{
		ExternalAgentSessionID: "external-1",
		After:                  first.NextCursor,
		Limit:                  2,
	})
	if err != nil {
		t.Fatalf("ReadConversation: %v", err)
	}
	if len(second.Turns) != 1 || !second.Complete {
		t.Fatalf("second batch = %+v, want 1 turn and Complete=true", second)
	}
	if second.Turns[0].ID != "3" {
		t.Fatalf("second.Turns[0].ID = %s, want 3", second.Turns[0].ID)
	}
}
