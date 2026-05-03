# Handoff: `duo repl` — interactive MCP exploration

Companion to `notes/cli-control-plane.md`. The core CLI handles one-shot subcommands; the REPL handles persistent, exploratory sessions where round-trip cost and bound-session state both matter.

## Why a REPL

Two distinct gaps the one-shot CLI can't close:

1. **`bind_session_process` actually persists.** Each `duo <subcommand>` invocation opens a fresh `SoloClient`, runs one tool call, disconnects. The bind happens but immediately drops. A REPL holds one connection open across many calls — exactly the scenario `bind_session_process` is designed for. This is the killer feature.
2. **Tangible exploration.** The user has stated the CLI's main purpose is to "explore the MCP API in a tangible way versus being abstracted from view via a harness." `mcp2cli` does this for the raw Solo surface; `duo repl` does it for Duo's curated surface, with persistent state, history, and tab-completion that knows about *this* project's processes.

If neither of those matters for a given workflow, the one-shot CLI is enough. Don't build the REPL until you've felt the pain.

## Surface

```
$ duo repl
duo (project=duo, process=42) > help
duo (project=duo, process=42) > agent list
duo (project=duo, process=42) > agent spawn coder --name fix-auth
duo (project=duo, process=42) > proc logs fix-auth --follow
^C   (cancels the follow, returns to prompt — does not exit)
duo (project=duo, process=42) > exit
```

The prompt shows resolved scope, so the user always knows which project and process they're acting as. If `processId` is unbound, show `process=—`.

## What works inside the REPL

Every command from `notes/cli-control-plane.md` core surface, with these adjustments:

- **No `--cwd` flag.** The REPL captured cwd at startup; changing it mid-session would break scope assumptions.
- **No `duo mcp`.** Starting a server inside a REPL is nonsense.
- **No `duo doctor`.** Or rather: `doctor` is fine, but it should report against the *live* connection, not re-run the connect dance.
- **`exit` / `quit` / Ctrl-D** end the session cleanly (disconnect first, then exit).
- **Ctrl-C** cancels the *current* in-flight command (especially `--follow` streams) without killing the REPL itself. Second Ctrl-C at an empty prompt = exit.

## What's *additionally* useful in the REPL

```
help                                 # list available commands
help <command>                       # help for one command
history                              # last N commands
!<n>                                 # rerun command n
clear                                # clear the screen
set --json                           # toggle default --json on for the session
unset --json
scope                                # show current project / process
rebind <process_id>                  # call bind_session_process again (dangerous; warn)
```

`rebind` is a foot-gun and should be gated behind a `--yes-i-mean-it` flag or a confirmation prompt. The orchestrator companion's identity should not normally shift mid-flight.

## Tab completion

This is the second half of the value. Targets, in priority order:

1. **Subcommand names** — trivial, framework-provided.
2. **Flag names** — trivial, framework-provided.
3. **Process ids and names** — call `list_processes` lazily, cache for ~5s. Big quality-of-life jump.
4. **Project ids and names** — same idea, longer cache (60s).
5. **Agent tier names** — cached for the session; the tier list rarely changes.

A simple cache wrapping `list_processes` / `list_projects` / `list_agent_tiers`, invalidated on TTL or explicit `refresh`, is enough.

## Implementation choices

### Library

Pick **Node's built-in `readline` / `node:repl`** if simplicity is paramount. Pick **`ink`** or **`inquirer`** only if there's a real reason (rich rendering, multi-line editing). For an MCP REPL, `readline` is plenty.

### Output streaming

`proc logs <id> --follow` is the hard case. The REPL needs to:

1. Hand control of stdout to the streaming command.
2. Suspend the prompt.
3. Catch Ctrl-C and signal cancellation to the stream, not to the REPL process.
4. Restore the prompt on cancel or stream end.

Most readline libraries don't make this easy out of the box. Plan to do it manually: track an "active streaming command" handle on the REPL state, pause the readline interface, install a SIGINT handler that calls the handle's cancel, then resume readline on completion.

### Argument parsing inside the REPL

Parse REPL input the same way the core CLI parses argv. Tokenize the line (shell-style — handle quotes), prepend `duo`-equivalent context, hand off to the same parser used by the one-shot CLI. **Do not** maintain a second parser definition; that drift is unforgiving.

If the chosen CLI framework doesn't expose a "parse this argv" entry point cleanly, that's a strong signal to wrap it in one early so the REPL can reuse it.

## Connection lifecycle

```
on REPL start:
  connectSolo()           # same dance as core CLI
  resolve scope (project_id, process_id)
  display scope in banner
  if bind failed: print warning, continue

on each command:
  reuse the open client; do not reconnect

on REPL exit:
  disconnect cleanly; flush any pending streams
```

If the connection drops mid-session (Solo crashes, transport dies):
- Print a clear error.
- Offer to reconnect: `connection lost. type 'reconnect' to retry, 'exit' to quit.`
- Do **not** auto-reconnect silently — the session state is gone, and silently re-binding could land the user on a different process if Solo restarted.

## Out of scope

- **Recording / replay.** Recording a REPL session as a script would be cute but is YAGNI until someone asks twice.
- **Multi-session / multi-project REPL.** One REPL = one project = one bound process. Switching projects = exit and restart. (Or `set --project foo`, but that complicates the bind story; defer.)
- **Programmable scripting inside the REPL.** No `for` loops, no variables. If you need that, you're in the wrong tool — write a shell script that calls `duo` subcommands.

## Implementation steps

1. **Refactor the core CLI** so its argv parser can be invoked with an arbitrary argv array (not just `process.argv.slice(2)`). This is the prerequisite — without it, the REPL has to maintain a parallel command definition. Best done as part of the core CLI handoff if foreseen.
2. **`src/cli/commands/repl.ts`** — the new entry. Owns the readline loop, history, prompt, banner.
3. **`src/cli/repl/cache.ts`** — TTL cache wrapper around `list_processes` / `list_projects` / `list_agent_tiers` for completion.
4. **Streaming command handoff** — generalize whatever `proc logs --follow` does in the one-shot CLI so the REPL can swap stdout-ownership and SIGINT-handling around it.
5. **Tests.** Hard to fully automate, but a basic harness that pipes lines into a REPL instance and asserts on output covers most regressions.
6. **Docs.** A short README section: "want a persistent session? `duo repl`." Don't oversell it; it's a power-user feature.

## Effort

~1 day of focused work, gated on the core CLI being parser-reusable. Streaming + Ctrl-C handling is the part that surprises; budget half the time there.

## Open questions

- Does `bind_session_process` actually persist across many `tools/call` requests on a single MCP session? **Resolution: empirical test, same one called for in `notes/make-process-id-usable.md`.** If it doesn't persist, the REPL's headline value evaporates and this becomes a much weaker proposal — possibly not worth building at all.
- Should the REPL support `!<shell command>` shell-out (à la `psql`)? Convenient, but adds attack surface and complicates Ctrl-C handling. Default no; revisit if requested.
