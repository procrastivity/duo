package amp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SettingsFileName is the file name MaterializeSettings writes the
// generated `amp --settings-file` document to.
const SettingsFileName = "amp-settings.json"

// MintScriptFileName is the file name MaterializeMintScript writes the
// generated mint wrapper script to.
const MintScriptFileName = "mint.sh"

// DefaultHarnessDir returns the per-launch-resolution, per-leaf directory
// Duo materializes an Amp launch's mint harness files into:
// $XDG_DATA_HOME/duo/harness/<launch-resolution-id>/<leaf>, falling back
// to ~/.local/share/duo/harness/... when XDG_DATA_HOME is unset — the
// same tree, and the same XDG base-directory convention,
// claude.DefaultHarnessDir already uses for its close-on-exit harness
// files. A launch leaf belongs to exactly one runtime kind, so two
// adapters never contend for the same <launch-resolution-id>/<leaf>
// directory.
func DefaultHarnessDir(launchResolutionID, leaf string) (string, error) {
	if launchResolutionID == "" {
		return "", fmt.Errorf("amp: harness directory needs a launch-resolution ID")
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("amp: resolving XDG_DATA_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "duo", "harness", launchResolutionID)
	if leaf != "" {
		dir = filepath.Join(dir, leaf)
	}
	return dir, nil
}

// ampSettings is the narrow slice of Amp's settings.json shape this
// package ever writes: disable the CLI's own auto-update check (a mint
// run must stay pinned to the binary Probe already found, not silently
// re-exec a newer one mid-mint) and its terminal animation (noise a
// headless, tee'd mint run never needs). Neither key carries a
// credential or transcript content.
type ampSettings struct {
	UpdatesMode       string `json:"amp.updates.mode"`
	TerminalAnimation bool   `json:"amp.terminal.animation"`
}

// RenderSettings returns the settings.json document MaterializeSettings
// writes: {"amp.updates.mode":"disabled","amp.terminal.animation":false}.
func RenderSettings() ([]byte, error) {
	s := ampSettings{UpdatesMode: "disabled", TerminalAnimation: false}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("amp: rendering settings: %w", err)
	}
	return b, nil
}

// MaterializeSettings writes the generated settings document into dir
// and returns its absolute path — what a caller passes as `amp
// --settings-file <path>`.
//
// dir is created if missing at 0700, matching claude.MaterializeCloseOnExit's
// harness-directory discipline: nothing written here is a secret, but the
// directory stays as narrow as a launch-resolution record's own directory,
// not world- or group-readable. The settings file is written 0600 (owner
// rw); nothing ever executes it.
//
// dir is caller-chosen (DefaultHarnessDir is the production choice) so
// this function stays free of any opinion about where a launch resolution
// or its leaves keep their generated files.
func MaterializeSettings(dir string) (settingsPath string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("amp: creating harness directory %s: %w", dir, err)
	}
	settings, err := RenderSettings()
	if err != nil {
		return "", err
	}
	settingsPath = filepath.Join(dir, SettingsFileName)
	if err := os.WriteFile(settingsPath, settings, 0o600); err != nil {
		return "", fmt.Errorf("amp: writing settings %s: %w", settingsPath, err)
	}
	return settingsPath, nil
}

// MaterializeMintScript writes the mint wrapper script into dir and
// returns its absolute path — what the launch leaf augmenter's Amp leg
// names as the leaf's own command (docs/cli/decisions.md, 2026-09-09,
// "Amp mint delivery needs a Duo-materialized wrapper script": `amp -x`
// takes its prompt on stdin, not argv, and the leaf augmenter only ever
// appends args and env, so the prompt has to arrive through a
// Duo-materialized script rather than through that seam directly).
//
// settingsPath and mintLogPath are baked into the script body,
// shell-quoted, so the script carries no argv of its own beyond what
// `amp -x` itself needs — the mint prompt still arrives on
// MintPromptEnvVar, not as a script argument, keeping the script body
// byte-identical across every launch that materializes one.
//
// This is the thread-creation mint only (`amp -x` creates the thread);
// per-turn delivery to an already-minted thread is `amp threads
// continue`, a different recipe wired up in step-03.
//
// dir is created if missing at 0700, and the script itself is written
// 0700 (owner rwx): Claude Code's close-on-exit hook and Devin's
// duo-hook.sh are both invoked as direct executables the same way, so
// the file's own execute bit is what makes it runnable.
func MaterializeMintScript(dir, settingsPath, mintLogPath string) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("amp: creating harness directory %s: %w", dir, err)
	}
	script := renderMintScript(settingsPath, mintLogPath)
	scriptPath := filepath.Join(dir, MintScriptFileName)
	if err := os.WriteFile(scriptPath, []byte(script), 0o700); err != nil {
		return "", fmt.Errorf("amp: writing mint script %s: %w", scriptPath, err)
	}
	return scriptPath, nil
}

// renderMintScript builds the mint wrapper script body. PIPESTATUS needs
// bash (dash and other strict-POSIX /bin/sh builds do not have it), so
// the script requires bash rather than the plain `#!/bin/sh` Devin's
// hook script uses — the explicit PIPESTATUS exits are load-bearing:
// without them, a failing `amp -x` behind a successful `tee` would
// report exit 0, and a failing `tee` behind a successful mint would hide
// a truncated mint log.
func renderMintScript(settingsPath, mintLogPath string) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("# Generated by Duo; do not edit. `amp -x` mints one Amp thread; this\n")
	b.WriteString("# script exists only because the launch leaf augmenter can append args\n")
	b.WriteString("# and env to a leaf's own command but cannot put the prompt on stdin or\n")
	b.WriteString("# tee the stream-JSON output itself (docs/cli/decisions.md, 2026-09-09,\n")
	b.WriteString("# \"Amp mint delivery needs a Duo-materialized wrapper script\"). The\n")
	b.WriteString("# prompt arrives on " + MintPromptEnvVar + ", not argv, so this script's own\n")
	b.WriteString("# body stays constant across launches; only the baked-in --settings-file\n")
	b.WriteString("# and tee paths below vary per launch leaf.\n")
	b.WriteString("printf '%s\\n' \"$" + MintPromptEnvVar + "\" | amp -x --no-archive-after-execute --settings-file " +
		shellQuote(settingsPath) + " --stream-json | tee " + shellQuote(mintLogPath) + "\n")
	b.WriteString("rc=(\"${PIPESTATUS[@]}\")\n")
	b.WriteString("if [ \"${rc[1]}\" -ne 0 ]; then exit \"${rc[1]}\"; fi\n")
	b.WriteString("exit \"${rc[2]}\"\n")
	return b.String()
}

// shellQuote wraps s in single quotes for POSIX sh, escaping any single
// quote s itself contains with the standard close-quote, escaped-quote,
// reopen-quote idiom. Mirrors internal/scrub/pane.go's shellQuote: single
// quotes are used because they are the only POSIX quoting form with no
// exceptions, not even backslash.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
