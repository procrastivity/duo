// duo-inject.ts — Duo's generated Pi inject extension.
//
// GENERATED FILE. Duo writes this from the asset embedded in
// github.com/procrastivity/duo internal/runtime/pi/inject and rewrites
// it on every materialization. Local edits are lost.
//
// Target: pi 0.83.0 (@earendil-works/pi-coding-agent). This file is not
// installed into ~/.pi/agent/extensions/ — nothing does that for the
// shipped reporter extension either (internal/runtime/pi's package doc
// calls installation someone else's concern, and that someone was never
// built: RenderExtension/ExtensionFileName have zero non-test call sites).
// Instead this asset is materialized per launch resolution and per leaf
// (internal/runtime/pi/inject.go's MaterializeInject) and loaded with
// pi's own `-e <path>` flag for that one launch only (pi 0.83.0
// `pi --help`: `--extension, -e <path>` loads an extension file for one
// launch, repeatable, and an explicit `-e` path still loads even under
// `--no-extensions`). One materialized file, one `-e` argument, one pi
// process — this extension never has to distinguish "the pane's session"
// from some other pi invocation the way the reporter does, because no
// other invocation can ever load it.
//
// Deliberately separate from duo-pi-reporter.ts and from
// duo-close-on-exit.ts: inject is prompt delivery, not correlation
// reporting and not pane close.
//
// No ctx.mode gate. This asset is loaded with `-e` exclusive to this
// process; abort is out of this stage; idle is a connect-line byproduct,
// not an `agent_settled` push.

import { createServer } from "node:net";
import { mkdirSync, unlinkSync } from "node:fs";
import { dirname } from "node:path";

// Duo does not build against pi's published typings at generation time, so
// this local alias, not an import, is what this file touches pi's API
// through.
type PiAPI = { on: (event: string, handler: (event: any, ctx: any) => void) => void; sendUserMessage: (text: string) => void };

// Read once at module load, before any turn can run, in one place — the
// same discipline the reporter's SOCKET_PATH/TOKEN follow (notes/18 §6).
// Unlike the reporter's credential, neither names a secret: a socket path
// override and XDG's runtime directory, not a Duo credential — so nothing
// here scrubs them.
const DUO_PI_SOCK = process.env["DUO_PI_SOCK"] ?? "";
const XDG_RUNTIME_DIR = process.env["XDG_RUNTIME_DIR"] ?? "";

export default function (pi: PiAPI) {
  let server: any = null;
  let servedSessionId: string | null = null;

  function runtimeDir(): string {
    if (XDG_RUNTIME_DIR !== "") return XDG_RUNTIME_DIR;
    return `/run/user/${process.getuid()}`;
  }

  function socketPathFor(sessionId: string): string {
    if (DUO_PI_SOCK !== "") return DUO_PI_SOCK;
    return `${runtimeDir()}/duo/pi-inject/${sessionId}.sock`;
  }

  const claimLine = (ctx: any): string =>
    JSON.stringify({
      sessionId: safe(() => ctx.sessionManager.getSessionId()),
      sessionFile: safe(() => ctx.sessionManager.getSessionFile()),
      cwd: ctx.cwd ?? "",
      hasUI: ctx.hasUI === true,
      mode: ctx.mode ?? "",
      idle: safe(() => ctx.isIdle()) === true,
    }) + "\n";

  pi.on("session_start", (event: any, ctx: any) => {
    const sessionId = safe(() => ctx.sessionManager.getSessionId());
    if (sessionId && sessionId === servedSessionId) return;
    if (!sessionId && DUO_PI_SOCK === "") return;
    servedSessionId = sessionId;

    stop();
    const path = socketPathFor(sessionId || "");
    try {
      mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
      try {
        unlinkSync(path);
      } catch {
        // stale sock
      }
      server = createServer((socket: any) => {
        socket.on("error", () => {});
        socket.write(claimLine(ctx));
        socket.setEncoding("utf8");
        let remainder = "";
        socket.on("data", (chunk: string) => {
          remainder += chunk;
          const lines = remainder.split("\n");
          remainder = lines.pop() ?? "";
          for (const line of lines) {
            if (line === "") continue;
            try {
              const parsed = JSON.parse(line);
              if (typeof parsed?.text === "string") {
                pi.sendUserMessage(parsed.text);
              }
            } catch {
              // swallow parse errors
            }
          }
        });
      });
      server.on("error", () => {
        server = null;
      });
      server.listen(path, () => server?.unref?.());
    } catch {
      server = null;
    }
  });

  pi.on("session_shutdown", (event: any) => {
    if (event?.reason !== "quit") return;
    servedSessionId = null;
    stop();
  });

  function stop() {
    try {
      server?.close?.();
    } catch {
      // fire-and-forget: an inject failure never surfaces in the user's turn.
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
