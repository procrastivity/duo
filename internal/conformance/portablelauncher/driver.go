package portablelauncher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// TaskArgument is the placeholder a driver replaces with the canonical task.
const (
	TaskArgument       = "{PORTABLE_LAUNCHER_TASK_BYTES}"
	completeRunTimeout = 10 * time.Minute
)

// DriverSpec defines one launcher's thin outer-process invocation.
type DriverSpec struct {
	Name       string
	Executable string
	Arguments  []string
}

// DriverRequest supplies the isolated paths and allowed environment additions
// for one launcher invocation.
type DriverRequest struct {
	RunRoot             string
	Workspace           string
	ScenarioPath        string
	RunOriginBootTimeNS string
	Environment         map[string]string
}

// DriverCapture contains the raw process streams collected from a launcher.
type DriverCapture struct {
	Events []byte
	Stderr []byte
}

// PersistDriverCapture writes the launcher's raw process streams below the
// isolated capture directory without interpreting or repairing either one.
func PersistDriverCapture(runRoot string, capture DriverCapture) error {
	root, err := validatePrivateTempDir(runRoot)
	if err != nil || !strings.HasPrefix(filepath.Base(root), "duo-portable-launcher-") {
		return fmt.Errorf("persist driver capture: unsafe run root")
	}
	dir := filepath.Join(root, "capture")
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil || !within(root, resolved) {
		return fmt.Errorf("persist driver capture: unsafe capture directory")
	}
	files := []struct {
		name string
		data []byte
	}{
		{name: "launcher-events.jsonl", data: capture.Events},
		{name: "launcher-stderr.log", data: capture.Stderr},
	}
	for _, file := range files {
		path := filepath.Join(dir, file.name)
		out, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr != nil {
			return fmt.Errorf("persist driver capture %s: %w", file.name, openErr)
		}
		_, writeErr := out.Write(file.data)
		closeErr := out.Close()
		if writeErr != nil {
			return fmt.Errorf("persist driver capture %s: %w", file.name, writeErr)
		}
		if closeErr != nil {
			return fmt.Errorf("persist driver capture %s: %w", file.name, closeErr)
		}
	}
	return nil
}

// RunDriver only constructs and executes the outer process invocation. It
// submits the common task once and returns the raw launcher process streams.
func RunDriver(ctx context.Context, spec DriverSpec, request DriverRequest) (DriverCapture, error) {
	if err := validateDriverRequest(spec, request); err != nil {
		return DriverCapture{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, completeRunTimeout)
	defer cancel()
	args := append([]string(nil), spec.Arguments...)
	taskCount := 0
	for i := range args {
		if args[i] == TaskArgument {
			args[i] = string(CanonicalTaskBytes)
			taskCount++
		}
	}
	if taskCount != 1 {
		return DriverCapture{}, fmt.Errorf("driver must submit the canonical task exactly once")
	}
	cmd := exec.CommandContext(ctx, spec.Executable, args...)
	cmd.Dir = request.Workspace
	cmd.Stdin = nil
	cmd.Env = isolatedEnvironment(request)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	capture := DriverCapture{Events: stdout.Bytes(), Stderr: stderr.Bytes()}
	if runErr != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return capture, fmt.Errorf("launcher complete-run deadline exceeded: %w", ctx.Err())
		}
		return capture, fmt.Errorf("launcher process failed: %w", runErr)
	}
	return capture, nil
}

func validateDriverRequest(spec DriverSpec, r DriverRequest) error {
	if err := validateRunOrigin(r.RunOriginBootTimeNS); err != nil {
		return err
	}
	if spec.Name == "" || spec.Executable == "" || !filepath.IsAbs(spec.Executable) {
		return fmt.Errorf("driver executable must be named and absolute")
	}
	if !filepath.IsAbs(r.RunRoot) || !strings.HasPrefix(filepath.Base(r.RunRoot), "duo-portable-launcher-") {
		return fmt.Errorf("driver run root must be an isolated temporary child")
	}
	root, err := validatePrivateTempDir(r.RunRoot)
	if err != nil {
		return fmt.Errorf("driver run root must exist as a private directory")
	}
	if !RecognizedLauncher(spec.Name) {
		return fmt.Errorf("driver names an unsupported launcher")
	}
	pin, err := loadRunLauncherPin(root)
	if err != nil {
		return err
	}
	if pin.Name != spec.Name {
		return fmt.Errorf("driver launcher identity disagrees with setup")
	}
	for label, path := range map[string]string{"executable": spec.Executable, "workspace": r.Workspace, "scenario": r.ScenarioPath} {
		resolved, resolveErr := filepath.EvalSymlinks(path)
		if resolveErr != nil || !within(root, resolved) {
			return fmt.Errorf("driver %s path escapes run root", label)
		}
	}
	resolvedExecutable, _ := filepath.EvalSymlinks(spec.Executable)
	if filepath.Dir(resolvedExecutable) != filepath.Join(root, "bin") {
		return fmt.Errorf("driver executable must resolve from the pinned run bin")
	}
	if info, err := os.Lstat(spec.Executable); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("driver executable is missing or not a regular file")
	}
	executable, err := os.ReadFile(spec.Executable)
	if err != nil || Digest(executable) != pin.ExecutableSHA256 {
		return fmt.Errorf("driver executable does not match the pinned digest")
	}
	scenario, err := os.ReadFile(r.ScenarioPath)
	if err != nil {
		return fmt.Errorf("driver scenario is unavailable: %w", err)
	}
	wantScenario, err := ScenarioJSON(CanonicalScenario())
	if err != nil || !bytes.Equal(scenario, wantScenario) {
		return fmt.Errorf("driver scenario does not match canonical bytes")
	}
	for key := range r.Environment {
		upper := strings.ToUpper(key)
		if key == "" || strings.Contains(key, "=") || upper == "PATH" || upper == "HOME" || strings.HasPrefix(upper, "XDG_") || strings.HasPrefix(upper, "DUO_CONFORMANCE_") || strings.Contains(upper, "MCP") || strings.Contains(upper, "PLUGIN") {
			return fmt.Errorf("driver environment key %q is forbidden", key)
		}
	}
	return nil
}

func loadRunLauncherPin(root string) (LauncherPin, error) {
	path := filepath.Join(root, filepath.FromSlash(launcherPinRelativePath))
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return LauncherPin{}, fmt.Errorf("driver run launcher identity is unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return LauncherPin{}, fmt.Errorf("driver read run launcher identity: %w", err)
	}
	var pin LauncherPin
	if err := decodeStrict(data, &pin); err != nil || !validLauncherPin(pin) {
		return LauncherPin{}, fmt.Errorf("driver run launcher identity is malformed")
	}
	return pin, nil
}

func validateRunOrigin(value string) error {
	if value == "" {
		return fmt.Errorf("driver common run origin must be a positive decimal int64")
	}
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return fmt.Errorf("driver common run origin must be a positive decimal int64")
		}
	}
	origin, err := strconv.ParseInt(value, 10, 64)
	if err != nil || origin <= 0 {
		return fmt.Errorf("driver common run origin must be a positive decimal int64")
	}
	return nil
}

// RequestFromRunRoot constructs the common isolated paths used by every thin
// adapter. It does not inspect or alter scenario semantics.
func RequestFromRunRoot(root, runOriginBootTimeNS string) DriverRequest {
	return DriverRequest{
		RunRoot: root, Workspace: filepath.Join(root, "workspace"), RunOriginBootTimeNS: runOriginBootTimeNS,
		ScenarioPath: filepath.Join(root, "workspace", ".duo-conformance", "scenario.json"),
	}
}

func isolatedEnvironment(r DriverRequest) []string {
	env := map[string]string{
		"CODEX_HOME":                    filepath.Join(r.RunRoot, "xdg", "config", "codex"),
		"HOME":                          filepath.Join(r.RunRoot, "home"),
		"PATH":                          filepath.Join(r.RunRoot, "bin"),
		"XDG_CONFIG_HOME":               filepath.Join(r.RunRoot, "xdg", "config"),
		"XDG_DATA_HOME":                 filepath.Join(r.RunRoot, "xdg", "data"),
		"XDG_STATE_HOME":                filepath.Join(r.RunRoot, "xdg", "state"),
		"XDG_CACHE_HOME":                filepath.Join(r.RunRoot, "xdg", "cache"),
		"XDG_RUNTIME_DIR":               filepath.Join(r.RunRoot, "xdg", "runtime"),
		"PI_CODING_AGENT_DIR":           filepath.Join(r.RunRoot, "xdg", "config", "pi"),
		"PI_CODING_AGENT_SESSION_DIR":   filepath.Join(r.RunRoot, "xdg", "data", "pi", "sessions"),
		"HERDR_CONFIG_PATH":             filepath.Join(r.RunRoot, "herdr", "config.yaml"),
		"DUO_CONFORMANCE_SCENARIO_PATH": r.ScenarioPath,
		CommandCaptureDirectoryEnv:      filepath.Join(r.RunRoot, "capture"),
		CommandRunOriginEnv:             r.RunOriginBootTimeNS,
	}
	for key, value := range r.Environment {
		env[key] = value
	}
	out := make([]string, 0, len(env))
	for key, value := range env {
		out = append(out, key+"="+value)
	}
	return out
}

var forbiddenDriverSemantics = []*regexp.Regexp{
	regexp.MustCompile(`\bduo\s+(doctor|session|prompt|command|wait)\b`),
	regexp.MustCompile(`DUO_PORTABLE_LAUNCHER_OK_V1|b40d3a68|dbe904a4`),
	regexp.MustCompile(`(?i)\b(poll|retry|sigstop|pane\.close|send_keys|send_text|repair_result)\b`),
}

// ValidateDriverSource is a static guard for thin adapter sources.
func ValidateDriverSource(source []byte) error {
	for _, pattern := range forbiddenDriverSemantics {
		if pattern.Match(source) {
			return fmt.Errorf("launcher driver contains scenario/oracle semantics matching %s", pattern)
		}
	}
	return nil
}

// ValidateSemanticSource rejects launcher-specific branching/imports in the
// common scenario, oracle, and validator sources.
func ValidateSemanticSource(source []byte) error {
	lower := strings.ToLower(string(source))
	for _, token := range []string{"internal/runtime/amp", "internal/runtime/opencode", "internal/runtime/codex"} {
		if strings.Contains(lower, token) {
			return fmt.Errorf("semantic source imports launcher package %q", token)
		}
	}
	if regexp.MustCompile(`switch\s+(launcher|driver)\b|if\s+(launcher|driver)\b`).MatchString(lower) {
		return fmt.Errorf("semantic source branches on launcher identity")
	}
	return nil
}
