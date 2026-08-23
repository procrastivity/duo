// Package asset implements the chassis asset-resolution chain: user
// override -> shipped default -> embedded last-resort fallback. An override
// and a shipped default at the same relative path are the same logical
// asset — the override fully replaces the default, never merged (shadow by
// name). Assets are inputs, never state: this package only ever reads.
package asset

import (
	"fmt"
	"os"
	"path/filepath"

	rootassets "github.com/procrastivity/duo/assets"
)

// appName names both the XDG config subdirectory and the installed share
// subtree: $XDG_CONFIG_HOME/duo and <prefix>/share/duo/assets.
const appName = "duo"

// executable is os.Executable, indirected so tests can point default-dir
// resolution at a fake <prefix>/bin/duo without needing a real installed
// layout on disk.
var executable = os.Executable

// Source names which link of the resolution chain satisfied a Resolve call.
type Source int

const (
	// SourceOverride is the user-override directory, $XDG_CONFIG_HOME/duo.
	SourceOverride Source = iota
	// SourceDefault is the shipped default directory, <prefix>/share/duo/assets.
	SourceDefault
	// SourceEmbedded is the last-resort fallback compiled into the binary.
	SourceEmbedded
)

func (s Source) String() string {
	switch s {
	case SourceOverride:
		return "override"
	case SourceDefault:
		return "default"
	case SourceEmbedded:
		return "embedded"
	default:
		return "unknown"
	}
}

// Resolved is one asset located by Resolve.
type Resolved struct {
	Source Source
	// Path is the filesystem path the asset was read from; empty when
	// Source is SourceEmbedded (the asset lives in the compiled binary,
	// not on disk).
	Path string

	data []byte
}

// Bytes returns the asset's content.
func (r Resolved) Bytes() []byte {
	return r.data
}

// OverrideDir returns $XDG_CONFIG_HOME/duo, the user-override directory.
func OverrideDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("asset: resolving XDG_CONFIG_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appName), nil
}

// DefaultDir returns <prefix>/share/duo/assets, resolved relative to the
// running executable per the chassis's "relative-to-self" call: the binary
// is assumed to sit at <prefix>/bin/duo, with a sibling
// <prefix>/share/duo/assets — the layout buildGoModule's $out already
// guarantees, so no build-time patching is needed.
func DefaultDir() (string, error) {
	exe, err := executable()
	if err != nil {
		return "", fmt.Errorf("asset: locating running executable: %w", err)
	}
	prefix := filepath.Dir(filepath.Dir(exe)) // .../bin/duo -> .../bin -> <prefix>
	return filepath.Join(prefix, "share", appName, "assets"), nil
}

// Resolve locates relativeName by the override -> default -> embedded
// chain and returns its content. Shadow by name: an override present at
// relativeName fully replaces a shipped default at the same relative path.
func Resolve(relativeName string) (Resolved, error) {
	if r, ok, err := ReadOverride(relativeName); err == nil && ok {
		return r, nil
	}
	return ReadDefault(relativeName)
}

// ReadOverride reads relativeName from the user-override directory only,
// reporting whether it was present. Config uses this directly, since its
// override and default filenames differ (config.yaml vs.
// config.default.yaml) and so can't share one Resolve call.
func ReadOverride(relativeName string) (Resolved, bool, error) {
	dir, err := OverrideDir()
	if err != nil {
		return Resolved{}, false, err
	}
	path := filepath.Join(dir, relativeName)
	if data, ok := readIfExists(path); ok {
		return Resolved{Source: SourceOverride, Path: path, data: data}, true, nil
	}
	return Resolved{}, false, nil
}

// ReadDefault reads relativeName from the shipped default directory, falling
// back to the embedded last-resort copy when no installed share tree has it.
func ReadDefault(relativeName string) (Resolved, error) {
	if dir, err := DefaultDir(); err == nil {
		path := filepath.Join(dir, relativeName)
		if data, ok := readIfExists(path); ok {
			return Resolved{Source: SourceDefault, Path: path, data: data}, nil
		}
	}

	if data, err := rootassets.FS.ReadFile(relativeName); err == nil {
		return Resolved{Source: SourceEmbedded, data: data}, nil
	}

	return Resolved{}, fmt.Errorf("asset: %q not found in default directory or embedded fallback", relativeName)
}

func readIfExists(path string) ([]byte, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}
