package materialize

import (
	"path/filepath"
	"strings"
)

// normalizePath resolves symlinks and cleans p, so a recorded root and a
// live working directory compare equal even when one of them travelled
// through a symlink. A path that does not resolve — a fake in a test, a
// directory deleted since it was recorded — falls back to a plain Clean,
// which keeps the comparison total.
func normalizePath(p string) string {
	if resolved, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(p)
}

// pathWithin reports whether path equals root or sits strictly under it,
// on a path-separator boundary. Both arguments must already be normalized.
func pathWithin(path, root string) bool {
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// bestRootMatch finds the candidate whose claimed root contains path most
// deeply. The deepest (longest normalized) matching root wins outright;
// when several distinct instances match at the winning depth the result is
// a tie, returned as the tied candidates with no winner — the rung reports
// the ambiguity and yields nothing. An instance claiming the winning root
// more than once is not a tie with itself. path must be normalized; roots
// are normalized here.
func bestRootMatch(path string, cands []InstanceRoots) (winner *InstanceRoots, root string, tied []InstanceRoots) {
	best := -1
	bestRoot := ""
	var holders []*InstanceRoots
	for i := range cands {
		for _, r := range cands[i].Roots {
			nr := normalizePath(r)
			if !pathWithin(path, nr) {
				continue
			}
			switch {
			case len(nr) > best:
				best = len(nr)
				bestRoot = nr
				holders = holders[:0]
				holders = append(holders, &cands[i])
			case len(nr) == best:
				dup := false
				for _, h := range holders {
					if h == &cands[i] {
						dup = true
						break
					}
				}
				if !dup {
					holders = append(holders, &cands[i])
				}
			}
		}
	}
	switch len(holders) {
	case 0:
		return nil, "", nil
	case 1:
		return holders[0], bestRoot, nil
	default:
		out := make([]InstanceRoots, 0, len(holders))
		for _, h := range holders {
			out = append(out, *h)
		}
		return nil, bestRoot, out
	}
}
