# duo packaging — Bun-compiled macOS binaries (Channel 2)

**Status**: draft
**Date**: 2026-05-03
**Profile**: solo-local-cli
**Source input**: `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 2)

## Summary

Produce self-contained `duo-darwin-arm64` and `duo-darwin-x64` binaries via Bun `--compile` and attach them to GitHub Releases on every `v*` tag push. This is the *optional* "no Node required" channel — users without Node 22 installed can download a binary, run `xattr -d com.apple.quarantine`, and execute. Codesigning/notarization is deferred to v2.

## Behavior boundary

After this lands:

- A `git push origin vX.Y.Z` triggers a CI workflow that builds two macOS binaries and attaches them to the GitHub Release for that tag.
- Each binary is fully self-contained: it embeds the Bun runtime; no `node` in PATH required.
- Each binary is unsigned, so first-run on macOS requires the user to either right-click → Open or run `xattr -d com.apple.quarantine ./duo` manually. This is documented, not silently fixed.
- A repo-local smoke script (`scripts/smoke-bin.sh`) validates `--help`, `whoami`, `version`, and a minimal MCP stdio handshake against the produced binary before upload.

Out of scope:

- Linux or Windows binaries (start with macOS; expand later if demand exists).
- Codesigning + notarization (follow-up; tracked as an Open Question).
- Replacing the npm channel — both channels ship in parallel.
- A binary for the `procrastivity-duo` package name; this is just `duo`.

## Inputs / outputs

**Inputs**: `src/index.ts` entry; the same source tree Channel 1 bundles, but Bun does its own bundling internally — the `dist/duo.mjs` output is *not* consumed here.

**Outputs**:

- `dist/bin/duo-darwin-arm64` — self-contained binary for Apple Silicon.
- `dist/bin/duo-darwin-x64` — self-contained binary for Intel Macs.
- Release assets attached at `https://github.com/procrastivity/duo/releases/tag/vX.Y.Z`.

## Edge cases & error handling

- **MCP SDK + Bun stdio compat**: `@modelcontextprotocol/sdk` uses Node-style `process.stdin/stdout`. Should work under Bun, but unverified. The smoke script's MCP stdio handshake is the gate that catches this before a release ships a broken binary.
- **`execa` under Bun**: pure JS, but Bun's child-process semantics differ subtly from Node. Smoke test exercises any subcommand that invokes `execa` (`doctor`, `proc`, etc.) to catch regressions.
- **`pino` under Bun**: synchronous `pino.destination(2)` is the simplest case; should work. If Bun's stderr handling differs, smoke test catches via captured output.
- **macOS Gatekeeper**: unsigned binary triggers quarantine. Documented mitigation is `xattr -d com.apple.quarantine ./duo`. Acceptable for v1 per source plan; revisit if friction is real.
- **Cross-arch CI runners**: GitHub-hosted `macos-14` is arm64, `macos-13` is x64. Both must be in the matrix; cross-compilation via Bun is possible but unverified for this CLI's deps.

## Integration points

- **`flake.nix`** — add `bun` to devShell `buildInputs` so contributors can build binaries locally without a global install.
- **`package.json`** — new scripts only: `build:bin:darwin-arm64`, `build:bin:darwin-x64`. No change to `bin`, `files`, or publish flow.
- **`.github/workflows/release-bin.yml`** — new workflow, parallel to `release.yml`. Triggered on `v*` tag push.
- **`scripts/smoke-bin.sh`** — new repo-local smoke script.
- **Channel 1 (npm bundle)** — independent. Both can land in either order.
- **Channel 4 (install UX)** — downstream consumer. Both `curl | sh` and Homebrew tap depend on these binaries existing as Release assets with stable filenames.

## Implementation notes (preserved from source plan)

**Add `bun` to `flake.nix`** devShell `buildInputs` so contributors get it locally.

**New `package.json` scripts**:

```
build:bin:darwin-arm64:  bun build src/index.ts --compile --target=bun-darwin-arm64 --outfile=dist/bin/duo-darwin-arm64
build:bin:darwin-x64:    bun build src/index.ts --compile --target=bun-darwin-x64   --outfile=dist/bin/duo-darwin-x64
```

**New `scripts/smoke-bin.sh`**: runs the produced binary against:

- `--help`
- `whoami`
- `version`
- a minimal MCP stdio handshake

…to catch Bun-compat surprises in `@modelcontextprotocol/sdk` / `execa`.

**New GitHub Actions workflow `.github/workflows/release-bin.yml`**:

- Triggered on tag push (`v*`).
- Matrix: `macos-14` (arm64) + `macos-13` (x64).
- Installs Bun via `oven-sh/setup-bun`.
- Builds the binary for the runner's arch.
- Runs `scripts/smoke-bin.sh` against the produced binary.
- Uploads the artifact to the release with `softprops/action-gh-release`.

**macOS codesigning / notarization is out of scope for v1**. Document that users will need to run `xattr -d com.apple.quarantine ./duo` after download. Add a follow-up note to revisit signing once the binary path is proven.

**Local verification before wiring CI**:

```
npm run build:bin:darwin-arm64
env -i PATH=/usr/bin ./dist/bin/duo-darwin-arm64 --help
```

The `env -i PATH=/usr/bin` strips Node from PATH, proving the binary is genuinely self-contained. Run smoke script locally before pushing the workflow.

## Roadmap shape

### Step 1 — Build and smoke macOS binaries locally

**Goal**: prove the Bun `--compile` path works end-to-end before wiring CI.

**Shipping criteria**:

- [ ] `bun` added to flake devShell.
- [ ] `npm run build:bin:darwin-arm64` produces `dist/bin/duo-darwin-arm64`.
- [ ] `npm run build:bin:darwin-x64` produces `dist/bin/duo-darwin-x64`.
- [ ] `env -i PATH=/usr/bin ./dist/bin/duo-darwin-arm64 --help` works on a Mac with no `node` in PATH.
- [ ] `scripts/smoke-bin.sh` passes against both binaries (or against the local-arch binary if cross-arch testing is impractical).
- [ ] MCP stdio handshake step in the smoke script exercises the SDK's `process.stdin/stdout` path successfully.

**Risk**: medium. Bun/MCP-SDK compat is unverified; the smoke script is the gate.

### Step 2 — Wire CI workflow and ship a release-candidate tag

**Goal**: a `v*` tag push produces both binaries on the GitHub Release page, automatically.

**Shipping criteria**:

- [ ] `.github/workflows/release-bin.yml` exists and is valid.
- [ ] Tagging a `v0.1.4-rc.0` (or similar pre-release) uploads both binaries to the corresponding GitHub Release.
- [ ] Smoke script runs in CI and gates the upload — a smoke failure aborts the workflow before any artifact uploads.
- [ ] Asset filenames are stable: `duo-darwin-arm64` and `duo-darwin-x64` (no version suffix in the filename, since the Release tag carries the version). Channel 4 depends on this stability.

**Deferred decisions resolved in this step**:

- Codesigning/notarization stays deferred. Document the `xattr` workaround in the Release notes template.

**New deps**: `bun` (devShell + CI), `oven-sh/setup-bun`, `softprops/action-gh-release`.

**Risk**: medium. CI runner availability for both macOS archs is reliable but not guaranteed; if `macos-13` (x64) is deprecated by the time this lands, drop x64 from the matrix and document arm64-only.

## Coverage map

| Source item (i-want-to-better-peppy-shamir.md, Channel 2) | Step | Status | Notes |
|---|---|---|---|
| Add `bun` to flake.nix | Step 1 | planned | |
| `build:bin:darwin-arm64` script | Step 1 | planned | |
| `build:bin:darwin-x64` script | Step 1 | planned | |
| `scripts/smoke-bin.sh` | Step 1 | planned | covers `--help`, `whoami`, `version`, MCP stdio |
| Local self-contained verify (`env -i PATH=/usr/bin`) | Step 1 | planned | shipping criterion |
| `.github/workflows/release-bin.yml` | Step 2 | planned | |
| Tag a `v0.1.4-rc.0` and confirm 2 artifacts upload | Step 2 | planned | shipping criterion |
| Codesigning / notarization | — | deferred | tracked in Open Questions |
| `xattr -d com.apple.quarantine` in user docs | Step 2 | planned | release-notes / README hint |

## Open questions

- Should codesigning/notarization land in v2, or wait for actual user friction reports? (Default: wait.)
- Do we want a Linux binary at the same time, or stage that as a separate channel after macOS proves out? (Default: stage separately.)
- Asset naming convention: `duo-darwin-arm64` (current plan) vs `duo-vX.Y.Z-darwin-arm64` (version in filename). Channel 4 assumes the former.

## Risks

- **Bun compat with MCP SDK stdio**: the SDK uses Node-style `process.stdin/stdout`; should work under Bun but unverified. Smoke script catches this before release. If it fails, fallback is Node SEA — not currently planned, but the source plan flags it as the escape hatch.
- **macOS Gatekeeper**: unsigned binaries require manual `xattr` removal. Acceptable for v1; signing/notarization is a follow-up if friction is real.
- **CI runner deprecation**: GitHub's macOS runner labels rotate. Pin to specific labels in the workflow and revisit annually.
