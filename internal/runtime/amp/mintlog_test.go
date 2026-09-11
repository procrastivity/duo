package amp_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/runtime/amp"
)

func TestMintLogPathUsesXDGDataHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	got, err := amp.MintLogPath("lr_test", "primary")
	if err != nil {
		t.Fatalf("MintLogPath: %v", err)
	}
	want := filepath.Join(root, "duo", "amp-mint", "lr_test", "primary.jsonl")
	if got != want {
		t.Fatalf("MintLogPath = %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("MintLogPath %q is not absolute", got)
	}
	if !strings.HasSuffix(got, ".jsonl") {
		t.Fatalf("MintLogPath %q does not end with .jsonl", got)
	}
}

func TestMintLogPathEmptyLaunchResolutionIDErrors(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := amp.MintLogPath("", "primary"); err == nil {
		t.Fatal("empty launch-resolution ID: want an error")
	}
}

func TestMintLogPathEmptyLeafErrors(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := amp.MintLogPath("lr_test", ""); err == nil {
		t.Fatal("empty leaf: want an error")
	}
}

func TestThreadIDFromMintLogReturnsMintedThreadID(t *testing.T) {
	got, err := amp.ThreadIDFromMintLog("testdata/mint-log-full.jsonl")
	if err != nil {
		t.Fatalf("ThreadIDFromMintLog: %v", err)
	}
	if got != "T-loop-c-ok" {
		t.Fatalf("ThreadIDFromMintLog = %q, want %q", got, "T-loop-c-ok")
	}
}

func TestThreadIDFromMintLogMissingSessionIDIsHonestMiss(t *testing.T) {
	got, err := amp.ThreadIDFromMintLog("testdata/mint-log-missing-session-id.jsonl")
	if err != nil {
		t.Fatalf("ThreadIDFromMintLog: %v", err)
	}
	if got != "" {
		t.Fatalf("ThreadIDFromMintLog = %q, want empty for a log without session_id", got)
	}
}

func TestThreadIDFromMintLogMissingFileIsHonestMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	got, err := amp.ThreadIDFromMintLog(path)
	if err != nil {
		t.Fatalf("ThreadIDFromMintLog: %v", err)
	}
	if got != "" {
		t.Fatalf("ThreadIDFromMintLog = %q, want empty for a missing file", got)
	}
}

func TestThreadIDFromMintLogMissingResultIsMintIncomplete(t *testing.T) {
	got, err := amp.ThreadIDFromMintLog("testdata/mint-log-missing-result.jsonl")
	if got != "" {
		t.Fatalf("ThreadIDFromMintLog = %q, want empty on an incomplete mint", got)
	}
	if !errors.Is(err, amp.ErrMintIncomplete) {
		t.Fatalf("ThreadIDFromMintLog error = %v, want it to wrap amp.ErrMintIncomplete", err)
	}
}
