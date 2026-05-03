# Duo

**Tier-based agent selection for Solo MCP orchestration**

## Overview

**Duo** is a standalone MCP server that surfaces a tier-based capability layer over Solo's process primitives. Any MCP client that wants to spawn Solo-managed agent processes by capability tier (`small` / `medium` / `large`) — instead of by hard-coded `agent_tool_id` — can install and run Duo directly.

Solo's `spawn_process` tool remains directly available. Use Duo when you want tier-based selection, alternative listing, override-aware diagnostics, and structured resolution logs. Reach for direct `spawn_process` only for explicit one-off tooling overrides that don't fit the tier model. Playbooks and interactive agents should prefer the companion.

## Requirements

- **Node.js**: ≥ 22.0.0
- **Solo MCP server**: reachable and configured for stdio command-spawn execution (Duo communicates with Solo as an MCP client, so Solo must be running or configured to spawn on demand)

## Installation

### Via `npx` (recommended for ad-hoc use)

```bash
npx @procrastivity/duo
```

The `@procrastivity/duo` package is fetched and executed directly; no local installation required.

### Global installation

```bash
npm install -g @procrastivity/duo
duo
```

After installation, run `duo` from anywhere.

### Local installation for embedding

```bash
npm install @procrastivity/duo
```

Then reference it in your MCP client config or import its types in a TypeScript project.

## MCP Client Setup

Register Duo as an MCP server in your MCP client configuration. Below is an example for **Claude Desktop**:

```json
{
  "mcpServers": {
    "duo": {
      "command": "npx",
      "args": ["-y", "@procrastivity/duo"],
      "env": {
        "DUO_CONFIG": "./duo.config.yaml"
      }
    }
  }
}
```

**For Solo's own MCP client config**, add a similar entry to your Solo configuration:

```yaml
mcpServers:
  duo:
    command: npx
    args:
      - -y
      - @procrastivity/duo
    env:
      DUO_CONFIG: ./duo.config.yaml
```

The `DUO_CONFIG` environment variable points to a local YAML configuration file (see Configuration section below).

## Configuration

### `duo.config.yaml`

Duo requires a configuration file (default: `duo.config.yaml` in the current working directory; override with `DUO_CONFIG` environment variable). Here is a minimal example:

```yaml
solo:
  transportType: "stdio"
```

**Configuration fields**:

- `solo.transportType` — (required) set to `"stdio"` for standard stdio command-spawn transport

Project and process scope are **not** YAML fields. They resolve once at server start:
- `SOLO_PROJECT_ID` (env) is the hard override. If set, Duo uses it directly.
- Otherwise Duo calls Solo's `list_projects` and picks the project whose `path` is the longest prefix of the current working directory.
- If `SOLO_PROCESS_ID` is set, Duo calls Solo's `bind_session_process` once at connect; subsequent process-scoped calls are routed to that process automatically by Solo.

### Environment variables

- `DUO_CONFIG` — path to the configuration file (default: `duo.config.yaml`)
- `DUO_POLICY` — path to the policy file (default: `duo.policy.yaml`; silently ignored if not present unless explicitly set)
- `SOLO_PROJECT_ID` — Solo project ID (integer). Hard override; bypasses pwd→project lookup.
- `SOLO_PROCESS_ID` — Solo process ID (integer). When set, Duo binds the MCP session to this process at connect.

## Tools

Duo exposes three MCP tools for tier-based agent management.

### `list_agent_tiers`

Lists the available agent tool tiers (`small`, `medium`, `large`) and their current availability.

**Input** (no arguments):

```json
{}
```

**Example response** (abbreviated):

```json
{
  "small": {
    "available": true,
    "default": {
      "agent_tool_id": 1,
      "tool_name": "opencode-ghc-haiku",
      "tool_type": "opencode",
      "command": "opencode --model haiku",
      "classification_source": "command"
    },
    "alternatives": [
      {
        "agent_tool_id": 3,
        "tool_name": "codex-fast",
        "tool_type": "codex",
        "classification_source": "command"
      }
    ],
    "diagnostics": {
      "requested_tier": "small",
      "total_tools": 5,
      "candidates_considered": 2,
      "strategy": "random",
      "ignored_tools": [],
      "preference_applied": false
    }
  },
  "medium": {
    "available": true,
    "default": {
      "agent_tool_id": 2,
      "tool_name": "opencode-ghc-sonnet",
      "tool_type": "opencode",
      "command": "opencode --model sonnet",
      "classification_source": "command"
    },
    "alternatives": [],
    "diagnostics": {
      "requested_tier": "medium",
      "total_tools": 5,
      "candidates_considered": 1,
      "strategy": "random",
      "ignored_tools": [],
      "preference_applied": false
    }
  },
  "large": {
    "available": true,
    "default": {
      "agent_tool_id": 5,
      "tool_name": "codex-flagship",
      "tool_type": "codex",
      "command": "codex --profile flagship",
      "classification_source": "command"
    },
    "alternatives": [],
    "diagnostics": {
      "requested_tier": "large",
      "total_tools": 5,
      "candidates_considered": 1,
      "strategy": "random",
      "ignored_tools": [],
      "preference_applied": false
    }
  }
}
```

### `resolve_agent_tool`

Resolves a tier label to a specific agent tool, returning the selected tool details, alternatives, and resolution diagnostics.

**Input**:

```json
{
  "tier": "medium"
}
```

**Example response**:

```json
{
  "selected": {
    "agent_tool_id": 2,
    "tool_name": "opencode-ghc-sonnet",
    "tool_type": "opencode",
    "command": "opencode --model sonnet",
    "token_source": "command_token",
    "matched_tokens": [
      {
        "token": "sonnet",
        "source": "command"
      }
    ]
  },
  "classification_source": "command",
  "alternatives": [
    {
      "agent_tool_id": 4,
      "tool_name": "codex-standard",
      "tool_type": "codex",
      "classification_source": "command",
      "token_source": "command_token"
    }
  ],
  "diagnostics": {
    "requested_tier": "medium",
    "total_tools": 5,
    "candidates_considered": 2,
    "strategy": "random",
    "ignored_tools": [],
    "preference_applied": false,
    "override_token_count": 0
  }
}
```

**Response fields**:

- `selected.agent_tool_id` — the selected tool's numeric ID
- `selected.tool_name` — the tool's human-readable name
- `selected.token_source` — how the tier was matched (`"command_token"` or `"name_token"`)
- `selected.matched_tokens` — array of `{ token, source }` objects showing which tokens matched the tier
- `classification_source` — whether the match came from command parsing (`"command"`) or name fallback (`"name_fallback"`)
- `alternatives` — other tools matching the same tier (not selected)
- `diagnostics` — resolution strategy, candidate count, and override application info

### `spawn_agent`

Resolves a tier and spawns a Solo agent process using the selected tool.

**Input**:

```json
{
  "tier": "large",
  "name": "step-05-coordinator",
  "project_id": "42"
}
```

(Optional fields: `name` and `project_id` can be omitted.)

**Example response** (success):

```json
{
  "process_id": "12345",
  "name": "step-05-coordinator",
  "tier": "large",
  "tool": {
    "agent_tool_id": 5,
    "tool_name": "codex-flagship",
    "tool_type": "codex",
    "command": "codex --profile flagship",
    "classification_source": "command"
  },
  "project_id": "42"
}
```

**Response fields**:

- `process_id` — the Solo process ID (string)
- `name` — the assigned process name (either from the request or auto-generated by Solo)
- `tier` — the tier that was resolved (`"small"`, `"medium"`, or `"large"`)
- `tool` — summary of the selected agent tool
- `project_id` — the project scope (included if provided or configured)

## Tier Policy Overrides

By default, Duo uses built-in command-token patterns to classify tools into tiers (e.g., "haiku" → small, "sonnet" → medium, "opus" / "flagship" → large). You can customize this classification with a `duo.policy.yaml` file.

### Example policy: add "pro" as a large-tier token

Create `duo.policy.yaml`:

```yaml
command_tokens:
  large:
    tokens:
      - pro
    mode: "extend"
```

The `"extend"` mode adds `"pro"` to the existing large-tier patterns; `"replace"` would use only the specified tokens.

### Example policy: custom selection preference

```yaml
selection:
  preference:
    - tool_type: opencode
    - tool_type: codex
```

This directs Duo to prefer OpenCode tools over Codex tools when multiple tiers are available.

For a complete policy schema and more advanced overrides, see `src/types/policy.ts` or `docs/policy.md` (if present).

## Logging

Duo emits structured JSON logs to stderr for operational visibility. Logs go to stderr; stdout is reserved for MCP protocol traffic. Prompts and free-form task content are never logged by design.

**Example `resolution.success` log** (single line):

```json
{"level":"info","event":"resolution.success","requested_tier":"medium","selected_tool_id":2,"selected_tool_name":"opencode-ghc-sonnet","match_source":"command","token_source":"command_token","candidate_count":2,"strategy":"random","preference_applied":false}
```

**Example `resolution.failure` log**:

```json
{"level":"error","event":"resolution.failure","requested_tier":"purple","error_code":"unsupported_tier","available_tiers":["small","medium","large"]}
```

**Example `spawn.success` log**:

```json
{"level":"info","event":"spawn.success","requested_tier":"large","selected_tool_id":5,"solo_process_id":"12345","process_name":"step-05-coordinator"}
```

Each log is a single JSON object printed to stderr, one per line. Applications parsing logs can deserialize each line independently.

## Direct `spawn_process`

Solo's `spawn_process` tool remains available for direct use. Use Duo when you want tier-based selection, alternative listing, override-aware diagnostics, or structured resolution logs. Reach for direct `spawn_process` only for one-off explicit `agent_tool_id` overrides where tiers don't apply.

Example of when to use direct `spawn_process`:

- You know the exact `agent_tool_id` and don't need tier-based abstraction.
- You want to spawn a non-agent process (Solo supports `kind: "terminal"` and `kind: "command"` as well).
- You want to bypass Duo's policy overrides entirely and specify the tool directly.

## Releases & Versioning

Duo uses semantic versioning. Releases are published to npm via GitHub Actions.

**Release flow**:

1. Update `package.json` version and commit to `main`.
2. Create a git tag matching the version: `git tag v0.1.0`.
3. Push the tag: `git push origin v0.1.0`.
4. GitHub Actions `release.yml` workflow triggers, runs tests and build, and publishes to npm with provenance.

**Installing a specific version**:

```bash
npx @procrastivity/duo@0.1.0
```

Or in `package.json`:

```json
{
  "dependencies": {
    "@procrastivity/duo": "^0.1.0"
  }
}
```

## License

See [LICENSE](./LICENSE).
