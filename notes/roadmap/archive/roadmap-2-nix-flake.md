# Roadmap 2 — duo packaging: Nix flake `packages.duo` (Channel 3)

**Project**: Duo
**Status**: shipped
**Started**: 2026-05-03
**Shipped**: 2026-05-03 (commit `8afbe01`, local; awaiting human-authorized push)
**Proposal**: `notes/proposals/duo-packaging-nix-flake.md`
**Intake**: `notes/proposals/duo-packaging-nix-flake-intake.md`

> **Round numbering note**: This is the project's *second real shipped round*. The placeholder `notes/roadmap/archive/roadmap-1.md` is prior scaffolding (not shipped scope); `notes/roadmap/archive/roadmap-1-npm-bundle.md` was Round 1 (Channel 1, npm esbuild bundle). This Round 2 is Channel 3 (Nix flake), unblocked because Channel 1 shipped `dist/duo.mjs`.

---

## Summary

Extend `flake.nix` with a `packages.${system}.duo` (and `packages.${system}.default` alias) output so Nix users can `nix run github:procrastivity/duo` or `nix profile install` it. The derivation runs `npm ci` + `npm run build` inside the Nix sandbox via `pkgs.buildNpmPackage`, then wraps the resulting `dist/duo.mjs` as `$out/bin/duo` against `nodejs_24` via `makeWrapper`. Existing `devShells.default` is untouched.

This round contains **one well-bounded step**, lifted verbatim from the intake (intake recommendation: adopt as-is, no decomposition).

The Channel 1 prerequisite has shipped (Round 1, 2026-05-03), so the derivation can target the stable single-file `dist/duo.mjs` artifact directly — no multi-file workaround needed.

---

## Step 1 — Add `packages.duo` to `flake.nix`

**Goal**: `nix build .#duo` produces a working `duo` wrapper that runs against `nodejs_24`, with `nix flake check` green and the existing devShell unchanged.

**Workplan**: `notes/roadmap/step-01-workplan.md`

**Shipping criteria** (lifted from proposal §Roadmap shape and intake):

- [ ] `flake.nix` outputs include `packages.${system}.duo` and `packages.${system}.default`.
- [ ] `nix build .#duo` produces `result/bin/duo`.
- [ ] `./result/bin/duo --help` works.
- [ ] `nix run .#duo -- whoami` works.
- [ ] `nix flake check` passes.
- [ ] `npmDepsHash` recompute instructions present as a comment in `flake.nix`.
- [ ] Existing `devShells.default` continues to work (`nix develop` opens the same shell as before).
- [ ] At least one Linux verification: `nix build .#duo` succeeds on `x86_64-linux` (CI runner or local VM).

**Deferred decisions resolved in this step** (from intake):

- Use `pkgs.buildNpmPackage` over `pnpm2nix` / `nix-npm-buildpackage`. `package-lock.json` already exists; `buildNpmPackage` is the standard nixpkgs path. Switch only if hashing/lockfile pain proves unmanageable.
- Pin runtime to `nodejs_24` (matches project's stated minimum Node version).
- Use `makeWrapper` against `nodejs_24` rather than `#!/usr/bin/env node` shebang reliance — pins the Node version and avoids relying on a generic `node` on PATH inside the Nix build environment.
- No CI guard for `npmDepsHash` drift in this channel (comment-block mitigation is sufficient for solo-local-cli scale).
- Keep `flake.lock` floating on `nixos-unstable` (no pinning change).

**New deps**: none in `package.json`. `flake.nix` gains `nodejs_24`, `makeWrapper`, and `buildNpmPackage` as derivation inputs.

**Risk**: low–medium. Standard `buildNpmPackage` recipe; primary hazard is `npmDepsHash` churn discipline (every lockfile change forces a hash update — comment-block mitigation in place). Secondary hazard is `buildNpmPackage` lockfile-format incompatibility (rare on `npm@>=10`); escape hatch is `nix-npm-buildpackage`. Rollback is a flake-level revert; no published-artifact concerns.

---

## Out of scope (sibling channels and explicit non-goals)

- Channel 2 — Bun-compiled macOS binaries (`notes/proposals/duo-packaging-bun-binaries.md`). Independent of this round.
- Channel 4 — Install UX / GitHub Releases / curl|sh / Homebrew (`notes/proposals/duo-packaging-install-ux.md`). Depends on Channel 2.
- Removing the Node runtime requirement (Channel 2 territory).
- Replacing the npm publish flow (parallel channel by design).
- Vendoring nixpkgs or pinning to a specific Node minor beyond `nodejs_24`.
- `docs/PUBLISHING.md` "Nix users" section (clean follow-up after the flake output ships).

## Open optional refinements (do not block round)

- Wire the Linux verification into a CI job (`nix-build` step on an `ubuntu-latest` runner). Builder discretion during Step 1; if added, becomes an extra shipping criterion.
