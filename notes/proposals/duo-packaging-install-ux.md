# duo packaging — install UX (GitHub Releases / curl|sh / Homebrew tap) (Channel 4)

**Status**: draft
**Date**: 2026-05-03
**Profile**: solo-local-cli
**Source input**: `~/.claude/plans/i-want-to-better-peppy-shamir.md` (Channel 4, items a/b/c)

## Summary

Three planned install surfaces for the Channel 2 macOS binaries, rolled out incrementally: (a) GitHub Releases artifacts (already falls out of Channel 2), (b) a `curl | sh` installer hosted in the repo, (c) a Homebrew tap at `procrastivity/homebrew-tap`. Each step compounds on the previous; (a) is essentially free given Channel 2 lands, and (c) is the highest-touch because it adds a second repo to maintain.

## Behavior boundary

After all three steps land:

- (a) Every `vX.Y.Z` tag automatically produces a GitHub Release page with `duo-darwin-arm64` and `duo-darwin-x64` attached. (This is Channel 2's output; nothing extra to do for (a) beyond polishing the Release notes template.)
- (b) `curl -fsSL <stable-url> | sh` on a Mac downloads the right binary, installs it to `~/.local/bin/duo` (or `/usr/local/bin` if invoked with sudo), strips the quarantine xattr, and prints a PATH hint.
- (c) `brew install procrastivity/tap/duo` installs the right macOS binary via Homebrew. The tap formula auto-updates on each release.

Out of scope:

- Linux installer support (defer until a Linux binary exists).
- `apt`/`dnf`/`pacman` packaging.
- Any installer that downloads from a place other than GitHub Releases (no CDN, no S3 bucket).

## Inputs / outputs

**Inputs**: GitHub Release assets produced by Channel 2 — specifically `duo-darwin-arm64` and `duo-darwin-x64` with stable filenames at predictable URLs (`https://github.com/procrastivity/duo/releases/download/vX.Y.Z/duo-darwin-<arch>`).

**Outputs**:

- (a) Release notes template that documents the `xattr` workaround.
- (b) `scripts/install.sh` committed to the repo; served via either `https://raw.githubusercontent.com/procrastivity/duo/main/scripts/install.sh` or via a `gh-pages`/`docs` site if a stable URL is wanted (Open Question).
- (c) A new repository `procrastivity/homebrew-tap` containing `Formula/duo.rb`. A tap-update step in `release-bin.yml` opens a PR against the tap repo with the new version + sha256 on each release.

## Edge cases & error handling

- **Arch detection in `install.sh`**: uses `uname -sm`; must distinguish `Darwin arm64` vs `Darwin x86_64` and pick the right asset. Unknown combinations exit nonzero with a clear message ("duo currently only ships macOS binaries; use npm or nix instead").
- **PATH hint accuracy**: detects whether `~/.local/bin` is on PATH and only prints the "add this to your shell rc" hint when it isn't.
- **Quarantine xattr**: `xattr -d com.apple.quarantine` is best-effort; no-ops if the attribute isn't present (older macOS, or the file wasn't downloaded via a quarantining transport).
- **sudo install path**: if invoked as root, install to `/usr/local/bin/duo`; otherwise `~/.local/bin/duo`. Don't auto-elevate.
- **Homebrew formula sha256 drift**: the auto-PR must compute sha256 from the actual uploaded asset, not from the local pre-upload binary, to catch any post-build mutation by GitHub's release pipeline.
- **Tap repo PR auth**: the cross-repo PR needs a token with write access to `procrastivity/homebrew-tap`. Document the token setup as a one-time bootstrap.

## Integration points

- **Channel 2 (Bun binaries)** — strict prerequisite. All three steps consume Channel 2's Release assets. Asset filenames must be stable: `duo-darwin-arm64`, `duo-darwin-x64`. If Channel 2 changes its naming convention, all three steps break.
- **`.github/workflows/release-bin.yml`** (Channel 2) — Step 3 (Homebrew) extends this workflow with a tap-update job that runs after the binaries upload successfully.
- **New repo `procrastivity/homebrew-tap`** — created in Step 3. Adds a second maintenance surface; this is the explicit reason the source plan orders it last.
- **`docs/PUBLISHING.md`** — the existing npm install path doc may want a "Other install channels" section pointing at the three new options. Not required to land in this channel; tracked as a follow-up.

## Implementation notes (preserved from source plan)

All three are *planned* now; pick implementation order later. Recommended order: (a) GitHub Releases artifacts (falls out of Channel 2), (b) `curl | sh` installer, (c) Homebrew tap.

### (a) GitHub Releases

Already covered by Channel 2's workflow. No extra work beyond a release-notes template that mentions the `xattr` workaround for first-run on macOS.

### (b) `curl | sh` installer

- New `scripts/install.sh` (committed to repo, served via `https://raw.githubusercontent.com/procrastivity/duo/main/scripts/install.sh` or via a `gh-pages`/`docs` site if we want a stable URL).
- Detects `uname -sm`, picks the right release asset, downloads to `~/.local/bin/duo` (or `/usr/local/bin` with `sudo`), `chmod +x`, runs `xattr -d com.apple.quarantine` on macOS, prints a "duo installed; ensure ~/.local/bin is on PATH" hint.
- Verify in a fresh macOS VM / clean container: `curl -fsSL <url> | sh` → `duo --help` works.

### (c) Homebrew tap

- New repo `procrastivity/homebrew-tap` containing `Formula/duo.rb`.
- Formula downloads the macOS binary from Releases, verifies sha256, installs to `bin/duo`. Update on each release via a tap-update step in `release-bin.yml` (open a PR against the tap repo with the new version + sha).
- Verify: `brew install procrastivity/tap/duo && duo --help`.

## Roadmap shape

### Step 1 — GitHub Releases polish (a)

**Goal**: every release page tells the user clearly how to use the binary on macOS.

**Shipping criteria**:

- [ ] A release-notes template exists (in `.github/release-template.md` or inline in `release-bin.yml`) that documents the `xattr -d com.apple.quarantine` workaround.
- [ ] First post-Channel-2 release uses the template and is verifiably useful: a fresh user, told only "go to the Releases page", can download, dequarantine, and run.

**New deps**: none.

**Risk**: low.

### Step 2 — `curl | sh` installer (b)

**Goal**: `curl -fsSL <url> | sh` installs `duo` on macOS in one command.

**Shipping criteria**:

- [ ] `scripts/install.sh` exists, executable, idempotent (re-run upgrades in place).
- [ ] Arch detection covers `Darwin arm64` and `Darwin x86_64`; unknown combos exit clearly.
- [ ] Default install path: `~/.local/bin/duo`. Sudo-invocation install path: `/usr/local/bin/duo`.
- [ ] PATH hint prints only when the install dir is not on `$PATH`.
- [ ] `xattr -d com.apple.quarantine` runs (best-effort) on macOS.
- [ ] Stable URL resolved (Open Question). Verified end-to-end in a fresh macOS VM or clean container.

**Deferred decisions resolved in this step**:

- Stable URL: pick between `raw.githubusercontent.com/.../main/scripts/install.sh` (zero infra, version churn affects URL stability minimally) vs `gh-pages` site at e.g. `https://procrastivity.github.io/duo/install.sh` (requires gh-pages setup; URL won't move if the script relocates in-repo).

**New deps**: none in code; possibly a `gh-pages` branch.

**Risk**: low–medium. Mostly bash hygiene + uname matrix; main hazard is the stable-URL choice creating cleanup churn later.

### Step 3 — Homebrew tap (c)

**Goal**: `brew install procrastivity/tap/duo` works, and the formula auto-updates on each release.

**Shipping criteria**:

- [ ] Repo `procrastivity/homebrew-tap` exists with `Formula/duo.rb`.
- [ ] Initial formula manually populated for the most recent release; `brew install procrastivity/tap/duo && duo --help` works on both arm64 and x64 Macs.
- [ ] `release-bin.yml` extended with a tap-update job that, on every successful release, opens a PR against `procrastivity/homebrew-tap` bumping `version` and `sha256` for both arch assets.
- [ ] Cross-repo PR auth bootstrap documented (which token, what scope, how to rotate).

**Deferred decisions resolved in this step**:

- PR vs direct push to tap repo. Default: PR (gives a review checkpoint and an audit trail). Switch to direct push only if PR latency causes user-visible install delays.

**New deps**: new repo `procrastivity/homebrew-tap`; a cross-repo write token in CI secrets.

**Risk**: medium. Highest blast radius of the three — second repo, token handling, cross-repo PR plumbing. Source plan orders it last for this reason.

## Coverage map

| Source item (i-want-to-better-peppy-shamir.md, Channel 4) | Step | Status | Notes |
|---|---|---|---|
| (a) GitHub Releases artifacts | Step 1 | planned | falls out of Channel 2; this step adds the notes template |
| (b) `scripts/install.sh` | Step 2 | planned | uname matrix + quarantine + PATH hint |
| (b) Stable install URL | Step 2 | planned | Open Question; raw.githubusercontent vs gh-pages |
| (b) Fresh-VM verification | Step 2 | planned | shipping criterion |
| (c) `procrastivity/homebrew-tap` repo | Step 3 | planned | new repo |
| (c) `Formula/duo.rb` | Step 3 | planned | sha256 + version on each release |
| (c) Tap-update step in `release-bin.yml` | Step 3 | planned | auto-PR against tap repo |
| (c) `brew install` end-to-end verify | Step 3 | planned | shipping criterion |

## Open questions

- **Stable URL for `install.sh`**: `raw.githubusercontent.com/procrastivity/duo/main/scripts/install.sh` (zero infra) vs a `gh-pages`/`docs` site (resilient to in-repo relocation). Pick before Step 2 ships.
- **Sudo behavior in `install.sh`**: silently install to `/usr/local/bin` when invoked with sudo, or always require explicit `--prefix`? (Default: silent install to `/usr/local/bin` when `EUID=0`.)
- **Homebrew tap auto-PR vs auto-push**: PR for review checkpoint, or direct push for lower latency? (Default: PR.)
- **Linux support deferral**: when does the Linux story start? (Out of scope here; tracked as a follow-up to Channel 2.)

## Risks

- **macOS Gatekeeper** (shared with Channel 2): unsigned binaries require `xattr` removal. Steps 2 + 3 paper over this in the installer/formula; Step 1 just documents.
- **Asset-name coupling**: all three steps assume `duo-darwin-arm64` / `duo-darwin-x64` as stable filenames. A Channel 2 rename would break all three; coordinate with Channel 2's Open Questions.
- **Tap repo as a second maintenance surface**: the source plan flags this explicitly. Default mitigation is the auto-PR; if that breaks, manual updates become drag.
- **Stable-URL drift**: a poor choice in Step 2 means doc churn every time the install script moves in-repo. The `gh-pages` option insulates against this but adds setup work.
