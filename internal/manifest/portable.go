package manifest

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/asset"
)

const (
	// PortableTargetName is the manifest and stamp identity for the shared
	// Amp, OpenCode, and Codex filesystem-skill projection.
	PortableTargetName = "portable_launchers"
	// PortableProjectionRoot is the workspace-relative managed directory.
	PortableProjectionRoot = ".agents/skills/duo-delegation-loop"
	// PortableProjectionFormat versions the installed skill contract.
	PortableProjectionFormat = "duo.skill/duo-delegation-loop/v1"
	// PortableSkillAsset is the canonical logical shipped-asset path.
	PortableSkillAsset = "skills/duo-delegation-loop/SKILL.md"
	// PortableSkillFile is the payload path relative to the projection root.
	PortableSkillFile = "SKILL.md"
	// ProjectionStampFile is the ownership record beside the projected skill.
	ProjectionStampFile   = ".duo-generated.json"
	projectionStampSchema = "duo.projection-stamp/v1"
	// LauncherEligibilityCapabilityEvidence admits recognized launchers from
	// current suite evidence rather than from historical tested versions.
	LauncherEligibilityCapabilityEvidence = "capability_evidence"
)

var portableLaunchers = []Launcher{
	{Name: "amp", TestedVersions: []string{"0.0.1789675234-g2899fe", "0.0.1789724374-g0d2ed0"}},
	{Name: "opencode", TestedVersions: []string{"1.18.31"}},
	{Name: "codex", TestedVersions: []string{"0.154.0"}},
}

// ProjectionState is the closed portable projection inspection result.
type ProjectionState string

const (
	// StateCurrent means the installed bytes and ownership stamp match the
	// current binary's portable projection.
	StateCurrent ProjectionState = "current"
	// StateMissing means an expected projection or owned file is absent.
	StateMissing ProjectionState = "missing"
	// StateStale means intact owned output differs from the current manifest.
	StateStale ProjectionState = "stale"
	// StateModified means an owned file no longer matches its stamped digest.
	StateModified ProjectionState = "modified"
	// StateIncompatible means the ownership record or filesystem shape is unsupported.
	StateIncompatible ProjectionState = "incompatible"
	// StateUnownedConflict means an expected destination exists without valid ownership.
	StateUnownedConflict ProjectionState = "unowned_conflict"
)

// DigestedPath is one slash-separated path owned or consumed by a stamp.
type DigestedPath struct {
	Path   string `json:"path"`
	Digest string `json:"digest"`
}

// ProjectionTarget identifies the portable target and its evidence pins.
type ProjectionTarget struct {
	Harness             string     `json:"harness"`
	TestedVersionRange  string     `json:"tested_version_range"`
	LauncherEligibility string     `json:"launcher_eligibility,omitempty"`
	Launchers           []Launcher `json:"launchers"`
}

// UnmarshalJSON permits the target-level extension fields allowed by the v1
// schema while Launcher itself keeps each launcher row closed.
func (t *ProjectionTarget) UnmarshalJSON(data []byte) error {
	type targetAlias ProjectionTarget
	var decoded targetAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*t = ProjectionTarget(decoded)
	return nil
}

// ProjectionStamp is the normative duo.projection-stamp/v1 ownership record.
type ProjectionStamp struct {
	Schema           string           `json:"schema"`
	ProductVersion   string           `json:"product_version"`
	ManifestDigest   string           `json:"manifest_digest"`
	ProjectionFormat string           `json:"projection_format"`
	Target           ProjectionTarget `json:"target"`
	Components       []string         `json:"components"`
	InstallationID   string           `json:"installation_id"`
	GeneratedAt      string           `json:"generated_at"`
	SourceAssets     []DigestedPath   `json:"source_assets"`
	Files            []DigestedPath   `json:"files"`
}

// PortableInspection is a read-only projection diagnosis.
type PortableInspection struct {
	Target         string          `json:"target"`
	State          ProjectionState `json:"state"`
	Root           string          `json:"root"`
	InstallationID string          `json:"installation_id"`
	FormatVersion  string          `json:"format_version"`
	ContentDigest  string          `json:"content_digest"`
	stamp          *ProjectionStamp
	rootPresent    bool
	stampPresent   bool
}

// InstallResult is the stable successful install result.
type InstallResult struct {
	Target         string          `json:"target"`
	State          ProjectionState `json:"state"`
	Changed        bool            `json:"changed"`
	Root           string          `json:"root"`
	InstallationID string          `json:"installation_id"`
	FormatVersion  string          `json:"format_version"`
	ContentDigest  string          `json:"content_digest"`
}

// ProjectionError carries the stable public code for an expected refusal.
type ProjectionError struct {
	Code    string
	Message string
}

func (e *ProjectionError) Error() string { return e.Message }

type portableSpec struct {
	manifest Manifest
	target   HarnessTarget
	payload  []byte
}

func portableSpecFor(m Manifest) (portableSpec, error) {
	if len(m.HarnessTargets) != 1 || m.HarnessTargets[0].Name != PortableTargetName {
		return portableSpec{}, fmt.Errorf("manifest: portable launcher target is unavailable")
	}
	r, err := asset.ReadDefault(PortableSkillAsset)
	if err != nil {
		return portableSpec{}, err
	}
	t := m.HarnessTargets[0]
	if got := "sha256:" + Checksum(r.Bytes()); got != t.Artifact.ContentDigest {
		return portableSpec{}, fmt.Errorf("manifest: portable skill digest %s does not match target %s", got, t.Artifact.ContentDigest)
	}
	return portableSpec{manifest: m, target: t, payload: r.Bytes()}, nil
}

// InspectPortableLaunchers classifies the selected workspace without writing.
func InspectPortableLaunchers(workspace string, m Manifest) (PortableInspection, error) {
	spec, err := portableSpecFor(m)
	if err != nil {
		return PortableInspection{}, err
	}
	workspace, err = validateWorkspace(workspace)
	if err != nil {
		return PortableInspection{}, err
	}
	root := filepath.Join(workspace, filepath.FromSlash(PortableProjectionRoot))
	base := PortableInspection{
		Target:        PortableTargetName,
		State:         StateMissing,
		Root:          root,
		FormatVersion: PortableProjectionFormat,
		ContentDigest: spec.target.Artifact.ContentDigest,
	}
	if conflict, err := managedParentConflict(workspace, filepath.Dir(root)); err != nil {
		return PortableInspection{}, err
	} else if conflict {
		base.State = StateUnownedConflict
		return base, nil
	}

	rootInfo, err := os.Lstat(root)
	if errors.Is(err, os.ErrNotExist) {
		return base, nil
	}
	if err != nil {
		return PortableInspection{}, fmt.Errorf("manifest: inspecting projection root: %w", err)
	}
	base.rootPresent = true
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		base.State = StateUnownedConflict
		return base, nil
	}

	stampPath := filepath.Join(root, ProjectionStampFile)
	stampInfo, err := os.Lstat(stampPath)
	if errors.Is(err, os.ErrNotExist) {
		if exists, err := pathExistsNoFollow(filepath.Join(root, PortableSkillFile)); err != nil {
			return PortableInspection{}, err
		} else if exists {
			base.State = StateUnownedConflict
		}
		return base, nil
	}
	if err != nil {
		return PortableInspection{}, fmt.Errorf("manifest: inspecting projection stamp: %w", err)
	}
	base.stampPresent = true
	if !stampInfo.Mode().IsRegular() {
		base.State = StateIncompatible
		return base, nil
	}

	stamp, err := readProjectionStamp(stampPath)
	if err != nil || validatePortableStamp(stamp) != nil {
		base.State = StateIncompatible
		return base, nil
	}
	base.stamp = &stamp
	base.InstallationID = stamp.InstallationID

	claimed := make(map[string]bool, len(stamp.Files))
	missing := false
	for _, file := range stamp.Files {
		claimed[file.Path] = true
		filePath := filepath.Join(root, filepath.FromSlash(file.Path))
		info, err := os.Lstat(filePath)
		if errors.Is(err, os.ErrNotExist) {
			missing = true
			continue
		}
		if err != nil {
			return PortableInspection{}, fmt.Errorf("manifest: inspecting owned file %q: %w", file.Path, err)
		}
		if !info.Mode().IsRegular() {
			base.State = StateIncompatible
			return base, nil
		}
		data, err := os.ReadFile(filePath)
		if err != nil {
			return PortableInspection{}, fmt.Errorf("manifest: reading owned file %q: %w", file.Path, err)
		}
		if "sha256:"+Checksum(data) != file.Digest {
			base.State = StateModified
			return base, nil
		}
	}

	expectedPath := filepath.Join(root, PortableSkillFile)
	if !claimed[PortableSkillFile] {
		if exists, err := pathExistsNoFollow(expectedPath); err != nil {
			return PortableInspection{}, err
		} else if exists {
			base.State = StateUnownedConflict
			return base, nil
		}
		missing = true
	}
	if missing {
		base.State = StateMissing
		return base, nil
	}

	want := expectedStamp(spec, stamp.InstallationID, stamp.GeneratedAt)
	if !stampCurrent(stamp, want) {
		base.State = StateStale
		return base, nil
	}
	base.State = StateCurrent
	return base, nil
}

// InstallPortableLaunchers performs the contract's non-destructive install or repair.
func InstallPortableLaunchers(workspace string, repair bool, m Manifest) (InstallResult, error) {
	spec, err := portableSpecFor(m)
	if err != nil {
		return InstallResult{}, err
	}
	inspection, err := InspectPortableLaunchers(workspace, m)
	if err != nil {
		return InstallResult{}, err
	}
	if inspection.State == StateCurrent {
		return resultFromInspection(inspection, false), nil
	}
	switch inspection.State {
	case StateModified:
		return InstallResult{}, &ProjectionError{Code: "projection.modified", Message: "The owned portable launcher skill was modified; move or restore it before retrying."}
	case StateIncompatible:
		return InstallResult{}, &ProjectionError{Code: "projection.incompatible", Message: "The portable launcher projection has an unknown or malformed ownership record; upgrade Duo or move it for manual review."}
	case StateUnownedConflict:
		return InstallResult{}, &ProjectionError{Code: "projection.user_file_conflict", Message: "Unowned content blocks the portable launcher projection; move or remove it explicitly before installing."}
	case StateMissing:
		if (inspection.stampPresent || (inspection.stamp != nil && inspection.rootPresent)) && !repair {
			return InstallResult{}, &ProjectionError{Code: "invalid.precondition", Message: "The owned portable launcher projection is incomplete; inspect it and rerun with --repair."}
		}
	case StateStale:
		if !repair {
			return InstallResult{}, &ProjectionError{Code: "invalid.precondition", Message: "The owned portable launcher projection is stale; inspect it and rerun with --repair."}
		}
	}

	workspace, err = validateWorkspace(workspace)
	if err != nil {
		return InstallResult{}, err
	}
	root := inspection.Root
	if err := ensureManagedParents(workspace, filepath.Dir(root)); err != nil {
		return InstallResult{}, err
	}
	if !inspection.rootPresent {
		if err := os.Mkdir(root, 0o755); err != nil {
			return InstallResult{}, fmt.Errorf("manifest: creating projection root: %w", err)
		}
	}

	installationID := inspection.InstallationID
	if installationID == "" {
		installationID, err = newInstallationID()
		if err != nil {
			return InstallResult{}, err
		}
	}
	stamp := expectedStamp(spec, installationID, time.Now().UTC().Format(time.RFC3339Nano))
	stampBytes, err := json.MarshalIndent(stamp, "", "  ")
	if err != nil {
		return InstallResult{}, fmt.Errorf("manifest: encoding projection stamp: %w", err)
	}
	stampBytes = append(stampBytes, '\n')

	stage, err := os.MkdirTemp(filepath.Dir(root), ".duo-portable-launchers-")
	if err != nil {
		return InstallResult{}, fmt.Errorf("manifest: creating projection staging directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(stage) }()
	if err := writeSynced(filepath.Join(stage, PortableSkillFile), spec.payload); err != nil {
		return InstallResult{}, err
	}
	if err := writeSynced(filepath.Join(stage, ProjectionStampFile), stampBytes); err != nil {
		return InstallResult{}, err
	}

	// Reinspect immediately before placement so a concurrent user edit cannot
	// turn the earlier ownership proof into overwrite permission.
	again, err := InspectPortableLaunchers(workspace, m)
	if err != nil {
		return InstallResult{}, err
	}
	if again.State != inspection.State || again.InstallationID != inspection.InstallationID {
		return InstallResult{}, &ProjectionError{Code: "invalid.precondition", Message: "The portable launcher projection changed during installation; inspect it and retry."}
	}
	if err := os.Rename(filepath.Join(stage, PortableSkillFile), filepath.Join(root, PortableSkillFile)); err != nil {
		return InstallResult{}, fmt.Errorf("manifest: placing portable skill: %w", err)
	}
	if inspection.stamp != nil {
		for _, old := range inspection.stamp.Files {
			if old.Path == PortableSkillFile {
				continue
			}
			if err := os.Remove(filepath.Join(root, filepath.FromSlash(old.Path))); err != nil && !errors.Is(err, os.ErrNotExist) {
				return InstallResult{}, fmt.Errorf("manifest: removing obsolete owned file %q: %w", old.Path, err)
			}
		}
	}
	// The ownership stamp is deliberately the final rename/commit marker.
	if err := os.Rename(filepath.Join(stage, ProjectionStampFile), filepath.Join(root, ProjectionStampFile)); err != nil {
		return InstallResult{}, fmt.Errorf("manifest: committing projection stamp: %w", err)
	}
	if err := syncDir(root); err != nil {
		return InstallResult{}, err
	}
	return InstallResult{
		Target: PortableTargetName, State: StateCurrent, Changed: true, Root: root,
		InstallationID: installationID, FormatVersion: PortableProjectionFormat,
		ContentDigest: spec.target.Artifact.ContentDigest,
	}, nil
}

func resultFromInspection(i PortableInspection, changed bool) InstallResult {
	return InstallResult{Target: i.Target, State: i.State, Changed: changed, Root: i.Root, InstallationID: i.InstallationID, FormatVersion: i.FormatVersion, ContentDigest: i.ContentDigest}
}

func expectedStamp(spec portableSpec, installationID, generatedAt string) ProjectionStamp {
	return ProjectionStamp{
		Schema: projectionStampSchema, ProductVersion: spec.manifest.Product.Version,
		ManifestDigest: spec.manifest.ManifestDigest, ProjectionFormat: PortableProjectionFormat,
		Target: ProjectionTarget{
			Harness: PortableTargetName, TestedVersionRange: testedVersionDisplay(portableLaunchers),
			LauncherEligibility: LauncherEligibilityCapabilityEvidence, Launchers: cloneLaunchers(portableLaunchers),
		},
		Components: []string{"filesystem_skill"}, InstallationID: installationID, GeneratedAt: generatedAt,
		SourceAssets: []DigestedPath{{Path: PortableSkillAsset, Digest: spec.target.Artifact.ContentDigest}},
		Files:        []DigestedPath{{Path: PortableSkillFile, Digest: spec.target.Artifact.ContentDigest}},
	}
}

func stampCurrent(got, want ProjectionStamp) bool {
	got.GeneratedAt, want.GeneratedAt = "", ""
	return got.Schema == want.Schema && got.ProjectionFormat == want.ProjectionFormat &&
		got.InstallationID == want.InstallationID && got.Target.Harness == want.Target.Harness &&
		slices.Equal(got.Components, want.Components) && slices.Equal(got.SourceAssets, want.SourceAssets) &&
		slices.Equal(got.Files, want.Files)
}

func testedVersionDisplay(launchers []Launcher) string {
	rows := make([]string, 0, len(launchers))
	for _, launcher := range launchers {
		rows = append(rows, launcher.Name+"="+strings.Join(launcher.TestedVersions, ","))
	}
	return strings.Join(rows, ";")
}

func readProjectionStamp(filename string) (ProjectionStamp, error) {
	f, err := os.Open(filename)
	if err != nil {
		return ProjectionStamp{}, err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(io.LimitReader(f, 1<<20))
	dec.DisallowUnknownFields()
	var stamp ProjectionStamp
	if err := dec.Decode(&stamp); err != nil {
		return ProjectionStamp{}, err
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return ProjectionStamp{}, fmt.Errorf("trailing JSON content")
	}
	return stamp, nil
}

func validatePortableStamp(stamp ProjectionStamp) error {
	if stamp.Schema != projectionStampSchema || stamp.ProjectionFormat != PortableProjectionFormat ||
		stamp.Target.Harness != PortableTargetName || stamp.ProductVersion == "" || stamp.InstallationID == "" ||
		stamp.GeneratedAt == "" || stamp.Target.TestedVersionRange == "" ||
		(stamp.Target.LauncherEligibility != "" && stamp.Target.LauncherEligibility != LauncherEligibilityCapabilityEvidence) {
		return fmt.Errorf("unsupported stamp identity")
	}
	if _, err := time.Parse(time.RFC3339, stamp.GeneratedAt); err != nil {
		return fmt.Errorf("invalid generated_at")
	}
	if !validDigest(stamp.ManifestDigest) || !slices.Equal(stamp.Components, []string{"filesystem_skill"}) || len(stamp.Target.Launchers) == 0 {
		return fmt.Errorf("invalid stamp metadata")
	}
	launcherNames := map[string]bool{}
	for _, launcher := range stamp.Target.Launchers {
		if !portableLauncherName(launcher.Name) || launcherNames[launcher.Name] || len(launcher.TestedVersions) == 0 {
			return fmt.Errorf("invalid launcher row")
		}
		launcherNames[launcher.Name] = true
		versions := map[string]bool{}
		for _, version := range launcher.TestedVersions {
			if version == "" || versions[version] {
				return fmt.Errorf("invalid launcher version")
			}
			versions[version] = true
		}
	}
	if len(stamp.SourceAssets) == 0 || len(stamp.Files) == 0 {
		return fmt.Errorf("empty stamp paths")
	}
	if err := validateDigestedPaths(stamp.SourceAssets, true); err != nil {
		return err
	}
	if err := validateDigestedPaths(stamp.Files, true); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, item := range append(slices.Clone(stamp.SourceAssets), stamp.Files...) {
		if seen[item.Path] {
			return fmt.Errorf("duplicate stamp path %q", item.Path)
		}
		seen[item.Path] = true
	}
	return nil
}

func portableLauncherName(name string) bool {
	switch name {
	case "amp", "opencode", "codex":
		return true
	default:
		return false
	}
}

func validateDigestedPaths(paths []DigestedPath, rejectStamp bool) error {
	seen := map[string]bool{}
	for _, item := range paths {
		if !safeSlashPath(item.Path) || !validDigest(item.Digest) || seen[item.Path] || (rejectStamp && item.Path == ProjectionStampFile) {
			return fmt.Errorf("unsafe or duplicate stamp path %q", item.Path)
		}
		seen[item.Path] = true
	}
	return nil
}

func safeSlashPath(p string) bool {
	return p != "" && p == path.Clean(p) && !path.IsAbs(p) && p != "." && !strings.Contains(p, "\\") &&
		p != ".." && !strings.HasPrefix(p, "../")
}

func validDigest(d string) bool {
	if len(d) != len("sha256:")+64 || !strings.HasPrefix(d, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(d, "sha256:"))
	return err == nil && d == strings.ToLower(d)
}

func validateWorkspace(workspace string) (string, error) {
	if workspace == "" {
		workspace = "."
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", fmt.Errorf("manifest: resolving workspace: %w", err)
	}
	abs = filepath.Clean(abs)
	info, err := os.Lstat(abs)
	if err != nil {
		return "", &ProjectionError{Code: "invalid.precondition", Message: fmt.Sprintf("Workspace %q must be an existing non-symlink directory.", abs)}
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return "", &ProjectionError{Code: "invalid.precondition", Message: fmt.Sprintf("Workspace %q must be an existing non-symlink directory.", abs)}
	}
	if err := rejectSymlinkAncestors(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func rejectSymlinkAncestors(filename string) error {
	volume := filepath.VolumeName(filename)
	rest := strings.TrimPrefix(filename, volume)
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(rest, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return fmt.Errorf("manifest: inspecting workspace ancestor %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return &ProjectionError{Code: "invalid.precondition", Message: fmt.Sprintf("Workspace ancestor %q must not be a symlink.", current)}
		}
	}
	return nil
}

func managedParentConflict(workspace, parent string) (bool, error) {
	rel, err := filepath.Rel(workspace, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, &ProjectionError{Code: "invalid.precondition", Message: "The managed projection path escapes the selected workspace."}
	}
	current := workspace
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("manifest: inspecting managed parent %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

func ensureManagedParents(workspace, parent string) error {
	rel, err := filepath.Rel(workspace, parent)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return &ProjectionError{Code: "invalid.precondition", Message: "The managed projection path escapes the selected workspace."}
	}
	current := workspace
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return fmt.Errorf("manifest: creating managed parent %q: %w", current, err)
			}
			continue
		}
		if err != nil {
			return fmt.Errorf("manifest: inspecting managed parent %q: %w", current, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return &ProjectionError{Code: "projection.user_file_conflict", Message: fmt.Sprintf("Managed destination ancestor %q is not a real directory.", current)}
		}
	}
	return nil
}

func pathExistsNoFollow(filename string) (bool, error) {
	_, err := os.Lstat(filename)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("manifest: inspecting %q: %w", filename, err)
	}
	return true, nil
}

func newInstallationID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("manifest: minting installation id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func writeSynced(filename string, data []byte) error {
	f, err := os.OpenFile(filename, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("manifest: staging %q: %w", filepath.Base(filename), err)
	}
	if _, err = io.Copy(f, bytes.NewReader(data)); err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return fmt.Errorf("manifest: staging %q: %w", filepath.Base(filename), err)
	}
	if closeErr != nil {
		return fmt.Errorf("manifest: closing staged %q: %w", filepath.Base(filename), closeErr)
	}
	return nil
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("manifest: opening projection directory for sync: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("manifest: syncing projection directory: %w", err)
	}
	return nil
}
