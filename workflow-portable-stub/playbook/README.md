# Playbook

Quick commands:

- `orchestrator status`
- `orchestrator intake-proposal`
- `orchestrator start-next-round`
- `orchestrator continue`

Long-form prompts:

- `Route this request to the project's orchestrator. If a process named "orchestrator" is already running, deliver the task below to it; otherwise spawn a new agent, have it adopt the orchestrator role per playbook/orchestrator.md (which includes renaming its process to "orchestrator"), and deliver the task to that new process. Relay the orchestrator's response — do not act as the orchestrator yourself. Task for the orchestrator: report (a) whether we are in the middle of a round right now, (b) if not, what step/round is planned next, (c) if nothing is planned, what are the current candidates for the next round, sourced **separately** from notes/backlog.md (deferred/un-scoped work) and notes/proposals/ (drafted proposals awaiting Proposal Intake). Use roadmap (notes/roadmap/archive/) and workflow notes for context. Before recommending any backlog item, verify it has not already shipped — skip entries marked ~~strikethrough~~, "Pulled into round N", or otherwise covered by an archived roadmap-N.md step. Keep it concise and include concrete file references.`
- `Route this request to the project's orchestrator. If a process named "orchestrator" is already running, deliver the task below to it; otherwise spawn a new agent, have it adopt the orchestrator role per playbook/orchestrator.md, and deliver the task to that new process. Relay the response — you are not the orchestrator. Task for the orchestrator: run Proposal Intake on the provided planning inputs (idea notes, proposal, PRD, stories, spec, or any subset). Use notes/proposals/intake-output-template.md as the output shape. Produce: (1) a proposed roadmap step breakdown with goals, shipping criteria, deferred decisions, and risk; (2) a story/requirement coverage map from source inputs to proposed steps; (3) unresolved questions that block confident planning; and (4) a recommended next action: start step N now vs refine inputs first. Keep it format-agnostic; do not assume any specific generator schema.`
- `Route this request to the project's orchestrator. If a process named "orchestrator" is already running, deliver the task below to it; otherwise spawn a new agent, have it adopt the orchestrator role per playbook/orchestrator.md, and deliver the task to that new process. Relay the response — you are not the orchestrator. Task for the orchestrator: start the next round. If the next round is already defined/planned, begin it immediately (spawn coordinator/researcher flow per playbook). If not defined, ask one focused question: which feature is understood well enough to plan next. Then proceed with the bootstrap needed to move forward.`
- `Route this request to the project's orchestrator. If a process named "orchestrator" is already running, deliver the task below to it; otherwise spawn a new agent, have it adopt the orchestrator role per playbook/orchestrator.md, and deliver the task to that new process. Relay the response — you are not the orchestrator. Task for the orchestrator: continue exactly from current project state (active round/step/escalations/todos) and resume execution per playbook, including spawning or reconnecting to coordinator/researcher/builders as needed. If blocked on a human decision, surface only the blocking decision and options.`

## Files

- `shared-static.md`
- `agent-tool-selection.md`
- `orchestrator.md`
- `coordinator.md`
- `researcher.md`
- `builder.md`

## Path Conventions

All planning artifacts referenced by these playbooks are expected under `notes/`:

- `notes/roadmap/`
- `notes/proposals/`
- `notes/backlog.md`
- `notes/project-planning-workflow-notes.md`
