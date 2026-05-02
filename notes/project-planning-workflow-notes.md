# Project-Planning Workflow Notes

**Project**: Duo
**Started**: <YYYY-MM-DD>
**Purpose**: Working description of the planning/execution process. Append-mostly. Revise when practice proves better defaults.
**Status**: draft

---

## Usage Policy

- Treat this as execution/process history, not product spec canon.
- Prefer "the project" over a proper project name in templated sections.
- Keep entries concrete (dates, decisions, outcomes, follow-ups).
- Append new retros rather than rewriting old conclusions.

## Directory Assumptions (relative to `notes/`)

- `roadmap/`
- `roadmap/archive/`
- `proposals/`
- `proposals/archive/`
- `backlog.md`
- `project-planning-workflow-notes.md` (this file)

---

## Three Phases

### Phase A.0 — Proposal Intake (before roadmap drafting)

Normalize planning inputs into a roadmap-ready shape. Inputs can be any mix of:

- idea notes
- proposal docs
- PRDs
- user stories
- specs
- architecture notes

This phase is format-agnostic. It should not depend on any single generator schema.

Required outputs:

- Intake artifact using `notes/proposals/intake-output-template.md`.
- Proposed step breakdown for a round.
- Candidate goals and shipping criteria per step.
- Deferred decisions list (what is still unknown and where it should be resolved).
- Story/requirement coverage map linking source inputs to proposed steps.
- Explicit unresolved blockers that require human decisions.
- Recommendation: start step N now, or refine inputs first.

### Proposal Intake Checklist

Use this checklist when turning planning inputs into roadmap/workplan artifacts:

1. Gather and list all planning inputs with file paths.
2. Extract candidate requirements/outcomes from each input.
3. Group outcomes into step-sized increments that can ship independently.
4. Draft per-step goals and shipping criteria.
5. Identify deferred decisions and assign each to a target step.
6. Build a coverage map:
   - each requirement/story should map to a step or be explicitly deferred
   - each planned step should map back to at least one source input
7. Write an intake artifact under `notes/proposals/` using `intake-output-template.md`.
8. Surface unresolved blockers and the smallest set of human decisions needed.
9. Decide:
   - proceed to `roadmap-N.md` + next `step-NN-workplan.md`, or
   - pause for input refinement.

### Phase A — Roadmap

Short scannable plan for a round.

Per step include:

- Goal
- Shipping criteria
- Deferred decisions
- New dependencies
- Risk

### Phase B — Workplan (per step, just in time)

Concrete task list for the next step only.

Per step include:

- Ordered mergeable tasks
- Files/modules touched
- Test strategy
- Relevant docs/spec references
- Definition-of-done checklist

### Phase C — Build

Implementation against the current step workplan with human review at defined boundaries.

---

## Step Boundary Ritual

When a step ships:

1. Mark step status in the active roadmap.
2. Capture hardened design decisions (spec update and/or ADR as needed).
3. Update roadmap if reality drifted.
4. Append step retro in this file.
5. Update `CHANGELOG.md` at round shipping gate.
6. Prepare next step workplan.
7. Get human review before coding next step.
8. Push `HEAD` and any new tag(s) to `origin` when the round closes (or per your team policy for intermediate steps).

---

## Open Questions

1. Workplan granularity defaults.
2. Mid-step roadmap revision timing.
3. Retry/escalation thresholds.
4. What signals indicate process-context drift?

---

## Retro Template

Use this for each shipped step.

```markdown
#### Step N (shipped YYYY-MM-DD)

**Structured Eval**

*Batching outcomes:*
- Batch [tasks A, B]: <outcome>. Assessment: <notes>.
- Solo task M: scope signal <files touched, comment size>. Adjacent overlap: <none/some>. Assessment: <notes>.

*Escalations:*
- Count: N.
- By type: ambiguity=X, test-failure=Y, scope-question=Z, other=V.
- Per escalation: <todo id> — <type> — preventable with better workplan? <yes/no/notes>.

*Retries:*
- Tasks with retries: <task numbers>.
- Per task: <task M> — <N retries> — <failure type>.
- 2-retry ceiling hit without success: <tasks or "none">.

*Time and overhead:*
- Total wall-clock: <hh:mm>.
- Per-task wall-clock: <task M = mm:ss, ...>.
- Coordinator wake-up count: <N>.
- Context drift symptoms: <notes or "none observed">.

**Notes**

(What worked, what did not, what to adjust for next step.)
```

---

## Round Close-Out Template

```markdown
### Round N Retro (closed YYYY-MM-DD)

**Round scope**:

**Delivery summary**:

- Milestones shipped:
- Escalations/retries totals:
- Major risks retired:

**Patterns that held**:

1.
2.
3.

**Surprises**:

1.
2.

**Carry-forward actions**:

1.
2.
3.

**End of round N.**
```

---

## Retrospectives

(append one section per shipped step below)

---

### Step 2 Retro — 2026-05-02

**Duration**: ~16 minutes elapsed (7 tasks, 5 batches, 4 commits)

**What worked**:
- Batching strategy (A → B → C → D parallel → E) executed flawlessly; no blockers.
- Parallel builders (Batch D: tasks 5, 6) reduced task 7 wait time with no idle builders.
- Fixture-first approach (Task 2) unified testing across classifier, resolver, and MCP tools; single source of truth prevented test divergence.
- Large-tier escalation for Task 4 (resolver) paid off — no rework needed on complex PRD §7b pipeline.
- Timer-based orchestration (idle detection) eliminated polling; builders waited cleanly for prior batches.
- SDK's registerTool + input validation pattern worked well; no manual schema parsing bugs.

**What didn't**:
- Task 5 test initially failed due to confusion over which disabled IDs applied to mixedRealistic fixture; required one iteration to fix. Fixture documentation could be more explicit about per-set disabled vs. enabled ids.
- Solo timer model added mild context overhead during batch transitions; might benefit from a "batch done" shorthand in future steps.

**Metrics**:
- Tests: 103 passing (9 files)
- Commits: 4 (implementation, roadmap, archive cleanup, gitignore)
- Builders spawned: 7 (1 × Task 1, 1 × Task 2, 1 × Task 3, 2 × Task 4, 2 × Tasks 5–6)
- Tier assignments: small (1), medium (5), large (2) per playbook policy

**Next step readiness**:
- Step 3 (spawn_agent) can proceed; MCP surface stable and tested.
- Playbook follow-up docs added for Solo timer patterns and mcp-cli anti-pattern.
- Archive structure in place; step-02-workplan ready for archival after Step Boundary.

---

### Step 3 Retro — 2026-05-02

**Duration**: ~45 minutes elapsed (4 tasks, 3 batches, 2 commits)

**What worked**:
- Batch parallelism (A: Tasks 1–2 parallel) with large-tier escalation for Task 3 (spawn_agent logic) eliminated rework; spawned as opus from the start and completed without iteration.
- Fixture-based testing with mocked SoloClient (spawn-results.ts) provided a comprehensive contract test surface for all error paths, precedence permutations, and edge cases.
- Project_id precedence logic (caller > config > omit) implemented cleanly via helper function; independent unit testing verified all 4 cases.
- Solo error taxonomy decision (single spawn_rejected code with solo_code + verbatim message) simplified handler without sacrificing diagnostic clarity; all error sources (name, agent_tool_id, permission) routed uniformly.
- Step 2 retro lessons applied: explicit fixture/disabled-id documentation, batch-done shorthand in builder outcomes, todo sync at batch boundaries (no state drift).

**What didn't**:
- No issues; build proceeded smoothly across all 4 tasks with no escalations or retries.

**Metrics**:
- Tests: 134 passing (10 files, +31 new tests from Step 3)
- Commits: 2 (implementation + workplan archive)
- Builders spawned: 4 (1 × Task 1, 1 × Task 2, 1 × Task 3 large-tier, 1 × Task 4)
- Tier assignments: small (0), medium (3), large (1) per playbook policy
- Batch outcome: Batch A (T1/T2 parallel) → Batch B (T3 large) → Batch C (T4)

**Shipping criteria verified**:
- [x] spawn_agent with tier + optional name calls Solo spawn_process(kind="agent", agent_tool_id=N)
- [x] Response includes Solo process_id, final name, selected tier, and tool summary
- [x] Caller project_id takes precedence; SOLO_PROJECT_ID is default scope fallback
- [x] Solo rejection returns structured error (spawn_rejected code + solo_code + request echo); never reports success on failure
- [x] Name rejection returns structured error; no hidden retry with different name

**Next step readiness**:
- Step 4 (YAML policy overrides and structured logging) can proceed; spawn_agent surface stable and tested.
- Playbook follow-up: batch-done shorthand and fixture/disabled-id documentation patterns now established for future steps.
- Archive structure in place; step-03-workplan ready for reference; researcher and coordinator processes closed post-boundary.

---

### Step 4 Retro — 2026-05-02

**Duration**: ~52 minutes elapsed (6 tasks, 4 batches, 4 commits)

**What worked**:
- Batch A parallelism (T1 + T4, policy schema + logger module, both leaf modules) completed in ~3m with zero dependencies; 177 tests, foundation solid.
- Batch B sequential with large-tier escalation (T2 → T3): T2 (classifier override-awareness, 191 tests) executed smoothly; T3 (large-tier resolver) handled the critical matched_tokens shape change (string[] → object[]) without fixture breakage — resolver invariants held.
- Idle-based orchestration (solo_timer_fire_when_idle_any) remained performant; prevented manual polling; builder transition between batches automated cleanly.
- Fixture-based test strategy (no live Solo connection needed) proved again: all 229 tests fixture-based, all passing pre-boundary.
- Per-token source tagging (built_in|override) design validated: extend/replace merge logic clear, dedup behavior explicit, and diagnostics unambiguous when both token types present in same tier.
- Logger allow-list discipline (explicit field destructuring per event type) enforced sensitive-field invariant (no prompt/task/project_id/requested_name leakage); 3-event surface (resolution.success, resolution.failure, spawn.success) aligned with Story 13 exactly.
- Git archive pattern (git mv source → archive/, single commit) executed correctly; source-path deletion leakage finally eliminated (Step 2/3 recurring issue fixed).

**What didn't**:
- Phase 1 (workplan production): researcher (process_id 255) completed workplan silently without volunteer ping. Required orchestrator nudge to pull output and verify completion. Lesson: active orchestrator polling (not passive builder/researcher pings) is the reliable signal in async environments. Applied for Phase 2 — all task completions detected via idle timer, no silent waits.
- Minor: T3 (resolver) build took ~6m37s (1m longer than T1/T2 peers), but within acceptable large-tier timeline; no escalation needed.

**Metrics**:
- Tests: 229 passing (12 files, +95 new tests from Step 4: 27 policy + 16 logger + 16 classifier + 16 resolver + 20 tool handlers)
- Commits: 4 (workplan add, implementation, workplan archive, roadmap header)
- Builders spawned: 6 (1 × T1, 1 × T4 both medium; 1 × T2 medium, 1 × T3 large; 1 × T5 medium, 1 × T6 medium)
- Researcher: 1 (process_id 255, claude-opus, Phase 1 workplan production)
- Tier assignments: small (0), medium (5), large (1) per risk assessment
- Batch outcome: A (parallel, 1m43s) → B (sequential T2/T3, 2m03s + 6m37s) → C (5 single, 3m22s) → D (single, 2m38s)

**Shipping criteria verified**:
- [x] YAML policy overrides for command-token patterns (extend/replace per-tier, default extend)
- [x] Structured operational logs for resolution + spawn (pino to stderr, 3 event types, ISO timestamp)
- [x] Field-level YAML errors (zod validation, error-path strings, 3 snapshot tests)
- [x] Resolver diagnostics distinguishing override vs built-in (per-token source tag, override_token_count, preference_applied)
- [x] Logs omit prompts/free-form task content (allow-list per event, 16 forbidden-field sweep tests)
- [x] Complete test coverage (invalid YAML, override-source diagnostics, log shapes all exercised)

**Next step readiness**:
- Step 5 (docs, packaging, final review) can proceed; core feature set stable at 229 tests.
- Playbook refinement: researcher silent-completion pattern identified; future Phase 1 roles should include explicit "done" marker or rely on orchestrator active polling.
- Git archive pattern (source-path deletion + workplan archival) now working as documented — no leakage on Step 4.
- Coordinator context scratchpad (step-04-context) archived post-boundary; all builders/researcher closed cleanly.

---
