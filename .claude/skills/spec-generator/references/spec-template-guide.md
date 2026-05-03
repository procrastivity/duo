# Spec Template Guide

Annotated walkthrough of `docs/specs/_template.md` (the canonical spec shape), plus the LDS research priority order and the decomposition manifest template. Read this file in Phase 3 (spec drafting) — the section descriptions tell you what each part of the template is for and what counts as good vs. weak content.

---

## Frontmatter

Every spec opens with three fields:

```
**Version**: 0.1.0
**Date**: 2026-04-25
**Status**: Draft
```

- **Version** — semantic version of the spec itself (not the feature). New specs start at `0.1.0`. Bump the patch (`0.1.1`) for clarifications and copy edits; bump the minor (`0.2.0`) when behavior, schema, or contracts change. Major versions (`1.0.0`+) are rare and indicate the spec has been ratified.
- **Date** — the spec's last revision date in ISO format. Always today's date when generating a new spec.
- **Status** — `Draft` for new specs and amendments. `Approved` is set by the human after review. `Shipped` is set when the feature is implemented.

The frontmatter is plain markdown bold lines, not YAML. Match the existing specs' shape exactly — `docs/specs/filesystem-search.md` is the reference.

---

## Cross-Reference Conventions

Use relative paths from `docs/specs/` to other LDS layers:

- ADRs: `../decisions/{number}-{slug}.md` (e.g., `../decisions/0004-three-search-modes-as-peers.md`)
- Architecture sections: `../architecture/overview.md#{anchor}` (e.g., `#search-api`)
- Reference docs: `../reference/configuration.md#{anchor}` (e.g., `#watcher`)
- Other specs: `./{other-spec-slug}.md` (same directory)
- Appendices (optional, for large content): `./appendices/{feature_slug}/examples.md`

When citing a symbol or concept, use the anchor name; when citing a code location, use `path:line` only for research citations or machine-checked fingerprints. **Line numbers in prose rot on the next unrelated edit** — prefer symbol names (`fn watch_loop`, `struct ChangeEvent`) over line numbers in spec body text.

---

## The 10 Sections

### 1. Overview

A 1-2 paragraph description of what the feature does and why it exists. The first sentence should make the feature recognizable in isolation: *"Filesystem search answers path-shaped questions: what files exist in the vault, what's in this subdirectory, what matches this glob pattern."*

The Overview lists the **Related Documents** — the ADRs and architecture sections that constrain or inform this spec. If the feature touches an existing spec, reference it here too. List **Appendices** (if any) here as well.

**Strong:** specific, concrete, names the kind of question the feature answers.
**Weak:** vacuous ("This spec describes the X feature"), or overlapping with the implementation notes.

### 2. Behavior

Two subsections:

- **Normal Flow** — a numbered list of steps describing the happy path. Each step is one observable action ("Receive request with optional `prefix`, `glob`, `max_depth`" / "Query the files table in the store"). Don't include error branches here; those go in Edge Cases or Error Handling.
- **State Machine** (optional, when the feature has explicit states) — an ASCII diagram + a transitions table. Skip if the feature is stateless or single-state; do not invent states to fill the section. If skipped, note `**State Machine**: N/A — this feature is stateless.`

**Strong:** observable steps from an external perspective; states named for their meaning, not implementation details.
**Weak:** "Calls method X on class Y" (implementation, not behavior); a state machine for a stateless feature.

### 3. Data Schema

Two parts:

- **Schema examples** in YAML (request, response, event, persisted record — whatever shapes the feature defines). Match the existing specs' shape.
- **Field tables** (`Field | Type | Required | Default | Description`). Every field in every example needs a row.

End with **Validation Rules** (a short list of constraints that aren't expressible in the type column — e.g., "`max_depth` must be ≥ 1 if provided", "`prefix` must end with `/` when non-empty").

**Strong:** examples come from realistic use; field types are precise (`ISO-8601 string`, `sha256: hash`, `vault-relative path` — not just `string`).
**Weak:** placeholder field names, missing required/default columns, validation rules that restate the type column.

### 4. Examples

Two named examples (Example 1, Example 2) showing concrete Input → Behavior → Result. Pick examples that illustrate the feature's interesting behavior, not its trivial happy path. If a code block exceeds 50 lines, move it to `appendices/{feature_slug}/examples.md` and link.

**Strong:** examples reveal a non-obvious behavior or boundary; one happy path + one less-trivial case.
**Weak:** two near-identical happy paths; examples that don't add information beyond the schema.

### 5. Edge Cases

2+ scenarios with Scenario / Behavior / Rationale. Edge cases are not error handling — they're unusual but in-scope inputs the feature has to behave correctly on (symlinks, empty results, case-sensitivity, very large inputs, concurrent access, conflict files, etc.).

The Rationale field is load-bearing — it's where the spec captures *why* the chosen behavior is correct, which keeps the next reader from "fixing" the design.

**Strong:** edge cases drawn from real friction points the project has hit; rationale that explains the decision, not just restates the behavior.
**Weak:** invented edge cases that don't actually happen; behavior with no rationale.

### 6. Error Handling

A table: Error Condition | Error Code/Type | Message | Recovery. Cover the errors the feature surfaces to its consumers — not internal errors that get logged and swallowed, and not Rust panics. If the table exceeds 20 rows, move the full catalog to `appendices/{feature_slug}/errors.md`.

**Strong:** every error has a recovery path the consumer can act on; messages are user-facing and actionable.
**Weak:** "Internal error → 500 → 'Something went wrong' → 'Try again'"; rows for errors the feature can't actually emit.

### 7. Integration Points

For each component / spec / external system this feature talks to, a subsection: how they connect, what data crosses the boundary, what guarantees apply. Optional ASCII data-flow diagram.

If the feature stands alone, write `**Integration Points**: This feature is self-contained; it consumes only the SQLite store and produces results to its caller.` Don't invent integrations to fill the section.

**Strong:** named modules + data shapes at the boundary; type guarantees stated explicitly.
**Weak:** "Talks to the database" (which database? what tables? what's the contract?).

### 8. Implementation Notes

Guidance for the engineer building the feature: invariants to maintain, patterns to follow, gotchas to watch for. **Not** a task list and **not** a class/method design. The format that works: a few short paragraphs or a bulleted list of "things the implementer needs to know that aren't obvious from the rest of the spec."

If the feature has a negative-fingerprint grep (e.g., "after this is done, `rg 'old_pattern'` should return zero matches"), put it here.

**Strong:** invariants the implementer would otherwise have to discover the hard way; negative greps; references to skills (`See \`.claude/skills/filesystem-watching\` for the debouncer pattern`).
**Weak:** prescribing class structure or method signatures; restating the Behavior section.

### 9. Open Questions

A checkbox list of items that could not be resolved during the spec session. Each open question should fit one of three shapes:

1. **Genuine post-approval decisions** — needs more data or external input ("Should symlinks be indexed at all? — needs real-world usage data").
2. **Workplan-time resolutions** — fits in 1-3 paragraphs of workplan prose with a 'Why' (per `notes/project-planning-workflow-notes.md` TBD rule).
3. **Out-of-scope flags for a future spec** — "Regex query support is out of scope for v0.1; deferred to a future spec on advanced search."

Do NOT use this section for `[TBD]` placeholders that depend on user input — those should be resolved in conversation before drafting.

**Strong:** each question names what would resolve it (data, decision, future spec).
**Weak:** open questions that are actually requirements ("How fast should it be?" — that's a requirement; press for an answer or scope the question precisely).

### 10. Revision History

A table: Version | Date | Changes. Initial draft gets one row. Amendments add a row each.

For amendments, the Changes column should name what changed and why ("Added regex support to filesystem search per ADR-0011; bumped to 0.2.0 because behavior contract changed"). For copy edits and clarifications that don't change behavior, bump the patch and note "Clarified Edge Cases #2 wording; no behavior change."

---

## TBD Handling Rule

From `notes/project-planning-workflow-notes.md`:

> *If the resolution fits in 1-3 paragraphs of workplan prose with a 'Why', it does not need to be an ADR.*

Use this rule when deciding whether an open item:

- **Stays in the spec's Open Questions section** (workplan-time resolution; small enough to handle inline) — fits the rule
- **Queues a new ADR in the decomposition manifest** (load-bearing decision worth its own document) — does NOT fit the rule

If you can write the resolution in 3 short paragraphs with a clear "Why," it's a workplan-time concern. If it would take a section, multiple alternatives, and trade-off analysis, it's an ADR.

---

## LDS Research Priority Order

Phase 1 of the skill reads in this order, then greps. The rationale per layer:

1. **`docs/product/vision.md`** — establishes scope boundary. The cheapest place to find out the proposal would amend product canon, before investing in a spec.
2. **`docs/decisions/*.md`** — load-bearing ADRs. Constrain the spec; flag conflicts before drafting.
3. **`docs/specs/*.md`** — sibling specs. Find amendments-vs-new-spec branches, identify shared behavior, avoid duplication.
4. **`docs/architecture/overview.md`** — system shape. Module boundaries, API contracts, where the new feature plugs in.
5. **`docs/implementation/tech-stack.md`** — what's available. Avoid speccing behavior the stack can't deliver.
6. **`.claude/skills/*/SKILL.md`** — subsystem patterns. For Hypomnema: `filesystem-watching`, `rusqlite-in-async`, `markdown-chunking`, `sqlite-vec-extension` are load-bearing for any feature that touches their domains.
7. **Codebase grep** — last. By now you know what to search for.

The order is high-to-low authority. Reading in this order surfaces conflicts at the layer that wins them: a spec that contradicts vision is wrong; a spec that conflicts with implementation reality should usually win (and the implementation gets revised). Reverse the order and you spend time on conflicts you'd discover later anyway.

---

## Decomposition Manifest Template

Present at the end of Phase 5, inline in the conversation when handing back the spec. Include only categories that have content.

```markdown
## Decomposition Manifest

### New ADRs to draft
- ADR draft for [decision]: [one-sentence summary of the decision and what makes it ADR-worthy]
- ...

### Vision amendments needed
- `docs/product/vision.md:NN-MM` — [what needs to change and why]
- ...

### Architecture diagram updates needed
- `docs/architecture/overview.md#anchor` — [what diagram or section needs updating]
- ...

### New CLI / config to add to `docs/reference/`
- `docs/reference/cli.md` — [new command or flag to document]
- `docs/reference/configuration.md` — [new config key with default + validation]
- ...

### Open Questions routed to other specs
- [Question] — belongs in `docs/specs/<other-spec>.md` § Open Questions
- ...

### Workplan-ready user stories
- `notes/proposals/<slug>-stories.md` (or `docs/specs/<feature>-stories.md` after promotion)
```

The manifest is the **handoff** — what the human needs to do next, in priority order. It is not a project plan; it doesn't sequence the work or assign owners. It just lists the artifacts that need to exist before the spec is fully integrated into the LDS.
