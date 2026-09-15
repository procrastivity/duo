package cli

import (
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/runtime/devin"
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
	if !strings.Contains(v.RecordDigest, devin.ConformanceRecordDigest) {
		t.Fatalf("digest = %q, want %s", v.RecordDigest, devin.ConformanceRecordDigest)
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
