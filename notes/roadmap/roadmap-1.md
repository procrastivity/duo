# Roadmap 1 — duo packaging: npm esbuild bundle (Channel 1)

**Project**: Duo
**Status**: active
**Started**: 2026-05-03
**Proposal**: `notes/proposals/duo-packaging-npm-bundle.md`
**Intake**: `notes/proposals/duo-packaging-npm-bundle-intake.md`

> **Note**: This is the project's *first real round*. The `notes/roadmap/archive/roadmap-1.md` file is a placeholder/template from prior scaffolding work and does not represent shipped scope. There is no numbering collision because the active file lives in `notes/roadmap/` and the placeholder lives in `notes/roadmap/archive/`; future ronds may need to renumber the archived placeholder if it persists.

---

## Summary

Replace `duo`'s current multi-file `tsc`-emitted runtime with a single-file ESM esbuild bundle published as the package `bin` (`dist/duo.mjs`). Pure build-pipeline rewire: smaller install, faster cold start, simpler `files` manifest, no user-visible CLI behavior change. This is one of three sibling packaging channels (npm bundle / Bun binaries / Nix flake) and is the foundation Channel 3 (Nix) consumes; Channel 2 (Bun) is independent.

This round contains **one well-bounded step**, lifted verbatim from the intake.

---

## Step 1 — Switch `duo` to a single-file esbuild bundle

**Goal**: Ship `dist/duo.mjs` as the npm `bin` entrypoint, replacing the multi-file `tsc` runtime artifact, with zero CLI behavior delta.

**Workplan**: `notes/roadmap/step-01-workplan.md`

**Shipping criteria** (lifted from proposal §Roadmap shape and intake):

- [ ] `esbuild` added to `devDependencies`.
- [ ] `npm run build` produces `dist/duo.mjs` with `#!/usr/bin/env node` shebang and executable bit set.
- [ ] `node dist/duo.mjs --help` prints CLI help.
- [ ] `node dist/duo.mjs whoami` runs end-to-end (config-loader dynamic-import site resolves, exits cleanly).
- [ ] `npm pack --dry-run` lists exactly `dist/duo.mjs`, `LICENSE`, `package.json`, `README.md` — no `src/`, no `node_modules`, no test files. (`README.md` is npm-mandated: npm forcibly includes a root `README.md` in every published tarball regardless of the `files` allowlist; we accept this rather than delete the file because it serves as the npm package landing page. Decision: Option A per researcher's blocking question, approved by human 2026-05-03.)
- [ ] `npx ./procrastivity-duo-*.tgz whoami` works in a scratch directory.
- [ ] `npm run test` still green.
- [ ] `prepublishOnly` chain succeeds end-to-end.
- [ ] Dynamic-import audit complete; only known site is `src/server.ts:133` (or any new sites refactored / marked external).
- [ ] `docs/PUBLISHING.md` step 8 updated to reference `dist/duo.mjs`.

**Deferred decisions resolved in this step**:

- Drop `build:types` (no `.d.ts` emission) — duo is a CLI, not a library; revisit if a downstream consumer ever imports from `@procrastivity/duo` programmatically.
- Update `docs/PUBLISHING.md` in the **same PR** — step 8 explicitly names `dist/index.js`, which no longer exists post-rewire.

**New deps**: `esbuild` (dev only).

**Risk**: low. Pure build-pipeline rewire; runtime behavior unchanged. Rollback is a one-commit revert. Primary failure mode is an undiscovered dynamic-import path silently missing from the bundle — mitigated by the grep guardrail and the `whoami`/test smokes.

---

## Out of scope (sibling channels)

- Channel 2 — Bun-compiled macOS binaries (`notes/proposals/duo-packaging-bun-binaries.md`). Independent of this round.
- Channel 3 — Nix flake `packages.duo` (`notes/proposals/duo-packaging-nix-flake.md`). Depends on this round shipping `dist/duo.mjs`.
- Channel 4 — Install UX / GitHub Releases / curl|sh / Homebrew (`notes/proposals/duo-packaging-install-ux.md`). Depends on Channel 2.

## Open optional refinements (do not block round)

- Wire the dynamic-import grep into CI as a guardrail. Builder discretion during Step 1; if added, becomes an extra shipping criterion.
