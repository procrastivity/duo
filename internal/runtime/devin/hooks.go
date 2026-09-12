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

// DevinWorkspaceProjectionFormat is the projection Duo owns for the Devin
// CLI versions covered by the Tier C evidence: the narrow hook set plus the
// exec-posture allow-list, emitted together under one stamp. Devin reads the
// files at process start; it does not reload them when they change.
const DevinWorkspaceProjectionFormat = "devin-workspace.v1"

// DevinHookProjectionFormat is the hooks-only projection name earlier Duo
// builds stamped. readStamp still accepts it so a stamped, intact hooks-only
// projection regenerates forward instead of reading as unowned.
const DevinHookProjectionFormat = "devin-hooks.v1"

const (
	devinDirName       = ".devin"
	devinHooksFileName = "hooks.v1.json"
	devinHookScript    = "duo-hook.sh"
	devinPostureFile   = "config.local.json"
	devinStampFileName = ".duo-generated.json"
	devinScriptSource  = "internal/runtime/devin/hooks.go:duo-hook.sh"
	devinPostureSource = "internal/runtime/devin/posture.go:config.local.json"
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
	// ProjectionCurrent means the generated hook projection matches Duo's
	// expected ownership stamp and contents.
	ProjectionCurrent ProjectionStatus = "current"
	// ProjectionMissing means no generated hook projection is present.
	ProjectionMissing ProjectionStatus = "missing"
	// ProjectionStale means the generated projection comes from an older
	// Duo or hook-projection version.
	ProjectionStale ProjectionStatus = "stale"
	// ProjectionModified means a Duo-owned projection was changed in place.
	ProjectionModified ProjectionStatus = "modified"
	// ProjectionIncompatible means the projection shape is not supported.
	ProjectionIncompatible ProjectionStatus = "incompatible"
	// ProjectionUnownedConflict means existing files are not Duo-owned.
	ProjectionUnownedConflict ProjectionStatus = "unowned_conflict"
)

// ProjectionInspection is the safe projection status returned to doctor.
type ProjectionInspection struct {
	Status         ProjectionStatus `json:"status"`
	Workspace      string           `json:"workspace"`
	HooksPath      string           `json:"hooks_path"`
	PosturePath    string           `json:"posture_path"`
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

// generatedFile is one payload file the projection owns: its path relative
// to the workspace, the bytes to write, and the file mode.
type generatedFile struct {
	relPath string
	content []byte
	mode    os.FileMode
}

// generatedProjectionFiles renders the payload files the projection owns in
// deterministic order: the hook config, its reporter script, and the exec
// posture config. The stamp is not in this set — it records the set.
func generatedProjectionFiles(workspace string) ([]generatedFile, error) {
	devinDir := filepath.Join(workspace, devinDirName)
	script := []byte(renderHookScript())
	config, err := renderHookConfig(filepath.Join(devinDir, devinHookScript))
	if err != nil {
		return nil, err
	}
	posture, err := renderPostureConfig()
	if err != nil {
		return nil, err
	}
	return []generatedFile{
		{relPath: filepath.Join(devinDirName, devinHooksFileName), content: config, mode: 0o600},
		{relPath: filepath.Join(devinDirName, devinHookScript), content: script, mode: 0o700},
		{relPath: filepath.Join(devinDirName, devinPostureFile), content: posture, mode: 0o600},
	}, nil
}

// generatedProjectionNames is the set of relative paths the projection may
// legitimately own — the guard that keeps a crafted stamp from pointing
// verification at files outside the generated set.
func generatedProjectionNames() map[string]struct{} {
	return map[string]struct{}{
		filepath.Join(devinDirName, devinHooksFileName): {},
		filepath.Join(devinDirName, devinHookScript):    {},
		filepath.Join(devinDirName, devinPostureFile):   {},
	}
}

// MaterializeProjection writes Duo's Devin workspace projection — the narrow
// hook set and the exec-posture allow-list — into workspace under one
// ownership stamp. Existing Duo-owned files may be regenerated only when
// their stamp and content still agree; an intact older shape regenerates
// forward. An unowned or modified file is never overwritten.
func MaterializeProjection(workspace, installationID string) error {
	if workspace == "" {
		return errors.New("devin: workspace projection needs a workspace")
	}
	if installationID == "" {
		return errors.New("devin: workspace projection needs an installation ID")
	}
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return fmt.Errorf("devin: resolving projection workspace: %w", err)
	}
	devinDir := filepath.Join(workspace, devinDirName)
	if err := os.MkdirAll(devinDir, 0o700); err != nil {
		return fmt.Errorf("devin: creating projection directory: %w", err)
	}

	files, err := generatedProjectionFiles(workspace)
	if err != nil {
		return err
	}
	stampPath := filepath.Join(devinDir, devinStampFileName)
	if err := checkExistingProjection(workspace, stampPath, files); err != nil {
		return err
	}

	posture, err := renderPostureConfig()
	if err != nil {
		return err
	}
	stamp := projectionStamp{
		Schema:         projectionSchema,
		ProductVersion: projectionProduct,
		ManifestDigest: "",
		Projection:     DevinWorkspaceProjectionFormat,
		Target: projectionTarget{
			Harness:            "devin",
			TestedVersionRange: ">=3000.6.2 <3000.6.8",
		},
		InstallationID: installationID,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		SourceAssets: []projectionDigest{
			{Path: devinScriptSource, Digest: digestBytes([]byte(renderHookScript()))},
			{Path: devinPostureSource, Digest: digestBytes(posture)},
		},
	}
	for _, file := range files {
		stamp.Files = append(stamp.Files, projectionDigest{Path: file.relPath, Digest: digestBytes(file.content)})
	}
	stamp.ManifestDigest = manifestDigestForStamp(stamp)

	stampBytes, err := json.MarshalIndent(stamp, "", "  ")
	if err != nil {
		return fmt.Errorf("devin: encoding projection stamp: %w", err)
	}
	for _, file := range files {
		if err := writeProjectionFile(filepath.Join(workspace, file.relPath), file.content, file.mode); err != nil {
			return err
		}
	}
	if err := writeProjectionFile(stampPath, stampBytes, 0o600); err != nil {
		return err
	}
	return nil
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

// checkExistingProjection decides whether the projection may be written into
// a workspace that already has files. The policy: every file the write set
// would overwrite must be accounted for by a valid Duo stamp whose recorded
// digest still matches what is on disk — so an intact older shape (a
// hooks-only stamp) regenerates forward, while an unowned file (a
// hand-written config.local.json) or a modified owned file refuses.
func checkExistingProjection(workspace, stampPath string, files []generatedFile) error {
	seen := false
	present := map[string]bool{}
	watch := make([]string, 0, len(files)+1)
	for _, file := range files {
		watch = append(watch, filepath.Join(workspace, file.relPath))
	}
	for _, path := range append(watch, stampPath) {
		if _, err := os.Stat(path); err == nil {
			seen = true
			present[path] = true
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("devin: checking existing projection %s: %w", path, err)
		}
	}
	if !seen {
		return nil
	}
	stamp, err := readStamp(stampPath)
	if err != nil {
		return fmt.Errorf("devin: refusing to overwrite unowned projection: %w", err)
	}
	names := generatedProjectionNames()
	stamped := map[string]projectionDigest{}
	for _, file := range stamp.Files {
		if _, ok := names[file.Path]; !ok {
			return fmt.Errorf("devin: refusing to overwrite projection: stamp claims unowned path %s", file.Path)
		}
		stamped[file.Path] = file
	}
	for path, stampedFile := range stamped {
		full := filepath.Join(workspace, path)
		b, err := os.ReadFile(full)
		if err != nil {
			return fmt.Errorf("devin: refusing to overwrite modified projection %s: %w", path, err)
		}
		if digestBytes(b) != stampedFile.Digest {
			return fmt.Errorf("devin: refusing to overwrite modified projection %s", path)
		}
	}
	for _, file := range files {
		full := filepath.Join(workspace, file.relPath)
		if present[full] {
			if _, ok := stamped[file.relPath]; !ok {
				return fmt.Errorf("devin: refusing to overwrite unowned file %s", file.relPath)
			}
		}
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

// knownProjectionFormats are the stamp projection names Duo still owns: the
// current workspace format plus the hooks-only name earlier builds wrote.
var knownProjectionFormats = map[string]struct{}{
	DevinWorkspaceProjectionFormat: {},
	DevinHookProjectionFormat:      {},
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
	_, known := knownProjectionFormats[stamp.Projection]
	if stamp.Schema != projectionSchema || !known || stamp.Target.Harness != "devin" || stamp.InstallationID == "" {
		return projectionStamp{}, errors.New("invalid Duo Devin projection stamp")
	}
	return stamp, nil
}

// verifyProjectionFiles reports whether the files a stamp records are intact
// and whether the stamp describes the current write set. A stamp listing
// paths outside the generated set is incompatible; a stamped file missing or
// digest-mismatched is modified; an intact stamp covering a different shape
// (a hooks-only projection) or carrying a different source-asset/manifest
// digest is stale — owned and regenerable, just not current.
func verifyProjectionFiles(workspace string, stamp projectionStamp) ProjectionStatus {
	names := generatedProjectionNames()
	stamped := map[string]struct{}{}
	for _, file := range stamp.Files {
		if _, ok := names[file.Path]; !ok {
			return ProjectionIncompatible
		}
		stamped[file.Path] = struct{}{}
		b, err := os.ReadFile(filepath.Join(workspace, file.Path))
		if err != nil {
			return ProjectionModified
		}
		if digestBytes(b) != file.Digest {
			return ProjectionModified
		}
	}
	if len(stamped) != len(names) {
		return ProjectionStale
	}
	if stamp.Projection != DevinWorkspaceProjectionFormat {
		return ProjectionStale
	}
	wantAssets := map[string]string{
		devinScriptSource:  digestBytes([]byte(renderHookScript())),
		devinPostureSource: "",
	}
	posture, err := renderPostureConfig()
	if err == nil {
		wantAssets[devinPostureSource] = digestBytes(posture)
	}
	if len(stamp.SourceAssets) != len(wantAssets) {
		return ProjectionStale
	}
	for _, asset := range stamp.SourceAssets {
		want, ok := wantAssets[asset.Path]
		if !ok || want != asset.Digest {
			return ProjectionStale
		}
	}
	return ProjectionCurrent
}

// InspectProjection reports whether the projection is present and trusted,
// and whether an active Devin launch loaded an older installation. The latter
// is the important session-start-only distinction: freshly generated files
// can be current on disk while an already-running Devin session is stale.
func InspectProjection(workspace string, active []ProjectionActiveLaunch) ProjectionInspection {
	workspace, err := filepath.Abs(workspace)
	if err != nil {
		return ProjectionInspection{Status: ProjectionIncompatible, Workspace: workspace, Detail: err.Error()}
	}
	devinDir := filepath.Join(workspace, devinDirName)
	hooksPath := filepath.Join(devinDir, devinHooksFileName)
	stampPath := filepath.Join(devinDir, devinStampFileName)
	inspection := ProjectionInspection{
		Workspace:   workspace,
		HooksPath:   hooksPath,
		PosturePath: filepath.Join(devinDir, devinPostureFile),
		StampPath:   stampPath,
	}
	anyPresent := false
	for _, name := range []string{devinHooksFileName, devinHookScript, devinPostureFile, devinStampFileName} {
		if _, err := os.Stat(filepath.Join(devinDir, name)); err == nil {
			anyPresent = true
			break
		}
	}
	if !anyPresent {
		inspection.Status = ProjectionMissing
		inspection.Detail = "the Duo Devin workspace projection is not generated"
		return inspection
	}
	if _, err := os.Stat(stampPath); os.IsNotExist(err) {
		inspection.Status = ProjectionUnownedConflict
		inspection.Detail = "Devin projection files exist without a Duo ownership stamp"
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
	if status := verifyProjectionFiles(workspace, stamp); status != ProjectionCurrent {
		inspection.Status = status
		inspection.Detail = "generated Devin projection files do not match their ownership stamp"
		return inspection
	}
	if status := validateHookConfig(hooksPath, filepath.Join(devinDir, devinHookScript)); status != ProjectionCurrent {
		inspection.Status = status
		inspection.Detail = "the Devin hook configuration is not Duo's verified narrow set"
		return inspection
	}
	if status := validatePostureConfig(inspection.PosturePath); status != ProjectionCurrent {
		inspection.Status = status
		inspection.Detail = "the Devin exec posture is not Duo's owned allow-list"
		return inspection
	}
	if stamp.ManifestDigest != manifestDigestForStamp(stamp) {
		inspection.Status = ProjectionStale
		inspection.Detail = "the Devin projection was generated by an older projection format"
		return inspection
	}
	for _, launch := range active {
		if launch.InstallationID != stamp.InstallationID {
			inspection.Status = ProjectionStale
			inspection.Detail = "an active Devin session loaded an older projection; restart it to consume the regenerated files"
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
