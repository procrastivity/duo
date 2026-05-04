# Introduce release-it for duo

**Status**: draft (handoff)
**Date**: 2026-05-04
**Source input**: conversation comparing release flows in `~/Code/hypomnema` and `~/Code/hypomnema-app` (cargo-release + git-cliff via `contrib/changelog-hook`)

## Summary

Adopt [`release-it`](https://github.com/release-it/release-it) as duo's local release driver, mirroring the shape of cargo-release in the hypomnema repos: a single human-run command that (1) computes the next version, (2) prepends `CHANGELOG.md` via git-cliff, (3) writes the new version to `package.json`, (4) creates a commit and an annotated `vX.Y.Z` tag, and (5) **does not push**. The human pushes the tag when ready; existing GitHub workflows (`.github/workflows/release.yml`, `release-bin.yml`) take over from there.

This is a tooling change only — no behavior change for end users, no change to the published artifact, no change to CI. It replaces the current ad-hoc "edit package.json by hand, tag, push" flow.

## Why this shape

The hypomnema repos use `cargo release <level>` driven by `[package.metadata.release]` in `Cargo.toml`, with a `pre-release-hook` pointing at `contrib/changelog-hook` (a tiny shell script that just calls `git-cliff --unreleased --tag "v${NEW_VERSION}" --prepend CHANGELOG.md`). Push is opt-in (`push = false`).

`release-it` is the closest Node-side analog:

- Config in `.release-it.json` (vs. `[package.metadata.release]`).
- Lifecycle hooks (`hooks["before:bump"]`) play the role of cargo-release's `pre-release-hook`.
- `git.push: false` mirrors cargo-release's `push = false`.
- `npm.publish: false` keeps publishing out of the local flow (CI handles it).

Other options considered:

- **`npm version`** — built-in, zero-dep, has `version` lifecycle hook. Workable, but no plugin ecosystem and the hook contract is awkward (you have to `git add` the changelog from inside the hook before npm's auto-commit). Acceptable fallback if we want to avoid adding a tool.
- **`changesets`** — designed for monorepo changeset accumulation; overkill for a single-package repo.
- **`semantic-release`** — fully automated, no human-in-the-loop. Wrong shape for this project (see "Don't push" requirement).

Recommendation: `release-it`. Keeps the same mental model the user already has from cargo-release.

## Existing duo state to integrate with

- **Single package**: pure Node/TypeScript. Only `package.json` carries a version. No `Cargo.toml`, no `src-tauri/`, no other version sources to sync. (This is simpler than hypomnema-app, which had four version sources.)
- **No `cliff.toml`** in the repo yet. Needs to be added — copy from `~/Code/hypomnema/cliff.toml` and update the `remote_url` macro to `https://github.com/procrastivity/duo`.
- **No `CHANGELOG.md`** yet. First release-it run will create it.
- **No `contrib/` dir** yet. Create it for the changelog hook.
- **Existing tags**: `v0.1.0` … `v0.1.4-rc.0`, `v0.1.4-rc.1`. Pre-release suffix history exists, so release-it config should accept `--preRelease=rc` for future RCs (`pnpm release minor --preRelease=rc`).
- **CI tag triggers** (`.github/workflows/release.yml`, `release-bin.yml`): both fire on `v*` tag push. `release.yml` verifies `package.json` version matches the tag — release-it's default tagName `v${version}` aligns with this.
- **Package manager**: duo uses `npm` (`package-lock.json` present, no `pnpm-lock.yaml`). All commands below use `npm`. (Compare: hypomnema-app uses pnpm.)

## Files to add / change

### 1. `cliff.toml` (new)

Copy from `~/Code/hypomnema/cliff.toml`. Change the `remote_url` macro:

```diff
 {%- macro remote_url() -%}
-  https://github.com/gethmn/hypomnema
+  https://github.com/procrastivity/duo
 {%- endmacro -%}
```

Leave the rest as-is unless duo wants a different changelog grouping.

### 2. `contrib/changelog-hook` (new, executable: `chmod +x`)

```sh
#!/bin/sh
set -eu

# Accept the new version as $1 (release-it passes ${version} via templating).
# Falls back to NEW_VERSION env var so the same script could run under
# cargo-release in another repo if that's ever wanted.
VERSION="${1:-${NEW_VERSION:-}}"
: "${VERSION:?version not provided; pass as arg or set NEW_VERSION}"

git-cliff --unreleased --tag "v${VERSION}" --prepend CHANGELOG.md
git add CHANGELOG.md
```

The trailing `git add CHANGELOG.md` matters: on the very first run `CHANGELOG.md` is untracked, and release-it's commit step (`git add . --update`) only stages already-tracked modifications. Adding it here makes the first run work without special-casing.

### 3. `.release-it.json` (new)

```json
{
  "$schema": "https://unpkg.com/release-it/schema/release-it.json",
  "git": {
    "tagName": "v${version}",
    "commitMessage": "chore(release): v${version}",
    "tagAnnotation": "v${version}",
    "push": false,
    "requireBranch": "main",
    "requireCleanWorkingDir": true
  },
  "npm": {
    "publish": false
  },
  "github": {
    "release": false
  },
  "hooks": {
    "before:bump": "./contrib/changelog-hook ${version}"
  }
}
```

No `@release-it/bumper` plugin needed — duo only has one version source. (Bumper was needed in hypomnema-app to sync four files; not relevant here.)

`tagName: "v${version}"` matches what the existing `.github/workflows/release.yml` expects (it strips a leading `v` and compares to `package.json`).

`requireBranch: "main"` matches cargo-release's `allow-branch = ["main"]` in hypomnema.

### 4. `package.json` (edit)

Add a `release` script and the two devDeps:

```diff
   "scripts": {
     ...
-    "build:bin:darwin-x64": "bun build src/index.ts --compile --target=bun-darwin-x64 --outfile=dist/bin/duo-darwin-x64"
+    "build:bin:darwin-x64": "bun build src/index.ts --compile --target=bun-darwin-x64 --outfile=dist/bin/duo-darwin-x64",
+    "release": "release-it"
   },
   ...
   "devDependencies": {
     "@types/node": "^22.15.21",
     "esbuild": "^0.28.0",
+    "release-it": "^17.10.0",
     "typescript": "^5.8.3",
     "vitest": "^2.1.8"
   },
```

(Pin to `^17` rather than `^20` — version 17 is what was tested in the hypomnema-app session; bump later if desired.)

Then `npm install` to update `package-lock.json`.

### 5. `flake.nix` — add `git-cliff` to dev shell

Verify whether `git-cliff` is already available in the duo dev shell. If not, add it to the package list so the changelog-hook works inside `direnv` / `nix develop`. (hypomnema's flake includes it; duo's may already too — check before adding.)

## Verification

After landing the four files, do a dry run on a clean working tree:

```sh
npm install              # picks up release-it
npx release-it patch --dry-run --ci
```

Expected output should include, in order:

1. `./contrib/changelog-hook 0.1.5` (or whatever the bump target is)
2. `npm version 0.1.5 --no-git-tag-version`
3. `git commit --message chore(release): v0.1.5`
4. `git tag --annotate --message v0.1.5 v0.1.5`
5. **No** `git push`. **No** `npm publish`. **No** GitHub release.

Then a real run, after merging this change and being on `main`:

```sh
npm run release -- patch
# inspect: git log -1, git show v0.1.5, head CHANGELOG.md
git push --follow-tags   # only when ready
```

For a release candidate:

```sh
npm run release -- minor --preRelease=rc
# produces v0.2.0-rc.0
```

## Edge cases / things to flag

- **`requireCleanWorkingDir: true`** means uncommitted `dist/` artifacts or stray files block the release. Either commit/clean before running, or keep `dist/` ignored (it already is).
- **`prepublishOnly` script** runs `npm run build && npm run test`. release-it does not invoke `npm publish` (we set `npm.publish: false`), so `prepublishOnly` won't fire from the release flow. CI publishing is unaffected.
- **First run** creates `CHANGELOG.md`. The hook's `git add CHANGELOG.md` plus release-it's commit step handles this. Verify it lands in the release commit, not as a stray untracked file.
- **Existing pre-release tags** (`v0.1.4-rc.0`, `v0.1.4-rc.1`): git-cliff's `--unreleased --tag v0.1.5` will collect everything since the most recent tag, which may include commits already covered by the rc tags. Manually curate the first `CHANGELOG.md` if that produces noise, or run git-cliff with a narrower `--tag-pattern` once to seed history. Not a blocker for the tooling install — just expect the first changelog to need a quick edit.
- **`git-cliff` availability**: the hook calls `git-cliff` as a bare command. CI doesn't need it (CI doesn't run release-it), but the local dev shell does. See item 5 above.

## Out of scope

- Automating the push step. The whole point of this shape is human-gated push.
- Replacing the CI publish workflow. `release.yml` and `release-bin.yml` keep working as-is; they trigger on tag push, regardless of how the tag was made.
- Multi-package / monorepo support. duo is a single package; revisit if that changes.
- Migrating commit-message conventions. git-cliff already groups by conventional-commits prefixes; whatever convention duo uses today continues to work.

## Done criteria

- `npm run release -- patch --dry-run --ci` on a clean `main` produces the expected sequence above with no errors.
- A real run produces a `vX.Y.Z` tag locally, an updated `CHANGELOG.md` in the release commit, and the working tree returns to clean.
- `git push --follow-tags` triggers the existing CI release workflow successfully (validate once on a real release).
