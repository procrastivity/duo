# Release Process

This file describes how releases are cut for this project. Fill it in once and edit as the process evolves. The orchestration playbook delegates all release/tag/version concerns here so the rest of the workflow can stay tool-agnostic.

**Tool**: <git-cliff | cargo-release | npm version | ./contrib/release | TBD>
**Version scheme**: <semver | calver | unversioned | TBD>
**Trigger**: <per round | per step | on demand>

## Pre-cut checks

- [ ] CI is green on the commit being released
- [ ] CHANGELOG (or generated equivalent) reviewed
- [ ] (project-specific checks)

## Cut command

```
<literal command, or "see ./contrib/release", or step-by-step>
```

## Push policy

- <e.g. "push HEAD and tags to origin", or "open PR first then tag from main">

## Versioning notes

Versions are decided at cut time from the merged history (commit messages, manual judgment, or whatever the chosen tool consumes). **Do not pre-assign versions to rounds or steps in the roadmap** — a single late-arriving change can flip the resulting version, and tools like `cargo-release` / `git-cliff` infer the version themselves.
