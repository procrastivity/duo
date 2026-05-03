# Proposal Intake — duo packaging (npm esbuild bundle, Channel 1)

**Status**: draft
**Date**: 2026-05-03
**Intake inputs**:

- `notes/proposals/duo-packaging-npm-bundle.md` — drafted Channel 1 proposal (esbuild single-file bundle)
- `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 1) — original source plan referenced by the proposal (not re-read during intake; treated as already distilled into the proposal)
- `package.json`, `notes/roadmap/archive/` — current state cross-check

---

## Summary

Replace `duo`'s current multi-file `tsc`-emitted runtime with a single-file ESM esbuild bundle published as the package `bin` (`dist/duo.mjs`). Pure build-pipeline rewire: smaller install, faster cold start, simpler `files` manifest, no user-visible CLI behavior change. This is one of three sibling packaging channels (npm bundle / Bun binaries / Nix flake) and is the foundation Channel 3 (Nix) consumes; Channel 2 (Bun) is independent.

## Source Inputs

| Source | Type | Role in intake |
|---|---|---|
| `notes/proposals/duo-packaging-npm-bundle.md` | proposal | primary |
| `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 1) | idea / source plan | background (already distilled) |
| `package.json` | current state | supporting (verifies behavior boundary diff) |
| `notes/roadmap/archive/roadmap-1.md` + step-0N-workplan.md | shipped scope | supporting (no overlap with this work) |
| `notes/proposals/duo-packaging-{bun-binaries,nix-flake,install-ux}.md` | sibling channels | background (sequencing) |

## Candidate Outcomes

- Outcome: `npx @procrastivity/duo` and `npm i -g` install a single bundled file
  - Source: proposal §Behavior boundary
  - User-visible result: faster install, smaller tarball; CLI surface identical
  - Verification signal: `npm pack --dry-run` lists exactly `dist/duo.mjs`, `LICENSE`, `package.json`

- Outcome: All existing CLI subcommands continue to work after bundling
  - Source: proposal §Behavior boundary, §Edge cases (dynamic import at `src/server.ts:133`)
  - User-visible result: zero behavior delta for `whoami`, `version`, `agent`, `proc`, `project`, `doctor`, `config`, `mcp`
  - Verification signal: `node dist/duo.mjs whoami` end-to-end + `npm run test` green + `npx ./*.tgz whoami` smoke

- Outcome: Build pipeline produces an executable bundle with shebang
  - Source: proposal §Implementation notes
  - User-visible result: build artifact is directly executable
  - Verification signal: `node dist/duo.mjs --help` prints help; executable bit set

- Outcome: Channel 3 (Nix) unblocked
  - Source: proposal §Integration points
  - User-visible result: downstream Nix derivation has a stable artifact (`dist/duo.mjs`) to wrap
  - Verification signal: Channel 3 step plan can reference `build:bundle` script as a dependency

## Proposed Roadmap Shape

The proposal already specifies a single, well-bounded step. Intake recommendation: adopt as-is, lifted verbatim into a roadmap step with no decomposition.

### Step 1 — Switch `duo` to a single-file esbuild bundle

**Goal**: Ship `dist/duo.mjs` as the npm `bin` entrypoint, replacing the multi-file `tsc` runtime artifact, with zero CLI behavior delta.

**Shipping criteria** (lifted from proposal):

- [ ] `esbuild` added to `devDependencies`.
- [ ] `npm run build` produces `dist/duo.mjs` with `#!/usr/bin/env node` shebang and executable bit set.
- [ ] `node dist/duo.mjs --help` prints CLI help.
- [ ] `node dist/duo.mjs whoami` runs end-to-end (config-loader dynamic-import site resolves, exits cleanly).
- [ ] `npm pack --dry-run` lists exactly `dist/duo.mjs`, `LICENSE`, `package.json` — no `src/`, no `node_modules`, no test files.
- [ ] `npx ./procrastivity-duo-*.tgz whoami` works in a scratch directory.
- [ ] `npm run test` still green.
- [ ] `prepublishOnly` chain succeeds end-to-end.
- [ ] Dynamic-import audit complete; only known site is `src/server.ts:133` (or any new sites refactored / marked external).
- [ ] `docs/PUBLISHING.md` step 8 updated to reference `dist/duo.mjs`.

**Deferred decisions resolved in this step**:

- Decision: Drop `build:types` (no `.d.ts` emission)
  - Source: proposal §Open questions (default: drop, since duo is a CLI not a library)
  - Why this step: bundling change is the natural moment to retire the multi-file emit pipeline entirely

- Decision: Update `docs/PUBLISHING.md` in the same PR
  - Source: proposal §Open questions (recommended: same PR)
  - Why this step: step 8 explicitly names `dist/index.js`, which no longer exists post-rewire

**New deps**:

- `esbuild` (dev only)

**Risk**: low. Pure build-pipeline rewire; runtime behavior unchanged. Rollback is a one-commit revert. Primary failure mode is an undiscovered dynamic-import path silently missing from the bundle — mitigated by the grep guardrail and the `whoami`/test smokes.

**Source coverage**:

- `duo-packaging-npm-bundle.md` §Implementation notes → all `package.json` and script edits
- `duo-packaging-npm-bundle.md` §Edge cases → dynamic-import audit + size note
- `duo-packaging-npm-bundle.md` §Integration points → PUBLISHING.md edit
- `duo-packaging-npm-bundle.md` §Roadmap shape → shipping criteria verbatim

## Coverage Map

| Source item | Proposed step | Status | Notes |
|---|---|---|---|
| Add `esbuild` devDependency | Step 1 | planned | |
| New `build:bundle` script (esbuild flags lifted verbatim) | Step 1 | planned | |
| `package.json` `bin.duo` → `./dist/duo.mjs` | Step 1 | planned | |
| `package.json` `files` shrink to `["dist/duo.mjs", "LICENSE"]` | Step 1 | planned | |
| `scripts.build` rewire | Step 1 | planned | |
| `scripts.prepublishOnly` chain | Step 1 | planned | |
| Dynamic-import audit (`grep -rn "await import("`) | Step 1 | planned | only known site: `src/server.ts:133` |
| `npm pack` content verification | Step 1 | planned | shipping criterion |
| `npx ./tarball` smoke test | Step 1 | planned | shipping criterion |
| `docs/PUBLISHING.md` step 8 edit | Step 1 | planned | named in proposal §Integration points |
| Drop `build:types` / `.d.ts` emission | Step 1 | planned (deferred-decision resolved) | default per proposal §Open questions |
| Channel 2 (Bun binaries) | — | out-of-scope | sibling proposal, independent |
| Channel 3 (Nix flake) | — | out-of-scope | depends on Step 1's `build:bundle` artifact |
| Bundle-size documentation in PR | Step 1 | planned | proposal §Risks |
| (optional) CI guardrail for dynamic-import grep | — | deferred | proposal §Risks; "if paranoia warrants" |

## Deferred / Out-of-Scope Items

- Item: Removing the Node.js runtime requirement
  - Source: proposal §Behavior boundary (out of scope)
  - Reason: Channel 2 (Bun binaries) territory
  - Revisit trigger: Channel 2 intake

- Item: Renaming the `bin` or adding additional bins
  - Source: proposal §Behavior boundary (out of scope)
  - Reason: orthogonal to bundling
  - Revisit trigger: install-UX work (`duo-packaging-install-ux.md`)

- Item: Different module format than ESM
  - Source: proposal §Behavior boundary (out of scope)
  - Reason: not motivated; ESM works
  - Revisit trigger: downstream consumer constraint

- Item: CI guardrail for dynamic-import grep
  - Source: proposal §Risks
  - Reason: "if paranoia warrants"; smoke tests cover the realistic risk
  - Revisit trigger: a runtime "Cannot find module" regression slipping past tests

## Open Questions

None block confident planning. Both questions raised in the proposal have defensible defaults and are flagged as "deferred decisions resolved in this step" above:

- ~~Drop `build:types` entirely?~~ — default: drop. (Resolved in Step 1.)
- ~~Update `docs/PUBLISHING.md` in same PR or follow-up?~~ — default: same PR. (Resolved in Step 1.)

Optional refinement (does NOT block start):

- Question: Should the dynamic-import grep be wired into CI as a guardrail (vs. left as a manual pre-merge check)?
  - Why it matters: prevents future contributors from adding a dynamic-import that silently breaks the bundle
  - Blocks roadmap? no
  - Suggested owner: builder discretion during Step 1; if added, becomes an extra shipping criterion

## Recommendation

Proceed to:

- [x] Draft `notes/roadmap/roadmap-N.md` step entry from §Proposed Roadmap Shape above
- [x] Draft `notes/roadmap/step-NN-workplan.md` for Step 1
- [ ] Refine planning inputs first

Rationale: The proposal is already tightly scoped, single-step, low-risk, and ships independently of the sibling channels. Shipping criteria are concrete and verifiable. No source-input gaps. Recommend **start step now** rather than further refinement — additional planning churn would be pure overhead.

**Next action**: `orchestrator start-next-round` → spawn step-NN-coordinator to draft the workplan from this intake.

## Human Review Notes

(append review decisions here)
