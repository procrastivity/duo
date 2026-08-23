// duo-pi-reporter.ts — Duo's generated Pi reporter extension.
//
// GENERATED FILE. Duo writes this from the asset embedded in
// github.com/procrastivity/duo internal/runtime/pi and rewrites it on every
// install. Local edits are lost.
//
// Target: pi 0.83.0 (@earendil-works/pi-coding-agent). Pi extensions are
// in-process TypeScript modules (jiti-loaded, `export default function (pi)`)
// whose handlers are AWAITED inside the agent loop. A slow handler stalls the
// user's turn, so every handler below is fire-and-forget: it starts or stops
// I/O and returns, and it never awaits a socket.
//
// What this extension is for at Stage 1: it serves ONE correlation record —
// the live session id, session file, and cwd that only the in-process runtime
// knows — to Duo over a Unix socket, authenticated by a per-runtime-instance
// credential. It reports nothing else. It is a generated harness component,
// not part of the Go authority: if it fails, only its own paths disappear
// (conformance §7.4).
//
// Documented capability, deliberately NOT implemented here: the same
// extension shape can also deliver prompts natively. A probe at 0.83.0 proved
// that pi.sendUserMessage delivers injected text as a real user turn labelled
// `input.source: "extension"` (native provenance the PTY channel cannot
// produce), and that ctx.abort interrupts a running tool call over the same
// socket (notes/18 §5). That is Duo's Stage 2+ prompt surface; this asset
// stays read-only until the prompt-delivery contract lands.

import { createServer } from "node:net";

// Duo does not build against pi's published typings at generation time, so the
// two shapes this file touches are local aliases, not imports.
type PiAPI = { on: (event: string, handler: (event: any, ctx: any) => void) => void };
type Ctx = any;

const PROTOCOL = "__DUO_PROTOCOL__";

// Credential handling — probe-verified shape (notes/18 §6). Duo's spawn
// builder constructs the whole environment (conformance §8) and adds two
// variables for this instance: the reporter socket path and a per-instance
// token. Read BOTH at module load, which happens before any turn can run,
// hold them in module scope, then delete them from process.env. The scrub is
// what keeps the token out of every tool subprocess pi later spawns; it does
// not, and cannot, hide it from other code in this process.
const SOCKET_PATH = process.env["__DUO_SOCKET_ENV__"] ?? "";
const TOKEN = process.env["__DUO_TOKEN_ENV__"] ?? "";
delete process.env["__DUO_SOCKET_ENV__"];
delete process.env["__DUO_TOKEN_ENV__"];

export default function (pi: PiAPI) {
  let server: any = null;
  let servedSessionId: string | null = null;
  let startReason = "";
  let lastSettledAt = "";

  const claimLine = (ctx: Ctx): string =>
    JSON.stringify({
      protocol: PROTOCOL,
      token: TOKEN,
      sessionId: safe(() => ctx.sessionManager.getSessionId()),
      sessionFile: safe(() => ctx.sessionManager.getSessionFile()),
      cwd: ctx.cwd ?? "",
      mode: ctx.mode ?? "",
      hasUI: ctx.hasUI === true,
      idle: safe(() => ctx.isIdle()) === true,
      startReason: startReason,
      lastSettledAt: lastSettledAt,
      pid: process.pid,
    }) + "\n";

  pi.on("session_start", (event: any, ctx: Ctx) => {
    // Pane-session gate. `ctx.mode === "tui"` is the ONLY safe root-session
    // discriminator at 0.83.0: rpc mode installs a real extension UI context,
    // so ctx.hasUI is true there too and does not exclude an rpc-driven pi
    // masquerading as the pane's session (notes/18 §2, conformance §7.4).
    // Duo attaches pane sessions at Stage 1; a future rpc-driven launch
    // variant must widen this gate deliberately, not by accident.
    if (ctx.mode !== "tui") return;
    if (SOCKET_PATH === "" || TOKEN === "") return;

    const sessionId = safe(() => ctx.sessionManager.getSessionId());
    // Rebind dedupe: an rpc `new_session` produces two `session_start` events
    // for the same session in the same millisecond, and /reload rebinds the
    // extension runtime without a new session (notes/18 §2). Serving is
    // idempotent per session id.
    if (sessionId && sessionId === servedSessionId) return;
    servedSessionId = sessionId;
    startReason = event?.reason ?? "";

    stop();
    try {
      server = createServer((socket: any) => {
        socket.on("error", () => {});
        socket.end(claimLine(ctx));
      });
      server.on("error", () => {
        server = null;
      });
      // unref: the reporter never keeps pi alive on its own.
      server.listen(SOCKET_PATH, () => server?.unref?.());
    } catch {
      server = null;
    }
  });

  // Idle edge: `agent_settled`, never `agent_end`. After agent_end pi may
  // still auto-retry, auto-compact and retry, or continue with a queued
  // follow-up; agent_settled is emitted in a `finally`, so it also fires
  // after a user interrupt (notes/07-pi.md §1). Stage 1 only stamps the
  // record — condition observation is Stage 2.
  pi.on("agent_settled", (_event: any, ctx: Ctx) => {
    if (safe(() => ctx.isIdle()) === true) lastSettledAt = new Date().toISOString();
  });

  pi.on("session_shutdown", (event: any) => {
    // Terminality: only `quit` means the agent process is going away. Pi tears
    // down and rebinds extension runtimes for /reload, /new, /resume and
    // /fork, and releasing on those suppresses the replacement runtime's
    // reports (notes/07-pi.md §2, notes/18 §2).
    if (event?.reason !== "quit") return;
    servedSessionId = null;
    stop();
  });

  function stop() {
    try {
      server?.close?.();
    } catch {
      // fire-and-forget: a reporter failure never surfaces in the user's turn.
    }
    server = null;
  }
}

function safe<T>(read: () => T): T | "" {
  try {
    return read();
  } catch {
    return "";
  }
}
