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
	if !strings.Contains(v.RecordDigest, "notes63-amp-0.0.1788048110-g570348") {
		t.Fatalf("digest = %q, want notes63-amp-0.0.1788048110-g570348", v.RecordDigest)
	}
}
