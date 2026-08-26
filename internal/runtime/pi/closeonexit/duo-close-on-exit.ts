// duo-close-on-exit.ts — Duo's generated Pi close-on-exit extension.
//
// GENERATED FILE. Duo writes this from the asset embedded in
// github.com/procrastivity/duo internal/runtime/pi/closeonexit and rewrites
// it on every materialization. Local edits are lost.
//
// Target: pi 0.83.0 (@earendil-works/pi-coding-agent). This file is not
// installed into ~/.pi/agent/extensions/ — nothing does that for the
// shipped reporter extension either (internal/runtime/pi's package doc
// calls installation someone else's concern, and that someone was never
// built: RenderExtension/ExtensionFileName have zero non-test call sites).
// Instead this asset is materialized per launch resolution and per leaf
// (internal/runtime/pi/closeonexit.go's MaterializeCloseOnExit) and loaded
// with pi's own `-e <path>` flag for that one launch only (pi 0.83.0
// `pi --help`: `--extension, -e <path>` loads an extension file for one
// launch, repeatable, and an explicit `-e` path still loads even under
// `--no-extensions`). One materialized file, one `-e` argument, one pi
// process — this extension never has to distinguish "the pane's session"
// from some other pi invocation the way the reporter does, because no
// other invocation can ever load it.
//
// Deliberately separate from duo-pi-reporter.ts, not a second
// responsibility bolted onto it: close-on-exit must not drag the
// reporter's credential/socket concerns into a minimal, close-only
// extension, and the reporter's own close block (still present, guarded by
// the same DUO_CLOSE_PANE_ON_EXIT flag, for whenever reporter installation
// gets built) stays untouched. A future double-load — this asset AND an
// eventually-installed reporter both closing the same pane — is not a
// failure: the second `herdr pane close` call gets `pane_not_found`, an
// absence, not an error, because the pane the first call closed is simply
// already gone.

import { execFileSync } from "node:child_process";

// Duo does not build against pi's published typings at generation time, so
// this local alias, not an import, is what this file touches pi's API
// through.
type PiAPI = { on: (event: string, handler: (event: any, ctx: any) => void) => void };

// Read once at module load, before any turn can run, in one place — the
// same discipline the reporter's SOCKET_PATH/TOKEN follow (notes/18 §6),
// verified here by extension_test.go's no-late-env-access assertions.
// Unlike the reporter's credential, none of these four name a secret: an
// on/off flag and Herdr's own pane identifiers, not a Duo credential — so,
// same reasoning as duo-pi-reporter.ts's CLOSE_PANE_ON_EXIT block, nothing
// here scrubs them. They are also HERDR_*'s and Herdr's environment to
// keep or remove, not this asset's.
//
// DUO_CLOSE_PANE_ON_EXIT lasts the pane's life, so a second pi started by
// hand in that pane also closes it on quit (notes/51 record 9c).
const DUO_CLOSE_PANE_ON_EXIT = process.env["DUO_CLOSE_PANE_ON_EXIT"] === "1";
const HERDR_ENV = process.env["HERDR_ENV"] === "1";
const HERDR_PANE_ID = process.env["HERDR_PANE_ID"] ?? "";
const HERDR_BIN_PATH = process.env["HERDR_BIN_PATH"] ?? "";

// No ctx.mode gate. The reporter (duo-pi-reporter.ts) gates session_start
// on `ctx.mode !== "tui"` because it is installed globally and must reject
// an rpc-driven pi masquerading as the pane's session (notes/18 §2,
// conformance §7.4) — a discovery-directory problem. This asset is never
// discovered: it reaches exactly one process through that process's own
// `-e <path>` launch argument, and a Herdr-launched leaf is always a tui
// pane, so there is no second pi invocation this file could ever be
// mistaken for. pi.on("session_shutdown") also does not receive a ctx
// argument at all (see duo-pi-reporter.ts's own handler), so a mode gate
// here would need its own session_start handler just to capture one — for
// a discriminator this asset does not need.
export default function (pi: PiAPI) {
  pi.on("session_shutdown", (event: any) => {
    // Terminality: only `quit` means the agent process is going away.
    // /reload, /new, /resume, and /fork rebind the extension runtime while
    // the agent process lives on — closing the pane on one of those would
    // yank it out from under a still-live agent (notes/07-pi.md §2,
    // notes/18 §2 — the same trap duo-pi-reporter.ts documents).
    if (event?.reason !== "quit") return;
    if (!DUO_CLOSE_PANE_ON_EXIT || !HERDR_ENV) return;
    if (!HERDR_PANE_ID || !HERDR_BIN_PATH) return;
    try {
      // Synchronous, on purpose: live probing (2026-08-24) found pi's own
      // teardown signal-kills this extension's child process on a quit
      // shutdown, so execFileSync's local exit-status read is untrustworthy
      // by the time this call returns — but the herdr `pane close` RPC
      // itself has already landed by then. An async call would race the
      // same teardown and might never even start. Every error is
      // swallowed: a failed close must never break pi shutdown, and pi's
      // signal-kill of the child is exactly the "error" this try/catch
      // exists to absorb without surfacing it.
      execFileSync(HERDR_BIN_PATH, ["pane", "close", HERDR_PANE_ID], { stdio: "ignore" });
    } catch {
      // fire-and-forget: same contract as duo-pi-reporter.ts's stop().
    }
  });
}
