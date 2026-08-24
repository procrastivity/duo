# duo.config/v3 exit gate (Stage 1v3) — 2026-08-24

Workplan: wip matter `duo-config-v3` step 17 (terminal-multiplexers).
Binary under test: `duo version a1e1cf9` (branch `go`). Full suite:
`00-make-check.txt` (lint 0 issues, all packages pass). Live tools:
herdr 0.8.2, claude 2.1.241, pi 0.83.0 — the Stage 1 re-pinned set.

Live legs ran against two disposable Herdr servers (`gate17a`,
`gate17b`) under an isolated `XDG_CONFIG_HOME`/`XDG_DATA_HOME`/
`XDG_STATE_HOME`, started with a scrubbed (`env -i`) environment, torn
down after. No live user session was touched. The user's step-24 draft
config does not exist yet, so bullet 1 used the old v2 fixture
(`git show ce0a12c:fixtures/duo-external-v1/config.json`), as the
Workplan allows.

| Gate bullet | Evidence | Result |
|---|---|---|
| Migrate a v2 document; `--write` refuses until families authored; result loads | `01-migrate.txt`: stdout preview reports `model_family: manual`; `--write` refuses exit 3 naming variant `review`; `--set-model-family review=gpt --write` succeeds with the rename/state-to-bind report; the migrated document loads and resolves (`session launch --dry-run` reaches resolution) | PASS |
| Cold start, fresh workspace, Herdr pane: ambient-env deduction, confirmation asked, bind written, show and doctor agree | `02-cold-start-bind.txt`: `[y/N]` prompt on an interactive TTY, `workspace.host_bound` `fact_e03afa…`, `ses_9385a3…`/`lrr_5e0707…`, claude/sonnet-5 selected; `workspace host show` and `doctor --json` print the same binding and fact ID | PASS |
| Second launch, same workspace, unrelated pane: correlation beats env, outranked capture named | `03-correlation-beats-env.txt`: env points at `gate17b`; correlation chooses `gate17a`; both `HERDR_SOCKET_PATH` and `HERDR_SESSION` captures named as outranked, rebind pointer printed; pi/gpt-5.6 launches (`ses_54074b…`) | PASS |
| `duo workspace host rebind` to another socket records both instances with fingerprints | `04-rebind.txt`: `workspace.host_rebound` `fact_933136…` carries `previous_host` (gate17a, full fingerprint) and `host` (gate17b, session-name fingerprint); a fingerprint-less attempt refuses (kept in the transcript) | PASS |
| `duo provider disable codex` exhausts a codex-only preset under `launch.no_eligible_candidate` with tallies and pointers; enable restores | `05-provider-disable.txt`: two `provider_disabled` eliminations, tally `count:2`, deduced host with outranked evidence, `evidence_bundle` citing the provider fact ID, full pointer set; after `enable` the same preset resolves (pi/gpt-5.6-codex selected) | PASS |
| `--avoid model_family=gpt` and `--host <disabled kind>` behave per the fixtures | `06-constraints.txt`: avoid removes the gpt candidates and claude/sonnet-5 wins (fixture `session-launch-model-family-avoid`); `--host tmux:gate-dummy` yields `host_source: explicit-flag`, every join `session_host_disabled`, correlation and env both listed as outranked (fixture `session-launch-explicit-host`) | PASS |
| Herdr+Claude and Herdr+Pi each launch through deduction; Stage-1 launch legs re-run green; record commits before spawn | Claude via ambient-env rung (`02-…`), Pi via workspace-correlation rung (`03-…`), both spawned live; `07-launch-legs-rerun.txt`: `internal/launch`, `internal/launchrecord`, `internal/cli` green, `TestRecordCommitsBeforePrepareLaunch` PASS; live records (`lrr_…`) printed before their sessions in both transcripts | PASS |
| `make check` green and `git diff --exit-code contracts/` clean | `00-make-check.txt` (exit 0); `08-contracts-clean.txt` (clean; `contracts/SOURCE` at terminal-multiplexers `210bfd3`, which contains the step-03 seal `32e01fe`) | PASS |

## Notes recorded at gate time

- The fingerprint-less rebind refusal in `04-…` is the domain guard
  working as designed (step 09: a correlation requires fingerprint
  evidence); the verb carries per-field fingerprint flags. Kept in the
  transcript because the first gate attempt hit it.
- Bullet 5 used a pi-runtime variant tagged `provider: codex`
  (notes/42 §3.3b's shape) so "enable restores it" is provable through
  resolution; a codex-runtime-only preset would exhaust on
  `no_conformance_evidence` after enable, which proves the provider
  gate lifted but not restoration.
- Step 14's dogfood caveat stands (recorded in its finding): a launch
  in a wrongly-bound workspace from a pane with no `HERDR_*` variables
  outranks nothing and so warns nothing; `duo workspace host show` /
  `duo doctor` are the visibility rail. Carried to step 24.

Verdict: every bullet passes. `duo.config/v3` is the shipped schema as
of duo-vnext `go` @ the commit that carries this file; `duo.config/v2`
documents refuse with the `duo config migrate --to duo.config/v3`
pointer. duo-dogfood step 24 may start (step 18 hands back).
