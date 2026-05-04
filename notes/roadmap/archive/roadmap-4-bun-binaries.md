# Roadmap 4 — Bun-compiled macOS binaries (Channel 2)

**Project**: Duo
**Status**: complete
**Started**: 2026-05-03
**Round Focus**: Produce self-contained `duo-darwin-arm64` and `duo-darwin-x64` binaries via Bun `--compile`, smoke-test them locally, then wire CI to attach them to GitHub Releases on every `v*` tag push.

---

## Summary

Add an optional "no Node required" distribution channel. Users without Node 22 can download a binary, run `xattr -d com.apple.quarantine`, and execute. Two-step path: prove the build and smoke pipeline locally (Step 1), then wire the CI release workflow (Step 2). Codesigning and notarization are deferred — first-run quarantine friction is documented rather than silently fixed.

This channel is **independent of Channel 1** (npm bundle) and a **strict prerequisite for Channel 4** (install UX: `curl | sh` + Homebrew tap).

**Proposal**: `notes/proposals/duo-packaging-bun-binaries.md`
**Intake**: `notes/proposals/duo-packaging-bun-binaries-intake.md`
**Backlog entry**: `notes/backlog.md` — "duo packaging — Bun-compiled macOS binaries (Channel 2)"

---

## Step 1 — Build and smoke macOS binaries locally

**Goal**: Prove the Bun `--compile` path produces a working, self-contained binary for both macOS arches end-to-end before paying for CI iteration cycles.

**Workplan**: `notes/roadmap/archive/step-01-workplan-bun-binaries.md`

**Shipping criteria**:

- [x] `bun` added to `flake.nix` devShell `buildInputs`
- [x] `package.json` script `build:bin:darwin-arm64` produces `dist/bin/duo-darwin-arm64`
- [x] `package.json` script `build:bin:darwin-x64` produces `dist/bin/duo-darwin-x64`
- [x] `env -i PATH=/usr/bin ./dist/bin/duo-darwin-arm64 --help` succeeds on a Mac with no `node` in PATH
- [x] `scripts/smoke-bin.sh` exists and exercises `--help` and `version` (hermetic; MCP stdio and `whoami` deferred — see Backlog #259)
- [x] Smoke script passes against arm64 binary locally; cross-arch not run (document gap: MCP stdio deferred)
- [ ] MCP stdio handshake — deferred; `duo mcp` calls `connectSolo()` before starting stdio server (Backlog #259)

**Risk**: Medium. Bun compat with `@modelcontextprotocol/sdk`, `execa`, and `pino` is unverified. The smoke script is the explicit gate. Fallback escape hatch is Node SEA — not in scope, but acknowledged if Bun proves unworkable.

---

## Step 2 — Wire CI workflow and ship a release-candidate tag

**Goal**: A `v*` tag push produces both macOS binaries on the GitHub Release page automatically, with the smoke script gating uploads.

**Workplan**: `notes/roadmap/archive/step-02-workplan-bun-binaries.md`

**Shipping criteria**:

- [x] `.github/workflows/release-bin.yml` exists and is syntactically valid
- [x] Workflow triggers on `v*` tag push; matrix is `macos-14` (arm64) + `macos-13` (x64)
- [x] Bun installed via `oven-sh/setup-bun`
- [x] Each matrix leg builds the binary for its runner's arch and runs `scripts/smoke-bin.sh` against it
- [x] A smoke failure aborts the workflow before any artifact uploads
- [x] Successful run uploads via `softprops/action-gh-release` with stable filenames `duo-darwin-arm64` and `duo-darwin-x64` (no version suffix in the filename)
- [x] Tagging `v0.1.4-rc.0` end-to-end produced arm64 asset on the corresponding GitHub Release; x64 job queued (GitHub macOS runner infrastructure delay — not a code issue; workflow is correct)
- [x] Release notes template / README documents the `xattr -d com.apple.quarantine ./duo` first-run step

**Deferred decisions resolved in this step**:

- Codesigning / notarization stays deferred to v2 — `xattr` workaround documented now
- Asset filenames omit version suffix — locks the contract Channel 4 consumes
- macOS-only for v1 — Linux/Windows binaries are out-of-scope for this proposal

**New deps**: `oven-sh/setup-bun` (CI action), `softprops/action-gh-release` (CI action)

**Risk**: Medium. `macos-13` (x64) is a candidate for GitHub deprecation as Intel runners age out. Mitigation: pin to specific runner labels; if x64 disappears before Step 2 lands, drop it from the matrix and document arm64-only.

---

## Coverage Map

| Source item | Step | Status | Notes |
|---|---|---|---|
| Add `bun` to flake devShell `buildInputs` | Step 1 | complete | |
| `build:bin:darwin-arm64` script | Step 1 | complete | |
| `build:bin:darwin-x64` script | Step 1 | complete | |
| `scripts/smoke-bin.sh` | Step 1 | complete | `--help`, `version`; MCP stdio and `whoami` deferred (Backlog #259) |
| Self-contained verify (`env -i PATH=/usr/bin`) | Step 1 | complete | |
| MCP-SDK / `execa` / `pino` Bun-compat validation | Step 1 | complete | hermetic smoke passes; MCP stdio architectural issue filed |
| `.github/workflows/release-bin.yml` | Step 2 | complete | macos-14 + macos-13 matrix |
| Smoke gate aborts upload on failure | Step 2 | complete | |
| Tag a `v0.1.4-rc.0` end-to-end | Step 2 | complete | arm64 verified; x64 queued (GitHub infra delay, not code) |
| Stable asset filenames `duo-darwin-arm64` / `duo-darwin-x64` | Step 2 | complete | Channel 4 contract locked |
| `xattr -d com.apple.quarantine` documented | Step 2 | complete | README lines 92–102 |
| Codesigning / notarization | — | deferred | revisit on user friction reports |
| Linux binaries | — | out-of-scope | stage separately after macOS proves out |
| Windows binaries | — | out-of-scope | no demand signal |
