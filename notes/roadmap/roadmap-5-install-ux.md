# Roadmap 5 — Install UX: GitHub Releases / curl|sh / Homebrew tap (Channel 4)

**Project**: Duo
**Status**: in-progress
**Started**: 2026-05-03
**Round Focus**: Three macOS install surfaces on top of Channel 2 binary releases, in increasing order of blast radius.

**Proposal**: `notes/proposals/duo-packaging-install-ux.md`
**Intake**: `notes/proposals/duo-packaging-install-ux-intake.md`

---

## Summary

Layer three macOS install surfaces on top of the Channel 2 binary releases (`duo-darwin-arm64` / `duo-darwin-x64`), in increasing order of blast radius:

1. **GitHub Releases polish** — Release-notes template documenting the `xattr -d com.apple.quarantine` workaround; first post-Channel-2 release uses it; fresh-user verify.
2. **curl|sh installer** — Bash script with arch detection, install path smarts, dequarantine, and PATH hints; stable URL; end-to-end tested on fresh VM.
3. **Homebrew tap** — `procrastivity/homebrew-tap` with auto-updating formula; cross-repo PR plumbing; sha256 computed post-upload.

All three steps consume the same Channel 2 contract: stable filenames and predictable Release URLs. This channel is **strictly downstream of Channel 2**; Channel 2 must ship first with the locked asset filenames.

**Prerequisite**: Channel 2 (Bun binaries) Step 2 CI release workflow shipped with v0.1.4-rc.0 tag and locked asset filenames `duo-darwin-arm64` / `duo-darwin-x64`.

---

## Step 1 — GitHub Releases Polish

**Goal**: Every Release page tells a macOS user clearly how to use the binary, including the first-run `xattr` workaround.

**Workplan**: `notes/roadmap/step-01-workplan.md` (to be generated)

**Shipping criteria**:

- [ ] A release-notes template exists (in `.github/release-template.md` or inline in `release-bin.yml`) that documents the `xattr -d com.apple.quarantine` workaround.
- [ ] First post-Channel-2 release uses the template.
- [ ] Verifiably useful: a fresh user, told only "go to the Releases page", can download, dequarantine, and run.

**Deferred decisions resolved in this step**:

- Decision: Template lives alongside `release-bin.yml` (in `.github/`) rather than as a separate docs page (source: proposal §Step 1 shipping criteria).

**New deps**: None.

**Risk**: Low. Pure documentation against an asset contract Channel 2 already locks.

---

## Step 2 — curl | sh Installer

**Goal**: `curl -fsSL <stable-url> | sh` installs `duo` on macOS in one command, with arch detection, sane install path, dequarantine, and PATH hinting.

**Workplan**: (to be generated; deferred until Step 1 ships)

**Shipping criteria** (lifted from intake):

- [ ] `scripts/install.sh` exists, executable, idempotent (re-run upgrades in place).
- [ ] Arch detection covers `Darwin arm64` and `Darwin x86_64`; unknown combos exit nonzero with a clear message ("duo currently only ships macOS binaries; use npm or nix instead").
- [ ] Default install path: `~/.local/bin/duo`. Sudo-invocation install path: `/usr/local/bin/duo`. No auto-elevation.
- [ ] PATH hint prints only when the install dir is not on `$PATH`.
- [ ] `xattr -d com.apple.quarantine` runs (best-effort) on macOS; no-ops if attribute is absent.
- [ ] Stable URL resolved (deferred decision in this step). End-to-end verified in a fresh macOS VM or clean container: `curl -fsSL <url> | sh` → `duo --help` works.

**Deferred decisions to resolve in this step**:

- Stable URL choice — `raw.githubusercontent.com/.../main/scripts/install.sh` vs `gh-pages`/`docs` site.
- Sudo behavior — silent install to `/usr/local/bin` when `EUID=0` (default).

**New deps**: None in code.

**Risk**: Low–medium. Mostly bash hygiene and the `uname` matrix; principal hazard is a poor stable-URL choice creating cleanup churn later.

---

## Step 3 — Homebrew Tap

**Goal**: `brew install procrastivity/tap/duo` works on both macOS arches, and the formula auto-updates on each release via a cross-repo PR.

**Workplan**: (to be generated; deferred until Step 2 ships)

**Shipping criteria** (lifted from intake):

- [ ] Repo `procrastivity/homebrew-tap` exists with `Formula/duo.rb`.
- [ ] Initial formula manually populated for the most recent release; `brew install procrastivity/tap/duo && duo --help` works on both arm64 and x64 Macs.
- [ ] `release-bin.yml` extended with a tap-update job that, on every successful release, opens a PR against `procrastivity/homebrew-tap` bumping `version` and `sha256` for both arch assets.
- [ ] sha256 in the auto-PR is computed from the actual uploaded asset (post-upload), not the local pre-upload binary.
- [ ] Cross-repo PR auth bootstrap documented (which token, what scope, how to rotate).

**Deferred decisions to resolve in this step**:

- Auto-PR vs auto-push to tap repo (default: PR for review checkpoint and audit trail).
- Tap repo name (fixed as `procrastivity/homebrew-tap`).
- Fine-grained PAT vs GitHub App installation token (builder discretion at workplan time; no block).

**New deps**: New repo `procrastivity/homebrew-tap`; cross-repo write token in CI secrets.

**Risk**: Medium. Highest blast radius of the three — second repo to maintain, cross-repo PR plumbing, token handling. Source plan orders it last for this reason. Mitigation: auto-PR (not auto-push) gives a review checkpoint, and the bootstrap doc keeps the token rotateable.

---

## Out of Scope

- Linux installer support (no Linux binary; tracked as Channel 2 follow-up).
- macOS codesigning / notarization (signed binaries are a v2 goal; Steps 1–3 paper over with `xattr` workaround).
- Non-GitHub-Releases hosting (CDN, S3).
- `docs/PUBLISHING.md` cross-links (tracked as a follow-up after any step ships).

---

## Notes

This channel is strictly downstream of Channel 2. All three steps consume the locked filename contract `duo-darwin-arm64` / `duo-darwin-x64`. Channel 2 Step 2 must ship at v0.1.4-rc.0 or later before Round 5 work begins. Independent of Channels 1 and 3 — npm bundle and Nix flake are alternative install surfaces.

**Next action**: Start Step 1 (release-notes template polish — low risk, immediate user-visible win), then Step 2 (`install.sh`), then Step 3 (Homebrew tap). Step workplans are deferred until each predecessor ships.
