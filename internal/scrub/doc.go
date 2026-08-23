// Package scrub is the single source for Duo's spawn-environment deny
// list and the two shapes a launcher needs to apply it.
//
// # Why this exists
//
// Conformance §8 (terminal-multiplexers/duo-vnext-integration-conformance.md,
// 2026-08-23 amendment) makes the scrubbed spawn a hard Stage-1 gate: a
// Duo-launched runtime that still carries an inherited agent-harness
// marker (CLAUDECODE, CLAUDE_CODE_CHILD_SESSION, a CLAUDE_-prefixed
// variable a wrapping Claude Code session set, or a generic AI_AGENT
// flag) silently disables its own transcript. The P7 probe session
// reproduced the failure signature live, through a Herdr pane that
// inherited its server's environment (notes/19-herdr-probes.md §0;
// review/05-close-report.md §1; docs/adapters/decisions.md, Herdr
// section, "Launch mapping is lossy, and the environment seam is the
// reason").
//
// # The single-source rule
//
// Markers and WildcardPrefixes in markers.go are the only place the deny
// list is written down. TestNoDuplicateMarkerLiteralsOutsideScrub greps
// every other .go file in the module for the exact marker literals and
// fails the build if it finds one — the internal/registry package's
// TestNoDuplicateOperationTableOutsideRegistry tripwire, applied here.
//
// # Two shapes, because one seam cannot express fail-closed scrubbing
//
// A direct child spawn (exec.Cmd.Env) and a Duo-owned Herdr server both
// take a plain environment slice: build it with Guard, which scrubs and
// then re-verifies its own output before handing it back, so a bug in
// the scrub logic becomes a refused launch instead of a leak.
//
// A Herdr *pane* cannot use that shape. Herdr's `env` map on
// workspace.create/pane.split can only set variables in the pane's
// shell; it cannot unset one the pane inherits from the Herdr server
// process (verified live, docs/adapters/decisions.md, Herdr section).
// So a Herdr-hosted spawn has exactly two sound options: launch the
// Herdr server itself scrubbed (shape (a), Guard on the server's own
// spawn environment — every pane then inherits clean), or, when Duo
// does not own the server's launch, wrap the pane's command so the
// scrub runs inside the pane at exec time (shape (b), PaneCommand).
//
// # Fail closed, not warn-and-continue
//
// Verify (and therefore Guard) returns an error the instant a marker
// survives; nothing in this package logs a warning and proceeds. A
// caller wiring this into a launcher must treat that error as a refused
// launch, per conformance §8's "hard Stage 1 gate."
//
// See docs/scrub/decisions.md for the shell-script design behind
// PaneCommand and the live paired-test evidence.
package scrub
