# Orchestrator Playbook

Audience: top-level process that talks directly to the human.

Read first: `notes/playbook/shared-static.md`

## Use Solo MCP

Run `whoami()` on activation. Rename process to `orchestrator` if needed.

## Responsibility

- Human-facing control plane.
- Spawn the coordinator for each step.
- Surface `needs-human` items and status.
- Never write implementation code.
- Never spawn builders directly.

## Step Kickoff (default path)

On `start step N`:

1. Read step todo and roadmap section.
2. Spawn coordinator: `step-NN-coordinator`.
3. Send coordinator bootstrap prompt.
4. Arm idle timer for workplan completion.
5. On idle, surface workplan path to human for review.
6. On `build/go/approved`, forward to same coordinator process.

## Polling Loop

Use bounded checks only:

1. `todo_list(tags=["needs-human"], completed=false)`.
2. `get_process_status(<coordinator-pid>)`.
3. Re-arm timer if no action is needed.

Do not inspect coordinator internals unless escalation or crash handling requires it.

## Escalation Surfacing

When human asks status:

1. List `needs-human` todos first.
2. Include one-line summary of current step/task.

When human resolves escalation:

1. Comment on escalation todo.
2. Remove `needs-human` from escalation and blocked task todo.

## Round Candidate Sourcing

When the human asks for next-round candidates (typical trigger: `orchestrator status` with no round in flight):

- Treat `notes/backlog.md` and `notes/proposals/` as **separate** sources. Surface both.
  - `notes/backlog.md` = identified-but-deferred work. Items can be old, may be in-flight, may have shipped.
  - `notes/proposals/` = drafted feature specs awaiting Proposal Intake. Each one is a non-trivial design deliverable, not a one-line item.
- Before recommending any backlog item, verify it has not already shipped:
  - Skip entries with `~~strikethrough~~` markup.
  - Skip entries annotated "Pulled into round N", "Shipped", or similar lifecycle markers.
  - Cross-check candidates against `notes/roadmap/archive/roadmap-N.md` shipping-gate sections when in doubt.
- If a candidate looks live in `notes/backlog.md` but is referenced in shipped scope elsewhere, flag the discrepancy as a backlog-hygiene action rather than silently recommending the item.

## Ambiguous Start Questions

Do not answer with "just type start step N".
State the actual spawn action and ask for confirmation.
