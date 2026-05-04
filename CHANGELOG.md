# Changelog

All notable changes to this project will be documented in this file.

## [0.1.5] - 2026-05-04

### Maintenance

- maint: add release-it
- maint: Added CHANGELOG
## [Unreleased]

### Maintenance

- maint: add release-it
## [0.1.4-rc.1] - 2026-05-04

### Added

- Step 1 (Round 5): GitHub release-notes template + PR-review fixes (#2) by @simensen in [#2](https://github.com/procrastivity/duo/pull/2)

### Fixed

- Round 3 (config and spawn) close-out: archive workplan/roadmap + retro by @simensen
- config: default to stdio when no file; clearer error when empty (#3) by @simensen in [#3](https://github.com/procrastivity/duo/pull/3)

### Infrastructure

- backlog: mark packaging Channels 1-3 shipped by @simensen

### Maintenance

- maint: archived some docs by @simensen

### Other

- docs: archive bun binaries-related docs by @simensen
- Decouple MCP stdio startup from Solo connection by @simensen
- Output Solo URL by @simensen
## [0.1.4-rc.0] - 2026-05-04

### Added

- Decouple release process into project-owned notes/release-process.md by @simensen
- Add manual-testing runbook under notes/manual-testing/ by @simensen
- Add per-exercise driver scripts under notes/manual-testing/scripts/ by @simensen
- Verify bind_session_process premise; add handoff spec by @simensen
- Resolve project/process scope at SoloClient.connect() by @simensen
- Update manual-testing runbook for connect-time scope resolution by @simensen
- Add CLI router + foundation (citty, connect helper, output) by @simensen
- Add CLI commands: agent, proc, project, whoami, doctor, version, config by @simensen
- Step 1 · Tasks 4-6: verify bundle, update PUBLISHING.md, record size by @simensen
- Round 1 planning: roadmap-1, step-01 workplan, backlog #223 entry by @simensen
- Step 1 · Task 1+2: add packages.duo flake output (Channel 3 Nix) by @simensen
- docs: Add bootstrap prompt examples and documentation to README by @simensen
- feat: XDG config compliance, Solo MCP auto-detection, and agent spawn bootstrap prompt by @simensen
- feat: add Bun-compiled macOS binaries (Channel 2, Step 1) by @simensen
- Step 2: Add release-bin.yml workflow for macOS binaries by @simensen
- Step 2: Add xattr documentation for unsigned macOS binaries by @simensen

### Changed

- Round 1 (npm bundle) close-out: archive workplan/roadmap + retro by @simensen
- Round 2 (Nix flake) close-out: archive workplan/roadmap + retro by @simensen

### Documentation

- Document connect-time scope resolution in README + PRD by @simensen
- README: document CLI surface; MCP client config now invokes 'duo mcp' by @simensen

### Fixed

- Correct smoke-check expectations in 00-setup.md (Duo doesn't self-exit) by @simensen
- Use timeout+sleep in raw JSON-RPC drivers; flag Solo-handshake bug by @simensen
- Fix SoloClient handshake: send initialize + notifications/initialized by @simensen

### Maintenance

- maint: move around notes and include new proposals by @simensen
- Step 1 · Tasks 2+3: switch duo to single-file esbuild bundle by @simensen
- chore: bump version to 0.1.4 for rc tag coordination by @simensen

### Other

- Archive solo-orchestrator-companion-intake.md (Roadmap 1 shipped) by @simensen
- Stop threading project scope through spawn_agent tool by @simensen
- docs: Recorded plans for CLI control plane by @simensen
- Inject client.projectId into callTool when caller omits it by @simensen

### Removed

- Drop solo.processId/projectId YAML fields; switch IDs to integers by @simensen
## [0.1.3] - 2026-05-03

### Maintenance

- maint: bumped version by @simensen
## [0.1.2] - 2026-05-02

### Maintenance

- maint: bump version by @simensen
## [0.1.1] - 2026-05-02

### Maintenance

- maint: Update CI nodejs versions (#1) by @simensen in [#1](https://github.com/procrastivity/duo/pull/1)
## [0.1.0] - 2026-05-02

### Added

- Rewrite orchestrator entrypoint prompts as router model by @simensen
- Step 2: Add tier classifier, resolver, MCP tools, and server integration by @simensen
- Step 2 follow-up: Document Solo timer patterns and mcp-cli anti-pattern in playbook by @simensen
- Add build artifacts to .gitignore by @simensen
- Add Step 2 retrospective entry (2026-05-02) by @simensen
- Step 3: spawn_agent MCP tool integration and project scope by @simensen
- Add Step 3 retrospective to project-planning-workflow-notes by @simensen
- Add step-04-workplan.md by @simensen
- Implement Step 4: YAML policy overrides and structured logging (Tasks 1-6, 229 tests passing) by @simensen
- Add Step 4 retrospective to project-planning-workflow-notes by @simensen
- Step 5 · Task 4: Add CI and release workflow pipelines by @simensen
- Step 5: Documentation, packaging, and adoption setup by @simensen
- Step 5 follow-up: Fix ESM imports, clean build artifacts, untrack egg-info by @simensen
- Mark Round 1 shipped in roadmap-1.md by @simensen

### Changed

- Mark Step 2 complete and update roadmap status by @simensen

### Maintenance

- Add Step 5 retro and Round 1 retro to project-planning-workflow-notes by @simensen

### Other

- Initialize project by @simensen
- Archive step-02-workplan.md by @simensen
- Untrack stray duo.egg-info build artifacts by @simensen
- Archive step-03-workplan.md by @simensen
- Mark Step 4 active in roadmap-1 by @simensen
- Archive step-04-workplan.md source (now in archive/) by @simensen
- Mark Step 5 active in roadmap-1 by @simensen
- Archive roadmap-1.md (Round 1 complete) by @simensen
- Archive step-05-workplan.md by @simensen

### Removed

- Remove step-01-workplan.md source (now in archive/) by @simensen
- Remove step-03-workplan.md source (now in archive/) by @simensen

