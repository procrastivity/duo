# Step 1 Workplan — Add `packages.duo` to `flake.nix`

**Status**: locked (refined 2026-05-03 via step-01-researcher; awaiting `build/go/approved` from orchestrator before builder spawn)
**Roadmap**: `notes/roadmap/roadmap-2.md`
**Proposal**: `notes/proposals/duo-packaging-nix-flake.md`
**Intake**: `notes/proposals/duo-packaging-nix-flake-intake.md`
**Source coverage**: see Coverage Map in intake (all source items map to this single step).

---

## Scope

- **Goal**: Ship a `packages.${system}.duo` flake output (with `packages.${system}.default` alias) such that `nix build .#duo` produces `result/bin/duo`, `nix run .#duo -- whoami` works, and `nix flake check` is green. Existing `devShells.default` must remain unchanged.
- **Out of scope**: removing the Node runtime requirement (Channel 2); CI guard for `npmDepsHash` drift; pinning `flake.lock` off `nixos-unstable`; `docs/PUBLISHING.md` "Nix users" section (follow-up).

---

## Authoritative inputs

- `notes/proposals/duo-packaging-nix-flake.md`
- `notes/proposals/duo-packaging-nix-flake-intake.md`
- `notes/roadmap/roadmap-2.md`
- Current state: `flake.nix`, `package.json`, `package-lock.json`, `dist/duo.mjs` (Round 1 artifact, verified present 2026-05-03).
- Round 1 archive: `notes/roadmap/archive/roadmap-1-npm-bundle.md`, `notes/roadmap/archive/step-01-workplan-npm-bundle.md`.

---

## Resolved open questions (from researcher pass, 2026-05-03)

1. **Linux verification path**: one-time local run via `nixos/nix:latest` Docker container. No CI workflow this round (deferred per roadmap §Open optional refinements).
2. **`buildNpmPackage` quirks**: defaults are sufficient. `pkgs.buildNpmPackage` is the correct top-level attr on `nixos-unstable`. `package.json` has no `preinstall`/`postinstall`/`prepare`/husky/lefthook hooks; `prepublishOnly` is irrelevant to `npm ci`. esbuild's optionalDependencies postinstall is handled natively by `buildNpmPackage`'s FOD npm cache — do **not** pass `--ignore-scripts`. Set `npmBuildScript = "build";` explicitly (defensive against future default changes). No `npmFlags`, no `makeCacheWritable`, no `dontNpmBuild`.
3. **`src` filter**: `lib.fileset.toSource` with an allow-list of `{ package.json, package-lock.json, src/, scripts/, tsconfig.json }`. Repo carries substantial unrelated noise (`notes/`, `.claude/`, `.direnv/`, `dist/`, `duo.egg-info/`, `pyproject.toml`, `uv.lock`, `workflow-portable-stub/`, `result*` symlinks); allow-list is more durable than blocklist. Add a one-line comment noting the allow-list must be extended if `npm run build` starts reading new top-level paths.
4. **`flake-utils` vs hand-rolled `eachDefaultSystem`**: keep existing `flake-utils.lib.eachDefaultSystem`. Extend its return set with `packages.duo` and `packages.default = self.packages.${system}.duo;` alongside the existing `devShells.default`. No new flake input.

Repo-state cross-check (researcher): no blocking conflicts. `package.json engines.node ">=22.0.0"` is satisfied by `nodejs_24`. `dist/duo.mjs` is 1.47 MB, executable, has the expected `#!/usr/bin/env node` + `createRequire` shim banner. `package-lock.json` is lockfileVersion 3.

---

## Locked task list

### Task 1 — Implement `packages.duo` derivation in `flake.nix`

Edit `flake.nix` to extend the existing `flake-utils.lib.eachDefaultSystem` return set with:

- `packages.duo` — `pkgs.buildNpmPackage { … }` derivation with:
  - `pname = "duo";`
  - `version = (lib.importJSON ./package.json).version;` (currently `0.1.3`)
  - `src = lib.fileset.toSource { root = ./.; fileset = lib.fileset.unions [ ./package.json ./package-lock.json ./src ./scripts ./tsconfig.json ]; };` with a one-line comment: "allow-list — extend if `npm run build` starts reading new top-level paths."
  - `npmDepsHash = lib.fakeHash;` (will be replaced after first build per recompute comment block).
  - `npmBuildScript = "build";` (explicit; matches `scripts.build` invoking `scripts/build.mjs`).
  - `nativeBuildInputs = [ pkgs.makeWrapper ];`
  - Default `buildPhase` (runs `npm run build`).
  - Custom `installPhase`:
    ```
    mkdir -p $out/lib/duo $out/bin
    cp dist/duo.mjs $out/lib/duo/duo.mjs
    chmod +x $out/lib/duo/duo.mjs
    makeWrapper ${pkgs.nodejs_24}/bin/node $out/bin/duo --add-flags $out/lib/duo/duo.mjs
    ```
- `packages.default = self.packages.${system}.duo;` (alias so `nix build .` works).
- Comment block above `npmDepsHash` per proposal §Implementation notes:
  ```
  # To recompute after package-lock.json changes:
  #   1. Set npmDepsHash = lib.fakeHash;
  #   2. Run nix build .#duo
  #   3. Copy the "got: sha256-..." value from the failure output into npmDepsHash
  ```

Constraints:
- Existing `devShells.default` body must remain byte-identical (or functionally identical if reformatted by accident — verify with `nix develop` smoke after edit).
- Do not introduce any new flake input.
- Do not edit `package.json`, `package-lock.json`, `scripts/build.mjs`, or anything under `src/`.

### Task 2 — Local verification (darwin)

1. Run `nix build .#duo` — expect failure with `npmDepsHash` mismatch; copy the `got: sha256-…` value into `flake.nix`.
2. Re-run `nix build .#duo` — expect success; verify `result/bin/duo` exists and is executable.
3. Smoke: `./result/bin/duo --help`, `nix run .#duo -- whoami`.
4. Run `nix flake check` — expect green.
5. Run `nix develop` — confirm devShell is unchanged (cd in, run `which node` or whatever the existing shellHook prints; exit cleanly).
6. Capture build wall-clock duration of the *second* (post-hash-fix) build and the size of `result/bin/duo` + `result/lib/duo/duo.mjs` for the PR description. First-build numbers are misleading and must not be used.

### Task 3 — Linux verification (Docker)

From the host shell:
```
docker run --rm -v "$PWD":/work -w /work nixos/nix:latest \
  nix --extra-experimental-features 'nix-command flakes' build .#duo
```
Then in the same container (or a follow-up `docker run` with the same mount):
```
./result/bin/duo --help
```

Capture both outputs for the PR description. If the host is `aarch64-darwin`, the container runs `x86_64-linux` under emulation — acceptable for verification, slow but one-shot.

### Task 4 — PR + documentation breadcrumbs

PR description includes:
- `nix build .#duo` and `nix run .#duo -- whoami` outputs (darwin).
- Docker `nix build .#duo` output (Linux verification).
- Build duration + artifact sizes from Task 2 step 6.
- Sanity check: `npm pack --dry-run` is unaffected by this change.
- Deferred-decision recap from roadmap §"Deferred decisions resolved in this step."
- No `docs/PUBLISHING.md` edit (deferred per roadmap §Out of scope).

---

## Locked batching

| Batch | Tasks | Rationale |
|---|---|---|
| Batch A | Task 1, Task 2 | Single-builder unit: derivation draft + local darwin verification + `lib.fakeHash` → real-hash recompute loop. Coherent edit-build-fix cycle. |
| Batch B | Task 3, Task 4 | Linux verification (Docker) + PR polish. Gated on Batch A green. |

(Original Task 1 "inspect current shape" was collapsed — researcher pass covered it.)

---

## Definition of Done

All shipping criteria from `notes/roadmap/roadmap-2.md` checked off. Round is single-step; step DoD == round DoD.

Specifically:
- [ ] `flake.nix` outputs include `packages.${system}.duo` and `packages.${system}.default`.
- [ ] `nix build .#duo` produces `result/bin/duo`.
- [ ] `./result/bin/duo --help` works.
- [ ] `nix run .#duo -- whoami` works.
- [ ] `nix flake check` passes.
- [ ] `npmDepsHash` recompute instructions present as a comment in `flake.nix`.
- [ ] Existing `devShells.default` continues to work.
- [ ] `nix build .#duo` succeeds on `x86_64-linux` (Docker run).

---

## Build authorization

Workplan is **locked**. Coordinator has surfaced refined plan to orchestrator and is waiting for a `build/go/approved` signal before spawning any builders. Researcher (process 314) remains alive for build-phase consults.
