# Portable Workflow Stub

This directory is a copy-paste starter kit for the orchestration workflow.

## Goal

Provide a generic, repo-local workflow system that does not assume:

- a specific project name
- LDS
- existing roadmap/workflow/backlog content

## Layout

- `playbook/`: generic role playbooks and command prompts
- `notes/`: starter planning artifacts and templates (all paths are relative to `notes/`)
- `scripts/install-workflow-stub`: POSIX installer for copying this stub into another repo

## Quick Start

1. Run installer from this directory (or copied standalone repo):
   - `./scripts/install-workflow-stub --target /path/to/target/repo`
2. Optional project token replacement:
   - `./scripts/install-workflow-stub --target /path/to/target/repo --project-name "My Project"`
3. Choose conflict behavior if target files already exist:
   - `--mode skip` (default)
   - `--mode overwrite`
   - `--mode backup`
4. Run with the three commands in `playbook/README.md`.
5. If starting from ideas/proposals/stories/specs, run `orchestrator intake-proposal` before `start-next-round`.

## Required `notes/` Paths

- `notes/roadmap/`
- `notes/roadmap/archive/`
- `notes/proposals/`
- `notes/proposals/archive/`
- `notes/proposals/intake-output-template.md`
- `notes/backlog.md`
- `notes/project-planning-workflow-notes.md`

## Naming Policy

Templates should prefer "the project" over a proper project name.
Use `PROJECT_NAME` only where explicit branding is useful.

## Migration Checklist

- [ ] Install files into target repo.
- [ ] Replace `PROJECT_NAME` if desired.
- [ ] Verify `notes/playbook/README.md` prompt wording suits your team.
- [ ] Create first `notes/roadmap/roadmap-1.md` before `start-next-round`.
- [ ] Keep `notes/project-planning-workflow-notes.md` append-mostly.
