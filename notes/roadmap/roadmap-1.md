# Roadmap 1 — Solo Orchestrator Companion

**Project**: Duo  
**Status**: active  
**Intake**: `notes/proposals/solo-orchestrator-companion-intake.md`  
**Currently working on**: Step 4

---

## Step 1 — Project scaffold and Solo connection

**Goal**: TypeScript MCP server skeleton with working Solo connection (stdio command-spawn), startup config validation, and test harness. No tier logic yet.

**Shipping criteria**:

- [ ] TypeScript MCP server starts and registers with `@modelcontextprotocol/sdk`
- [ ] Solo connection via stdio command-spawn; transport layer is abstracted for future HTTP/other modes
- [ ] Invalid or missing Solo connection config fails startup with a structured error
- [ ] `vitest` test suite runs; Solo client is injectable for testing
- [ ] `SOLO_PROCESS_ID` and `SOLO_PROJECT_ID` are detected from environment and surfaced in session context (informational only)

**Deferred decisions resolved in this step**:

- Transport: stdio command-spawn first; abstraction layer for future modes
- Startup vs first-call config validation: validate at startup

**New deps**: `@modelcontextprotocol/sdk`, `zod`, `vitest`, `yaml`, `execa`

**Risk**: low

---

## Step 2 — Tier classifier, resolver, and fixture coverage

**Goal**: Classifier + resolver that turns a `list_agent_tools` payload into tier resolution. Deliver `list_agent_tiers` and `resolve_agent_tool` MCP tools. Fixture tests cover all observed Solo runtimes plus edge cases.

**Shipping criteria**:

- [x] `list_agent_tiers` returns `small`, `medium`, `large` availability with selected default and alternatives
- [x] `resolve_agent_tool` returns selected `agent_tool_id`, classification source (`command` or `name_fallback`), alternatives, and diagnostics
- [x] Disabled tools excluded before any tier matching
- [x] Command tokens classify first; name tokens only on fallback; match source always reported
- [x] Default selection strategy when multiple candidates match: **random**; alternatives always listed
- [x] `unsupported_tier` error for unknown tier labels; `tier_unavailable` when no enabled candidate exists
- [x] Fixture tests: `opencode-ghc-haiku`, `opencode-ghc-sonnet`, `codex-fast`, `codex-standard`, `codex-flagship` (all enabled)
- [x] Fixture tests: disabled variants of matching tools (assert ignored)
- [x] Fixture tests: misleading name + accurate command; accurate name + misleading command
- [x] Fixture tests: ambiguous and unknown command cases with expected structured diagnostics

**Deferred decisions resolved in this step**:

- Ranking when multiple candidates match: random (default), round-robin, custom (opt-in via policy)
- No implicit tier fallback: fail loudly; explicit policy only
- Default classifier rules: embedded TypeScript constants for MVP

**New deps**: (none beyond Step 1)

**Risk**: medium

---

## Step 3 — Spawn integration and project scope

**Goal**: `spawn_agent` that resolves a tier and delegates to Solo `spawn_process`. Handles optional name, project scope, and structured errors.

**Shipping criteria**:

- [ ] `spawn_agent` with `tier` + optional `name` calls `Solo spawn_process(kind="agent", agent_tool_id=N)`
- [ ] Response includes Solo process id, final name, selected tier, and tool summary
- [ ] Caller `project_id` takes precedence; `SOLO_PROJECT_ID` is default scope fallback
- [ ] Solo rejection returns a structured error; never reports success on failure
- [ ] Name rejection returns structured error; no hidden retry with different name

**Deferred decisions resolved in this step**:

- `spawn_agent` input: `tier`, `name`, `project_id` only in MVP; prompt/bootstrap deferred

**New deps**: (none beyond Step 1)

**Risk**: medium

---

## Step 4 — YAML policy overrides and structured logging

**Goal**: Local YAML config for custom token patterns and selection preference ordering. Structured operational logs for resolution and spawn decisions.

**Shipping criteria**:

- [ ] YAML policy can add/adjust command-token patterns per tier
- [ ] YAML policy can define selection preference ordering (maps to `custom` selection mode)
- [ ] Invalid YAML fails with field-level errors
- [ ] Resolver diagnostics identify override vs built-in rule matches
- [ ] Resolution and spawn logs include required fields (see Story 13)
- [ ] Logs omit prompts and free-form task content

**New deps**: `yaml` (Step 1), structured logging lib (e.g. `pino`)

**Risk**: low

---

## Step 5 — Documentation, packaging, and adoption

**Goal**: Concise usage docs with working examples for all three MCP tools using tier labels, plus a primary distribution channel that lets MCP clients install and invoke the companion without manual build steps.

**Shipping criteria**:

- [ ] Examples for `list_agent_tiers`, `resolve_agent_tool`, `spawn_agent` using tier labels (no `agent_tool_id`)
- [ ] Explicitly states companion is standalone and not a Hypomnema feature
- [ ] States direct Solo `spawn_process` remains available; playbooks should prefer companion
- [ ] README covers installation, configuration, MCP client setup, basic usage
- [ ] Published as an npm package with a `bin` entry runnable via `npx`; MCP client config examples use the published name
- [ ] Release pipeline produces a versioned, tagged npm publish from CI on a release tag
- [ ] README documents the supported Node version range and any runtime prerequisites

**Deferred decisions resolved in this step**:

- Primary distribution: **npm package** (Node-based MCP servers are conventionally invoked via `npx`; users configuring an MCP client already have Node available)
- Standalone single-file binaries (e.g. `bun build --compile`) and Homebrew-style channels are **deferred** to a follow-up step; revisit if non-Node installation friction is reported or if Solo itself ships as a binary

**Risk**: low
