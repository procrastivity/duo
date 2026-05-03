# Handoff: Duo CLI control plane (core)

## Goal

Give Duo a real CLI surface so humans can drive the project's own MCP tools — and a small, curated set of Solo passthroughs — without going through Claude Code or `mcp2cli`. The CLI is for tangible exploration and day-to-day workflows, not for full Solo coverage (use `mcp2cli` when you need raw access to anything Solo exposes; see `notes/mcp2cli-notes.md`).

This doc covers the **core** surface. Stretch features live in `notes/cli-control-plane-stretch.md`. The interactive REPL lives in `notes/cli-repl.md`.

## Dependencies

This work assumes the connect-time scope resolution from `notes/make-process-id-usable.md` has landed (env + pwd → `projectId`, env → `processId` + `bind_session_process`). Every CLI subcommand reuses that same `SoloClient.connect()` dance, so it must be done once and done well. Land that handoff first, or coordinate the two.

## Top-level reorg

```
duo                 → print help (today: starts MCP server)
duo mcp             → start the MCP server (the current bare-duo behavior)
duo <subcommand>    → CLI subcommands
```

Bare `duo` printing help is a behavior change. Anyone whose runner invokes `duo` expecting the server will need to switch to `duo mcp`. Search for invocations across:

- `duo.config.yaml` examples in the README
- `notes/playbook/` references
- any agent-tool definitions / Solo `solo.yml` entries that point at the duo binary
- `docs/solo-orchestrator-companion-prd.md` and stories

Update them as part of this change.

## CLI framework

Pick one and stop. Suggested: **`citty`** (UnJS, lightweight, Bun-friendly, declarative subcommands). Acceptable alternatives: `commander` (mature, common), `clipanion` (great types, heavier). Avoid `oclif` (heavy, generator-driven; overkill).

Whatever's chosen, the framework should give us:
- nested subcommands (`duo proc ls`)
- typed flags with defaults
- automatic `--help` / per-command help
- a clean way to share global flags (`--json`, `--quiet`, `--cwd`) across commands

## Core command surface

### Duo's own tools

```
duo agent list                       → list_agent_tiers
duo agent resolve <tier>             → resolve_agent_tool
duo agent spawn <tier> [--name N] [--project-id ID]
                                     → spawn_agent
```

`agent` is the chosen noun (over `tier`); tiers are an implementation detail of the agent surface.

### Solo passthroughs (curated)

Only commands where Duo adds value over `mcp2cli` — auto-resolved scope, sane defaults, friendlier output.

```
duo whoami                           # process_id, project_id, project name/path
duo project ls                       # list_projects
duo project status                   # get_project_status

duo proc ls                          # list_processes (current project)
duo proc logs <id> [--follow] [--since 5m]
                                     # get_process_output
duo proc grep <id> <pattern>         # search_output
duo proc status <id>                 # get_process_status
duo proc stop <id>                   # stop_process
duo proc restart <id>                # restart_process
duo proc kill <id>                   # close_process
```

### Diagnostics & meta

```
duo doctor                           # health check (see below)
duo version                          # version + git sha
duo config show                      # effective config after env merge
duo config path                      # which duo.config.yaml is loaded
```

`duo doctor` is the highest-value affordance in the whole proposal. It should check, in order:

1. `duo` binary version + git sha
2. `duo.config.yaml` discovered + parses cleanly
3. Solo binary discoverable (transport command resolves)
4. Solo MCP handshake succeeds (`initialize` + `notifications/initialized`)
5. `SOLO_PROJECT_ID` env value, if set
6. `SOLO_PROCESS_ID` env value, if set
7. cwd → project resolution result (matched / no match / multiple → longest)
8. `bind_session_process` outcome, if attempted
9. `list_agent_tiers` returns a non-empty list

Output: one line per check, ✓ / ✗ / —, with the actual values. Exit non-zero on any ✗.

## Cross-cutting conventions

- `--json` on every read command. Default is a small human-readable table.
- `-q` / `--quiet` for scripting: emit only the primary identifier (e.g. process id).
- `--cwd <path>` for testing; defaults to `process.cwd()`. Consumed by the connect-time pwd→project derivation from `make-process-id-usable`.
- `DUO_PROJECT=<id>` env override for one-shot project switches.
- Exit codes:
  - `0` — success
  - `1` — user error (bad args, unknown subcommand, validation)
  - `2` — Solo error (server returned an error response; print structured `{code, message}` to stderr)
  - `3` — connection error (handshake failed, transport died, no Solo binary)
- All commands share the connect dance. Each invocation = one fresh `SoloClient` connection, run subcommand, disconnect. Cheap enough for short-lived calls; the REPL handoff covers the persistent case.

## Output format guidelines

- Tables: align columns, no fancy unicode borders. Just whitespace.
- Truncate long fields (process names, paths) by default; full output with `--json` or a wider terminal.
- Color: yes, but respect `NO_COLOR` and `--no-color`. Default to off when stdout is not a TTY.
- Errors to stderr; data to stdout. Always. So `duo proc ls --json | jq` never sees a stray log line.
- Logs from the connect dance go to stderr at `info`. `--quiet` suppresses them.

## Out of scope (in this doc)

- Stretch features (`--all` for cross-project listing, `--attach`, `--wait`, completions, etc.) — see `notes/cli-control-plane-stretch.md`.
- Interactive REPL (`duo repl`) — see `notes/cli-repl.md`.
- Solo primitives Duo doesn't currently use: KV, scratchpads, todos, timers, locks. Use `mcp2cli` for these. If Duo grows a domain reason to expose them, revisit then.
- New write commands beyond what Duo's MCP tools already write. The CLI is a thin face on existing tool semantics.

## Anti-goals

- **No `duo proc spawn`** for raw process spawning. That's `mcp2cli` territory; agents spawn via `duo agent spawn`. If a non-agent process needs spawning, it doesn't belong to Duo.
- **No re-implementation of mcp2cli.** If the only value-add of a hypothetical `duo <tool>` is "we typed the flag names ourselves," skip it.
- **No noun-explosion.** Resist adding `duo kv`, `duo lock`, `duo timer`, etc. They're Solo's primitives, not Duo's.

## Implementation steps

1. **Choose the framework.** Add the dep. Set up `src/cli/` with the entry point.
2. **Wire `bin`** in `package.json` so `duo` resolves to the CLI router. Bare `duo` → help. `duo mcp` → call into the existing server entry point (`src/index.ts` / `src/server.ts`).
3. **Shared connect helper.** A single `connectSolo(opts)` factory used by every subcommand. Wraps the connect dance, returns the client and a `dispose()` callback. Each subcommand: connect → run → dispose.
4. **`duo doctor` first.** It exercises the connect dance most thoroughly and gives immediate feedback on the refactor's correctness.
5. **`duo whoami` and `duo project ls`** next — trivial readers, prove the table/JSON output convention.
6. **`duo agent list` / `duo agent resolve`** — proves Duo's own tools route through the CLI cleanly.
7. **`duo agent spawn`** — first writer; verify error propagation and exit codes.
8. **`duo proc *`** — bulk of the Solo passthroughs. Watch the `--follow` case for `logs` (need streaming, not buffered output).
9. **`duo config show` / `duo config path` / `duo version`** — bookkeeping.
10. **Tests.** End-to-end against a fake `SoloClient` for argument parsing + output. A small handful of integration tests that drive a real Solo instance behind a feature flag (skip in CI unless Solo is available).
11. **Docs.** Update `README.md` to lead with `duo doctor` and `duo agent spawn` examples. Move "starts an MCP server" to a section about `duo mcp`.

## Files most likely to change

- `package.json` — add CLI framework dep; `bin` entry stays.
- `src/cli/` (new) — `index.ts`, `commands/agent.ts`, `commands/proc.ts`, `commands/project.ts`, `commands/doctor.ts`, `commands/whoami.ts`, `commands/config.ts`, `commands/version.ts`, `commands/mcp.ts` (delegates to existing server entry).
- `src/cli/connect.ts` (new) — shared `connectSolo()` helper.
- `src/cli/output.ts` (new) — table/json formatting helpers.
- `src/index.ts` — becomes the CLI entry; defers to `src/server.ts` only on `duo mcp`.
- `src/server.ts` — exposes a `runServer()` function the CLI can call.
- `README.md` — substantial rewrite of the "running" section.
- `docs/solo-orchestrator-companion-prd.md` — note the new `duo mcp` invocation.

## Estimated effort

~1 full day, gated on `make-process-id-usable` having landed. Most of the time goes into: framework wiring, output formatting, and `duo doctor`. Each individual subcommand after that is 30–60 minutes including tests.
