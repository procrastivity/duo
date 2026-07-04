# Duo

**Preset-based agent launching for Solo MCP orchestration**

## Overview

**Duo** is a standalone MCP server that surfaces a *preset* layer over Solo's process primitives. Instead of spawning Solo-managed agent processes by hard-coded `agent_tool_id`, you declare named **presets** — `builder`, `reviewer`, `default`, whatever you like — each mapping to one or more agent-tool definitions, and Duo picks an eligible definition at launch time.

A preset definition carries an `agent_tool_id`, an optional opaque `extra_args` string (tokenized and threaded to the spawned process), and an optional filename-safe `provider` label. **Providers** are user-defined labels with a lock-free enabled/disabled state; a definition whose provider is disabled is skipped at launch, so you can turn a whole class of agents on or off without editing config. Launches also accept a per-launch `extra_args` append and a soft `avoid_provider` preference, which powers workflows like "spawn the reviewer on a different provider than the builder."

Solo's `spawn_process` tool remains directly available. Reach for Duo when you want named presets, provider toggles, per-launch `extra_args`, and structured resolution logs; reach for direct `spawn_process` only for explicit one-off tooling overrides that don't fit the preset model.

Duo also ships a CLI for driving its tools (and a curated set of Solo passthroughs) directly from a shell — see [Command-line interface](#command-line-interface). Upgrading from an older build that picked agents by capability level? Read the migration section at the end.

## Command-line interface

The `duo` binary is a CLI router. **Bare `duo` prints help.** Use `duo mcp` to start the MCP server (this is the form MCP clients should invoke).

```text
duo                            # print help
duo mcp                        # run the MCP server (stdio)
duo doctor                     # run setup health checks
duo whoami                     # show resolved project + bound process
duo project ls|status          # list / inspect Solo projects

duo agent list                 # list configured presets + availability
duo agent resolve <preset>     # show which tool a preset would select
duo agent launch <preset>      # launch an agent process for a preset

duo config show|path           # inspect effective config
duo config preset add|list|remove    # manage preset definitions
duo config provider enable|disable|list  # toggle provider enabled-state

duo proc ls|logs|grep|status|stop|restart|kill <id|name>
duo version                    # version + git sha
```

`duo doctor` is the fastest way to verify your setup: it checks the binary version, that the config file parses, that the Solo binary is on `$PATH`, that the MCP handshake succeeds, and that connect-time scope resolution + `bind_session_process` work as expected. It exits non-zero on any failed check.

### Preset & provider management

Presets and providers are configured through `duo config`:

```bash
# Add a definition to the "builder" preset, resolving the tool by name.
duo config preset add builder --agent-tool=Codex \
  --extra-arguments="--model=gpt5.5 --effort=xhigh" --provider=openai

duo config preset list                 # all presets + definitions (best-effort tool names)
duo config preset list builder         # filter to one preset
duo config preset remove aaaa1111      # remove a single definition by its stable id

duo config provider disable openai     # skip openai-backed definitions at launch
duo config provider enable openai      # re-enable
duo config provider list               # providers + enabled/disabled status
```

`duo config preset add` resolves `--agent-tool` against Solo's live `list_agent_tools` (a numeric id, or an exact tool name); an ambiguous or unmatched selector prints the candidates and exits non-zero without writing. Provider verbs and `duo config preset list`/`remove` are **offline** — they touch only local config and XDG state, no Solo connection required.

### Runtime verbs

```bash
duo agent list                                          # presets + which are spawnable now
duo agent resolve builder                               # dry-run which tool a preset picks
duo agent resolve builder --avoid-provider=openai       # dry-run, soft-avoiding a provider
duo agent launch builder --name worker-1                # launch a builder-preset agent
duo agent launch reviewer --avoid-provider=openai       # launch, soft-avoiding a provider
duo agent launch builder --prompt "Analyze the codebase"  # launch with a bootstrap prompt
duo agent launch builder --extra-arguments="--verbose"  # append per-launch args
```

`--prompt` (CLI only) delivers a message to the spawned agent's first turn. `--extra-arguments` on `launch` is tokenized the same way as preset `extra_args` and appended after the preset's resolved args. `--avoid-provider` on `resolve`/`launch` is a *soft* preference: Duo restricts the candidate set to definitions whose provider differs, and only relents (allowing the avoided provider) if no preset can otherwise be satisfied — it never hard-fails on `avoid_provider` alone.

### Cross-cutting flags

- `--json` — emit JSON instead of a human-readable table (read commands).
- `-q` / `--quiet` — suppress connect logs and chrome; emit only the primary identifier (e.g. process id).
- `--cwd <path>` — override the working directory used for Solo/project resolution. Config still comes from `DUO_CONFIG` or the XDG config path.
- `NO_COLOR=1` and `--no-color` (on `duo doctor`) disable ANSI color.

### Exit codes

- `0` — success
- `1` — user error (bad args, validation, config not found)
- `2` — Solo error (server returned an error response; `{code, message}` printed to stderr)
- `3` — connection error (handshake failed, transport died, no Solo binary)

### Examples

```bash
duo doctor                                                   # verify setup
duo agent launch builder --name worker-1                     # launch a builder agent
duo agent launch reviewer --avoid-provider=openai            # launch on a different provider
duo agent launch builder --extra-arguments="--verbose"       # append per-launch args
duo proc ls --json | jq '.[] | .name'                        # script-friendly output
duo proc logs 298 --follow                                   # tail a process
SOLO_PROJECT_ID=6 duo whoami                                 # one-shot project override
```

Errors always go to stderr; data always goes to stdout, so `duo proc ls --json | jq` is safe.

## Requirements

- **Node.js**: ≥ 24.0.0
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

### Via Nix flake

Two install targets, each optimized for a different need:

```bash
# Run without installing — builds from source, works from ANY ref
# (main, a branch, a specific commit/PR):
nix run github:procrastivity/duo -- version
nix run github:procrastivity/duo/my-branch -- version

# Install the lean, self-contained standalone binary (no Node in the
# closure). Released tags only — see the tradeoff note below:
nix profile install github:procrastivity/duo#duo-bin
duo version
```

| Target | Builds from | Node in closure | Works from arbitrary ref? |
| --- | --- | --- | --- |
| `#duo` / `.#default` | source (`buildNpmPackage`) | yes (`nodejs_24`) | ✅ yes |
| `#duo-bin` | prebuilt release binary | **no** | ❌ released tags only |

**Tradeoff:** `#duo-bin` fetches the standalone binary attached to a GitHub release, so it can only ever install a *released* version — pointing it at `main` or a commit yields whatever version the pinned manifest (`nix/prebuilt-binaries.json`) references, **not** that ref's source. Use the default `#duo` (from source) to install/test an unreleased branch or commit. A `nixpkgs` overlay (`overlays.default`) exposes both `duo` and `duo-bin` for downstream flakes.

> **Maintainers:** refreshing `nix/prebuilt-binaries.json` after a release is automatic — the `update-nix-manifest` job in `release-bin.yml` runs once the binaries are uploaded (stable tags only) and commits the updated manifest to the default branch. To backfill by hand:
> ```bash
> node scripts/update-nix-binaries.mjs vX.Y.Z
> ```

### macOS Gatekeeper (unsigned binary)

Downloaded binaries are not codesigned. On first run, macOS may block execution with a Gatekeeper dialog. Remove the quarantine attribute before running:

```bash
xattr -d com.apple.quarantine ./duo-darwin-arm64
# or
xattr -d com.apple.quarantine ./duo-darwin-x64
```

Alternatively, right-click the binary in Finder → **Open** → **Open** to approve it once via the GUI.

## MCP Client Setup

Register Duo as an MCP server in your MCP client configuration. Below is an example for **Claude Desktop**:

```json
{
  "mcpServers": {
    "duo": {
      "command": "npx",
      "args": ["-y", "@procrastivity/duo", "mcp"],
      "env": {
        "DUO_CONFIG": "/path/to/config.yaml"
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
      - mcp
    env:
      DUO_CONFIG: /path/to/config.yaml
```

The `DUO_CONFIG` environment variable overrides the config file location (see Configuration section below).

## Configuration

### Config file

Duo reads a YAML config file. The path resolves in this order:

1. `DUO_CONFIG` (env) — verbatim path, highest priority.
2. `$XDG_CONFIG_HOME/duo/config.yaml` — if `XDG_CONFIG_HOME` is set.
3. `~/.config/duo/config.yaml` — the default fallback.

If no config file exists at the resolved path, Duo starts with built-in defaults (a stdio transport and no presets). Here is a minimal file:

```yaml
solo:
  transport:
    type: stdio
```

And a fuller example with presets:

```yaml
solo:
  transport:
    type: stdio

presets:
  builder:
    - { id: bld1a2b3, agent_tool_id: 17, extra_args: "--provider=openrouter --model=zai/glm-5.2", provider: openrouter }
    - { id: bld4c5d6, agent_tool_id: 5,  extra_args: "-m sonnet", provider: anthropic }
  reviewer:
    - { id: rev7e8f9, agent_tool_id: 4,  extra_args: "--model=gpt5.5 --effort=xhigh --agent=custom-reviewer", provider: openai }
  default:
    - { id: def0a1b2, agent_tool_id: 4, provider: openai }
```

**Config fields**:

- `solo.transport.type` — (required) set to `"stdio"` for standard stdio command-spawn transport.
- `presets` — (optional) a map of preset name → array of definitions. Each definition has:
  - `id` — a stable short id used to target the definition for removal. `duo config preset add` generates one for you.
  - `agent_tool_id` — the Solo agent-tool id to spawn.
  - `extra_args` — (optional) an opaque string; Duo splits it shell-style and threads the tokens to the spawned process. Duo does **not** validate agent-specific flags.
  - `provider` — (optional) a provider label matching `^[A-Za-z0-9._-]+$` other than `.`, or `..`. When the provider is disabled, the definition is skipped at launch.

Prefer editing presets through `duo config preset add|remove` (which generates ids and validates tool selectors) over hand-editing the file.

Project and process scope are **not** YAML fields. They resolve once at server start:

- `SOLO_PROJECT_ID` (env) is the hard override. If set, Duo uses it directly.
- Otherwise Duo calls Solo's `list_projects` and picks the project whose `path` is the longest prefix of the current working directory.
- If `SOLO_PROCESS_ID` is set, Duo calls Solo's `bind_session_process` once at connect; subsequent process-scoped calls are routed to that process automatically by Solo.

### Provider state

Provider enabled-state lives **outside** the config file as lock-free XDG state — one file per provider at `$XDG_STATE_HOME/duo/providers/<provider>` (default `~/.local/state/duo/providers/<provider>`). File content `0` means disabled; the file being absent, or holding anything else, means **enabled** (opt-out default). The state is read fresh on every launch, so a toggle takes effect immediately with no restart. Manage it with `duo config provider enable|disable|list` or the `set_provider_enabled` / `list_providers` MCP tools.

### Environment variables

- `DUO_CONFIG` — path to the config file (overrides the XDG default).
- `XDG_CONFIG_HOME` — base for the default config path (`$XDG_CONFIG_HOME/duo/config.yaml`).
- `XDG_STATE_HOME` — base for provider state files (`$XDG_STATE_HOME/duo/providers/`).
- `SOLO_PROJECT_ID` — Solo project ID (integer). Hard override; bypasses pwd→project lookup.
- `SOLO_PROCESS_ID` — Solo process ID (integer). When set, Duo binds the MCP session to this process at connect.

## Tools

Duo exposes **five** MCP tools: three for preset resolution/launch and two for provider state.

### `list_presets`

Lists the configured presets with per-preset availability and definitions. A definition is `enabled: false` when its provider is currently disabled; a preset is `available` when it (or its `default` fallback) has at least one enabled definition.

**Input** (no arguments):

```json
{}
```

**Example response**:

```json
{
  "builder": {
    "available": true,
    "definitions": [
      { "id": "bld1a2b3", "agent_tool_id": 17, "provider": "openrouter", "enabled": true },
      { "id": "bld4c5d6", "agent_tool_id": 5, "provider": "anthropic", "enabled": true }
    ]
  },
  "reviewer": {
    "available": false,
    "definitions": [
      { "id": "rev7e8f9", "agent_tool_id": 4, "provider": "openai", "enabled": false }
    ]
  },
  "default": {
    "available": true,
    "definitions": [
      { "id": "def0a1b2", "agent_tool_id": 4, "provider": "openai", "enabled": false }
    ]
  }
}
```

### `resolve_preset`

Dry-run: resolve which agent tool a preset would select, without spawning anything. Provider-aware — it filters to definitions whose provider is enabled, picks one at random, and falls back to the `default` preset when the requested one has no eligible definition.

**Input**:

```json
{
  "preset": "builder",
  "avoid_provider": "openai"
}
```

`avoid_provider` is optional; when set it is a *soft* preference (see below).

**Example response**:

```json
{
  "agent_tool_id": 5,
  "extra_args": ["-m", "sonnet"],
  "provider": "anthropic",
  "preset_requested": "builder",
  "preset_used": "builder",
  "fell_back_to_default": false,
  "relented_on_avoid_provider": false
}
```

**Response fields**:

- `agent_tool_id` — the selected Solo agent-tool id.
- `extra_args` — the selected definition's `extra_args`, tokenized into an array (empty when the definition has none).
- `provider` — the selected definition's provider label (omitted when it has none).
- `preset_requested` — the preset you asked for.
- `preset_used` — the preset that actually supplied the definition (equals `preset_requested`, or `"default"` when Duo fell back).
- `fell_back_to_default` — `true` when the requested preset had no eligible definition and Duo used the `default` preset.
- `relented_on_avoid_provider` — `true` when `avoid_provider` was set but no preset could satisfy it, so Duo allowed the avoided provider rather than fail.

**Errors** (structured tool error, `isError: true`):

- `unknown_preset` — the named preset is not configured.
- `preset_unavailable` — no eligible definition in the preset or `default`; the payload's `diagnostics` names which providers were disabled.

### `launch_agent`

Resolve a preset and spawn a Solo agent process using the selected tool.

**Input**:

```json
{
  "preset": "builder",
  "name": "step-05-coordinator",
  "project_id": 42,
  "avoid_provider": "openai",
  "extra_args": ["--verbose"]
}
```

Only `preset` is required. `name`, `project_id`, `avoid_provider`, and `extra_args` are optional.

**Caller `extra_args` append**: the array you pass in `extra_args` is appended **after** the selected definition's resolved args (order: `[...preset args, ...caller args]`). The merged array is both what reaches the spawned Solo process and what `result.extra_args` reports.

**Example response** (success):

```json
{
  "process_id": 12345,
  "name": "step-05-coordinator",
  "preset": "builder",
  "agent_tool_id": 5,
  "extra_args": ["-m", "sonnet", "--verbose"],
  "provider": "anthropic",
  "project_id": 42
}
```

**Response fields**:

- `process_id` — the Solo process ID (number).
- `name` — the assigned process name (from the request or auto-generated by Solo).
- `preset` — the preset that was launched.
- `agent_tool_id` — the selected Solo agent-tool id.
- `extra_args` — the merged effective args array (preset args first, caller append second) that was sent to Solo.
- `provider` — **always present.** The selected definition's provider label, or `null` when the selected definition has no provider. This lets a caller chain "launch the next agent avoiding whatever provider this one used."
- `project_id` — the project scope (included when provided or configured).

**Errors** (structured tool error): `unknown_preset`, `preset_unavailable` (as for `resolve_preset`), and `spawn_rejected` when Solo declines the spawn.

### `list_providers`

Lists the providers tracked in provider state with their enabled/disabled status. Offline — reads only the XDG provider-state directory, no Solo connection.

**Input** (no arguments):

```json
{}
```

**Example response**:

```json
{
  "providers": [
    { "provider": "anthropic", "enabled": true },
    { "provider": "openai", "enabled": false }
  ]
}
```

Scope is the state directory only — providers are listed once they've been toggled at least once. Providers that appear only in preset definitions but have never been toggled are not enumerated here.

### `set_provider_enabled`

Enable or disable a provider by writing its XDG state file. Offline — no Solo connection.

**Input**:

```json
{
  "provider": "openai",
  "enabled": false
}
```

**Example response**:

```json
{
  "provider": "openai",
  "enabled": false
}
```

An invalid provider label (empty, `.`, `..`, or containing a path separator; labels must match `^[A-Za-z0-9._-]+$`) returns a structured `invalid_provider_label` error and writes nothing.

## Logging

Duo emits structured JSON logs to stderr for operational visibility. Logs go to stderr; stdout is reserved for MCP protocol traffic. Prompts and free-form task content are never logged by design — each log carries only an allow-listed set of fields.

**Example `resolution.success` log** (single line):

```json
{"level":"info","event":"resolution.success","requested_preset":"builder","preset_used":"builder","selected_tool_id":5,"fell_back_to_default":false,"relented_on_avoid_provider":true}
```

**Example `resolution.failure` log**:

```json
{"level":"info","event":"resolution.failure","requested_preset":"ghost","error_code":"unknown_preset"}
```

**Example `spawn.success` log**:

```json
{"level":"info","event":"spawn.success","requested_preset":"builder","selected_tool_id":5,"solo_process_id":"12345","process_name":"step-05-coordinator"}
```

Each log is a single JSON object printed to stderr, one per line. Applications parsing logs can deserialize each line independently.

## Direct `spawn_process`

Solo's `spawn_process` tool remains available for direct use. Reach for Duo when you want preset-based launching, provider toggles, per-launch `extra_args`, or structured resolution logs. Reach for direct `spawn_process` only for one-off explicit `agent_tool_id` overrides where presets don't apply.

Example of when to use direct `spawn_process`:

- You know the exact `agent_tool_id` and don't need the preset abstraction.
- You want to spawn a non-agent process (Solo supports `kind: "terminal"` and `kind: "command"` as well).
- You want to bypass Duo's preset layer entirely and specify the tool directly.

## Migrating from tiers (pre-1.0 breaking change)

Earlier builds of Duo selected agents by **tier** — `small` / `medium` / `large` — inferring each tool's tier from tokens in its command or name (a *classifier*), with an optional `duo.policy.yaml` to customize that classification. That whole model has been **removed** and replaced by explicit **presets** plus **provider** toggles. This is a pre-1.0 clean break with no compatibility aliases; update your config and callers.

### MCP tool renames

| Old tool | New tool |
|---|---|
| `list_agent_tiers` | `list_presets` |
| `resolve_agent_tool` | `resolve_preset` |
| `spawn_agent` | `launch_agent` |

`list_providers` and `set_provider_enabled` are new.

### Input / argument changes

- The `tier` argument on `resolve_agent_tool` / `spawn_agent` is gone. `resolve_preset` and `launch_agent` take a **`preset`** string instead (a preset name you configured, not a fixed `small`/`medium`/`large` label).
- `launch_agent` gains optional `avoid_provider` (soft provider preference) and `extra_args` (a caller-supplied append on top of the preset's args); its result now **always** reports `provider` (label or `null`).
- On the CLI, `duo agent spawn <tier>` became `duo agent launch <preset>`, and the positional argument on `duo agent launch`/`resolve` is now a `preset` name rather than a tier. `--avoid-provider` is new on both.

### Removed: classification & policy

- The built-in command/name **token classifier** that mapped tools into tiers is gone. You now declare, per preset, exactly which `agent_tool_id`(s) it may use.
- `duo.policy.yaml` and the `DUO_POLICY` environment variable are removed, along with the `command_tokens` (custom tier tokens) and `selection` (tool-type preference) sections they carried. There is no policy file anymore.
- What replaces them: **explicit presets** give you deterministic control over which tools a name maps to, and **provider enabled-state** lets you turn definitions on and off at launch time without editing config. Where you once added a `command_tokens` entry to re-classify a tool, you now add a preset definition; where you once used `selection.preference` to steer between tool types, you now either shape each preset's definition list or disable a provider.

### Config-shape change

The old tier config had no explicit preset map — tiers were derived, and customization lived in `duo.policy.yaml`. The new config declares presets directly under a `presets:` key in `config.yaml` (see [Configuration](#configuration)), and provider enabled-state lives as lock-free files under `$XDG_STATE_HOME/duo/providers/`. Note the config file itself is now resolved from the XDG location (`~/.config/duo/config.yaml` or `DUO_CONFIG`), not a `duo.config.yaml` in the working directory.

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
</content>
</invoke>
