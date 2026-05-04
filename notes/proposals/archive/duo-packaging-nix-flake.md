# duo packaging — Nix flake `packages.duo` (Channel 3)

**Status**: draft
**Date**: 2026-05-03
**Profile**: solo-local-cli
**Source input**: `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 3)

## Summary

Extend `flake.nix` with a `packages.duo` (and `packages.default`) output so `nix run github:procrastivity/duo` and `nix profile install` work for Nix users. The derivation builds the Channel 1 esbuild bundle inside the Nix sandbox and wraps the resulting `dist/duo.mjs` as `$out/bin/duo` against `nodejs_24`. The existing devShell stays untouched.

## Behavior boundary

After this lands:

- `nix build .#duo` produces `result/bin/duo`; `./result/bin/duo --help` works.
- `nix run .#duo -- whoami` works.
- `nix run github:procrastivity/duo -- whoami` works (once the change is on `main`).
- `nix flake check` passes.
- The runtime is bundled Node (`nodejs_24` from nixpkgs); the `duo` binary in `$out/bin` is a `makeWrapper`-generated shell stub that invokes Node against `dist/duo.mjs`.

Out of scope:

- Removing the Node runtime requirement (Channel 2 covers that for non-Nix users).
- Replacing the npm publish flow.
- Vendoring nixpkgs or pinning to a specific Node minor version beyond `nodejs_24`.

## Inputs / outputs

**Inputs**: full source tree, `package.json`, `package-lock.json`. The derivation runs `npm ci` inside the sandbox, so `package-lock.json` must be committed and current.

**Outputs**:

- `result/bin/duo` — wrapper script that execs `nodejs_24` against the bundled `dist/duo.mjs`.
- `packages.${system}.duo` — the derivation output, available via `nix build .#duo`.
- `packages.${system}.default` — alias, so `nix build .` (no `#duo`) works.

## Edge cases & error handling

- **`npmDepsHash` churn**: every change to `package-lock.json` invalidates the hash. First build prints the expected hash; CI must catch hash mismatches and fail loudly. Document the recompute command in `flake.nix` comments.
- **System coverage**: `eachDefaultSystem` covers Linux for free since this is JS+Node. Plan initially restricts attention to `aarch64-darwin` + `x86_64-darwin` (matching Channel 2's binary targets), but Linux should "just work" — explicit verification on `x86_64-linux` is a shipping criterion.
- **Bundle path coupling**: the derivation copies `dist/duo.mjs` from `npm run build`'s output. If Channel 1 lands first (recommended), this path is stable. If the Nix work lands before Channel 1, the derivation must build from `dist/index.js` instead — explicit in the dependency note.
- **Wrapper vs shebang**: `makeWrapper` is preferred over relying on `#!/usr/bin/env node`, because the Nix build environment may not have a generic `node` on PATH. Wrapping with `nodejs_24` as a runtime input pins the Node version.

## Integration points

- **`flake.nix`** — extend `outputs` with `packages.${system}.duo` + `packages.${system}.default`. Keep existing `devShells.default` untouched.
- **`package.json`** — no edits in this channel; the derivation consumes `npm run build` as a black box.
- **Channel 1 (npm bundle)** — strict prerequisite. The derivation's build step is `npm run build`, which after Channel 1 produces `dist/duo.mjs`. Without Channel 1, the derivation must point at `dist/index.js` (multi-file, harder to wrap cleanly).
- **`docs/PUBLISHING.md`** — no direct edit, but a follow-up could add a "Nix users" section pointing at `nix profile install github:procrastivity/duo`.

## Implementation notes (preserved from source plan)

**Extend `flake.nix` outputs** with a `packages.${system}.duo` derivation built via `pkgs.buildNpmPackage` (or `pnpm2nix` / `nix-npm-buildpackage` if npm-lockfile hashing turns out painful — start with `buildNpmPackage` since we already have `package-lock.json`).

**The derivation**:

- Runs `npm ci`.
- Runs `npm run build` (the new esbuild bundle from Channel 1).
- Installs `dist/duo.mjs` as `$out/bin/duo` with a `nodejs_24` runtime dependency wrapped via `makeWrapper` (so the resulting `duo` invokes the bundled Node).

**Initial system coverage**: `aarch64-darwin` and `x86_64-darwin` to match the binary targets, but `eachDefaultSystem` covers Linux for free since this is just JS + Node.

**`npmDepsHash`** will need to be filled in (Nix prints the expected hash on first build). Document the recompute command in `flake.nix` comments — something like:

```
# To recompute after package-lock.json changes:
#   1. Set npmDepsHash = lib.fakeHash;
#   2. Run nix build .#duo
#   3. Copy the "got: sha256-..." value from the failure output into npmDepsHash
```

## Roadmap shape

### Step 1 — Add `packages.duo` to flake.nix

**Goal**: `nix build .#duo` produces a working `duo` wrapper.

**Shipping criteria**:

- [ ] `flake.nix` outputs include `packages.${system}.duo` and `packages.${system}.default`.
- [ ] `nix build .#duo` produces `result/bin/duo`.
- [ ] `./result/bin/duo --help` works.
- [ ] `nix run .#duo -- whoami` works.
- [ ] `nix flake check` passes.
- [ ] `npmDepsHash` recompute instructions present as a comment in `flake.nix`.
- [ ] Existing `devShells.default` continues to work (`nix develop` opens the same shell as before).
- [ ] At least one Linux verification: `nix build .#duo` succeeds on `x86_64-linux` (CI runner or local VM).

**Deferred decisions resolved in this step**:

- `buildNpmPackage` vs alternatives. Default: `buildNpmPackage` because `package-lock.json` is already present. Switch only if hashing pain is unmanageable.
- Wrapper Node version. Default: `nodejs_24`. Revisit if the bundled CLI ever needs a runtime feature only available in a newer line.

**New deps**: none in `package.json`; `flake.nix` gains `nodejs_24` and `makeWrapper` as derivation inputs.

**Risk**: low–medium. Standard `buildNpmPackage` recipe; main hazard is `npmDepsHash` churn discipline (every lockfile change forces a hash update). Rollback is a flake-level revert; no published-artifact concerns.

## Coverage map

| Source item (i-want-to-better-peppy-shamir.md, Channel 3) | Step | Status | Notes |
|---|---|---|---|
| `flake.nix` `packages.${system}.duo` | Step 1 | planned | |
| `packages.${system}.default` alias | Step 1 | planned | so `nix build .` works |
| `pkgs.buildNpmPackage` derivation | Step 1 | planned | fallback to alternatives if hashing painful |
| `npm ci` + `npm run build` in sandbox | Step 1 | planned | depends on Channel 1 |
| `makeWrapper` against `nodejs_24` | Step 1 | planned | |
| `npmDepsHash` initial value + comment | Step 1 | planned | comment block included |
| `nix build .#duo` shipping check | Step 1 | planned | shipping criterion |
| `nix run .#duo` shipping check | Step 1 | planned | shipping criterion |
| `nix flake check` shipping check | Step 1 | planned | shipping criterion |
| Linux system coverage | Step 1 | planned | Linux is "free" via eachDefaultSystem; verify once |

## Open questions

- Hash recompute cadence: do we want a CI job that fails when `package-lock.json` and `npmDepsHash` drift? (Default: no — manual is fine for solo-local-cli, but flag if recompute pain becomes routine.)
- Pin to a specific nixpkgs revision in `flake.lock` discipline, or float on `nixos-unstable`? (Default: keep current `nixos-unstable` per existing flake.)

## Risks

- **`npmDepsHash` churn**: every lockfile change forces a flake hash update. Documented mitigation is the comment block; structural mitigation is a CI guard, deferred per Open Questions.
- **`buildNpmPackage` lockfile compatibility**: if our lockfile uses npm features the helper hasn't caught up to (rare on `npm@>=10`), fall back to `nix-npm-buildpackage`. Source plan acknowledges this as the escape hatch.
- **Channel 1 ordering**: if this channel ships before Channel 1, the derivation has to wrap a multi-file `dist/` tree — workable but uglier. Strong recommendation in the orchestrator playbook is "Ch1 first".
