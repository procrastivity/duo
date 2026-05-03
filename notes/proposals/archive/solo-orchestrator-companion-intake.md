# Proposal Intake — Solo Orchestrator Companion

**Status**: draft  
**Date**: 2026-05-01  
**Intake inputs**:

- `docs/solo-orchestrator-companion-prd.md` — PRD defining problem, requirements, tool surface, risks, open questions
- `docs/solo-orchestrator-companion-stories.md` — 15 user stories across 5 epics

---

## Summary

Build a standalone TypeScript MCP server that sits between agent playbooks and Solo's low-level process tools. Today, orchestration workflows must hard-code `agent_tool_id` values that are local database details — fragile, drift-prone, and unportable. The companion introduces a stable capability-tier surface (`small`, `medium`, `large`) so playbooks can say "spawn a large agent" and the companion resolves it to the correct enabled local Solo tool deterministically, using the tool command as the primary classification signal and display name only as fallback.

## Source Inputs

| Source | Type | Role in intake |
|---|---|---|
| `docs/solo-orchestrator-companion-prd.md` | PRD | primary |
| `docs/solo-orchestrator-companion-stories.md` | stories | primary |

## Candidate Outcomes

- Outcome: Playbooks can list which tiers are available locally without knowing any Solo tool IDs
  - Source: PRD §5, REQ-001, Story 1
  - User-visible result: `list_agent_tiers` returns availability and selected defaults
  - Verification signal: fixture tests with observed tool list resolve all three tiers

- Outcome: Playbooks can resolve a tier to a specific Solo tool and inspect the decision before spawning
  - Source: PRD §6 Use Case 2, REQ-002, Stories 3–6
  - User-visible result: `resolve_agent_tool` returns `agent_tool_id`, classification source, alternatives
  - Verification signal: repeated calls return same candidate; tests cover command-first and name-fallback cases

- Outcome: Playbooks can spawn an agent by tier without maintaining local ID mappings
  - Source: PRD §5, REQ-003, Stories 7–9
  - User-visible result: `spawn_agent` returns Solo process id and name; no `agent_tool_id` in caller prompts
  - Verification signal: integration test against mock Solo MCP; optional live smoke test

- Outcome: Local installations can customize tier policy without editing source code
  - Source: PRD §6 Use Case 4, REQ-010, Story 12
  - User-visible result: YAML config accepted and validated; resolver diagnostics show override match source
  - Verification signal: config validation test with field-level errors; resolver test with custom rule

---

## Proposed Roadmap Shape

### Step 1 — Project scaffold and Solo connection

**Goal**: Establish the TypeScript MCP server skeleton with working Solo connection configuration, startup validation, and test harness. No tier logic yet — just a server that starts, connects to Solo, and fails clearly when misconfigured.

**Shipping criteria**:

- [ ] TypeScript MCP server starts and registers with `@modelcontextprotocol/sdk`
- [ ] Solo connection configuration accepted at startup (explicit mode first; at least one transport supported)
- [ ] Invalid or missing Solo connection config fails startup with a structured error and clear message
- [ ] `vitest` test suite runs; Solo client is injectable for testing
- [ ] `SOLO_PROCESS_ID` and `SOLO_PROJECT_ID` are detected from environment and exposed in session context (no override behavior yet)

**Deferred decisions resolved in this step**:

- Decision: Which transport mode(s) to support first for connecting to Solo
  - Source: PRD Open Question 4
  - Why this step: Cannot write Solo client without picking a transport; must be resolved before any Solo interaction
  - Candidate answer: Start with stdio/command-spawning as primary mode; HTTP as optional secondary

- Decision: Where to validate config (startup vs first tool call)
  - Source: Story 10 AC — "Invalid connection configuration fails startup with a clear error"
  - Why this step: Startup validation is part of the scaffold contract; deferred validation would require rework

**New deps**:

- `@modelcontextprotocol/sdk`
- `zod`
- `vitest`
- `yaml` (added here even if YAML policy is Step 4, to keep dep management in one place)
- `execa` (if command-spawning transport is chosen)

**Risk**: low

**Source coverage**:

- `PRD`: REQ-008, §7b (Solo remains authority), §10 (error messages tell caller what to do)
- `stories`: Story 10, Story 11 (detection half)

---

### Step 2 — Tier classifier, resolver, and fixture coverage

**Goal**: Implement the classifier and resolver that turn a `list_agent_tools` payload into a deterministic tier resolution. Deliver `list_agent_tiers` and `resolve_agent_tool` MCP tools. Fixture tests cover all observed Solo runtimes plus misleading-name, disabled, ambiguous, and unknown cases.

**Shipping criteria**:

- [ ] `list_agent_tiers` returns `small`, `medium`, `large` availability with selected default and alternatives
- [ ] `resolve_agent_tool` returns selected `agent_tool_id`, classification source (`command` or `name_fallback`), alternatives, and resolver diagnostics
- [ ] Disabled tools are excluded from classification before any tier matching
- [ ] Classification uses command tokens first; name tokens only if command yields no match, and match source is `name_fallback`
- [ ] Selection is deterministic: fixture test with same input produces same selected ID and alternatives order across repeated runs
- [ ] `unsupported_tier` error returned for unknown tier labels; `tier_unavailable` error returned when no enabled candidate exists for a valid tier
- [ ] Fixture tests cover: `opencode-ghc-haiku`, `opencode-ghc-sonnet`, `codex-fast`, `codex-standard`, `codex-flagship` (all enabled)
- [ ] Fixture tests cover: disabled variants of matching tools (assert ignored)
- [ ] Fixture tests cover: misleading name + accurate command; accurate name + misleading command
- [ ] Fixture tests cover: ambiguous and unknown command cases with expected structured diagnostics

**Deferred decisions resolved in this step**:

- Decision: Ranking order when both Codex and OpenCode candidates match the same tier
  - Source: PRD Open Question 2
  - Why this step: Determinism requirement cannot be satisfied without a defined ranking rule
  - Candidate answer: Rank by `tool_type` then `id` as stable tiebreaker; document as local policy default

- Decision: Whether tier labels are fixed or configurable for v0
  - Source: PRD Open Question 1
  - Why this step: Classifier must know the canonical tier set to classify into
  - Candidate answer: Fixed `small`, `medium`, `large` for v0; custom labels deferred to Step 4 policy

- Decision: Whether no-match resolution may fall back to a lower tier
  - Source: PRD Open Question 5
  - Why this step: `fail loudly` invariant requires an explicit decision here; silent fallback would violate §7b
  - Candidate answer: No implicit fallback; all fallback behavior requires explicit local policy (Step 4)

- Decision: Where default classifier rules live
  - Source: PRD Open Question 6
  - Why this step: Classifier needs a rule source; can't write tests without knowing the canonical rule location
  - Candidate answer: Embedded TypeScript constants for MVP; YAML override layer added in Step 4

**New deps**:

- (none beyond Step 1)

**Risk**: medium — classifier correctness depends on undocumented command token patterns; fixture tests are the primary guardrail

**Source coverage**:

- `PRD`: REQ-001, REQ-002, REQ-004, REQ-005, REQ-006, REQ-009, §7b (all six invariants exercised)
- `stories`: Stories 1, 2, 3, 4, 5, 6, 15

---

### Step 3 — Spawn integration and project scope

**Goal**: Add `spawn_agent` that resolves a tier and delegates to Solo `spawn_process`. Handle optional caller-provided process name, optional project scope from input or `SOLO_PROJECT_ID`, and structured errors when Solo rejects the request.

**Shipping criteria**:

- [ ] `spawn_agent` with `tier` and optional `name` calls Solo `spawn_process(kind="agent", agent_tool_id=N, name=...)` with the resolved tool
- [ ] Response includes Solo process id, final process name, selected tier, and selected tool summary
- [ ] Caller-supplied `name` passes through to Solo; if omitted, Solo generates the name
- [ ] Caller-supplied `project_id` takes precedence; if absent, `SOLO_PROJECT_ID` is used as default scope
- [ ] If no project scope is available from input or environment, behavior follows configured connection mode (session project or structured config error)
- [ ] If Solo rejects spawn, `spawn_agent` returns a structured error with Solo's failure message; does not report success
- [ ] If Solo rejects a provided name, companion returns a structured validation/spawn error and does not retry with a hidden name

**Deferred decisions resolved in this step**:

- Decision: Whether `spawn_agent` accepts a first-prompt/instructions payload
  - Source: PRD Open Question 3
  - Why this step: Must define `spawn_agent` input shape; can't finalize the tool schema without this decision
  - Candidate answer: Accept only `tier`, `name`, and `project_id` in MVP; prompt/bootstrap deferred to a separate story per open splitting note

**New deps**:

- (none beyond Step 1)

**Risk**: medium — depends on Solo `spawn_process` contract remaining stable; error handling quality depends on Solo error payload shape

**Source coverage**:

- `PRD`: REQ-003, REQ-007, REQ-011
- `stories`: Stories 7, 8, 9, Story 11 (project-scope half)

---

### Step 4 — YAML policy overrides and structured logging

**Goal**: Add local YAML configuration for custom command-token patterns and candidate preference ordering. Add structured operational logs for resolution and spawn decisions. Both are P1 requirements and are self-contained enough to ship after the core spawn flow is proven.

**Shipping criteria**:

- [ ] YAML policy accepted that adds or adjusts command-token patterns for `small`, `medium`, and `large`
- [ ] YAML policy accepted that defines preference ordering when multiple candidates match the same tier
- [ ] Invalid YAML policy fails validation with specific field-level errors
- [ ] Resolver diagnostics identify when a candidate matched a configured override vs a built-in rule
- [ ] Successful resolution log includes: requested tier, selected tool id, selected tool name, match source, candidate count
- [ ] Failed resolution log includes: requested tier, error code, available tier labels
- [ ] Successful spawn log includes: requested tier, selected tool id, Solo process id, process name
- [ ] Logs do not include full prompts or free-form task content

**Deferred decisions resolved in this step**:

- (None — custom tier labels remain out of scope per Step 2 decision)

**New deps**:

- `yaml` (already added in Step 1)
- structured logging library (e.g., `pino`) or custom structured JSON logger

**Risk**: low — additive layer; does not touch classifier invariants from Step 2

**Source coverage**:

- `PRD`: REQ-010, REQ-012
- `stories`: Story 12, Story 13

---

### Step 5 — Documentation and adoption

**Goal**: Deliver concise usage docs with working examples for all three MCP tools using tier labels. Explicitly state the companion is standalone and not a Hypomnema feature. State that direct Solo `spawn_process` remains available.

**Shipping criteria**:

- [ ] Documentation includes examples for `list_agent_tiers`, `resolve_agent_tool`, and `spawn_agent`
- [ ] Examples use tier labels, not fixed `agent_tool_id` values
- [ ] Documentation explicitly states the companion is standalone and not a Hypomnema feature
- [ ] Documentation states direct Solo `spawn_process` remains available; playbooks should prefer the companion for tier-based spawning
- [ ] README covers: installation, configuration, MCP client setup, basic usage

**New deps**:

- (none)

**Risk**: low

**Source coverage**:

- `PRD`: §10 (UX), MVP acceptance criteria
- `stories`: Story 14

---

## Coverage Map

| Source item | Proposed step | Status | Notes |
|---|---|---|---|
| `PRD#REQ-001` list tiers | Step 2 | planned | |
| `PRD#REQ-002` resolve without spawning | Step 2 | planned | |
| `PRD#REQ-003` spawn by tier | Step 3 | planned | |
| `PRD#REQ-004` exclude disabled tools | Step 2 | planned | |
| `PRD#REQ-005` command-first classification | Step 2 | planned | |
| `PRD#REQ-006` deterministic selection | Step 2 | planned | |
| `PRD#REQ-007` caller-supplied process names | Step 3 | planned | |
| `PRD#REQ-008` explicit Solo connection config | Step 1 | planned | |
| `PRD#REQ-009` resolver diagnostics in tool results | Step 2 | planned | |
| `PRD#REQ-010` YAML policy overrides | Step 4 | planned | P1 |
| `PRD#REQ-011` SOLO_PROJECT_ID / SOLO_PROCESS_ID defaults | Steps 1 & 3 | planned | detection in Step 1, project-scope use in Step 3 |
| `PRD#REQ-012` structured logs | Step 4 | planned | P1 |
| `PRD#REQ-013` live health check | deferred | out-of-scope | P2; add after MVP |
| `stories#S1` list_agent_tiers | Step 2 | planned | |
| `stories#S2` exclude disabled tools | Step 2 | planned | |
| `stories#S3` resolve_agent_tool | Step 2 | planned | |
| `stories#S4` classify by command before name | Step 2 | planned | |
| `stories#S5` fail loudly | Step 2 | planned | |
| `stories#S6` deterministic ranking | Step 2 | planned | |
| `stories#S7` spawn by tier | Step 3 | planned | |
| `stories#S8` pass-through names | Step 3 | planned | |
| `stories#S9` project scope | Step 3 | planned | |
| `stories#S10` configure Solo connection | Step 1 | planned | |
| `stories#S11` Solo env context | Steps 1 & 3 | planned | |
| `stories#S12` YAML policy overrides | Step 4 | planned | |
| `stories#S13` structured logs | Step 4 | planned | |
| `stories#S14` documentation | Step 5 | planned | |
| `stories#S15` fixture coverage for observed runtimes | Step 2 | planned | |

---

## Deferred / Out-of-Scope Items

- Item: Live Solo health check tool (`REQ-013`)
  - Source: PRD §7 (P2)
  - Reason: P2 priority; not required for MVP acceptance; easily added after core flow ships
  - Revisit trigger: User requests it, or operational debugging friction is reported

- Item: Custom tier labels from config
  - Source: PRD Open Question 1, stories open-splitting note
  - Reason: Adds label validation surface complexity; fixed labels sufficient for v0 with YAML token overrides
  - Revisit trigger: Real installation has a tier that cannot be expressed as small/medium/large

- Item: First-prompt/instructions payload in `spawn_agent`
  - Source: PRD Open Question 3, stories open-splitting note
  - Reason: Solo's spawn behavior may not support it cleanly; scope creep risk for MVP
  - Revisit trigger: Solo adds explicit prompt/bootstrap field to `spawn_process`

- Item: Cost accounting, quota management, budget enforcement
  - Source: PRD §9 Non-Goals
  - Reason: Explicitly out of scope; requires external model catalog data Solo does not provide
  - Revisit trigger: Solo exposes cost metadata natively

---

## Open Questions

All three blocking questions are resolved. No open questions remain for Step 1.

~~- Question: Which transport mode(s) should the companion support first for connecting to Solo?~~
**Resolved**: stdio command-spawn is the first transport. Design the Solo client with a transport abstraction layer so HTTP and other modes can be added without rewriting the client. The MCP SDK ecosystem is mature enough to make this straightforward.

~~- Question: What is the canonical ranking order when both Codex and OpenCode candidates match the same tier?~~
**Resolved**: Default selection strategy when multiple candidates match a tier is **random**. The companion will expose three selection modes: `random` (default, zero-config), `round-robin`, and `custom`. This deliberately relaxes the PRD/Story 6 "deterministic" invariant — the trade-off is intentional: zero-config usability is preferred over repeatability by default, and users who need repeatability can opt into `round-robin` or `custom` via policy. Capture this as a named decision in Step 2 planning; fixture tests should test classifier output (candidate set) rather than selected candidate when mode is random.

~~- Question: Should `SOLO_PROCESS_ID` detection affect anything at runtime beyond diagnostics?~~
**Resolved**: `SOLO_PROCESS_ID` is informational only for this project's MVP. Its purpose is self-identification — a process that needs to communicate *about itself* to others (e.g., "I am the orchestrator at process 42") would use it. For tier resolution and spawn, we don't need to pass it around. Detect and surface it in diagnostics as awareness; no behavioral effect in MVP.

---

## Recommendation

Proceed to:

- [x] Start Step 1 now — scaffold and Solo connection
- [x] All blocking open questions resolved
- [ ] Draft/update `notes/roadmap/roadmap-1.md` with steps 1–5
- [ ] Draft/update `notes/roadmap/step-01-workplan.md` with Step 1 tasks

Rationale: All blocking open questions are resolved. Transport is stdio command-spawn with an abstraction layer; selection strategy is random-default with round-robin and custom as opt-in modes. Step 1 can begin immediately.

## Human Review Notes

(append review decisions here)
