package portablelauncher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/procrastivity/duo/internal/manifest"
)

// Artifact identifies one immutable executable used by a suite run.
type Artifact struct {
	Name    string
	Path    string
	Version string
	Digest  string
	Commit  string
}

// Credential carries isolated provider credentials into a fixture.
type Credential struct {
	Present    bool
	SourceKind string
	Bytes      []byte
}

// SetupInput contains the pinned inputs required to prepare an isolated run.
type SetupInput struct {
	BaseDir               string
	Artifacts             map[string]Artifact
	HostProtocol          string
	HostSchemaDigest      string
	ProductManifest       manifest.Manifest
	ConfigBytes           []byte
	EffectiveConfigDigest string
	Credential            Credential
}

// Fixture identifies the isolated filesystem paths for one suite run.
type Fixture struct {
	Root      string
	Workspace string
	Capture   string
	Result    string
	cleaned   bool
}

// CheckSetupPrerequisites is deliberately pure: callers must run it before
// creating a run root or touching shared state.
func CheckSetupPrerequisites(in SetupInput) error {
	var p Problems
	if _, err := validatePrivateTempDir(in.BaseDir); err != nil {
		p.Add("setup base must be a private child of the system temporary directory: " + err.Error())
	}

	required := []string{"launcher", "duo", "host", "runtime"}
	for _, key := range required {
		artifact, ok := in.Artifacts[key]
		if !ok {
			p.Add("missing pinned artifact: " + key)
			continue
		}
		if err := validateArtifact(artifact); err != nil {
			p.Add(key + ": " + err.Error())
		}
	}
	if launcher, ok := in.Artifacts["launcher"]; ok {
		want, accepted := AcceptedLauncherPin(launcher.Name)
		if !accepted || launcher.Version != want.Version || launcher.Digest != want.Digest {
			p.Add("launcher: exact accepted version and executable digest required")
		}
	}
	if duo, ok := in.Artifacts["duo"]; ok && (duo.Name != "duo" || duo.Version == "" || duo.Commit != DuoSourceCommit) {
		p.Add("duo: build must identify the locked source commit")
	}
	if host, ok := in.Artifacts["host"]; ok {
		if host.Name != "herdr" || host.Version != "0.8.2" || in.HostProtocol != "herdr-socket-api/20" || in.HostSchemaDigest != HostSchemaDigest {
			p.Add("host: exact Herdr 0.8.2/protocol 20/schema prerequisite absent")
		}
	}
	if runtime, ok := in.Artifacts["runtime"]; ok && (runtime.Name != "pi" || runtime.Version != "0.83.0") {
		p.Add("runtime: exact Pi 0.83.0 prerequisite absent")
	}
	if len(in.ProductManifest.HarnessTargets) != 1 || in.ProductManifest.HarnessTargets[0].Name != manifest.PortableTargetName || in.ProductManifest.HarnessTargets[0].Artifact.ContentDigest != SkillContentDigest {
		p.Add("skill: product manifest does not carry the locked projection identity")
	}
	if len(in.ConfigBytes) == 0 || !digestPattern.MatchString(in.EffectiveConfigDigest) || Digest(in.ConfigBytes) != in.EffectiveConfigDigest {
		p.Add("config: deterministic bytes must match the effective digest")
	}
	if !in.Credential.Present || len(in.Credential.Bytes) == 0 || !oneOf(in.Credential.SourceKind, "ci_secret", "operator_copy", "environment") {
		p.Add("credential: isolated selected-provider credential prerequisite absent")
	}
	return p.Err()
}

func validateArtifact(a Artifact) error {
	if a.Name == "" || a.Version == "" || !digestPattern.MatchString(a.Digest) || a.Path == "" {
		return fmt.Errorf("identity is unpinned or incomplete")
	}
	info, err := os.Lstat(a.Path)
	if err != nil {
		return fmt.Errorf("artifact unavailable: %w", err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("artifact must be a regular file")
	}
	b, err := os.ReadFile(a.Path)
	if err != nil {
		return fmt.Errorf("read artifact: %w", err)
	}
	if Digest(b) != a.Digest {
		return fmt.Errorf("artifact digest mismatch")
	}
	return nil
}

// PrepareFixture validates all prerequisites and materializes an isolated run
// without modifying shared launcher or user state.
func PrepareFixture(in SetupInput) (*Fixture, error) {
	if err := CheckSetupPrerequisites(in); err != nil {
		return nil, err
	}
	root, err := os.MkdirTemp(in.BaseDir, "duo-portable-launcher-")
	if err != nil {
		return nil, fmt.Errorf("create run root: %w", err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	f := &Fixture{Root: root, Workspace: filepath.Join(root, "workspace"), Capture: filepath.Join(root, "capture"), Result: filepath.Join(root, "result")}
	if err := f.materialize(in); err != nil {
		_ = os.RemoveAll(root)
		return nil, err
	}
	return f, nil
}

func (f *Fixture) materialize(in SetupInput) error {
	dirs := []string{
		"home", "xdg/config", "xdg/config/amp", "xdg/config/codex", "xdg/config/duo", "xdg/config/opencode", "xdg/config/pi", "xdg/data", "xdg/data/pi/sessions", "xdg/state", "xdg/cache", "xdg/runtime",
		"bin", "secrets", "herdr", "workspace", "workspace/fixture", "workspace/.git/refs/heads",
		"workspace/.duo-conformance", "capture", "result",
	}
	for _, rel := range dirs {
		path, err := f.Path(rel)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(path, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", rel, err)
		}
	}
	files := []struct {
		rel  string
		data []byte
		mode os.FileMode
	}{
		{"workspace/fixture/request.txt", RequestBytes, 0o600},
		{"workspace/.git/HEAD", []byte("ref: refs/heads/main\n"), 0o600},
		{"workspace/.git/config", []byte("[core]\n\trepositoryformatversion = 0\n\tbare = false\n"), 0o600},
		{"xdg/config/duo/duo.config.yaml", in.ConfigBytes, 0o600},
		{"xdg/config/amp/settings.json", []byte("{}\n"), 0o600},
		{"secrets/provider", in.Credential.Bytes, 0o600},
		{"xdg/config/pi/auth.json", in.Credential.Bytes, 0o600},
	}
	scenario, err := ScenarioJSON(CanonicalScenario())
	if err != nil {
		return err
	}
	files = append(files, struct {
		rel  string
		data []byte
		mode os.FileMode
	}{"workspace/.duo-conformance/scenario.json", scenario, 0o600})
	for _, file := range files {
		path, err := f.Path(file.rel)
		if err != nil {
			return err
		}
		if err := os.WriteFile(path, file.data, file.mode); err != nil {
			return fmt.Errorf("write %s: %w", file.rel, err)
		}
	}
	installed, err := manifest.InstallPortableLaunchers(f.Workspace, false, in.ProductManifest)
	if err != nil {
		return fmt.Errorf("install portable launcher skill: %w", err)
	}
	if installed.State != manifest.StateCurrent || installed.ContentDigest != SkillContentDigest || installed.InstallationID == "" {
		return fmt.Errorf("portable launcher skill installation did not produce the locked current projection")
	}
	for key, artifact := range in.Artifacts {
		if err := copyFile(artifact.Path, filepath.Join(f.Root, "bin", artifact.Name), 0o700); err != nil {
			return fmt.Errorf("copy %s: %w", key, err)
		}
	}
	return nil
}

// Path resolves a clean relative path below the fixture root.
func (f *Fixture) Path(relative string) (string, error) {
	if relative == "" || filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe fixture-relative path %q", relative)
	}
	path := filepath.Join(f.Root, relative)
	if !within(f.Root, path) {
		return "", fmt.Errorf("fixture path escapes run root: %q", relative)
	}
	return path, nil
}

func within(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func validatePrivateTempDir(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("path must exist as a mode-private, non-symlink directory")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	temp, err := filepath.EvalSymlinks(filepath.Clean(os.TempDir()))
	if err != nil {
		return "", err
	}
	if filepath.Clean(resolved) != filepath.Clean(abs) {
		return "", fmt.Errorf("path contains a symlink component")
	}
	if resolved == temp || !within(temp, resolved) {
		return "", fmt.Errorf("path is outside the system temporary directory")
	}
	return resolved, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return errors.Join(err, in.Close())
	}
	_, copyErr := io.Copy(out, in)
	return errors.Join(copyErr, out.Close(), in.Close())
}

// ResourceInspector verifies that no run-owned resources remain active before
// a fixture is removed.
type ResourceInspector func(context.Context, *Fixture) error

// EvidenceExporter preserves the capture needed for offline validation before
// a fixture is removed.
type EvidenceExporter func(context.Context, *Fixture) error

// Cleanup records failures by returning them even though it performs a
// best-effort exact-root removal afterwards. It never broad-kills processes.
func (f *Fixture) Cleanup(ctx context.Context, inspect ResourceInspector, export EvidenceExporter) error {
	if f == nil || f.Root == "" || !strings.HasPrefix(filepath.Base(f.Root), "duo-portable-launcher-") {
		return fmt.Errorf("refusing cleanup of unsafe root")
	}
	if _, err := validatePrivateTempDir(f.Root); err != nil {
		return fmt.Errorf("refusing cleanup of missing or symlinked root")
	}
	var p Problems
	if inspect == nil {
		p.Add("cleanup resource inspection omitted")
	} else if err := inspect(ctx, f); err != nil {
		p.Add("cleanup resource inspection failed: " + err.Error())
	}
	if export == nil {
		p.Add("cleanup evidence export omitted")
	} else if err := export(ctx, f); err != nil {
		p.Add("cleanup evidence export failed: " + err.Error())
	}
	if err := os.RemoveAll(f.Root); err != nil {
		p.Add("cleanup root removal failed: " + err.Error())
	} else {
		f.cleaned = true
	}
	return p.Err()
}

// ProcessIdentity identifies one exact run-owned process and attachment.
type ProcessIdentity struct {
	PID          int
	StartTime    string
	TerminalID   string
	AttachmentID string
}

// ControlCheckpoint records an independently verified fault-controller action.
type ControlCheckpoint struct {
	Target          ProcessIdentity
	StoppedVerified bool
	MonotonicMS     int64
}

// FaultController is the boundary later live stages implement. The common
// suite owns when it is called; a launcher driver never receives this seam.
type FaultController interface {
	SuspendExactProcess(context.Context, ProcessIdentity) (ControlCheckpoint, error)
	CloseExactPane(context.Context, ProcessIdentity) (ControlCheckpoint, error)
}
