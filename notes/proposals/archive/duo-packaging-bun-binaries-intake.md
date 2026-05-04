# Proposal Intake — duo packaging (Bun-compiled macOS binaries, Channel 2)

**Status**: draft
**Date**: 2026-05-03
**Intake inputs**:

- `notes/proposals/duo-packaging-bun-binaries.md` — drafted Channel 2 proposal (Bun `--compile` macOS binaries)
- `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 2) — original source plan referenced by the proposal (treated as already distilled)
- `package.json`, `flake.nix`, `notes/roadmap/archive/` — current state cross-check
- `notes/proposals/duo-packaging-{npm-bundle,nix-flake,install-ux}.md` — sibling channels (sequencing context)

---

## Summary

Add an *optional* "no Node required" distribution channel: self-contained `duo-darwin-arm64` and `duo-darwin-x64` binaries produced by `bun build --compile` and attached to GitHub Releases on every `v*` tag. Two-step path: prove the build/smoke locally on both arches, then wire CI. Codesigning/notarization is explicitly deferred — first-run friction is documented (`xattr -d com.apple.quarantine`) rather than silently fixed. Independent of Channel 1 (npm bundle); a downstream prerequisite for Channel 4 (`curl | sh` + Homebrew tap).

## Source Inputs

| Source | Type | Role in intake |
|---|---|---|
| `notes/proposals/duo-packaging-bun-binaries.md` | proposal | primary |
| `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 2) | idea / source plan | background (already distilled) |
| `package.json` | current state | supporting (no `bin`/`files` change expected) |
| `flake.nix` | current state | supporting (devShell `buildInputs` add) |
| `notes/roadmap/archive/` | shipped scope | supporting (no overlap; greenfield CI workflow) |
| `notes/proposals/duo-packaging-npm-bundle.md` | sibling | background (independent; either order) |
| `notes/proposals/duo-packaging-install-ux.md` | sibling | background (downstream consumer of Release assets) |

## Candidate Outcomes

- Outcome: A `git push origin vX.Y.Z` produces two macOS binaries on the GitHub Release page automatically
  - Source: proposal §Behavior boundary, §Step 2
  - User-visible result: users without Node 22 can download and run `duo` directly
  - Verification signal: tagging `v0.1.4-rc.0` uploads `duo-darwin-arm64` and `duo-darwin-x64` to the release

- Outcome: Each binary is genuinely self-contained (no `node` in PATH required)
  - Source: proposal §Behavior boundary, §Implementation notes (`env -i PATH=/usr/bin`)
  - User-visible result: end users can run the binary on a clean machine without a Node toolchain
  - Verification signal: `env -i PATH=/usr/bin ./dist/bin/duo-darwin-arm64 --help` succeeds

- Outcome: Binary CLI surface matches the npm CLI surface (no behavior delta)
  - Source: proposal §Edge cases (Bun/MCP-SDK/`execa`/`pino` compat)
  - User-visible result: `whoami`, `version`, `doctor`, `proc`, and the MCP stdio server all work under Bun runtime
  - Verification signal: `scripts/smoke-bin.sh` passes — `--help`, `whoami`, `version`, MCP stdio handshake

- Outcome: Smoke gate prevents shipping a broken binary
  - Source: proposal §Step 2 shipping criteria
  - User-visible result: a Bun-compat regression in MCP-SDK/`execa`/`pino` aborts CI before any artifact uploads
  - Verification signal: induced smoke failure causes the workflow to fail before the upload step

- Outcome: Stable asset filenames for downstream consumers
  - Source: proposal §Step 2 shipping criteria, §Open questions (asset naming)
  - User-visible result: Channel 4 (`curl | sh`, Homebrew tap) can hardcode `duo-darwin-arm64` / `duo-darwin-x64`
  - Verification signal: release assets named exactly `duo-darwin-arm64` and `duo-darwin-x64` (no version suffix)

- Outcome: Local contributor parity via flake
  - Source: proposal §Integration points
  - User-visible result: `nix develop` provides `bun`; contributors can build binaries without a global install
  - Verification signal: flake devShell exposes `bun --version` post-change

## Proposed Roadmap Shape

The proposal already defines a clean two-step decomposition (local proof → CI wiring). Intake recommendation: adopt verbatim. The split is meaningful — Step 1 burns down the Bun-compat unknowns before paying for CI iteration cycles on macos-13/14 runners.

### Step 1 — Build and smoke macOS binaries locally

**Goal**: Prove the Bun `--compile` path produces a working, self-contained binary for both macOS arches end-to-end before wiring CI.

**Shipping criteria** (lifted from proposal):

- [ ] `bun` added to `flake.nix` devShell `buildInputs`.
- [ ] `package.json` script `build:bin:darwin-arm64` produces `dist/bin/duo-darwin-arm64`.
- [ ] `package.json` script `build:bin:darwin-x64` produces `dist/bin/duo-darwin-x64`.
- [ ] `env -i PATH=/usr/bin ./dist/bin/duo-darwin-arm64 --help` succeeds on a Mac with no `node` in PATH.
- [ ] `scripts/smoke-bin.sh` exists and exercises `--help`, `whoami`, `version`, and a minimal MCP stdio handshake.
- [ ] Smoke script passes against both binaries (or against the local-arch binary if cross-arch testing is impractical — document the gap).
- [ ] MCP stdio handshake successfully exercises `@modelcontextprotocol/sdk`'s `process.stdin/stdout` path under Bun.

**Deferred decisions resolved in this step**:

- Decision: No cross-arch local testing required if impractical; smoke script can target the local arch only
  - Source: proposal §Step 1 shipping criteria ("or against the local-arch binary if cross-arch testing is impractical")
  - Why this step: keeps Step 1 unblocked on a single dev machine; CI matrix in Step 2 covers both arches anyway

**New deps**:

- `bun` (devShell only — no runtime dep added to `package.json`)

**Risk**: medium. Bun compat with `@modelcontextprotocol/sdk` (Node-style stdio), `execa` (child-process semantics), and `pino` (synchronous stderr destination) is unverified. The smoke script is the explicit gate. Fallback escape hatch is Node SEA — not in scope here, but called out in the source plan if Bun proves unworkable.

**Source coverage**:

- `duo-packaging-bun-binaries.md` §Implementation notes → all `package.json` and `flake.nix` edits, smoke script contents
- `duo-packaging-bun-binaries.md` §Edge cases → smoke script targets (MCP-SDK stdio, `execa`, `pino`)
- `duo-packaging-bun-binaries.md` §Step 1 shipping criteria → adopted verbatim

### Step 2 — Wire CI workflow and ship a release-candidate tag

**Goal**: A `v*` tag push produces both macOS binaries on the GitHub Release page automatically, with the smoke script gating uploads.

**Shipping criteria** (lifted from proposal):

- [ ] `.github/workflows/release-bin.yml` exists and is syntactically valid.
- [ ] Workflow triggers on `v*` tag push; matrix is `macos-14` (arm64) + `macos-13` (x64).
- [ ] Bun installed via `oven-sh/setup-bun`.
- [ ] Each matrix leg builds the binary for its runner's arch and runs `scripts/smoke-bin.sh` against it.
- [ ] A smoke failure aborts the workflow before any artifact uploads.
- [ ] Successful run uploads via `softprops/action-gh-release` with stable filenames `duo-darwin-arm64` and `duo-darwin-x64` (no version suffix in the filename — version is on the Release tag).
- [ ] Tagging a `v0.1.4-rc.0` (or similar pre-release) end-to-end produces both assets on the corresponding GitHub Release.
- [ ] Release notes template / README documents the `xattr -d com.apple.quarantine ./duo` first-run step.

**Deferred decisions resolved in this step**:

- Decision: Codesigning / notarization stays deferred to v2
  - Source: proposal §Risks, §Open questions (default: wait for actual user friction reports)
  - Why this step: this is the moment binaries become user-facing; the `xattr` workaround needs to be documented now, but signing is a separate workstream
- Decision: Asset filenames omit version suffix (`duo-darwin-arm64`, not `duo-vX.Y.Z-darwin-arm64`)
  - Source: proposal §Open questions (current plan + Channel 4 dependency)
  - Why this step: locks the contract Channel 4 (`curl | sh`, Homebrew tap) consumes
- Decision: macOS-only for v1; Linux/Windows binaries deferred
  - Source: proposal §Out of scope, §Open questions (default: stage Linux separately)
  - Why this step: avoids matrix sprawl while the Bun path is still being proven

**New deps**:

- `oven-sh/setup-bun` (CI action)
- `softprops/action-gh-release` (CI action)

**Risk**: medium. CI runner availability for both macOS archs is reliable but not guaranteed long-term — `macos-13` (x64) is a candidate for deprecation as GitHub ages out Intel runners. Mitigation: pin to specific runner labels, revisit annually; if x64 runner disappears before this lands, drop x64 from the matrix and document arm64-only.

**Source coverage**:

- `duo-packaging-bun-binaries.md` §Implementation notes (workflow shape) → workflow file
- `duo-packaging-bun-binaries.md` §Step 2 shipping criteria → adopted verbatim
- `duo-packaging-bun-binaries.md` §Edge cases (Gatekeeper) → release-notes/README hint

## Coverage Map

| Source item | Proposed step | Status | Notes |
|---|---|---|---|
| Add `bun` to flake devShell `buildInputs` | Step 1 | planned | |
| `build:bin:darwin-arm64` script | Step 1 | planned | exact flags lifted from proposal |
| `build:bin:darwin-x64` script | Step 1 | planned | exact flags lifted from proposal |
| `scripts/smoke-bin.sh` | Step 1 | planned | covers `--help`, `whoami`, `version`, MCP stdio handshake |
| Self-contained verify (`env -i PATH=/usr/bin`) | Step 1 | planned | shipping criterion |
| MCP-SDK / `execa` / `pino` Bun-compat validation | Step 1 | planned | folded into smoke script |
| `.github/workflows/release-bin.yml` | Step 2 | planned | macos-14 + macos-13 matrix |
| Smoke gate aborts upload on failure | Step 2 | planned | shipping criterion |
| Tag a `v0.1.4-rc.0` end-to-end | Step 2 | planned | shipping criterion |
| Stable asset filenames `duo-darwin-arm64` / `duo-darwin-x64` | Step 2 | planned | shipping criterion; Channel 4 contract |
| `xattr -d com.apple.quarantine` documented in release notes / README | Step 2 | planned | proposal §Edge cases |
| Codesigning / notarization | — | deferred | open question; revisit on user friction |
| Linux binary | — | out-of-scope | sibling stage; default: separate channel |
| Windows binary | — | out-of-scope | not motivated by source plan |
| Renaming/removing existing npm `bin` | — | out-of-scope | both channels ship in parallel |
| `procrastivity-duo` binary name | — | out-of-scope | this is just `duo` |

## Deferred / Out-of-Scope Items

- Item: macOS codesigning + notarization
  - Source: proposal §Behavior boundary (deferred to v2), §Risks, §Open questions
  - Reason: Apple Developer enrollment + notarytool wiring is a meaningful workstream; documented `xattr` workaround is acceptable for v1
  - Revisit trigger: actual user friction reports, or any move to make `curl | sh` the recommended path for non-technical users

- Item: Linux binaries (`duo-linux-x64`, `duo-linux-arm64`)
  - Source: proposal §Out of scope, §Open questions
  - Reason: prove macOS path first; avoid matrix sprawl
  - Revisit trigger: macOS channel proves out, or external demand

- Item: Windows binaries
  - Source: proposal §Out of scope
  - Reason: not in source plan; no demand signal
  - Revisit trigger: explicit user request

- Item: Replacing the npm channel
  - Source: proposal §Out of scope
  - Reason: channels ship in parallel by design
  - Revisit trigger: never (architectural decision, not deferral)

- Item: Asset filename versioning (`duo-vX.Y.Z-darwin-arm64`)
  - Source: proposal §Open questions
  - Reason: Channel 4 assumes unversioned filenames; flipping later is a breaking contract change
  - Revisit trigger: Channel 4 design changes, or a multi-version distribution requirement emerges

- Item: Node SEA fallback path
  - Source: proposal §Risks (escape hatch)
  - Reason: only relevant if Bun compat proves unworkable; not currently planned
  - Revisit trigger: Step 1 smoke script fails irrecoverably on a critical compat issue

## Open Questions

The proposal lists three open questions; all have defensible defaults and are resolved as deferred decisions above. None block confident planning.

- ~~Codesigning/notarization in v2 vs wait for friction reports?~~ — default: wait. (Resolved: deferred.)
- ~~Linux binary now or stage separately?~~ — default: stage separately. (Resolved: out-of-scope for this proposal.)
- ~~Asset naming: `duo-darwin-arm64` vs `duo-vX.Y.Z-darwin-arm64`?~~ — default: unversioned filenames (Channel 4 contract). (Resolved in Step 2.)

Optional refinement (does NOT block start):

- Question: Should the smoke MCP stdio handshake be a real handshake against a known tool list, or just a process-level "server starts and responds to `initialize`" probe?
  - Why it matters: a deeper handshake catches more `@modelcontextprotocol/sdk`/Bun-stdio surprises; a shallow probe is faster and less brittle
  - Blocks roadmap? no
  - Suggested owner: builder discretion during Step 1; document the chosen depth in the smoke script

- Question: Should Step 2 also include a "delete the rc release after green smoke" cleanup step, or leave the rc tags as historical artifacts?
  - Why it matters: prevents Release page clutter from rc iteration
  - Blocks roadmap? no
  - Suggested owner: builder discretion / human preference at workplan time

## Recommendation

Proceed to:

- [x] Draft `notes/roadmap/roadmap-N.md` step entries from §Proposed Roadmap Shape above (two steps)
- [x] Draft `notes/roadmap/step-NN-workplan.md` for Step 1 (Step 2 workplan can be deferred until Step 1 ships, since Step 2 is gated on Step 1's smoke success)
- [ ] Refine planning inputs first

Rationale: The proposal is well-scoped with a clean two-step decomposition that meaningfully separates the Bun-compat unknowns (Step 1) from the CI plumbing (Step 2). Shipping criteria are concrete and externally verifiable. The medium risk is real but contained — the smoke script is the explicit gate, and the escape hatch (Node SEA) is acknowledged. No source-input gaps. Recommend **start Step 1 now** rather than further refinement.

**Sequencing note**: This channel is independent of Channel 1 (npm bundle) and can run in either order. It is a *prerequisite* for Channel 4 (install UX) — Channel 4's `curl | sh` and Homebrew tap consume the Release assets this proposal produces. If the round prioritizes Channel 4, Channel 2 must land first.

**Next action**: `orchestrator start-next-round` → spawn step-NN-coordinator to draft the Step 1 workplan from this intake.

## Human Review Notes

(append review decisions here)
