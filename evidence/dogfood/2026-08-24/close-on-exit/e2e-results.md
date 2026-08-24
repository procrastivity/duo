# `--close-on-exit` live e2e — results

Date: 2026-08-24
Binary under test: `SP/duo-go`, built from the uncommitted `/home/dev/Code/duo-vnext`
working tree with `CGO_ENABLED=0 go build -o SP/duo-go ./cmd/duo`
(repo root has no Go files; `make build` targets `./cmd/duo`).
Working tree not committed, not reverted, not modified.

Versions: herdr 0.8.2 (protocol 20), claude 2.1.241, pi 0.83.0.

`SP` = `/tmp/claude-1000/-home-dev-Code-terminal-multiplexers/384bd77d-8b10-45a2-a9b0-e15a12151884/scratchpad`
Probe socket = `/home/dev/.config/herdr/sessions/duo46-probe/herdr.sock`

## Verdict summary

| leg | what | level | verdict |
|---|---|---|---|
| 1 | claude, flag on, `/exit` -> container gone | pane-level (see deviation) | **PASS** (2/2 runs) |
| 2 | claude, flag on, `kill -9` -> pane survives at shell | pane-level | **PASS** |
| 3 | claude, flag off, `/exit` -> pane survives at shell | pane-level | **PASS** |
| 4 | pi, flag on, Ctrl-D -> container gone | pane-level | **PASS** |
| 5 | SessionEnd `reason` vocabulary (extra) | pane-level | **PASS** |
| — | CLI `duo session launch --close-on-exit` end-to-end | CLI-level | **BLOCKED** — see below |

## Isolation actually used

- `XDG_CONFIG_HOME=SP/xdg/config`, `XDG_DATA_HOME=SP/xdg/data`,
  `XDG_STATE_HOME=SP/xdg/state` on every `duo-go` invocation.
- `HERDR_SOCKET_PATH` explicitly overridden to the probe socket on every
  `duo-go` invocation; `env -u HERDR_SOCKET_PATH` on every direct socket call.
- Config authored at `SP/xdg/config/duo/duo.config.yaml` (`duo.config/v3`,
  trimmed copy of `evidence/dogfood/2026-08-24/duo.config.yaml`: presets
  `probe_claude` -> `claude_haiku`, `probe_pi` -> `pi_openai_gpt56luna`).
- Scratch project root `SP/proj` (git-init'd).
- The user's `~/.config/duo*`, `~/.local/share/duo*`, `~/.claude/settings.json`
  and `~/.pi/agent/` were not written.

Side effects that did touch real per-user state, all benign and noted for
completeness:
- `~/.claude.json` gained a trusted-folder entry for `SP/proj` (claude's
  first-run "Is this a project you trust?" prompt).
- claude wrote transcripts under `~/.claude/projects/-tmp-claude-1000-...proj/`.
- pi's project-trust prompt was answered **"Trust (this session only)"**, so no
  durable pi trust entry was created.

## Deviation from the CLI-level path (important)

The CLI path was exercised as far as it can go and then **refused by duo's own
fail-closed scrub gate**, for a reason unrelated to `--close-on-exit`:

```
$ duo session launch probe_claude --close-on-exit
duo: session launch: refusing to launch: the environment the new session-host
pane inherited from its server (wB:p1) still carries agent-harness marker(s)
AI_AGENT, CLAUDE_CODE_BRIDGE_SESSION_ID, CLAUDE_CODE_ENTRYPOINT,
CLAUDE_CODE_EXECPATH, CLAUDE_CODE_MESSAGING_SOCKET, CLAUDE_CODE_MESSAGING_TOKEN,
CLAUDE_CODE_SESSION_ID, CLAUDE_EFFORT, CLAUDE_PID. A runtime started there
disables its own transcript silently. Restart the session host from a shell
that no agent harness is running in, then launch again ...
exit=3
```

The disposable `duo46-probe` server was started from inside this Claude Code
harness with only `-u HERDR_SOCKET_PATH -u CLAUDECODE -u CLAUDE_CODE_CHILD_SESSION`
scrubbed, so its panes inherit nine more markers that
`internal/scrub.Markers` + `WildcardPrefixes` (`CLAUDE_*`) catch.
`internal/host/herdr/scrubgate.go` gates on the pane's real inherited
environment and has no override; the same gate sits under the
`TestLiveHerdr` adapter-level fallback, so that fallback was blocked too.

The fix is a probe herdr server started from a marker-free environment.
**Both `herdr server stop` (on the probe socket) and starting a clean-env
`herdr server` were denied by the Claude Code permission classifier**, twice,
so the CLI-level and adapter-level legs could not be run. This is a harness
permission limit, not a defect in the feature.

What the CLI leg *did* prove before the refusal (the refusal happens in the
host adapter, downstream of the launcher's `LeafAugmenter` call):

```
$ find SP/xdg/data -type f
SP/xdg/data/duo/duo.db
SP/xdg/data/duo/harness/lrr_9210b89b3b4d0a31403cd6aab980a03b/main/session-end-hook.sh
SP/xdg/data/duo/harness/lrr_9210b89b3b4d0a31403cd6aab980a03b/main/close-on-exit-settings.json

drwx------  (dir 0700)
-rw-------  close-on-exit-settings.json (0600)
-rwx------  session-end-hook.sh (0700)
```

i.e. `stage1LeafAugmenter.Augment` ran for the `claude` leaf under the real CLI,
`claude.DefaultHarnessDir` honoured the isolated `XDG_DATA_HOME`,
`MaterializeCloseOnExit` wrote both files with the documented modes, and the
rendered settings document names the hook by absolute path with exactly one
`SessionEnd` entry and no matcher. `--dry-run --close-on-exit` materialized
nothing (correct: `Augment` is only called in the spawn path).

The **generated artifacts from that real CLI run are what all four live legs
below actually used** — nothing about the hook or the settings file was
hand-written.

### Substitute harness for the live legs

Panes were created directly on the probe server
(`workspace.create` -> lone-pane tab in a fresh workspace, the same container
shape duo's Herdr adapter creates for its default background-tab placement),
and the agent was started by `pane.send_text` + `pane.send_keys ["enter"]`.
The pane's inherited `CLAUDE_*`/`AI_AGENT` markers were removed inside the pane
by `SP/scrub-exec.sh`, which mirrors `internal/scrub.PaneCommand` shape (b):

```sh
for v in $(env | sed -n 's/^\(CLAUDE_[^=]*\)=.*/\1/p'); do unset "$v"; done
unset CLAUDECODE AI_AGENT CLAUDE_CODE_CHILD_SESSION
exec "$@"
```

This is functionally what duo's launch does (herdr's own `agent.start` also
types the launch line into the pane's shell), minus herdr agent-registry
registration — which the close-on-exit hook does not use: it closes via
`$HERDR_BIN_PATH pane close $HERDR_PANE_ID`.

Verified the probe pane environment carries what the hook needs, set by herdr
itself and pointing at the probe server (never the live one):

```
HERDR_ENV=1
HERDR_PANE_ID=wC:p1
HERDR_BIN_PATH=/home/dev/.local/bin/herdr
HERDR_SOCKET_PATH=/home/dev/.config/herdr/sessions/duo46-probe/herdr.sock
```

---

## Leg 1 — claude, flag on, clean `/exit`

Commands (run 1, workspace `wC`):

```
hc workspace.create '{"cwd":"SP/proj","label":"duo-e2e-leg1"}'      -> wC / wC:t1 / wC:p1
pane.send_text  'sh SP/scrub-exec.sh claude --model haiku --settings \
   SP/xdg/data/duo/harness/lrr_9210b89b3b4d0a31403cd6aab980a03b/main/close-on-exit-settings.json'
pane.send_keys  ["enter"]        # + one enter for claude's trust prompt
pane.send_text  '/exit'
pane.send_keys  ["enter"]        # x2 when the slash-command menu is open
```

Observations:

- `pane.process_info` before exit:
  `fg 3197978 claude` with argv
  `["claude","--model","haiku","--settings",".../close-on-exit-settings.json"]`
  — the duo-generated settings path is the live argv.
- `pane.read` showed a clean boot: `Claude Code v2.1.241`, `Haiku 4.5`,
  **no "Transcript saving is off" warning** (the scrub worked).
- snapshot before `/exit`: `workspaces ['wC'] tabs ['wC:t1'] panes ['wC:p1']`
- snapshot after `/exit` (first poll, ~1 s): `workspaces [] tabs [] panes []`

Run 2 (workspace `wD`, reusing the leg-3 pane, with explicit timing):

- snapshot before: `workspaces ['wD'] tabs ['wD:t1'] panes ['wD:p1']`
- first `enter` only confirmed claude's slash-command autocomplete; the second
  `enter` submitted `/exit`.
- t+1s .. t+9s: pane still present; **t+10s: `workspaces [] tabs [] panes []`**

**Verdict: PASS (2/2).** The lone-pane tab and its workspace both disappear
from `session.snapshot`, exactly as specified.

## Leg 2 — claude, flag on, `kill -9` (crash leg)

```
hc workspace.create '{"cwd":"SP/proj","label":"duo-e2e-leg2"}'  -> wD / wD:t1 / wD:p1
<same --settings launch line>
# after boot:
pane.process_info -> shell_pid 3205753, fg 3206258 claude
kill -9 3206258
```

Observations ~4 s later:

- snapshot: `workspaces ['wD'] tabs ['wD:t1'] panes ['wD:p1']` — **container intact**
- `pane.process_info`: `shell_pid 3205753`, `fg 3205753 bash /bin/bash`

**Verdict: PASS.** A killed agent leaves the pane open at its shell, as designed
(no SessionEnd hook runs, so nothing closes the pane).

## Leg 3 — claude, flag off (control)

Same surviving pane `wD:p1`, launched without the generated settings:

```
pane.send_text 'sh SP/scrub-exec.sh claude --model haiku'
pane.send_keys ["enter"]
# after boot (fg 3209181 claude, cmdline "claude --model haiku"):
pane.send_text '/exit'; pane.send_keys ["enter"]
```

Observations:

- snapshot at t+1s .. t+6s: `workspaces ['wD'] tabs ['wD:t1'] panes ['wD:p1']`
- claude took ~10 s to exit; afterwards
  `pane.process_info` -> `fg 3205753 bash`, PID 3209181 gone,
  snapshot still `workspaces ['wD'] tabs ['wD:t1'] panes ['wD:p1']`

**Verdict: PASS.** Old behavior unchanged: the pane survives at the shell prompt.

## Leg 4 — pi, flag on, Ctrl-D

Extension activation, project-local only (no global pi config touched):

```
mkdir -p SP/proj/.pi/extensions
sed -e 's/__DUO_PROTOCOL__/duo.pi.reporter\/v1/g' \
    -e 's/__DUO_SOCKET_ENV__/DUO_REPORTER_SOCKET/g' \
    -e 's/__DUO_TOKEN_ENV__/DUO_REPORTER_TOKEN/g' \
    internal/runtime/pi/extension/duo-pi-reporter.ts \
  > SP/proj/.pi/extensions/duo-pi-reporter.ts
```

(the same three substitutions `pi.RenderExtension` performs; duo has no CLI verb
that installs the extension, so this was done by hand. The close-on-exit path in
the extension does not depend on `DUO_REPORTER_SOCKET`/`DUO_REPORTER_TOKEN`
being set — only on `DUO_CLOSE_PANE_ON_EXIT`, `HERDR_ENV`, `HERDR_PANE_ID`,
`HERDR_BIN_PATH` — so an unset credential does not disable it.)

Env marker delivered the way duo's `LeafAugmenter` delivers it, through the
herdr pane env map:

```
hc workspace.create '{"cwd":"SP/proj","label":"duo-e2e-leg4-pi",
                      "env":{"DUO_CLOSE_PANE_ON_EXIT":"1"}}'  -> wE / wE:t1 / wE:p1
# /proc/<shell_pid>/environ confirms:  DUO_CLOSE_PANE_ON_EXIT=1
pane.send_text 'sh SP/scrub-exec.sh pi --provider openai-codex --model gpt-5.6-luna'
pane.send_keys ["enter"]
pane.send_keys ["down","down","enter"]     # "Trust (this session only)"
pane.send_keys ["ctrl+d"]
```

Observations:

- pi's startup banner listed `[Extensions] duo-pi-reporter.ts, herdr-agent-state.ts,
  moshi-hooks.ts, pi-effort` — the duo extension loaded.
- snapshot before Ctrl-D: `workspaces ['wE'] tabs ['wE:t1'] panes ['wE:p1']`
- t+1s: still present; **t+2s: `workspaces [] tabs [] panes []`**

**Verdict: PASS.** The pi leg closes the container on a clean quit, and it did so
without touching `~/.pi/agent/` or creating a durable trust entry.

## Leg 5 (extra) — SessionEnd `reason` vocabulary

`closeonexit/session-end.sh` hardcodes `TERMINAL_REASONS="prompt_input_exit logout"`
and warns that `clear`/`resume` must never be added. That list was verified live
rather than assumed, with a logging-only SessionEnd hook:

```
SP/log-settings.json -> SessionEnd -> SP/loghook.sh   (cat >> SP/sessionend.log)
sh SP/scrub-exec.sh claude --model haiku --settings SP/log-settings.json
# then /clear, then /exit
```

Captured payloads:

```
{... "hook_event_name":"SessionEnd","reason":"clear"}
{... "hook_event_name":"SessionEnd","reason":"prompt_input_exit"}
```

- `/clear` -> `clear`: not in `TERMINAL_REASONS`, and the pane stayed open
  through it (a live agent is not closed out from under the user). Correct.
- `/exit` -> `prompt_input_exit`: in `TERMINAL_REASONS`. Correct, and it is
  the reason that fired in legs 1 and 4's claude equivalents.
- The payload's `"reason":"..."` is a flat top-level string member, so the
  hook's jq-free `sed` extraction matches the real shape.
- `logout` was not reproducible in this harness (no logged-out account) —
  untested but harmless.

**Verdict: PASS.**

## Unit tests

`go test ./internal/runtime/claude/... ./internal/cli/... ./internal/launch/...
./internal/runtime/pi/... ./internal/host/herdr/...` — all `ok`.

New/changed tests: `TestCloseOnExitAppendsSettingsFlagForAClaudeLeaf`,
`TestCloseOnExitLeavesArgsUnchangedWithoutTheFlag`,
`TestCloseOnExitSetsEnvVarForAPiLeaf`,
`TestCloseOnExitLeavesEnvUnchangedForAPiLeafWithoutTheFlag`,
seven `closeonexit_test.go` hook tests, and
`TestExtensionClosesPaneOnExitWhenActivated`.

## Things that looked wrong in the feature

1. **The generated harness directory is never cleaned up.**
   `$XDG_DATA_HOME/duo/harness/<launch-resolution-id>/<leaf>/` is created per
   launch and nothing removes it — not on a normal session end, and not on a
   *refused* launch (the refused CLI run above left a complete orphan harness
   behind). Slow, unbounded growth; each dir is small but every launch adds one.
   Worth a note in notes/46 even if teardown is deferred.

2. **`--settings` is appended after the variant's `append_arguments`.**
   If a `launch_variant` ever declares its own `--settings`, two `--settings`
   flags reach claude and the winner is claude's undefined precedence, not
   duo's. Not reachable with today's dogfood config, but it is an unguarded
   collision on a user-authored field.

3. **`DUO_CLOSE_PANE_ON_EXIT` is left in the pane environment for the pane's
   whole life** (the extension deliberately does not scrub it, unlike the
   reporter credential). Consequence: a *second* pi started by hand in that
   same pane after the first quits will also close the pane on its own quit.
   The extension's comment acknowledges leaving it; the second-order effect
   does not appear to be written down anywhere.

4. **Not a feature bug, but a dogfood-day trap worth recording:** duo's scrub
   gate makes `--close-on-exit` (and every launch) untestable against any herdr
   server that was itself started from inside an agent harness. The refusal text
   is clear and correct, but there is no `duo doctor` line that reports "the
   deduced host's panes would be refused", so the failure is only discoverable
   at launch time.

## Cleanup

- Final `session.snapshot` on the probe socket:
  `{"workspaces": [], "tabs": [], "panes": [], "layouts": [], "agents": []}`
  (the one pane that survived by design, `wF:p1` from leg 5, was closed with
  `pane.close`).
- The `duo46-probe` herdr server is **left running**.
- The user's live herdr server was never contacted: every `duo-go` call set
  `HERDR_SOCKET_PATH` to the probe socket explicitly, every direct socket call
  used `env -u HERDR_SOCKET_PATH` plus an explicit probe path.
- `/home/dev/Code/duo-vnext` `git status` is byte-identical to the state at
  start (6 modified, 4 untracked); nothing committed, nothing reverted.

---

# CLI-level rerun (duo46-clean)

The coordinator provided a marker-free probe server started from an `env -i`
shell: `/home/dev/.config/herdr/sessions/duo46-clean/herdr.sock` (herdr 0.8.2,
protocol 20, empty at start). All four legs were rerun through the **real
`duo session launch` CLI**, retargeting the same XDG-isolated config
(`SP/xdg/config/duo/duo.config.yaml`, `SP/xdg/data`, `SP/xdg/state`) by setting
`HERDR_SOCKET_PATH` to the clean socket on each `duo-go` invocation.

Deduction confirmed first:

```
$ duo doctor
  host deduction (what M1 would deduce now):
    herdr:/home/dev/.config/herdr/sessions/duo46-clean/herdr.sock (host_source=ambient-env)
```

## Scrub gate: PASSES

The refusal that blocked the first attempt is gone. Verified directly on the
pane duo created (`/proc/<shell_pid>/environ`):

```
=== pane env markers (should be none) ===
(empty)
=== herdr env in pane ===
HERDR_BIN_PATH=/home/dev/.local/bin/herdr
HERDR_ENV=1
HERDR_PANE_ID=w2:p1
HERDR_SESSION=duo46-clean
HERDR_SOCKET_PATH=/home/dev/.config/herdr/sessions/duo46-clean/herdr.sock
HERDR_TAB_ID=w2:t1
HERDR_WORKSPACE_ID=w2
```

Note the pane's own `HERDR_SOCKET_PATH` points at the clean server, so the
hook's `herdr pane close` targets it and never the user's live server.

## CLI Leg 1 — claude, `--close-on-exit`, `/exit`

```
$ cd SP/proj
$ env XDG_CONFIG_HOME=SP/xdg/config XDG_DATA_HOME=SP/xdg/data \
      XDG_STATE_HOME=SP/xdg/state \
      HERDR_SOCKET_PATH=/home/dev/.config/herdr/sessions/duo46-clean/herdr.sock \
      SP/duo-go session launch probe_claude --close-on-exit
exit=0
duo: herdr:/home/dev/.config/herdr/sessions/duo46-clean/herdr.sock was deduced
     from the ambient environment of the pane this command is running in.
duo: bind workspace ws_cd0b254cb6baf3100134119ee0a73ccd to it ...? [y/N]
duo: host binding skipped: no answer was read from the terminal ...
session:  ses_e4a85cba8cb6b51b47bbbe8cfb9d1f77
record:   lrr_816026501e8c5e35c4ab801abc57d718
host:      herdr:.../duo46-clean/herdr.sock (host_source=ambient-env)
  main: claude / haiku-4.5 (determined) -> selected
```

Container duo created (session was empty, so `workspace.create`):

```
WS   w2      label duo-main-lrr_816026501e8c5e35c4a   tabs 1  panes 1
TAB  w2:t1   focused True  panes 1
PANE w2:p1   w2:t1  idle
AGENT name=duo-main-lrr_816026501e8c5e35c4a agent=claude agent_status=idle
      interactive_ready=True terminal_id=term_659d29a2eaccd2
```

Live argv in the pane (`/proc/3248028/cmdline`) — the augmenter's flag reached
the real process, pointing at this launch's own resolution id:

```
claude --model haiku --settings \
  SP/xdg/data/duo/harness/lrr_816026501e8c5e35c4ab801abc57d718/main/close-on-exit-settings.json
```

`/exit` (two `enter`s: the first confirms claude's slash-command menu):

```
t+1s .. t+9s: workspaces ['w2'] tabs ['w2:t1'] panes ['w2:p1']
t+10s:        workspaces [] tabs [] panes []
```

**Verdict: PASS.** Pane, its lone tab, and the workspace all leave
`session.snapshot`.

## CLI Leg 2 — claude, `--close-on-exit`, `kill -9`

```
$ SP/duo-go session launch probe_claude --close-on-exit
session:  ses_946d52b9b3b784d04e758170e0681f3e
record:   lrr_9018ae2cbc078ee65a43afb8a0a43188
snapshot: workspaces ['w3'] tabs ['w3:t1'] panes ['w3:p1']
pane.process_info -> shell_pid 3251181, fg 3251361 claude (--settings ...)
$ kill -9 3251361
```

```
t+1s .. t+8s: workspaces ['w3'] tabs ['w3:t1'] panes ['w3:p1']
pane.process_info -> shell_pid 3251181, fg 3251181 bash /bin/bash
```

**Verdict: PASS.** The crash leaves the container open at the shell, as designed.

## CLI Leg 3 — claude, no flag, `/exit`

Launched into the now non-empty session, so this also exercised the **default
background-tab placement** (`tab.create` in the existing workspace):

```
$ SP/duo-go session launch probe_claude
session:  ses_f37f5b177cba4952e45f64718ccfed26
record:   lrr_f4afe7e55f3a6b65fb18f44be60cf49c
snapshot: workspaces ['w3'] tabs ['w3:t1','w3:t2'] panes ['w3:p1','w3:p2']
```

Two negative checks, both correct:

- live argv is `claude --model haiku` — **no `--settings` appended**
- `SP/xdg/data/duo/harness/lrr_f4afe7e55f3a6b65fb18f44be60cf49c` **does not
  exist** — nothing materialized when the flag is off

After `/exit`, 20 s of polling:

```
t+1s .. t+20s: workspaces ['w3'] tabs ['w3:t1','w3:t2'] panes ['w3:p1','w3:p2']
pane.process_info w3:p2 -> fg 3253243 bash /bin/bash  (claude 3253402 gone)
```

**Verdict: PASS.** Old behavior unchanged: pane and its tab survive at the shell.

## CLI Leg 4 — pi, `--close-on-exit`, Ctrl-D

The project-local extension from the earlier pass carried over
(`SP/proj/.pi/extensions/duo-pi-reporter.ts`), so this leg ran CLI-level too.

```
$ SP/duo-go session launch probe_pi --close-on-exit
session:  ses_37e02fe8646b4a3879fcb998ba1a61d1
record:   lrr_9603ec25c590a117e869a8275a5710c9
  main: pi / gpt-5.6-luna (determined) -> selected
snapshot: workspaces ['w3'] tabs ['w3:t1','w3:t2','w3:t3']
          panes ['w3:p1','w3:p2','w3:p3']
```

The augmenter's env marker, delivered by the real CLI through herdr's pane env
map and confirmed on both the pane shell and the pi process itself:

```
/proc/3255802/environ (pane shell):  DUO_CLOSE_PANE_ON_EXIT=1
/proc/3255962/environ (pi):          DUO_CLOSE_PANE_ON_EXIT=1
typed launch line:  pi --provider openai-codex --model gpt-5.6-luna
```

pi's banner listed `[Extensions] duo-pi-reporter.ts, herdr-agent-state.ts,
moshi-hooks.ts, pi-effort`. Project trust answered **"Trust (this session
only)"** again, so no durable pi trust entry was created.

After `ctrl+d`:

```
t+1s .. t+3s: tabs ['w3:t1','w3:t2','w3:t3'] panes ['w3:p1','w3:p2','w3:p3']
t+4s:         tabs ['w3:t1','w3:t2']         panes ['w3:p1','w3:p2']
```

**Verdict: PASS.** pi's pane and its lone-pane tab close; the workspace
correctly survives because other tabs still hold panes — the first live proof
that the close is scoped to the container duo created, not to the workspace.

## CLI-level rerun summary

| leg | verdict |
|---|---|
| 1 — claude, flag on, `/exit` -> container gone | **PASS** |
| 2 — claude, flag on, `kill -9` -> pane survives | **PASS** |
| 3 — claude, flag off, `/exit` -> pane survives | **PASS** |
| 4 — pi, flag on, Ctrl-D -> container gone | **PASS** |
| scrub gate | **PASSES** on the clean server |

## Observation 1 confirmed at CLI level

The harness-directory leak is real and reproducible through the CLI. After the
rerun, `SP/xdg/data/duo/harness/` holds:

```
lrr_9210b89b3b4d0a31403cd6aab980a03b   <- the earlier REFUSED launch (orphan)
lrr_816026501e8c5e35c4ab801abc57d718   <- CLI leg 1, session ended cleanly
lrr_9018ae2cbc078ee65a43afb8a0a43188   <- CLI leg 2, agent killed
```

Both completed claude close-on-exit launches left their generated hook and
settings behind after the session was over, and so did the refused one. The pi
leg (`lrr_9603ec...`) correctly created nothing — the leak is claude-leg-only.

## Cleanup (rerun)

- `w3:p1` and `w3:p2` — the two panes that survived by design in legs 2 and 3 —
  were closed with `pane.close` on the clean socket.
- Final `session.snapshot` on `duo46-clean`:
  `{"workspaces": [], "tabs": [], "panes": [], "layouts": [], "agents": []}`
- The `duo46-clean` server is left running. `duo46-probe` was not touched during
  the rerun and was already empty.
- No server was started or stopped.
- `/home/dev/Code/duo-vnext` working tree still unmodified beyond the original
  6 modified + 4 untracked files.

---

# pi -e fix rerun (duo46-fix)

The earlier pi PASS was invalid: it was carried by a **project-local install of
the reporter extension** at `SP/proj/.pi/extensions/duo-pi-reporter.ts` that I
had created by hand. Nothing installs that extension in production, so the env
var alone was inert — my harness masked the defect.

**First action of this round was to delete that install** (`rm -rf SP/proj/.pi`),
so nothing project-local could mask the new mechanism. Verified absent before
every launch below.

Binary: `make build` -> `/home/dev/Code/duo-vnext/bin/duo`,
`duo version c2cab35-dirty (commit c2cab35, built 2026-08-24T22:55:27Z)`.
`~/.local/bin/duo-go` was not overwritten.
Server: `/home/dev/.config/herdr/sessions/duo46-fix/herdr.sock`, empty at start.
Deduction re-confirmed via `duo doctor` -> `host_source=ambient-env` on that socket.

## Fix under test

`stage1LeafAugmenter.Augment`'s `case "pi"` now materializes
`internal/runtime/pi/closeonexit/duo-close-on-exit.ts` into the same per-launch
harness dir and returns `Args: ["-e", extensionPath]` **plus** the existing
`Env: {DUO_CLOSE_PANE_ON_EXIT: "1"}`.

## Leg 1 — pi, `--close-on-exit`, Ctrl-D

```
$ bin/duo session launch probe_pi --close-on-exit
exit=0
record:   lrr_3f116c8024124841ea71bf269da5660f
  main: pi / gpt-5.6-luna (determined) -> selected
snapshot: workspaces ['w1'] tabs ['w1:t1'] panes ['w1:p1']
```

Materialized file:

```
SP/xdg/data/duo/harness/lrr_3f116c8024124841ea71bf269da5660f/main/
drwx------  (dir 0700)
-rw-------  duo-close-on-exit.ts   5420 bytes   (0600, matching closeonexit.go)
```

`DUO_CLOSE_PANE_ON_EXIT=1` confirmed on the pi process's own
`/proc/<pid>/environ`.

**Live argv.** `pi` rewrites its process title, so `/proc/<pid>/cmdline` and
herdr's `pane.process_info` both report bare `pi`. Captured from the pane screen
on a second flag-on launch instead, read before it scrolled
(`lrr_9d67f3fa4798759f39d369eb341f5420`):

```
λ …/proj › pi --provider openai-codex --model gpt-5.6-luna \
  -e /tmp/.../xdg/data/duo/harness/lrr_9d67f3fa4798759f39d369eb341f5420/main/duo-close-on-exit.ts
```

`-e` is appended after the variant's own `append_arguments`, and the path names
that launch's own resolution id.

**Independent proof the `-e` load is what activated it** — pi's startup banner
lists the extension, and it exists in no discovery path:

```
[Extensions]
  duo-close-on-exit.ts, herdr-agent-state.ts, moshi-hooks.ts, pi-effort

$ ls ~/.pi/agent/extensions/     -> herdr-agent-state.ts, moshi-hooks.ts   (no duo file)
$ ls -d SP/proj/.pi              -> No such file or directory
```

`duo-pi-reporter.ts` is correctly **absent** from the loaded list — the fix does
not depend on the reporter.

Ctrl-D, both runs:

```
run 1: t+1s panes ['w1:p1']  ->  t+2s workspaces [] tabs [] panes []
run 2: t+1s panes ['w2:p1']  ->  t+2s workspaces [] tabs [] panes []
```

**Verdict: PASS (2/2).**

## Trust prompt: NONE

The specific question — whether the explicit `-e` load triggers pi's project
trust gate — answers **no**. The pane screen goes straight from the typed launch
line to pi's banner:

```
λ …/proj › pi --provider openai-codex --model gpt-5.6-luna -e /tmp/.../duo-close-on-exit.ts

 pi v0.83.0
 escape interrupt · ctrl+c/ctrl+d clear/exit · / commands · ! bash · ctrl+o more
```

No "Trust project folder?" dialog appeared on either flag-on launch, and none was
answered. This is a real improvement over the old project-local approach, which
required a trust answer ("Trust (this session only)") before the extension would
load — an interactive gate no headless duo launch could have cleared. Consistent
with the asset's own note that an explicit `-e` path bypasses discovery entirely.

## Leg 2 — pi control, flag off

```
$ bin/duo session launch probe_pi
exit=0
record:   lrr_c91791a504402e15beafd37c1e980bd4
snapshot: workspaces ['w3'] tabs ['w3:t1'] panes ['w3:p1']
```

Four negative checks, all correct:

```
typed line:   pi --provider openai-codex --model gpt-5.6-luna     (no -e)
harness dir:  lrr_c91791a504402e15beafd37c1e980bd4 -> does not exist
pane env:     no DUO_* variables at all
[Extensions]: herdr-agent-state.ts, moshi-hooks.ts, pi-effort     (no duo file)
```

After `ctrl+d`, 12 s of polling:

```
t+1s .. t+12s: workspaces ['w3'] tabs ['w3:t1'] panes ['w3:p1']
pane.process_info -> shell_pid 3352922, fg 3352922 bash /bin/bash
```

**Verdict: PASS.** pi exits, the pane stays at the shell.

## Leg 3 — claude regression, flag on

```
$ bin/duo session launch probe_claude --close-on-exit
exit=0
record:   lrr_bcfe54f8c3d1f951f8fe95c3f5aa41a4
snapshot: workspaces ['w3'] tabs ['w3:t1','w3:t2'] panes ['w3:p1','w3:p2']
```

Claude branch untouched by the pi change:

```
live argv: claude --model haiku --settings \
  SP/xdg/data/duo/harness/lrr_bcfe54f8c3d1f951f8fe95c3f5aa41a4/main/close-on-exit-settings.json
harness dir contents (claude files only, no pi extension):
  -rw-------  close-on-exit-settings.json   356   (0600)
  -rwx------  session-end-hook.sh          4184   (0700)
```

After `/exit`:

```
t+1s .. t+7s: tabs ['w3:t1','w3:t2'] panes ['w3:p1','w3:p2']
t+8s:         tabs ['w3:t1']         panes ['w3:p1']
```

**Verdict: PASS.** Claude's container (pane + its lone tab) closes; the workspace
survives because the leg-2 control pane still holds a tab — correct scoping.

## Unit tests

`go test ./internal/runtime/pi/... ./internal/cli/... ./internal/runtime/claude/...
./internal/launch/...` — all `ok`.

## Rerun summary

| leg | verdict |
|---|---|
| 1 — pi, flag on, `-e <path>` + Ctrl-D -> container gone | **PASS** (2/2) |
| 2 — pi control, flag off -> pane survives | **PASS** |
| 3 — claude regression, flag on -> container gone | **PASS** |
| pi trust prompt on explicit `-e` | **none — no gate, nothing answered** |

## Harness leak: still open, now on both legs

The claude-leg leak reported earlier now reproduces on the pi leg too — each
flag-on launch leaves its generated file behind after the session ends. Six
orphan directories accumulated across this session's launches:

```
lrr_3f116c8024124841ea71bf269da5660f   pi   (duo46-fix leg 1 run 1)
lrr_9d67f3fa4798759f39d369eb341f5420   pi   (duo46-fix leg 1 run 2)
lrr_bcfe54f8c3d1f951f8fe95c3f5aa41a4   claude (duo46-fix leg 3)
lrr_816026501e8c5e35c4ab801abc57d718   claude (duo46-clean leg 1)
lrr_9018ae2cbc078ee65a43afb8a0a43188   claude (duo46-clean leg 2)
lrr_9210b89b3b4d0a31403cd6aab980a03b   claude (the original REFUSED launch)
```

Flag-off launches correctly create nothing.

## Cleanup (pi -e fix rerun)

- `w3:p1`, the control pane that survived by design in leg 2, closed with
  `pane.close` on the `duo46-fix` socket.
- Final `session.snapshot` on `duo46-fix`:
  `{"workspaces": [], "tabs": [], "panes": [], "layouts": [], "agents": []}`
- Server left running; no server started or stopped.
- `SP/proj/.pi/` remains deleted — the masking install is gone for good.
- `~/.pi/agent/` and `~/.claude/settings.json` untouched; no pi trust entry was
  created this round (no prompt appeared).
