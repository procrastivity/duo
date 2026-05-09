# 01 · Running Duo

> Applies to: Duo current `main`.
> Prereqs: [`00-setup.md`](./00-setup.md) complete; `dist/duo.mjs`
> built; `./duo.config.yaml` points at a real Solo binary; Solo has
> agent tools registered.

This doc verifies that Duo comes up as an MCP stdio server, that an
MCP client can complete the `initialize` handshake, and that
`tools/list` returns Duo's three tools. It does not exercise the
tools themselves (that's [`02-tier-tools.md`](./02-tier-tools.md))
or policy overrides (that's
[`03-policy-overrides.md`](./03-policy-overrides.md)).

Duo speaks **stdio MCP only**. Stdout is the protocol channel —
keep it clean — and stderr is operational logs. Drive Duo from any
MCP client that supports stdio command-spawn servers.

## Option A — Drive from Claude Code (recommended)

This is the path most testers will use.

### Register Duo with Claude Code

From any directory:

```bash
claude mcp add duo \
  --scope user \
  --command node \
  --args "$(pwd)/dist/duo.mjs" --args mcp \
  --env DUO_CONFIG="$(pwd)/duo.config.yaml"
```

(Substitute `--scope project` and adjust paths if you prefer a
project-scoped registration. Confirm via `claude mcp list`.)

### Verify the handshake

Open a Claude Code session in the Duo repo root and ask:

> List the MCP tools provided by the `duo` server.

Expect three tools:

- `mcp__duo__list_agent_tiers`
- `mcp__duo__resolve_agent_tool`
- `mcp__duo__spawn_agent`

If only `duo` shows up but no tools (or zero tools), the most likely
causes are:

- Duo crashed during init — check Claude Code's MCP server log for
  the stderr stream (look for `Failed to read config from …` or
  `solo.transport.command is required`).
- Solo binary path in `duo.config.yaml` is wrong — Duo's child
  spawn fails as soon as it attempts the Solo handshake.

### Sanity-check stdout cleanliness

Watch Claude Code's MCP log for the `duo` server. Stdout should
contain only JSON-RPC messages (one per line). If you see any
free-form text on stdout, that's a regression — file it.

## Option B — Drive via raw JSON-RPC stdio

Useful for reproducing exactly what's on the wire, or when no
MCP-aware client is available. The MCP handshake is two messages:
`initialize` then `notifications/initialized`. After that, regular
requests work.

In one shell, prepare a tiny driver script:

```bash
cat > /tmp/duo-driver.sh <<'BASH'
#!/usr/bin/env bash
# Pipe a sequence of JSON-RPC lines into Duo and print stdout.
# `sleep` keeps stdin open long enough for Duo to write responses
# before we EOF; `timeout` is a safety net so a hung run can't
# block the shell.
{
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"runbook","version":"0"}}}'
  echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
  echo '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
  sleep 5
} | timeout 10 node ./dist/duo.mjs mcp
BASH
chmod +x /tmp/duo-driver.sh
```

Run it from the Duo repo root (so `./duo.config.yaml` is found):

```bash
/tmp/duo-driver.sh
```

Expect exit code `0` — Duo shuts down cleanly when stdin reaches
EOF after the `sleep` ends. (Exit `124` indicates `timeout` had to
kill a hung process; treat it as a regression.)

Or use the committed driver — same payload, parameterizable
through `lib.sh`:

```sh
./notes/manual-testing/scripts/01-tools-list.sh
```

Expected on stdout, line by line:

1. `initialize` response — `result.serverInfo.name` is `duo`,
   `result.protocolVersion` echoes back.
2. `tools/list` response — `result.tools` is a length-3 array with
   `name` values `list_agent_tiers`, `resolve_agent_tool`,
   `spawn_agent`.

Stderr will carry one or more structured log lines from Duo's
logger (see [`04-logging.md`](./04-logging.md)). Discard for now —
the goal of this step is just protocol liveness.

If `tools/list` returns an error, capture stderr alongside stdout:

```bash
/tmp/duo-driver.sh 2>/tmp/duo.err >/tmp/duo.out
jq < /tmp/duo.out      # parse each line as JSON
cat /tmp/duo.err       # operational logs
```

## Common gotchas

- **Duo exits silently with no stdout** — config file missing or
  malformed. Run with `DUO_CONFIG=$(pwd)/duo.config.yaml node
  ./dist/duo.mjs mcp < /dev/null` and watch stderr. The first line is
  usually the cause.
- **Duo logs `solo.transport.command is required`** — your config
  uses the README's flat `solo.transportType` shape. Switch to the
  nested `solo.transport.{type,command,args}` form per
  [`00-setup.md`](./00-setup.md) §4.
- **`tools/list` returns three tools but `resolve_agent_tool` later
  returns "no tools available"** — Solo is up but has zero agent
  tools registered, or the tools it has don't classify into any
  tier. Either is fine for `tools/list` (Duo's surface is static)
  but blocks step 02.
- **`initialize` and `tools/list` respond, but any tool call into
  Solo (`list_agent_tiers`, `resolve_agent_tool`, `spawn_agent`)
  hangs with no response** — earlier symptom of a missing
  Duo→Solo MCP handshake. `SoloClient.connect()` now performs
  `initialize` + `notifications/initialized` against Solo before
  returning. If you still see this on a fresh build, confirm
  `dist/duo.mjs` was rebuilt after pulling — `npm run build`.

You're ready for [`02-tier-tools.md`](./02-tier-tools.md).
