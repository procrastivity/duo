# Proposal Intake — duo packaging (install UX: GitHub Releases / curl|sh / Homebrew tap, Channel 4)

**Status**: draft
**Date**: 2026-05-03
**Intake inputs**:

- `notes/proposals/duo-packaging-install-ux.md` — drafted Channel 4 proposal
- `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 4, items a/b/c) — original source plan referenced by the proposal (treated as already distilled)
- `notes/proposals/duo-packaging-bun-binaries.md` + `…-intake.md` — strict upstream prerequisite (Channel 2)
- `notes/proposals/duo-packaging-{npm-bundle,nix-flake}.md` — sibling channels (sequencing context)
- `notes/roadmap/archive/`, `docs/PUBLISHING.md` — current state cross-check

---

## Summary

Layer three macOS install surfaces on top of the Channel 2 binary releases, in increasing order of blast radius: (a) polish the GitHub Releases page so a user told only "go to Releases" can succeed, (b) add a hosted `scripts/install.sh` so `curl -fsSL <url> | sh` installs `duo` with arch detection, PATH hint, and `xattr` dequarantine, and (c) stand up `procrastivity/homebrew-tap` with a `Formula/duo.rb` that auto-updates on each release. All three steps consume the same Channel 2 contract: stable filenames `duo-darwin-arm64` / `duo-darwin-x64` at predictable Release URLs.

## Source Inputs

| Source | Type | Role in intake |
|---|---|---|
| `notes/proposals/duo-packaging-install-ux.md` | proposal | primary |
| `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 4) | idea / source plan | background (already distilled) |
| `notes/proposals/duo-packaging-bun-binaries.md` | sibling proposal | prerequisite (asset producer) |
| `notes/proposals/duo-packaging-bun-binaries-intake.md` | sibling intake | prerequisite (locks asset filename contract) |
| `notes/proposals/duo-packaging-npm-bundle.md` | sibling | background (independent install channel) |
| `notes/proposals/duo-packaging-nix-flake.md` | sibling | background (independent install channel) |
| `docs/PUBLISHING.md` | current state | supporting (follow-up doc target, not in scope) |
| `notes/roadmap/archive/` | shipped scope | supporting (no overlap; greenfield install surfaces) |

## Candidate Outcomes

- Outcome: A user told only "go to the Releases page" can download, dequarantine, and run `duo` on macOS
  - Source: proposal §Step 1 shipping criteria
  - User-visible result: each `vX.Y.Z` Release page documents the `xattr -d com.apple.quarantine` workaround inline
  - Verification signal: a fresh user follows the Release notes start-to-finish and gets a working `duo --help`

- Outcome: `curl -fsSL <stable-url> | sh` installs `duo` on macOS in one command
  - Source: proposal §Behavior boundary (b), §Step 2
  - User-visible result: arch is detected, the right asset downloads, install lands at `~/.local/bin/duo` (or `/usr/local/bin` under sudo), quarantine is stripped, PATH hint prints only when needed
  - Verification signal: end-to-end on a fresh macOS VM / clean container — `curl -fsSL <url> | sh` then `duo --help` succeeds

- Outcome: `brew install procrastivity/tap/duo` works and stays current
  - Source: proposal §Behavior boundary (c), §Step 3
  - User-visible result: tap install path on both arm64 and x64 Macs, formula bumps on every release without manual edits
  - Verification signal: `brew install procrastivity/tap/duo && duo --help` on both arches; a release after the auto-PR plumbing lands generates a tap PR with the correct version + sha256

- Outcome: Unknown arch combinations fail loudly, not silently
  - Source: proposal §Edge cases & error handling (arch detection)
  - User-visible result: `Linux x86_64` (or any unsupported combo) gets a clear "use npm or nix instead" message, not a 404 or a misnamed binary
  - Verification signal: `install.sh` exits nonzero with a known message when `uname -sm` is not a supported macOS combo

- Outcome: Tap formula sha256 reflects the actually-uploaded asset
  - Source: proposal §Edge cases (Homebrew formula sha256 drift)
  - User-visible result: `brew install` never fails because the recorded sha256 was computed pre-upload and diverged from what GitHub serves
  - Verification signal: tap-update job computes sha256 by re-downloading the published asset, not from the local pre-upload binary

- Outcome: Cross-repo PR plumbing is documented and rotateable
  - Source: proposal §Edge cases (Tap repo PR auth)
  - User-visible result: a future maintainer can rotate the cross-repo write token without spelunking
  - Verification signal: a one-page bootstrap doc names the token, scope, and rotation procedure

## Proposed Roadmap Shape

The proposal already defines a clean three-step decomposition ordered by blast radius (release-page polish → installer script → cross-repo Homebrew tap). Intake recommendation: adopt verbatim. The split is meaningful — Step 1 is essentially free given Channel 2 ships, Step 2 is a contained bash + URL-stability question, and Step 3 introduces a second repo and cross-repo CI auth that warrants its own gate.

### Step 1 — GitHub Releases polish (a)

**Goal**: Every Release page tells a macOS user clearly how to use the binary, including the first-run `xattr` workaround.

**Shipping criteria** (lifted from proposal):

- [ ] A release-notes template exists (in `.github/release-template.md` or inline in `release-bin.yml`) that documents the `xattr -d com.apple.quarantine` workaround.
- [ ] First post-Channel-2 release uses the template.
- [ ] Verifiably useful: a fresh user, told only "go to the Releases page", can download, dequarantine, and run.

**Deferred decisions resolved in this step**:

- Decision: Template lives alongside `release-bin.yml` (in `.github/`) rather than as a separate docs page
  - Source: proposal §Step 1 shipping criteria (template location options)
  - Why this step: keeps the user-visible install instructions co-located with the workflow that produces the assets

**New deps**:

- (none)

**Risk**: low. Pure documentation against an asset contract Channel 2 already locks.

**Source coverage**:

- `duo-packaging-install-ux.md` §Step 1 → adopted verbatim
- `duo-packaging-install-ux.md` §Implementation notes (a) → release-notes template

### Step 2 — `curl | sh` installer (b)

**Goal**: `curl -fsSL <stable-url> | sh` installs `duo` on macOS in one command, with arch detection, sane install path, dequarantine, and PATH hinting.

**Shipping criteria** (lifted from proposal):

- [ ] `scripts/install.sh` exists, executable, idempotent (re-run upgrades in place).
- [ ] Arch detection covers `Darwin arm64` and `Darwin x86_64`; unknown combos exit nonzero with a clear message ("duo currently only ships macOS binaries; use npm or nix instead").
- [ ] Default install path: `~/.local/bin/duo`. Sudo-invocation install path: `/usr/local/bin/duo`. No auto-elevation.
- [ ] PATH hint prints only when the install dir is not on `$PATH`.
- [ ] `xattr -d com.apple.quarantine` runs (best-effort) on macOS; no-ops if attribute is absent.
- [ ] Stable URL resolved (see Open Questions). End-to-end verified in a fresh macOS VM or clean container: `curl -fsSL <url> | sh` → `duo --help` works.

**Deferred decisions resolved in this step**:

- Decision: Stable URL choice — `raw.githubusercontent.com/.../main/scripts/install.sh` vs `gh-pages`/`docs` site
  - Source: proposal §Open questions, §Step 2 deferred decisions
  - Why this step: the stable URL is a public commitment; choosing late means doc churn every time the script relocates in-repo
- Decision: Sudo behavior — silent install to `/usr/local/bin` when `EUID=0`
  - Source: proposal §Open questions (default: silent)
  - Why this step: defines the externally observable contract before the installer goes public

**New deps**:

- None in code; possibly a `gh-pages` branch depending on stable-URL choice.

**Risk**: low–medium. Mostly bash hygiene and the `uname` matrix; principal hazard is a poor stable-URL choice creating cleanup churn later.

**Source coverage**:

- `duo-packaging-install-ux.md` §Step 2 → adopted verbatim
- `duo-packaging-install-ux.md` §Edge cases & error handling → arch detection, PATH hint, quarantine, sudo install path
- `duo-packaging-install-ux.md` §Implementation notes (b) → script shape and verification

### Step 3 — Homebrew tap (c)

**Goal**: `brew install procrastivity/tap/duo` works on both macOS arches, and the formula auto-updates on each release via a cross-repo PR.

**Shipping criteria** (lifted from proposal):

- [ ] Repo `procrastivity/homebrew-tap` exists with `Formula/duo.rb`.
- [ ] Initial formula manually populated for the most recent release; `brew install procrastivity/tap/duo && duo --help` works on both arm64 and x64 Macs.
- [ ] `release-bin.yml` extended with a tap-update job that, on every successful release, opens a PR against `procrastivity/homebrew-tap` bumping `version` and `sha256` for both arch assets.
- [ ] sha256 in the auto-PR is computed from the actual uploaded asset (post-upload), not the local pre-upload binary.
- [ ] Cross-repo PR auth bootstrap documented (which token, what scope, how to rotate).

**Deferred decisions resolved in this step**:

- Decision: Auto-PR vs auto-push to tap repo — default PR for review checkpoint and audit trail
  - Source: proposal §Open questions, §Step 3 deferred decisions
  - Why this step: the auth + plumbing is shaped by which path is chosen; flipping later means re-doing the tap-update job
- Decision: Tap repo name `procrastivity/homebrew-tap` (so `brew install procrastivity/tap/duo` resolves)
  - Source: proposal §Behavior boundary (c), §Inputs/outputs
  - Why this step: tap name becomes the public install command

**New deps**:

- New repo `procrastivity/homebrew-tap`
- Cross-repo write token in CI secrets (scope: `contents:write` on the tap repo, or PAT with equivalent rights)

**Risk**: medium. Highest blast radius of the three — second repo to maintain, cross-repo PR plumbing, token handling. Source plan orders it last for this reason. Mitigation: auto-PR (not auto-push) gives a review checkpoint, and the bootstrap doc keeps the token rotateable.

**Source coverage**:

- `duo-packaging-install-ux.md` §Step 3 → adopted verbatim
- `duo-packaging-install-ux.md` §Edge cases (sha256 drift, tap PR auth) → shipping criteria
- `duo-packaging-install-ux.md` §Implementation notes (c) → tap repo + formula + workflow extension

## Coverage Map

| Source item | Proposed step | Status | Notes |
|---|---|---|---|
| (a) GitHub Releases artifacts | Step 1 | planned | falls out of Channel 2; this step adds the notes template |
| (a) Release-notes template (`xattr` workaround) | Step 1 | planned | lives in `.github/` |
| (a) Fresh-user "follow the Release page" verify | Step 1 | planned | shipping criterion |
| (b) `scripts/install.sh` | Step 2 | planned | bash; idempotent; uname matrix |
| (b) Arch detection + clear error on unsupported combo | Step 2 | planned | shipping criterion |
| (b) Install path (`~/.local/bin` vs `/usr/local/bin` under sudo) | Step 2 | planned | no auto-elevation |
| (b) PATH hint (only when not on PATH) | Step 2 | planned | shipping criterion |
| (b) `xattr -d com.apple.quarantine` (best-effort) | Step 2 | planned | shipping criterion |
| (b) Stable install URL | Step 2 | planned | open question; resolved as deferred decision in Step 2 |
| (b) Fresh-VM / clean-container verification | Step 2 | planned | shipping criterion |
| (c) `procrastivity/homebrew-tap` repo | Step 3 | planned | new repo |
| (c) `Formula/duo.rb` (initial manual population) | Step 3 | planned | shipping criterion |
| (c) Tap-update job in `release-bin.yml` | Step 3 | planned | auto-PR against tap repo |
| (c) sha256 computed post-upload | Step 3 | planned | shipping criterion (drift mitigation) |
| (c) Cross-repo PR auth bootstrap doc | Step 3 | planned | shipping criterion |
| (c) `brew install` end-to-end verify on both arches | Step 3 | planned | shipping criterion |
| Linux installer support | — | out-of-scope | depends on a Linux binary, which Channel 2 doesn't ship |
| `apt`/`dnf`/`pacman` packaging | — | out-of-scope | not in source plan |
| Non-GitHub-Releases hosting (CDN, S3) | — | out-of-scope | not in source plan |
| `docs/PUBLISHING.md` "Other install channels" section | — | follow-up | proposal explicitly defers; not blocking |

## Deferred / Out-of-Scope Items

- Item: Linux installer support
  - Source: proposal §Behavior boundary (out of scope), §Open questions
  - Reason: no Linux binary exists; nothing to install
  - Revisit trigger: Channel 2 (or a sibling) starts producing Linux binaries

- Item: `apt`/`dnf`/`pacman` packaging
  - Source: proposal §Behavior boundary (out of scope)
  - Reason: not motivated by source plan; depends on Linux binaries first
  - Revisit trigger: Linux channel ships and demand emerges

- Item: Hosting binaries anywhere other than GitHub Releases (CDN, S3)
  - Source: proposal §Behavior boundary (out of scope)
  - Reason: source plan explicitly anchors on GitHub Releases as the single distribution surface
  - Revisit trigger: GitHub Release bandwidth/availability becomes a real constraint

- Item: macOS codesigning / notarization
  - Source: shared with Channel 2 §Risks; proposal §Risks (papers over with installer/formula `xattr`)
  - Reason: belongs to Channel 2's signing roadmap, not Channel 4
  - Revisit trigger: signed binaries land — Steps 2 + 3 can drop the `xattr` step

- Item: Auto-push (instead of auto-PR) to tap repo
  - Source: proposal §Open questions, §Step 3
  - Reason: PR gives review checkpoint and audit trail; latency hasn't proven to be a problem
  - Revisit trigger: PR latency causes user-visible install delays

- Item: `docs/PUBLISHING.md` "Other install channels" section
  - Source: proposal §Integration points
  - Reason: explicitly tracked as a follow-up; not blocking the install paths themselves
  - Revisit trigger: any of Steps 1–3 ship and need a discoverable cross-link from existing docs

## Open Questions

The proposal lists four open questions; three have defensible defaults and are resolved as deferred decisions inside their respective steps. One is genuinely unresolved but does not block start.

- ~~Stable URL for `install.sh` (`raw.githubusercontent.com` vs `gh-pages`)?~~ — resolved as a Step 2 deferred decision (must be picked before Step 2 ships).
- ~~Sudo behavior in `install.sh` (silent `/usr/local/bin` vs require `--prefix`)?~~ — default: silent install when `EUID=0`. (Resolved in Step 2.)
- ~~Homebrew tap auto-PR vs auto-push?~~ — default: PR. (Resolved in Step 3.)
- ~~Linux support deferral?~~ — out-of-scope here; tracked as a Channel 2 follow-up.

Optional refinement (does NOT block start):

- Question: Should Step 2's stable-URL choice be settled at intake time, or is it acceptable to enter Step 2 with both options open and let the workplan author decide?
  - Why it matters: the URL is a public commitment; picking it early avoids workplan-time debate, but the trade-off (zero infra vs relocation resilience) is genuinely close
  - Blocks roadmap? no
  - Suggested owner: human at workplan kickoff for Step 2

- Question: Should the cross-repo write token in Step 3 be a fine-grained PAT scoped to `procrastivity/homebrew-tap` only, or a GitHub App installation token?
  - Why it matters: a GitHub App is more rotateable and auditable but adds setup; a fine-grained PAT is faster to bootstrap
  - Blocks roadmap? no
  - Suggested owner: builder discretion / human preference at Step 3 workplan time

- Question: Should the installer record an install receipt (e.g., `~/.local/share/duo/install-version`) for later upgrade-in-place reasoning, or stay stateless?
  - Why it matters: stateless is simpler; a receipt enables a future `duo upgrade` self-check without re-curling
  - Blocks roadmap? no
  - Suggested owner: builder discretion in Step 2

## Recommendation

Proceed to:

- [x] Draft `notes/roadmap/roadmap-N.md` step entries from §Proposed Roadmap Shape above (three steps)
- [x] Draft `notes/roadmap/step-NN-workplan.md` for Step 1 (Steps 2 and 3 workplans can be deferred until each predecessor lands; Step 1 is a documentation-only delta and can also be bundled with Step 2 if a round prefers a single user-visible install ship)
- [ ] Refine planning inputs first

Rationale: The proposal is well-scoped with a three-step decomposition ordered explicitly by blast radius. Shipping criteria are concrete and externally verifiable. Open questions all have defensible defaults that are localized to the step where they bite. The medium risk in Step 3 (cross-repo plumbing, token handling) is real but contained by the auto-PR default and a documented bootstrap. No source-input gaps.

**Sequencing note**: This channel is **strictly downstream of Channel 2** (`duo-packaging-bun-binaries`). All three steps consume Channel 2's Release assets and depend on the locked filename contract `duo-darwin-arm64` / `duo-darwin-x64`. Channel 2 must ship first; in particular, a Channel 2 rename of those filenames would break all three steps. Independent of Channel 1 (npm bundle) and Channel 3 (nix flake) — they ship in parallel as alternative install surfaces.

**Recommended start**: do not start Channel 4 until Channel 2 Step 2 (CI release workflow) ships at least one rc tag with the locked asset filenames. Once that's done, **start Step 1 next** (release-notes template polish — low risk, immediate user-visible win), then Step 2 (`install.sh`), then Step 3 (Homebrew tap).

**Next action**: queue Channel 4 behind Channel 2 in round planning. When Channel 2 ships, run `orchestrator start-next-round` → spawn step-NN-coordinator to draft the Step 1 workplan from this intake.

## Human Review Notes

(append review decisions here)
