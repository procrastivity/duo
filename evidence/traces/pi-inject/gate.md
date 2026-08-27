# Pi inject live gate (Stage C) — 2026-08-27

Workplan: wip matter `duo-pi-inject` stage `c-live-gate` step 01
(terminal-multiplexers `.wip/generated/duo-pi-inject/workplan-c-live-gate.md`).
Binary under test: TBD (built at step 02). Disposable Herdr session `dpiNN`
under `/tmp/duo-dpiNN/` (unique `HERDR_SESSION`, scratch XDG quartet including
`XDG_RUNTIME_DIR`, Duo client `--host herdr:$SOCK`, no ambient `HERDR_*` on
the Duo client). Torn down after. No live user session was touched.

This gate is duo-pi-inject Stage C. It does **not** claim Stage 2. It does
**not** rewrite the 2026-08-26 §3c PASS entry. It does **not** submit
notes/52.

Each gate bullet maps to a command transcript or test name in this directory.

| Gate bullet | Evidence | Result |
|---|---|---|
| Herdr+Pi launch via preset becomes `live` without waiting for Herdr `launch_pending` to clear. Active `agent.session` and `transcript` correlations are present. | `04-launch-pi.txt`, `05-session-show-after-launch.txt`, `05b-session-show-live.txt`, `08-correlations.txt`. Tests already sealed B: `TestPiLaunchPendingIdleMarksLive`. | TBD |
| `duo prompt send` delivers on the runtime inject path, not `agent.prompt`. | `09-prompt-send.txt` `responsibility_state: delivered`; `10-prompt-show.txt`; `11-store-path-kind.txt` `"path_kind":"runtime"` on the delivered attempt; `13-pane-read.txt` prompt text + `duo-inject.ts`. I-15. | TBD |
| Fake-host + fake-runtime still pass every cross-composition test | `01-fake-pair.txt` | TBD |
| `nix develop --command make check` is green, and `git diff --exit-code contracts/` is clean | `00-make-check.txt`; `02-contracts-clean.txt` | TBD |
| Herdr still `launch_pending` after deliver is a named remainder, not a fail | `06-herdr-agent-list.txt` / `14-herdr-launch-pending-remainder.txt`. Expected. | TBD |

## Known limits (not Stage 2 claims)

- Does not claim Stage 2. Nested D1 (Duo-launched orchestrator day) is out.
- tmux + Codex stay deferred.
- Herdr issue (notes/52) remains unsubmitted.

Verdict: TBD (orchestrator fills after live run).
