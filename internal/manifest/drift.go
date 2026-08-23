package manifest

import "sort"

// Drifted is one entry in Drift's report: a generated file whose current
// content no longer matches what a prior install stamped.
type Drifted struct {
	// Path is the generated file's path, relative to the install directory.
	Path string `json:"path"`
	// Reason distinguishes why the file drifted: "added" (the current
	// binary would generate it but the stamp predates it), "removed" (the
	// stamp has it but the current binary no longer generates it), or
	// "changed" (both have it, checksums differ).
	Reason string `json:"reason"`
}

// Drift compares want — the checksums the current binary would generate —
// against a prior install's stamp, and reports every file whose content
// moved. This package exposes the comparison and does not itself wire it
// into any verb.
func Drift(want map[string]string, stamp Stamp) []Drifted {
	var out []Drifted
	for path, sum := range want {
		if stampSum, ok := stamp.Files[path]; !ok {
			out = append(out, Drifted{Path: path, Reason: "added"})
		} else if stampSum != sum {
			out = append(out, Drifted{Path: path, Reason: "changed"})
		}
	}
	for path := range stamp.Files {
		if _, ok := want[path]; !ok {
			out = append(out, Drifted{Path: path, Reason: "removed"})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
