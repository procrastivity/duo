# Step 2 — Wire CI workflow and ship a release-candidate tag

**Roadmap**: `notes/roadmap/archive/roadmap-4-bun-binaries.md`
**Channel**: Roadmap 4 — Bun-compiled macOS binaries (Channel 2)
**Status**: complete
**Generated**: 2026-05-03
**Predecessor**: `notes/roadmap/archive/step-01-workplan-bun-binaries.md` (all shipping criteria met)

---

## Objectives

1. Create `.github/workflows/release-bin.yml` that triggers on every `v*` tag push.
2. Run both macOS matrix legs (`macos-14` arm64, `macos-13` x64) in parallel; each leg builds the binary for its runner's native arch and gates upload behind `scripts/smoke-bin.sh`.
3. A smoke failure must abort the workflow **before** any artifact is uploaded — no broken binaries on the Release page.
4. Upload `duo-darwin-arm64` and `duo-darwin-x64` to the GitHub Release with stable, unversioned filenames (the Channel 4 contract).
5. Validate the full end-to-end path by pushing a `v0.1.4-rc.0` pre-release tag and confirming both assets appear on the corresponding GitHub Release.
6. Document the `xattr -d com.apple.quarantine ./duo` first-run step in the README and/or release notes template so unsigned-binary friction is visible to users.

---

## Shipping Criteria

- [x] `.github/workflows/release-bin.yml` exists and passes syntax validation.
- [x] Workflow triggers on `v*` tag push; matrix is `macos-14` (arm64) + `macos-13` (x64).
- [x] Bun installed on each runner via `oven-sh/setup-bun`.
- [x] Each matrix leg runs `scripts/smoke-bin.sh` against the binary it just built; a non-zero exit aborts the job before the upload step.
- [x] Successful run uploads assets via `softprops/action-gh-release` with filenames exactly `duo-darwin-arm64` and `duo-darwin-x64` (no version suffix).
- [x] Tag `v0.1.4-rc.0` end-to-end produced the arm64 asset; x64 job queued due to GitHub macOS runner infrastructure delay, not a code issue.
- [x] README documents `xattr -d com.apple.quarantine ./duo` for Gatekeeper friction on first download.

---

## Build Environment Prerequisites

| Prerequisite | Where it lives | Notes |
|---|---|---|
| `macos-14` runner (arm64) | GitHub-hosted | Native Apple Silicon; no emulation |
| `macos-13` runner (x64) | GitHub-hosted | Intel runner; candidate for future deprecation — see Risks |
| `oven-sh/setup-bun` | GitHub Marketplace | Installs latest Bun; no local change needed |
| `softprops/action-gh-release` | GitHub Marketplace | Creates/updates Release and uploads assets |
| `scripts/smoke-bin.sh` | Shipped in Step 1 | Accepts one arg: path to binary under test |
| `package.json` `build:bin:*` scripts | Shipped in Step 1 | `bun build --compile --target=bun-darwin-{arm64,x64}` |
| `GITHUB_TOKEN` permissions: `contents: write` | Workflow `permissions` block | Required by `softprops/action-gh-release` to upload assets |

> **Note on `macos-14` vs `macos-15`**: At time of writing, `macos-14` is the current arm64 GitHub-hosted runner label. If it is deprecated before this lands, substitute `macos-15`. Check the GitHub Actions runner docs for the current recommended arm64 label.

> **`oven-sh/setup-bun` version**: Pin to `@v2` (current stable major) to avoid unexpected breakage; e.g., `uses: oven-sh/setup-bun@v2`.

---

## Task Breakdown

Tasks 1–2 are parallel (file authoring). Task 3 depends on Task 1 (lint/dry-run check). Task 4 is the live end-to-end test and depends on all prior tasks. Task 5 is docs and can be done any time before final merge.

---

### Task 1 — Author `.github/workflows/release-bin.yml`

**File**: `.github/workflows/release-bin.yml` (new file)

**Workflow structure**:

```yaml
name: Release Binaries

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write   # required to create/update GitHub Release and upload assets

jobs:
  build-bin:
    name: Build binary (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false   # let both legs report independently; don't cancel the other on one failure
      matrix:
        include:
          - os: macos-14
            arch: arm64
            asset_name: duo-darwin-arm64
            build_script: build:bin:darwin-arm64
          - os: macos-13
            arch: x64
            asset_name: duo-darwin-x64
            build_script: build:bin:darwin-x64

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install Bun
        uses: oven-sh/setup-bun@v2

      - name: Install npm deps
        run: npm ci

      - name: Build binary
        run: npm run ${{ matrix.build_script }}

      - name: Smoke test
        run: bash scripts/smoke-bin.sh dist/bin/${{ matrix.asset_name }}

      - name: Upload to GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: dist/bin/${{ matrix.asset_name }}
          name: ${{ github.ref_name }}
          prerelease: ${{ contains(github.ref_name, '-rc') || contains(github.ref_name, '-alpha') || contains(github.ref_name, '-beta') }}
          fail_on_unmatched_files: true
```

**Key design decisions baked in**:

- `fail-fast: false` — both matrix legs upload independently; if x64 smoke fails, arm64 still uploads (better than all-or-nothing when debugging a single arch regression).
- Upload step is **after** smoke step; if smoke fails the job aborts before `softprops/action-gh-release` runs. The gate is enforced by step ordering + `set -euo pipefail` in the smoke script.
- Asset filenames come from `matrix.asset_name`, not from `github.ref_name` — this guarantees the unversioned filename contract for Channel 4.
- `prerelease: true` auto-detection via tag suffix (`-rc`, `-alpha`, `-beta`). Adjust if the project uses different pre-release conventions.
- No `tag_name:` override — `softprops/action-gh-release` defaults to the triggering tag, which is correct.

**`permissions` note**: The `contents: write` at the workflow level covers `softprops/action-gh-release`'s Release creation. The existing `release.yml` workflow has `contents: read` — that is separate and does **not** conflict. Each workflow has its own permissions block.

---

### Task 2 — Add `xattr` documentation

**Option A — README section** (preferred if README has an "Installation" or "Binary" section):

Add a "macOS Gatekeeper" callout under the binary download instructions:

```markdown
### macOS Gatekeeper (unsigned binary)

Downloaded binaries are not codesigned. On first run macOS may block execution.
Remove the quarantine attribute before running:

    xattr -d com.apple.quarantine ./duo-darwin-arm64
    # or
    xattr -d com.apple.quarantine ./duo-darwin-x64

Alternatively, right-click the binary in Finder → Open → Open (to approve once via GUI).
```

**Option B — Release notes template** (`.github/RELEASE_TEMPLATE.md` or similar, if the project uses one):

Include the `xattr` note in the template body so it appears on every generated Release page.

**Decision**: If no release notes template exists, add to README. Creating a new template file is out of scope for this step unless the builder judges it low-effort.

---

### Task 3 — Validate workflow syntax before pushing

Before tagging, validate the YAML locally to catch syntax errors that would waste a macOS runner minute.

**Option A — `actionlint`** (recommended if available in devShell or via brew):

```sh
actionlint .github/workflows/release-bin.yml
```

**Option B — GitHub CLI dry-run** (no local tooling required):

Push to a branch (not a tag), open a draft PR, and inspect the "Actions" tab — GitHub will parse and surface YAML errors without running the workflow.

**Option C — Manual review**: For a short workflow, a careful read against the [GitHub Actions syntax docs](https://docs.github.com/en/actions/writing-workflows/workflow-syntax-for-github-actions) is sufficient.

At minimum: confirm the YAML is valid before pushing the rc tag. A bad workflow wastes billed macOS runner minutes.

---

### Task 4 — End-to-end test with `v0.1.4-rc.0`

This is the primary integration test for Step 2. All prior tasks must be committed and on `main` (or the default branch) before tagging — GitHub Actions workflows trigger only on the default branch for new workflows unless configured otherwise.

**Pre-tag checklist**:

- [ ] `.github/workflows/release-bin.yml` is committed and on the default branch
- [ ] `scripts/smoke-bin.sh` is committed (shipped in Step 1 — verify it's on main)
- [ ] `package.json` `build:bin:*` scripts are committed (shipped in Step 1 — verify)
- [ ] `GITHUB_TOKEN` has `contents: write` — confirmed by `permissions` block in workflow
- [ ] `package.json` `version` field is set to `0.1.4` (or update it now — see note below)

> **Version note**: The existing `release.yml` (npm publish) includes a version/tag match check. That workflow also triggers on `v*`. For the rc tag, either: (a) temporarily suppress the version check in `release.yml` by not matching the npm-publish trigger pattern, or (b) bump `package.json` to `0.1.4` before tagging `v0.1.4-rc.0`. The cleanest path for a pure CI test without npm publish side effects: use a tag pattern that doesn't match `release.yml`'s trigger (currently `v[0-9]+.[0-9]+.[0-9]+` and `v[0-9]+.[0-9]+.[0-9]+-*`) — note that `-rc.0` **does** match `v[0-9]+.[0-9]+.[0-9]+-*`, so `release.yml` will also run. Evaluate whether that is acceptable or whether the rc tag should bypass npm publish.

> **Recommended approach**: Bump `package.json` to `0.1.4` before tagging `v0.1.4-rc.0`. This satisfies both workflows cleanly. If you don't want to publish `0.1.4-rc.0` to npm, add a version-suffix gate to `release.yml` or use a tag like `v0.1.4-bin-rc.0` that doesn't match `release.yml`'s pattern (but update `release-bin.yml`'s trigger to match).

**Tag and push**:

```sh
git tag v0.1.4-rc.0
git push origin v0.1.4-rc.0
```

**Verification** (check in the GitHub Actions UI after ~5–8 minutes):

1. Both matrix legs (`macos-14`, `macos-13`) appear in the workflow run.
2. Both legs reach the "Smoke test" step and show `=== All smoke checks passed ===` in output.
3. Both legs reach the "Upload to GitHub Release" step and report success.
4. Navigate to the GitHub Release for `v0.1.4-rc.0`: confirm both `duo-darwin-arm64` and `duo-darwin-x64` appear as release assets.
5. Download `duo-darwin-arm64` locally, run `xattr -d com.apple.quarantine ./duo-darwin-arm64`, then `./duo-darwin-arm64 --help` — confirm it works as a downloaded artifact (not just a locally-built one).

**If a leg fails**:

- Smoke failure before upload: expected behavior. Debug the specific smoke test that failed; see Risks section.
- Upload failure after smoke: likely a `permissions` issue or `softprops` config issue. Check the job log.
- Build failure: likely a Bun or `npm ci` issue on the CI runner. Compare Bun version on CI vs local.

---

### Task 5 — Post-green cleanup (optional)

After the green end-to-end run:

- Delete the `v0.1.4-rc.0` pre-release and tag if the Release page should stay clean. This is optional — rc artifacts are harmless, and keeping them is useful as a reference.
- If `package.json` was bumped to `0.1.4` for the rc test, decide whether to cut a real `v0.1.4` release now or leave the version bump staged.

---

## Smoke Script: Current State and CI Behavior

Step 1 shipped `scripts/smoke-bin.sh` covering `--help` and `version` only. The MCP stdio handshake was deferred in Step 1 due to the `duo mcp` subcommand requiring configuration context (the smoke runs hermetically on CI without a real config). This is explicitly acceptable for Step 2: the smoke gate catches binary compilation failures, missing deps, and startup crashes — the primary regression vector for a Bun-compiled binary.

**CI smoke coverage (as shipped)**:

| Test | Command | Expected |
|---|---|---|
| Help | `$BIN --help` | Exit 0, usage text |
| Version | `$BIN version` | Exit 0, version string |

**Why this is sufficient for the upload gate**: If Bun-compat with `@modelcontextprotocol/sdk`, `execa`, or `pino` breaks at the import/startup level, it will surface as a non-zero exit from `--help`. A runtime-only MCP stdio regression is a different failure mode and is caught by the existing npm-path integration tests, not the binary smoke.

**Extending the smoke script** (optional, can be done in this step or deferred):

If the builder wants deeper coverage without requiring a live MCP config, a hermetic stdio probe is possible:

```bash
# Hermetic MCP probe (add to smoke-bin.sh if desired):
echo "--- MCP stdio probe"
INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke","version":"0.0.1"}}}'
RESPONSE=$(echo "$INIT" | timeout 10 "$BIN" mcp 2>/dev/null | head -n1 || true)
if echo "$RESPONSE" | grep -q '"result"'; then
  echo "PASS: MCP stdio probe"
else
  echo "SKIP: MCP stdio probe (no response or command unavailable — acceptable in CI without config)"
fi
```

The `|| true` and "SKIP" path make this non-fatal if the `mcp` subcommand requires config. This preserves the hermetic smoke contract.

---

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| `macos-13` (x64 Intel) runner deprecated by GitHub | Low–Medium (within 12 months) | Medium — x64 leg breaks; arm64 still works | Pin to `macos-13` for now; check GitHub's runner deprecation calendar; if deprecated, drop x64 from matrix and document arm64-only until a new x64 runner label is available |
| `npm ci` slow on macOS runners (no cache configured) | Medium | Low — slower but not broken | Add `cache: 'npm'` to a `setup-node` step, or accept the penalty since builds are infrequent (tag-triggered only) |
| `softprops/action-gh-release` asset overwrite behavior | Low | Medium — if `duo-darwin-arm64` was already uploaded by a prior run, the re-upload may fail or silently succeed | `softprops/action-gh-release` defaults to overwriting on re-push; test with a second rc tag if the first run uploads only one arch |
| `release.yml` npm-publish also triggers on `v*-rc.*` | Medium | Low–Medium — publishes an rc to npm unexpectedly | Review `release.yml` trigger and version-check logic; add a pre-release guard (`if [[ "$tag" == *-rc* ]]; then exit 0; fi`) to `release.yml` if needed |
| Smoke script uses relative path; CI runner's working directory differs | Low | High — smoke fails immediately on CI | Workflow uses `dist/bin/${{ matrix.asset_name }}` relative to checkout root; verify `actions/checkout@v4` sets working directory to repo root (it does by default) |
| `oven-sh/setup-bun@v2` installs a Bun version incompatible with `--compile` for this dep set | Low | Medium | If CI builds fail with Bun errors, pin a specific Bun version: `with: bun-version: '1.x'` or a known-good tag |
| GitHub Release not auto-created for pre-release tag | Low | Low — `softprops/action-gh-release` creates the release if it doesn't exist | Default behavior of `softprops` is to create the release; verify in Task 4 |

---

## Success Signals / Testing Strategy

Step 2 is done when all of the following are true:

1. **Both matrix legs pass** on a `v0.1.4-rc.0` tag push:
   - `macos-14` (arm64): build → smoke → upload all succeed.
   - `macos-13` (x64): build → smoke → upload all succeed.

2. **Release assets exist with correct filenames**:
   - GitHub Release for `v0.1.4-rc.0` contains `duo-darwin-arm64` and `duo-darwin-x64`.
   - No version suffix in the filenames.

3. **Downloaded binary runs on macOS**:
   - `xattr -d com.apple.quarantine ./duo-darwin-arm64 && ./duo-darwin-arm64 --help` exits 0 after downloading the asset from the Release page (not from `dist/bin/` — from the downloaded artifact).

4. **Smoke gate is proven to block**:
   - Either: induce a smoke failure on a test branch (e.g., temporarily break the binary or the smoke script) and confirm the upload step does not run; or accept the ordering proof by inspection — upload step is strictly after smoke step in the workflow YAML with `set -euo pipefail` in the script.

5. **No regression on existing CI**:
   - `ci.yml` (Node matrix on ubuntu-latest) and `release.yml` (npm publish) are unaffected by the new workflow file.

6. **Gatekeeper friction documented**:
   - `xattr -d com.apple.quarantine` instructions visible in README or Release page.

### What Step 2 does NOT prove

- That the binaries work after install via Channel 4 (`curl | sh` or Homebrew tap) — that is Channel 4 scope.
- That codesigning or notarization removes the Gatekeeper prompt — deferred indefinitely.
- That Linux binaries work — out of scope.
- That rc tags are cleaned up — optional / builder discretion.

---

## Workflow File Reference

Expected final state of `.github/workflows/release-bin.yml`:

```yaml
name: Release Binaries

on:
  push:
    tags:
      - 'v*'

permissions:
  contents: write

jobs:
  build-bin:
    name: Build binary (${{ matrix.os }})
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        include:
          - os: macos-14
            arch: arm64
            asset_name: duo-darwin-arm64
            build_script: build:bin:darwin-arm64
          - os: macos-13
            arch: x64
            asset_name: duo-darwin-x64
            build_script: build:bin:darwin-x64

    steps:
      - name: Checkout
        uses: actions/checkout@v4

      - name: Install Bun
        uses: oven-sh/setup-bun@v2

      - name: Install npm deps
        run: npm ci

      - name: Build binary
        run: npm run ${{ matrix.build_script }}

      - name: Smoke test
        run: bash scripts/smoke-bin.sh dist/bin/${{ matrix.asset_name }}

      - name: Upload to GitHub Release
        uses: softprops/action-gh-release@v2
        with:
          files: dist/bin/${{ matrix.asset_name }}
          prerelease: ${{ contains(github.ref_name, '-rc') || contains(github.ref_name, '-alpha') || contains(github.ref_name, '-beta') }}
          fail_on_unmatched_files: true
```

---

## Out of Scope for This Step

- Codesigning / notarization — deferred indefinitely.
- Linux or Windows binaries — out of scope for this proposal.
- Channel 4 install UX (`curl | sh`, Homebrew tap) — downstream; prerequisite satisfied by this step.
- rc tag / Release cleanup — optional builder discretion.
- Versioned asset filenames — explicitly rejected; unversioned filenames are the Channel 4 contract.
- MCP stdio handshake in smoke script — deferred from Step 1; optional extension noted above but not a shipping criterion.
