package herdr

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sessionroots.go reads the directory identities a Herdr session claims,
// for the launch materializer's cwd-correlation rung.
//
// The source is the session's persisted UI state,
// $XDG_CONFIG_HOME/herdr/sessions/<session>/session.json (Herdr 0.8.2,
// document version 3), whose workspaces each carry an `identity_cwd` — the
// directory the workspace was created for. It survives server restarts and
// exists for stopped sessions. Like instances.go, nothing here dials a
// socket (invariant I-3): a metadata file on disk is evidence, not an
// attestation that the server is alive.

// SessionFileName is the persisted session document inside a session
// directory, beside the API socket.
const SessionFileName = "session.json"

// sessionDocumentVersion is the session.json document version this reader
// understands. A foreign version yields no roots rather than a guess.
const sessionDocumentVersion = 3

// sessionDocument is the subset of Herdr's session.json this package
// reads. Everything else in the document is ignored on purpose.
type sessionDocument struct {
	Version    int `json:"version"`
	Workspaces []struct {
		IdentityCwd string `json:"identity_cwd"`
	} `json:"workspaces"`
}

// SessionRoots returns the directory identities the named session claims,
// sorted and deduplicated.
//
// Generic roots are filtered out: Herdr records `identity_cwd: $HOME` for
// a session started without a project directory (observed live on 3 of 11
// sessions), so the home directory, its ancestors, and the filesystem root
// are fallback markers, not identity — left in, they would claim every
// directory under home.
//
// Best-effort by design: a missing, unreadable, or malformed document, or
// one with a foreign version, is (nil, nil) — no roots for that session is
// an answer, and a broken metadata file must not fail a materialization.
// Only the failure to resolve the sessions directory itself is an error.
func SessionRoots(session string) ([]string, error) {
	dir, err := SessionsDir()
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(dir, session, SessionFileName))
	if err != nil {
		return nil, nil
	}
	var doc sessionDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil
	}
	if doc.Version != sessionDocumentVersion {
		return nil, nil
	}

	seen := map[string]bool{}
	var out []string
	for _, w := range doc.Workspaces {
		root := w.IdentityCwd
		if root == "" || genericRoot(root) || seen[root] {
			continue
		}
		seen[root] = true
		out = append(out, root)
	}
	sort.Strings(out)
	return out, nil
}

// genericRoot reports whether root is one of Herdr's no-project fallback
// locations: the filesystem root, the home directory, or an ancestor of
// the home directory.
func genericRoot(root string) bool {
	cleaned := filepath.Clean(root)
	if cleaned == string(filepath.Separator) {
		return true
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	home = filepath.Clean(home)
	if cleaned == home {
		return true
	}
	return strings.HasPrefix(home, cleaned+string(filepath.Separator))
}
