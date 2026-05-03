# 02 · Tier tools

> Applies to: Duo current `main`.
> Prereqs: [`01-running-duo.md`](./01-running-duo.md) complete; Duo
> registered with Claude Code (or driveable via raw JSON-RPC); Solo
> has agent tools spanning at least two tiers.

This doc exercises Duo's three tools end-to-end. The acceptance bar
is that each tool returns the documented response shape and that
`spawn_agent` produces a Solo process visible to other Solo
clients.

The examples below assume Claude Code as the MCP client; the
JSON-RPC equivalents from [`01-running-duo.md`](./01-running-duo.md)
work too — just send a `tools/call` request with the tool name and
arguments.

## 1. `list_agent_tiers`

Ask Claude:

> Call `mcp__duo__list_agent_tiers` with no arguments and show me
> the response.

Expected response shape (abbreviated, exact contents depend on what
Solo has registered):

```json
{
  "small":  { "available": true,  "default": { … }, "alternatives": [ … ], "diagnostics": { … } },
  "medium": { "available": true,  "default": { … }, "alternatives": [ … ], "diagnostics": { … } },
  "large":  { "available": false, "default": null,   "alternatives": [],    "diagnostics": { … } }
}
```

Acceptance:

- Three tiers always present (`small`, `medium`, `large`).
- At least one tier reports `available: true` with a populated
  `default.tool_name` and `default.command`.
- For each available tier, `diagnostics.candidates_considered` is
  ≥ 1 and `diagnostics.strategy` is `random` (default with no
  policy file in place).
- Unavailable tiers report `available: false`, `default: null`, and
  `diagnostics.candidates_considered: 0`.

Capture which tiers are available — you'll use them in steps 2 and
3.

## 2. `resolve_agent_tool`

For each available tier from step 1, ask Claude:

> Call `mcp__duo__resolve_agent_tool` with `tier: "<tier>"` and show
> me the response.

Expected response shape:

```json
{
  "selected": {
    "agent_tool_id": <number>,
    "tool_name": "<string>",
    "tool_type": "<string>",
    "command": "<string>",
    "token_source": "command_token" | "name_token",
    "matched_tokens": [ { "token": "<string>", "source": "command" | "name" } ]
  },
  "classification_source": "command" | "name_fallback",
  "alternatives": [ … ],
  "diagnostics": { … }
}
```

Acceptance per tier:

- `selected` is non-null and matches the `default` reported by
  `list_agent_tiers` for the same tier (modulo the `random`
  selection strategy — if there are alternatives, the chosen tool
  may differ across calls; see step 3 for a determinism check).
- `selected.matched_tokens` is non-empty and the token aligns with
  the tier's expected vocabulary (`haiku/mini/fast` → small,
  `sonnet/standard` → medium, `opus/flagship/pro` → large).
- `classification_source` is `"command"` for tools whose `command`
  field carries the token; `"name_fallback"` otherwise.
- `diagnostics.requested_tier` echoes the input.
- `diagnostics.preference_applied` is `false` (no policy file in
  place yet).

### Failure case — unsupported tier

> Call `mcp__duo__resolve_agent_tool` with `tier: "purple"`.

Expected: an MCP tool-call error whose payload (after the JSON-RPC
envelope) contains `error_code: "unsupported_tier"` and
`available_tiers: ["small","medium","large"]`. A
`resolution.failure` event is also written to stderr (see
[`04-logging.md`](./04-logging.md)).

## 3. `spawn_agent`

Pick a tier from step 1 that's `available: true`. Ask Claude:

> Call `mcp__duo__spawn_agent` with
> `{ "tier": "<tier>", "name": "duo-runbook-test" }` and show me
> the response.

Expected response shape:

```json
{
  "process_id": "<string>",
  "name": "duo-runbook-test",
  "tier": "<tier>",
  "tool": {
    "agent_tool_id": <number>,
    "tool_name": "<string>",
    "tool_type": "<string>",
    "command": "<string>",
    "classification_source": "command" | "name_fallback"
  },
  "project_id": "<string>"   // present only if config or request supplied one
}
```

Acceptance:

- `process_id` is a non-empty string (Solo's surrogate ID).
- `name` matches the request (Solo accepts the supplied name).
- `tool` matches what `resolve_agent_tool` returned for the same
  tier on a representative call.
- `project_id` reflects what was supplied or, if neither config nor
  request supplied one, is omitted from the response.

### Confirm the spawn landed in Solo

From any other MCP client wired to Solo (or another Claude Code
session), call:

```
mcp__solo__list_processes
```

Expect the new process to appear with the `process_id`, the
`name` `duo-runbook-test`, and a `command` matching the resolved
agent tool. `status` should be `Running`.

Stop it when you're done:

```
mcp__solo__stop_process { "process_id": "<id>" }
```

### `project_id` variants

Repeat the spawn three ways and confirm what comes back:

1. Config-provided default: set `solo.projectId` in
   `duo.config.yaml`, restart Duo, call `spawn_agent` without
   `project_id`. The response includes `project_id` matching the
   config value.
2. Per-call override: leave config as before, pass `project_id:
   "<other>"` in the call. Response uses the call value, not the
   config value.
3. Neither: clear `solo.projectId` from config, restart Duo, call
   without `project_id`. Response omits the field; Solo applies
   whatever default scope the binary itself uses.

### Failure case — unsupported tier

> Call `mcp__duo__spawn_agent` with `tier: "purple"`.

Expected: same `unsupported_tier` error as in step 2, plus a
`spawn.failure` event on stderr.

## 4. Determinism check (optional)

Re-run `resolve_agent_tool` for a tier that has multiple
candidates (`alternatives` non-empty in step 1). With the default
`random` strategy, the `selected` tool may rotate across calls;
`alternatives` should always be `length(candidates) - 1`. This is
the natural cue that `selection.strategy` is doing what it says —
step 3 of [`03-policy-overrides.md`](./03-policy-overrides.md)
swaps the strategy and pins the choice.

You're ready for [`03-policy-overrides.md`](./03-policy-overrides.md).
