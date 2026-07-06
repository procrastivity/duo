# Changelog

All notable changes to this project will be documented in this file.

## [0.2.3] - 2026-07-06

### Added

- feat(nix): inject DUO_GIT_SHA so from-source builds report a real git_sha (#34) by @simensen in [#34](https://github.com/procrastivity/duo/pull/34)

### Maintenance

- chore(nix): update prebuilt manifest and npmDepsHash for v0.2.2 by @github-actions[bot]
- chore: retire in-repo prebuilt path (duo-bin) — Round 3 (#35) by @simensen in [#35](https://github.com/procrastivity/duo/pull/35)
- feat: clast-style contrib/release, retire release-it — Round 4 (#36) by @simensen in [#36](https://github.com/procrastivity/duo/pull/36)
## [0.2.2] - 2026-07-06

### Added

- ci(release): recompute npmDepsHash post-tag by @simensen

### Fixed

- fix(nix): correct npmDepsHash for v0.2.1 by @simensen

### Maintenance

- chore(nix): update prebuilt binary manifest for v0.2.1 by @github-actions[bot]
## [0.2.1] - 2026-07-06

### Added

- feat(nix): add prebuilt standalone binary install target (#29) by @simensen in [#29](https://github.com/procrastivity/duo/pull/29)
- fix(server): wrap list_presets result in MCP content envelope (#30) by @simensen in [#30](https://github.com/procrastivity/duo/pull/30)
## [0.2.0] - 2026-07-04

Replaces the tier-based agent-selection model with explicit **presets** and **provider** toggles. This is a pre-1.0 clean break with no compatibility aliases — read [Migrating from tiers](README.md#migrating-from-tiers-pre-10-breaking-change) before upgrading.

### ⚠️ Breaking changes

- **Agent selection is now preset-based, not tier-based.** The `small`/`medium`/`large` tiers and the command/name token classifier that inferred them are gone. You declare, per preset, exactly which `agent_tool_id`(s) it may use.
- **MCP tools renamed:** `list_agent_tiers` → `list_presets`, `resolve_agent_tool` → `resolve_preset`, `spawn_agent` → `launch_agent`. The `tier` argument is now a `preset` string.
- **CLI renamed:** `duo agent spawn <tier>` → `duo agent launch <preset>`; the positional on `launch`/`resolve` is now a preset name.
- **Policy subsystem removed:** `duo.policy.yaml`, the `DUO_POLICY` env var, and the `command_tokens` / `selection` config sections no longer exist. Presets are declared under a `presets:` key in `config.yaml`, resolved from the XDG location (`~/.config/duo/config.yaml` or `DUO_CONFIG`).

### Added

- **Presets:** `presets:` config schema plus `duo config preset add|list|remove` CLI verbs for managing them offline.
- **Providers:** `list_providers` and `set_provider_enabled` MCP tools, `duo config provider enable|disable|list` CLI verbs, and an `--avoid-provider` flag on `launch`/`resolve`. Provider enabled-state persists as lock-free files under `$XDG_STATE_HOME/duo/providers/`.
- **Per-launch `extra_args`:** callers can append extra arguments through to the spawned agent process.

### Changed

- `resolve_preset` / `launch_agent` always report the selected provider and expose `avoid_provider`.

### Removed

- Deleted the tier classifier, the legacy resolver, and the entire policy subsystem.

### Fixed

- Addressed preset/provider review findings; fixed smoke-test tool names and the package description for the preset rename.

### Documentation

- README rewrite with a "Migrating from tiers" section, plus refreshed manual-testing docs ([#18](https://github.com/procrastivity/duo/pull/18), [#19](https://github.com/procrastivity/duo/pull/19)).
- Added the Solo CLI vs MCP backend decision record and the Duo project auto-scoping design record ([#22](https://github.com/procrastivity/duo/pull/22)).

### Dependencies

- Bumped npm deps: `yaml`, `esbuild` (security), `release-it`, `vitest`, `@types/node` ([#28](https://github.com/procrastivity/duo/pull/28)).
- Bumped `softprops/action-gh-release` 3.0.0 → 3.0.1 ([#26](https://github.com/procrastivity/duo/pull/26)).

### Maintenance

- Stopped tracking local Claude Code tooling; added a Linear project reference.

## [0.1.8] - 2026-05-09

### Documentation

- docs(manual-testing): refresh 00-setup for bundle path and XDG config (#17) by @simensen in [#17](https://github.com/procrastivity/duo/pull/17)

### Maintenance

- chore: wire up release body
## [0.1.7] - 2026-05-06

### Added

- fix: inject version into bun-compiled binary (#16) by @simensen in [#16](https://github.com/procrastivity/duo/pull/16)
## [0.1.6] - 2026-05-05

### Changed

- ci: pin GitHub Actions to commit SHAs and bump majors (#13) by @simensen in [#13](https://github.com/procrastivity/duo/pull/13)

### Maintenance

- chore: release process upgrade (#4) by @simensen in [#4](https://github.com/procrastivity/duo/pull/4)
- chore: upgrade deps (#5) by @simensen in [#5](https://github.com/procrastivity/duo/pull/5)
- chore: add dependabot config for npm and github-actions by @simensen
- chore: modernize deps and toolchain (Node 24 LTS + Bun pin) (#14) by @simensen in [#14](https://github.com/procrastivity/duo/pull/14)
- chore: drop Node 22 support (#15) by @simensen in [#15](https://github.com/procrastivity/duo/pull/15)
- chore: Update .gitignore by @simensen
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

