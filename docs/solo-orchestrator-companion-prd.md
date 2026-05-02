# Solo Orchestrator Companion — PRD

**Author**: Codex / Solo orchestration session  
**Status**: Draft  
**Last Updated**: 2026-05-01  
**Scope Profile**: solo-local-cli  
**Stakeholders**: Solo users, playbook authors, agent-runtime maintainers  
**User Stories**: [`notes/proposals/solo-orchestrator-companion-stories.md`](./solo-orchestrator-companion-stories.md)

---

## 1. Problem Statement

Solo exposes low-level process tools that are powerful but brittle for orchestration workflows. Today, an agent or playbook that wants another agent must know which concrete `agent_tool_id` to pass to `spawn_process`. That ID is a local Solo database detail, not a product-level capability. It can vary across installations, change when tools are added or replaced, and forces workflow authors to encode environment-specific mappings in prompts or playbooks.

The actual user pain is not "agents need more spawn options." It is that orchestration authors want to say "spawn a small/medium/large agent for this job" and trust the local Solo environment to choose the right runtime. When each workflow manually resolves IDs, the same policy gets duplicated, drift-prone, and hard to audit.

This matters now because Solo already has enough runtime diversity to expose the problem: the current `list_agent_tools` output includes multiple enabled tools with model-bearing commands, such as `opencode-ghc-haiku`, `opencode-ghc-sonnet`, `codex-fast`, `codex-standard`, and `codex-flagship`. The metadata includes `id`, `name`, `command`, `tool_type`, and `enabled`, but does not expose explicit parsed model, context, cost, or capability fields. A lightweight companion can provide stable capability semantics without modifying Solo itself.

Evidence level: planning-conversation evidence plus live Solo MCP inspection from project 4. This PRD assumes the runtime list above is representative enough for a first local developer tool, but broader validation across other Solo installations remains open.

## 2. Current State & Research Findings

### Existing Functionality

Solo MCP currently exposes low-level process operations:

- `list_agent_tools` lists configured agent runtimes.
- `spawn_process(kind="agent", agent_tool_id=N, name=...)` creates a Solo-managed agent process.
- `whoami` can identify the current Solo process, actor, and effective project when the session is bound.

The current live tool metadata observed for this session:

| id | name | command | tool_type | enabled |
|---:|---|---|---|---|
| 2 | `opencode-ghc-haiku` | `direnv exec . opencode -m github-copilot/claude-haiku-4.5` | `opencode` | true |
| 10 | `opencode-ghc-sonnet` | `direnv exec . opencode -m github-copilot/claude-sonnet-4.6` | `opencode` | true |
| 15 | `codex-fast` | `direnv exec . codex --model gpt-5.4-mini` | `codex` | true |
| 16 | `codex-standard` | `direnv exec . codex --model gpt-5.3-codex` | `codex` | true |
| 17 | `codex-flagship` | `direnv exec . codex --model gpt-5.5` | `generic` | true |

### Technical Context

The desired product is standalone and not a Hypomnema feature. The architecture direction is:

`Agent/playbook -> companion orchestrator MCP -> Solo MCP client -> Solo MCP server -> Solo-managed processes`

Preferred stack:

- TypeScript on Node.js
- `@modelcontextprotocol/sdk`
- `zod` for schemas and config validation
- `yaml` for configuration files
- `execa` where child-process invocation is needed
- `vitest` for tests
- simple structured logging

There does not appear to be a Solo MCP tool that returns the current MCP connection descriptor or transport endpoint. The companion should therefore support explicit Solo connection configuration first, then use best-effort environment/project detection from `SOLO_PROJECT_ID` and `SOLO_PROCESS_ID` when available.

### Gaps & Opportunities

- No stable capability-tier surface exists for playbooks.
- No explicit Solo-provided model metadata exists, so tiering must be derived from available fields.
- Classifying primarily by human-friendly agent names would make names load-bearing and discourage readable local naming.
- A small standalone MCP server can centralize tier policy while leaving Solo unchanged.

## 3. Target Users & Personas

### Primary Persona: Playbook Author

Writes repeatable Solo orchestration flows and wants them to work across projects and machines. They care about stable intent-level APIs, deterministic behavior, and small prompts that do not hard-code local Solo IDs.

### Secondary Persona: Agent Operator

Runs orchestration sessions locally and needs spawned agents to have understandable names in Solo. They care about inspecting what happened, finding the right spawned process, and changing local runtime preferences without editing every playbook.

### Secondary Persona: Runtime Maintainer

Maintains the local set of Solo agent tools. They care about adding, disabling, or renaming tools without breaking orchestration workflows.

## 4. Goals & Success Metrics

| Metric | Current Baseline | Target | How Measured |
|---|---:|---:|---|
| Playbooks hard-coding `agent_tool_id` for agent spawning | Common/manual pattern | Zero in companion-adopted playbooks | Grep playbook repos for `agent_tool_id` outside the companion |
| Successful tier resolution for enabled local runtimes | Manual, unmeasured | 100% for documented supported model tokens in test fixtures | Vitest fixture suite against representative `list_agent_tools` payloads |
| Spawn flow completion through companion | N/A | `spawn_agent` returns a Solo process id and name when a tier has an enabled candidate | Integration test against a mock Solo MCP server; optional live smoke test |
| Guardrail: no accidental disabled-tool selection | Manual responsibility | Disabled tools are never selected | Unit tests and structured resolver diagnostics |

## 5. Proposed Solution / Elevator Pitch

Build a standalone TypeScript MCP server that exposes capability-tiered agent spawning for Solo. Agents and playbooks call `list_agent_tiers`, `resolve_agent_tool`, and `spawn_agent` instead of calling Solo `spawn_process` directly, and the companion resolves `small`, `medium`, or `large` into the best enabled local Solo agent tool deterministically.

The companion is a policy layer, not a Solo fork. It reads Solo's current runtime list, applies transparent local classification rules, and delegates the actual process creation back to Solo.

## 6. User Journeys & Use Cases

### Use Case: Inspect Available Agent Tiers

**Persona**: Playbook Author  
**Trigger**: A playbook wants to decide whether a requested delegation tier is available.  
**Steps**:
1. The playbook calls `list_agent_tiers`.
2. The companion queries Solo `list_agent_tools`.
3. The companion filters to enabled agent tools and classifies candidates into tiers.
4. The playbook receives tier availability and selected default candidates.

**Outcome**: The playbook can adapt to the local machine without knowing Solo IDs.

**Edge cases / error states**:
- Solo connection is unavailable.
- No enabled tools match a requested tier.
- Multiple candidates match; response explains deterministic choice.

### Use Case: Resolve a Tier Without Spawning

**Persona**: Agent Operator  
**Trigger**: An agent wants to preview which runtime would be used for a job.  
**Steps**:
1. The agent calls `resolve_agent_tool` with `tier: "medium"`.
2. The companion returns the selected Solo tool id, command summary, source classification signals, and alternatives.

**Outcome**: The operator can audit policy decisions before creating a process.

**Edge cases / error states**:
- Tier is unknown.
- Classification is ambiguous and no deterministic policy applies.
- Candidate exists but is disabled.

### Use Case: Spawn an Agent by Tier

**Persona**: Playbook Author  
**Trigger**: A workflow needs a helper agent with a requested capability tier.  
**Steps**:
1. The playbook calls `spawn_agent` with `tier`, optional human-friendly `name`, and optional project scope.
2. The companion resolves the tier to an enabled Solo tool.
3. The companion calls Solo `spawn_process(kind="agent", agent_tool_id=N, name=...)`.
4. The companion returns the Solo process id, final process name, selected tier, and selected tool summary.

**Outcome**: The workflow delegates work without hard-coding local runtime IDs.

**Edge cases / error states**:
- The requested tier has no candidate.
- Solo rejects the spawn request.
- The provided name conflicts with Solo naming rules.

### Use Case: Configure Local Tier Policy

**Persona**: Runtime Maintainer  
**Trigger**: A local machine has custom model names or preferred runtime ordering.  
**Steps**:
1. The maintainer edits explicit companion config.
2. The companion validates the config on startup.
3. Resolver output shows which rules matched and why.

**Outcome**: Local policy can evolve without updating playbooks.

**Edge cases / error states**:
- Config references unsupported tier labels.
- Config creates two equally preferred candidates.
- Config conflicts with live Solo enabled state.

## 7. Functional Requirements

**[P0] REQ-001 — Playbook authors can list supported tier labels and current availability**  
Context: Workflows need a stable discovery surface before spawning.

**[P0] REQ-002 — Playbook authors can resolve `small`, `medium`, and `large` to a Solo agent tool without spawning**  
Context: Resolution must be inspectable and testable separately from process creation.

**[P0] REQ-003 — Playbook authors can spawn a Solo-managed agent by tier**  
Context: This is the core replacement for direct `spawn_process` calls from workflows.

**[P0] REQ-004 — The resolver filters out disabled tools before classification and selection**  
Context: Disabled Solo tools must never be selected, even if their names or commands match a tier.

**[P0] REQ-005 — The resolver classifies by `command` first and `name` second**  
Context: Names should remain human-friendly labels, not the primary policy surface.

**[P0] REQ-006 — Candidate selection is deterministic when multiple enabled tools match a tier**  
Context: Repeated calls on the same Solo tool list should produce the same selected candidate.

**[P0] REQ-007 — Spawned Solo process names can be provided by the caller**  
Context: Operators need readable process names in Solo while tier selection remains policy-driven.

**[P0] REQ-008 — The companion supports explicit Solo connection configuration**  
Context: Solo does not currently expose an MCP connection descriptor through its tool surface.

**[P0] REQ-009 — The companion reports resolver diagnostics in tool results**  
Context: Operators need to understand why a tier resolved to a specific tool without reading logs.

**[P1] REQ-010 — The companion supports local YAML policy overrides for model token classification and ranking**  
Context: Different installations may use custom agent tools before Solo exposes richer runtime metadata.

**[P1] REQ-011 — The companion uses `SOLO_PROJECT_ID` and `SOLO_PROCESS_ID` as best-effort defaults when present**  
Context: Solo-managed agents can provide useful context, but explicit config remains the primary path.

**[P1] REQ-012 — The companion exposes structured logs for resolution and spawn decisions**  
Context: Local debugging needs machine-readable traces without requiring a full observability stack.

**[P2] REQ-013 — The companion can run a live health check against Solo**  
Context: Operators may want a quick command or tool call that proves configuration is valid.

## 7b. Global Invariants

- The companion never requires users to maintain static `agent_tool_id` mappings. **Why:** this is the defect class the product exists to remove. **How to verify:** docs and examples use tier labels or policy rules, not fixed IDs; tests build IDs dynamically from mocked `list_agent_tools` payloads.
- Disabled Solo tools are excluded before tier classification. **Why:** a disabled tool matching a known model token must not be selected accidentally. **How to verify:** fixture tests include disabled matching tools and assert they are absent from resolution candidates.
- Classification uses `command` as the primary signal and `name` only as fallback. **Why:** readable local names should not become the policy contract. **How to verify:** tests include misleading names with accurate commands and accurate names with misleading commands; command-first behavior wins.
- Resolver output is deterministic for identical inputs. **Why:** orchestration playbooks must be repeatable and debuggable. **How to verify:** property or fixture tests assert stable selected IDs and candidate ordering across repeated runs.
- Solo remains the authority for process creation. **Why:** the companion is a policy/orchestration layer, not a Solo replacement. **How to verify:** `spawn_agent` delegates to Solo `spawn_process(kind="agent", agent_tool_id=...)` and does not create agent processes directly.
- Unknown or unclassified tiers fail loudly unless an explicit configured fallback exists. **Why:** silent fallback from "large" to a weaker runtime would make delegation quality unpredictable. **How to verify:** tests assert `resolve_agent_tool` returns a structured error for unsupported tiers and no-match cases.

## 8. Initial MCP Tool Surface

### `list_agent_tiers`

Returns supported tier labels, availability, selected default candidate per tier, alternatives, and diagnostics explaining why each candidate matched.

### `resolve_agent_tool`

Input: requested tier and optional policy hints.  
Output: selected Solo `agent_tool_id`, selected tool metadata, matched tier, classification signals, alternatives, and structured errors when no candidate exists.

### `spawn_agent`

Input: requested tier, optional human-friendly process name, optional project scope, and optional prompt/bootstrap fields if later supported by Solo.  
Output: Solo process id, final process name, selected tier, selected tool summary, and Solo-returned agent instructions when available.

## 9. Non-Goals / Out of Scope

- Modifying Solo itself.
- Requiring users to maintain static `agent_tool_id` mappings.
- Building a general workflow engine.
- Managing process lifecycle beyond the spawn call.
- Implementing cost accounting, quota management, or budget enforcement in MVP.
- Inferring exact model pricing, context length, or capability from external catalogs in MVP.
- Replacing Solo's process list, status, start, restart, or terminal tools.
- Making this a Hypomnema feature or coupling it to Hypomnema's daemon architecture.

## 10. UX Considerations

- Tool names should be short and directly usable from agent prompts.
- Error messages should tell the caller what to do next: configure Solo connection, enable a candidate tool, adjust policy, or request a different tier.
- Resolver diagnostics should be concise enough to appear in agent context without flooding it.
- Human-friendly names supplied to `spawn_agent` should pass through to Solo where valid; name policy should not be overloaded with tier policy.

## 11. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Model tokens in commands change faster than classifier rules | Tiers become stale or unresolved | Use local YAML override rules and fixture tests for known command patterns |
| Name fallback makes misleading selections | Wrong runtime chosen | Make command-first invariant explicit; include diagnostics showing fallback use |
| Companion cannot discover Solo MCP endpoint automatically | Setup friction | Explicit config first; environment detection only as convenience |
| Deterministic selection picks a surprising candidate | User mistrust | Return alternatives and ranking reasons; allow local preference overrides |
| Tier labels imply precision the metadata cannot support | Overpromising | Treat tiers as local policy categories, not universal benchmark claims |

## 12. Open Questions

1. Should tier labels be fixed to `small`, `medium`, `large` for v0, or should custom labels be accepted from config while the initial docs standardize those three?
2. What exact ranking order should apply when both Codex and OpenCode candidates match the same tier?
3. Should `spawn_agent` accept only `tier` and `name` initially, or also allow a first prompt/instructions payload if Solo's current spawn behavior can support it cleanly?
4. What transport should the companion use to reach Solo in the first implementation: explicit command spawning, explicit stdio descriptor, HTTP endpoint, or a small set of supported connection modes?
5. Should no-match resolution ever fall back to a lower tier, or should all fallback behavior require explicit local policy?
6. Where should default classifier rules live for maintainability: embedded constants, YAML shipped with the package, or both?

## 13. MVP Acceptance

MVP is acceptable when:

- A local agent can call `list_agent_tiers` and see availability for `small`, `medium`, and `large`.
- A local agent can call `resolve_agent_tool` and receive the same selected candidate across repeated calls for the same Solo tool list.
- A local agent can call `spawn_agent` with a tier and optional name, and Solo creates an agent process using the selected enabled tool.
- Fixture tests cover the currently observed Solo tools plus disabled, ambiguous, unknown, and misleading-name cases.
- Documentation states that the companion is standalone and does not require Hypomnema.
