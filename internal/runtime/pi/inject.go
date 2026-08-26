package pi

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// InjectExtensionSource is the embedded, per-launch Pi inject extension Duo
// materializes for every pi launch:
// inject/duo-inject.ts, embedded verbatim (no template
// substitution — everything load-bearing is fixed in the asset itself and
// guarded by inject_asset_test.go), deliberately separate from
// extensionAsset (the reporter) and from CloseOnExitExtensionSource:
// inject must not drag the reporter's credential/socket concerns or
// close-on-exit's pane-close behavior into a minimal delivery extension.
// See host.ResolvedLaunchTuple and stage1LeafAugmenter.
//
//go:embed inject/duo-inject.ts
var InjectExtensionSource string

// InjectExtensionFileName is the file name MaterializeInject
// writes the extension to, inside its caller-chosen harness directory. Pi
// loads it by exact path with `-e <path>` (`pi --extension, -e <path>`,
// pi 0.83.0 `pi --help`); nothing about the name matters to pi beyond
// being a valid module path, but a stable name keeps a materialized
// harness directory's contents legible.
const InjectExtensionFileName = "duo-inject.ts"

// InjectSocketEnvVar names the optional full-path listen override the
// inject extension reads at module load. Go locate never reads it.
const InjectSocketEnvVar = "DUO_PI_SOCK"

func xdgRuntimeDir() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
}

// InjectSocketDir returns the directory where the inject extension binds
// its per-session Unix socket: $XDG_RUNTIME_DIR/duo/pi-inject, falling
// back to /run/user/<uid>/duo/pi-inject when XDG_RUNTIME_DIR is unset.
//
// Duplicated from the claude package's own derivedMessagingSocket rather
// than hoisted: internal/runtime's package doc bounds it to the §5.3 adapter
// contract's shared interfaces and evidence types, not general-purpose
// filesystem helpers, and this repository already has one precedent for
// duplicating this exact XDG convention independently per call site
// (DefaultHarnessDir). Fifteen duplicated lines keep each runtime adapter
// self-contained the same way closeonexit.go already is.
func InjectSocketDir() string {
	return filepath.Join(xdgRuntimeDir(), "duo", "pi-inject")
}

// InjectSocketPath returns the conventional Unix socket path Duo uses to
// deliver prompts to one Pi session. sessionID may be a bare session uuid
// or a transcript file path; SessionIDFromTranscriptName reduces the
// latter before the path is built. Go locate never reads DUO_PI_SOCK.
func InjectSocketPath(sessionID string) (string, error) {
	id := SessionIDFromTranscriptName(sessionID)
	if id == "" {
		id = sessionID
	}
	if id == "" || strings.ContainsAny(id, `/\`) {
		return "", fmt.Errorf("pi: inject socket path needs a Pi session id, not a path")
	}
	path := filepath.Join(InjectSocketDir(), id+".sock")
	if err := ValidateSocketPath(path); err != nil {
		return "", err
	}
	return path, nil
}

// MaterializeInject writes the inject extension into dir and
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
func MaterializeInject(dir string) (extensionPath string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("pi: creating inject harness directory %s: %w", dir, err)
	}
	extensionPath = filepath.Join(dir, InjectExtensionFileName)
	if err := os.WriteFile(extensionPath, []byte(InjectExtensionSource), 0o600); err != nil {
		return "", fmt.Errorf("pi: writing inject extension %s: %w", extensionPath, err)
	}
	return extensionPath, nil
}
