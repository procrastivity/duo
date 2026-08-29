package devin

import (
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
