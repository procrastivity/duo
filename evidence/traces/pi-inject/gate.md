# Pi runtime inject live gate (duo-pi-inject Stage C) — 2026-08-27

Workplan: wip matter `duo-pi-inject` stage `c-live-gate`.
Binary under test: `duo version 1678d2c` (branch `go`). Full suite:
`00-make-check.txt` (lint 0 issues, all packages pass). Live tools:
herdr 0.8.2, pi 0.84.3. Disposable Herdr session `dpi01` under
`/tmp/duo-dpi01` (unique `HERDR_SESSION`, isolated XDG including
`XDG_RUNTIME_DIR`, `setsid env -i` server, real HOME, no `CLAUDE_*` /
`CLAUDECODE` / `AI_AGENT` on the server). Duo `--host herdr:$SOCK`.
Torn down after. No live user session was touched. Pre-start herdr
PIDs still existed after stop.

This gate is duo-pi-inject Stage C. It does **not** claim the Stage 2
exit gate. It does **not** rewrite the 2026-08-26 §3c PASS. It does
**not** submit notes/52.

Each gate bullet maps to a command transcript or test name in this
directory.

| Gate bullet | Evidence | Result |
|---|---|---|
| Herdr+Pi launch via preset becomes `live` without waiting for Herdr `launch_pending` to clear. Active `agent.session` and `transcript` correlations are present. | `04-launch-pi.txt` exit 0 `ses_b5a7a3bb…`. `05-session-show-after-launch.txt` / `05b-session-show-live.txt`: `runtime_instance_state: live` at `elapsed_ms=0`. `08-correlations.txt`: active `agent.session` and `transcript` on the named Pi JSONL path; `instance.state` evidence `host reported agent-session identity and the runtime reports ready`. `06-herdr-agent-list.txt` still `launch_pending: true` at that moment. Tests: `TestPiLaunchPendingIdleMarksLive`. | PASS |
| `duo prompt send` delivers on the runtime inject path, not `agent.prompt`. | `09-prompt-send.txt` `responsibility_state: delivered` exit 0. `10-prompt-show.txt` native attempt `recorded_result: delivered`. `11-store-path-kind.txt` `command.attempt_created` / `command.delivered` `path_kind: runtime` (no host). `13-pane-read.txt`: prompt text, model `pong`, `[Extensions]` includes `duo-inject.ts`. I-15. | PASS |
| Fake-host + fake-runtime still pass every cross-composition test | `01-fake-pair.txt` `EXIT_CLI:0` / `EXIT_PACKAGES:0` | PASS |
| `nix develop --command make check` is green, and `git diff --exit-code contracts/` is clean | `00-make-check.txt` (exit 0); `02-contracts-clean.txt` (`EXIT:0`; `contracts/SOURCE` `7162572`) | PASS |
| Herdr still `launch_pending` after deliver is a named remainder, not a fail | `06-herdr-agent-list.txt`: pending true at bind. `14-herdr-launch-pending-remainder.txt`: after deliver `launch_pending=False`, `agent_status=idle`, `screen_detection_skipped=True`. Cleared honestly; not a Herdr-fix claim. | PASS (named: pending at bind, cleared after deliver) |

## Known limits (not Stage 2 claims)

- Does not claim Stage 2. Nested D1 (Duo-launched orchestrator day) is out.
- tmux + Codex stay deferred.
- Herdr issue (notes/52) remains unsubmitted.
- `07-inject-socket.txt` ls missed: the live capture trimmed `.jsonl` as five bytes and left a trailing `.` on the uuid. Inject is still proven by pane `-e duo-inject.ts` and store `path_kind: runtime`. `capture-live.sh` now uses `removesuffix(".jsonl")`.
- `session show` `condition.value: unknown` / `missing transcript` while the path-shaped transcript correlation is active. Not a C bullet.
- `view` stayed `recovering` on a `live` inspect. Same as §3c.

Verdict: duo-pi-inject Stage C passes. Herdr+Pi launch is `live` via idle-as-ready while Herdr may still report `launch_pending`; `prompt send` delivers on the runtime inject path. Claude D3 is unchanged. Stage 2 is not claimed.
