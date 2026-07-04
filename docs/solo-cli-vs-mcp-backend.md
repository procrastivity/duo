# Solo CLI vs MCP — backend decision

> Design record for Duo contributors. Status: **decided** — stay on the Solo
> MCP backend. Revisit only when the triggers below are met.

## Problem & question

Solo now ships a first-class `solo` CLI (v0.7.1) alongside its MCP server. Two
questions followed:

1. Should Duo shift from the Solo **MCP** backend to the Solo **CLI**?
2. If not, should Duo support **both** as insurance against the MCP and CLI
   surfaces drifting apart over time?

The underlying worry is API stability: neither the MCP tool surface nor the
CLI surface is contractually frozen, and a single hard dependency on either is
a risk if that surface changes.

## How Duo uses Solo today

Every Solo interaction goes through one seam: `SoloClient` in
[`src/solo-client.ts`](../src/solo-client.ts). It speaks MCP over JSON-RPC 2.0
through an injected `Transport` (stdio today). The class splits cleanly into
two halves:

- **Logical operations** (`src/solo-client.ts:156`–`249`) — the surface the
  rest of Duo actually consumes:
  - `listAgentTools()` → Solo `list_agent_tools`
  - `listProjects()` → Solo `list_projects`
  - `spawnProcess()` → Solo `spawn_process` (kind=`agent`)
  - `sendInput()` → Solo `send_input`
  - `_bindSessionProcess()` → Solo `bind_session_process`
  - `callTool<T>()` → generic passthrough for thin CLI wrappers
    (`get_process_status`, `stop_process`, …) with auto-injected `project_id`
- **MCP/JSON-RPC plumbing** (`src/solo-client.ts:251`–`301`) — `_request`,
  `_handleMessage`, `_extractText`, the pending-request map, message IDs, the
  `initialize` handshake. This half is transport-protocol-specific; nothing
  above the logical surface should know it exists.

Duo's **hot path** is narrow and specific: `list_agent_tools` is the *entire*
basis for mapping a requested tier (`small`/`medium`/`large`) to a concrete
`agent_tool_id`; `spawn_process` launches the chosen agent; `send_input`
delivers the bootstrap prompt to it. Without all three, Duo's core feature
does not function.

Note that scope resolution is **already transport-agnostic**. The cwd/env
longest-prefix logic lives in
[`src/solo-client/scope.ts`](../src/solo-client/scope.ts)
(`resolveProjectIdAtConnect`, `longestPathMatch`) as pure functions over
`{ id, name, path }` records — it does not care whether projects came from an
MCP tool or `solo projects list --json`.

## Capability-gap matrix (Solo CLI v0.7.1)

Confirmed against `solo --version` (0.7.1) and `solo processes spawn --help`.

| Duo-required operation        | Solo MCP | Solo CLI v0.7.1 | Notes |
|-------------------------------|:--------:|:---------------:|-------|
| `list_agent_tools`            | ✅       | ❌ **fatal**    | No CLI command enumerates agent runtimes. Tier→`agent_tool_id` mapping is impossible without it. |
| `send_input`                  | ✅       | ❌ **fatal**    | No CLI command sends input to a process. The bootstrap prompt cannot be delivered. |
| `spawn_process` (kind=agent)  | ✅       | ⚠️ partial      | `solo processes spawn --project-id <id> --kind agent --agent-tool-id <id> [--arg …]` exists, but requires an `agent_tool_id` the CLI cannot enumerate, and an explicit `--project-id`. |
| `list_projects`               | ✅       | ✅              | `solo projects list --json`. |
| `bind_session_process`        | ✅       | ❌              | No CLI equivalent; session-scoped binding is MCP-only. |
| Process output / status reads | ✅       | ⚠️ partial      | `solo processes get/list` only; no output/raw-output/search-output. |
| `whoami` / `identify_session` | ✅       | ❌              | No CLI identity/scope introspection. |

The CLI is HTTP-based (localhost control plane), stateless per invocation, and
`--json`-capable on every command — operationally clean, but functionally a
**subset** of the MCP surface for Duo's needs.

## Decision

**Stay on the Solo MCP backend. Do not switch, and do not build a dual
backend now.**

Rationale:

- A full switch is **impossible today**: `list_agent_tools` and `send_input`
  — two of Duo's three hot-path operations — have no CLI equivalent in v0.7.1.
- A CLI backend would be **strictly less capable** than the MCP backend, not
  an equal alternative. The current drift runs *against* the CLI (it trails
  the MCP surface), not toward it.
- The value of dual-backend support is therefore **future-proofing only**. It
  is real insurance, but it is not justified by present capability, and
  building an unusable second backend now is speculative cost with ongoing
  maintenance and test surface.

The correct hedge against drift is not a second implementation today — it is
keeping the seam clean so a second implementation is *cheap when warranted*.

## Future seam (documented, not built)

If the CLI reaches parity, dual-backend support is a contained refactor, not a
rewrite, because `SoloClient` already separates the logical surface from the
JSON-RPC plumbing:

1. Extract a `SoloBackend` interface equal to `SoloClient`'s logical surface:
   `listAgentTools`, `listProjects`, `spawnProcess`, `sendInput`,
   `bindSessionProcess`, `callTool` (plus the `projectId`/`processId`
   getters). This is the boundary at `src/solo-client.ts:156`–`249`.
2. `SoloClient` becomes the **MCP implementation** of that interface; the
   JSON-RPC half (`src/solo-client.ts:251`–`301`) stays private to it.
3. A future `SoloCliBackend` implements the same interface by shelling out to
   `solo … --json` and parsing stdout.
4. Scope resolution is **reused unchanged** —
   `src/solo-client/scope.ts` is already pure and transport-agnostic.

Rough cost when triggered: a new interface file, an interface-only change to
`SoloClient`'s declaration (no behavior change), and the CLI implementation
itself. The expensive part — disentangling protocol from logic — is already
done by the existing structure. **No part of this is implemented now**; it is
recorded so the option stays cheap.

## Revisit triggers

Reopen this decision if any of the following becomes true:

- The Solo CLI gains **both** `list_agent_tools` (or any agent-runtime
  enumeration) **and** `send_input` (or any process-input delivery). These are
  the minimum bar for a viable CLI backend; spawn alone is not enough.
- The Solo MCP server is deprecated, marked unstable, or its tool surface
  changes in a way that breaks Duo's hot path.
- Operational isolation from the MCP server lifecycle (e.g. running Duo
  actions without a live MCP session) becomes a hard product requirement —
  the CLI's stateless HTTP model would then have independent value.

## Relationship to Solo

This mirrors the stance already taken in
[`solo-cli-project-auto-scoping.md`](./solo-cli-project-auto-scoping.md) and
the analysis in [`../notes/mcp2cli-notes.md`](../notes/mcp2cli-notes.md): Duo
keeps its Solo-facing logic **client-side and transport-agnostic**, depends on
no Solo protocol changes, and treats the CLI as a portability target rather
than a coupling. The scope-resolution algorithm was already proven portable to
the CLI with zero protocol changes; the same client-side discipline is what
keeps the backend choice reversible here.
