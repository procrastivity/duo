# Post-launch identity-bind exit gate (§3c) — 2026-08-26

Workplan: wip matter `duo-launch-bind` step 09 (terminal-multiplexers).
Binary under test: `duo version e02d886` (branch `go`). Full suite:
`00-make-check.txt` (lint 0 issues, all packages pass). Live tools:
herdr 0.8.2, claude 2.1.246, pi 0.84.3. Disposable Herdr session `dlb09`
under isolated `XDG_{CONFIG,DATA,STATE}_HOME` at `/tmp/duo-dlb09`
(unique `HERDR_SESSION`, no ambient `HERDR_*`, agent-harness markers
unset on the **server**). Torn down after. No live user session was
touched.

This gate is the roadmap §3c exit. It does **not** claim the Stage 2
exit gate. Nested D1 (Duo-launched orchestrator + real handoff) is out.

Each gate bullet maps to a command transcript or test name in this
directory.

| Gate bullet | Evidence | Result |
|---|---|---|
| Herdr+Claude and Herdr+Pi each launch via preset. The runtime instance leaves `starting` and becomes `live`. Active `agent.session` and `transcript` correlations are present on the instance. | `03-dry-run.txt` / `04-launch-claude.txt`: `builder --require agent_runtime=claude` → `ses_46dfda4a…`, `lrr_17bc1848…`, `target: tab`, `target_source: built-in`, exit 0. `05-launch-pi.txt`: `builder --require agent_runtime=pi` → `ses_57e7b6f2…`, `lrr_094f06c7…`, same placement pair, exit 0. Launch's 3s identity wait returned while both instances were still `starting` (`06-session-show-and-conversation.txt`, I-8 omit condition). Herdr already named identity (`agent.list`: Claude id `c8192c32-…`, Pi path). Claude `prompt send` then bound and `MarkLive`'d (`06b-show-after-bind.txt`: `runtime_instance_state: live`). Pi stayed `starting` while `launch_pending: true`. Claude `agent.session` present after send; live `transcript` correlation empty (claim had no `WorkingDirectory` / `TranscriptPath`; file existed at `~/.claude/projects/-tmp-duo-dlb09-ws/c8192c32-….jsonl`). Tests: `TestLaunchBindsIdentityAndMarksLiveWithoutHandInjectedMarkLive`, `TestLaunchLeavesStartingWhenIdentityNeverAppears`, `TestLaunchPendingDoesNotMarkLive`. I-1: `02-i1.txt` `TestRecordCommitsBeforePrepareLaunch`; live reports print `lrr_` with the session. | PARTIAL (Claude live + `agent.session` after send PASS; launch-time 3s often misses; transcript locator not written for id-kind; Pi not live while `launch_pending`) |
| `duo session show` reports condition. `duo conversation list` returns transcript turns on both compositions. | Tests: `01-fake-pair.txt` `TestSessionInspectAndConversationList_FakePair`, `TestLaunchInspectAndConversationList_FakePair`, `TestSessionShow_StartingOmitsCondition`. Live `06-session-show-and-conversation.txt`: both inspect `starting`, omit `condition` (I-8). After Claude send, `06b-show-after-bind.txt`: Claude `live` with `condition.value: unknown` (`missing transcript`); `conversation.list` → `internal.conversation_list_failed` opening an empty path. Pi conversation.list used the bound path identity; the jsonl was not on disk yet. | PARTIAL (tests PASS; live show condition after Claude bind PASS; live conversation turns blocked on missing transcript locator) |
| Post-launch `duo prompt send` delivers. Claude uses the messaging socket. Same idempotency key replays; a changed request conflicts. | `07-prompt-send.txt`: Claude `responsibility_state: delivered` after implicit wait (identity already on `agent.list`; settle window already elapsed). `09-pane-read.txt`: pane shows `Another Claude session sent a message` then `● pong` — messaging socket, not a typed pane paste. `08-prompt-show.txt`: same key replays `cmd_511aa50a…`; different text → `command.idempotency_conflict` exit 1. Tests: `TestPromptSend_DelayedIdentityEndsDelivered`, `TestPromptSend_NeverBindsExpiresLoudly`, `TestPromptSend_DeliveredFakePair`, `TestPromptSend_ReplaySameDigest`, `TestPromptSend_IdempotencyConflict`. No `duo session settle`; no launch `--prompt`. | PASS (Claude live socket deliver + replay/conflict) |
| Fake-host + fake-runtime still pass every cross-composition test | `01-fake-pair.txt`: prompt send/show/conflict/replay, launch inspect without `MarkLive`, starting omit-condition; `./internal/host/fake`, `./internal/runtime/fake`, `./internal/conformance`. | PASS |
| `nix develop --command make check` is green, and `git diff --exit-code contracts/` is clean | `00-make-check.txt` (exit 0); `13-contracts-clean.txt` (clean; `contracts/SOURCE` at terminal-multiplexers `7162572`, which contains the step 01 starting-inspect fixture) | PASS |
| Pi remaining `no_effect` after bind and ready is a named remainder, not a silent pass | `09-pi-prompt-send.txt`: Herdr still `launch_pending: true` / `screen_detection_skipped: true` on an otherwise idle Pi pane. Send waited until `--expires-at` and failed `command.expired` (`responsibility_state: expired`), not `delivered` and not a queued hold that looks like success. Pane (`09-pane-read.txt`) never received the prompt. | PASS (named: expired while `launch_pending`, not silent) |
| Nested D1 orchestrator day is out | Not run. Cursor drove this gate against Duo-launched builders. | out of scope |

## Known limits (not Stage 2 claims)

- Launch waits 3s (`defaultIdentityBindTimeout`) then returns `starting` if identity is not yet on `agent.list`. Live Claude identity appeared after that window; `prompt send` recovered via `bindStartingIdentity` and command expiry. This is the designed send-path wait, not a `session.settle` verb.
- Live Claude `conversation.list` / condition-from-transcript need a transcript locator. `claimFromHostIdentity` sets `ExternalAgentSessionID` for kind `id` but not `WorkingDirectory`, so `Correlate` leaves `TranscriptID` empty even when the JSONL exists at Claude's cwd slug. Not a directory-newest scan (I-6). Named remainder.
- Pi stayed `launch_pending` with path-shaped identity bound; send expired loudly. Same host remainder as §3b (`screen_detection_skipped`), now with honest expiry instead of silent `no_effect` success.
- `view` stayed `recovering` on a `live` Claude inspect. Not a §3c bullet.
- Nested D1 is a use-day after this milestone ships. tmux + Codex stay deferred.

Verdict: the §3c identity-bind gate passes at the named scope (Claude post-launch live + messaging-socket deliver with replay/conflict, honest starting inspect, fake pair, `make check`, contracts, Pi remainder named). Live conversation turns and launch-time MarkLive within 3s remain limits, not a Stage 2 claim.
