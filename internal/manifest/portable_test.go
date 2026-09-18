package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/surface"
)

func portableTestManifest(t *testing.T) Manifest {
	t.Helper()
	root := &cobra.Command{Use: "duo"}
	verb := &cobra.Command{Use: "noop", RunE: func(*cobra.Command, []string) error { return nil }}
	surface.Annotate(verb, surface.Plumbing)
	root.AddCommand(verb)
	m, err := Build(root, buildinfo.Info{Version: "v-test", Commit: "abc", Date: "2026-09-17T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func portableRoot(workspace string) string {
	return filepath.Join(workspace, filepath.FromSlash(PortableProjectionRoot))
}

func loadStamp(t *testing.T, workspace string) ProjectionStamp {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(portableRoot(workspace), ProjectionStampFile))
	if err != nil {
		t.Fatal(err)
	}
	var stamp ProjectionStamp
	if err := json.Unmarshal(b, &stamp); err != nil {
		t.Fatal(err)
	}
	return stamp
}

func saveStamp(t *testing.T, workspace string, stamp ProjectionStamp) {
	t.Helper()
	b, err := json.MarshalIndent(stamp, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	b = append(b, '\n')
	if err := os.WriteFile(filepath.Join(portableRoot(workspace), ProjectionStampFile), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func inspectState(t *testing.T, workspace string, m Manifest) ProjectionState {
	t.Helper()
	i, err := InspectPortableLaunchers(workspace, m)
	if err != nil {
		t.Fatal(err)
	}
	return i.State
}

func projectionErrorCode(t *testing.T, err error) string {
	t.Helper()
	var pe *ProjectionError
	if !errors.As(err, &pe) {
		t.Fatalf("error = %v, want ProjectionError", err)
	}
	return pe.Code
}

func TestPortableManifestTargetAndAssetIdentity(t *testing.T) {
	m := portableTestManifest(t)
	if len(m.HarnessTargets) != 1 {
		t.Fatalf("targets = %d, want 1", len(m.HarnessTargets))
	}
	target := m.HarnessTargets[0]
	if target.Name != "portable_launchers" || target.Status != "unverified" || target.Scope != "workspace" {
		t.Fatalf("target identity = %#v", target)
	}
	if target.DiscoveryRoot != ".agents/skills" || target.ProjectionRoot != PortableProjectionRoot || target.StampFile != ProjectionStampFile {
		t.Fatalf("target paths = %#v", target)
	}
	if !reflect.DeepEqual(target.Components, []string{"filesystem_skill"}) {
		t.Fatalf("components = %v", target.Components)
	}
	wantLaunchers := []Launcher{
		{Name: "amp", TestedVersions: []string{"0.0.1789675234-g2899fe"}},
		{Name: "opencode", TestedVersions: []string{"1.18.31"}},
		{Name: "codex", TestedVersions: []string{"0.154.0"}},
	}
	if !reflect.DeepEqual(target.Launchers, wantLaunchers) {
		t.Fatalf("launchers = %#v", target.Launchers)
	}
	authored, err := os.ReadFile(filepath.Join("..", "..", "skills", "duo-delegation-loop", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(authored)
	wantDigest := "sha256:" + hex.EncodeToString(sum[:])
	if target.Artifact != (Artifact{Name: "duo-delegation-loop", MediaType: "text/markdown", SourceAsset: PortableSkillAsset, OutputPath: PortableSkillFile, ContentDigest: wantDigest}) {
		t.Fatalf("artifact = %#v", target.Artifact)
	}
	found := false
	for _, a := range m.Assets {
		if a.Path == PortableSkillAsset {
			found = true
			if a.SHA256 != hex.EncodeToString(sum[:]) {
				t.Fatalf("asset digest = %s, want %x", a.SHA256, sum)
			}
		}
	}
	if !found {
		t.Fatal("canonical skill is absent from assets inventory")
	}
	second := portableTestManifest(t)
	if second.ManifestDigest != m.ManifestDigest {
		t.Fatalf("manifest digest changed: %s != %s", second.ManifestDigest, m.ManifestDigest)
	}
	withoutDigest := m
	withoutDigest.ManifestDigest = ""
	encoded, err := json.Marshal(withoutDigest)
	if err != nil {
		t.Fatal(err)
	}
	manifestSum := sha256.Sum256(encoded)
	if want := "sha256:" + hex.EncodeToString(manifestSum[:]); m.ManifestDigest != want {
		t.Fatalf("manifest digest = %s, independent digest = %s", m.ManifestDigest, want)
	}
}

func TestPortableFreshCurrentIdempotentAndByteIdentical(t *testing.T) {
	workspace := t.TempDir()
	overrideRoot := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", overrideRoot)
	override := filepath.Join(overrideRoot, "duo", filepath.FromSlash(PortableSkillAsset))
	if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(override, []byte("must not be installed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := portableTestManifest(t)
	if got := inspectState(t, workspace, m); got != StateMissing {
		t.Fatalf("fresh state = %s", got)
	}
	first, err := InstallPortableLaunchers(workspace, false, m)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Changed || first.State != StateCurrent || first.InstallationID == "" {
		t.Fatalf("first result = %#v", first)
	}
	authored, _ := os.ReadFile(filepath.Join("..", "..", "skills", "duo-delegation-loop", "SKILL.md"))
	rendered, _ := os.ReadFile(filepath.Join(portableRoot(workspace), PortableSkillFile))
	if !bytes.Equal(authored, rendered) {
		t.Fatal("rendered skill differs from authored bytes")
	}
	stampBefore, _ := os.ReadFile(filepath.Join(portableRoot(workspace), ProjectionStampFile))
	skillInfo, _ := os.Stat(filepath.Join(portableRoot(workspace), PortableSkillFile))
	stampInfo, _ := os.Stat(filepath.Join(portableRoot(workspace), ProjectionStampFile))
	if stampInfo.ModTime().Before(skillInfo.ModTime()) {
		t.Fatalf("stamp mtime %s precedes payload mtime %s; stamp was not staged last", stampInfo.ModTime(), skillInfo.ModTime())
	}
	second, err := InstallPortableLaunchers(workspace, false, m)
	if err != nil {
		t.Fatal(err)
	}
	stampAfter, _ := os.ReadFile(filepath.Join(portableRoot(workspace), ProjectionStampFile))
	if second.Changed || second.InstallationID != first.InstallationID || !bytes.Equal(stampBefore, stampAfter) {
		t.Fatalf("current install was not an exact no-op: %#v", second)
	}
}

func TestPortableStatesPrecedenceAndNoWriteConflicts(t *testing.T) {
	m := portableTestManifest(t)

	t.Run("modified", func(t *testing.T) {
		workspace := t.TempDir()
		_, _ = InstallPortableLaunchers(workspace, false, m)
		file := filepath.Join(portableRoot(workspace), PortableSkillFile)
		if err := os.WriteFile(file, []byte("user edit\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(file)
		if got := inspectState(t, workspace, m); got != StateModified {
			t.Fatalf("state = %s", got)
		}
		_, err := InstallPortableLaunchers(workspace, true, m)
		if code := projectionErrorCode(t, err); code != "projection.modified" {
			t.Fatalf("code = %s", code)
		}
		after, _ := os.ReadFile(file)
		if !bytes.Equal(before, after) {
			t.Fatal("modified file was overwritten")
		}
	})

	t.Run("incompatible_precedes_modified", func(t *testing.T) {
		workspace := t.TempDir()
		_, _ = InstallPortableLaunchers(workspace, false, m)
		file := filepath.Join(portableRoot(workspace), PortableSkillFile)
		_ = os.WriteFile(file, []byte("user edit\n"), 0o644)
		stampPath := filepath.Join(portableRoot(workspace), ProjectionStampFile)
		_ = os.WriteFile(stampPath, []byte("not-json\n"), 0o644)
		if got := inspectState(t, workspace, m); got != StateIncompatible {
			t.Fatalf("state = %s", got)
		}
		before, _ := os.ReadFile(file)
		_, err := InstallPortableLaunchers(workspace, true, m)
		if code := projectionErrorCode(t, err); code != "projection.incompatible" {
			t.Fatalf("code = %s", code)
		}
		after, _ := os.ReadFile(file)
		if !bytes.Equal(before, after) {
			t.Fatal("incompatible projection was changed")
		}
	})

	t.Run("unowned_file", func(t *testing.T) {
		workspace := t.TempDir()
		root := portableRoot(workspace)
		_ = os.MkdirAll(root, 0o755)
		file := filepath.Join(root, PortableSkillFile)
		_ = os.WriteFile(file, []byte("user file\n"), 0o644)
		if got := inspectState(t, workspace, m); got != StateUnownedConflict {
			t.Fatalf("state = %s", got)
		}
		_, err := InstallPortableLaunchers(workspace, false, m)
		if code := projectionErrorCode(t, err); code != "projection.user_file_conflict" {
			t.Fatalf("code = %s", code)
		}
		got, _ := os.ReadFile(file)
		if string(got) != "user file\n" {
			t.Fatal("unowned file was changed")
		}
	})

	t.Run("destination_directory_symlink", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		parent := filepath.Dir(portableRoot(workspace))
		_ = os.MkdirAll(parent, 0o755)
		if err := os.Symlink(outside, portableRoot(workspace)); err != nil {
			t.Fatal(err)
		}
		if got := inspectState(t, workspace, m); got != StateUnownedConflict {
			t.Fatalf("state = %s", got)
		}
		_, err := InstallPortableLaunchers(workspace, true, m)
		if code := projectionErrorCode(t, err); code != "projection.user_file_conflict" {
			t.Fatalf("code = %s", code)
		}
		entries, _ := os.ReadDir(outside)
		if len(entries) != 0 {
			t.Fatal("installer followed destination symlink")
		}
	})

	t.Run("destination_ancestor_symlink", func(t *testing.T) {
		workspace := t.TempDir()
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(workspace, ".agents")); err != nil {
			t.Fatal(err)
		}
		if got := inspectState(t, workspace, m); got != StateUnownedConflict {
			t.Fatalf("state = %s", got)
		}
		_, err := InstallPortableLaunchers(workspace, true, m)
		if code := projectionErrorCode(t, err); code != "projection.user_file_conflict" {
			t.Fatalf("code = %s", code)
		}
		entries, _ := os.ReadDir(outside)
		if len(entries) != 0 {
			t.Fatal("installer followed managed ancestor symlink")
		}
	})
}

func TestPortableStatePrecedence(t *testing.T) {
	m := portableTestManifest(t)

	t.Run("modified_before_unowned", func(t *testing.T) {
		workspace := t.TempDir()
		_, _ = InstallPortableLaunchers(workspace, false, m)
		stamp := loadStamp(t, workspace)
		stamp.Files = []DigestedPath{{Path: "OLD.txt", Digest: "sha256:" + string(bytes.Repeat([]byte("a"), 64))}}
		saveStamp(t, workspace, stamp)
		if err := os.WriteFile(filepath.Join(portableRoot(workspace), "OLD.txt"), []byte("modified\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := inspectState(t, workspace, m); got != StateModified {
			t.Fatalf("state = %s, want modified before unowned_conflict", got)
		}
	})

	t.Run("unowned_before_missing", func(t *testing.T) {
		workspace := t.TempDir()
		_, _ = InstallPortableLaunchers(workspace, false, m)
		stamp := loadStamp(t, workspace)
		stamp.Files = []DigestedPath{{Path: "ABSENT.txt", Digest: "sha256:" + string(bytes.Repeat([]byte("a"), 64))}}
		saveStamp(t, workspace, stamp)
		if got := inspectState(t, workspace, m); got != StateUnownedConflict {
			t.Fatalf("state = %s, want unowned_conflict before missing", got)
		}
	})

	t.Run("missing_before_stale", func(t *testing.T) {
		workspace := t.TempDir()
		_, _ = InstallPortableLaunchers(workspace, false, m)
		stamp := loadStamp(t, workspace)
		stamp.ManifestDigest = "sha256:" + string(bytes.Repeat([]byte("a"), 64))
		saveStamp(t, workspace, stamp)
		if err := os.Remove(filepath.Join(portableRoot(workspace), PortableSkillFile)); err != nil {
			t.Fatal(err)
		}
		if got := inspectState(t, workspace, m); got != StateMissing {
			t.Fatalf("state = %s, want missing before stale", got)
		}
	})
}

func TestPortablePlainVersusRepairAndPreservation(t *testing.T) {
	workspace := t.TempDir()
	m := portableTestManifest(t)
	first, err := InstallPortableLaunchers(workspace, false, m)
	if err != nil {
		t.Fatal(err)
	}
	root := portableRoot(workspace)
	unlisted := filepath.Join(root, "NOTES.txt")
	if err := os.WriteFile(unlisted, []byte("keep me\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, PortableSkillFile)); err != nil {
		t.Fatal(err)
	}
	stampBefore, _ := os.ReadFile(filepath.Join(root, ProjectionStampFile))
	if got := inspectState(t, workspace, m); got != StateMissing {
		t.Fatalf("missing-owned state = %s", got)
	}
	_, err = InstallPortableLaunchers(workspace, false, m)
	if code := projectionErrorCode(t, err); code != "invalid.precondition" {
		t.Fatalf("plain missing code = %s", code)
	}
	stampAfterPlain, _ := os.ReadFile(filepath.Join(root, ProjectionStampFile))
	if !bytes.Equal(stampBefore, stampAfterPlain) {
		t.Fatal("plain install changed partial owned output")
	}
	repaired, err := InstallPortableLaunchers(workspace, true, m)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.InstallationID != first.InstallationID || !repaired.Changed {
		t.Fatalf("missing repair result = %#v", repaired)
	}
	if got, _ := os.ReadFile(unlisted); string(got) != "keep me\n" {
		t.Fatal("repair removed or changed an unlisted file")
	}

	stamp := loadStamp(t, workspace)
	obsoleteData := []byte("old generated file\n")
	obsoleteSum := sha256.Sum256(obsoleteData)
	obsolete := filepath.Join(root, "OLD.txt")
	if err := os.WriteFile(obsolete, obsoleteData, 0o644); err != nil {
		t.Fatal(err)
	}
	stamp.Files = append(stamp.Files, DigestedPath{Path: "OLD.txt", Digest: "sha256:" + hex.EncodeToString(obsoleteSum[:])})
	saveStamp(t, workspace, stamp)
	if got := inspectState(t, workspace, m); got != StateStale {
		t.Fatalf("obsolete-file state = %s", got)
	}
	if _, err := InstallPortableLaunchers(workspace, true, m); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(obsolete); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("obsolete owned file remains: %v", err)
	}
	if got, _ := os.ReadFile(unlisted); string(got) != "keep me\n" {
		t.Fatal("obsolete-file repair removed an unlisted file")
	}

	stamp = loadStamp(t, workspace)
	stamp.ManifestDigest = "sha256:" + string(bytes.Repeat([]byte("a"), 64))
	saveStamp(t, workspace, stamp)
	if got := inspectState(t, workspace, m); got != StateStale {
		t.Fatalf("stale state = %s", got)
	}
	staleStamp := loadStamp(t, workspace)
	_, err = InstallPortableLaunchers(workspace, false, m)
	if code := projectionErrorCode(t, err); code != "invalid.precondition" {
		t.Fatalf("plain stale code = %s", code)
	}
	repaired, err = InstallPortableLaunchers(workspace, true, m)
	if err != nil {
		t.Fatal(err)
	}
	if repaired.InstallationID != staleStamp.InstallationID || inspectState(t, workspace, m) != StateCurrent {
		t.Fatalf("stale repair did not preserve installation id: %#v", repaired)
	}
}

func TestPortableRejectsUnsafeDuplicateAndSymlinkStampedPaths(t *testing.T) {
	m := portableTestManifest(t)
	tests := []struct {
		name   string
		mutate func(*ProjectionStamp)
	}{
		{name: "unsafe", mutate: func(s *ProjectionStamp) { s.Files[0].Path = "../escape" }},
		{name: "duplicate", mutate: func(s *ProjectionStamp) { s.Files = append(s.Files, s.Files[0]) }},
		{name: "stamp_claim", mutate: func(s *ProjectionStamp) { s.Files[0].Path = ProjectionStampFile }},
		{name: "source_stamp_claim", mutate: func(s *ProjectionStamp) { s.SourceAssets[0].Path = ProjectionStampFile }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			_, _ = InstallPortableLaunchers(workspace, false, m)
			stamp := loadStamp(t, workspace)
			tt.mutate(&stamp)
			saveStamp(t, workspace, stamp)
			if got := inspectState(t, workspace, m); got != StateIncompatible {
				t.Fatalf("state = %s", got)
			}
		})
	}

	t.Run("stamped_payload_symlink", func(t *testing.T) {
		workspace := t.TempDir()
		_, _ = InstallPortableLaunchers(workspace, false, m)
		file := filepath.Join(portableRoot(workspace), PortableSkillFile)
		if err := os.Remove(file); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join("..", "elsewhere"), file); err != nil {
			t.Fatal(err)
		}
		if got := inspectState(t, workspace, m); got != StateIncompatible {
			t.Fatalf("state = %s", got)
		}
	})
}
