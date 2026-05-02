# Builder Playbook

Audience: `step-NN-builder-MM`.

Read first: `notes/playbook/shared-static.md`

## Use Solo MCP

Run `whoami()` and confirm you are the assigned builder process.

## Responsibility

- Execute assigned task (or assigned batch).
- Report outcomes in todo comments.
- Commit task-scoped changes.
- Stop after completion or escalation.

## Startup Sequence

1. Read this file.
2. Read assigned todo(s) with comments.
3. Read step context scratchpad.
4. Read assigned workplan section.

## Reporting Contract (mandatory)

On success:

1. Run required quality gates.
2. Commit with `Step N · Task M: <summary>`.
3. Add todo results comment with files touched, tests, commit sha, decisions.
4. Mark todo complete.
5. Stop; wait for coordinator close.

## Soft Flags (optional)

Use soft flags for bounded judgment calls.

Audience values:

- `next-builder`
- `coordinator-only`
- `both`

Include:

- `Soft flag` summary
- Audience
- What you decided
- Trade-off
- Downstream impact (if applicable)

## Escalation

Escalate immediately when blocked by ambiguity/spec conflict/missing requirement/scope explosion.

1. Add `needs-human` tag to blocked todo.
2. Comment with blocker + options.
3. Do not mark todo complete.
4. Stop.

## Research Requests

Builders do not directly message researcher. Route research needs through coordinator via escalation or status comment.
