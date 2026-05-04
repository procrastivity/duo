# Roadmap 3 — Configuration and Spawn Infrastructure

**Project**: Duo
**Status**: in-progress
**Started**: 2026-05-03
**Round Focus**: Three infrastructure/config improvements as one combined step

---

## Summary

Three focused improvements to configuration handling and agent spawning:

1. **XDG Base Directory compliance** — Use XDG_CONFIG_HOME for config file lookup (standardized cross-platform convention)
2. **Agent spawn with optional prompt** — Allow passing an optional bootstrap prompt argument when spawning an agent
3. **Auto-detect Solo MCP path** — Infer the Solo MCP executable path from known locations if not explicitly configured

These are grouped as **one step with three parallel task streams** to move infrastructure improvements forward efficiently.

---

## Step 1 — Config/Spawn Infrastructure Improvements

**Goal**: Improve configuration portability and agent spawning flexibility by implementing XDG compliance, optional spawn prompts, and MCP path auto-detection.

**Workplan**: `notes/roadmap/step-01-workplan.md` (to be generated)

**Related todos**:
- solo://proj/6/todo/follow-xdg-base-dire--247
- solo://proj/6/todo/ability-to-pass-an-o--246
- solo://proj/6/todo/solo-mpc-path-should--245

**Shipping criteria**:

- [ ] Config file lookup respects XDG_CONFIG_HOME if set, falls back to ~/.config/duo/config.yaml
- [ ] Agent spawn accepts optional final `--prompt` argument (or equivalent) as bootstrap input
- [ ] Solo MCP path auto-detection checks `/Applications/Solo.app/Contents/MacOS/mcp` on macOS
- [ ] Explicit config option for MCP path is preserved (auto-detection is fallback only)
- [ ] All changes tested and documented
- [ ] No regressions in existing config/spawn behavior

**Deferred decisions**:

- Linux/Windows MCP path auto-detection paths (noted as "as other OS options become available")
- Exact API/syntax for optional spawn prompt (coordinator/researcher to determine)
- Config schema changes (if needed) and migration strategy

**New deps**: None expected

**Risk**: Low. These are isolated configuration improvements with no external API or user-facing breaking changes.

---

## Out of scope

- Changes to config file format or schema (unless required by XDG work)
- Installer/packaging impact (separate channels in packaging roadmap)
- Solo UI/UX changes (Solo's domain)

---

## Notes

This round unblocks future work that may depend on flexible agent spawning and standardized config paths. All three items were identified as medium priority and have clear, bounded scopes.
