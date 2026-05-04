# Step 01 Workplan — Config / Spawn Infrastructure

**Roadmap**: `notes/roadmap/roadmap-3-config-and-spawn.md`
**Status**: approved (orchestrator decisions recorded 2026-05-03)
**Generated**: 2026-05-03

---

## Overview

Three parallel, low-risk infrastructure improvements to configuration handling and agent spawning. All three are independent — no cross-stream dependencies. They can be assigned to separate implementors and merged in any order.

**Streams**:

| # | Stream | Todo | Effort |
|---|--------|------|--------|
| A | XDG Base Directory compliance | solo://proj/6/todo/follow-xdg-base-dire--247 | Small |
| B | Agent spawn optional bootstrap prompt | solo://proj/6/todo/ability-to-pass-an-o--246 | Small–Medium |
| C | Auto-detect Solo MCP path | solo://proj/6/todo/solo-mpc-path-should--245 | Small |

**Dependency graph**: None. All three streams are independent and can be implemented and merged in parallel.

**Recommended execution**: All three can be worked simultaneously by separate implementors, or batched A+C (both config-layer changes) with B as an independent track.

---

## Stream A — XDG Base Directory Compliance

**Todo**: solo://proj/6/todo/follow-xdg-base-dire--247
**Priority**: Medium

### Current Behavior

`src/cli/config-loader.ts:17–18`:
```
resolveConfigPath(cwd) = process.env.DUO_CONFIG ?? resolve(cwd, "duo.config.yaml")
```

Config is always resolved relative to `cwd`. There is no global/user config lookup. No XDG support.

### Target Behavior

Resolution order (highest to lowest priority):

1. `DUO_CONFIG` env var — verbatim path (existing behavior, preserved)
2. `$XDG_CONFIG_HOME/duo/config.yaml` — if `XDG_CONFIG_HOME` is set
3. `~/.config/duo/config.yaml` — unconditional fallback (XDG default)

Note: The todo body does **not** include `cwd`-relative lookup in the new flow. Dropping the cwd-relative fallback is an intentional design change per the todo description: _"I don't know that we'd ever want to look in `cwd` for the config file."_ This is a **design decision that needs coordinator confirmation** (see Open Questions).

### Scope

**Files to change**:
- `src/cli/config-loader.ts` — update `resolveConfigPath()` logic

**Files to review for impact**:
- `src/cli/commands/config.ts` — `duo config path` command displays the resolved path; should continue to work correctly
- `src/cli/commands/doctor.ts` — uses resolved config; no change expected
- `src/cli/connect.ts` — calls `loadConfig(cwd?)`; the `cwd` parameter may become irrelevant for config path resolution (see Open Questions)
- All commands that accept `--cwd` flag and pass it to `loadConfig` — behavior may change

### Subtasks

- [ ] **A1** — Update `resolveConfigPath()` in `src/cli/config-loader.ts`:
  - Remove (or deprioritize) cwd-relative lookup
  - Add `$XDG_CONFIG_HOME/duo/config.yaml` check when `XDG_CONFIG_HOME` is set
  - Add `~/.config/duo/config.yaml` as the final fallback
  - Use Node's `os.homedir()` for `~` expansion
- [ ] **A2** — Update `resolvePolicyPath()` similarly if policy should follow XDG conventions (coordinator decision needed)
- [ ] **A3** — Review and update `--cwd` flag behavior across all commands — determine if `--cwd` still makes sense or should be deprecated
- [ ] **A4** — Update `duo config path` output to reflect new resolution logic
- [ ] **A5** — Add/update tests for `resolveConfigPath()` covering all three resolution cases
- [ ] **A6** — Update documentation (README or docs/) if config path is documented

### Dependencies

None on other streams.

### Acceptance Criteria

- `DUO_CONFIG` env var still overrides everything (existing behavior preserved)
- When `XDG_CONFIG_HOME=/custom/path` is set, config loads from `/custom/path/duo/config.yaml`
- When `XDG_CONFIG_HOME` is not set, config loads from `~/.config/duo/config.yaml`
- `duo config path` reports the resolved path correctly under all cases
- Existing tests pass; new tests cover XDG resolution
- No regression when a valid config exists at the XDG path

### Effort Estimate

**Small** — ~2–3 hours. Simple logic change in one function; most effort is in tests and ensuring the `--cwd` flag impact is understood.

### Risk

Low. The only behavioral break is removing cwd-relative config lookup, which is an intentional design change. The `DUO_CONFIG` env var escape hatch is preserved. Local development using `duo.config.yaml` in cwd will break unless users migrate to `~/.config/duo/config.yaml` or use `DUO_CONFIG`.

---

## Stream B — Agent Spawn Optional Bootstrap Prompt

**Todo**: solo://proj/6/todo/ability-to-pass-an-o--246
**Priority**: Medium

### Current Behavior

`src/cli/commands/agent.ts` — `duo agent spawn <tier>`:
```
duo agent spawn <tier> [--name <name>] [--project-id <id>] [--cwd <path>] [--json] [--quiet]
```

The spawn call (`src/solo-client.ts:201–215`) sends `{ kind, agent_tool_id, name?, project_id? }` to Solo via `spawn_process`. No bootstrap prompt is passed.

Looking at the Solo MCP tool definition (`src/tools/`), `spawn_process` already accepts an optional `include_agent_instructions` field, but no free-text prompt injection.

### Target Behavior

`duo agent spawn <tier> [options] [prompt]`

An optional final positional argument (or `--prompt` flag) is accepted as a bootstrap prompt string. When provided, it is passed through to Solo's `spawn_process` as a bootstrap prompt for the spawned agent's first turn.

**API/syntax is a deferred decision** (see Open Questions). The todo title says "optional final argument" suggesting a positional, but `--prompt` flag is also viable.

### Scope

**Files to change**:
- `src/cli/commands/agent.ts` — add optional prompt argument to the `spawn` subcommand
- `src/solo-client.ts` — update `spawnProcess()` to accept and forward an optional `prompt` field
- `src/types/` — update the Solo spawn response/request type if prompt is added to the schema

**Files to review**:
- Solo MCP `spawn_process` tool schema — need to confirm whether Solo already accepts a prompt field or whether this requires a Solo-side change (critical dependency check)

### Subtasks

- [ ] **B1** — Audit Solo's `spawn_process` MCP tool schema to determine if a prompt/bootstrap field already exists and what it's called (check `src/tools/` or query the running Solo binary)
- [ ] **B2** — Decide on CLI syntax: positional final arg vs `--prompt` flag (coordinator input needed)
- [ ] **B3** — Add the optional argument to `duo agent spawn` command definition in `src/cli/commands/agent.ts`
- [ ] **B4** — Update `SoloClient.spawnProcess()` in `src/solo-client.ts` to accept and pass the prompt field
- [ ] **B5** — Update relevant Zod/TypeScript types if spawn request shape changes
- [ ] **B6** — Add test coverage for spawn with and without prompt
- [ ] **B7** — Update `duo agent spawn --help` output / documentation

### Dependencies

- **B1 is a prerequisite for B3–B5** — the Solo-side API must be understood before implementing the Duo-side changes
- No dependencies on streams A or C

### Acceptance Criteria

- `duo agent spawn small` works unchanged (no regression)
- `duo agent spawn small "Do the thing"` (or `--prompt "Do the thing"`) spawns an agent and delivers the prompt as the first message
- Prompt is optional; omitting it behaves identically to current behavior
- `--help` output documents the prompt argument
- Tests cover both cases

### Effort Estimate

**Small–Medium** — ~3–5 hours. Effort depends heavily on B1 (whether Solo already supports a prompt field). If Solo already has the field, this is a small plumbing change. If not, this may require Solo-side work that blocks Duo-side implementation.

### Risk

**Medium** — contingent on Solo's `spawn_process` supporting a prompt/bootstrap field. If Solo does not yet support this, Duo cannot implement it without a Solo-side change first. This is the only stream with an external dependency risk.

---

## Stream C — Auto-Detect Solo MCP Path

**Todo**: solo://proj/6/todo/solo-mpc-path-should--245
**Priority**: Medium

### Current Behavior

`src/config.ts:7`:
```ts
command: z.string().min(1, "solo.transport.command is required"),
```

`command` is required in the config schema. If not set, config validation fails. Users must explicitly set the path in `duo.config.yaml`.

Local config (`duo.config.yaml`):
```yaml
solo:
  transport:
    type: stdio
    command: /Applications/Solo.app/Contents/MacOS/mcp
```

### Target Behavior

- `solo.transport.command` becomes **optional** in the config schema
- If not configured, Duo checks known paths in order and uses the first one that exists and is executable
- macOS: `/Applications/Solo.app/Contents/MacOS/mcp`
- Other OS paths: deferred (noted for future augmentation)
- If none found: fail with a clear, actionable error message (not a Zod validation error)
- Explicit config value always takes precedence over auto-detection

### Scope

**Files to change**:
- `src/config.ts` — make `command` optional (`z.string().optional()`)
- `src/transport/stdio.ts` (or a new helper) — add path resolution logic that checks known paths
- `src/cli/commands/doctor.ts` — update the MCP path check to reflect auto-detection; show detected path
- `src/cli/connect.ts` — after config load, resolve the transport command before building StdioTransport

**Recommended approach**: Add a `resolveTransportCommand(configured?: string): string` function in a new `src/transport/resolve-command.ts` (or inline in `connect.ts`) that:
1. Returns `configured` if truthy
2. Checks `existsSync` on each known path in order
3. Returns the first found path
4. Throws a descriptive `DuoError` if none found

### Subtasks

- [ ] **C1** — Make `solo.transport.command` optional in `src/config.ts` Zod schema
- [ ] **C2** — Implement `resolveTransportCommand()` with known-path auto-detection (macOS: `/Applications/Solo.app/Contents/MacOS/mcp`)
- [ ] **C3** — Integrate into connection flow: call `resolveTransportCommand` before `new StdioTransport()`
- [ ] **C4** — Update `duo doctor` to show detected vs configured path and pass/fail accordingly
- [ ] **C5** — Add descriptive error when no path is found (neither configured nor auto-detected)
- [ ] **C6** — Add tests: configured path used, auto-detected path used, neither found → error
- [ ] **C7** — Update docs/README if the config field is documented as required

### Dependencies

None on other streams. However, Stream A (XDG) affects where the config file is loaded from — if both A and C are implemented together, C should be tested against an XDG-resolved config (not just cwd config). This is a **testing concern**, not a code dependency.

### Acceptance Criteria

- Existing explicit `command` config still works (no regression)
- With `command` omitted from config, Duo auto-detects `/Applications/Solo.app/Contents/MacOS/mcp` on macOS if it exists
- `duo doctor` reports the resolved path (and whether it was auto-detected or configured)
- Clear error message when Solo is not found at any known path and not configured
- No Zod schema validation error for missing `command` — that check is moved to runtime resolution

### Effort Estimate

**Small** — ~2–3 hours. Straightforward: make one field optional, add a small resolver function, update the doctor check.

### Risk

Low. Auto-detection is a fallback only; explicit config is preserved. The only risk is a poor error message when detection fails, which is mitigated by subtask C5.

---

## Build Sequence Recommendations

### Option 1: Full parallel (three implementors)

All three streams can be worked simultaneously. No cross-stream code conflicts expected (they touch different files/layers). Merge in any order.

### Option 2: Batched (two implementors)

- **Track 1**: A + C together (both config-layer changes; one implementor can do both sequentially in ~5–6 hours)
- **Track 2**: B independently (needs B1 audit first)

Rationale: A and C both touch the config loading path. Doing them together reduces the chance of merge conflicts and allows A's new XDG resolution to be tested together with C's optional-command behavior in a single integration pass.

### Option 3: Sequential (one implementor)

Recommended order: **C → A → B**

- C is the smallest and most self-contained
- A has a design decision (cwd removal) that needs confirmation
- B has an external dependency (Solo API audit) that may introduce a wait

---

## Risk Assessment

| Risk | Stream | Likelihood | Impact | Mitigation |
|------|--------|-----------|--------|------------|
| Solo `spawn_process` doesn't support prompt field | B | Medium | High — blocks B entirely | Audit Solo schema (B1) before starting B3–B5 |
| Removing cwd config lookup breaks existing workflows | A | Medium | Medium — developer experience | Document migration; `DUO_CONFIG` env var is a safe escape hatch |
| XDG path doesn't exist on first run (no config yet) | A | High | Low — expected; just fails to load | Graceful "no config found" error with instructions |
| Solo path auto-detection false positive (wrong binary at known path) | C | Low | Low | `duo doctor` will show the path; user can override |
| Merge conflicts between A and C | A+C | Low | Low | Coordinate if same implementor; otherwise A touches `config-loader.ts`, C touches `config.ts` + `transport/` — minimal overlap |

---

## Open Questions / Decisions Needed from Coordinator

### OQ-1 (Stream A) — Drop cwd-relative config lookup?

The todo body explicitly says _"I don't know that we'd ever want to look in `cwd` for the config file"_, implying the cwd lookup should be removed. However, Duo currently ships with a `duo.config.yaml` in the project root, which is the cwd-relative config.

**Decision needed**: Confirm that cwd-relative lookup is intentionally removed. If so, what is the migration path for existing local `duo.config.yaml` files?

**✅ ORCHESTRATOR DECISION (2026-05-03)**: YES — Remove cwd-relative lookup. Users with a local `duo.config.yaml` should move it to `~/.config/duo/config.yaml` or set `DUO_CONFIG`. Document this in the changelog.

### OQ-2 (Stream A) — Should policy file also follow XDG?

The todo only mentions `config.yaml`, but `duo.policy.yaml` has a parallel resolution path. Should `resolvePolicyPath()` also be updated to XDG, or left as cwd-relative?

**Suggested answer**: Policy is workspace/project-scoped (not user-scoped), so cwd-relative makes sense for policy. Leave `resolvePolicyPath()` unchanged.

### OQ-3 (Stream B) — CLI syntax for bootstrap prompt

Positional final argument (`duo agent spawn small "prompt text"`) vs named flag (`duo agent spawn small --prompt "prompt text"`)?

**✅ ORCHESTRATOR DECISION (2026-05-03)**: Use `--prompt` flag (named). Cleaner and unambiguous when combined with other optional args.

### OQ-4 (Stream B) — Does Solo's `spawn_process` already support a prompt/bootstrap field?

This must be audited before implementing B. If Solo doesn't support it, is there a Solo-side ticket to implement it, and what is the expected field name?

**Action required**: Implementor to audit Solo MCP schema as B1 before proceeding.

**✅ ORCHESTRATOR DECISION (2026-05-03)**: Stream B should proceed. Researcher suspects prompt support is already in Solo MCP. If not, implementor can instrument prompt delivery via `send_input` after spawn. Run B1 audit to confirm, then proceed with implementation regardless.

### OQ-5 (Stream C) — Error behavior when no Solo path found

Should the error be thrown at connection time (lazily, when a command tries to connect) or at config load time (eagerly)? Current Duo behavior is to fail at connection time.

**Suggested answer**: Keep lazy — fail at connection time with a clear message. This is consistent with current behavior and avoids blocking commands like `duo config path` that don't need the Solo transport.
