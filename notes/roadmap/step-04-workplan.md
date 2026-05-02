# Step 4 Workplan — YAML Policy Overrides and Structured Logging

**Status**: planned
**Roadmap**: `notes/roadmap/roadmap-1.md`
**Intake**: `notes/proposals/solo-orchestrator-companion-intake.md`
**Source coverage**: PRD REQ-009 (override-source diagnostics extension), REQ-010, REQ-012; Stories 12, 13

---

## Scope

- **Goal**: Add a local YAML *policy* layer that can extend or replace the built-in command-token patterns per tier and define a `custom` selection-preference ordering. Add structured operational logs (pino, stderr) for resolution success/failure and spawn success, with an explicit allow-list of fields per event so prompts/task content cannot leak. Surface `built_in` vs `override` provenance in resolver diagnostics so operators can audit which rule matched.
- **Out of scope**: custom *tier labels* (Step 2 decision: fixed `small`/`medium`/`large`); name-token overrides (built-in name fallback only — overriding name tokens broadens the loadbearing-name surface and was not asked for); selection modes beyond `custom` and `random` (round-robin remains intake-noted but unscoped); spawn-failure logs (Story 13 ACs cover only the three event types listed); log levels beyond a single `info` channel; log sampling, rotation, or shipping; live observability (REQ-013 health check is P2/deferred); packaging and docs (Step 5).

---

## Tasks

### Task 1 — Policy schema, loader, and validation (`src/policy.ts`, `src/types/policy.ts`)

Defines the YAML *policy* — a separate file from `duo.config.yaml` (the connection config). Policy is optional; absence means "built-ins only, random selection." Presence with malformed content fails startup (mirrors `parseConfig`'s startup-validation pattern).

**Schema (zod)** — strict, field-level errors:

```ts
// src/types/policy.ts

const TokenListSchema = z.array(z.string().min(1)).default([]);

const TierTokenOverrideSchema = z
  .object({
    // "extend" (default): merge override tokens with built-in tokens, dedup case-insensitively.
    // "replace": override tokens become the entire token set for this tier; built-ins discarded.
    mode: z.enum(["extend", "replace"]).default("extend"),
    tokens: TokenListSchema,
  })
  .strict();

const CommandTokenOverridesSchema = z
  .object({
    small: TierTokenOverrideSchema.optional(),
    medium: TierTokenOverrideSchema.optional(),
    large: TierTokenOverrideSchema.optional(),
  })
  .strict();

const PreferenceSelectorSchema = z
  .object({
    // At least one of tool_type / tool_name must be present (refine).
    tool_type: z.string().min(1).optional(),
    tool_name: z.string().min(1).optional(),
  })
  .strict()
  .refine(
    (s) => s.tool_type !== undefined || s.tool_name !== undefined,
    { message: "selector must specify tool_type and/or tool_name" },
  );

const SelectionPolicySchema = z
  .object({
    // When `preference` is present, strategy becomes "custom"; otherwise it remains "random".
    // First selector that matches a candidate wins; ties within the matched bucket fall back
    // to the existing random RNG. Unmatched candidates remain eligible at the end of the list
    // and are also resolved via random tiebreak. Order matters.
    preference: z.array(PreferenceSelectorSchema).min(1).optional(),
  })
  .strict();

export const PolicySchema = z
  .object({
    command_tokens: CommandTokenOverridesSchema.optional(),
    selection: SelectionPolicySchema.optional(),
  })
  .strict();

export type Policy = z.infer<typeof PolicySchema>;
```

**Override semantics** (the central decision — fixture-tested in Tasks 2/3):

- *Per-tier* merge: `extend` (default) = built-in ∪ override (case-insensitive dedup, override order preserved after built-ins); `replace` = override only. Tiers not mentioned in the policy fall through to built-ins entirely (no implicit replace).
- An empty `tokens: []` with `mode: replace` is *legal* and means "this tier has no command-token rules" — the tier becomes name-fallback-only. We don't warn; the operator asked for it. It will surface in `tier_unavailable` diagnostics if no tools resolve.
- An empty `tokens: []` with `mode: extend` is a no-op. We accept it without warning to avoid noisy validation on partial configs.
- Name-token policy is *not* exposed in v0. Name fallback uses the built-in `NAME_TOKENS` constant. (Documented as deferred decision below.)

**Selection-preference semantics**:

- `selection.preference` is an ordered list. For each candidate, find the *first* selector that matches it; assign that selector's index as the candidate's preference rank. Lower rank wins. Candidates matching no selector are ranked `Infinity`.
- A selector matches a candidate when *all* present fields match (`tool_type === selector.tool_type` AND `tool_name === selector.tool_name`). Missing fields on the selector are wildcards (matched against everything). At least one field must be present (enforced by `.refine`).
- Within a rank bucket, RNG-based tiebreak runs (preserves Step 2's `random` default behavior locally). This means `custom` is *layered on top of* `random`, not a replacement: when no preference applies, behavior is identical to current Step 2 resolver.
- When `selection.preference` is absent, strategy stays `"random"` exactly as today.

**Loader**:

```ts
// src/policy.ts
export const loadPolicy = (
  source: unknown,        // already-parsed YAML object (caller reads file)
): Policy => { /* zod parse + field-level error formatter */ };
```

The loader does *not* read the file itself. The caller (Task 6 / `index.ts`) handles file IO and YAML parsing — same separation `parseConfig` already follows. This keeps the loader pure and trivially unit-testable.

**Field-level error format**: reuse the `formatZodError` pattern from `src/config.ts:37-46`. First issue's `path.join(".")` + message. Examples:
- `command_tokens.medium.mode: Invalid enum value. Expected 'extend' | 'replace', received 'merge'`
- `selection.preference.0: selector must specify tool_type and/or tool_name`
- `command_tokens.small.tokens.0: String must contain at least 1 character(s)`

**Files**: `src/types/policy.ts` (schemas + types), `src/policy.ts` (`loadPolicy`, error formatter)

**Tests** (`src/policy.test.ts`):
- *Empty input* (`undefined`, `{}`): returns `{}` (no overrides, no preferences). Both forms allowed.
- *Valid extend policy*: `command_tokens.large = { mode: "extend", tokens: ["pro"] }` parses; result reflects mode + tokens.
- *Valid replace policy*: same with `mode: "replace"`.
- *Mode defaults to `extend`* when omitted: `{ tokens: ["pro"] }` parses, `mode === "extend"`.
- *Empty tokens array* legal in both modes (intentional — see semantics).
- *Valid selection preference*: `preference: [{ tool_type: "codex" }]` parses.
- *Selector with both fields*: `{ tool_type: "codex", tool_name: "codex-flagship" }` parses.
- *Invalid: empty selector* `{}` → error mentions `selection.preference.0` with refine message.
- *Invalid: unknown tier label* in `command_tokens` (e.g., `huge`) → error path `command_tokens` (strict object rejects unknown keys).
- *Invalid: unknown top-level key* (`logging: {}`) → strict-object rejection.
- *Invalid: empty token string* `tokens: [""]` → error path `command_tokens.<tier>.tokens.0`.
- *Invalid: bad mode* (`mode: "merge"`) → error path `command_tokens.<tier>.mode`.
- *Invalid: empty preference array* `preference: []` → `.min(1)` rejection.
- *Field-level message shape*: snapshot the formatted error string for one case so future zod upgrades don't silently change UX.

**Notes**:
- Schema uses `.strict()` on every object so typos in keys fail closed (Step 1 retro lesson on config).
- The token list is *not* lowercased at parse time; the classifier already lowercases comparisons. Storing tokens as authored makes round-tripping logs/diagnostics clearer.

---

### Task 2 — Classifier accepts external token policy and emits source diagnostics (`src/classifier.ts`)

Today the classifier reads `COMMAND_TOKENS` and `NAME_TOKENS` as module-level constants. To support overrides we (a) make command-token policy injectable, and (b) tag each token with its provenance so the resolver can report `built_in` vs `override`.

**API change**:

```ts
export type TokenSource = "built_in" | "override";

export interface ClassifierTokenPolicy {
  command: Readonly<Record<Tier, ReadonlyArray<{ token: string; source: TokenSource }>>>;
  name: Readonly<Record<Tier, readonly string[]>>;  // unchanged for v0, no source tagging needed
}

export const buildClassifierPolicy = (
  policy: Policy,
): ClassifierTokenPolicy => { /* applies extend/replace per tier */ };

export const classify = (
  tool: SoloAgentTool,
  policy?: ClassifierTokenPolicy,    // optional; defaults to built-ins-only
): Classification;
```

`Classification` extends with one new field:

```ts
export interface Classification {
  // ... existing fields
  matchSource: TokenSource;  // "built_in" | "override" — only meaningful when source ≠ "none"
}
```

**Why a discriminated source per token (not per tier)**: when `mode: "extend"` mixes built-in and override tokens for the same tier, we still want to report whether the *winning token* was an override. Per-token tagging is the smallest representation that supports that.

**Built-in policy bootstrap**: a function `defaultPolicy(): ClassifierTokenPolicy` produces the original `COMMAND_TOKENS`/`NAME_TOKENS` data with every command token tagged `built_in`. This is what the resolver passes when no overrides are configured.

**Merge logic** (`buildClassifierPolicy`):
- For each tier:
  - If override absent → use built-in tokens, all tagged `built_in`.
  - If `mode: "replace"` → use override tokens only, all tagged `override`.
  - If `mode: "extend"` → built-in tokens (tagged `built_in`) followed by override tokens (tagged `override`); dedup case-insensitively, the *first* occurrence wins (so a token shared by built-in and override is reported as `built_in`). This matters for diagnostics: an operator who lists a built-in token in their override file should not be surprised to see `built_in` reported — it's an accurate description of "this token would have matched anyway."

**Files**: `src/classifier.ts` (extend types + signature, add `buildClassifierPolicy`, add `defaultPolicy`).

**Tests** (extend `src/classifier.test.ts`):
- *No policy passed* → existing behavior unchanged; `matchSource === "built_in"` for all hits.
- *Extend mode adds new token*: tier `large` gains `pro`; tool with `command: "runner --pro"` classifies large with `matchSource === "override"`.
- *Replace mode wipes built-ins*: tier `small` replaced with `["tiny"]`; tool whose command contains `haiku` no longer classifies as small (falls through to name fallback if name has a small token; otherwise unclassifiable).
- *Override token shadowed by built-in*: extend mode adds `haiku` (already built-in); winning token reports `matchSource === "built_in"` (dedup keeps built-in).
- *Override-only tier hit*: replace `medium` with `["bespoke-mid"]`; matching tool reports `matchSource === "override"`.
- *Mixed extend with command containing both built-in and override tokens for same tier*: tool command `--haiku --tiny` (where `tiny` is the override) classifies small; matched tokens include both; `matchSource` reports the source of the *first* matched token in iteration order — assert the documented rule explicitly so a future iteration order change is a deliberate test break.
- *Source defaulting*: when `source === "none"` (no match), `matchSource` field is `built_in` by convention (it's irrelevant; assert the chosen default so consumers don't read it as `override` accidentally).

**Notes**:
- The existing `escapeRegex` + word-boundary scan in `findTokens` handles regex-special tokens. We keep that — operators authoring overrides like `gpt-5.5` still work.
- We do not re-order built-in token arrays. The resolver test for token-iteration order is a *characterization* test, not an architectural commitment; if a future change shuffles iteration order, the test should be updated *deliberately*.

---

### Task 3 — Resolver custom selection strategy and override-source surfacing (`src/resolver.ts`, `src/errors.ts`)

The resolver gains a `custom` strategy and propagates classifier provenance into both the selected-resolution shape and the unavailability diagnostics.

**Type changes**:

```ts
// src/resolver.ts
export type SelectionStrategy = "random" | "custom";

export interface ResolverOptions {
  strategy?: SelectionStrategy;
  excludeIds?: number[];
  rng?: () => number;
  preference?: PreferenceSelector[];   // required when strategy === "custom"
  classifierPolicy?: ClassifierTokenPolicy;  // forwarded to classify()
}

export interface ResolutionSelected {
  // ... existing fields
  token_source: TokenSource;     // "built_in" | "override"
  matched_tokens: { token: string; source: TokenSource }[];  // upgraded from string[]
}

export interface ResolutionAlternative {
  // ... existing fields
  token_source: TokenSource;
}
```

**Diagnostics changes** (`src/errors.ts`):

```ts
export interface ResolverDiagnostics {
  // ... existing fields
  strategy: "random" | "custom";   // was: literal "random"
  override_token_count: number;    // tokens that came from override policy across this resolution
  preference_applied: boolean;     // true when strategy === "custom" and at least one selector matched a candidate
}

export interface IgnoredToolDiagnostic {
  // ... existing fields
  match_source?: TokenSource;      // when reason === "wrong_tier" — useful for "the override matched, but wrong tier"
}
```

**Custom selection algorithm**:
1. Compute per-candidate rank: `rank = preference.findIndex((sel) => matches(sel, candidate))`; if `-1`, rank = `Infinity`.
2. Sort candidates ascending by rank.
3. Within the lowest-rank bucket, pick by `rng` (existing `randomStrategy.select`).
4. Set `diagnostics.preference_applied = true` iff *any* candidate had a finite rank.

**Why preserve random tiebreak inside the rank bucket**: the intake's "three modes" framing treats `custom` as preference-ordered selection. If two candidates tie at the top rank (e.g., both `tool_type: "codex"`, no `tool_name` field on the selector), the operator did not express a tiebreaker, so we keep the existing `random` default behavior rather than inventing an implicit one. This is consistent with the Step 2 decision to relax determinism for zero-config UX.

**`alternatives` ordering under `custom`**: alternatives sort by `rank` ascending, then by `agent_tool_id` ascending (stable secondary). Today the resolver sorts alternatives by `agent_tool_id`; under `custom` the rank-first order is more useful to the operator. Tests pin this.

**Files**: `src/resolver.ts`, `src/errors.ts`, and the resolver passes `classifierPolicy` into each `classify(tool, classifierPolicy)` call.

**Tests** (extend `src/resolver.test.ts`):
- *Custom strategy with preference*: two candidates (codex + opencode), preference `[{ tool_type: "codex" }]` → codex selected deterministically, no RNG call needed for the top bucket.
- *Custom strategy, no preference match*: two candidates, preference `[{ tool_type: "nonexistent" }]` → behaves like random; `diagnostics.preference_applied === false`.
- *Custom strategy, partial preference match*: three candidates, preference matches one → that one selected; alternatives include the other two ranked `Infinity`.
- *Custom with `tool_name`-only selector*: selects by name, ignores `tool_type`.
- *Custom with both `tool_type` and `tool_name`* (AND semantics): only the candidate matching both wins.
- *Custom strategy without `preference` option*: throws (caller misuse — the option pair should be enforced). Or: defaults to random behavior with `preference_applied: false`. **Decision**: throw a configuration error (`InvalidResolverOptions`) at strategy-selection time, surfaced as a 5xx-equivalent — this is a programmer error, not a runtime data error. Document in workplan.
- *`token_source` on selected*: built-in token match → `"built_in"`; override token match → `"override"`.
- *`matched_tokens` shape*: array of `{ token, source }` objects. Existing tests expecting `string[]` updated.
- *`override_token_count` in diagnostics*: count tokens where `source === "override"` across the *selected* candidate's `matchedTokens`. (Not across all candidates — that would be confusing and hard to interpret.)
- *`tier_unavailable` diagnostics include `override_token_count`* and `preference_applied: false`.
- *Ignored tool with override match but wrong tier*: a tool whose override token matched a different tier appears in `ignored_tools` with `match_source: "override"`.

**Notes**:
- Existing fixture tests in `agent-tools.ts` continue to pass with no policy: `classifierPolicy` defaults to `defaultPolicy()` (all `built_in`).
- The `excludeIds` set still applies *before* preference ranking — exclusion is a hard filter, preference is a soft sort. Pin this with a test.

---

### Task 4 — Logger module (`src/logger.ts`)

**Logger choice: `pino`** writing to **stderr**.

Why pino:
- Roadmap explicitly lists it as the candidate (`structured logging library (e.g. pino)`).
- Single-binary-friendly, fast enough that we don't have to think about hot-path overhead.
- Native JSON output; trivial to assert in tests via a destination override.
- Has `child()` for binding per-request context (useful when we add request ids later, out of scope here).

Why not the alternative ("custom JSON logger"): the only thing we'd save is the dependency. We'd reimplement timestamp formatting, level handling, and serialization safety. Pino is ~20kb gzipped and stable.

Why **stderr**: the MCP stdio transport reserves stdout for protocol messages. Logging to stdout corrupts the wire (Step 1 invariant). All log destinations must be stderr or file paths the operator chooses. We default to stderr; do not expose a `destination` option in v0.

**Module shape** (allow-list-driven — this is the linchpin of Story 13's "no prompts/free-form content" criterion):

```ts
// src/logger.ts
import pino from "pino";

export interface ResolutionSuccessLog {
  event: "resolution.success";
  requested_tier: "small" | "medium" | "large";
  selected_tool_id: number;
  selected_tool_name: string;
  match_source: "command" | "name_fallback";
  candidate_count: number;
  // Step 4 additions beyond Story 13 minimum (cheap, useful):
  token_source: "built_in" | "override";
  strategy: "random" | "custom";
  preference_applied: boolean;
}

export interface ResolutionFailureLog {
  event: "resolution.failure";
  requested_tier: string;          // raw input — may not be a valid tier label
  error_code: "unsupported_tier" | "tier_unavailable" | string;  // string for forward-compat (Solo errors)
  available_tiers: ("small" | "medium" | "large")[];
}

export interface SpawnSuccessLog {
  event: "spawn.success";
  requested_tier: "small" | "medium" | "large";
  selected_tool_id: number;
  solo_process_id: string;
  process_name: string;            // Solo's returned final name
}

export interface Logger {
  resolutionSuccess(fields: Omit<ResolutionSuccessLog, "event">): void;
  resolutionFailure(fields: Omit<ResolutionFailureLog, "event">): void;
  spawnSuccess(fields: Omit<SpawnSuccessLog, "event">): void;
}

export const createLogger = (
  destination?: pino.DestinationStream,   // for tests; default = pino.destination(2) — stderr
): Logger => { /* wraps pino; each method passes ONLY its declared fields */ };
```

**Allow-list discipline**: each helper destructures and re-emits *only* the fields named in its log type. The methods do not accept a `Record<string, unknown>` overflow bag. Callers cannot accidentally log a free-form `task` or `prompt` field because the call sites don't have arguments named that way.

**Sensitive-field invariant** (Story 13 AC: "logs do not include full prompts or free-form task content"):
- `spawn_agent` input includes `name`, but `name` is a process-identifier, not free-form task content — it's safe and useful. We log Solo's *returned* `process_name`, not the caller's input `name`, because (a) Solo may have transformed/normalized it and (b) logging the returned value is a positive confirmation that Solo accepted the value we sent.
- We do not log `project_id` in v0 even though it's not free-form. Reason: project ids may carry tenant/customer identifiers in some installations; conservative omission until a real use case demands it. Documented as deferred decision below.
- We do not log `requested_name` (the caller's pre-Solo input) for symmetry with the above.

**Initialization**: pino with default options except:
- `level: "info"` — single channel, no debug spam in v0.
- `timestamp: pino.stdTimeFunctions.isoTime` — readable in stderr without jq.
- `formatters: { level: (label) => ({ level: label }) }` — emit `level: "info"` instead of numeric.
- `destination` defaults to `pino.destination(2)` (stderr file descriptor).

**Files**: `src/logger.ts`, `src/types/logger.ts` (or co-located in logger.ts — small enough).

**Tests** (`src/logger.test.ts`):
- *Resolution success log shape*: capture into a `Writable` destination; parse the line; assert fields exactly match the allow-list (no extras, no missing).
- *Resolution failure log shape*: same, with both `unsupported_tier` and `tier_unavailable` cases.
- *Spawn success log shape*: same.
- *No prompt/task field leakage*: even if a caller passes extra properties (TypeScript would reject this; we use a runtime cast in the test), they should not appear in output. This requires the helper to use explicit destructuring, not spread.
- *Stderr by default*: when no destination supplied, the logger's underlying stream targets fd 2. (Assert via the pino destination's metadata or by writing to a tmp pipe and confirming default path; the simplest viable assertion is to confirm `process.stdout` is not written to during a log call.)
- *Timestamp present and ISO 8601*: regex check.
- *Level field is `"info"`*: not a number.

---

### Task 5 — Tool handlers emit structured logs (`src/tools/resolve-agent-tool.ts`, `src/tools/spawn-agent.ts`)

Wire the logger into the two tools that have Story 13 events. **`list_agent_tiers` is intentionally not logged** — Story 13 ACs cover only resolution and spawn, and tier-listing is broad/repeated enough that logging it produces noise without adding diagnostic value. (Documented as deferred decision below.)

**`resolve_agent_tool` handler** changes:
- Constructor signature receives a `Logger`.
- After successful `resolveAgentTool`, call `logger.resolutionSuccess({ requested_tier, selected_tool_id: resolution.selected.agent_tool_id, selected_tool_name: resolution.selected.tool_name, match_source: resolution.classification_source, candidate_count: resolution.diagnostics.candidates_considered, token_source: resolution.selected.token_source, strategy: resolution.diagnostics.strategy, preference_applied: resolution.diagnostics.preference_applied })`.
- On `UnsupportedTierError`: `logger.resolutionFailure({ requested_tier: input.tier, error_code: "unsupported_tier", available_tiers: TIER_LABELS })`.
- On `TierUnavailableError`: `logger.resolutionFailure({ requested_tier: input.tier, error_code: "tier_unavailable", available_tiers: TIER_LABELS })`.
- On `SoloClientError` from `listAgentTools`: `logger.resolutionFailure({ requested_tier: input.tier, error_code: String(err.code), available_tiers: TIER_LABELS })`. (Forward-compat: error_code is the Solo code as a string.)

**`spawn_agent` handler** changes:
- Constructor signature receives a `Logger`.
- On success: `logger.spawnSuccess({ requested_tier: tier, selected_tool_id: resolution.selected.agent_tool_id, solo_process_id: spawnResult.process_id, process_name: spawnResult.name })`.
- On any failure path: **do not log a spawn-failure event** in v0 (Story 13 ACs do not require it; resolution-failure paths are still logged because the resolver fired; Solo-rejection paths exit before any log is emitted by the spawn handler). Documented below.
- The handler also logs a `resolutionSuccess` event before calling `spawnProcess`, because the resolver did succeed — operators need that breadcrumb when a spawn fails. **This is the only "double-event" path in v0**, and it is intentional.

**Files**: `src/tools/resolve-agent-tool.ts`, `src/tools/spawn-agent.ts`.

**Tests** (extend `src/tools/resolve-agent-tool.test.ts`, `src/tools/spawn-agent.test.ts`; both are fixture-based with mock `SoloClient`):
- Inject a fake `Logger` recorder; assert *exact* call shapes (function called once, with the expected fields object).
- *resolve_agent_tool happy path* → one `resolutionSuccess` call with all expected fields.
- *resolve_agent_tool unsupported tier* → one `resolutionFailure` call with `error_code: "unsupported_tier"`.
- *resolve_agent_tool tier unavailable* → one `resolutionFailure` call with `error_code: "tier_unavailable"`.
- *resolve_agent_tool listAgentTools fails* → one `resolutionFailure` call; `error_code` is the Solo code (assert it's the `String(...)` of the fixture's code).
- *spawn_agent happy path* → one `resolutionSuccess` call followed by one `spawnSuccess` call (assert order).
- *spawn_agent resolver fails* → one `resolutionFailure` call, no `spawnSuccess`.
- *spawn_agent Solo rejects* → one `resolutionSuccess` call (resolver succeeded), then *no further log calls* (we are deferring spawn-failure logs to a future step). Assert recorder length === 1.
- *No `requested_name`, `requested_project_id`, or `prompt` in any log call* — sweep the recorder's collected calls and assert these keys are absent.

**Notes**:
- The fake `Logger` recorder is the contract test for the allow-list. The pino-output-shape test (Task 4) is the contract test for *what gets serialized*. Together they prove: handlers pass the right fields, and the logger emits exactly those fields.

---

### Task 6 — Server/index integration: load policy, inject logger (`src/server.ts`, `src/index.ts`, `src/config.ts`)

Wire policy loading and logger construction into startup. Both are optional inputs: missing policy = built-ins; logger always exists (default stderr).

**Policy file location**: separate from `duo.config.yaml`. Two acceptable shapes:
1. `DUO_POLICY` env var pointing at a YAML file (parallel to `DUO_CONFIG`).
2. A `policy: { ... }` block embedded in `duo.config.yaml` itself.

**Decision**: support **option 1 only** in v0 (separate `DUO_POLICY` file with default path `duo.policy.yaml` in cwd). Reasons:
- Keeps connection config (which may contain credentials/paths) and policy (which the operator iterates on freely) separate concerns.
- A second YAML file is the path of least surprise — it parallels `.eslintrc.yaml` / `.prettierrc.yaml` ergonomics.
- We can layer "embed in config" later without breaking anyone.

If `DUO_POLICY` is set and the file does not exist → **fail startup** (operator explicitly requested it). If `DUO_POLICY` is unset and `duo.policy.yaml` does not exist → silent no-op (built-ins). If the file exists but is malformed YAML or fails schema → fail startup with the field-level error from Task 1.

**`SoloConfig` extension** (`src/config.ts`):

```ts
soloConfigSchema = z.object({
  solo: z.object({
    transport: soloStdioTransportSchema,
    processId: z.string().min(1).optional(),
    projectId: z.string().min(1).optional(),
  }).strict(),
  // Optional, populated by index.ts after a separate policy file load.
  // Schema-typed here so tests can pass a config object directly without going through index.ts.
  policy: PolicySchema.optional(),
}).strict();
```

This avoids `DuoServer` taking three separate constructor args (config, policy, logger) — config carries policy, and the only new constructor argument is the logger.

**`DuoServer` constructor**:

```ts
constructor(
  config: SoloConfig,
  soloClient?: SoloClient,
  logger?: Logger,                 // default: createLogger() — stderr
) { ... }
```

Inside `start()`, build the `ClassifierTokenPolicy` from `config.policy` once and pass it (plus `config.policy?.selection?.preference`) into both `resolve_agent_tool` and `spawn_agent` handler closures.

**`src/index.ts`** changes:
- After reading `DUO_CONFIG`, also attempt to read `DUO_POLICY` (default `duo.policy.yaml`).
- Parse with `loadPolicy`; on error, write to stderr and `process.exit(1)`.
- Merge the parsed policy into the config object before calling `createServer`.

**Files**: `src/server.ts`, `src/index.ts`, `src/config.ts`, and the two tool handlers from Task 5 take a `Logger` argument from the server.

**Tests** (extend `src/server.test.ts`, `src/config.test.ts`):
- *Config without policy block*: parses; `config.policy` is undefined; resolver receives default classifier policy.
- *Config with valid policy block*: parses; resolver receives override-aware classifier policy; selection preference forwarded.
- *Config with invalid policy block*: `parseConfig` throws field-level error.
- *Server constructs default logger* when none injected; spies on logger methods are not possible in this case but a lightweight test verifies `new DuoServer(config)` does not throw when the logger is omitted.
- *Server forwards injected logger* to both tool handlers (assert via fake logger + happy-path call into each handler).
- *Server forwards classifier policy to both tools*: a fixture tool whose command matches an override-only token classifies under override and resolves via the spawn path.
- *Server preference is applied*: with two candidates and a preference favoring one, `resolve_agent_tool` selects the preferred one.

**Notes**:
- We do not add `SOLO_PROJECT_ID` or `SOLO_PROCESS_ID` semantics here — Step 3 already wired those. The policy layer is independent.
- The split between "config has a `.policy` field after merge" vs "policy is its own arg" is a deliberate ergonomic choice — keeps tests writable in one config object.

---

## Mocks vs. Live Considerations

Step 4 is purely additive — no Solo wire calls change. All tests remain fixture-based with the existing mocked `SoloClient`. The new test surfaces are:

- **YAML/zod parsing** (Task 1) — pure unit tests against literal objects; YAML *string* parsing is exercised once via integration in Task 6's `index.ts` startup test, but the `loadPolicy` API itself takes already-parsed objects so we don't have to assert YAML-library behavior.
- **Logger output shape** (Task 4) — pino destination override into a `Writable` buffer; line-by-line JSON parse and field assertions.
- **Logger call shape** (Task 5) — fake `Logger` recorder with one method per event type; assert call counts and exact field objects.

No live Solo connection required; no live filesystem watcher required.

---

## Deferred Decisions Resolved Here

- **Override merge mode → per-tier `mode: "extend" | "replace"`, default `"extend"`**
  Built-ins ∪ overrides per tier under extend; override-only under replace. Tiers not mentioned fall through entirely. Empty `tokens: []` legal in both modes (intentional). Source: this workplan, Task 1.

- **Selection preference → `selection.preference`, ordered list of `{ tool_type?, tool_name? }` selectors, AND-semantics within a selector, first-match wins, RNG tiebreak within rank bucket**
  Maps to `strategy: "custom"` automatically when `preference` is present; missing preference keeps strategy `"random"`. Source: roadmap Step 4 criterion 2; intake §"Open Questions" (selection-mode resolution).

- **Logger choice → `pino` to stderr**
  Default destination is `pino.destination(2)`. No stdout writes (would corrupt MCP transport). No file/sink configuration in v0. Source: roadmap Step 4 deps note (`structured logging lib (e.g. pino)`); MCP stdio invariant from Step 1.

- **Log event surface → three events, allow-list per event**
  `resolution.success`, `resolution.failure`, `spawn.success`. No `spawn.failure` in v0. No `list_agent_tiers` event. Each event's helper accepts only its declared fields (no overflow bag). Source: Story 13 ACs; this workplan, Task 4.

- **Sensitive-field policy → omit `prompt`, `task`, `project_id`, `requested_name` from all logs**
  `process_name` (Solo's *returned* value) is logged. Caller's input `name` is not. `project_id` is omitted as a conservative default; revisit if operators ask for it. Source: Story 13 AC ("logs do not include full prompts or free-form task content"); this workplan, Task 4.

- **Override-source diagnostics → per-token `built_in | override` tag; resolver surfaces selected token-source and an `override_token_count` summary**
  Rationale: shipping criterion 4 ("Resolver diagnostics identify override vs built-in rule matches"). Per-token tagging is the smallest representation that works under `mode: extend` mixing. Source: this workplan, Task 2.

- **Policy file location → separate `duo.policy.yaml` (path overridable via `DUO_POLICY`)**
  Not embedded in `duo.config.yaml`. Missing default path is silently treated as "no overrides"; explicit `DUO_POLICY` env pointing at a missing file is a startup error. Source: this workplan, Task 6.

- **Custom tier labels → still out of scope**
  Step 2's fixed `small`/`medium`/`large` decision stands. Policy schema strictly rejects unknown tier keys. Source: roadmap Step 2 deferred-decisions list.

- **Name-token overrides → out of scope in v0**
  Only command-token overrides are exposed. Reason: name fallback is intentionally a weak signal; broadening its surface area dilutes the "command-first" invariant (PRD §7b). Source: this workplan, Task 1.

- **Spawn-failure logs and `list_agent_tiers` logs → deferred**
  Not in Story 13 ACs. Safer to add later when we know what fields are useful (Solo error code shape may evolve). Source: this workplan, Task 5.

---

## Edge Cases Worth Pre-Fixtures

These extend the existing `agent-tools.ts` fixture set and motivate new policy fixtures.

**Policy parsing fixtures** (Task 1 tests, literal objects):
- `validExtendOnly` — only `command_tokens.large.tokens = ["pro"]` set, `mode: "extend"`.
- `validReplaceOnly` — `command_tokens.small.mode = "replace"`, `tokens = ["tiny"]`.
- `validMixed` — extend on one tier, replace on another, untouched third.
- `validPreferenceCodexFirst` — `selection.preference = [{ tool_type: "codex" }]`.
- `validPreferenceMultiSelector` — two selectors, first by `tool_type`, second by `tool_name`.
- `validPreferenceWithModeOverrides` — combines preference with command_tokens overrides.
- `invalidUnknownTier`, `invalidEmptySelector`, `invalidBadMode`, `invalidEmptyTokenString`, `invalidUnknownTopLevelKey`, `invalidEmptyPreferenceArray` — one fixture per error path; each test asserts the field-level error path string.

**Classifier override fixtures** (extend `agent-tools.ts`):
- `customRunnerProToken` — tool with `command: "runner --pro --opt"`, name unrelated; only matches when `pro` is added as a `large` override token.
- `replaceTierClassifyMiss` — combine `replace` mode emptying small with a tool whose only signal is a built-in small token: must classify as unclassifiable, not small.
- `extendShadowsBuiltIn` — override list re-includes `haiku`; deduplication test material.

**Resolver custom-strategy fixtures** (extend `agent-tools.ts`):
- Reuse `enabledRuntimes` (codex + opencode mix) — sufficient material for `tool_type`-based preference tests.
- Add a fixture pair where two candidates share `tool_type` so `tool_name` selectors are needed (e.g., `codex-fast` + `codex-flagship` both medium under some override config).

**Logger fixtures** (Task 4 / 5 tests):
- Snapshot a single resolution-success line and a single spawn-success line for shape regression. Snapshots include only the field set; timestamps are normalized in the snapshot serializer.

**Disabled-id note** (Step 2 retro lesson): for any new fixture that pairs with an existing fixture set, document explicitly which `agent_tool_id`s the tests assume and whether each is in `enabledRuntimes` / `disabledVariants`. Especially relevant for the override-shadow fixture where dedup behavior depends on what's already enabled.

---

## Definition of Done

- [ ] `npm test` (vitest) green — every new and pre-existing test passes; existing Step 1–3 suites unchanged in behavior
- [ ] `loadPolicy` parses every documented valid policy shape and rejects every documented invalid shape with a field-level error string anchored at the offending path
- [ ] Classifier accepts `ClassifierTokenPolicy`; built-ins-only mode produces identical output to pre-Step-4 behavior; extend mode merges + dedupes; replace mode wipes built-ins per tier
- [ ] `Classification.matchSource` reports `built_in` vs `override` for every command-token hit
- [ ] Resolver supports `strategy: "custom"` with ordered preference selectors; AND-semantics within a selector; RNG tiebreak within rank bucket
- [ ] `resolve_agent_tool` and `spawn_agent` results expose `token_source` on selected and per-alternative; `diagnostics.override_token_count` and `diagnostics.preference_applied` populated
- [ ] `tier_unavailable` diagnostics include `override_token_count` and `preference_applied`
- [ ] Pino logger writes to stderr by default; never to stdout; emits `level: "info"` and ISO timestamp
- [ ] Three event types only: `resolution.success`, `resolution.failure`, `spawn.success`; each enforced by an explicit allow-list per helper
- [ ] No log line contains `prompt`, `task`, `project_id`, or `requested_name` keys (sweep test)
- [ ] `resolve_agent_tool` emits one `resolution.{success|failure}` per call; `spawn_agent` emits `resolution.success` then `spawn.success` on happy path; `resolution.failure` only on resolver failure; no log on Solo spawn rejection
- [ ] `DuoServer` constructor accepts an optional `Logger`; `index.ts` constructs a default stderr logger when none is configured
- [ ] `DUO_POLICY` env var points at a YAML file; default `duo.policy.yaml`; explicit-but-missing fails startup; default-and-missing is silent no-op
- [ ] `SoloConfig.policy` is optional and `PolicySchema`-typed; tests can construct configs with embedded policy without touching `index.ts`

---

## Suggested Build Batching

| Batch | Tasks | Notes |
|---|---|---|
| Batch A | Task 1, Task 4 | Policy schema + logger module — both are leaf modules with no inter-dependency. Run in parallel. |
| Batch B | Task 2, Task 3 | Classifier override-awareness + resolver custom strategy. Task 3 depends on Task 2 (classifier exposes `ClassifierTokenPolicy` + `matchSource`); run sequentially within the batch, or parallelize after Task 2's exported types land. |
| Batch C | Task 5 | Tool handlers consume the Logger from Task 4 and the resolver/classifier outputs from Tasks 2/3. Single builder. |
| Batch D | Task 6 | Server/index integration; depends on all prior. Single builder. |

Batch A → B → C → D is the spine; A's two tasks parallelize, B's two are near-parallel after Task 2's types are exported.

**Risk note**: Task 3 (resolver custom strategy + diagnostics surface change) is the highest-risk — it touches the resolver invariants, changes the public shape of `Resolution.matched_tokens` (string → object), and must keep all existing fixture tests green. Per Step 2/3 retro precedent (resolver/spawn-handler escalation worked well), route Task 3 to `large`. Tasks 1, 2, 4, 5, 6 are comfortable on `medium`. (Task 4 has tricky ergonomics around pino destination capture in tests but no semantic risk.)

**Batch-done shorthand** (Step 2/3 retro lesson): builders report batch completion with a one-line summary plus file list rather than re-narrating each task; coordinator verifies with `git diff --stat`.

**Watch-for** during build:
- Pino's `pino.destination(2)` interaction with vitest's stdio capture — may need `sync: true` in test config or a memory destination override to avoid flake (Task 4).
- The `matched_tokens` shape change (string[] → `{ token, source }[]`) is a *visible* contract change in tool results. Step 5 docs will need to reflect it. No Step 1–3 caller of `resolve_agent_tool` parses `matched_tokens`, but downstream playbooks may. Acceptable for v0 since we're pre-1.0; document in Step 5 README diff.

---

## Source-of-Truth References

- Roadmap: `notes/roadmap/roadmap-1.md` lines 84–99 (Step 4 shipping criteria)
- Intake: `notes/proposals/solo-orchestrator-companion-intake.md` lines 175–204 (Step 4 proposed shape)
- PRD: REQ-009 (in `docs/solo-orchestrator-companion-prd.md` line 189), REQ-010 (line 192), REQ-012 (line 198)
- Stories: 12, 13 (in `docs/solo-orchestrator-companion-stories.md` lines 156–176)
- Step 2 retro: `notes/project-planning-workflow-notes.md` lines 202–229 — fixture-disabled-id documentation pattern, batch-done shorthand
- Step 3 retro: `notes/project-planning-workflow-notes.md` lines 231–263 — large-tier escalation for risk-bearing tasks; fixture-based mock SoloClient as the unit-test contract surface
- Reference implementation patterns:
  - `src/classifier.ts` — token-iteration order, `escapeRegex` + word-boundary regex (preserve under override)
  - `src/resolver.ts` — strategy plug-in shape (extend with `custom`)
  - `src/errors.ts` — diagnostics typing (extend with `override_token_count`, `preference_applied`, `match_source` on ignored)
  - `src/config.ts:37-46` — `formatZodError` field-level error pattern (mirror in `loadPolicy`)
  - `src/tools/resolve-agent-tool.ts` — handler shape; minimal change beyond logger injection
  - `src/tools/spawn-agent.ts` — error mapping pattern; preserves; gains logger calls only
  - `src/index.ts` — config loading at startup; mirror for policy
