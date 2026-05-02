# Shared Playbook Instructions

These instructions are shared across all roles: `Orchestrator`, `Coordinator`, `Researcher`, and `Builder`.

## Use Solo MCP

You are operating inside Solo MCP. Use Solo tools for process control, todos, scratchpads, timers, and process status.

- Use `whoami()` early to confirm your process identity.
- Keep process names explicit and role-scoped.
- Use todos as the primary durable coordination surface.
- Use scratchpads for rolling context, not as a replacement for todo state.

### Pausing and waiting

Solo timers let an agent pause and resume on a signal. When a timer fires, Solo injects its `body` into your PTY as a fresh user turn, so your next action picks up automatically — no polling loop, no burned context.

- `mcp__solo__timer_set` — pause for a fixed duration (one-shot, or repeating via `loop` / `repeat_every_ms`).
- `mcp__solo__timer_fire_when_idle_any` / `mcp__solo__timer_fire_when_idle_all` — resume when watched processes go idle (i.e., finish their current task). Use for worker quiet periods, not service readiness.
- `mcp__solo__wait_for_bound_port` — for service readiness (port open), not worker idle.

### Anti-pattern: Do not use `mcp-cli` from bash

❌ **Wrong**: Do not call `mcp-cli solo ...` from bash scripts or Monitor commands.

```bash
# WRONG — do not do this
mcp-cli solo get_process_output --process-name orchestrator
mcp-cli solo spawn_process kind=agent agent_tool_id=3
```

✅ **Right**: Use the Solo MCP tool interface directly via the tool calls available to you.

```
mcp__solo__get_process_output(process_name="orchestrator")
mcp__solo__spawn_process(kind="agent", agent_tool_id=3)
```

**Why**: `mcp-cli` is for CLI usage outside of MCP; inside an agent you have direct access to the MCP tools. Calling `mcp-cli` from bash introduces complexity, shell escaping issues, and slower poll loops. Instead:

- **For one-shot queries**: Call the MCP tool directly (e.g., `mcp__solo__get_process_output()`).
- **For polling/waiting**: Use `Monitor` with a bash loop that checks local conditions (file existence, exit codes from quick commands), not `mcp-cli` calls. Or use `mcp__solo__timer_fire_when_idle_any()` / `mcp__solo__timer_fire_when_idle_all()` for process idle detection.
- **For coordination**: Use `mcp__solo__kv_set()`, `mcp__solo__todo_*()`, `mcp__solo__scratchpad_*()` instead of bash-based state files.

### Solo control-plane terminology

Use this language consistently in prompts, comments, and playbook updates:

- `process`: the runtime instance managed by Solo (agent or terminal).
- `agent process`: a process spawned with `kind=\"agent\"` (used for orchestrator/coordinator/researcher/builder roles).
- `terminal process`: an interactive shell process (when shell execution is needed).
- `spawn`: create a new process via Solo MCP.
- `agent_tool_id`: the runtime/tool selection used when spawning an agent process.

When in doubt, refer to units as **processes** and then qualify as **agent process** or **terminal process** for clarity.

## Tiered Spawn Interface

Use the tiered spawn contract in [`notes/playbook/agent-tool-selection.md`](./agent-tool-selection.md):

- Request capability tier (`small`, `medium`, `large`) instead of hard-coding `agent_tool_id`.
- Prefer `/spawn-agent <tier>` as the control-plane abstraction.
- Keep direct `agent_tool_id` usage for exceptional/manual cases only.

## Role Invariants

- `Orchestrator` and `Coordinator` are never the same process.
- `Coordinator` and `Researcher` are separate processes.
- `Builder` processes are ephemeral and scoped to task execution.
- `Researcher` is long-lived for the lifetime of a step and remains available for consultation during build.

## Naming Conventions

| Thing | Pattern | Example |
|---|---|---|
| Coordinator process | `step-NN-coordinator` | `step-01-coordinator` |
| Researcher process | `step-NN-researcher` | `step-01-researcher` |
| Builder process | `step-NN-builder-MM` (or `-MM-r1`) | `step-01-builder-03` |
| Step context scratchpad | `step-NN-context` | `step-01-context` |
| Step todo | `Step N · Task M — <one-line>` | `Step 1 · Task 3 — Logging init` |
| Escalation todo | `[ESCALATION step-NN/builder-MM] <summary>` | `[ESCALATION step-01/builder-03] EnvFilter strategy unclear` |

## Tags

- `roadmap`
- `step-NN`
- `task`
- `needs-human`
- `escalation`
- `coordinator-context`

## Scratchpad Template

```markdown
# Step N — Rolling Context

**Coordinator**: <process name and id>
**Researcher**: <process name and id>
**Workplan**: notes/roadmap/step-NN-workplan.md
**Build started**: <ISO timestamp>

## Batching plan

| Batch | Tasks | Rationale |
|---|---|---|
| (filled by coordinator at setup) | | |

> **Live task status**: query `todo_list(tags=["step-NN"])` rather than maintaining a status table here.

## Decisions made during build

(append during build)

## Escalations

(append as escalations occur)

## Per-task outcomes

(append one outcome paragraph per task completion)
```
