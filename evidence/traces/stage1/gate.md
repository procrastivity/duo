# Stage 1 exit gate (re-targeted, handoff 20) — 2026-08-23

Workplan: dogfood Workplan Step 23 (terminal-multiplexers). Binary under
test: `duo version a1249de` (branch `go`). Full suite:
`00-make-check.txt` (lint 0 issues, all packages pass). Live tools:
herdr 0.8.2, claude 2.1.241, pi 0.83.0 — the re-pinned set from
`review/05-close-report.md`.

Each gate bullet maps to a command transcript or test name in this
directory. Live legs ran against disposable Herdr servers only
(isolated `XDG_CONFIG_HOME`, unique `HERDR_SESSION`, torn down after —
the isolation shape `internal/scrub/live_test.go` documents). No live
user session was touched.

| Gate bullet | Evidence | Result |
|---|---|---|
| Herdr+Claude launches/enrolls a disposable session returning opaque Duo IDs | `05-launch-claude.txt`: `duo session launch daily-claude` → `ses_b404c847…`, `lrr_e177fe16…`, leaf claude/sonnet-5 selected, exit 0 | PASS |
| Herdr+Pi launches/enrolls a disposable session returning opaque Duo IDs | `06-launch-pi-and-list.txt`: `duo session launch daily-pi` → `ses_cac34102…`, `lrr_b75cd5d1…`, leaf pi/pi-default selected, exit 0 | PASS |
| Two concurrent sessions in one directory distinct | `02-concurrent-distinct.txt`: `TestTwoSessionsInOneDirectoryStayDistinct`; live in `06-…`: three sessions share `ws_b4645c41…` with pairwise-distinct `ses_`/`ri_` IDs | PASS |
| Restart preserve/quarantine correct | `01-restart-recovery.txt`: `TestAuthorityKillAndRestart` (preserves proved-live, quarantines conflicting claims), `TestRestartPlacesLiveInstancesInRecovering`, `TestConflictingClaimQuarantinesBothSessions`, `TestRestartKeepsTheSessionAndReplacesTheInstance` | PASS |
| Late reporter cannot revive | `01-restart-recovery.txt`: `TestAuthorityKillAndRestart/a_late_reporter_cannot_revive_an_exited_instance`; `TestExitIsFinal` | PASS |
| Both spawn-test legs | `04-spawn-test-legs.txt`: `TestLiveScrubPairedTranscriptLoss` live — negative leg (marker-carrying server, interactive spawn) shows the loss signature, positive leg (Guard-scrubbed server) publishes exactly one transcript | PASS |
| All launch-resolution fixture behaviors complete pre-spawn | `03-launch-resolution-prespawn.txt`: `internal/launch` fixture + resolver suite and `internal/launchrecord` (43 tests, incl. `TestCommittedRecordIsTheResolverRecord`); observed live in `05/06`: a spawn failure during the gate run still left its pre-spawn-committed session durable (`ses_e866183a…` in `06-…`'s list) | PASS |
| tmux + Codex rows | Deferred per handoff 20 / step 01's scope amendment; nothing in this gate exercises them. Re-check before the slice completion gate. | DEFERRED (as required) |

## Gate-run additions

The gate run itself forced two changes, both committed before the
evidence binary was built:

1. **Step 22b (`660c329`)** — the Step 20 scrub was never consumed by
   the live launch path. The Herdr adapter now verifies the new pane's
   inherited environment between `createPane` and `agent.start` and
   refuses fail-closed. Proven live here: `07-scrub-refusal-live.txt`
   shows `duo session launch` against a marker-carrying server refusing
   with `refusal.spawn_environment`, exit 3, marker names only, and the
   clean-server legs (`05`/`06`) passing the same gate.
2. **Agent-name fix (`a1249de`)** — Herdr 0.8.2 refuses `agent.start`
   names over 32 characters (`invalid_agent_name`, observed live on the
   first launch attempt). Names now fold and truncate;
   `TestAgentNameFitsHerdrsLiveRules` pins the live rules.

## Known limits carried forward (not gate failures)

- Sessions list as view `recovering` from the first post-launch command:
  the one-shot CLI has no live-evidence resolver call yet
  (`docs/cli/decisions.md`; step-21 finding). Detach/reattach and
  recovery semantics are proven at the domain layer (`01-…`).
- The pane-environment gate reads exec-time procfs environ; a marker a
  shell startup file exports later is invisible (documented in
  `docs/scrub/decisions.md` 2026-08-23 section).
- The failed first launch's committed session (`ses_e866183a…`)
  demonstrates pre-spawn commit but also shows no compensating
  exit/cleanup on spawn failure exists yet; dogfood follow-on.

Verdict: every bullet passes; tmux rows are recorded as deferred exactly
as the bullet requires. Stage 1 (narrowed, handoff 20) exits; Stage F
(dogfood checkpoint, step 24) may start.
