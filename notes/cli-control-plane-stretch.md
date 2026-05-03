# Handoff: Duo CLI control plane (stretch features)

Companion to `notes/cli-control-plane.md`. Land that handoff first; this is a menu of opt-in additions that earn their keep only after the core surface exists.

These are listed roughly in order of value-to-effort. Pick what hurts most.

## Cross-project process listing — `duo proc ls --all`

```
duo proc ls --all
```

Iterates `list_projects` → `list_processes(project_id=N)` per project, merges into one table with a `project` column.

**Why stretch:** `list_processes` cannot do a cross-project query (confirmed via mcp2cli; see `notes/make-process-id-usable.md`). The only workaround is the loop — mechanical but wastes round-trips. If/when Solo adds a native cross-project listing, replace the loop with one call.

**Effort:** ~30 min. Trivial once the project list is in hand.

**Caveat:** in projects without `bind_session_process`, `list_processes` may also require a `process_id`. Worth confirming: per-project listings might fail silently for projects where the caller has no bound process. If so, fall back to skipping those projects with a warning.

## Spawn-and-attach — `duo agent spawn ... --attach`

```
duo agent spawn coder --name fix-auth --attach
```

Spawns the agent, then immediately tails its output. Equivalent to `duo agent spawn coder --name fix-auth && duo proc logs fix-auth --follow`, but one command.

**Why stretch:** Real ergonomic win for "watch this thing run" workflows. Belongs in stretch only because it requires composing two existing capabilities cleanly, with thoughtful Ctrl-C handling and exit-code propagation.

**Effort:** ~1 hour. Mostly signal handling and deciding what `--attach` means when the spawned process exits (return its exit code? always 0? configurable?).

## Spawn-and-wait — `duo agent spawn ... --wait`

```
duo agent spawn coder --name fix-auth --wait
```

Blocks until the spawned process is *bound and healthy* (whatever Solo's readiness signal is — `wait_for_bound_port`, status transitions, etc.), then returns. Useful in scripts that need the process up before doing the next thing.

**Why stretch:** Lower value than `--attach` for interactive use, but high value for automation. Also depends on having a clear "process is ready" signal from Solo.

**Effort:** ~1–2 hours, depending on what Solo exposes for readiness.

## Process name → id resolution

```
duo proc logs orchestrator           # instead of duo proc logs 42
duo proc stop orchestrator
```

If the argument doesn't look like a numeric id, treat it as a name and resolve via `list_processes`. On duplicates: prefer the most recently started, warn to stderr.

**Why stretch:** Pure ergonomics. The core surface uses ids; resolution is a quality-of-life upgrade.

**Effort:** ~1 hour including the dupe-handling tests. Worth doing once `duo proc *` commands are bedded in.

**Risk:** Naming collisions are silent footguns. Mitigation: always echo the resolved id to stderr (`→ resolved orchestrator → 42`) unless `--quiet`.

## Shell completions — `duo completion <shell>`

```
duo completion bash > /etc/bash_completion.d/duo
duo completion zsh > ~/.zsh/completion/_duo
duo completion fish > ~/.config/fish/completions/duo.fish
```

**Why stretch:** Most CLI frameworks (`citty`, `commander`, `clipanion`) have completion generators built in or as plugins. Low effort; high daily value once installed.

**Effort:** ~30 min wiring, plus install instructions in the README.

**Bonus:** dynamic completion for process ids (call `list_processes` from the completion script). Skip on the first pass — static command/flag completion is enough.

## `duo events tail` (if/when Solo exposes events)

```
duo events tail [--filter spawn,exit]
```

Stream Solo events as they happen. Useful for watching what an orchestrator is doing without grepping logs.

**Why stretch:** Depends on Solo exposing an event stream. Today's MCP surface is request/response; check whether Solo has a `subscribe_*` or notification mechanism before scoping this.

**Effort:** unknown until the Solo capability is verified. Could be 1 hour or "not possible."

## `duo proc top`

```
duo proc top
```

A live-updating `top`-style view of all processes in the current project: name, status, uptime, last log line. Refreshes every 2s.

**Why stretch:** Genuinely nice for orchestration debugging, but it's a real TUI (probably needs `blessed` / `ink` / similar) and adds dep weight. Only worth it if you find yourself running `watch -n2 duo proc ls` regularly.

**Effort:** ~half day, mostly TUI library setup.

## `duo config validate`

```
duo config validate [path]
```

Parses a `duo.config.yaml` and reports errors with line numbers. Useful pre-commit / CI.

**Why stretch:** `duo doctor` already covers this for the *active* config. A standalone validator helps when editing examples or generating configs programmatically.

**Effort:** ~30 min if Zod's error reporting is leveraged directly.

## Anti-stretch (explicitly not doing)

- **`duo agent kill <name>`** — already covered by `duo proc kill <id>` plus the name-resolution stretch. No need for a separate verb.
- **`duo orchestrator status` / `duo orchestrator continue`** — these are *prompts*, not commands. They belong in the playbook, not the CLI. Trying to encode them as subcommands re-creates the playbook in code.
- **A `duo plugin` system.** YAGNI. If Duo grows extensibility needs, MCP itself is the extension point.
- **`duo init`** for scaffolding new projects. Solo's own project-creation flow handles this; Duo doesn't need to mirror it.

## Suggested sequencing

If picking up multiple of these, this order tends to compound nicely:

1. **Process name → id resolution** — every `duo proc *` command improves immediately.
2. **`--attach`** — biggest day-to-day ergonomic win.
3. **Shell completions** — small effort, lasting payoff.
4. **`--all`** — when cross-project visibility starts to matter.
5. **`--wait`** — when scripted spawn flows show up.
6. Everything else as actual pain motivates it.
