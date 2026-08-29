package cli

import (
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/launch"
)

func TestStage1SupportAcceptsHerdrDevin(t *testing.T) {
	v := stage1Support{}.Supported(launch.Tuple{
		HostKind:     herdr.AdapterID,
		HostVersion:  herdr.PinnedVersion,
		AgentRuntime: "devin",
	})
	if !v.OK {
		t.Fatalf("devin on pinned herdr refused: %+v", v)
	}
	if !strings.Contains(v.RecordDigest, "notes59-devin-3000.6.7") {
		t.Fatalf("digest = %q, want notes59-devin-3000.6.7", v.RecordDigest)
	}
}

func TestStage1SupportStillRefusesUnknownRuntime(t *testing.T) {
	v := stage1Support{}.Supported(launch.Tuple{
		HostKind:     herdr.AdapterID,
		HostVersion:  herdr.PinnedVersion,
		AgentRuntime: "codex",
	})
	if v.OK {
		t.Fatalf("codex must still refuse: %+v", v)
	}
}

func TestStage1SupportStillAcceptsClaude(t *testing.T) {
	v := stage1Support{}.Supported(launch.Tuple{
		HostKind:     herdr.AdapterID,
		HostVersion:  herdr.PinnedVersion,
		AgentRuntime: "claude",
	})
	if !v.OK {
		t.Fatalf("claude regression: %+v", v)
	}
}
