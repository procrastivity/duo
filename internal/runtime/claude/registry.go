package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// registryEntry is the subset of `~/.claude/sessions/<pid>.json` this
// adapter reads. The full 2.1.240 shape (notes/16 §6) carries more fields
// (startedAt, procStart, peerFeatures, name, nameSource, nameSince,
// status, updatedAt, statusUpdatedAt, bridgeSessionId, tmux, ...);
// SessionID is load-bearing for Correlate's inferred-confidence lookup,
// and PID plus MessagingSocketPath locate the per-session inbox for
// RuntimePromptProvider. This is undocumented, version-fragile surface
// (notes/16 §6: "still undocumented, still inferred-cap") —
// json.Unmarshal already ignores fields it does not know about, and an
// individual file that fails to parse at all is skipped rather than
// treated as a lookup failure (see registryHasSession).
type registryEntry struct {
	PID                 int    `json:"pid"`
	SessionID           string `json:"sessionId"`
	MessagingSocketPath string `json:"messagingSocketPath"`
}

// registryHasSession reports whether any file under
// `<claudeDir>/sessions/*.json` names sessionID as its `sessionId` field.
// A missing sessions directory (registry absent, or this Claude Code
// version does not write one) is "no evidence," not an error — the
// registry is an OPTIONAL source (Step 18 spec). An error is returned
// only for a filesystem failure other than "the directory does not
// exist," since that is the one case a caller might reasonably want to
// distinguish from an honest empty result.
func (r *Runtime) registryHasSession(sessionID string) (bool, error) {
	_, ok, err := r.registryLookup(sessionID)
	return ok, err
}

// registryLookup returns the first parseable sessions/*.json entry whose
// sessionId matches. Missing directory is "no evidence," not an error.
func (r *Runtime) registryLookup(sessionID string) (registryEntry, bool, error) {
	dir := filepath.Join(r.claudeDir, "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return registryEntry{}, false, nil
		}
		return registryEntry{}, false, err
	}

	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, de.Name()))
		if err != nil {
			// One unreadable file (permissions, a race with the file
			// being removed on process exit — notes/16 §6: the registry
			// file is removed on clean exit) does not fail the whole
			// lookup; this is best-effort, inferred-grade evidence by
			// design.
			continue
		}
		var entry registryEntry
		if json.Unmarshal(raw, &entry) != nil {
			continue
		}
		if entry.SessionID == sessionID {
			return entry, true, nil
		}
	}
	return registryEntry{}, false, nil
}
