package pi

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

// CloseOnExitExtensionSource is the embedded, close-only Pi extension Duo
// materializes when a pi launch requests close-on-exit:
// closeonexit/duo-close-on-exit.ts, embedded verbatim (no template
// substitution — everything load-bearing is fixed in the asset itself and
// guarded by extension_test.go), deliberately separate from
// extensionAsset (the reporter): close-on-exit must not drag the
// reporter's credential/socket concerns into a minimal, close-only
// extension. See host.ResolvedLaunchTuple.CloseOnExit.
//
//go:embed closeonexit/duo-close-on-exit.ts
var CloseOnExitExtensionSource string

// CloseOnExitExtensionFileName is the file name MaterializeCloseOnExit
// writes the extension to, inside its caller-chosen harness directory. Pi
// loads it by exact path with `-e <path>` (`pi --extension, -e <path>`,
// pi 0.83.0 `pi --help`); nothing about the name matters to pi beyond
// being a valid module path, but a stable name keeps a materialized
// harness directory's contents legible.
const CloseOnExitExtensionFileName = "duo-close-on-exit.ts"

// DefaultHarnessDir returns the per-launch-resolution, per-leaf directory
// Duo materializes a pi launch's close-on-exit extension into:
// $XDG_DATA_HOME/duo/harness/<launch-resolution-id>/<leaf>, falling back to
// ~/.local/share/duo/harness/... when XDG_DATA_HOME is unset — the same
// XDG base-directory convention internal/doctor.DefaultStorePath and
// internal/runtime/claude.DefaultHarnessDir both use.
//
// Duplicated from the claude package's own DefaultHarnessDir rather than
// hoisted: internal/runtime's package doc bounds it to the §5.3 adapter
// contract's shared interfaces and evidence types, not general-purpose
// filesystem helpers, and this repository already has one precedent for
// duplicating this exact XDG convention independently per call site
// (internal/doctor.DefaultStorePath). Fifteen duplicated lines keep each
// runtime adapter self-contained the same way claude/closeonexit.go
// already is, and trivially satisfy internal/adapter's TestRoleSeparation
// (neither copy imports anything the other doesn't already import).
//
// No planning document fixes a generated-harness-tree path yet
// (internal/manifest's HarnessTarget is scaffolding for a later renderer,
// not a path convention: "No harness renderer exists yet"); this is this
// feature's own call.
func DefaultHarnessDir(launchResolutionID, leaf string) (string, error) {
	if launchResolutionID == "" {
		return "", fmt.Errorf("pi: close-on-exit harness directory needs a launch-resolution ID")
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("pi: resolving XDG_DATA_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	dir := filepath.Join(base, "duo", "harness", launchResolutionID)
	if leaf != "" {
		dir = filepath.Join(dir, leaf)
	}
	return dir, nil
}

// MaterializeCloseOnExit writes the close-on-exit extension into dir and
// returns its absolute path — what a caller passes as `pi -e <path>`.
//
// dir is created if missing at 0700, and the extension file itself is
// written 0600: unlike claude's hook script (invoked directly as an
// executable, so its own execute bit is what makes it runnable), pi's
// `-e <path>` loads this file as a jiti-loaded TypeScript module — nothing
// ever executes it as a file — and nothing written here is a secret
// either, but a generated per-launch harness file still stays as narrow as
// a launch-resolution record's own directory, not world- or
// group-readable, matching claude's MaterializeCloseOnExit.
//
// dir is caller-chosen (DefaultHarnessDir is the production choice) so
// this function stays free of any opinion about where a launch resolution
// or its leaves keep their generated files.
func MaterializeCloseOnExit(dir string) (extensionPath string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("pi: creating close-on-exit harness directory %s: %w", dir, err)
	}
	extensionPath = filepath.Join(dir, CloseOnExitExtensionFileName)
	if err := os.WriteFile(extensionPath, []byte(CloseOnExitExtensionSource), 0o600); err != nil {
		return "", fmt.Errorf("pi: writing close-on-exit extension %s: %w", extensionPath, err)
	}
	return extensionPath, nil
}
