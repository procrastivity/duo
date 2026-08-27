# D2 observe live gate (duo-d2-observe Stage C) — 2026-08-27

Workplan locator: `duo-d2-observe/c-live-gate`. Binary under test:
`duo version 728dbdd-dirty` (branch `go`; `-dirty` is unrelated
`evidence/traces/recovery/fixture-reconcile.json`). Live tools: herdr
0.8.2, pi 0.84.3. Disposable Herdr session `d2o01` (Pi) under
`/tmp/duo-d2o01` (unique `HERDR_SESSION`, isolated XDG quartet
including `XDG_RUNTIME_DIR`, `setsid env -i` server, real HOME). Duo
`--host herdr:$SOCK`. Torn down after. No live user session was
touched. Pre-start herdr PIDs still existed after stop.

This gate is duo-d2-observe **Stage C**. It does **not** claim the
Stage 2 exit gate. It does **not** submit notes/52. It is **not** a
skill edit.

| Gate bullet | Evidence | Result |
|---|---|---|
| Herdr+Pi launch → `live` → send → `session show` is `idle` or `working` with a reason that is not header-mismatch-as-missing. `conversation list` returns the sent turn. | `pi-04-launch.txt` exit 0 `ses_1c1fa196…`. `pi-05b-show-live.txt` `live` at `elapsed_ms=0`. `pi-09-prompt-send.txt` `delivered`. `pi-15-observe-poll.txt` `outcome=PASS` at `elapsed_ms=2000`: `idle` / `inferred from stopReason stop (lastSettledAt analog)` (not `missing transcript` once the JSONL existed). `pi-18-list-final.txt` user prompt + assistant `pong`. Early poll samples `unknown`/`missing transcript` with `host_jsonl_exists=false` (file-not-yet, ~2s). | PASS |
| Pi `agent.session` correlation is a UUID (I-11), `transcript` is the host JSONL path. | `pi-08-correlations.txt`: `agent.session` `01a04229-d66b-7fd0-a933-cdb29d487a96` (`agent.session_classification=uuid`); `transcript` host path `…/2026-08-27T07-40-33-260Z_01a04229-….jsonl`. Herdr `agent.session` remains path-kind (`pi-06-agent-list.txt`); Duo stores the peeled UUID. | PASS |
| Herdr+Claude launch → `live` → send → same observe bullets. File-not-yet wait is measured. | `claude-04-launch.txt`, `claude-05b-show-live.txt`, `claude-09-prompt-send.txt`, `claude-15-observe-poll.txt`, `claude-17-show-final.txt`, `claude-18-list-final.txt` | TBD |
| `view: recovering` is recorded; a `live` inspect still includes `condition`. | `pi-05b-show-live.txt` / `pi-17-show-final.txt`: `view: recovering` with `condition` present. Claude TBD. | TBD |
| Fake-host + fake-runtime still pass every cross-composition test | `01-fake-pair.txt` `EXIT_CLI:0` / `EXIT_PACKAGES:0` | PASS |
| `nix develop --command make check` is green, and `git diff --exit-code contracts/` is clean | `00-make-check.txt` `EXIT:0`; `02-contracts-clean.txt` `EXIT:0` | PASS |

## Known limits (not Stage 2 claims)

- Does not claim Stage 2. Nested D1 (Duo-launched orchestrator day) is out.
- tmux + Codex stay deferred.
- Herdr issue (notes/52) remains unsubmitted.
- Skill observe poll is Stage D, not this gate.

Verdict: TBD.
