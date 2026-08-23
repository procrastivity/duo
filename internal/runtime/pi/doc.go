// Package pi is the Pi agent-runtime adapter, pinned to pi 0.83.0
// (@earendil-works/pi-coding-agent).
//
// # Scope
//
// Stage 1 implements exactly the two §5.3 interfaces internal/runtime
// defines — RuntimeCorrelator and ConversationProvider — plus the §5.1
// factory envelope. ConditionProvider, RuntimePromptProvider,
// UsageProvider, RuntimeConfigurationProvider, and HarnessRenderer are
// Stage 2+ and are deliberately not scaffolded here, for the same reason
// internal/runtime does not declare them: a stub would commit later
// sessions to field shapes nobody has designed.
//
// # Two channels
//
// Correlation comes from a generated in-process TypeScript extension
// (extension/duo-pi-reporter.ts, rendered by RenderExtension). Pi
// extensions are jiti-loaded modules whose handlers are awaited inside the
// agent loop, so the generated reporter serves one authenticated
// correlation record over a Unix socket and does nothing else. The session
// id, session file, and cwd it reports come from live ctx accessors
// (ctx.sessionManager.getSessionId / .getSessionFile, ctx.cwd) — values no
// out-of-process observer can read. The extension is a generated harness
// component, not part of the Go authority: its failure removes only its own
// paths (conformance §7.4).
//
// Conversation comes from Pi's own session JSONL under
// ~/.pi/agent/sessions/<cwd-slug>/<ts>_<uuid>.jsonl — the semantic read
// channel, parsed here without any terminal emulation. The parser is ported
// from apps/transcript-tail/adapter_pi.go in the research repo, whose
// assumptions were re-verified against 0.83.0 on 2026-08-23.
//
// # Three findings this package encodes
//
// The pane-session gate is ctx.mode === "tui". ctx.hasUI is true in rpc mode
// too (rpc installs a real extension UI context at 0.83.0), so hasUI alone
// does not stop an rpc-driven pi from masquerading as the pane's session.
// ReportedClaim.Validate re-checks the mode Duo-side rather than trusting
// the extension's own gate.
//
// session_shutdown is terminal only on reason == "quit". /reload, /new,
// /resume, and /fork tear down and rebind the extension runtime while the
// agent process lives on.
//
// The reporter credential is delivered in the environment, read at module
// load before any turn can run, and then deleted from process.env. That
// scrub keeps the token out of the tool subprocesses pi spawns; it does not
// hide it from other code in the same process, and the token therefore
// authenticates the process, not the extension.
//
// # Unsupported at 0.83.0 (Stage 2 constraints, recorded not worked around)
//
// Blocked condition evidence is structurally absent. Pi has no permission
// system and emits no blocked-family event; the only blocked channel is the
// cooperative `herdr:blocked` EventBus convention, and a 2026-08-23 sweep of
// the installed tree found zero emitters (one listener, no producer, and
// nothing in the pi dist). A Duo reporter cannot lift evidence that is not
// produced, so a future ConditionProvider must degrade this facet
// explicitly rather than infer it.
//
// Working mode is unsupported. Pi has no built-in working-mode concept; its
// --mode flag is an execution surface (tui/rpc/json/print) and plan behavior
// is extension-only. A future RuntimeConfigurationProvider must report the
// facet as unsupported rather than map --mode onto it.
//
// # Documented, not implemented
//
// Native prompt delivery works: a 0.83.0 probe proved that the same
// extension shape can deliver text as a real user turn labelled
// input.source "extension" (native provenance the PTY channel cannot
// produce) and can interrupt a running tool call, both over the same socket.
// That is Duo's Stage 2+ prompt surface. The generated asset stays read-only
// until the prompt-delivery contract lands, and extension_test.go guards
// that it has no delivery call in it.
//
// See docs/adapters/decisions.md for the decisions behind these choices and
// their evidence.
package pi
