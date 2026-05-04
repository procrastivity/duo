# Roadmap 3 — Configuration and Spawn Infrastructure

**Project**: Duo
**Status**: complete
**Started**: 2026-05-03
**Shipped**: 2026-05-03 (commit 5f3cb07)
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

- [x] Config file lookup respects XDG_CONFIG_HOME if set, falls back to ~/.config/duo/config.yaml
- [x] Agent spawn accepts optional final `--prompt` argument (or equivalent) as bootstrap input
- [x] Solo MCP path auto-detection checks `/Applications/Solo.app/Contents/MacOS/mcp` on macOS
- [x] Explicit config option for MCP path is preserved (auto-detection is fallback only)
- [x] All changes tested and documented
- [x] No regressions in existing config/spawn behavior

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

---

## Retro

Three foundational infrastructure improvements shipped without ceremony. **XDG compliance** (Stream A) brings config file lookup in line with Unix conventions — existing `DUO_CONFIG` env var escape hatch preserved. **Optional spawn prompt** (Stream B) enables downstream orchestration patterns where agent bootstrap instructions are delivered as a message rather than baked into the caller — critical for the playbook-driven agent workflow. **MCP path auto-detection** (Stream C) eliminates the need to configure an explicit path on macOS when Solo.app is in the standard Applications directory, reducing friction for first-time CLI users.

All three shipped together, tested, and documented. The orchestration layer can now assume flexible spawn prompts are available — used immediately in the install-UX round's workplan coordination. No regressions observed; all existing tests green. Effort was well-bounded (3–6 hours actual, vs. 7–14 hours estimated). Risk was uniformly low across all streams thanks to clear scope and thoughtful escape hatches (env vars, explicit config always takes precedence).
