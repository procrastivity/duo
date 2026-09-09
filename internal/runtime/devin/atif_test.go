package devin_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/runtime/devin"
)

func TestATIFPathUsesXDGDataHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	got, err := devin.ATIFPath("lr_test", "primary")
	if err != nil {
		t.Fatalf("ATIFPath: %v", err)
	}
	want := filepath.Join(root, "duo", "devin-atif", "lr_test", "primary.json")
	if got != want {
		t.Fatalf("ATIFPath = %q, want %q", got, want)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("ATIFPath %q is not absolute", got)
	}
	if !strings.HasSuffix(got, ".json") {
		t.Fatalf("ATIFPath %q does not end with .json", got)
	}
}

func TestATIFPathEmptyIDErrors(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := devin.ATIFPath("", "primary"); err == nil {
		t.Fatal("empty launch-resolution ID: want an error")
	}
}

func TestATIFPathEmptyLeafIsFileNamedID(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	got, err := devin.ATIFPath("lr_test", "")
	if err != nil {
		t.Fatalf("ATIFPath: %v", err)
	}
	want := filepath.Join(root, "duo", "devin-atif", "lr_test.json")
	if got != want {
		t.Fatalf("empty leaf ATIFPath = %q, want %q", got, want)
	}
}

func TestSessionIDFromExportReturnsMintedID(t *testing.T) {
	got, err := devin.SessionIDFromExport("testdata/atif-user-assistant.json")
	if err != nil {
		t.Fatalf("SessionIDFromExport: %v", err)
	}
	if got != "special-platinum" {
		t.Fatalf("SessionIDFromExport = %q, want %q", got, "special-platinum")
	}
}

func TestSessionIDFromExportMissingFileIsHonestMiss(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")
	got, err := devin.SessionIDFromExport(path)
	if err != nil {
		t.Fatalf("SessionIDFromExport: %v", err)
	}
	if got != "" {
		t.Fatalf("SessionIDFromExport = %q, want empty for missing file", got)
	}
}

func TestSessionIDFromExportNoSessionIDIsHonestMiss(t *testing.T) {
	got, err := devin.SessionIDFromExport("testdata/atif-no-session-id.json")
	if err != nil {
		t.Fatalf("SessionIDFromExport: %v", err)
	}
	if got != "" {
		t.Fatalf("SessionIDFromExport = %q, want empty for document without session_id", got)
	}
}
