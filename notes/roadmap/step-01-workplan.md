# Step 01 Workplan — GitHub Releases Polish (Release-Notes Template)

**Roadmap**: `notes/roadmap/roadmap-5-install-ux.md`
**Status**: approved (orchestrator decisions recorded 2026-05-03)
**Generated**: 2026-05-03

---

## Overview

A single, low-risk documentation task: create a release-notes template in `.github/` that documents the `xattr -d com.apple.quarantine` workaround for macOS users. First post-Channel-2 release (next `v*` tag after v0.1.4-rc.0) uses it. Fresh-user verification ensures the Release page is self-contained — no external links required to get a working `duo` binary.

**Effort**: Very small (1–2 hours). This is a template creation + one release using it + a fresh-user smoke test.

**Risk**: Low. Pure markdown documentation. No code changes, no CI plumbing, no external dependencies. Rollback is a simple revert.

---

## Scope

### Files to Create

- `.github/release-template.md` — release-notes template with embedded `xattr` instructions and quick-start for both arm64 and x64 binaries.

### Files to Reference (No Changes)

- `notes/roadmap/roadmap-5-install-ux.md` — links this workplan as Step 1 target.
- `.github/workflows/release-bin.yml` — to understand which binaries are attached to each release (already ships them; this step just documents their use).

### Verification

- Template created and committed to main.
- Next `v*` tag push uses the template (release notes auto-populate with it).
- Fresh macOS VM test: download arm64 or x64 binary from Release page, run `xattr -d com.apple.quarantine ./duo-darwin-*`, then `./duo-darwin-* --help` succeeds.

---

## Tasks

| # | Task | Effort | Owner |
|---|------|--------|-------|
| 1 | Create `.github/release-template.md` with `xattr` doc | 30 min | — |
| 2 | Review template against Channel 2's binary contract (filenames, structure) | 15 min | — |
| 3 | Tag a release post-merge (e.g., v0.1.5-rc.0 or v0.1.5); verify Release page uses the template | 30 min | — |
| 4 | Fresh macOS VM test: download, dequarantine, run | 30 min | — |

Total: ~2 hours

---

## Design Notes

### Release-Notes Template Location

**Decision**: `.github/release-template.md` in the repo (not a separate docs file).

**Why**: Keeps the user-facing install instructions co-located with the workflow that produces the assets. GitHub's release UI auto-populates release notes from this file for manually-drafted releases; it's the standard pattern. If we later wire an auto-notes generator (Step 2+3 context), the template is the natural handoff point.

### Content Shape

The template should cover:

1. **Header**: "Binary releases for macOS" or similar (2–3 lines).
2. **Arch selection**: Brief guidance on which file to download (arm64 vs x64) — link to `uname -m` output or a quick detection script.
3. **Dequarantine step**: `xattr -d com.apple.quarantine ./duo-darwin-arm64` (copy-paste ready).
4. **Quick test**: `./duo --help` to verify it runs.
5. **Next steps**: Hint at `curl | sh` installer (Step 2) and Homebrew tap (Step 3) as future options; note that they are not yet available (call this out explicitly so users don't fish for them).
6. **Optional**: SHA256 checksums if the release includes them (easy to add post-hoc via GitHub release UI; no need to compute programmatically yet).

The template is **GitHub Markdown** (not a bash script; not a shell one-liner). It lives in the release UI.

### Verification Scope

- **Scope in**: Release page documents the workaround clearly enough for a macOS user to succeed unaided.
- **Scope out**: Codesigning, notarization, or improving the first-run friction itself — those are v2 work. This step explicitly accepts the `xattr` workaround as-is.

---

## Acceptance Criteria

- [ ] `.github/release-template.md` exists and is valid markdown.
- [ ] Template includes arch selection guidance, `xattr` command (copy-paste ready), and a test (e.g., `--help`).
- [ ] A release is published using the template (next `v*` tag after v0.1.4-rc.0).
- [ ] Fresh macOS VM test passes: download, dequarantine, run `--help` successfully without external docs.
- [ ] Existing CI workflows (e.g., `release-bin.yml`) are unmodified; this step adds only the template.

---

## Decisions from Intake

- **Stable URL for `install.sh`** (Step 2 concern): deferred to Step 2 workplan.
- **Sudo behavior in `install.sh`** (Step 2): deferred to Step 2 workplan.
- **Cross-repo tap auth** (Step 3): deferred to Step 3 workplan.

---

## Open Questions

None that block start. The template location and content are straightforward. If questions arise during implementation (e.g., what about non-macOS releases? what about future signed builds?), they can be escalated or deferred to the follow-up step/retro.

---

## Risk & Mitigation

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| Template is unclear or incomplete | Low | Low | Fresh user test catches it before step ships |
| Release page rendering differs from local preview | Very Low | Low | GitHub's release UI is stable; preview locally before publishing |
| Users expect notarized binaries now | Medium | Low | Template explicitly states `xattr` is the current workaround; next step (Step 2) will re-emphasize this; v2 commitment notes codesigning |

---

## Build Sequence

Single-task step. Create the template, commit it, test a release, verify on a fresh VM, done. No dependencies on other steps (downstream: Step 2 and 3 reference the Release page pattern, but don't depend on this specific template content).
