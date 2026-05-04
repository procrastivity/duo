# Proposal Intake — duo packaging (Nix flake `packages.duo`, Channel 3)

**Status**: draft
**Date**: 2026-05-03
**Intake inputs**:

- `notes/proposals/duo-packaging-nix-flake.md` — drafted Channel 3 proposal (Nix flake `packages.duo`)
- `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 3) — original source plan referenced by the proposal (treated as already distilled)
- `flake.nix`, `package.json`, `package-lock.json`, `notes/roadmap/archive/` — current state cross-check
- `notes/proposals/duo-packaging-{npm-bundle,bun-binaries,install-ux}.md` — sibling channels (sequencing context)

---

## Summary

Extend the existing `flake.nix` with a `packages.${system}.duo` (and `packages.${system}.default` alias) output so Nix users can `nix run github:procrastivity/duo` or `nix profile install` it. The derivation runs `npm ci` + `npm run build` inside the Nix sandbox and wraps the resulting `dist/duo.mjs` as `$out/bin/duo` against `nodejs_24` via `makeWrapper`. Existing `devShells.default` is untouched. Strong dependency on Channel 1 (npm esbuild bundle) — without Channel 1, the derivation has to wrap a multi-file `dist/` tree, which is workable but uglier.

## Source Inputs

| Source | Type | Role in intake |
|---|---|---|
| `notes/proposals/duo-packaging-nix-flake.md` | proposal | primary |
| `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 3) | idea / source plan | background (already distilled) |
| `flake.nix` | current state | supporting (extension target; existing devShell preserved) |
| `package.json`, `package-lock.json` | current state | supporting (`buildNpmPackage` consumes lockfile) |
| `notes/roadmap/archive/` | shipped scope | supporting (no overlap; greenfield Nix output) |
| `notes/proposals/duo-packaging-npm-bundle.md` | sibling | background (strict prerequisite for clean Step 1) |
| `notes/proposals/duo-packaging-bun-binaries.md` | sibling | background (independent) |
| `notes/proposals/duo-packaging-install-ux.md` | sibling | background (potential downstream consumer of `nix profile install` instructions) |

## Candidate Outcomes

- Outcome: `nix build .#duo` produces a working `result/bin/duo`
  - Source: proposal §Behavior boundary
  - User-visible result: Nix users can build `duo` from the flake locally
  - Verification signal: `./result/bin/duo --help` works after `nix build .#duo`

- Outcome: `nix run github:procrastivity/duo -- whoami` works once the change is on `main`
  - Source: proposal §Behavior boundary
  - User-visible result: zero-install try-it path for Nix users
  - Verification signal: `nix run .#duo -- whoami` succeeds locally pre-merge; `nix run github:procrastivity/duo -- whoami` succeeds post-merge

- Outcome: `nix flake check` passes
  - Source: proposal §Behavior boundary
  - User-visible result: flake stays well-formed and consumable as an input by other flakes
  - Verification signal: `nix flake check` exits 0

- Outcome: Existing devShell continues to work unchanged
  - Source: proposal §Integration points
  - User-visible result: contributors using `nix develop` see no regression
  - Verification signal: `nix develop` opens the same shell as before; ad-hoc smoke after change

- Outcome: Linux "just works" via `eachDefaultSystem`
  - Source: proposal §Edge cases (System coverage)
  - User-visible result: `x86_64-linux` Nix users can also build/install
  - Verification signal: `nix build .#duo` succeeds on `x86_64-linux` (CI runner or local VM) at least once

- Outcome: Pinned Node runtime (`nodejs_24`) baked into the wrapper
  - Source: proposal §Edge cases (Wrapper vs shebang), §Implementation notes
  - User-visible result: behavior is reproducible; users don't need a generic `node` on PATH
  - Verification signal: `result/bin/duo` runs on a system with no `node` in PATH

- Outcome: Documented `npmDepsHash` recompute path
  - Source: proposal §Implementation notes
  - User-visible result: future contributors aren't blocked when `package-lock.json` changes
  - Verification signal: comment block in `flake.nix` near `npmDepsHash` describes the `lib.fakeHash` → "got: sha256-…" recompute flow

## Proposed Roadmap Shape

The proposal already specifies a single, well-bounded step. Intake recommendation: adopt as-is, lifted verbatim into a roadmap step with no decomposition. The work is one cohesive unit (extend `flake.nix`, validate locally, sanity-check Linux); splitting would add ceremony without risk reduction.

### Step 1 — Add `packages.duo` to `flake.nix`

**Goal**: `nix build .#duo` produces a working `duo` wrapper that runs against `nodejs_24`, with `nix flake check` green and the existing devShell unchanged.

**Shipping criteria** (lifted from proposal):

- [ ] `flake.nix` outputs include `packages.${system}.duo` and `packages.${system}.default`.
- [ ] `nix build .#duo` produces `result/bin/duo`.
- [ ] `./result/bin/duo --help` works.
- [ ] `nix run .#duo -- whoami` works.
- [ ] `nix flake check` passes.
- [ ] `npmDepsHash` recompute instructions present as a comment in `flake.nix`.
- [ ] Existing `devShells.default` continues to work (`nix develop` opens the same shell as before).
- [ ] At least one Linux verification: `nix build .#duo` succeeds on `x86_64-linux` (CI runner or local VM).

**Deferred decisions resolved in this step**:

- Decision: Use `pkgs.buildNpmPackage` (over `pnpm2nix` / `nix-npm-buildpackage`)
  - Source: proposal §Implementation notes, §Risks
  - Why this step: `package-lock.json` already exists; `buildNpmPackage` is the standard nixpkgs path. Switch only if hashing/lockfile pain is unmanageable.

- Decision: Pin runtime to `nodejs_24`
  - Source: proposal §Roadmap shape (Deferred decisions resolved in this step)
  - Why this step: matches the project's stated minimum Node version. Revisit only if the bundled CLI ever needs a runtime feature only available in a newer line.

- Decision: Use `makeWrapper` against `nodejs_24` rather than relying on `#!/usr/bin/env node` shebang
  - Source: proposal §Edge cases (Wrapper vs shebang)
  - Why this step: pins the Node version and avoids relying on a generic `node` on PATH inside the Nix build environment.

- Decision: No CI guard for `npmDepsHash` drift in this channel
  - Source: proposal §Open questions (default: manual is fine for solo-local-cli)
  - Why this step: comment-block mitigation is sufficient for the volume of lockfile churn expected; revisit if recompute pain becomes routine.

- Decision: Keep `flake.lock` floating on `nixos-unstable` (no pinning change)
  - Source: proposal §Open questions (default: keep current)
  - Why this step: no signal motivating a pin change; out of scope for this proposal.

**New deps**:

- None in `package.json`.
- `flake.nix` gains `nodejs_24` and `makeWrapper` (plus `buildNpmPackage`) as derivation inputs.

**Risk**: low–medium. Standard `buildNpmPackage` recipe; primary hazard is `npmDepsHash` churn discipline (every lockfile change forces a hash update). Secondary hazard is `buildNpmPackage` lockfile-format incompatibility (rare on `npm@>=10`); escape hatch is `nix-npm-buildpackage`. Rollback is a flake-level revert; no published-artifact concerns.

**Source coverage**:

- `duo-packaging-nix-flake.md` §Implementation notes → all `flake.nix` edits, derivation shape, comment block
- `duo-packaging-nix-flake.md` §Edge cases → `npmDepsHash` discipline, system coverage, wrapper-vs-shebang decision
- `duo-packaging-nix-flake.md` §Roadmap shape → shipping criteria verbatim
- `duo-packaging-nix-flake.md` §Integration points → devShell preservation, Channel 1 dependency note

## Coverage Map

| Source item | Proposed step | Status | Notes |
|---|---|---|---|
| `flake.nix` `packages.${system}.duo` output | Step 1 | planned | |
| `packages.${system}.default` alias | Step 1 | planned | so `nix build .` works |
| `pkgs.buildNpmPackage` derivation | Step 1 | planned | fallback to `nix-npm-buildpackage` if hashing painful |
| `npm ci` inside sandbox | Step 1 | planned | provided by `buildNpmPackage` |
| `npm run build` inside sandbox | Step 1 | planned | depends on Channel 1 for `dist/duo.mjs` artifact |
| `makeWrapper` against `nodejs_24` | Step 1 | planned | pins Node runtime |
| `npmDepsHash` initial value | Step 1 | planned | `lib.fakeHash` → recompute on first build |
| `npmDepsHash` recompute comment block | Step 1 | planned | shipping criterion |
| `nix build .#duo` shipping check | Step 1 | planned | shipping criterion |
| `nix run .#duo -- whoami` shipping check | Step 1 | planned | shipping criterion |
| `nix flake check` shipping check | Step 1 | planned | shipping criterion |
| Existing `devShells.default` preserved | Step 1 | planned | shipping criterion |
| Linux system verification (`x86_64-linux`) | Step 1 | planned | shipping criterion; Linux is "free" via `eachDefaultSystem` |
| Channel 1 (npm esbuild bundle) | — | external prerequisite | strongly preferred to land first |
| Channel 2 (Bun binaries) | — | out-of-scope | independent sibling |
| Removing Node runtime requirement | — | out-of-scope | Channel 2 territory |
| Replacing npm publish flow | — | out-of-scope | parallel channel by design |
| Vendoring nixpkgs / pinning Node minor | — | out-of-scope | `nodejs_24` is sufficient |
| Follow-up `docs/PUBLISHING.md` "Nix users" section | — | deferred | proposal §Integration points (post-merge follow-up) |
| CI guardrail for `npmDepsHash` drift | — | deferred | proposal §Open questions; revisit if recompute pain becomes routine |
| Pinning `flake.lock` off `nixos-unstable` | — | out-of-scope | proposal §Open questions (default: keep current) |

## Deferred / Out-of-Scope Items

- Item: Removing the Node runtime requirement
  - Source: proposal §Behavior boundary (out of scope)
  - Reason: Channel 2 (Bun binaries) territory
  - Revisit trigger: Channel 2 ships and a Nix-native Bun derivation makes sense

- Item: Replacing the npm publish flow
  - Source: proposal §Behavior boundary (out of scope)
  - Reason: channels ship in parallel by design
  - Revisit trigger: never (architectural decision, not deferral)

- Item: Vendoring nixpkgs / pinning to a specific Node minor
  - Source: proposal §Behavior boundary (out of scope)
  - Reason: `nodejs_24` from `nixos-unstable` is sufficient and matches stated minimum
  - Revisit trigger: a runtime feature requirement that forces a Node minor pin

- Item: `docs/PUBLISHING.md` "Nix users" section
  - Source: proposal §Integration points
  - Reason: not strictly needed for the channel to function; clean follow-up after the flake output ships
  - Revisit trigger: first user-facing Nix install instructions, or `nix profile install` becoming a recommended path

- Item: CI guardrail for `npmDepsHash` drift
  - Source: proposal §Open questions, §Risks
  - Reason: comment-block mitigation is acceptable for solo-local-cli scale
  - Revisit trigger: recompute pain becomes routine, or a contributor lands a lockfile change without updating the hash

- Item: Pinning `flake.lock` off `nixos-unstable`
  - Source: proposal §Open questions
  - Reason: no signal motivating a change; current discipline matches existing flake
  - Revisit trigger: a reproducibility regression traced to nixpkgs unstable churn

## Open Questions

The proposal lists two open questions; both have defensible defaults and are resolved as deferred decisions above. None block confident planning.

- ~~CI guard for `npmDepsHash` drift?~~ — default: no, manual recompute is fine. (Resolved: deferred.)
- ~~Pin to a specific nixpkgs revision vs float on `nixos-unstable`?~~ — default: keep floating. (Resolved: out-of-scope.)

Sequencing question (does NOT block start in the abstract, but blocks scope crispness):

- Question: Does Channel 1 (npm esbuild bundle) ship before this channel?
  - Why it matters: if Channel 1 lands first, the derivation wraps a single `dist/duo.mjs` file (clean `makeWrapper` invocation). If this channel lands first, the derivation has to wrap a multi-file `dist/` tree (workable but uglier; Step 1 shipping criteria need a small adjustment).
  - Blocks roadmap? **soft yes** — strongly preferred to land Channel 1 first; the proposal explicitly recommends "Ch1 first" in the orchestrator playbook.
  - Suggested owner: orchestrator at round-planning time.

Optional refinement (does NOT block start):

- Question: Should the Linux verification be wired into a CI job (e.g., a `nix-build` step on an `ubuntu-latest` runner) or remain a one-time manual check?
  - Why it matters: a one-time check can drift; a CI job catches Linux-only regressions early, but adds workflow surface.
  - Blocks roadmap? no
  - Suggested owner: builder discretion during Step 1; if added, becomes an additional shipping criterion.

## Recommendation

Proceed to:

- [x] Draft `notes/roadmap/roadmap-N.md` step entry from §Proposed Roadmap Shape above (single step)
- [x] Draft `notes/roadmap/step-NN-workplan.md` for Step 1 — **after Channel 1 ships**, so the derivation can target the stable `dist/duo.mjs` artifact
- [ ] Refine planning inputs first

Rationale: The proposal is tightly scoped, single-step, and low–medium risk. Shipping criteria are concrete and externally verifiable. No source-input gaps. The only meaningful planning input is **sequencing**: this channel is much cleaner if Channel 1 lands first. Recommend **start Step 1 after Channel 1 ships** rather than further refinement of this proposal — the planning artifact is ready; the dependency is the gating factor.

**Sequencing note**: Channel 1 → Channel 3 is the strongly preferred order. Channel 2 (Bun binaries) is independent and can interleave freely. If the human wants to start Channel 3 *before* Channel 1, this is workable — Step 1 shipping criteria need a small adjustment to wrap `dist/index.js` (multi-file tree) instead of `dist/duo.mjs`, and `makeWrapper` becomes slightly more involved. Flag the discrepancy at workplan time.

**Next action** (assuming Channel 1 has shipped or is queued ahead): `orchestrator start-next-round` → spawn step-NN-coordinator to draft the Step 1 workplan from this intake.

## Human Review Notes

(append review decisions here)
