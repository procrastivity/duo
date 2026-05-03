# duo packaging — npm esbuild bundle (Channel 1)

**Status**: draft
**Date**: 2026-05-03
**Profile**: solo-local-cli
**Source input**: `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 1)

## Summary

Replace `duo`'s current multi-file `tsc`-emitted runtime artifact with a single-file ESM esbuild bundle published as the package's `bin`. This is the primary npm/`npx` channel: smaller install, faster cold start, simpler `files` manifest. `tsc` stays available for type checking; whether to keep emitting `.d.ts` is an open question (CLI, not a library).

## Behavior boundary

After this lands:

- `npx @procrastivity/duo` and `npm i -g @procrastivity/duo && duo` both invoke a single bundled file at `dist/duo.mjs` with a `#!/usr/bin/env node` shebang.
- The published tarball contains only `dist/duo.mjs`, `LICENSE`, and `package.json`. No `src/`, no per-module `dist/cli/*.js`, no `node_modules`.
- Engine requirement remains `node ≥ 22.0.0`.
- All current CLI subcommands (`whoami`, `version`, `agent`, `proc`, `project`, `doctor`, `config`, `mcp`) continue to work identically — this is a build-pipeline change with zero user-visible behavior delta beyond perf and install size.

Out of scope for this channel:

- Removing the Node.js runtime requirement (that's Channel 2).
- Changing `bin` name or adding additional bins.
- Migrating to a different module format (the bundle stays ESM).

## Inputs / outputs

**Inputs**: `src/index.ts` entry; full `src/` tree resolved by esbuild's bundler; runtime `dependencies` get inlined; `devDependencies` are excluded.

**Outputs**:

- `dist/duo.mjs` — single bundled ESM file, executable bit set, with shebang banner.
- (optional, TBD) `dist/*.d.ts` if `build:types` is retained.

**Published artifact**: `@procrastivity/duo` tarball on the npm registry. `npm pack --dry-run` should list exactly `dist/duo.mjs`, `LICENSE`, `package.json`.

## Edge cases & error handling

- **Dynamic imports**: a single `await import("./cli/config-loader.js")` exists at `src/server.ts:133`. The path is static and relative; esbuild bundles it correctly with `--bundle`. No `external` marker needed. Verify post-build that the bundle still resolves config-loader behavior (covered by `whoami` smoke test, which loads config).
- **Native modules**: none. All deps (`@modelcontextprotocol/sdk`, `citty`, `execa`, `pino`, `yaml`, `zod`) are pure JS — bundling is safe.
- **`pino` transports**: `pino.destination(2)` is synchronous; no worker-thread transport that would break under bundling.
- **Top-level `await`**: `src/index.ts` uses `await run()` at top level. esbuild with `--format=esm --target=node22` preserves this.
- **Bundle size**: estimated 1–3 MB. Well under npm tarball limits; document the size delta in PR.
- **Errors**: bundling failures surface during `npm run build`; CI catches before publish via `prepublishOnly`.

## Integration points

- **`package.json`** — `bin`, `files`, `scripts.build`, `scripts.prepublishOnly` all change. See Implementation Notes for the full diff.
- **`docs/PUBLISHING.md`** — references `dist/index.js` in step 8's smoke test ("the `duo` bin entrypoint (from `dist/index.js`) is invoked"). Needs an edit to point at `dist/duo.mjs`.
- **`.github/workflows/release.yml`** (referenced from PUBLISHING.md, not yet read) — no change expected; the workflow runs `npm run build` and `npm publish`, both of which still work post-rewire.
- **Channel 3 (Nix flake)** — depends on this channel's `build:bundle` script existing and producing `dist/duo.mjs`. Nix derivation wraps `dist/duo.mjs` with `makeWrapper` against `nodejs_24`.
- **Channel 2 (Bun binaries)** — independent; Bun bundles its own way from `src/index.ts` directly, doesn't consume `dist/duo.mjs`.

## Implementation notes (preserved from source plan)

**Add devDependency**: `esbuild`.

**New script**: `build:bundle`:

```
esbuild src/index.ts \
  --bundle \
  --platform=node \
  --format=esm \
  --target=node22 \
  --outfile=dist/duo.mjs \
  --banner:js="#!/usr/bin/env node"
```

Followed by `chmod +x dist/duo.mjs`.

**`package.json` edits**:

- `bin.duo` → `./dist/duo.mjs`
- `files` → `["dist/duo.mjs", "LICENSE"]` (drop everything else)
- `scripts.build` → run `build:bundle` (keep `build:types` if `.d.ts` emission is wanted; otherwise drop entirely)
- `scripts.prepublishOnly` → `npm run build && npm run test`

**Pre-merge audit**: spot-check `src/cli/commands/`, `src/tools/`, `src/transport/stdio.ts` for any dynamic `import()` of paths that bundling would break. If any dynamic loading exists beyond the known `src/server.ts:133` site, mark the affected modules as `external` or refactor to static imports.

**Negative-fingerprint grep** — after the bundle ships, this should be the *only* dynamic-import site:

```
grep -rn "await import(" src/
# expected: src/server.ts:133:  const { loadConfig } = await import("./cli/config-loader.js");
```

If the grep ever returns additional non-statically-resolvable paths, the bundle may silently miss code at runtime.

## Roadmap shape

### Step 1 — Switch `duo` to a single-file esbuild bundle

**Goal**: ship `dist/duo.mjs` as the npm `bin` entrypoint, replacing the multi-file `tsc` runtime artifact.

**Shipping criteria**:

- [ ] `esbuild` added to `devDependencies`.
- [ ] `npm run build` produces `dist/duo.mjs` with shebang and executable bit.
- [ ] `node dist/duo.mjs --help` prints CLI help.
- [ ] `node dist/duo.mjs whoami` runs end-to-end (loads config via the dynamic-import site, exits cleanly).
- [ ] `npm pack --dry-run` lists exactly `dist/duo.mjs`, `LICENSE`, `package.json`. No `src/`, no `node_modules`, no test files.
- [ ] `npx ./procrastivity-duo-*.tgz whoami` works in a scratch directory.
- [ ] `npm run test` still green.
- [ ] `prepublishOnly` chain succeeds.

**Deferred decisions resolved in this step**:

- Whether to keep `tsc`-emitted `.d.ts`. Default: drop `build:types` since this is a CLI, not a library; revisit if a downstream consumer ever imports from `@procrastivity/duo` programmatically.

**New deps**: `esbuild` (dev only).

**Risk**: low. Pure build-pipeline rewire; runtime behavior unchanged. The single dynamic-import site is statically resolvable. Rollback is a one-commit revert.

## Coverage map

| Source item (i-want-to-better-peppy-shamir.md, Channel 1) | Step | Status | Notes |
|---|---|---|---|
| Add esbuild devDependency | Step 1 | planned | |
| New `build:bundle` script | Step 1 | planned | flags lifted verbatim |
| `package.json` `bin` rewire | Step 1 | planned | |
| `package.json` `files` shrink | Step 1 | planned | |
| `scripts.build` rewire | Step 1 | planned | |
| `scripts.prepublishOnly` rewire | Step 1 | planned | |
| Dynamic-import audit | Step 1 | planned | only known site is `src/server.ts:133` |
| `npm pack` content verification | Step 1 | planned | shipping criterion |
| `npx ./tarball` smoke | Step 1 | planned | shipping criterion |

## Open questions

- Drop `build:types` entirely, or retain for hypothetical future programmatic consumers? (Default: drop.)
- Update `docs/PUBLISHING.md` step 8 in the same PR, or as a follow-up? (Recommend: same PR — the smoke test there names `dist/index.js` explicitly.)

## Risks

- **Bundle size for npm**: esbuild bundle of MCP SDK + deps is likely 1–3 MB, well under npm's limits. Document the actual size in the PR description for posterity.
- **Hidden dynamic imports**: low probability, but the grep above is the only thing standing between a green build and a runtime `Cannot find module` on a code path the smoke tests don't exercise. Add the grep to CI as a guardrail if paranoia warrants.
