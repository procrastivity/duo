# Step 2 Workplan — Tier Classifier, Resolver, and Fixture Coverage

**Status**: complete  
**Roadmap**: `notes/roadmap/roadmap-1.md`  
**Intake**: `notes/proposals/solo-orchestrator-companion-intake.md`  
**Source coverage**: PRD REQ-001, REQ-002, REQ-004, REQ-005, REQ-006 (with intake-amended random-default reading), REQ-009, §7b invariants; Stories 1, 2, 3, 4, 5, 6, 15

---

## Scope

- **Goal**: Turn a Solo `list_agent_tools` payload into tier resolution. Ship two MCP tools — `list_agent_tiers` (availability + default + alternatives across all three tiers) and `resolve_agent_tool` (single-tier selection with classification source and diagnostics). Cover the classifier and resolver with fixture tests for every observed Solo runtime plus the agreed edge cases.
- **Out of scope**: `spawn_agent` (Step 3), YAML policy overrides and structured logging (Step 4), `round-robin`/`custom` selection strategies (only `random` is implemented now; the abstraction is plumbed but no other strategies ship), live Solo health check (deferred), custom tier labels (deferred), packaging/docs (Step 5).

---

## Tasks

### Task 1 — Update Solo client `listAgentTools` (`src/solo-client.ts`)

The Step 1 implementation calls the generic `tools/list` MCP method and returns a `{ name, description, inputSchema }`-shaped record. Step 2 needs Solo's domain payload: `id`, `name`, `command`, `tool_type`, `enabled`. Switch `listAgentTools` to invoke Solo's `list_agent_tools` MCP tool via `tools/call` and parse the structured content into a typed `SoloAgentTool` shape. Validate the response with `zod` so a malformed payload fails loudly with a field-level error rather than propagating undefined.

**Files**: `src/solo-client.ts`, `src/types/solo.ts` (new — exported `SoloAgentTool` type and zod schema)  
**Tests** (extend `src/solo-client.test.ts`):
- `listAgentTools` calls `tools/call` with name `list_agent_tools` and zero arguments
- Returns the parsed array when mock transport returns a valid Solo payload (all five known runtimes)
- Throws structured `SoloClientError` when transport returns an MCP error
- Throws a parse error with field-level message when payload is missing required fields (e.g. `command`)

**Notes**: keep the `AgentTool` placeholder type defined in Step 1 for backward compatibility only if other code still depends on it; otherwise remove it in favor of `SoloAgentTool`.

---

### Task 2 — Agent tool fixtures (`src/__fixtures__/agent-tools.ts`)

Centralize fixture payloads used by both classifier and resolver tests, plus by the MCP-tool integration tests. Fixtures should be plain TypeScript constants matching the `SoloAgentTool` shape so a single source-of-truth feeds every test in the step.

**Required fixture sets**:
- `enabledRuntimes` — the five observed runtimes, all `enabled: true`:
  - `opencode-ghc-haiku` (small via command token `haiku`)
  - `opencode-ghc-sonnet` (medium via command token `sonnet`)
  - `codex-fast` (small via command token `fast`)
  - `codex-standard` (medium via command token `standard`)
  - `codex-flagship` (large via command token `flagship`)
- `disabledVariants` — same five runtimes with `enabled: false` (asserted to be ignored by the resolver before classification)
- `misleadingNameAccurateCommand` — e.g. tool named `quick-helper` whose command contains `opus`; classification must pick `large` from the command, not `small` from the name
- `accurateNameMisleadingCommand` — e.g. tool named `sonnet-runner` whose command contains no recognized model token; classification should fall back to the name (`medium`) and report `name_fallback`
- `ambiguousCommand` — command contains tokens from two different tiers (e.g. `haiku` and `opus`); classifier reports ambiguous diagnostic
- `unknownCommand` — command contains no recognized token and name has no recognized token; classifier yields no tier with an `unclassifiable` diagnostic
- Optional helper `mixedRealistic` — combination of enabled + disabled + edge cases, used by the `list_agent_tiers` integration test to exercise the full pipeline at once

**Files**: `src/__fixtures__/agent-tools.ts`  
**Tests**: no direct tests; consumed by Tasks 3, 4, 5, 6.

---

### Task 3 — Token classifier (`src/classifier.ts`)

Pure, side-effect-free classifier. Embeds the token policy as TypeScript constants per the resolved deferred decision. Case-insensitive matching. Command tokens classify first; name tokens are consulted only when the command yields no match. Always reports the match source.

**Token policy** (embedded constants):

```
COMMAND_TOKENS:
  small:  ["haiku", "mini", "flash", "fast", "cheap", "small"]
  medium: ["sonnet", "standard", "medium", "default", "gpt-5.2", "gpt-5.3-codex", "gpt-5.4"]
  large:  ["opus", "flagship", "max", "large", "gpt-5.5"]

NAME_TOKENS:
  small:  ["haiku", "mini", "flash", "fast", "cheap", "small"]
  medium: ["sonnet", "standard", "medium", "default"]
  large:  ["opus", "flagship", "pro", "max", "large"]   # "pro" is a weak large-tier signal
```

**API** (illustrative shape — builders may refine):

```ts
type Tier = "small" | "medium" | "large";
type ClassificationSource = "command" | "name_fallback" | "none";

interface Classification {
  tier: Tier | null;
  source: ClassificationSource;
  matchedTokens: string[];          // tokens that produced the tier
  ambiguous: boolean;               // command matched >1 tier
  diagnostics: {
    commandTokensSeen: { tier: Tier; token: string }[];
    nameTokensSeen:    { tier: Tier; token: string }[];
  };
}

function classify(tool: SoloAgentTool): Classification;
```

**Rules**:
- Tokenize `command` and `name` independently. Lowercase both before matching. Token match is substring within whitespace-/punctuation-separated segments (so `gpt-5.2` matches `gpt-5.2` but not `gpt-5.20`).
- If `command` produces matches in exactly one tier → `tier=that`, `source="command"`.
- If `command` produces matches in multiple tiers → `tier=null`, `source="none"`, `ambiguous=true`. **Do not** fall back to name in the ambiguous case (ambiguity is a signal, not a fallback trigger; surfaces as a resolver diagnostic).
- If `command` produces no matches → consult `name`. If `name` matches exactly one tier → `tier=that`, `source="name_fallback"`. If multiple or none → `tier=null`, `source="none"`.
- Always populate `diagnostics` with every token observed across both fields, regardless of which path was taken.

**Files**: `src/classifier.ts`  
**Tests** (`src/classifier.test.ts`):
- Each of the five `enabledRuntimes` classifies to its expected tier with `source="command"`
- `misleadingNameAccurateCommand` classifies via command (not name)
- `accurateNameMisleadingCommand` classifies via name fallback with `source="name_fallback"`
- `ambiguousCommand` returns `tier=null`, `ambiguous=true`, `source="none"` — and **does not** consult name
- `unknownCommand` returns `tier=null`, `source="none"`
- Case-insensitivity verified (e.g. `OPUS`, `Sonnet`)
- `pro` in name maps to `large` only when no other name token matches (weak signal verification)
- Classifier is pure — repeated calls with same input return identical output

---

### Task 4 — Tier resolver (`src/resolver.ts`)

Composes filtering, classification, candidate selection, and diagnostics into the resolution pipeline used by both MCP tools. Throws structured errors that map directly to MCP error codes.

**Pipeline** (order matters — see PRD §7b invariants):
1. Validate `tier` is one of `small | medium | large`. Otherwise throw `UnsupportedTierError`.
2. Drop tools where `enabled !== true` **before** any classification.
3. Drop tools whose `id` is in `excludeIds` (input is plumbed but optional; defaults to `[]`).
4. Classify each remaining tool with `classify()`.
5. Collect candidates whose tier matches the requested tier.
6. If `candidates.length === 0` → throw `TierUnavailableError` with diagnostics: requested tier, total tools, enabled count, excluded count, ambiguous count, unclassifiable count, plus the classification of each ignored tool.
7. Select using the configured strategy. **Default = `random`.** Selected tool is omitted from `alternatives`; alternatives are listed in deterministic order (sort by `id` ascending) so tests can assert on them regardless of the random pick.

**API**:

```ts
type SelectionStrategy = "random";   // round-robin and custom land in Step 4

interface ResolverOptions {
  strategy?: SelectionStrategy;       // default "random"
  excludeIds?: number[];
  rng?: () => number;                 // injectable for deterministic tests; defaults to Math.random
}

interface Resolution {
  selected: {
    agent_tool_id: number;
    tool_name: string;
    tool_type: string;
    command: string;
  };
  classification_source: "command" | "name_fallback";
  matched_tokens: string[];
  alternatives: Array<{
    agent_tool_id: number;
    tool_name: string;
    tool_type: string;
    classification_source: "command" | "name_fallback";
  }>;
  diagnostics: {
    requested_tier: Tier;
    total_tools: number;
    enabled_count: number;
    excluded_count: number;
    ambiguous_count: number;
    unclassifiable_count: number;
    candidates_considered: number;
    strategy: SelectionStrategy;
  };
}

function resolveAgentTool(
  tools: SoloAgentTool[],
  tier: string,
  options?: ResolverOptions,
): Resolution;
```

**Errors** (subclass `Error` with discriminator field for MCP mapping):

- `UnsupportedTierError` — `code: "unsupported_tier"`, includes the offending label and the canonical tier list
- `TierUnavailableError` — `code: "tier_unavailable"`, includes the resolver diagnostics block

**Selection strategy abstraction**: factor `select(candidates, options) -> selected` behind a small internal interface so Step 4 can plug in `round-robin` and `custom` without re-touching the pipeline. Only `random` is implemented in this step.

**Files**: `src/resolver.ts`, `src/errors.ts` (new — error classes shared with MCP tool layer)  
**Tests** (`src/resolver.test.ts`, fixture-driven):
- Each of the five `enabledRuntimes` resolves to its expected tier when that tier is requested
- `disabledVariants` are excluded **before** classification (assert on `diagnostics.enabled_count` and absence in any tier's candidate set)
- `misleadingNameAccurateCommand` resolves on command, `classification_source === "command"`
- `accurateNameMisleadingCommand` resolves via name fallback, `classification_source === "name_fallback"`
- `ambiguousCommand` increments `diagnostics.ambiguous_count`, does **not** appear as a candidate
- `unknownCommand` increments `diagnostics.unclassifiable_count`, does not appear as a candidate
- Unknown tier label (`"giant"`, `""`, `"SMALL"` if case-strict) throws `UnsupportedTierError`
- Empty candidate set (only disabled tools, or no matching tools) throws `TierUnavailableError` with diagnostics
- Multi-candidate scenario with injected seeded `rng` selects a known candidate; `alternatives` includes the rest in `id`-sorted order and excludes the selected
- Multi-candidate scenario across two different `tool_type`s (e.g. `codex-fast` + `opencode-ghc-haiku` for `small`) lists both as candidates regardless of which is selected
- `excludeIds` removes a tool from candidates; if exclusion empties the set, raises `TierUnavailableError`
- Resolver does not mutate its inputs (defensive copy / readonly check)

---

### Task 5 — `list_agent_tiers` MCP tool (`src/tools/list-agent-tiers.ts`)

Aggregates resolution across all three tiers in a single call. Useful for "what's available locally?" introspection without needing to call `resolve_agent_tool` three times. A tier reports `available: false` when its candidate set is empty rather than throwing — this tool is intentionally non-failing across tiers so callers see the full picture.

**Response shape**:

```ts
interface ListAgentTiersResult {
  small:  TierAvailability;
  medium: TierAvailability;
  large:  TierAvailability;
}

interface TierAvailability {
  available: boolean;
  default?: {                       // present iff available
    agent_tool_id: number;
    tool_name: string;
    tool_type: string;
    command: string;
    classification_source: "command" | "name_fallback";
  };
  alternatives: Array<{
    agent_tool_id: number;
    tool_name: string;
    tool_type: string;
    classification_source: "command" | "name_fallback";
  }>;
  diagnostics: ResolverDiagnostics; // same shape as resolver diagnostics, minus selection details when unavailable
}
```

**Behavior**:
- Calls `SoloClient.listAgentTools` once and reuses the payload across all three resolutions.
- For each tier, runs the resolver in `random` mode (same selection rule as `resolve_agent_tool`).
- If the resolver throws `TierUnavailableError` for a tier, that tier is rendered as `{ available: false, alternatives: [], diagnostics: {...} }`. Other tier errors should not occur here (tier labels are hard-coded).
- Catches `SoloClientError` from the underlying call and re-throws as a structured MCP error.

**Files**: `src/tools/list-agent-tiers.ts`  
**Tests** (`src/tools/list-agent-tiers.test.ts`):
- All three tiers `available: true` with `mixedRealistic` fixture
- A tier with no candidates (e.g. omit all small-tier tools) reports `available: false` with diagnostics
- Disabled tools never appear in any tier's `alternatives`
- Solo client error propagates as a structured MCP error
- Calling once invokes `listAgentTools` exactly once (cached across the three tier resolutions within a single call)

---

### Task 6 — `resolve_agent_tool` MCP tool (`src/tools/resolve-agent-tool.ts`)

Single-tier resolution surface. Input is a `tier` label; output is a Resolution mirroring Task 4's shape, with errors mapped to MCP error responses.

**Input schema** (`zod`):
```ts
z.object({
  tier: z.string().min(1, "tier is required"),
  // exclude_ids and strategy intentionally omitted from MVP input surface;
  // resolver supports them but tool input does not expose them yet (Step 4)
}).strict();
```

**Behavior**:
- Calls `SoloClient.listAgentTools`.
- Calls `resolveAgentTool(tools, input.tier)`.
- On success, returns the Resolution shape.
- On `UnsupportedTierError` → MCP error code `unsupported_tier`, message lists supported tier labels.
- On `TierUnavailableError` → MCP error code `tier_unavailable`, message includes a one-line summary plus the diagnostics block as structured data.
- On `SoloClientError` → MCP error with code from the underlying error.

**Files**: `src/tools/resolve-agent-tool.ts`  
**Tests** (`src/tools/resolve-agent-tool.test.ts`):
- Happy path: `tier: "medium"` against `enabledRuntimes` returns one of `{codex-standard, opencode-ghc-sonnet}` with both as candidates (selected + alternatives)
- `tier: "purple"` returns `unsupported_tier` MCP error
- `tier: "small"` against a tool list with only large-tier tools returns `tier_unavailable` with diagnostics
- Misleading-name fixture: `classification_source === "command"`, `matched_tokens` includes the model token
- Accurate-name-misleading-command fixture: `classification_source === "name_fallback"`
- Disabled-variant fixture: `tier_unavailable` if all matching tools are disabled, with `diagnostics.enabled_count` reflecting the drop

---

### Task 7 — Server registration (`src/server.ts`)

Wire both MCP tools into the `McpServer` instance. The Solo client must be constructed once at server start and injected into each tool's handler so a single `SoloClient` is shared across all tool calls (matches the Step 1 lifecycle).

**Files**: `src/server.ts`  
**Tests** (extend `src/server.test.ts`):
- `list_agent_tiers` and `resolve_agent_tool` are registered under those exact names
- Each tool's input schema rejects malformed input (e.g. `resolve_agent_tool` with no `tier`)
- Tool handlers receive the same `SoloClient` instance (constructor-inject a mock client into the server for this assertion)

**Notes**:
- Tool registration uses the SDK's `registerTool` / equivalent; do not bypass the SDK's input validation by parsing in the handler.
- Keep the `SoloClient` injectable via the `DuoServer` constructor so tests don't need a live transport.

---

## Deferred Decisions Resolved Here

- **Default selection strategy → `random`**  
  Multiple candidates matching a tier are chosen from uniformly at random. Alternatives are always listed (in `id`-ascending order so tests can assert on them). `round-robin` and `custom` are plumbed behind a strategy interface but only `random` ships in Step 2.  
  Source: PRD Open Question 2 (intake-resolved); roadmap Step 2 deferred-decisions list.

- **No implicit tier fallback**  
  When a tier has no enabled candidates, the resolver throws `TierUnavailableError` instead of degrading to a lower tier. Any fallback behavior is opt-in through Step 4 policy.  
  Source: PRD §7b invariants; PRD Open Question 5 (intake-resolved).

- **Default classifier rules → embedded TypeScript constants**  
  The token policy in Task 3 lives as TS constants. YAML overrides arrive in Step 4.  
  Source: PRD Open Question 6 (intake-resolved).

- **Tier labels fixed to `small | medium | large` for v0**  
  Custom tier labels are deferred. Unknown labels surface as `unsupported_tier`.  
  Source: PRD Open Question 1 (intake-resolved).

- **Ambiguity is not a fallback trigger**  
  When `command` matches multiple tiers, classification stops with `tier=null` and `ambiguous=true`. The classifier does not consult `name` to break the tie. Ambiguity is reported as a resolver diagnostic, not silently resolved.  
  Source: agent-tool-selection.md §"Failure Behavior"; this workplan, Task 3.

---

## Definition of Done

- [x] `npm test` (vitest) runs and passes — every fixture-test scenario in Tasks 3, 4, 5, 6 green
- [x] `list_agent_tiers` returns availability + default + alternatives for all three tiers
- [x] `resolve_agent_tool` returns selected `agent_tool_id`, classification source, alternatives, and diagnostics
- [x] Disabled tools are filtered before classification (verified by `diagnostics.enabled_count` and absence from candidate sets)
- [x] Command-token classification precedes name-fallback; match source is always reported
- [x] Multiple-candidate selection is random; alternatives always listed in deterministic order
- [x] `unsupported_tier` returned for unknown tier labels
- [x] `tier_unavailable` returned when no enabled candidate exists for a valid tier
- [x] Fixture tests cover the five enabled runtimes, their disabled variants, both name/command misleading combinations, ambiguous, and unknown — each with the documented assertion
- [x] `SoloClient.listAgentTools` parses the real Solo response shape (`id`, `name`, `command`, `tool_type`, `enabled`) and validates with `zod`

---

## Suggested Build Batching

| Batch | Tasks | Notes |
|---|---|---|
| Batch A | Task 1, Task 2 | Solo client shape update + fixtures; both unblock everything else and have no inter-dependency |
| Batch B | Task 3 | Classifier; depends on the `SoloAgentTool` type from Task 1 and consumes fixtures from Task 2 |
| Batch C | Task 4 | Resolver; depends on Task 3 (classifier) and Task 2 (fixtures) |
| Batch D | Task 5, Task 6 | Both MCP tools wrap the resolver; can be built in parallel once Batch C lands |
| Batch E | Task 7 | Server wiring; gate on D so both tools exist before registration |

Batch A can run in parallel with two builders. Batches B → C → D → E are a sequential spine; only Batch D is internally parallel.

**Risk note**: Task 4 (resolver) is the highest-risk item — it carries the PRD §7b invariants and is the seam where classifier output meets MCP error semantics. If a single builder is available end-to-end, route Task 4 to a `large` tier per playbook role policy; the rest are comfortable on `medium`.
