# Herdr 0.8.2 probe results — SessionEnd close-filter edge cases (R1-R4)

Probe server: `/home/dev/.config/herdr/sessions/duo46-probe/herdr.sock` (herdr 0.8.2, protocol 20).
Claude Code v2.1.241. pi v0.83.0 (`@earendil-works/pi-coding-agent`).
Date: 2026-08-24.

## R1 — `/clear` rebind trap

**Method.** `SP/duo-hooks-r1.json` (copy of `duo-hooks-h1.json` logging SessionEnd
payload + env to `SP/r1.log`). New workspace `w6` (root pane `w6:p1`), cwd = SP.
`agent.start` claude with `["--settings", ".../duo-hooks-r1.json"]`. After boot
(~13s), sent `/clear` + enter, waited 5s, read `r1.log` and `pane.process_info`.
Then sent `/exit` + enter, waited 5s, re-read both.

**Raw evidence.**

After `/clear`, `r1.log` entry 1:
```json
{"session_id":"78921b3c-...","...","hook_event_name":"SessionEnd","reason":"clear"}
```
`pane.process_info` immediately after: foreground process group still PID
3073374, `name: "claude"`, same argv as at boot — process did not exit.

After `/exit`, `r1.log` entry 2 (new session_id, new prompt_id):
```json
{"session_id":"1ee316d1-...","...","hook_event_name":"SessionEnd","reason":"prompt_input_exit"}
```
`pane.process_info` after `/exit`: foreground process group fell back to
PID 3072310, `name: "bash"` — claude process actually exited.

**Verdict.** Confirmed as expected: `/clear` fires a full SessionEnd with
`reason: "clear"` while claude stays resident as the pane's foreground
process (new session_id, same PID) — a close-on-SessionEnd hook without a
`reason` filter would incorrectly close a live, in-use pane. `/exit` fires
SessionEnd with `reason: "prompt_input_exit"` and the process genuinely
exits (foreground falls back to the shell). The close filter must exclude
`reason == "clear"`.

## R2 — Ctrl-D exit reason

**Method.** `SP/duo-hooks-r2.json` (same shape, logs to `SP/r2.log`). New
workspace `w7` (root pane `w7:p1`), cwd = SP. `agent.start` claude with the
r2 settings file. After boot, sent a lone `ctrl+d` key
(`pane.send_keys {"keys":["ctrl+d"]}` — herdr's key syntax is `ctrl+<letter>`,
not `ctrl-d`/`c-d`, both of which the socket rejected with
`invalid_key`/`unsupported key`). Read the screen and `r2.log`/process_info;
sent a second `ctrl+d` (per task instructions, "may need it twice"); waited
a total of ~20s. No screen change and no log entry either time. Fell back to
`/exit` + enter as instructed.

**Raw evidence.**

After 1st and 2nd `ctrl+d` (20s total elapsed): `r2.log` was empty both
times; `pane.process_info` showed claude still the foreground process
(same PID, 3078824) both times; `pane.read` screen was byte-identical to
the boot screen (still showing the welcome box and an empty `❯` composer,
no "press ctrl+d again" or any other hint).

After fallback `/exit` + enter:
```json
{"session_id":"3d014ea6-...","...","hook_event_name":"SessionEnd","reason":"prompt_input_exit"}
```

**Verdict.** Ctrl-D does **not** exit Claude Code v2.1.241 at an empty
composer — sent once or twice, it produced no visible effect, no process
exit, and no SessionEnd at all within 20s. This contradicts the older
assumption that Ctrl-D is a live exit path; on this version it appears to be
unbound (or intentionally disabled) at the composer, so `reason == "eof"` (or
any Ctrl-D-specific reason) is not something a close-filter needs to
special-case for this claude version — the only observed voluntary-exit
reason remains `prompt_input_exit` (same as `/exit`).

## R3 — crash asymmetry

**Method.** `duo-hooks-h3-direct.json` (existing asset; SessionEnd hook runs
`herdr pane close $HERDR_PANE_ID` synchronously, logging to `SP/h3.log`).
Counted existing `h3.log` entries (3, from earlier runs) before starting.
New workspace `w8` (root pane `w8:p1`), cwd = SP. `agent.start` claude with
the h3-direct settings file. After boot, read `pane.process_info` to get
claude's PID (3088376), ran `kill -9 3088376` directly from the probe shell
(not through herdr), waited 5s, re-counted `h3.log` entries, re-checked
`session.snapshot` and `pane.process_info`.

**Raw evidence.**

`h3.log` entry count before: 3 (`direct-attempt` lines). After SIGKILL and
5s wait: still 3 — byte-identical file, no new `direct-attempt` line.

`session.snapshot` after kill: pane `w8:p1` still present, in workspace `w8`,
unclosed.

`pane.process_info` after kill:
```json
{"foreground_process_group_id": 3087660, "foreground_processes":[{"pid":3087660,"name":"bash","argv":["/bin/bash"], ...}]}
```
Foreground fell back to the shell — claude's process is gone, but the pane
itself is not.

**Verdict.** Confirmed: `kill -9` bypasses the hook entirely (SIGKILL is not
catchable, so SessionEnd cannot fire) — the pane and its evidence (scrollback,
running shell) survive untouched, with no close signal ever reaching herdr.
Any close-on-SessionEnd design leaves crashed/killed sessions open
indefinitely; only a supervisory/process-exit-watching mechanism outside the
hook system could catch this case.

## R4 — pi leg (H4)

**Method.** Read `notes/07-pi.md` (event list, `session_shutdown`/`reason`
semantics, `agent_settled` vs `agent_end`) and Duo's shipped
`duo-pi-reporter.ts` for the in-process extension shape
(`export default function(pi) { pi.on(event, handler) }`, fire-and-forget
I/O, `ctx.mode === "tui"` gate pattern). Confirmed via `pi --help` and the
installed package's `docs/extensions.md`/`docs/security.md` that
project-local extensions load from `<project-cwd>/.pi/extensions/*.ts`
(vs. `~/.pi/agent/extensions/` global), and that project trust normally
prompts interactively for the `.pi` directory — `--approve`/`-a` trusts
project-local files for the run without a dialog.

Wrote `SP/r4-proj/.pi/extensions/r4-reporter.ts`: on `session_shutdown`,
always appends `session_shutdown reason=<reason>` to `SP/r4.log`; if
`reason === "quit"`, appends a `quit-detected` line, then — only if
`HERDR_PANE_ID` and `HERDR_BIN_PATH` are both set in the environment —
runs `execFileSync(HERDR_BIN_PATH, ["pane","close",HERDR_PANE_ID])`
synchronously and logs the outcome (including `status`/`stdout`/`stderr`
on failure).

New workspace with cwd = `SP/r4-proj`, `agent.start` kind `pi` with args
`["--approve"]`. After boot (~10-13s), read the screen to confirm the
extension loaded, then sent a lone `ctrl+d` (the pi footer explicitly reads
`ctrl+c/ctrl+d clear/exit`, unlike claude). Ran the probe twice (once with
a terser error log, once with `status`/`stdout`/`stderr` captured) to
resolve an ambiguous first result.

**Raw evidence.**

Boot screen `[Extensions]` line: `herdr-agent-state.ts, moshi-hooks.ts,
pi-effort, r4-reporter.ts` — project-local extension auto-discovered and
loaded with zero interactive trust prompt (confirms `--approve` suppressed
`project_trust`).

Run 1 (workspace `w9`, pane `w9:p1`), after one `ctrl+d`, `r4.log`:
```
session_shutdown reason=quit
quit-detected pane=w9:p1 bin=/home/dev/.local/bin/herdr
pane-close-err: Command failed: /home/dev/.local/bin/herdr pane close w9:p1
```
`session.snapshot` immediately after: workspace `w9` and pane `w9:p1` are
gone entirely (not merely idle) — despite the logged "err".

Run 2 (workspace `wA`, pane `wA:p1`), same extension with fuller error
capture, after one `ctrl+d`, `r4.log`:
```
session_shutdown reason=quit
quit-detected pane=wA:p1 bin=/home/dev/.local/bin/herdr
pane-close-err: status=null stdout=[] stderr=[] msg=Command failed: /home/dev/.local/bin/herdr pane close wA:p1
```
`session.snapshot` immediately after: workspace `wA`/pane `wA:p1` gone.
`status=null` with empty stdout/stderr is the signature of the child being
killed by a signal rather than exiting with a code — consistent with pi's
own process-group teardown on quit killing the `herdr pane close` child
before `execFileSync` could observe its exit, even though the RPC it sent
had already reached the server and closed the pane over the socket.

**Verdict.** A single `ctrl+d` cleanly exits pi (`reason: "quit"`, unlike
claude in R2). The project-local extension shape works exactly as
documented (`.pi/extensions/*.ts`, `--approve` to skip the trust prompt) and
`session_shutdown` + `reason === "quit"` + synchronous `execFileSync` pane
close reliably closes the pane — confirmed twice by the pane vanishing from
`session.snapshot`, even though the local child-process exit status races
with pi's own teardown and reports as a (spurious, signal-killed) error both
times. `r4.log` fired on both runs before the close attempt, so evidence
survives the close race even when the local exec status does not.

## Summary of verdicts

| Probe | Verdict |
|---|---|
| R1 | `/clear` fires SessionEnd (`reason: "clear"`) with claude still resident — close filter must exclude `"clear"`. Confirmed. |
| R2 | Ctrl-D does not exit Claude Code v2.1.241 at all (no reason, no exit, even after two presses / 20s) — no reason to special-case for this version; only `/exit` → `"prompt_input_exit"` observed. |
| R3 | `kill -9` bypasses the hook entirely — pane and evidence stay open, no SessionEnd, foreground falls back to the shell. Confirmed as expected crash asymmetry. |
| R4 | pi's `session_shutdown`/`reason:"quit"` + a project-local extension reliably closes the pane via synchronous `execFileSync`, confirmed twice; the local exec status is racy (signal-killed by pi's own teardown) but the server-side close still lands, and the log line fires before the close attempt either way. |

**Surprises:**
- herdr's `send-keys` key syntax is `ctrl+<letter>` (plus sign), not
  `ctrl-d`/`c-d` — both hyphenated forms are rejected as `invalid_key`.
- Claude Code v2.1.241 does not bind Ctrl-D to exit at all (contrary to the
  task's expectation that it might need pressing twice); pi does, in one
  press, and unlike claude's SessionEnd payload, pi's extension gets a
  literal `reason: "quit"` string with no ambiguity.
- In R4, the synchronous close call's local exit status is unreliable
  evidence of success/failure (races with pi's own teardown killing the
  child) — the pane's disappearance from `session.snapshot` is the only
  trustworthy success signal; log-before-attempt is important precisely
  because the attempt's own outcome can't be trusted.
