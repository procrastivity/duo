# Project-Planning Workflow Notes

**Project**: PROJECT_NAME
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
