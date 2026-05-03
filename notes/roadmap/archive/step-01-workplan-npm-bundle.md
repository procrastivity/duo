# Step 1 Workplan — Switch `duo` to a single-file esbuild bundle

**Status**: locked (researcher validation pass complete; ready for coordinator to spawn builders)
**Roadmap**: `notes/roadmap/roadmap-1.md`
**Proposal**: `notes/proposals/duo-packaging-npm-bundle.md`
**Intake**: `notes/proposals/duo-packaging-npm-bundle-intake.md`
**Source coverage**: see Coverage Map in intake (all source items map to this step).

---

## Scope

- **Goal**: Ship `dist/duo.mjs` as the npm `bin` entrypoint, replacing the multi-file `tsc` runtime artifact, with zero CLI behavior delta.
- **Out of scope**: removing the Node.js runtime requirement (Channel 2); renaming `bin` or adding bins; module-format changes (stays ESM); CI dynamic-import guardrail (optional refinement).

---

## Inputs the coordinator/researcher should treat as authoritative

- `notes/proposals/duo-packaging-npm-bundle.md` — full proposal (behavior boundary, edge cases, integration points, implementation notes, risks).
- `notes/proposals/duo-packaging-npm-bundle-intake.md` — intake with shipping criteria already lifted, deferred decisions resolved, coverage map, recommendation = start step now.
- `notes/roadmap/roadmap-1.md` — round-level summary and shipping criteria (mirrors intake).
- Current state cross-check: `package.json`, `docs/PUBLISHING.md`, `src/server.ts:133` (the known dynamic-import site), `src/index.ts` (entry), `.npmignore` (root).

---

## Findings from validation pass (researcher)

### 1. Dynamic-import audit confirmed

`grep -rn "await import(" src/` returns exactly one site:

```
src/server.ts:133:  const { loadConfig } = await import("./cli/config-loader.js");
```

A broader sweep for any `import(` call (non-await dynamic, runtime) also returns only that site. Proposal claim holds. **Task 1 expected output**: confirm single-site result, no per-site refactor needed, no `external` markers required. The `./cli/config-loader.js` path is a static relative literal — esbuild will inline it cleanly with `--bundle`.

### 2. Esbuild flag set confirmed

Entry is `src/index.ts`:

```ts
#!/usr/bin/env node
import { run } from "./cli/index.js";

await run();
```

Top-level `await` is present. Flags from proposal §Implementation notes:

```
esbuild src/index.ts \
  --bundle \
  --platform=node \
  --format=esm \
  --target=node22 \
  --outfile=dist/duo.mjs \
  --banner:js="#!/usr/bin/env node"
```

Validation:
- `--format=esm` + `--target=node22` preserves top-level `await`. ✓
- `--platform=node` keeps Node built-ins external. ✓
- Engines field in `package.json` = `node >=22.0.0`. `--target=node22` aligns. ✓
- All runtime deps are pure JS (`@modelcontextprotocol/sdk`, `citty`, `execa`, `pino`, `yaml`, `zod`) — bundling is safe; no native modules to mark `external`. ✓
- `pino.destination(2)` is sync; no worker-thread transport, so bundling does not break logging. ✓

**Recommended optional flag additions** (builder discretion; do not block):
- `--legal-comments=none` — drop inlined third-party license headers from the bundle. Reduces bundle size noise without behavior change. LICENSE remains in tarball.
- **Skip `--minify`** — CLI binary; readability/debuggability matters more than the few hundred KB saved.
- **Skip `--sourcemap`** — would require shipping the map (and listing it in `files`); we want a clean `dist/duo.mjs`-only artifact. If a future regression requires sourcemap debugging, regenerate locally.

### 3. `npm pack` content surprise — README.md will sneak in

**This is a discussion point for the coordinator/orchestrator.** npm always includes a root `README.md` in published tarballs regardless of the `files` array (along with `package.json` and `LICENSE`). A clean `npm pack --dry-run` after the rewire will list **four** files, not three:

- `dist/duo.mjs`
- `LICENSE`
- `package.json`
- `README.md` ← always-included by npm

The current shipping criterion (lifted from proposal) reads:

> `npm pack --dry-run` lists exactly `dist/duo.mjs`, `LICENSE`, `package.json` — no `src/`, no `node_modules`, no test files.

This will fail as written, even with a correct `files` array, because npm forcibly includes README.

Two options (researcher does **not** choose; surfacing to coordinator):

- **Option A (recommended)**: amend the shipping criterion in `roadmap-1.md` to add `README.md` to the expected list. Rationale: README is desirable on the npm package page anyway; fighting npm's always-include behavior is pure friction.
- **Option B**: explicitly exclude README via empty README workaround or by omitting the file entirely (delete it from the repo root) — strongly discouraged; loses the npm package landing page.

There is no option to keep the criterion *and* keep README, short of upstream npm changes.

**Additionally**: a `.npmignore` exists at the repo root with a permissive `!*.md` rule. After the `files`-array rewire, `.npmignore` is redundant (the `files` allowlist takes precedence) and confusing. Recommend deleting it as part of Task 3 to avoid future drift, but flag for coordinator confirmation.

### 4. Other pack-content notes

- The current tarball (pre-rewire, v0.1.3) includes 131 files including all `.d.ts`, `.js.map`, `.d.ts.map`. Post-rewire, this collapses to 3 (or 4 if we accept README) — a meaningful tarball-size reduction, in line with proposal §Risks ("1–3 MB" estimated bundle, well under prior aggregate).
- `AGENTS.md` and `CLAUDE.md` at repo root do **not** appear in the current tarball even though `.npmignore` whitelists `*.md`, because the `files` array `["dist","LICENSE"]` takes precedence. Same will be true after the rewire — only README sneaks past.

---

## Locked task list

### Task 1 — Dynamic-import audit + result documentation
Already validated by researcher (single site at `src/server.ts:133`). Builder action: re-run `grep -rn "await import(" src/` immediately before/after the build script lands as a guardrail; commit the grep output (or its negative-fingerprint expectation) into the PR description for posterity. No code changes expected unless the grep surfaces a new site introduced between researcher validation and build.

### Task 2 — Add `esbuild` devDep + `build:bundle` script
- `npm install --save-dev esbuild`.
- Add `scripts.build:bundle` exactly as proposal §Implementation notes (the multi-line esbuild invocation can be flattened to a single-line shell command in `package.json`). Append `&& chmod +x dist/duo.mjs`.
- Optional: add `--legal-comments=none`. Skip `--minify` and `--sourcemap`.

### Task 3 — `package.json` rewire (and `.npmignore` cleanup)
- `bin.duo` → `./dist/duo.mjs`.
- `files` → `["dist/duo.mjs", "LICENSE"]`.
- `scripts.build` → `npm run build:bundle` (drop `tsc -p tsconfig.json`; drop `build:types` per resolved deferred decision; `typecheck` script stays as-is for `tsc --noEmit`).
- `scripts.prepublishOnly` → `npm run build && npm run test` (already correct; verify chain is `build:bundle` → `test`).
- **Delete** root `.npmignore` (now redundant; `files` allowlist supersedes it). Surface to coordinator before deletion if any concern.

### Task 4 — Local build + smoke verification
Run, in order, against a clean `dist/`:
1. `npm run build` → produces `dist/duo.mjs` with shebang and `+x` bit (`ls -l dist/duo.mjs`, `head -1 dist/duo.mjs`).
2. `node dist/duo.mjs --help` → CLI help prints.
3. `node dist/duo.mjs whoami` → exits cleanly; exercises the `src/server.ts:133` dynamic-import path via config-loader.
4. `npm pack --dry-run` → lists the expected files (see Task 6 / shipping-criterion reconciliation).
5. `npm pack && npx ./procrastivity-duo-*.tgz whoami` in a scratch directory (`/tmp/duo-smoke/`) → works.
6. `npm run test` → green.
7. `npm run prepublishOnly` → end-to-end green.

### Task 5 — `docs/PUBLISHING.md` update
Step 8, line 220 currently reads: "The `duo` bin entrypoint (from `dist/index.js`) is invoked." Replace `dist/index.js` with `dist/duo.mjs`. Also audit step 5 line 122 ("Should list: `dist/`, `README.md`, `LICENSE`, `package.json`.") — update to the post-rewire expectation (`dist/duo.mjs` + whatever the reconciled shipping criterion ends up being). Grep the rest of the file for `dist/index.js` and `dist/` references and update accordingly.

### Task 6 — Bundle-size note + shipping-criterion reconciliation
- Measure final `dist/duo.mjs` size (`wc -c`, `du -h`); record in PR description (proposal §Risks calls this out).
- **RESOLVED 2026-05-03**: Option A approved by human; `roadmap-1.md` shipping criterion amended to list four files (`dist/duo.mjs`, `LICENSE`, `package.json`, `README.md`). README is npm-mandated. Builder verifies `npm pack --dry-run` output matches that four-file set.

---

## Suggested batching (locked)

| Batch | Tasks | Rationale |
|---|---|---|
| Batch A | Task 1 | Audit guardrail; cheap, runs before bundle work; revalidates researcher's finding immediately before code changes land. |
| Batch B | Task 2, Task 3 | Build script + `package.json` (+ `.npmignore` deletion) rewire; coherent single builder unit. Cannot land Task 3 without Task 2 because `npm run build` would break mid-PR. |
| Batch C | Task 4, Task 5, Task 6 | Verification + docs + size-note + shipping-criterion reconciliation; gated on Batch B; Task 6 may need orchestrator/human acknowledgement on README criterion before merge. |

Builders for Batches A and B can be the same process (small footprint each); Batch C is verification-heavy and likely a separate builder.

---

## Definition of Done

All shipping criteria from `notes/roadmap/roadmap-1.md` checked off — pending coordinator/orchestrator decision on the README criterion (see Findings §3 and Task 6). Round is single-step, so step DoD == round DoD.

---

## Open optional refinements (do not block)

- CI guardrail for the dynamic-import grep — builder discretion. If added, surface to coordinator to add as an extra shipping criterion in `roadmap-1.md`.
- `--legal-comments=none` esbuild flag — builder discretion; cosmetic.

---

## Blocking question for coordinator/orchestrator — RESOLVED 2026-05-03

**Resolution**: Option A approved by human. `roadmap-1.md` shipping criterion amended to list four files (`dist/duo.mjs`, `LICENSE`, `package.json`, `README.md`). README inclusion is npm-platform-mandated, not a project choice. No further blocker on Task 6 closure from this issue.
