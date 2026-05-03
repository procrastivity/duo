# User Story & Acceptance Criteria Guide

Reference for writing the user-stories peer artifact that ships alongside a spec. Read this before generating any user stories.

**Where stories live:** `notes/proposals/<slug>-stories.md` while the proposal is in flight; archived alongside the proposal (or promoted to `docs/specs/<feature>-stories.md` if the spec gets promoted with stories attached). Stories are NOT a section of the spec. The spec defines behavior; the stories define delivery scope.

**Cross-references:** Each stories file's first paragraph references its spec by relative path. The spec's Implementation Notes section can point back to the stories file.

---

## User Story Fundamentals

### Format

```
As a [specific persona/role], I want to [goal/action] so that [value/benefit].
```

All three parts are mandatory. The "so that" clause is the most important — it explains WHY, which lets the team find better solutions and make trade-off decisions.

For `solo-local-cli` features, the persona collapses to "the user" by default, since there's only one. Use "As the user" rather than inventing personas. If multi-tenancy is in scope (per the active scope profile or an accepted JIT trigger), see `concerns/multi-tenancy.md` for multi-persona patterns.

### The Three C's

User stories are defined by Card, Conversation, and Confirmation:

1. **Card** — The story itself. Brief enough to fit on an index card. It's a promise for a conversation, not a complete specification.
2. **Conversation** — The discussion between the spec author and the implementer that fleshes out the details. The story is an invitation to this conversation.
3. **Confirmation** — The acceptance criteria that define when the story is done.

### What Makes a Good Story

**DO:**
- Use specific personas where they exist. For multi-user features, "As a warehouse manager" not "As a user." For solo features, "As the user" is fine.
- Focus on one goal per story. If you see "AND" in the goal, split it.
- State the goal without prescribing the solution. "I want to find relevant reports quickly" not "I want a search bar with autocomplete in the top nav."
- Make the benefit concrete and distinct from the goal. "so that I can decide which note to read first" not "so that I can find notes" (which just restates the goal).
- Keep language clear and jargon-free.

**DON'T:**
- Use vague adjectives. "Modern experience" — what does that mean? "Fast" — how fast?
- Write stories from the system's perspective. "As the system, I want to send an email" — systems don't want things.
- Include implementation details. "As the user, I want a CLI flag that POSTs to the daemon" — that's a task, not a story.
- Write stories you can't validate. If the persona wouldn't recognize the goal as something they care about, rewrite.

---

## INVEST Criteria

Every story should be evaluated against INVEST before being considered ready for implementation:

### I — Independent
The story can be developed without depending on other stories being completed first. If two stories are tightly coupled, consider combining them or restructuring.

**Test:** Can this story be re-ordered in the workplan without breaking anything?

### N — Negotiable
The story is a starting point for conversation, not a rigid contract. The goal and value are fixed, but the details of implementation should be open for discussion with the implementer.

**Test:** Is the story describing WHAT and WHY, leaving HOW open?

### V — Valuable
The story delivers tangible value to a user (or, for solo-local-cli, to the user themselves). Technical tasks ("refactor the database schema") are not user stories — they may be necessary work items, but frame them as enablers, not stories.

**Test:** Would the persona recognize this as something worth building?

### E — Estimable
There's enough information to estimate the effort. If not, the story needs refinement or a spike.

**Test:** Could the implementer give a rough size estimate without asking a dozen clarifying questions?

### S — Small
The story can be completed in one workplan task or a tight sequence. Stories that span multiple workplan tasks are too large — use SPIDR to split them.

### T — Testable
The story has clear acceptance criteria that allow verification of completion. If you can't write a test for it, you can't confirm it's done.

**Test:** Can someone write a pass/fail test for every acceptance criterion?

---

## Acceptance Criteria

Acceptance criteria define "what done looks like" for a specific story. They are NOT the Definition of Done (which applies to all stories and covers process quality like code review, testing, etc.).

### Format Options

Choose the format that best fits the story's complexity:

#### 1. Checklist Format
Best for: simple stories, straightforward requirements.

```
**Story:** As the user, I want to filter search results by file type so that I can focus on the kind of note I'm looking for.

**Acceptance Criteria:**
- [ ] `--type md` flag restricts results to files with `.md` extension
- [ ] `--type md,txt` accepts a comma-separated list and matches any
- [ ] Without `--type`, all file types are included (current behavior preserved)
- [ ] Invalid extension (`--type "*.exe"`) returns a clear error message before issuing the search
```

#### 2. Given/When/Then (Scenario-Based)
Best for: complex behavior, multiple scenarios, stories with important edge cases.

```
**Story:** As the user, I want stale change events to be detected and dropped so that the indexer doesn't process events for files that have since been deleted.

**Acceptance Criteria:**

Scenario: Event for a file that still exists
  Given the watcher emits a Modified event for `notes/today.md`
  When the indexer processes the event
  Then the file is reindexed and the change is recorded in the outbox

Scenario: Event for a file that was deleted between event emission and processing
  Given the watcher emits a Modified event for `notes/today.md`
  And `notes/today.md` is deleted before the indexer reaches it
  When the indexer processes the event
  Then the indexer logs a stale-event drop and emits no outbox record
```

#### 3. Rule-Based
Best for: business logic, validation rules, invariants.

```
**Story:** As the user, I want my outbox events to be durable across crashes so that consumers don't miss changes.

**Rules:**
- Every event is fsynced to the outbox file before the watcher's debouncer commits the next batch
- A crash mid-batch does not produce a partially-written event (events are append-only with a length prefix)
- On startup, the outbox reader skips any trailing partial event from a prior crash
- The outbox file's content_hash for each event matches the file's content_hash at the time of the event
```

### Writing Good Acceptance Criteria

**DO:**
- Make each criterion independently testable with a clear pass/fail.
- Be specific: "Page loads in under 2 seconds" not "Page loads quickly."
- Cover the happy path AND edge cases (errors, empty states, boundary conditions).
- Include negative criteria where relevant: "Without `--type`, all file types are included."

**DON'T:**
- Restate the story as a criterion. The story says "I want to filter by type"; don't write "User can filter by type" as an AC.
- Include implementation details. "Uses the `globset` crate" — that's an engineering decision.
- Write criteria so broad they're untestable. "The feature is intuitive."

---

## Observability — criteria must be checkable from outside the database

A criterion is only useful if it can be proven by something a user (or an end-to-end test) can directly observe: an HTTP request/response, a CLI output, an event written to the outbox file, a row visible to the next consumer, an integration test that hits a real endpoint and inspects a real response.

If the only proof of a criterion is "a row exists in table X with column Y set," the criterion is satisfied at the wrong layer. The implementer can write code that creates the row while the user-visible behavior is silently broken — and the criterion will still pass.

**Bad (DB-layer):**
- `- [ ] When a file is modified, an entry is added to the outbox table with content_hash set.`

**Good (observable):**
- `- [ ] When a file is modified, the outbox file (queryable via the outbox reader) contains a Modified event with the file's path and content_hash; a consumer reading from the outbox sees the event before the next batch.`

The first can pass while the consumer sees nothing. The second cannot.

When you write a criterion, ask: "what does an external observer SEE when this is true?" That sentence is the criterion.

---

## Discriminating ACs — would the criterion still pass if the function returned a constant?

Universal smoke check for any AC: *would this still pass if the function under test returned a constant, a trivial value, or an overly-inclusive result?* If yes, the criterion is tautological and will not catch the bug it claims to prevent.

### Failure shape 1 — response-shape ACs that check structure, not values

**Bad:** `- [ ] The response includes a results array with paths.` (`assertJsonStructure(['results' => ['*' => ['path']]])` passes even if `path` is always the empty string.)

**Good:** `- [ ] When searching for "pgvector" in a vault containing exactly one file at "notes/databases/pgvector.md", the response is `{results: [{path: "notes/databases/pgvector.md", size: 4821, mtime: "2026-04-22T14:31:08Z", content_hash: "sha256:abc123…"}], truncated: false}`.`

### Failure shape 2 — selection / cutoff predicates keyed on non-authoritative fields

**Bad:** `- [ ] Files where `mtime <= cutoff.mtime` are returned.` (Two files with identical mtimes both match; the boundary cannot distinguish "before" from "same instant as.")

**Good:** `- [ ] Files where `(mtime, path) <= (cutoff.mtime, cutoff.path)` are returned.` (Authoritative, collision-free using path as the tiebreaker.) When specifying cutoffs across related ACs, use the same key style — mixing mtime-based and path-based boundaries for the same logical cutoff is silent drift.

### Failure shape 3 — guardrails where the fixture sets both sides

**Bad (tautological):** `- [ ] The test creates a file with content_hash="abc", indexes it, asserts the outbox event has content_hash="abc".` (The fixture sets both sides; the indexer could be returning a constant and the test would still pass.)

**Good (constructed oracle):** `- [ ] The test creates a file with arbitrary content; the test computes the expected content_hash by calling the same hashing function the indexer uses; the test fails if the outbox event's content_hash differs from the test's computed value.`

Smoke check before accepting any guardrail AC: *if the function under test returned a constant, would the criterion still pass?* If yes, the criterion is tautological.

---

## Boundary-Graph Check — enumerate every hop

If the story's logical work crosses module / spec / process boundaries (e.g., watcher → indexer → outbox → consumer), the AC set must enumerate **every hop the value crosses** with a type guarantee at each. Untyped passthroughs in unnamed hops are where half-wirings survive.

**Example:** A story for "the consumer sees a Modified event with the new content_hash":

- Hop 1: filesystem `notify` event → watcher's `RawEvent` (typed as `notify::Event`)
- Hop 2: watcher's `RawEvent` → debouncer's coalesced `ChangeEvent` (typed as `ChangeEvent::Modified { path, prior_hash, new_hash }`)
- Hop 3: indexer reads the file → computes content_hash → updates store
- Hop 4: indexer writes to the outbox → outbox event has `content_hash` field (typed as `String`, must match the new_hash from hop 2)
- Hop 5: consumer reads the outbox → sees the event with the same content_hash (round-trip preserves the value)

If any hop's type is `String` where it should be `Sha256Hash`, or `Option<String>` where the value is always present, name it explicitly in the story or its ACs.

---

## Negative-Fingerprint Check

For every anti-pattern the spec wants to prevent, write a grep that returns zero matches when the implementation is correct. Put it in the story's ACs (or in the spec's Implementation Notes if it applies cross-story).

**Example:** "After this story, `rg 'fn process_event\(.*?prior_hash: Option<' src/` returns zero matches" — if `prior_hash` should always be present on Delete events, the optional version of the parameter shouldn't survive in the codebase.

Positive-only greps cannot catch survivors in files the story didn't touch. Negative fingerprints catch them.

---

## Don't Prescribe Preserving a Known Anti-Pattern

ACs that explicitly preserve the defect class the spec exists to eliminate — e.g. "the path field continues to use untyped String — do not introduce narrowing as part of this cleanup" — bake the bug back in. If a boundary needs tightening, tighten it. If the tightening is genuinely out of scope, list it in the spec's Open Questions section with a reason; do not anchor it in an AC.

---

## Adversarial Criteria for Multi-User Features

If the active scope profile is `solo-local-cli` (no tenancy by definition), adversarial criteria do not apply.

For all other profiles, **if the feature touches per-user state, ownership-scoped resources, or any data that should be invisible to other users**, see `concerns/multi-tenancy.md` for the adversarial-AC patterns. The discipline is non-negotiable when in scope: cross-tenant denial criteria, ownership-keyed validation, attacker-model reasoning.

---

## Docs References — Symbol, Not Line

When a story's output is a doc, write references as `path` plus class / function / constant name — stable across refactors. Line numbers in prose rot on the next unrelated edit. Reserve line numbers for research citations and for machine-checked grep fingerprints.

---

## Organizing Stories

Group stories under epics if there are more than 5-6 stories per spec. An epic is a body of work that maps to a user journey or capability area within the feature.

```
## Epic: Watch loop wiring

### Story 1: Watcher emits Modified events for vault writes
...

### Story 2: Watcher debouncer coalesces editor save bursts
...
```

For a small spec (1-4 stories total), skip epics — just list stories.

---

## SPIDR: Splitting Large Stories

When a story is too large for a single workplan task, apply one of these five techniques (Mike Cohn):

### S — Spikes
If the story is large because of unknowns, separate the research from the implementation. Time-box a spike to learn what you need; then write implementation stories based on what you discover.

### P — Paths
If the persona can accomplish the goal through multiple paths, split by path.

### I — Interfaces
Split by surface (CLI vs HTTP vs MCP, desktop vs mobile, library vs binary).

### D — Data
Split by restricting the data scope initially, then expanding (e.g., "supports `.md` files only" first, then "supports all text files").

### R — Rules
Split by simplifying or deferring business rules.

### When to Split

Apply SPIDR when:
- The story would take more than one workplan task to complete.
- The story has more than 8-10 acceptance criteria.
- You can see multiple distinct scenarios or paths within one story.

---

## Story Quality Checklist

Before considering a story ready for the workplan, verify:

- [ ] Follows "As a / I want / So that" format with all three parts
- [ ] Persona is specific (or "the user" for solo-local-cli)
- [ ] Goal is a single action (no "AND")
- [ ] Benefit is concrete and distinct from the goal
- [ ] No implementation details in the story or acceptance criteria
- [ ] Passes INVEST evaluation
- [ ] Has 3-8 acceptance criteria (if more, consider splitting)
- [ ] Acceptance criteria are observable (not DB-layer)
- [ ] Each AC is discriminating (would not pass with a constant)
- [ ] Boundary-graph hops named for cross-module values
- [ ] Negative-fingerprint greps included where anti-patterns matter
- [ ] Edge cases and error states are considered
- [ ] If multi-user concerns apply (per active profile or accepted JIT trigger), adversarial criteria from `concerns/multi-tenancy.md` are applied

---

## Common Anti-Patterns

Watch for and correct these:

1. **The Epic Disguised as a Story:** "As the user, I want a complete watch loop." This is an epic — break it down.
2. **The Technical Task as a Story:** "As a developer, I want to refactor the indexer module." Not a user story. Frame it as an enabler if it's load-bearing prep work.
3. **The Solution-Prescribing Story:** "As the user, I want a CLI flag with a comma-separated list." What's the actual goal? Probably "I want to filter results by file type."
4. **The Tautological Benefit:** "As the user, I want to search so that I can search for things." The benefit must explain WHY the goal matters, not restate it.
5. **The Kitchen Sink Story:** Multiple goals chained with AND. Split.
6. **The Untestable Story:** "As the user, I want the CLI to feel responsive." How do you test "feel"? Rewrite with measurable criteria ("CLI returns first result within 100ms of stdin EOF").
7. **The Missing Persona:** "As a user..." For multi-user features, WHICH user? For solo-local-cli, "the user" is fine.
8. **The Implementation-Disguised AC:** "Uses tokio::sync::watch::channel for backpressure" — that's an implementation choice, not an AC. The AC is what the consumer observes when backpressure is correctly applied.
