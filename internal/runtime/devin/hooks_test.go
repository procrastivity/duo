package devin_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/runtime/devin"
)

func TestMaterializeProjectionWritesStampedWorkspaceProjection(t *testing.T) {
	workspace := t.TempDir()
	if err := devin.MaterializeProjection(workspace, "lrr_first"); err != nil {
		t.Fatalf("MaterializeProjection: %v", err)
	}

	devinDir := filepath.Join(workspace, ".devin")
	hooksPath := filepath.Join(devinDir, "hooks.v1.json")
	var config map[string]any
	b, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("read hooks: %v", err)
	}
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatalf("decode hooks: %v", err)
	}
	for _, event := range devin.DuoHookEvents {
		if _, ok := config[event]; !ok {
			t.Errorf("hooks missing %q", event)
		}
	}
	if _, ok := config["PermissionRequest"]; ok {
		t.Fatal("hooks unexpectedly include PermissionRequest")
	}

	posturePath := filepath.Join(devinDir, "config.local.json")
	pb, err := os.ReadFile(posturePath)
	if err != nil {
		t.Fatalf("read posture file: %v", err)
	}
	var posture struct {
		Permissions struct {
			Allow []string `json:"allow"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(pb, &posture); err != nil {
		t.Fatalf("decode posture file: %v", err)
	}
	if len(posture.Permissions.Allow) != len(devin.DuoExecAllowRules) {
		t.Fatalf("posture allow rules = %d, want %d", len(posture.Permissions.Allow), len(devin.DuoExecAllowRules))
	}
	for i, rule := range devin.DuoExecAllowRules {
		if posture.Permissions.Allow[i] != rule {
			t.Errorf("allow[%d] = %q, want %q", i, posture.Permissions.Allow[i], rule)
		}
	}

	stampBytes, err := os.ReadFile(filepath.Join(devinDir, ".duo-generated.json"))
	if err != nil {
		t.Fatalf("read stamp: %v", err)
	}
	var stamp struct {
		Schema     string `json:"schema"`
		Projection string `json:"projection_format"`
		Target     struct {
			Harness string `json:"harness"`
		} `json:"target"`
		InstallationID string `json:"installation_id"`
		Files          []struct {
			Path string `json:"path"`
		} `json:"files"`
		SourceAssets []struct {
			Path string `json:"path"`
		} `json:"source_assets"`
	}
	if err := json.Unmarshal(stampBytes, &stamp); err != nil {
		t.Fatalf("decode stamp: %v", err)
	}
	if stamp.Schema != "duo.projection-stamp/v1" || stamp.Projection != devin.DevinWorkspaceProjectionFormat || stamp.Target.Harness != "devin" || stamp.InstallationID != "lrr_first" {
		t.Fatalf("stamp = %+v, want Duo Devin ownership metadata", stamp)
	}
	if len(stamp.Files) != 3 {
		t.Fatalf("stamp files = %d, want 3 (hooks, script, posture)", len(stamp.Files))
	}
	if len(stamp.SourceAssets) != 2 {
		t.Fatalf("stamp source assets = %d, want 2 (script, posture)", len(stamp.SourceAssets))
	}

	for _, path := range []string{
		hooksPath,
		posturePath,
		filepath.Join(devinDir, "duo-hook.sh"),
		filepath.Join(devinDir, ".duo-generated.json"),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if info.Mode().Perm() != 0o600 && filepath.Base(path) != "duo-hook.sh" {
			t.Errorf("%s mode = %o, want 0600", path, info.Mode().Perm())
		}
		if filepath.Base(path) == "duo-hook.sh" && info.Mode().Perm() != 0o700 {
			t.Errorf("hook script mode = %o, want 0700", info.Mode().Perm())
		}
	}
}

func TestInspectProjectionReportsStaleActiveLaunch(t *testing.T) {
	workspace := t.TempDir()
	if err := devin.MaterializeProjection(workspace, "lrr_new"); err != nil {
		t.Fatalf("MaterializeProjection: %v", err)
	}
	inspection := devin.InspectProjection(workspace, []devin.ProjectionActiveLaunch{{InstallationID: "lrr_old"}})
	if inspection.Status != devin.ProjectionStale {
		t.Fatalf("status = %q, want stale; detail = %q", inspection.Status, inspection.Detail)
	}
	if inspection.Detail == "" {
		t.Fatal("stale projection has no diagnostic detail")
	}
}

func TestInspectProjectionCurrentAfterMaterialize(t *testing.T) {
	workspace := t.TempDir()
	if err := devin.MaterializeProjection(workspace, "lrr_first"); err != nil {
		t.Fatalf("MaterializeProjection: %v", err)
	}
	inspection := devin.InspectProjection(workspace, nil)
	if inspection.Status != devin.ProjectionCurrent {
		t.Fatalf("status = %q, want current; detail = %q", inspection.Status, inspection.Detail)
	}
	wantPosture := filepath.Join(workspace, ".devin", "config.local.json")
	if inspection.PosturePath != wantPosture {
		t.Fatalf("posture_path = %q, want %q", inspection.PosturePath, wantPosture)
	}
}

func TestMaterializeProjectionRefusesUnownedFile(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".devin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.v1.json"), []byte(`{"PermissionRequest":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := devin.MaterializeProjection(workspace, "lrr_first"); err == nil {
		t.Fatal("MaterializeProjection overwrote an unowned hooks file")
	}
	inspection := devin.InspectProjection(workspace, nil)
	if inspection.Status != devin.ProjectionUnownedConflict {
		t.Fatalf("status = %q, want unowned_conflict", inspection.Status)
	}
}

// A hand-written config.local.json is an unowned file — a user's personal
// overrides must never be clobbered by the projection.
func TestMaterializeProjectionRefusesUnownedPostureFile(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".devin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	mine := []byte(`{"permissions":{"allow":["Exec(rm)"]}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.local.json"), mine, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := devin.MaterializeProjection(workspace, "lrr_first"); err == nil {
		t.Fatal("MaterializeProjection overwrote an unowned config.local.json")
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(mine) {
		t.Fatal("unowned config.local.json content changed")
	}
	inspection := devin.InspectProjection(workspace, nil)
	if inspection.Status != devin.ProjectionUnownedConflict {
		t.Fatalf("status = %q, want unowned_conflict", inspection.Status)
	}
}

// A stamp can be self-consistent while describing the wrong posture — digests
// alone cannot see that, so InspectProjection re-validates the allow-list
// itself and reports incompatible.
func TestInspectProjectionRejectsWrongPostureContent(t *testing.T) {
	workspace := t.TempDir()
	if err := devin.MaterializeProjection(workspace, "lrr_first"); err != nil {
		t.Fatalf("MaterializeProjection: %v", err)
	}
	dir := filepath.Join(workspace, ".devin")
	wrong := []byte(`{"permissions":{"allow":["Exec(git)"]}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.local.json"), wrong, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := func(b []byte) string {
		sum := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	stampPath := filepath.Join(dir, ".duo-generated.json")
	stampBytes, err := os.ReadFile(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	var stamp map[string]any
	if err := json.Unmarshal(stampBytes, &stamp); err != nil {
		t.Fatal(err)
	}
	for _, f := range stamp["files"].([]any) {
		fm := f.(map[string]any)
		if fm["path"] == ".devin/config.local.json" {
			fm["digest"] = digest(wrong)
		}
	}
	stampBytes, err = json.Marshal(stamp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stampPath, stampBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	inspection := devin.InspectProjection(workspace, nil)
	if inspection.Status != devin.ProjectionIncompatible {
		t.Fatalf("status = %q, want incompatible", inspection.Status)
	}
}

func TestMaterializeProjectionRefusesModifiedPostureFile(t *testing.T) {
	workspace := t.TempDir()
	if err := devin.MaterializeProjection(workspace, "lrr_first"); err != nil {
		t.Fatalf("MaterializeProjection: %v", err)
	}
	posturePath := filepath.Join(workspace, ".devin", "config.local.json")
	if err := os.WriteFile(posturePath, []byte(`{"permissions":{"allow":["Exec(rm)"]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := devin.MaterializeProjection(workspace, "lrr_first"); err == nil {
		t.Fatal("MaterializeProjection overwrote a modified owned config.local.json")
	}
	inspection := devin.InspectProjection(workspace, nil)
	if inspection.Status != devin.ProjectionModified {
		t.Fatalf("status = %q, want modified", inspection.Status)
	}
}

// A hooks-only stamp from an older Duo build is intact owned state: the
// projection regenerates forward onto the new shape instead of refusing.
func TestMaterializeProjectionRegeneratesHooksOnlyStamp(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".devin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	hooks := []byte(`{"SessionStart":[]}`)
	script := []byte("#!/bin/sh\nexit 0\n")
	if err := os.WriteFile(filepath.Join(dir, "hooks.v1.json"), hooks, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "duo-hook.sh"), script, 0o700); err != nil {
		t.Fatal(err)
	}
	digest := func(b []byte) string {
		sum := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	oldStamp := map[string]any{
		"schema":            "duo.projection-stamp/v1",
		"product_version":   "stage1",
		"manifest_digest":   "sha256:old",
		"projection_format": "devin-hooks.v1",
		"target":            map[string]string{"harness": "devin", "tested_version_range": ">=3000.6.2 <3000.6.8"},
		"installation_id":   "lrr_old",
		"generated_at":      "2026-09-01T00:00:00Z",
		"source_assets":     []map[string]string{{"path": "internal/runtime/devin/hooks.go:duo-hook.sh", "digest": digest(script)}},
		"files": []map[string]string{
			{"path": ".devin/hooks.v1.json", "digest": digest(hooks)},
			{"path": ".devin/duo-hook.sh", "digest": digest(script)},
		},
	}
	stampBytes, err := json.Marshal(oldStamp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".duo-generated.json"), stampBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := devin.MaterializeProjection(workspace, "lrr_new"); err != nil {
		t.Fatalf("MaterializeProjection refused an intact hooks-only stamp: %v", err)
	}
	for _, name := range []string{"hooks.v1.json", "duo-hook.sh", "config.local.json", ".duo-generated.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s missing after regeneration: %v", name, err)
		}
	}
	inspection := devin.InspectProjection(workspace, nil)
	if inspection.Status != devin.ProjectionCurrent {
		t.Fatalf("status = %q after regeneration, want current; detail = %q", inspection.Status, inspection.Detail)
	}
	if inspection.InstallationID != "lrr_new" {
		t.Fatalf("installation_id = %q, want lrr_new", inspection.InstallationID)
	}
}

// A hooks-only stamp does not license overwriting a config.local.json the
// stamp never claimed — that file is unowned even when the stamp is intact.
func TestMaterializeProjectionRefusesPostureFileOutsideOldStamp(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".devin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	hooks := []byte(`{"SessionStart":[]}`)
	script := []byte("#!/bin/sh\nexit 0\n")
	for name, content := range map[string][]byte{"hooks.v1.json": hooks, "duo-hook.sh": script} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	digest := func(b []byte) string {
		sum := sha256.Sum256(b)
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	oldStamp := map[string]any{
		"schema":            "duo.projection-stamp/v1",
		"product_version":   "stage1",
		"manifest_digest":   "sha256:old",
		"projection_format": "devin-hooks.v1",
		"target":            map[string]string{"harness": "devin"},
		"installation_id":   "lrr_old",
		"generated_at":      "2026-09-01T00:00:00Z",
		"files": []map[string]string{
			{"path": ".devin/hooks.v1.json", "digest": digest(hooks)},
			{"path": ".devin/duo-hook.sh", "digest": digest(script)},
		},
	}
	stampBytes, err := json.Marshal(oldStamp)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".duo-generated.json"), stampBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	mine := []byte(`{"permissions":{"allow":["Exec(rm)"]}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.local.json"), mine, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := devin.MaterializeProjection(workspace, "lrr_new"); err == nil {
		t.Fatal("MaterializeProjection overwrote a config.local.json no stamp claimed")
	}
	b, err := os.ReadFile(filepath.Join(dir, "config.local.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != string(mine) {
		t.Fatal("unclaimed config.local.json content changed")
	}
}
