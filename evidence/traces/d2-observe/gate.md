# D2 observe live gate (duo-d2-observe Stage C) — scaffold

Workplan locator: `duo-d2-observe/c-live-gate`. Binary under test: TBD.
Disposable Herdr session `d2oNN` under `/tmp/duo-d2oNN` (unique
`HERDR_SESSION`, isolated XDG quartet including `XDG_RUNTIME_DIR`).
Duo `--host herdr:$SOCK`. This gate is duo-d2-observe **Stage C**. It
does **not** claim the Stage 2 exit gate. It does **not** submit
notes/52. It is **not** a skill edit.

| Gate bullet | Evidence | Result |
|---|---|---|
| Herdr+Pi launch → `live` → send → `session show` is `idle` or `working` with a reason that is not header-mismatch-as-missing. `conversation list` returns the sent turn. | `pi-04-launch.txt`, `pi-05b-show-live.txt`, `pi-08-correlations.txt`, `pi-09-prompt-send.txt`, `pi-15-observe-poll.txt`, `pi-17-show-final.txt`, `pi-18-list-final.txt` | TBD |
| Pi `agent.session` correlation is a UUID (I-11), `transcript` is the host JSONL path. | `pi-08-correlations.txt` | TBD |
| Herdr+Claude launch → `live` → send → same observe bullets. File-not-yet wait is measured. | `claude-04-launch.txt`, `claude-05b-show-live.txt`, `claude-09-prompt-send.txt`, `claude-15-observe-poll.txt`, `claude-17-show-final.txt`, `claude-18-list-final.txt` | TBD |
| `view: recovering` is recorded; a `live` inspect still includes `condition`. | `pi-05b-show-live.txt`, `claude-05b-show-live.txt`, `*-17-show-final.txt` | TBD |
| Fake-host + fake-runtime still pass every cross-composition test | `01-fake-pair.txt` | TBD |
| `nix develop --command make check` is green, and `git diff --exit-code contracts/` is clean | `00-make-check.txt`; `02-contracts-clean.txt` | TBD |

## Known limits (not Stage 2 claims)

- Does not claim Stage 2. Nested D1 (Duo-launched orchestrator day) is out.
- tmux + Codex stay deferred.
- Herdr issue (notes/52) remains unsubmitted.
- Skill observe poll is Stage D, not this gate.

Verdict: TBD.
