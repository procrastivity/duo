---
name: duo-delegation-loop
description: Wraps the live Duo CLI for the delegation loop. Use when launching a Duo builder, sending one instruction, and observing the result. "Launch a builder" means preset builder (duo session launch builder). When the user did not name a preset and did not say "a builder", run duo config show --output json and pick from result.presets[].name. After send, poll session show and conversation list until assistant turn text or a 20s timeout. Match on result.condition.value, not the whole condition object.
---

# Duo delegation loop

Use this skill for the Duo delegation loop. Do not type an ad-hoc `duo` recipe outside this sequence.

This skill wraps the live `duo` CLI. It does not invent MCP tools or a second protocol.

Pass `--output json` so the agent can read ids from the envelope.

## Sequence

Run the three verb families in this order. Finish launch before you send. Read status after you send.

### 1. Launch the builder

The positional argument is a **preset name**, not an agent runtime. `--require` / `--avoid` only narrow candidates inside that preset.

If the user says "a builder" / "launch a builder" and does not name another preset, use `builder`:

```
duo session launch builder --output json
```

With a runtime pin, still name the preset:

```
duo session launch builder --require agent_runtime=claude --output json
```

If they name `orchestrator` or `build_and_verify` (or another preset), use that name instead.

When the user did **not** name a preset and did **not** say "a builder", discover the roster with:

```
duo config show --output json
```

Pick from `result.presets[].name`. If intent still does not match a name, ask — do not guess `builder`. The builder default is only for the "a builder" phrasing. There is no `duo preset` / `duo preset list` family — do not grep the YAML.

Read `session_id` from the result. Launch has no `--prompt` flag. Delivery stays strictly post-launch.

Optional `--workspace` sets the launched working directory.

### 2. Deliver one instruction

```
duo prompt send <session-id> \
  --text <prompt text> \
  --idempotency-key <caller key> \
  [--expires-at <RFC3339>] \
  [--runtime-instance <runtime-instance-id>] \
  [--actor cli] \
  --output json
```

Pass `--text` and `--idempotency-key`. Both flags must be present.

`--expires-at` is optional RFC3339. The default is 15 minutes from now.

There is no `--queue-policy` flag. Queue is always `queue_until_safe`.

`--runtime-instance` is the optional I-5 bind check. Omit it to use the session current instance.

`--actor` defaults to `cli`. Omit it unless you need a different actor.

Choose a fresh idempotency key for each distinct instruction. Retry with the same key and the same text. Same key plus different text returns `command.idempotency_conflict`. The digest is SHA-256 of the prompt text only.

Read `command_id` from the send result. Text output also points at `duo prompt show`.

### 3. Read status and result

```
duo session show <session-id> --output json
duo conversation list <session-id> --output json
duo prompt show <command-id> --output json
```

After send, poll **show** and **list** until success or **20 seconds**. About one pair per second. The agent re-invokes the CLI; no shell `seq` loop. Do not `2>/dev/null`. Do not use `duo wait`, `--block-for`, or `seq 1` shell polling.

Extract condition and assistant text with:

```
jq -r '.result.condition.value // "absent"'
jq -r '.result.items[]? | select(.author_role=="assistant") | .blocks[0].content.text'
```

Do not print `result["condition"]` — that is an object; a `case` on it never matches `idle`.

| Observation | Action |
|---|---|
| `absent` / `unknown` / `missing transcript` | retry |
| list error containing `opening transcript` or `conversation_list_failed` | retry |
| list exit 0 with 0 items | retry (Claude can do this at 200ms) |
| `working` and no assistant text yet | retry |
| assistant text present | success; then `prompt show` for command state |
| `blocked` / `exited` | stop and report (not a silent timeout) |
| `idle` without assistant text | keep polling list until text or 20s |
| 20s with no assistant text | fail visibly (observe timed out) |

`author_role` `user` or `peer` is not the reply (Claude injects may list as peer).

Inspect command state with `duo prompt show`. Do not invent a `duo command` family. Registry `command.inspect` projects to `duo prompt show`.

Command states include accepted, queued, attempting, delivered, expired, and failed. A hold on queued means human priority. Wait and inspect again. Do not paste into a terminal. Do not call stop or interrupt.

Optional `--after` pages `duo conversation list` from `next_page`. Optional `--limit` caps the turn count.

## Out of scope

- Launch `--prompt`
- A `duo command` family
- Stop, interrupt, or terminal paste
- Composer-lease verbs
- MCP tools and presentation routes
- Stage 5 generated projection
- `duo wait`, `--block-for` (parked; notes/54)

## Install by hand

This file is the normative source. Install it by hand into the orchestrating harness. There is no Stage 5 renderer in this milestone.

Do not run these installs as part of authoring this skill.

### Claude Code (first)

Copy or symlink this directory to `~/.claude/skills/duo-delegation-loop/`. The directory must contain `SKILL.md`. Claude Code follows symlinks.

```
ln -s "$REPO/skills/duo-delegation-loop" ~/.claude/skills/duo-delegation-loop
```

Or copy:

```
cp -R "$REPO/skills/duo-delegation-loop" ~/.claude/skills/duo-delegation-loop
```

`$REPO` is the `duo` checkout that holds this file.

### Cursor

Install the same `SKILL.md` in either or both of these locations.

- Project skill: `.cursor/skills/duo-delegation-loop/`
- Personal skill: `~/.cursor/skills/duo-delegation-loop/`

Never write into `~/.cursor/skills-cursor/`. Cursor keeps that tree for built-in skills.

### Pi

This milestone parks Pi `--skill` load. Do not treat it as an install procedure.
