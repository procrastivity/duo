---
name: spec-generator
description: Generate feature specs (and a peer user-stories artifact) for Hypomnema or another LDS-shaped project through a structured discovery interview. Use this skill when the user wants to spec out a feature, write up what we're building, define requirements for a feature, or turn a feature idea into a `docs/specs/<feature>.md` document. Triggers on phrases like "spec out X", "write a spec for", "draft a spec", "feature spec for", "I have a feature idea I need to document", "create user stories for X", "let's plan feature Y". Output conforms to the project's `docs/specs/_template.md`. For Hypomnema specifically, this is the feature-scoped successor to `prd-generator` per the policy at `notes/project-planning-workflow-notes.md` § "PRD / spec-generator scope policy".
---

# Spec Generator

Generate feature specs that conform to a project's LDS (Layered Documentation System) layout, scaled to the project's actual stakes. The output is a spec, not a PRD: behavior, data schema, edge cases, error handling, integration points — not personas, business cases, or rollout plans (those live elsewhere when they exist at all).

## Core Philosophy

A spec defines a feature's **behavior boundary**: what it does, how it responds, what shapes its inputs and outputs take, what edge cases it handles. Workplans live elsewhere. Architectural decisions live in ADRs. Product framing lives in vision. The spec is the contract between "this feature is approved" and "engineering can build it without re-litigating the design."

Key principles:

- **Specs are lean.** Hypomnema's spec template is 10 sections; a focused feature spec might be 1-3 pages. Brevity forces clarity.
- **Content quality beats structural completeness.** Sections marked `TBD` with a reason are more honest than vacuous filler.
- **Scope-aware investigation.** A solo local CLI does not need analytics, rollout, or business-viability scrutiny. A public SaaS does. The skill asks one upfront question to set its investigative defaults; it pulls in deeper concerns just-in-time when the conversation surfaces a signal.
- **LDS authority order is load-bearing.** If a draft spec contradicts a higher-authority document (vision, ADRs, architecture), the skill flags it — it does not silently override.
- **Specs become specs, not PRDs.** This skill writes to `docs/specs/<feature>.md` (or `notes/proposals/<slug>.md` first, see Output Contract). It does not produce a parallel PRD canon.

## Independent Thought

Avoid simply agreeing with the user's points or taking their conclusions at face value. The goal is real intellectual challenge, not just affirmation. When they propose an idea:

- **Question their assumptions.** What are they treating as true that might be questionable?
- **Offer a skeptic's viewpoint.** What objections would a critical, well-informed voice raise?
- **Check their reasoning.** Are there flaws or leaps in logic being overlooked?
- **Suggest alternative angles.** How else might the idea be viewed, interpreted, or challenged?
- **Focus on accuracy over agreement.** If their argument is weak or wrong, correct them plainly and show how.

Stay constructive but rigorous. If you catch bias or unfounded assumptions, say so plainly.

---

## Process Overview

The flow has six phases:

0. **Scope profile** — one upfront question that sets investigative defaults
1. **LDS-aware research** — read the docs in priority order before grepping code
2. **Grounded conversation** — discuss what you found; press on the right things for the active profile
3. **Spec drafting** — produce the template-conformant spec + the user-stories peer artifact
4. **Consistency pass** — five mandatory checks before presenting
5. **Decomposition manifest** — the "what to do next" handoff to the human

The critical insight: **research before you ask, not after.** When the user describes a feature, your first move is to read the LDS docs (vision, ADRs, specs, architecture) and then grep the codebase. Then have a real conversation grounded in what you found.

---

## Phase 0: Scope Profile

**This is the first thing the skill does.** Ask exactly one question:

> *What's the rough shape of the project this spec is for? (Pick one: `solo-local-cli` / `internal-tool` / `public-saas` / `library-or-sdk`. Default if you're not sure: `solo-local-cli`.)*

Read `references/scope-profiles.md` to understand what each profile means and which concerns it pre-enables. The profile you record drives:

- Which questions you press on during Phase 2
- Which `references/concerns/<name>.md` files you load (only those the profile turns on, plus any the user accepts via JIT prompts)
- The rigor level for the user-stories peer artifact
- What the decomposition manifest surfaces

The profile is asked every session — there is no sticky configuration to read from. If the user does not answer or seems irritated by the question, default to `solo-local-cli` (the dominant Hypomnema case) and move on.

**Do not ask follow-up profile questions.** This is a one-question phase. Subsequent investigation is shaped by the profile but driven by the conversation itself.

---

## Phase 1: LDS-aware Research

Before asking ANY content questions, read the LDS docs in priority order. The point is to come back with informed context, not blank-slate questions.

### Read order

1. **`docs/product/vision.md`** — scope-boundary check. Does this proposal stay inside vision, or would it amend a guiding principle, success criterion, or Non-Goal? If it would amend vision, this is a load-bearing branch — see "Vision boundary check" below.
2. **`docs/decisions/*.md`** — load-bearing ADRs that constrain the spec. Identify any whose subject area overlaps the feature.
3. **`docs/specs/*.md`** — existing feature behavior. Does this spec touch, extend, or amend an existing one? If yes, this is the **amendment branch** — see Output Contract.
4. **`docs/architecture/overview.md`** — system shape constraints (module boundaries, API contracts).
5. **`docs/implementation/tech-stack.md`** — what's actually available to build with.
6. **`.claude/skills/*/SKILL.md`** — subsystem patterns the spec must respect (e.g., `filesystem-watching`, `rusqlite-in-async` — both load-bearing for Hypomnema).
7. **Codebase grep** — only after the docs are read. Look for partial implementations, related code, and patterns to follow.

### LDS canon conflict check

Per `notes/project-planning-workflow-notes.md` § "PRD / spec-generator scope policy", **if the proposal would amend a higher-authority layer (vision, ADR, architecture invariant) rather than slot into a spec, that is a signal to stop and route to canon-level work**, not produce a spec.

This applies to any layer above specs: a vision Non-Goal that would have to change, an ADR whose decision would have to be amended or superseded, an architecture invariant that the proposal would violate. A single proposal may conflict with multiple layers at once.

When you detect any such conflict, surface it explicitly to the user before drafting and recommend the canon-negotiation workflow:

> *"This proposal conflicts with [layer + specific item]. That's canon-level work, not a spec. Recommend invoking `@docs/maintenance/explore.md` for [proposal] — it walks through the load-bearing rationale, maps the impact, and produces canon edits + ADR drafts under your approval. Once canon is settled, come back here for the spec(s)."*

If the user wants to override ("draft the spec anyway, the canon amendment is intentional"), proceed — but call out in the decomposition manifest exactly which canon items need amending.

### LDS authority order

When a contradiction surfaces between layers, higher-authority layers win:

> Vision > Decisions (ADRs) > Specs > Architecture > Implementation > Reference

Never silently override a higher-authority layer with a lower-authority one. Surface the conflict and let the user decide.

### Time-box

Spend 2-5 minutes on initial research. You're not trying to understand everything — you're trying to know enough to have an intelligent conversation. You can always do more targeted research later in Phase 3 (extending Phase 1) when the conversation reveals a gap.

---

## Phase 2: Grounded Conversation

Now come back to the user with context. This is not a generic interview — you bring knowledge from Phase 1 to the table.

### Start with what you found

> "I read vision and ADRs 0001-0008. The closest related spec is `change-events.md`. Your proposal touches the watcher's debounce window, which is fixed at 250ms in the `filesystem-watching` skill — that's a constraint to design within. The proposal does not appear to amend vision; it stays inside the v0 scope. Let me push on the behavior shape..."

This builds trust and catches misunderstandings early.

### Press on what the profile cares about

Read `references/scope-profiles.md` for the active profile's concern matrix. Ask only about concerns the profile enables. **Do not interrogate on concerns the profile turns off.**

For every profile, you must clarify:

- **Behavior**: What is the normal flow? What states does the feature have?
- **Data schema**: What goes in, what comes out, what's the structure?
- **Edge cases**: What unusual inputs or conditions does the feature need to handle?
- **Error handling**: What can go wrong and how is it surfaced?
- **Integration points**: What other modules / specs / external systems does this touch?

For `solo-local-cli` specifically, **do not** press on personas, success metrics (beyond a one-line "what does success look like"), analytics, rollout plans, or business viability. Those concerns are off by default for this profile.

### JIT prompts: lightweight offers, not interrogation

The trigger table in `references/scope-profiles.md` lists signals → concerns to offer pulling in. When a signal appears in the conversation, make a single yes/no offer:

> "You mentioned 'we need to know if it's actually used' — that sounds like an Analytics concern. Want me to pull in the analytics investigation guide and press on instrumentation? (yes/no)"

If the user accepts, read the corresponding `references/concerns/<name>.md` file before asking further questions. If the user declines, **do not offer the same concern again in this session** — once is enough.

### Adapt to the user

- **If they gave a detailed proposal:** Validate against the LDS docs and code. Come back with conflicts, missed prior art, or constraints they may have overlooked.
- **If they gave a vague idea:** State your understanding explicitly, ask them to confirm or correct.
- **If they want to move fast:** Don't force unnecessary rounds. Get enough to write a good spec, not a perfect one.

### Resolve open questions before drafting

Do NOT proceed to Phase 3 until every question that requires user input is answered. The only acceptable `[TBD]` items in the spec itself are those:
- Requiring input from someone other than the user, OR
- That fit the project's TBD rule: *"if a resolution fits in 1-3 paragraphs of workplan prose with a 'Why', it does not need to be an ADR"* — these can stay in the spec's Open Questions section to be resolved at workplan time.

If the resolution would NOT fit in 1-3 workplan paragraphs, surface it now as a question to the user; do not paper over it as an open question.

---

## Phase 3: Spec Drafting

Read `references/spec-template-guide.md` for the section-by-section walkthrough. Read `references/user-story-guide.md` before generating the user-stories peer artifact.

### Output contract

Decide where the spec lands:

- **Non-trivial features** (most): write the spec to `notes/proposals/<slug>.md`. Stories go to `notes/proposals/<slug>-stories.md`. After approval, both promote: spec to `docs/specs/<feature>.md`, stories archived alongside the proposal under `notes/proposals/archive/`.
- **Trivial features** ("this is a one-paragraph spec amendment, not worth the proposal cycle"): write directly to `docs/specs/<feature>.md`, or as an amendment patch against an existing spec.
- **Amendment to an existing spec**: write the amendment patch with the **full revised spec re-printed** for review context. After approval, apply the patch to the canonical spec; bump Version in the frontmatter; add a row to the Revision History table.

**Always announce the path you're choosing before writing**, so the user can override:

> "This looks like a small enough change to `change-events.md` that I'll write it as an amendment patch rather than a new spec or a proposal. Override?"

### Always emit all 10 spec sections

The spec template has 10 sections (Overview, Behavior, Data Schema, Examples, Edge Cases, Error Handling, Integration Points, Implementation Notes, Open Questions, Revision History). Emit all of them. Sections without content get a brief `TBD: <reason>` marker — this is more honest than silently omitting them, and the next reader knows whether the gap is intentional or accidental.

**Scope profiles do NOT toggle spec sections.** Every spec emits the full template. Profiles control which **concerns the skill investigates** during conversation, what surfaces in the **decomposition manifest**, and the **rigor of acceptance criteria** in the user-stories artifact — not which spec sections appear.

### Frontmatter

Use the template's frontmatter:

```
**Version**: 0.1.0
**Date**: <today, ISO format>
**Status**: Draft
```

For amendments, increment Version (0.1.0 → 0.1.1 for clarifications, → 0.2.0 for behavior changes), keep Status as Draft until re-approved.

### User stories as a peer artifact

Stories are NOT a section of the spec — they live in their own file (`notes/proposals/<slug>-stories.md`). The spec defines behavior; the stories define delivery scope. They reference each other but live separately.

Read `references/user-story-guide.md` for INVEST + AC discipline. The story file should reference the spec by path; the spec's Implementation Notes section can reference the stories file by path.

### Resolve every open item before writing

Same rule as Phase 2: do not write `[TBD]` placeholders that depend on user input. The acceptable shapes for an Open Question in the spec are:

- A genuine post-approval decision (e.g., "should symlinks be indexed at all? — needs to be re-decided once we have real-world usage data")
- A workplan-time resolution (fits in 1-3 paragraphs, not yet detailed enough to commit)
- An out-of-scope detail flagged for a future spec

---

## Phase 4: Consistency Pass

**MANDATORY before presenting the spec to the user.** Run these five checks:

1. **Thesis check.** Does any section leave an instance of the problem the spec exists to solve intact? If yes: pull it in or narrow the spec.
2. **Boundary-graph check.** If the feature crosses module boundaries, has Behavior + Data Schema + Integration Points enumerated every hop the value crosses with a type guarantee at each? Untyped passthroughs in unnamed hops are where half-wirings survive.
3. **Discriminating-AC check** (for the user-stories file). Walk every acceptance criterion. Would it still pass if the function under test returned a constant, a trivial value, or an overly-inclusive result? See `references/user-story-guide.md` for the failure shapes.
4. **Negative-fingerprint check.** For any anti-pattern the spec wants to prevent, is there a grep that returns zero matches when the feature is implemented correctly? Add the grep to Implementation Notes.
5. **Entity surface check.** For features that mutate data, does Data Schema + Integration Points cover every persistence column, child relation, type/enum, validator, cache key, and external system record affected?

Edit the spec until all five pass.

---

## Phase 5: Decomposition Manifest

Produce a structured "what to do next" handoff. This is part of your output, separate from the spec file itself — present it inline in the conversation when you hand back the spec for review.

Categories (use only those that have content):

1. **New ADRs to draft** — one bullet per significant decision the spec depends on. Queued; the human drafts after the spec is approved.
2. **Vision amendments needed** — if the spec required overriding or expanding vision, what needs to change in `docs/product/vision.md` (with line references).
3. **Architecture diagram updates needed** — if the spec touches module boundaries, what changes in `docs/architecture/overview.md`.
4. **New CLI / config to add to `docs/reference/`** — if the spec exposes a new user-facing surface.
5. **Open Questions routed to the spec they touch** — if the conversation surfaced questions belonging to another existing spec, point them there.
6. **Workplan-ready user stories** — pointer to the peer artifact at `notes/proposals/<slug>-stories.md`.

Use the TBD rule for distinguishing "this needs an ADR" from "this resolves at workplan time": *if the resolution fits in 1-3 paragraphs of workplan prose with a 'Why', it does not need to be an ADR*.

---

## Anti-Patterns to Watch For

Flag these when you see them:

1. **Solution-first thinking.** Feature without articulated behavior boundary. Push for "what should it do" before "how should it work."
2. **Vacuous content.** "Robust error handling" / "scalable design" / "follows best practices" — be specific or mark TBD with a reason.
3. **Project-plan creep.** Sprint schedules, task lists, who's-doing-what — those don't belong in a spec. They belong in a workplan.
4. **Personas / metrics / analytics in solo-local-cli specs.** If the active profile is `solo-local-cli`, do not press on these. The profile turns them off for a reason.
5. **Silent canon override.** If the spec requires changing a guiding principle, Non-Goal, ADR decision, or architecture invariant from a higher-authority layer, surface it and route to `@docs/maintenance/explore.md` — do not let the spec quietly contradict canon.
6. **PRD framing that survived the fork.** "Target users and personas," "rollout plan," "go-to-market" — these are PRD vocabulary. Specs talk about behavior.
7. **Stories embedded in the spec.** Stories live in `<slug>-stories.md`. The spec references them; it does not contain them.
8. **`[TBD]` placeholders that depend on user input.** Resolve in Phase 2; don't paper them into the spec.

---

## Reference Files

Always-loaded references:
- `references/spec-template-guide.md` — the 10-section template walkthrough, frontmatter conventions, LDS research priority order, decomposition manifest template
- `references/scope-profiles.md` — the four profiles, the concern-to-investigation matrix, the JIT trigger table
- `references/user-story-guide.md` — INVEST, observability, discriminating-AC, boundary-graph, negative-fingerprint discipline for the user-stories peer artifact

Per-concern references (load only when the active profile enables them, OR when a JIT trigger is accepted):
- `references/concerns/analytics.md`
- `references/concerns/launch-and-rollout.md`
- `references/concerns/multi-tenancy.md`
- `references/concerns/business-viability.md`
- `references/concerns/success-metrics.md`
- `references/concerns/library-and-sdk.md`

Each concern file is self-contained: what the concern means, when it's in scope, what to investigate, what rigor applies, what the manifest entry looks like, examples of strong vs. weak coverage.
