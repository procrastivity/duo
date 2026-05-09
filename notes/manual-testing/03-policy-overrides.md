# 03 · Policy overrides

> Applies to: Duo current `main`.
> Prereqs: [`02-tier-tools.md`](./02-tier-tools.md) complete; you
> know which tools Solo has registered and which tier each one
> classifies into under built-in rules.

This doc exercises `duo.policy.yaml` — the optional file that
overrides classifier tokens and selection strategy. Each subsection
drops a focused policy in place, restarts Duo, and confirms one
specific behavior change. After each subsection, **revert the
policy file** before moving on, so each change is observed in
isolation.

The runbook fixture
[`fixtures/duo.policy.yaml`](./fixtures/duo.policy.yaml) carries
every example below as commented-out blocks. Uncomment one block at
a time, restart Duo, exercise, then re-comment.

## Restart contract

Duo loads policy at process start. **There is no live reload.**
After every edit to `duo.policy.yaml`:

1. Stop the running Duo process (Ctrl+C its driver, or restart the
   `duo` MCP server in your client — Claude Code restarts an MCP
   server when its config changes; failing that, restart Claude
   Code or use `claude mcp restart duo` if your version exposes
   it).
2. Reissue the `tools/list` / tool-call requests; the response
   should now reflect the new policy.

If you change behavior is not what's described below, stale Duo
process is the most common cause.

## 1. `command_tokens` — extend mode

**Goal**: add a project-specific token to the large tier without
losing built-in large tokens (`opus`, `flagship`, etc.).

Policy:

```yaml
command_tokens:
  large:
    mode: extend
    tokens:
      - pro
```

Setup: pick (or temporarily register) a Solo agent tool whose
`command` includes `pro` but **not** any built-in large token —
e.g. a tool with command `claude --model pro` registered as
`claude-pro`.

Exercise:

```
mcp__duo__resolve_agent_tool { "tier": "large" }
```

Or via driver:

```sh
./notes/manual-testing/scripts/02-resolve-agent-tool.sh large
```

Acceptance:

- The new `claude-pro` tool appears as a candidate (either as
  `selected` or in `alternatives`).
- `selected.matched_tokens` for that tool contains
  `{ "token": "pro", "source": "command" }`.
- Tools previously classified as large under built-in rules are
  still candidates — extend keeps the built-in token set.

Revert the policy file (re-comment the block) and restart Duo
before continuing.

## 2. `command_tokens` — replace mode

**Goal**: confirm `replace` discards the built-in token set for the
named tier.

Policy:

```yaml
command_tokens:
  large:
    mode: replace
    tokens:
      - flagship
```

Exercise:

```
mcp__duo__list_agent_tiers
```

Or via driver:

```sh
./notes/manual-testing/scripts/02-list-agent-tiers.sh
```

Acceptance:

- The large tier's `default` and `alternatives` only include tools
  whose command/name carries the literal token `flagship`.
- Tools whose only large-tier signal was `opus` / `pro` / etc. are
  no longer candidates for large under this policy. They may
  remain unclassified (no tier) and will appear in
  `diagnostics.ignored_tools` with a reason.

Revert and restart.

## 3. `selection.preference` — custom strategy

**Goal**: confirm a populated `selection.preference` flips
`diagnostics.strategy` from `random` to `custom` and biases the
chosen tool deterministically.

Setup: ensure at least one tier (typically `medium`) has
candidates of two distinct `tool_type` values (e.g. `opencode` and
`codex`) so the preference has work to do.

Policy:

```yaml
selection:
  preference:
    - tool_type: opencode
    - tool_type: codex
```

Exercise repeatedly:

```
mcp__duo__resolve_agent_tool { "tier": "medium" }
```

Or via driver:

```sh
./notes/manual-testing/scripts/02-resolve-agent-tool.sh medium
```

Acceptance:

- `diagnostics.strategy` is `"custom"` (not `"random"`).
- `diagnostics.preference_applied` is `true`.
- `selected.tool_type` is `opencode` on every call where any
  `opencode` candidate exists at the medium tier — the choice is
  pinned by the first matching selector.
- If you remove all `opencode` tools from Solo (or pick a tier
  with no `opencode` candidates), `selected.tool_type` falls
  through to `codex`, and a fresh request still reports
  `preference_applied: true` (the policy ran; it just didn't have
  a `opencode` match).

Revert and restart.

## 4. Missing-but-explicit `DUO_POLICY` errors

**Goal**: confirm the explicit-but-missing branch of policy
loading is surfaced as an unavailable-server tool error.

`runServer()` catches config/policy load failures and starts an
"unavailable server" that reports the error via a structured
`solo_connection_failed` tool response — *not* on stderr at boot
(see `src/server.ts` `runServer` / `createUnavailableServer`).
`tools/list` succeeds because Duo's tool surface is static; the
error only appears on a `tools/call`. Use the
`list_agent_tiers` driver to trigger one:

```bash
DUO_POLICY=/tmp/does-not-exist.yaml \
  ./notes/manual-testing/scripts/02-list-agent-tiers.sh
```

Acceptance:

- Boot itself does not error on stderr; exit code is `0` (or `124`
  if `DUO_TIMEOUT <= DUO_SLEEP` — see `scripts/README.md`).
- The `tools/call` response on stdout has `result.isError: true`,
  with `result.content[0].text` containing the JSON
  `{"code":"solo_connection_failed","message":"DUO_POLICY is set
  to \"/tmp/does-not-exist.yaml\" but file does not exist"}`.

For comparison, the silent-no-op branch (no `DUO_POLICY`, default
`./duo.policy.yaml` absent):

```bash
rm -f duo.policy.yaml
node ./dist/duo.mjs mcp < /dev/null; echo "exit=$?"
```

Acceptance: no policy-related stderr; exit `0`; Duo boots with
built-in classifier rules. (Stdin is closed, so the process exits
shortly after — that's expected.)

## 5. Schema rejection

**Goal**: confirm Zod rejects malformed policies at load time.

Policy:

```yaml
command_tokens:
  large:
    mode: replace_all      # invalid — only "extend" / "replace"
    tokens:
      - flagship
```

Exercise: start Duo.

Acceptance: process exits with code 1 and stderr contains
`Failed to parse policy from …` followed by a Zod issue path
(e.g. `command_tokens.large.mode`). Duo never reaches the MCP
handshake stage in this case.

Revert.

You're ready for [`04-logging.md`](./04-logging.md).
