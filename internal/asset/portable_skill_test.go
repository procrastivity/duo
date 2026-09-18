package asset

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestPortableSkillEmbeddedFallbackMatchesAuthoredSource(t *testing.T) {
	authored, err := os.ReadFile(filepath.Join("..", "..", "skills", "duo-delegation-loop", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	oldExecutable := executable
	executable = func() (string, error) { return filepath.Join(t.TempDir(), "bin", "duo"), nil }
	t.Cleanup(func() { executable = oldExecutable })

	resolved, err := ReadDefault("skills/duo-delegation-loop/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source != SourceEmbedded {
		t.Fatalf("source = %s, want embedded", resolved.Source)
	}
	if !bytes.Equal(resolved.Bytes(), authored) {
		t.Fatal("embedded portable skill differs from authored source bytes")
	}
}

func TestPortableSkillInstalledShareTreeMatchesAuthoredSource(t *testing.T) {
	authored, err := os.ReadFile(filepath.Join("..", "..", "skills", "duo-delegation-loop", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	prefix := t.TempDir()
	shipped := filepath.Join(prefix, "share", "duo", "assets", "skills", "duo-delegation-loop", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(shipped), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(shipped, authored, 0o644); err != nil {
		t.Fatal(err)
	}
	oldExecutable := executable
	executable = func() (string, error) { return filepath.Join(prefix, "bin", "duo"), nil }
	t.Cleanup(func() { executable = oldExecutable })

	resolved, err := ReadDefault("skills/duo-delegation-loop/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Source != SourceDefault || resolved.Path != shipped {
		t.Fatalf("resolution = source %s path %q, want default %q", resolved.Source, resolved.Path, shipped)
	}
	if !bytes.Equal(resolved.Bytes(), authored) {
		t.Fatal("installed-share portable skill differs from authored source bytes")
	}
}
