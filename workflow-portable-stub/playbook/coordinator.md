# Coordinator Playbook

Audience: `step-NN-coordinator`.

Read first: `notes/playbook/shared-static.md`

## Use Solo MCP

Run `whoami()` and confirm process identity before actions.

## Responsibility

- Drive one step end-to-end.
- Spawn and manage persistent researcher.
- Request workplan from researcher.
- Orchestrate builders during build.
- Route escalations to orchestrator/human.

## Phase 1: Workplan Production (researcher-first, default)

1. Spawn researcher: `step-NN-researcher`.
2. Send researcher the workplan request.
3. Wait for researcher completion.
4. Review generated workplan for structure/completeness.
5. Surface workplan path + summary to human for review.
6. Keep researcher process alive after approval.

No non-researcher fallback path is defined in this playbook.

## Phase 2: Build Orchestration

On `build/go/approved`:

1. Create step context scratchpad from shared template.
2. Record researcher process id in scratchpad header.
3. Decide batching and create per-task todos.
4. Execute per-task loop:
   - spawn builder
   - send builder bootstrap prompt
   - arm idle timer
   - route outcome: advance / retry / escalate

## Research Consult Routing (new default)

If a builder or coordinator is blocked on design/analysis/spec interpretation:

1. Pause task progression for the blocked task.
2. Send focused question to researcher.
3. Wait for researcher response.
4. Record result in `Decisions made during build`.
5. Forward distilled guidance to blocked builder as todo comment or in retry prompt.

Builders do not contact researcher directly; coordinator is the routing hub.

## Wake-up Routing (builder idle)

1. Check todo + comments.
2. If completed with results comment: append per-task outcome and close builder.
3. If `needs-human`: create coordinator escalation todo and pause further spawning.
4. If idle/no comment: do status-check prompt and re-arm short timer.
5. If dead process: respawn once, then escalate.

## Retry / Escalation Policy

- Up to 2 retries for fixable failures with clear error context.
- Same failure twice: escalate.
- Ambiguity/spec conflict/scope question: escalate immediately.

## Step Boundary

1. Verify shipping criteria.
2. Run post-build eval and append retro entry.
3. Archive workplan and scratchpad.
3a. If this step closes a round (last step in `roadmap-N.md`): also archive `roadmap-N.md` → `notes/roadmap/archive/`, and archive every proposal + intake file referenced by that roadmap's `**Intakes**:` block → `notes/proposals/archive/`. For each linked `intake-<slug>.md`, also move the matching `<slug>.md` and `<slug>-stories.md` if present. Run `scripts/check-proposal-hygiene.sh` to confirm no orphans remain.
4. Post step-shipped comment.
5. Close researcher and coordinator processes.
