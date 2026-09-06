package devin_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/runtime/devin"
)

func TestMaterializeHooksWritesNarrowStampedProjection(t *testing.T) {
	workspace := t.TempDir()
	hooksPath, err := devin.MaterializeHooks(workspace, "lrr_first")
	if err != nil {
		t.Fatalf("MaterializeHooks: %v", err)
	}
	wantHooks := filepath.Join(workspace, ".devin", "hooks.v1.json")
	if hooksPath != wantHooks {
		t.Fatalf("hooks path = %q, want %q", hooksPath, wantHooks)
	}

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

	stampBytes, err := os.ReadFile(filepath.Join(workspace, ".devin", ".duo-generated.json"))
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
	}
	if err := json.Unmarshal(stampBytes, &stamp); err != nil {
		t.Fatalf("decode stamp: %v", err)
	}
	if stamp.Schema != "duo.projection-stamp/v1" || stamp.Projection != devin.DevinHookProjectionFormat || stamp.Target.Harness != "devin" || stamp.InstallationID != "lrr_first" {
		t.Fatalf("stamp = %+v, want Duo Devin ownership metadata", stamp)
	}

	for _, path := range []string{
		hooksPath,
		filepath.Join(workspace, ".devin", "duo-hook.sh"),
		filepath.Join(workspace, ".devin", ".duo-generated.json"),
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
	if _, err := devin.MaterializeHooks(workspace, "lrr_new"); err != nil {
		t.Fatalf("MaterializeHooks: %v", err)
	}
	inspection := devin.InspectProjection(workspace, []devin.ProjectionActiveLaunch{{InstallationID: "lrr_old"}})
	if inspection.Status != devin.ProjectionStale {
		t.Fatalf("status = %q, want stale; detail = %q", inspection.Status, inspection.Detail)
	}
	if inspection.Detail == "" {
		t.Fatal("stale projection has no diagnostic detail")
	}
}

func TestMaterializeHooksRefusesUnownedFile(t *testing.T) {
	workspace := t.TempDir()
	dir := filepath.Join(workspace, ".devin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks.v1.json"), []byte(`{"PermissionRequest":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := devin.MaterializeHooks(workspace, "lrr_first"); err == nil {
		t.Fatal("MaterializeHooks overwrote an unowned hooks file")
	}
	inspection := devin.InspectProjection(workspace, nil)
	if inspection.Status != devin.ProjectionUnownedConflict {
		t.Fatalf("status = %q, want unowned_conflict", inspection.Status)
	}
}
