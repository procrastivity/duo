package doctor

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// DefaultHarnessRoot resolves the generated close-on-exit harness tree:
// $XDG_DATA_HOME/duo/harness, falling back to ~/.local/share/duo/harness
// when XDG_DATA_HOME is unset — the same XDG convention DefaultStorePath
// and the runtime adapters' DefaultHarnessDir use. Direct children are
// launch-resolution ids; each holds per-leaf materializations.
func DefaultHarnessRoot() (string, error) {
	base, err := xdgDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "duo", "harness"), nil
}

// KeepHarnessDir reports whether a launch-resolution id still has a live
// (non-terminal) runtime instance whose close-on-exit files must remain.
// The caller supplies authority knowledge; this package does not import
// internal/domain or dial anything (I-3).
type KeepHarnessDir func(launchResolutionID string) bool

// HarnessSweep is the result of one SweepHarnessDirs pass.
type HarnessSweep struct {
	// Reaped is the number of launch-resolution directories deleted.
	Reaped int `json:"reaped"`
	// Kept is the number of launch-resolution directories left in place
	// because KeepHarnessDir returned true.
	Kept int `json:"kept"`
	// IDs are the reaped launch-resolution directory names, sorted.
	IDs []string `json:"ids,omitempty"`
}

// SweepHarnessDirs deletes immediate subdirectories of root whose
// launch-resolution id KeepHarnessDir does not keep. A missing root is a
// successful no-op. Non-directories at the root are left alone. The sweep
// does not walk into kept directories, does not reap panes, and does not
// invent container_closed facts.
//
// keep may be nil, which reaps every launch-resolution directory (no live
// session can be identified without a predicate).
func SweepHarnessDirs(root string, keep KeepHarnessDir) (HarnessSweep, error) {
	out := HarnessSweep{IDs: []string{}}
	if root == "" {
		return out, fmt.Errorf("doctor: harness sweep needs a root")
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("doctor: reading harness directory %s: %w", root, err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if name == "" || name == "." || name == ".." {
			continue
		}
		if !entry.IsDir() {
			continue
		}
		// ReadDir returns base names; reject anything that would escape
		// root if joined (defensive: a name containing a separator).
		if filepath.Base(name) != name {
			continue
		}
		if keep != nil && keep(name) {
			out.Kept++
			continue
		}
		target := filepath.Join(root, name)
		if err := os.RemoveAll(target); err != nil {
			return out, fmt.Errorf("doctor: reaping harness directory %s: %w", target, err)
		}
		out.Reaped++
		out.IDs = append(out.IDs, name)
	}
	sort.Strings(out.IDs)
	return out, nil
}
