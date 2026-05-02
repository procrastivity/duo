# Step 3 Workplan — Spawn Integration and Project Scope

**Status**: planned
**Roadmap**: `notes/roadmap/roadmap-1.md`
**Intake**: `notes/proposals/solo-orchestrator-companion-intake.md`
**Source coverage**: PRD REQ-003, REQ-007, REQ-011, §7b invariants; Stories 7, 8, 9, 11 (project-scope half)

---

## Scope

- **Goal**: Add the `spawn_agent` MCP tool. Given a tier and optional `name` / `project_id`, resolve the tier (reusing Step 2's resolver), then delegate to Solo's `spawn_process(kind="agent", agent_tool_id=N, name?, project_id?)`. Surface Solo's response (process id, final name, selected tier, tool summary). Honor caller `project_id` over `SOLO_PROJECT_ID`. Return structured errors on Solo rejection — never claim success on failure, never silently retry on name rejection.
- **Out of scope**: `prompt`/bootstrap payload in `spawn_agent` input (intake-deferred); live Solo `spawn_process` smoke test against a running Solo instance (deferred — Step 5 or after MVP); YAML policy overrides and structured logging (Step 4); custom selection strategies beyond `random` (Step 4); packaging/docs (Step 5).

---

## Tasks

### Task 1 — Extend `SoloClient` with `spawnProcess` (`src/solo-client.ts`)

Add a `spawnProcess` method that invokes Solo's `spawn_process` MCP tool via `tools/call` and parses the structured content into a typed `SoloSpawnResult`. Validate with `zod` so a malformed payload fails loudly with a field-level error. Solo MCP-level errors (the JSON-RPC `error` envelope) continue to surface as `SoloClientError` exactly like `listAgentTools` already does.

**Solo payload contract** (assumed shape — refine on first integration if Solo's actual response differs):

```ts
// Input (sent in tools/call arguments)
{
  kind: "agent",
  agent_tool_id: number,
  name?: string,
  project_id?: string,
}

// Output (parsed from text content of tools/call result)
{
  process_id: string,         // Solo's assigned process id
  name: string,               // final name (may be Solo-generated when caller omitted name)
  agent_tool_id: number,
  project_id: string,
  // Solo may include additional descriptive fields; capture them as a
  // pass-through `details?: Record<string, unknown>` so we don't lose data
  // without forcing a schema update on every Solo addition.
}
```

**Files**: `src/solo-client.ts`, `src/types/solo.ts` (extend with `SoloSpawnArgsSchema` + `SoloSpawnResultSchema`)

**Tests** (extend `src/solo-client.test.ts`):
- `spawnProcess` calls `tools/call` with name `spawn_process` and the documented arguments (omits `name`/`project_id` keys when not provided — does not pass `undefined` through to Solo)
- Returns the parsed result on a valid success payload
- Throws `SoloClientError` (with Solo's code/message) when transport returns an MCP error envelope (e.g., Solo rejects name)
- Throws a parse error with field-level message when payload is missing required fields (e.g., `process_id`)

**Notes**:
- Keep the wire-call shape minimal. Solo may evolve to return more fields; absorb unknowns via a permissive zod `passthrough` on optional descriptive properties so we don't fail-closed on additive Solo changes.
- Do not retry on transport-level errors here. Retry policy (if any) is a Step 4 concern.

---

### Task 2 — Spawn fixtures (`src/__fixtures__/spawn-results.ts`)

Centralize spawn-response fixtures used by both client tests and tool tests. Plain TypeScript constants, one per scenario, so a single source of truth feeds every test in this step. Inherit the per-fixture documentation pattern from Step 2 (each export gets a comment block: what it represents, which tests must consume it, and how it differs from neighbors).

**Required fixture sets** — each export accompanied by a block comment with `Purpose:` / `Used by:` / `Disabled-id note:` (the last only when relevant):

- `spawnSuccessNamed` — Solo response when caller supplied `name="my-helper"`. Confirms passthrough.
- `spawnSuccessUnnamed` — Solo response when caller omitted `name`; Solo generated `agent-1234` (or similar). Confirms unnamed-spawn passthrough.
- `spawnSuccessWithProjectId` — Solo response with caller-supplied `project_id`; the fixture's `project_id` field equals the caller's value (precedence assertion).
- `spawnSuccessFromEnvProjectId` — Solo response when caller omitted `project_id` and `SOLO_PROJECT_ID` was used as fallback; fixture's `project_id` equals the env value.
- `spawnRejectionNameInUse` — Solo MCP `error` envelope `{ code: -32602, message: "name 'my-helper' already in use" }`. Drives the name-rejection passthrough test.
- `spawnRejectionInvalidAgentToolId` — Solo `error` envelope when `agent_tool_id` doesn't exist on Solo's side. Should be surfaced verbatim, not swallowed.
- `spawnRejectionPermissionDenied` — Solo `error` envelope for an unauthorized project scope. Verifies project-scope errors surface cleanly.
- `spawnMalformedPayload` — `tools/call` returns text content missing `process_id`. Verifies zod parse error path.

**Files**: `src/__fixtures__/spawn-results.ts`
**Tests**: no direct tests; consumed by Tasks 1, 3.

**Disabled-id note** (Step 2 retro lesson): for any fixture intended to interact with the agent-tools fixture set, document explicitly which `agent_tool_id` it references and whether that id is enabled in `enabledRuntimes` / `mixedRealistic`. This avoids the Step 2 Task 5 confusion about which disabled ids applied to which fixture.

---

### Task 3 — `spawn_agent` MCP tool (`src/tools/spawn-agent.ts`)

The orchestration seam. Composes resolver + project-scope precedence + Solo spawn + structured error mapping.

**Input schema** (`zod`):
```ts
z.object({
  tier: z.string().min(1, "tier is required"),
  name: z.string().min(1).optional(),
  project_id: z.string().min(1).optional(),
}).strict();
```

**Behavior** (order matters):
1. Validate input via the schema (handled by the SDK's `registerTool` wrapper — do not parse manually in the handler).
2. Call `soloClient.listAgentTools()`. On `SoloClientError`, map to MCP error using its code/message. Do not call Solo `spawn_process` if listing failed.
3. Call `resolveAgentTool(tools, input.tier)`. On `UnsupportedTierError` → MCP error code `unsupported_tier` (mirrors `resolve_agent_tool`). On `TierUnavailableError` → MCP error code `tier_unavailable` with diagnostics.
4. Resolve effective `project_id`:
   - if `input.project_id` is present → use it
   - else if `config.solo.projectId` is present (already populated from `SOLO_PROJECT_ID` at startup by `parseConfig`) → use it
   - else → omit `project_id` from the spawn call entirely (Solo applies its session default scope)
5. Call `soloClient.spawnProcess({ kind: "agent", agent_tool_id: resolution.selected.agent_tool_id, name?: input.name, project_id?: effectiveProjectId })`.
6. On Solo success → return the success shape (below).
7. On `SoloClientError` from spawn → map to MCP error with code `spawn_rejected`, message = Solo's message verbatim, plus structured data: `{ solo_code: err.code, requested_tier: input.tier, agent_tool_id, requested_name: input.name?, requested_project_id: effectiveProjectId? }`. **Do not** reattempt with a different name. **Do not** report success.

**Success response shape**:
```ts
interface SpawnAgentResult {
  process_id: string;         // Solo's assigned process id
  name: string;               // final name (Solo-generated if input.name omitted)
  tier: "small" | "medium" | "large";   // resolved tier (echoes input.tier — confirms it was canonical)
  tool: {
    agent_tool_id: number;
    tool_name: string;
    tool_type: string;
    command: string;
    classification_source: "command" | "name_fallback";
  };
  project_id?: string;        // present iff a project_id was actually used (caller or env); omitted when neither was set
}
```

**Error code map** (exhaustive — all paths):

| Source | MCP code | Notes |
|---|---|---|
| schema rejection (missing `tier`) | SDK's default validation error | not our code |
| `UnsupportedTierError` | `unsupported_tier` | message lists supported labels |
| `TierUnavailableError` | `tier_unavailable` | includes resolver diagnostics |
| `SoloClientError` from `listAgentTools` | passthrough Solo's `code` | message verbatim |
| `SoloClientError` from `spawnProcess` (any reason — name, agent_tool_id, permission, transport) | `spawn_rejected` | Solo message verbatim; structured `solo_code` and request echo as `data` |
| zod parse error from `spawnProcess` payload | re-thrown (5xx-equivalent) | indicates Solo contract drift; not normal-path |

**Why one bucket for spawn rejection**: the shipping criteria call out two scenarios (general Solo rejection, name rejection) but treat them the same way — surface the Solo error, never report success, never retry. We don't have a reliable way to distinguish "name rejected" from "agent_tool_id rejected" from "permission denied" without parsing Solo's free-form message strings. Routing them all through `spawn_rejected` with `solo_code` + verbatim message gives the caller everything they need without us inventing taxonomy.

**Files**: `src/tools/spawn-agent.ts`

**Tests** (`src/tools/spawn-agent.test.ts`, fixture-driven, mocked `SoloClient`):
- *Happy path, named*: `tier: "medium"`, `name: "my-helper"` → `listAgentTools` returns `enabledRuntimes`, `spawnProcess` called with `{ kind: "agent", agent_tool_id: <medium id>, name: "my-helper" }` (no `project_id` in args), returns `spawnSuccessNamed`. Result: `process_id`, `name === "my-helper"`, `tier === "medium"`, `tool.agent_tool_id` matches resolved id, no `project_id` field.
- *Happy path, unnamed*: same but `name` omitted. `spawnProcess` called with no `name` key. Result name is the Solo-generated value from `spawnSuccessUnnamed`.
- *Caller project_id wins*: input `project_id: "proj-A"`, env `SOLO_PROJECT_ID="proj-B"`. `spawnProcess` called with `project_id: "proj-A"`. Result `project_id === "proj-A"`.
- *Env project_id fallback*: no input `project_id`, env `SOLO_PROJECT_ID="proj-B"`. `spawnProcess` called with `project_id: "proj-B"`. Result `project_id === "proj-B"`.
- *No project scope anywhere*: no input `project_id`, no env. `spawnProcess` called with no `project_id` key. Result has no `project_id` field. (This is a precedence edge case — verifies we don't synthesize a value or pass `undefined`.)
- *Empty-string project_id rejected by schema*: `project_id: ""` → SDK validation error (schema's `.min(1)`). Asserts the schema, not our handler. (Catches the edge where caller's empty string would otherwise silently pass through to Solo.)
- *Empty-string name rejected by schema*: `name: ""` → SDK validation error.
- *Unknown tier*: `tier: "huge"` → MCP `unsupported_tier`. `spawnProcess` not called.
- *Tier unavailable*: tools list contains only large-tier tools; `tier: "small"` → MCP `tier_unavailable` with diagnostics. `spawnProcess` not called.
- *Solo spawn rejection (name in use)*: happy resolver, `spawnProcess` rejects with `spawnRejectionNameInUse`. Result: MCP `spawn_rejected` with `solo_code: -32602`, message = Solo's verbatim, `data.requested_name === "my-helper"`. **No retry**: `spawnProcess` mock called exactly once.
- *Solo spawn rejection (invalid agent_tool_id)*: same shape, different fixture. Verifies the same code path covers non-name failures.
- *Solo spawn rejection (permission denied)*: project-scope failure surfaces as `spawn_rejected` (not as `tier_unavailable` or any other invented code).
- *Solo `listAgentTools` failure*: client throws `SoloClientError`. Handler maps to MCP error with Solo's code; `spawnProcess` is **not** called (the resolver/spawn pipeline is gated on a successful list).
- *Resolver receives all tools incl. disabled*: assert `listAgentTools` returns the full payload and the resolver's diagnostics reflect `enabled_count`. (Sanity check that we did not pre-filter outside the resolver.)
- *Tool summary echoes resolved tool*: `tool.tool_name`, `tool.tool_type`, `tool.command`, `tool.classification_source` all match the resolver's `selected` / `classification_source` fields.
- *Misleading-name fixture spawn*: when the resolver picks `misleadingNameAccurateCommand`, the spawn call uses its `id`, and `tool.classification_source === "command"`.

**Project_id resolution helper**: pull the precedence logic into a small private helper (`resolveProjectId(input, config) → string | undefined`) so it is independently unit-testable and so its rules are easy to read. Test it directly with the four precedence permutations (caller-only, env-only, both, neither).

---

### Task 4 — Server registration (`src/server.ts`)

Wire the `spawn_agent` tool into `DuoServer`. The handler closes over the same `SoloClient` used by `list_agent_tiers` and `resolve_agent_tool`, plus the `SoloConfig` (needed for the env-project-id fallback inside the handler).

**Files**: `src/server.ts`

**Tests** (extend `src/server.test.ts`):
- `spawn_agent` is registered under that exact name
- Input schema rejects malformed input (missing `tier`, empty-string `name`, empty-string `project_id`)
- Handler receives the same injected `SoloClient` instance as the other two tools
- Handler has access to `config.solo.projectId` for the env fallback (assert via a happy-path test where the config carries a `projectId` and no caller `project_id` is passed → the `spawnProcess` mock receives the config's value)

**Notes**:
- Use SDK's `registerTool` exactly like the existing two tools.
- Do not parse the schema manually inside the handler — let the SDK do it.

---

## Mocks vs. Live Solo Integration

All Step 3 tests are **fixture-based with a mocked `SoloClient`** (extending the Step 2 pattern). No live Solo process is spawned. Justification:

- The Solo MCP wire contract is the same shape we already exercise in Step 1's `solo-client.test.ts` (`tools/call` request, structured-content text result, JSON-RPC error envelope). Re-asserting that contract via mocks is sufficient for unit confidence.
- Live spawn would create real Solo processes, requiring teardown discipline tests don't currently have. That risk doesn't pay back for what's effectively a thin wrapper.
- Solo's actual `spawn_process` payload may differ from the documented assumption in Task 1. The fixtures should be revisited on first hand-validated integration; if drift is found, update fixtures + zod schema in a follow-up commit, not as part of this step.

**Live e2e validation is deferred** to either a Step 5 manual smoke section in the README or to a separate follow-up after Step 4 ships logging (logs make e2e debugging tractable). If a smoke test surfaces during this step (e.g., a builder runs the tool against a local Solo by hand), capture findings in the Step 3 retro for follow-up — but do not block this step on it.

---

## Deferred Decisions Resolved Here

- **`spawn_agent` input shape → `tier`, `name`, `project_id` only**
  Prompt/bootstrap payload is intake-deferred. The MVP tool surface is exactly these three fields, all string, with `tier` required and the other two optional. Source: PRD Open Question 3 (intake-resolved); roadmap Step 3 deferred-decisions list.

- **Caller `project_id` precedence → caller > config (env-derived) > omit**
  When neither caller nor config has a `project_id`, the spawn call omits the field and Solo applies its session default. We do not synthesize a value and we do not error early. Source: roadmap Step 3 shipping criterion 3; intake §S9.

- **Spawn rejection taxonomy → single `spawn_rejected` MCP code**
  Solo's free-form error messages are not classified into invented sub-codes (e.g., we do not branch into `name_in_use` vs `invalid_agent_tool_id`). The MCP error preserves Solo's `code` as `solo_code` and Solo's message verbatim. Source: this workplan, Task 3 — rationale block.

- **No client-side name validation**
  We do not pre-validate `name` against character sets, length policies, or any heuristic. Solo owns name validation. The schema only rejects empty strings (defensive — empty string would silently strip in JSON serialization). The "no hidden retry with different name" criterion is satisfied because we never attempt a second call. Source: roadmap Step 3 shipping criterion 5.

- **Live e2e spawn validation → deferred**
  All Step 3 testing is mock-based. Manual smoke testing against a live Solo is a Step 5 README task (or earlier if a builder offers it). Source: this workplan, "Mocks vs. Live Solo Integration" section.

---

## Edge Cases Worth Pre-Fixtures

These are listed for completeness — most are covered by the fixtures in Task 2 and tests in Task 3.

**Solo spawn rejection scenarios** (Task 2 fixtures, Task 3 tests):
- Name already in use — `spawnRejectionNameInUse`
- Invalid `agent_tool_id` (Solo's perspective — e.g., id was disabled between list and spawn) — `spawnRejectionInvalidAgentToolId`
- Permission denied for the project scope — `spawnRejectionPermissionDenied`
- Malformed Solo response (contract drift) — `spawnMalformedPayload`

**Name validation edge cases** (Task 3 tests, schema-level):
- Empty string → schema rejects (`.min(1)`)
- Caller omits `name` → `spawnProcess` called without a `name` key (not `name: undefined`)
- Caller provides whitespace-only name (e.g., `"   "`) → schema accepts; Solo decides. Documented as Solo's responsibility, not a bug for us to fix.

**Caller `project_id` precedence edge cases** (Task 3 tests):
- Caller-only → caller wins
- Env-only → env used
- Both → caller wins (env never reaches Solo)
- Neither → no `project_id` sent; result lacks the field
- Empty-string caller `project_id` → schema rejects
- Caller `project_id` matches env value → indistinguishable from "caller wins"; both paths yield the same wire call. We don't add a special test for this — it's the conjunction of the caller-wins and env-fallback tests.

**Resolver-spawn ordering** (Task 3 tests):
- Resolver fails → spawn never called (asserted via mock call count)
- `listAgentTools` fails → resolver and spawn both never called
- Spawn fails after successful resolution → no retry, single spawn call

---

## Definition of Done

- [ ] `npm test` (vitest) runs and passes — every fixture/test scenario in Tasks 1, 3 green; existing Step 1 + Step 2 suites unchanged
- [ ] `spawn_agent` MCP tool registered under that exact name, with input schema `{ tier, name?, project_id? }` enforced by the SDK
- [ ] Successful spawn returns `{ process_id, name, tier, tool, project_id? }` with `tool` echoing the resolved tool summary and `classification_source`
- [ ] `spawn_process` is called with `kind="agent"`, the resolved `agent_tool_id`, and `name`/`project_id` only when present (omitted keys, never `undefined`)
- [ ] Caller `project_id` takes precedence over `SOLO_PROJECT_ID`; env fallback works when caller omits; neither → omitted from Solo call
- [ ] Solo MCP errors during spawn surface as MCP `spawn_rejected` with Solo's code and verbatim message; success is never reported on failure
- [ ] Name rejection (Solo error mentioning the name) surfaces as `spawn_rejected`; the spawn call is invoked exactly once (no hidden retry)
- [ ] `unsupported_tier` returned for unknown tier labels; `tier_unavailable` returned when no enabled candidate exists for a valid tier (consistent with `resolve_agent_tool`)
- [ ] `SoloClient.spawnProcess` parses Solo's response shape and validates with zod; malformed payload throws a parse error with field-level message
- [ ] Server tests confirm tool registration, schema enforcement, and shared `SoloClient` injection across all three tools

---

## Suggested Build Batching

| Batch | Tasks | Notes |
|---|---|---|
| Batch A | Task 1, Task 2 | Solo client `spawnProcess` + fixtures; both unblock Task 3 and have no inter-dependency. Run in parallel. |
| Batch B | Task 3 | `spawn_agent` MCP tool; depends on Tasks 1 + 2. Single builder. |
| Batch C | Task 4 | Server registration; depends on Task 3. Single builder. |

Batch A → B → C is a sequential spine with parallelism only inside Batch A.

**Risk note**: Task 3 is the highest-risk item — it owns the project-scope precedence rule, the error-mapping taxonomy, and the "no hidden retry" invariant. Per playbook role policy and Step 2 retro precedent (Task 4 routed to large), route Task 3 to a `large` tier. Tasks 1, 2, 4 are comfortable on `medium`.

**Batch-done shorthand** (Step 2 retro lesson): builders should report batch completion with a one-line summary plus file list rather than re-narrating each task; coordinator can verify with a single `git diff --stat` per batch.

---

## Source-of-Truth References

- Roadmap: `notes/roadmap/roadmap-1.md` lines 62–80 (Step 3 shipping criteria)
- Intake: `notes/proposals/solo-orchestrator-companion-intake.md` lines 141–172 (Step 3 proposed shape)
- PRD: REQ-003, REQ-007, REQ-011 (in `docs/solo-orchestrator-companion-prd.md`)
- Stories: 7, 8, 9, 11 (project-scope half) (in `docs/solo-orchestrator-companion-stories.md`)
- Step 2 retro: `notes/project-planning-workflow-notes.md` lines 202–229 — fresh lessons on fixture documentation and batch-done shorthand
- Reference implementation patterns: `src/tools/resolve-agent-tool.ts` (handler shape, error mapping), `src/tools/list-agent-tiers.ts` (Solo client error rethrow), `src/__fixtures__/agent-tools.ts` (fixture-export style)
