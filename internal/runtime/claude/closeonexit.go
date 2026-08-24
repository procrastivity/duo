package claude

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CloseOnExitHookScript is the embedded SessionEnd hook Duo installs when a
// claude launch requests --close-on-exit: closeonexit/session-end.sh,
// embedded verbatim (no template substitution — everything load-bearing is
// fixed in the asset itself and guarded by closeonexit_test.go), matching
// internal/runtime/pi/extension.go's embed pattern for a generated harness
// asset.
//
// PROVISIONAL (dogfood, 2026-08-24): --close-on-exit is a dogfood-day
// expedient ahead of change control; the ratified design is sketched in
// terminal-multiplexers notes/46. See host.ResolvedLaunchTuple.CloseOnExit.
//
//go:embed closeonexit/session-end.sh
var CloseOnExitHookScript string

// CloseOnExitHookFileName is the file name MaterializeCloseOnExit writes
// the hook script to, inside its caller-chosen harness directory.
const CloseOnExitHookFileName = "session-end-hook.sh"

// CloseOnExitSettingsFileName is the file name MaterializeCloseOnExit
// writes the rendered `claude --settings` document to.
const CloseOnExitSettingsFileName = "close-on-exit-settings.json"

// closeOnExitSettings is the narrow slice of Claude Code's settings.json
// shape this package ever writes: exactly one SessionEnd hook, no
// tool-name matcher (session-lifecycle hooks are not tool-scoped, unlike
// PreToolUse/PostToolUse). `claude --settings <path>` MERGES with the
// user's own settings rather than replacing them (verified live, herdr
// 0.8.2 + claude 2.1.241, 2026-08-24), so a generated document with only
// this one key never disturbs whatever hooks or configuration a user's own
// settings already declare.
type closeOnExitSettings struct {
	Hooks closeOnExitHooks `json:"hooks"`
}

type closeOnExitHooks struct {
	SessionEnd []closeOnExitHookMatcher `json:"SessionEnd"`
}

// closeOnExitHookMatcher carries no Matcher field: Claude Code's matcher
// syntax selects tool names for tool-scoped hook events, and SessionEnd is
// not one — every SessionEnd fires this hook, and the hook script itself
// (closeonexit/session-end.sh) is what decides, from the payload's own
// "reason" field, whether that particular SessionEnd is terminal.
type closeOnExitHookMatcher struct {
	Hooks []closeOnExitHookCommand `json:"hooks"`
}

type closeOnExitHookCommand struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

// RenderCloseOnExitSettings returns the settings.json document that wires
// hookPath — which must be absolute, since the rendered document outlives
// the working directory it was generated from — as a SessionEnd hook.
func RenderCloseOnExitSettings(hookPath string) ([]byte, error) {
	if !filepath.IsAbs(hookPath) {
		return nil, fmt.Errorf("claude: close-on-exit hook path %q is not absolute", hookPath)
	}
	s := closeOnExitSettings{
		Hooks: closeOnExitHooks{
			SessionEnd: []closeOnExitHookMatcher{{
				Hooks: []closeOnExitHookCommand{{Type: "command", Command: hookPath}},
			}},
		},
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("claude: rendering close-on-exit settings: %w", err)
	}
	return b, nil
}

// DefaultHarnessDir returns the per-launch-resolution, per-leaf directory
// Duo materializes a claude launch's close-on-exit harness files into:
// $XDG_DATA_HOME/duo/harness/<launch-resolution-id>/<leaf>, falling back to
// ~/.local/share/duo/harness/... when XDG_DATA_HOME is unset — the same
// XDG base-directory convention internal/doctor.DefaultStorePath uses for
// the authority store.
//
// No planning document fixes a generated-harness-tree path yet
// (internal/manifest's HarnessTarget is scaffolding for a later renderer,
// not a path convention: "No harness renderer exists yet"); this is this
// feature's own call, PROVISIONAL like the rest of --close-on-exit, and it
// is the first thing in this repository to write a generated per-session
// file to disk at all.
func DefaultHarnessDir(launchResolutionID, leaf string) (string, error) {
	if launchResolutionID == "" {
		return "", fmt.Errorf("claude: close-on-exit harness directory needs a launch-resolution ID")
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("claude: resolving XDG_DATA_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "duo", "harness", launchResolutionID)
	if leaf != "" {
		dir = filepath.Join(dir, leaf)
	}
	return dir, nil
}

// MaterializeCloseOnExit writes the hook script and its settings file into
// dir and returns the settings file's absolute path — what a caller passes
// as `claude --settings <path>`.
//
// dir is created if missing at 0700: nothing written under it is a secret
// (the hook script carries no credential; the settings file names only a
// path to itself), but the hook script's behavior — closing a pane — is
// security-relevant enough that this stays as narrow as a launch-resolution
// record's own directory, not world- or group-readable. The hook script is
// written 0700 (owner rwx) because Claude Code's "type": "command" hook
// invokes it as a direct executable — the file's own execute bit, not a
// shell wrapping it, is what makes it runnable — and 0600 (owner rw) is
// enough for the settings JSON, which nothing ever executes.
//
// dir is caller-chosen (DefaultHarnessDir is the production choice) so
// this function stays free of any opinion about where a launch resolution
// or its leaves keep their generated files.
func MaterializeCloseOnExit(dir string) (settingsPath string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("claude: creating close-on-exit harness directory %s: %w", dir, err)
	}
	hookPath := filepath.Join(dir, CloseOnExitHookFileName)
	if err := os.WriteFile(hookPath, []byte(CloseOnExitHookScript), 0o700); err != nil {
		return "", fmt.Errorf("claude: writing close-on-exit hook script %s: %w", hookPath, err)
	}
	settings, err := RenderCloseOnExitSettings(hookPath)
	if err != nil {
		return "", err
	}
	settingsPath = filepath.Join(dir, CloseOnExitSettingsFileName)
	if err := os.WriteFile(settingsPath, settings, 0o600); err != nil {
		return "", fmt.Errorf("claude: writing close-on-exit settings %s: %w", settingsPath, err)
	}
	return settingsPath, nil
}
