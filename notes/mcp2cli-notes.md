# Solo MCP → CLI: notes and options

## Full list of Solo MCP tools

Grouped by domain (extracted from the deferred-tool list):

**Project / session**
- `list_projects`, `select_project`, `get_project_status`, `get_project_stats`, `whoami`, `help`, `bind_session_process`, `submit_solo_feedback`

**Processes**
- `spawn_process`, `start_process`, `stop_process`, `restart_process`, `close_process`, `rename_process`, `list_processes`
- `start_all_commands`, `stop_all_commands`, `restart_all_commands`
- `get_process_status`, `get_process_output`, `get_process_raw_output`, `get_process_ports`, `clear_output`, `search_output`, `search_raw_output`, `send_input`, `wait_for_bound_port`
- `services_list`

**Agent integration**
- `register_agent`, `setup_agent_integration`, `list_agent_tools`

**KV store**
- `kv_get`, `kv_set`, `kv_list`, `kv_delete`

**Locks**
- `lock_acquire`, `lock_release`, `lock_status`

**Scratchpads**
- `scratchpad_write`, `scratchpad_read`, `scratchpad_append`, `scratchpad_clear`, `scratchpad_delete`, `scratchpad_rename`, `scratchpad_list`, `scratchpad_archive`, `scratchpad_transfer`, `scratchpad_save_to_file`, `scratchpad_load_from_file`, `scratchpad_add_tags`, `scratchpad_remove_tags`, `scratchpad_tags_list`

**Todos**
- `todo_create`, `todo_get`, `todo_list`, `todo_update`, `todo_complete`, `todo_delete`, `todo_transfer`, `todo_lock`, `todo_unlock`
- `todo_add_tag`, `todo_remove_tag`, `todo_tags_list`
- `todo_add_blocker`, `todo_remove_blocker`, `todo_set_blockers`
- `todo_comment_create`, `todo_comment_update`, `todo_comment_delete`, `todo_comment_list`

**Timers**
- `timer_set`, `timer_cancel`, `timer_list`, `timer_pause`, `timer_resume`, `timer_fire_when_idle_any`, `timer_fire_when_idle_all`

---

## Existing tooling — you may not need to build this

Several projects already wrap arbitrary MCP servers as CLIs at runtime, no codegen:

- **mcp2cli** (knowsuchagency) — `mcp2cli --mcp-stdio "<server cmd>" <tool> --arg value`. Lists tools, calls them, caches schemas. Closest to "wrap any MCP as a CLI" with zero work.
- **mcptools** (f/mcptools) — Go CLI: `mcp tools <server>`, `mcp call <tool> <server>`, plus an interactive shell.
- **mcp-cli** (Phil Schmid's writeup) — v0.3.0 has an `info`/`grep`/`call` subcommand split with a connection-pooling daemon to avoid restart cost.
- **mcpc** (apify) — universal client with persistent sessions, stdio + HTTP, OAuth, JSON output.
- **FastMCP** ships its own `fastmcp` CLI for servers built on it.

If the goal is just "drive Solo from a shell," `mcp2cli` or `mcptools` likely covers it. Both surface arguments as `--flag value` pairs derived from the tool's JSON Schema, with JSON passthrough for object/array params.

The case for hand-rolling something Duo-shaped:
1. **Naming.** Generic wrappers expose `list_processes`; you'd want `duo proc ls`. That's purely cosmetic but matters if humans type it often.
2. **Defaults.** Duo always operates in a known project context — a hand-rolled CLI can default `project_id`, bind sessions automatically, etc.
3. **Composition.** Pipelines like `duo proc logs orchestrator --since 5m | grep ERROR` benefit from a real CLI shape, not a JSON dump.

If those don't matter much, adopt `mcp2cli` and stop. If they do, the wrapper is small (one Python/TS file plus a noun→tool map).

---

## Hypothetical `duo` CLI — naming and argument shape

The Hypomnema pattern (`hmn vault list` ↔ `list_vaults`) generalizes well. Two viable styles:

**Style A — noun-first (Hypomnema/kubectl/gh).** Reads naturally for humans, requires a hand-maintained map.

```
list_processes                  → duo proc ls
get_process_output              → duo proc logs <id>
spawn_process                   → duo proc spawn -- <cmd...>
restart_all_commands            → duo proc restart --all
search_output                   → duo proc grep <id> <pattern>
wait_for_bound_port             → duo proc wait-port <id> --timeout 30s

list_projects                   → duo project ls
select_project                  → duo project use <id>
get_project_status              → duo project status

kv_get / kv_set / kv_list       → duo kv get|set|ls <key> [value]
lock_acquire / lock_release     → duo lock acquire|release <name>

scratchpad_write                → duo pad write <name> --content @-     # @- = stdin
scratchpad_append               → duo pad append <name> --content @file.md
scratchpad_list --tags a,b      → duo pad ls --tag a --tag b
scratchpad_save_to_file         → duo pad export <name> <path>

todo_create                     → duo todo new "<title>" --tag x --blocker T-12
todo_set_blockers               → duo todo blockers set <id> T-1 T-2 T-3
todo_comment_list               → duo todo comment ls <id>

timer_set                       → duo timer set <name> --in 5m --message "..."
timer_fire_when_idle_any        → duo timer when-idle any <name...>
```

**Style B — verb-passthrough (mechanical, zero map).** Generated automatically: `snake_case` → `kebab-case`, no grouping.

```
list_processes        → duo list-processes
get_process_output    → duo get-process-output <id>
todo_set_blockers     → duo todo-set-blockers <id> T-1 T-2
```

Style B is what `mcp2cli`/`mcptools` give you for free. Style A is what you'd hand-roll. A hybrid is fine: ship Style A for the ~20 tools you use daily, fall through to Style B (`duo raw <tool> --arg=...`) for the long tail.

### Argument shape

Most Solo tools take JSON-Schema-typed inputs. CLI conventions for each kind:

| Schema type | CLI form |
|---|---|
| `string`, `number`, `boolean` | `--name value`, `--count 3`, `--force` (boolean flags) |
| `enum` | `--status active\|paused\|done` (validated client-side) |
| `array<string>` | repeated flag: `--tag a --tag b`, or comma list: `--tags a,b`, or positional rest: `duo todo blockers set <id> T-1 T-2 T-3` |
| `array<object>` | `--items @file.json` or `--items '<json>'` |
| `object` | `--config @file.json`, or dotted keys: `--config.host=... --config.port=...` |
| Large text bodies (scratchpad content, todo description) | `--content @-` (stdin), `--content @file.md`, or `--content "inline"` |
| Output | default human table; `--json` for machine-readable; `--watch` for streaming where it makes sense (`proc logs`, `timer list`) |

Examples of the harder cases:

```
# object argument via dotted keys OR file
duo proc spawn --name api --command "bun run dev" --env.PORT=3000 --env.DEBUG=1
duo proc spawn --spec @spec.json

# array of strings (three forms supported)
duo todo new "ship it" --tag backend --tag urgent
duo todo new "ship it" --tags backend,urgent
duo pad ls -- backend urgent          # positional rest after --

# stdin for large content
git diff | duo pad append review-notes --content @-

# nested/structured output piping
duo proc ls --json | jq '.[] | select(.status=="running") | .id'
```

### Project/session context

Solo's `whoami`/`select_project`/`bind_session_process` deserve sugar:

```
duo use <project>            # writes ~/.duo/config or env, persists selection
duo whoami                   # prints process_id, actor_id, project
DUO_PROJECT=foo duo proc ls  # one-shot override
```

That removes `--project-id` from every other call.

---

## Recommendation

Try `mcp2cli` against the Solo MCP today (one shell line, no build) to see whether the mechanical mapping is good enough. If the noun-first ergonomics matter — and given Duo is itself an orchestration UX, they probably do — wrap it in a thin `duo` shim that hand-maps the ~20 hot tools and falls through to a generic `duo raw <tool>` for the rest. Total cost is small and you keep the schema as the source of truth.

## Sources

- [knowsuchagency/mcp2cli](https://github.com/knowsuchagency/mcp2cli)
- [f/mcptools](https://github.com/f/mcptools)
- [Phil Schmid — Introducing MCP CLI](https://www.philschmid.de/mcp-cli)
- [apify/mcpc](https://github.com/apify/mcpc)
- [FastMCP CLI](https://gofastmcp.com/patterns/cli)
- [MCP-CLI Adapter](https://mcpservers.org/servers/inercia/mcp-cli-adapter)
- [CLI Wrapper (mcpmarket)](https://mcpmarket.com/server/cli-wrapper)
