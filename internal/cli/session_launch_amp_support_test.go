package cli

import (
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/launch"
)

// TestStage1SupportAcceptsHerdrAmp mirrors
// TestStage1SupportAcceptsHerdrDevin (session_launch_devin_support_test.go)
// for the Amp candidate.
func TestStage1SupportAcceptsHerdrAmp(t *testing.T) {
	v := stage1Support{}.Supported(launch.Tuple{
		HostKind:     herdr.AdapterID,
		HostVersion:  herdr.PinnedVersion,
		AgentRuntime: "amp",
	})
	if !v.OK {
		t.Fatalf("amp on pinned herdr refused: %+v", v)
	}
	if !strings.Contains(v.RecordDigest, "amp-exclusive-writer-0.0.1789142434-g4f3b4d") {
		t.Fatalf("digest = %q, want amp-exclusive-writer-0.0.1789142434-g4f3b4d", v.RecordDigest)
	}
}
