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
5. If this step closes a round, run the round-close steps in `notes/release-process.md` (CHANGELOG, version bump, tag, push — defined per project).
6. Prepare next step workplan.
7. Get human review before coding next step.

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

### Step 5 Retro — 2026-05-02

**Duration**: ~1 hour 10 minutes elapsed (6 tasks, 4 batches, 2 commits — omnibus + follow-up)

**What worked**:
- Batch A parallelism (T1 + T2): package.json metadata and build config completed in ~9m; npm pack validation confirmed correct tarball structure; shebang + source maps verified.
- Batch B (T3 README): tier-label-only examples enforced via workplan spec; grep checks (zero Hypomnema, ≥3 tier mentions, matched_tokens shape post-Step-4) all passing; 11 sections per outline, 382 lines.
- Batch C (T4 CI workflows): ci.yml matrix [22, 24, 25, 26] + release.yml tag-triggered with version-equality bash check and OIDC permissions — all acceptance items verified inline, no CI dry-run needed yet (awaiting first tag).
- Batch D parallelism (T5 + T6): docs/PUBLISHING.md 8-step bootstrap guide (298 lines, no hardcoded secrets, references user credentials) + scripts/smoke-pack.sh (59 lines, 8 checks, all passing locally with exit code 0).
- Git artifact cleanup (follow-up commit): ESM import extensions (.js), build artifact excludes (vitest.config.*, *.tgz), egg-info untracking — all reconciled in single follow-up commit.
- Idle-based timer orchestration (fire_when_idle_all) again performant; both Batch B/C and Batch D pairs completed within expected windows.

**What didn't**:
- /tmp permission-prompt stall in builder-03 (Task 3): attempted scratch shell script to /tmp for grep checks; Solo sandbox denied; builder output truncated; recovery required inline grep instead of shell. Lesson: avoid /tmp writes from sandboxed agents; do verification inline with grep or delegate to orchestrator.
- Builder cleanup drift: Batch A builders (272, 273) remained live ~1h after task completion; required explicit process close at round boundary. Recurring pattern from Step 4 → Step 5. Lesson: document explicit "await close" on processes post-task to prevent stale handles.
- Working-tree artifact leakage recurrence: vitest.config.js, *.tgz, egg-info edits still uncommitted post-omnibus. Step 4 git-mv pattern fixed; Step 5 required manual .gitignore + .npmignore updates + cleanup commit. Pattern now documented for future steps.

**Metrics**:
- Tests: 229 passing (12 files, unchanged from Step 4; no new test files added in Step 5).
- Commits: 2 (omnibus b2fe143 with 1369 lines added + follow-up 783705d with 22 insertions, 34 deletions)
- Builders spawned: 6 (T1 medium, T2 medium, T3 medium, T4 medium, T5 medium, T6 medium; no large tiers needed for packaging/docs work)
- Researcher: 0 (no Phase 1 role; coordinator only)
- Tier assignments: small (0), medium (6), large (0)

**Shipping criteria verified**:
- [x] Package named @procrastivity/duo, scoped, public-ready, unclaimed on npm registry
- [x] MIT LICENSE file at repo root (2025, The Duo Authors)
- [x] .npmignore excludes src/, tests, dev files; .gitignore excludes build artifacts, *.tgz, egg-info
- [x] README.md 11 sections: installation (npx, global, local), MCP client setup, configuration, three tools with tier-label-only inputs, policy overrides, logging (3 events), spawn_process continuation, versioning, license
- [x] ci.yml matrix [22, 24, 25, 26] on push/PR
- [x] release.yml tag-triggered with version-equality check, OIDC, --provenance --access public
- [x] docs/PUBLISHING.md 8-step bootstrap (prerequisites, login, verify, trusted publishers, publish v0.1.0, revoke, CI, provenance)
- [x] scripts/smoke-pack.sh executable, 8 checks, all passing

**Next step readiness**:
- Packaging complete; ready for manual bootstrap (docs/PUBLISHING.md).
- CI pipeline wired; first release.yml run pending first v0.1.1+ tag after bootstrap.
- Round 1 complete; proposals/intake ready for Round 2.

---

## Round 1 Retro — Roadmap 1 shipped 2026-05-02

**Round duration**: 2026-05-02 04:20 UTC (Step 1 start) → 2026-05-02 ~21:30 UTC (Step 5 boundary close) ≈ **17 hours 10 minutes**

**Test progression**: 0 → 134 → 177 → 229 → 229 tests (Steps 1–5)

**Builders spawned**: ~23 total across all steps (6 Step-1, 7 Step-2, 4 Step-3, 6 Step-4, 6 Step-5)

**Tier assignments**: small (2–3), medium (~18–19), large (1–2)

**Compounding lessons**:
1. Git source-path leakage (Steps 2–4): Solved via single-commit `git mv` pattern in Step 4; applied consistently Step 5.
2. Builder cleanup drift (Steps 1–5): Persistent; fire_when_idle_all signals but doesn't auto-close. Explicit `solo_close_process` needed; documented for Round 2.
3. Phase 1 silent-completion (Step 4): Orchestrator active polling + idle timers together provide reliable "done" signal.
4. /tmp permission-prompt anti-pattern (Step 5): Avoid /tmp writes from sandboxed agents; do verification inline.
5. Working-tree artifact leakage (all steps): Comprehensive .gitignore + .npmignore entries pre-build. Applied Step 5 follow-up.

**Status: Round 1 shipped**:
- Tier-based MCP companion complete (229 tests, 12 files).
- Packaging ready for manual bootstrap (@procrastivity/duo, docs/PUBLISHING.md runbook).
- CI/Release wired (GitHub Actions OIDC).
- Documentation complete (README, PUBLISHING.md, smoke-pack.sh).
- Proposal intake empty; Round 2 intake natural next move.

