package devin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DevinHookProjectionFormat is the narrow hook projection Duo owns for the
// Devin CLI versions covered by the Tier C evidence. Devin reads the file at
// session start; it does not reload it when the file changes.
const DevinHookProjectionFormat = "devin-hooks.v1"

const (
	devinDirName       = ".devin"
	devinHooksFileName = "hooks.v1.json"
	devinHookScript    = "duo-hook.sh"
	devinStampFileName = ".duo-generated.json"
	projectionSchema   = "duo.projection-stamp/v1"
	projectionProduct  = "stage1"
)

// DuoHookEvents is the verified narrow set: lifecycle edges plus the tool
// edges that can distinguish a blocked tool from a successful one. Devin's
// PermissionRequest event is intentionally absent: the 3000.6.2 and
// 3000.6.7 probes never delivered it, so Duo declares that degradation
// rather than pretending permissions are observable.
var DuoHookEvents = []string{
	"PostToolUse",
	"PreToolUse",
	"SessionEnd",
	"SessionStart",
	"Stop",
	"UserPromptSubmit",
}

// ProjectionActiveLaunch is the small bit of authority state needed to
// diagnose Devin's session-start-only hook loading rule. InstallationID is
// the launch-resolution ID stamped into the generated projection.
type ProjectionActiveLaunch struct {
	InstallationID string
}

// ProjectionStatus is the doctor-facing state of a generated Devin hook
// projection. The values intentionally match the installation contract's
// drift vocabulary.
type ProjectionStatus string

const (
	ProjectionCurrent         ProjectionStatus = "current"
	ProjectionMissing         ProjectionStatus = "missing"
	ProjectionStale           ProjectionStatus = "stale"
	ProjectionModified        ProjectionStatus = "modified"
	ProjectionIncompatible    ProjectionStatus = "incompatible"
	ProjectionUnownedConflict ProjectionStatus = "unowned_conflict"
)

// ProjectionInspection is the safe projection status returned to doctor.
type ProjectionInspection struct {
	Status         ProjectionStatus `json:"status"`
	Workspace      string           `json:"workspace"`
	HooksPath      string           `json:"hooks_path"`
	StampPath      string           `json:"stamp_path"`
	InstallationID string           `json:"installation_id,omitempty"`
	ActiveLaunches int              `json:"active_launches,omitempty"`
	Detail         string           `json:"detail,omitempty"`
}

type projectionStamp struct {
	Schema         string             `json:"schema"`
	ProductVersion string             `json:"product_version"`
	ManifestDigest string             `json:"manifest_digest"`
	Projection     string             `json:"projection_format"`
	Target         projectionTarget   `json:"target"`
	InstallationID string             `json:"installation_id"`
	GeneratedAt    string             `json:"generated_at"`
	SourceAssets   []projectionDigest `json:"source_assets"`
	Files          []projectionDigest `json:"files"`
}

type projectionTarget struct {
	Harness            string `json:"harness"`
	TestedVersionRange string `json:"tested_version_range"`
}

type projectionDigest struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

type hookConfig map[string][]hookGroup

type hookGroup struct {
	Matcher *string       `json:"matcher,omitempty"`
	Hooks   []hookCommand `json:"hooks"`
}

type hookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Timeout int    `json:"timeout"`
}

// MaterializeHooks writes Duo's Devin hook projection into workspace and
// returns the generated hooks file path. Existing Duo-owned files may be
// regenerated only when their stamp and content still agree. An unowned or
// modified hooks file is never overwritten.
func MaterializeHooks(workspace, installationID string) (string, error) {
	if workspace == "" {
		return "", errors.New("devin: hook projection needs a workspace")
	}
	if installationID == "" {
		return "", errors.New("devin: hook projection needs an installation ID")
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("devin: resolving hook workspace: %w", err)
	}
	devinDir := filepath.Join(workspace, devinDirName)
	if err := os.MkdirAll(devinDir, 0o700); err != nil {
		return "", fmt.Errorf("devin: creating hook directory: %w", err)
	}

	hooksPath := filepath.Join(devinDir, devinHooksFileName)
	scriptPath := filepath.Join(devinDir, devinHookScript)
	stampPath := filepath.Join(devinDir, devinStampFileName)
	if err := checkExistingProjection(hooksPath, scriptPath, stampPath); err != nil {
		return "", err
	}

	script := []byte(renderHookScript())
	config, err := renderHookConfig(scriptPath)
	if err != nil {
		return "", err
	}
	stamp := projectionStamp{
		Schema:         projectionSchema,
		ProductVersion: projectionProduct,
		ManifestDigest: "",
		Projection:     DevinHookProjectionFormat,
		Target: projectionTarget{
			Harness:            "devin",
			TestedVersionRange: ">=3000.6.2 <3000.6.8",
		},
		InstallationID: installationID,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		SourceAssets: []projectionDigest{{
			Path:   "internal/runtime/devin/hooks.go:duo-hook.sh",
			Digest: digestBytes(script),
		}},
		Files: []projectionDigest{
			{Path: filepath.Join(devinDirName, devinHooksFileName), Digest: digestBytes(config)},
			{Path: filepath.Join(devinDirName, devinHookScript), Digest: digestBytes(script)},
		},
	}
	stamp.ManifestDigest = manifestDigestForStamp(stamp)

	stampBytes, err := json.MarshalIndent(stamp, "", "  ")
	if err != nil {
		return "", fmt.Errorf("devin: encoding hook projection stamp: %w", err)
	}
	if err := writeProjectionFile(scriptPath, script, 0o700); err != nil {
		return "", err
	}
	if err := writeProjectionFile(hooksPath, config, 0o600); err != nil {
		return "", err
	}
	if err := writeProjectionFile(stampPath, stampBytes, 0o600); err != nil {
		return "", err
	}
	return hooksPath, nil
}

func renderHookConfig(scriptPath string) ([]byte, error) {
	command := hookCommand{Type: "command", Command: scriptPath, Timeout: 5}
	config := hookConfig{}
	for _, event := range DuoHookEvents {
		group := hookGroup{Hooks: []hookCommand{command}}
		if event == "PreToolUse" || event == "PostToolUse" {
			matcher := ""
			group.Matcher = &matcher
		}
		config[event] = []hookGroup{group}
	}
	return json.MarshalIndent(config, "", "  ")
}

func renderHookScript() string {
	return `#!/bin/sh
# Generated by Duo; do not edit. This synchronous reporter preserves the
# hook payload for a later Duo observation consumer and never claims a
# permission decision. Devin's PermissionRequest event is intentionally not
# installed for the verified 3000.6.x projection.
set -eu
umask 077
hook_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
workspace=$(dirname -- "$hook_dir")
mkdir -p "$workspace/.duo"
cat >> "$workspace/.duo/devin-hooks.jsonl"
`
}

func checkExistingProjection(hooksPath, scriptPath, stampPath string) error {
	seen := false
	for _, path := range []string{hooksPath, scriptPath, stampPath} {
		if _, err := os.Stat(path); err == nil {
			seen = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("devin: checking existing hook projection %s: %w", path, err)
		}
	}
	if !seen {
		return nil
	}
	stamp, err := readStamp(stampPath)
	if err != nil {
		return fmt.Errorf("devin: refusing to overwrite unowned hook projection: %w", err)
	}
	if status := verifyProjectionFiles(hooksPath, stamp); status != ProjectionCurrent {
		return fmt.Errorf("devin: refusing to overwrite %s hook projection", status)
	}
	return nil
}

func writeProjectionFile(path string, data []byte, mode os.FileMode) error {
	if err := os.WriteFile(path, data, mode); err != nil {
		return fmt.Errorf("devin: writing hook projection %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("devin: setting hook projection permissions %s: %w", path, err)
	}
	return nil
}

func readStamp(path string) (projectionStamp, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return projectionStamp{}, err
	}
	var stamp projectionStamp
	if err := json.Unmarshal(b, &stamp); err != nil {
		return projectionStamp{}, err
	}
	if stamp.Schema != projectionSchema || stamp.Projection != DevinHookProjectionFormat || stamp.Target.Harness != "devin" || stamp.InstallationID == "" {
		return projectionStamp{}, errors.New("invalid Duo Devin projection stamp")
	}
	return stamp, nil
}

func verifyProjectionFiles(hooksPath string, stamp projectionStamp) ProjectionStatus {
	wantFiles := map[string]struct{}{
		filepath.Join(devinDirName, devinHooksFileName): {},
		filepath.Join(devinDirName, devinHookScript):    {},
	}
	for _, file := range stamp.Files {
		if _, ok := wantFiles[file.Path]; !ok {
			return ProjectionIncompatible
		}
		path := filepath.Join(filepath.Dir(filepath.Dir(hooksPath)), file.Path)
		b, err := os.ReadFile(path)
		if err != nil {
			return ProjectionModified
		}
		if digestBytes(b) != file.Digest {
			return ProjectionModified
		}
	}
	if len(stamp.Files) != 2 || !hasProjectionFile(stamp.Files, filepath.Join(devinDirName, devinHooksFileName)) || !hasProjectionFile(stamp.Files, filepath.Join(devinDirName, devinHookScript)) {
		return ProjectionIncompatible
	}
	if len(stamp.SourceAssets) != 1 || stamp.SourceAssets[0].Path != "internal/runtime/devin/hooks.go:duo-hook.sh" || stamp.SourceAssets[0].Digest != digestBytes([]byte(renderHookScript())) {
		return ProjectionStale
	}
	return ProjectionCurrent
}

func hasProjectionFile(files []projectionDigest, path string) bool {
	for _, file := range files {
		if file.Path == path {
			return true
		}
	}
	return false
}

// InspectProjection reports whether the projection is present and trusted,
// and whether an active Devin launch loaded an older installation. The latter
// is the important session-start-only distinction: a freshly generated file
// can be current on disk while an already-running Devin session is stale.
func InspectProjection(workspace string, active []ProjectionActiveLaunch) ProjectionInspection {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return ProjectionInspection{Status: ProjectionIncompatible, Workspace: workspace, Detail: err.Error()}
	}
	hooksPath := filepath.Join(workspace, devinDirName, devinHooksFileName)
	stampPath := filepath.Join(workspace, devinDirName, devinStampFileName)
	inspection := ProjectionInspection{Workspace: workspace, HooksPath: hooksPath, StampPath: stampPath}
	if _, err := os.Stat(hooksPath); os.IsNotExist(err) {
		inspection.Status = ProjectionMissing
		inspection.Detail = "Duo Devin hooks are not generated"
		return inspection
	}
	if _, err := os.Stat(stampPath); os.IsNotExist(err) {
		inspection.Status = ProjectionUnownedConflict
		inspection.Detail = "Devin hooks exist without a Duo ownership stamp"
		return inspection
	}
	stamp, err := readStamp(stampPath)
	if err != nil {
		inspection.Status = ProjectionIncompatible
		inspection.Detail = err.Error()
		return inspection
	}
	inspection.InstallationID = stamp.InstallationID
	inspection.ActiveLaunches = len(active)
	if status := verifyProjectionFiles(hooksPath, stamp); status != ProjectionCurrent {
		inspection.Status = status
		inspection.Detail = "generated Devin hook files do not match their ownership stamp"
		return inspection
	}
	if status := validateHookConfig(hooksPath, filepath.Join(workspace, devinDirName, devinHookScript)); status != ProjectionCurrent {
		inspection.Status = status
		inspection.Detail = "the Devin hook configuration is not Duo's verified narrow set"
		return inspection
	}
	if stamp.ManifestDigest != manifestDigestForStamp(stamp) {
		inspection.Status = ProjectionStale
		inspection.Detail = "the Devin hook projection was generated by an older projection format"
		return inspection
	}
	for _, launch := range active {
		if launch.InstallationID != stamp.InstallationID {
			inspection.Status = ProjectionStale
			inspection.Detail = "an active Devin session loaded an older hook projection; restart it to consume the regenerated file"
			return inspection
		}
	}
	inspection.Status = ProjectionCurrent
	return inspection
}

func validateHookConfig(hooksPath, scriptPath string) ProjectionStatus {
	b, err := os.ReadFile(hooksPath)
	if err != nil {
		return ProjectionModified
	}
	var config hookConfig
	if err := json.Unmarshal(b, &config); err != nil {
		return ProjectionIncompatible
	}
	if len(config) != len(DuoHookEvents) {
		return ProjectionIncompatible
	}
	wantEvents := make(map[string]struct{}, len(DuoHookEvents))
	for _, event := range DuoHookEvents {
		wantEvents[event] = struct{}{}
	}
	for event, groups := range config {
		if _, ok := wantEvents[event]; !ok || len(groups) != 1 || len(groups[0].Hooks) != 1 {
			return ProjectionIncompatible
		}
		hook := groups[0].Hooks[0]
		if hook.Type != "command" || hook.Command != scriptPath || hook.Timeout != 5 {
			return ProjectionIncompatible
		}
		if (event == "PreToolUse" || event == "PostToolUse") && (groups[0].Matcher == nil || *groups[0].Matcher != "") {
			return ProjectionIncompatible
		}
	}
	if _, forbidden := config["PermissionRequest"]; forbidden {
		return ProjectionIncompatible
	}
	return ProjectionCurrent
}

// manifestDigestForStamp keeps the stamp's manifest field deterministic while
// avoiding a self-referential digest. It commits to the generated hook file's
// bytes and the projection format, which is the installed product input that
// matters for this launch-owned projection.
func manifestDigestForStamp(stamp projectionStamp) string {
	return digestBytes([]byte(stamp.Projection + "\n" + stamp.Target.Harness + "\n" + strings.Join(sortedDigests(stamp.Files), "\n")))
}

func sortedDigests(files []projectionDigest) []string {
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.Path+"="+file.Digest)
	}
	sort.Strings(paths)
	return paths
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
