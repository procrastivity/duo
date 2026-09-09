package devin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ATIFPath is the Duo-owned ATIF export file for one Devin leaf:
// $XDG_DATA_HOME/duo/devin-atif/<launch-resolution-id>/<leaf>.json,
// falling back to ~/.local/share/duo/devin-atif/... when XDG_DATA_HOME
// is unset. Empty leaf names the file <id>.json directly under
// devin-atif/. The path is a locator, not a directory-newest scan (I-6).
//
// stage1LeafAugmenter passes this path as `devin --export`. Bind stores
// it as TranscriptID when Correlate leaves the field empty.
func ATIFPath(launchResolutionID, leaf string) (string, error) {
	if launchResolutionID == "" {
		return "", fmt.Errorf("devin: ATIF path needs a launch-resolution ID")
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("devin: resolving XDG_DATA_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	if leaf == "" {
		return filepath.Join(base, "duo", "devin-atif", launchResolutionID+".json"), nil
	}
	return filepath.Join(base, "duo", "devin-atif", launchResolutionID, leaf+".json"), nil
}

// SessionIDFromExport recovers the agent-session id a print-mint launch
// minted, after the process that wrote it is gone. `devin --export
// <path> --print <prompt>` writes the ATIF export and exits; a later
// CLI step reads path (built via ATIFPath) to learn what Devin minted
// for that turn. It never scans a directory for the newest export (I-6)
// — the caller supplies the exact path.
//
// A missing export file or a document with no session_id is an honest
// miss, not an error to act on: the mint may not have landed yet, or
// this ATIF document simply carries no session_id. SessionIDFromExport
// returns ("", nil) for both, matching how ObserveCondition treats a
// missing or unreadable transcript as unknown rather than a failure.
// Any other read or parse failure is still returned as an error.
func SessionIDFromExport(path string) (string, error) {
	doc, err := readATIF(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	return doc.SessionID, nil
}
