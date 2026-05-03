# 04 · Logging

> Applies to: Duo current `main`.
> Prereqs: any of [`02-tier-tools.md`](./02-tier-tools.md) or
> [`03-policy-overrides.md`](./03-policy-overrides.md) — this doc
> reads logs produced by the same calls.

This doc verifies Duo's structured-log contract:

- Stderr carries one JSON object per line, each with a `level` and
  an `event` field.
- Stdout carries only MCP JSON-RPC traffic.
- Each tool call produces exactly one terminal log event
  (`resolution.success` / `resolution.failure` / `spawn.success` /
  `spawn.failure`).
- Free-form prompt or task content is **not** logged.

## 1. Capture a stderr stream

The simplest capture: drive Duo via the raw JSON-RPC harness from
[`01-running-duo.md`](./01-running-duo.md), redirect stderr to a
file, and inspect.

Build a fuller driver that also exercises tools:

```bash
cat > /tmp/duo-logs.sh <<'BASH'
#!/usr/bin/env bash
{
  echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"runbook","version":"0"}}}'
  echo '{"jsonrpc":"2.0","method":"notifications/initialized"}'
  echo '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"list_agent_tiers","arguments":{}}}'
  echo '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"resolve_agent_tool","arguments":{"tier":"medium"}}}'
  echo '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"resolve_agent_tool","arguments":{"tier":"purple"}}}'
  sleep 1
} | node ./dist/index.js
BASH
chmod +x /tmp/duo-logs.sh
/tmp/duo-logs.sh 2>/tmp/duo.err >/tmp/duo.out
```

Then:

```bash
jq -c . < /tmp/duo.err   # one JSON object per line
```

If `jq` rejects any line, that's a regression — Duo's stderr
should be parseable line-by-line.

## 2. Expected events

Each request above should produce one terminal event. Expected
events (in order):

| Request | Terminal stderr event | Notable fields |
|---|---|---|
| `list_agent_tiers` | one `resolution.success` per available tier | `requested_tier`, `selected_tool_id`, `candidate_count`, `strategy` |
| `resolve_agent_tool { tier: "medium" }` | `resolution.success` | as above |
| `resolve_agent_tool { tier: "purple" }` | `resolution.failure` | `error_code: "unsupported_tier"`, `available_tiers` |

Stretch — add a `spawn_agent` call to the driver and confirm:

| Request | Terminal stderr event | Notable fields |
|---|---|---|
| `spawn_agent { tier: "<available>" }` | `resolution.success` then `spawn.success` | `solo_process_id`, `process_name` |
| `spawn_agent { tier: "purple" }` | `spawn.failure` (or `resolution.failure` followed by `spawn.failure`) | `error_code` |

## 3. Stdout-cleanliness check

Stdout must contain only MCP JSON-RPC messages — no
`console.log`, no banner lines, no warnings. Verify:

```bash
jq -c . < /tmp/duo.out >/dev/null && echo "stdout is clean JSONL"
```

If `jq` errors, look at the offending line in `/tmp/duo.out`. Any
non-JSON line is a regression — stdout cleanliness is a
load-bearing invariant for stdio MCP transport.

## 4. Privacy contract

Skim `/tmp/duo.err` for free-form content:

```bash
grep -i "prompt\|task\|message" /tmp/duo.err
```

The shipped logger has no field that carries prompt or task body.
You may match the literal **field names** above (e.g. a config
field or the word inside a tool description), but you should
**not** see user-supplied free-form strings, tool arguments other
than `tier`/`name`/`project_id`, or any narrative content. If you
do, file a privacy regression.

## 5. Log levels

Duo's logger emits `info` for success events and `error` for
failures. There is no shipped level filter — every event goes to
stderr. If you want to discard `info` for a noisy session:

```bash
/tmp/duo-logs.sh 2>&1 >/dev/null | jq -c 'select(.level=="error")'
```

(Send stderr through `jq` while routing stdout to `/dev/null` so
the protocol stream doesn't pollute the filter.)

## Done

You've exercised every documented Duo surface as of current `main`:
build, config, MCP handshake, three tools, policy extend / replace
/ preference, missing-policy error, schema rejection, structured
logging, stdout cleanliness, privacy contract.

If you found anything that doesn't match the runbook, the runbook is
likely wrong before the code is — open a fix or flag it back to the
orchestrator.
