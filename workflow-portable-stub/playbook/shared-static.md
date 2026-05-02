# Shared Playbook Instructions

These instructions are shared across all roles: `Orchestrator`, `Coordinator`, `Researcher`, and `Builder`.

## Use Solo MCP

You are operating inside Solo MCP. Use Solo tools for process control, todos, scratchpads, timers, and process status.

- Use `whoami()` early to confirm your process identity.
- Keep process names explicit and role-scoped.
- Use todos as the primary durable coordination surface.
- Use scratchpads for rolling context, not as a replacement for todo state.

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
