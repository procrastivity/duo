package scrub

import "strings"

// Markers lists the exact-name environment-variable markers Duo scrubs
// from every spawn environment. Each is a signal a wrapping agent harness
// (or a prior Duo launch) leaves behind that an agent runtime — Claude
// Code chief among them — reads as "I am already someone else's child"
// and responds to by silently disabling its own transcript writing
// (conformance §8; notes/19-herdr-probes.md §0).
//
// This is the single source for these three names. Nothing outside this
// package may hardcode one of them; TestNoDuplicateMarkerLiteralsOutsideScrub
// enforces that mechanically.
var Markers = []string{
	// CLAUDE_CODE_CHILD_SESSION is the specific marker conformance §8
	// names by name: a Claude Code process sets it on its own children,
	// and a Claude Code process that inherits it from its own parent
	// concludes a parent session already owns the transcript and stops
	// writing one.
	"CLAUDE_CODE_CHILD_SESSION",
	// CLAUDECODE is Claude Code's own "I am running under Claude Code"
	// flag. It has no underscore after the prefix, so the CLAUDE_
	// wildcard below does not catch it; it needs its own entry.
	"CLAUDECODE",
	// AI_AGENT is a generic cross-tool "an agent harness owns this
	// process" flag some runtimes set and others read.
	"AI_AGENT",
}

// WildcardPrefixes lists environment-variable name prefixes scrubbed in
// full, regardless of the exact suffix: CLAUDE_CODE_ENTRYPOINT,
// CLAUDE_CONFIG_DIR, CLAUDE_CODE_MESSAGING_TOKEN, and any future
// CLAUDE_-prefixed variable a harness or a prior Duo launch sets. The
// wildcard exists because the exact set of CLAUDE_* names Claude Code
// exports has already changed across versions this project tracked
// (docs/adapters/decisions.md, Claude Code adapter section) and cannot be
// enumerated once and for all.
var WildcardPrefixes = []string{
	"CLAUDE_",
}

// IsMarker reports whether name — an environment variable's bare name,
// with no "=value" — is a marker this package scrubs.
func IsMarker(name string) bool {
	for _, m := range Markers {
		if name == m {
			return true
		}
	}
	for _, p := range WildcardPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}
