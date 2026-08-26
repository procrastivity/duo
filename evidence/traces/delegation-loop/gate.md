# Delegation-loop exit gate (§3b) — 2026-08-26

Workplan: wip matter `duo-delegation-loop` step 17 (terminal-multiplexers).
Binary under test: `duo version 3ebaf6a` (branch `go`). Full suite:
`00-make-check.txt` (lint 0 issues, all packages pass). Live tools:
herdr 0.8.2, claude 2.1.246, pi 0.84.3. Disposable Herdr session `ddl17`
under isolated `XDG_{CONFIG,DATA,STATE}_HOME` at `/tmp/duo-ddl17`
(unique `HERDR_SESSION`, no ambient `HERDR_*`, agent-harness markers
unset). Torn down after. No live user session was touched.

This gate is the roadmap §3b exit. It does **not** claim the Stage 2 or
Stage 3 exit gates.

Each gate bullet maps to a command transcript or test name in this
directory.

| Gate bullet | Evidence | Result |
|---|---|---|
| Herdr+Claude and Herdr+Pi launch via preset; report carries `target` / `target_source`; close-on-exit on unless `--remain-on-exit`; record commits before spawn (I-1) | `03-dry-run.txt` and `04-launch-claude.txt`: `builder --require agent_runtime=claude` → `ses_dab265ab…`, `lrr_157c28d5…`, `target: tab`, `target_source: built-in`, `host_source: explicit-flag`, exit 0. `05-launch-pi.txt`: `builder --require agent_runtime=pi` → `ses_89225e61…`, `lrr_fe70f8db…`, same placement pair, exit 0. Close-on-exit artifacts in `12-close-on-exit-artifacts.txt` (claude SessionEnd hook + settings; pi `duo-close-on-exit.ts`). I-1: `02-i1-hold-doctor.txt` `TestRecordCommitsBeforePrepareLaunch` (launch + launchrecord); live reports print `lrr_` with the session. | PASS |
| Fake-host + fake-runtime still pass every cross-composition test (D6) | `01-fake-pair.txt`: prompt send/show/conflict/replay and `TestSessionInspectAndConversationList_FakePair`; `./internal/host/fake`, `./internal/runtime/fake`, `./internal/conformance`. `10-doctor.txt`: doctor still registers `fake-host` and `fake-runtime` as supported. | PASS |
| `duo session show` reports condition; `duo conversation list` returns transcript turns on both compositions | Tests: `01-fake-pair.txt` `TestSessionInspectAndConversationList_FakePair`. Live `06-session-show-and-conversation.txt`: both sessions inspect as `lifecycle: active`, `runtime_instance_state: starting`, `view: recovering`; `condition` omitted; `prompt.deliver` / `conversation.list` `unsupported`; `conversation list` → `object.not_found` (no `agent.session` correlation). Launch does not write those correlations; D2 cut the rank-2 reporter. Result observed instead on the Claude pane (`09-pane-read.txt`: `● pong`). | PARTIAL (tests PASS; live inspect identity PASS; live condition/transcript CLI blocked on missing correlation) |
| Post-launch `duo prompt send` delivers. Claude uses the messaging socket. Pi uses Herdr `agent.prompt`. Same key replays; changed request conflicts. | `07-prompt-send.txt` / `08-prompt-show.txt`: Claude `responsibility_state: delivered`, one native attempt (Herdr `agent.prompt`, not the messaging socket — runtime adapter never opened without correlation). Same key replays `cmd_e27c46de…`; different text → `command.idempotency_conflict` exit 1. Pi accepted then queued: three native attempts all `no_effect` while Herdr listed the agent as `launch_pending` (`09b-pi-retry.txt`). Tests: `TestPromptSend_DeliveredFakePair`, `TestPromptSend_ReplaySameDigest`, `TestPromptSend_IdempotencyConflict`; delivery `TestDuoCreatedSettledIdleSelectsRuntimePath` / `TestPiSelectsHostPath`. | PARTIAL (Claude live deliver + replay/conflict PASS; Claude socket path not live; Pi live `agent.prompt` no_effect) |
| Half-written human prompt in an attached pane holds when Duo has draft evidence. A Duo-created pane with no human attach may auto-release on launch-settled idle. | Tests in `02-i1-hold-doctor.txt`: `TestDraftHoldsEvenOnRuntimePath`, `TestAttachedPaneHolds`, `TestUnknownSettledDuoCreatedAutoReleases`, `TestDuoCreatedSettledIdleSelectsRuntimePath`. Live: Claude auto-released and delivered after settle (`07-prompt-send.txt`). Herdr cannot fill `Draft` / `LastHumanInput` / `HumanAttached` (notes/19); the CLI does not pass that evidence. Draft-hold is test-only on this host. | PASS (live auto-release + tests; draft-hold not live-provable on Herdr) |
| Doctor warns on scrub-gate; doctor sweep reaps orphan harness dirs | Tests: `TestDoctorVisibility_ScrubGateWarnsWhenSurvivors`, `TestDoctorVisibility_ScrubGateSilentWhenClean`, `TestDoctorReapsRefusedLaunchHarnessDir`, `TestSweepHarnessDirsReapsUnkeptAndLeavesKept`. Live `10-doctor.txt`: clean disposable server → `scrub_gate` absent. `11-doctor-sweep.txt`: planted `lrr_orphan_ddl17` reaped (`reaped: 1`), live launch dirs kept (`kept: 2`). | PASS |
| `make check` green and `git diff --exit-code contracts/` clean | `00-make-check.txt` (exit 0); `13-contracts-clean.txt` (clean; `contracts/SOURCE` at terminal-multiplexers `3c9c2ba`, which contains the step 01 seal `d248dcd`) | PASS |
| One real dogfood day drives one real handoff through the loop: a Duo-launched orchestrating agent, using the skill, instructs a builder session launched via preset, delivers the instruction, and observes completion and the result through Duo CLI surfaces (D1) | This session followed `skills/duo-delegation-loop/SKILL.md`: `session launch` → `prompt send` → `prompt show` / pane read. Claude builder (`ses_dab265ab…`) received the instruction and answered `pong` (`09-pane-read.txt`). Observation through `duo conversation list` did not work (correlation gap above). A nested Duo-launched orchestrator with a hand-installed skill was not stood up in this sitting. | PARTIAL |

## Known limits (not Stage 2 / Stage 3 claims)

- Launch leaves the runtime instance `starting` and writes no `agent.session` / transcript correlation. `session.inspect` condition and `conversation.list` therefore cannot bind a live adapter until a reporter or correlator exists. D2 cut the rank-2 reporter; this gate does not add one.
- Claude live delivery used Herdr `agent.prompt` (native), not the messaging socket, for the same reason. The socket path remains covered by tests (`TestDuoCreatedSettledIdleSelectsRuntimePath`).
- Pi live `agent.prompt` returned `no_effect` while Herdr reported `launch_pending` / `screen_detection_skipped` on an otherwise idle pane. Tests cover the host path (`TestPiSelectsHostPath`).
- Draft hold cannot be exercised through the CLI on Herdr (notes/19 writer-presence refuted).
- D1's nested "Duo-launched orchestrating agent" was not run. The skill sequence was driven from this Cursor session against Duo-launched builders.

Verdict: the §3b implementation gate passes at the named scope (launch placement, I-1, fake pair, Claude post-launch deliver with replay/conflict, doctor scrub-gate + sweep, `make check`, contracts). Live condition/transcript CLI, Claude socket path, Pi host delivery, and a nested Duo-launched orchestrator remain limits, not Stage 2 or Stage 3 claims. tmux + Codex stay deferred.
