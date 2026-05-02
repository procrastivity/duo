# Agent Tool Selection And Spawn

This document defines how tier-based agent spawning should work for Solo-managed agents.

## Goal

Callers should request capability (`small`, `medium`, `large`) rather than hard-coding `agent_tool_id` values.

Primary interface:

- `/spawn-agent <tier>`

Optional expanded form:

- `/spawn-agent <tier> --name <process-name> --purpose "<short reason>"`

The resolver should allow human-friendly Solo agent tool names. Tool names are useful display metadata, not the primary capability contract.

## Inputs

Required:

- `tier`: one of `small`, `medium`, `large`

Optional:

- `name`: process name to use at spawn time
- `purpose`: one-line reason for auditability
- `strategy`: `deterministic` (default) or `random`
- `exclude_ids`: list of `agent_tool_id` values to avoid

## Source Of Truth

Use Solo `list_agent_tools()` as the source of truth for available runtimes.

The current Solo response shape includes:

- `id`: Solo `agent_tool_id` used by `spawn_process(kind="agent", agent_tool_id=N)`
- `name`: human-readable tool name
- `command`: command Solo will execute
- `tool_type`: runtime family such as `codex`, `opencode`, or `generic`
- `enabled`: whether the tool is enabled

Do not require a project-maintained static `agent_tool_id` mapping. IDs are operational details and may change as tools are added, removed, or renamed.

## Resolution Order

Resolve candidate tools in this order:

1. Query Solo `list_agent_tools()`.
2. Drop tools where `enabled != true`.
3. Drop tools whose `id` is listed in `exclude_ids`.
4. Classify remaining tools by `command` tokens.
5. Use `name` tokens only as a fallback or tie signal.
6. Fail with a clear error if no confident candidates exist.

### Command-First Classification

Prefer model/runtime tokens found in `command` over display names.

Initial token policy:

- `small`: `haiku`, `mini`, `flash`, `fast`, `cheap`, `small`
- `medium`: `sonnet`, `standard`, `medium`, `default`, `gpt-5.2`, `gpt-5.3-codex`, `gpt-5.4`
- `large`: `opus`, `flagship`, `max`, `large`, `gpt-5.5`

Matching is case-insensitive.

If a token appears in both `command` and `name`, the `command` match wins. If `command` is ambiguous or unclassifiable, `name` may be used as a fallback, but the `selection_reason` must say that name fallback was used.

### Name Fallback

Name-based fallback exists for compatibility, not as the main contract.

Examples of useful fallback tokens:

- `small`: `haiku`, `mini`, `flash`, `fast`, `cheap`, `small`
- `medium`: `sonnet`, `standard`, `medium`, `default`
- `large`: `opus`, `flagship`, `pro`, `max`, `large`

Treat `pro` as a weak large-tier signal. Prefer stronger command/model tokens when present.

## Selection Strategy

Default: `deterministic`

- Sort matching candidates by `agent_tool_id` ascending.
- Select the first candidate.

Optional: `random`

- Choose uniformly from matching candidates.

Deterministic selection is the default for reproducibility. Use `random` only when intentional spread is desired.

## Spawn Behavior

- Spawn an **agent process** with `kind="agent"` and the selected `agent_tool_id`.
- Return the created process metadata.
- Do not send a bootstrap message unless explicitly requested by the caller.

## Return Shape

Return:

- `process_id`
- `agent_tool_id`
- `tool_name`
- `tool_type`
- `command`
- `tier`
- `selection_reason`
- `alternatives_considered`

`alternatives_considered` should be present even when empty.

## Failure Behavior

Hard-fail with an actionable message when:

- No enabled tools map confidently to the requested tier.
- Multiple tools produce conflicting classification signals that cannot be resolved deterministically.
- Spawn fails after selecting a candidate.

Failure message must include:

- requested tier
- discovered tools
- enabled tools after filtering
- excluded IDs, if any
- classification source used (`command`, `name`, or none)
- token policy checked

Do not silently fall back to an arbitrary enabled tool.

## Role Policy (Balanced Default)

- `Orchestrator`: `small`
- `Coordinator`: `small` default, `medium` when handling active escalations/retries
- `Researcher`: `large` default, `medium` allowed for low-risk narrow-decision steps
- `Builder`: `medium` default, `large` for medium-high/high-risk and load-bearing/novel tasks

Additional guardrail:

- If a `Builder` hits repeated same-shape failures on `medium`, next attempt should use `large` or escalate.

## Load-Bearing Builder Escalation

Use `large` for builder tasks that touch load-bearing or high-risk areas, including:

- SQLite and async runtime boundaries
- file watching and debouncing
- MCP protocol behavior
- cross-cutting refactors
- schema/indexing changes
- novel or hard-to-reverse implementation paths

Using a stronger model for these tasks is cheaper than debugging subtle orchestration or runtime failures later.
