# Step 5 Workplan — Documentation, Packaging, and Adoption

**Status**: planned (decision-blockers pending human sign-off)
**Roadmap**: `notes/roadmap/roadmap-1.md` (Step 5, lines 103–122)
**Intake**: `notes/proposals/solo-orchestrator-companion-intake.md`
**Source coverage**: roadmap-1 Step 5 shipping criteria 1–7

---

## Scope

- **Goal**: Make the Solo Orchestrator Companion installable and usable by external MCP clients without requiring a manual `tsc` build. Concretely: ship the existing TypeScript MCP server as a published npm package with a `bin` entrypoint that runs under `npx`; write a single, concise README that covers installation, configuration, MCP client wiring, the three tools with tier-label examples, the standalone-vs-Hypomnema framing, and the supported Node range; and stand up a CI release pipeline that publishes a versioned package from a release tag.
- **Out of scope**: standalone single-file binaries (`bun build --compile`), Homebrew, Docker images, or any non-npm channel — explicitly deferred per roadmap; in-package telemetry or update checks; bundling/minification (we ship the dependency graph honestly via `tsc` output); a separate companion website or hosted docs (README on the repo is sufficient for v0); generating an SBOM or signing artifacts beyond npm provenance; auto-bumping `version` in CI (the tag is the source of truth and the workflow asserts equality).
- **Step 4 docs follow-on**: the resolver's `matched_tokens` contract changed from `string[]` to `{ token, source }[]` in Step 4. The README must reflect that shape in any example output. Pre-1.0 — no migration shim needed.

---

## Decision Blockers (require human sign-off before build)

These are the decisions that are not derivable from the codebase and that would be expensive to revisit after publish (a name on npm is sticky; a tag pattern lives in CI; a Node minimum is a compatibility promise). Each one carries a **proposed default** with rationale; the coordinator should surface these to the human resolver before kicking off Task 1.

### A. npm package name

- **LOCKED**: `@procrastivity/duo` (scoped public package under the user's existing npm org)
- **Collision check**: before Task 1, run `npm view @procrastivity/duo`. If unexpectedly taken, surface to orchestrator immediately before publishing.
- **Rationale**: anchors on the user's established org scope and keeps the package name short and memorable (`duo`, matching the project name). Simplicity over extensibility in v0.
- **Status**: human-signed; proceed to build.

### B. npm scope ownership and visibility

- **LOCKED**: public visibility, published under the user's existing `@procrastivity` npm organization. 2FA + OIDC trusted publishing enabled on the org admin account.
- **CI credential strategy**: GitHub Actions OIDC trusted publishing (no long-lived `NPM_TOKEN` secret; identity comes from the GitHub workflow).
- **Bootstrap publish**: user will perform the first publish manually from local (`npm publish --access public --provenance` — note: provenance flag only works under GitHub Actions OIDC, so the first local publish will work but without provenance; that is expected and correct). User will then configure the trusted-publisher binding on npmjs.com pointing at the workflow. Subsequent CI publishes carry the provenance attestation.
- **Status**: human-signed; proceed to build.

### C. CI release pipeline shape

- **LOCKED**: GitHub Actions, two workflows (`ci.yml`, `release.yml`). Tag pattern `v[0-9]+.[0-9]+.[0-9]+` (and prerelease `v[0-9]+.[0-9]+.[0-9]+-*`). OIDC trusted publishing with `--provenance`. Version-equality gate enforced.
- **Workflow shapes** (see Task 4 for full YAML):
  - `ci.yml` — push to main + PR: `npm ci` → `npm run typecheck` → `npm test` → `npm run build`.
  - `release.yml` — tag-triggered: setup OIDC → `npm ci` → `npm test` → `npm run build` → **version-equality check** → `npm publish --provenance --access public`.
- **Status**: human-signed; proceed to build.

### D. Node version range

- **LOCKED**: `engines.node = ">=22.0.0"`. CI matrix: **[22, 24, 25, 26]** (user's explicit choice to test across newer LTS/current versions, skipping Node 20).
- **Rationale per user**: align with the user's deployment environment and testing strategy. Node 22 is Active LTS as of planning time; 24, 25, 26 cover the active maintenance/current band.
- **README Requirements section**: state "Node ≥ 22.0.0" (matching the `engines.node` floor).
- **Status**: human-signed; proceed to build.

### E. Bin name

- **LOCKED**: `duo` (not `duo-companion`). `npx duo` is the user-facing invocation.
- **Awareness check**: `npm view duo` will be run pre-Task-1 to flag if an unscoped `duo` package already exists on npm (informational only; the package name itself is `@procrastivity/duo`, so this is just for situational awareness).
- **Rationale per user**: short, memorable, matches the project name. The full qualified name on npm is `@procrastivity/duo`; the bin is just `duo`.
- **Status**: human-signed; proceed to build.

### F. README outline

- **Proposal** — single `README.md` at repo root, sections in this order:
  1. **Overview** (one paragraph: what it is; explicit standalone / not-Hypomnema language; one-line "Solo `spawn_process` still works directly, prefer the companion in playbooks" note)
  2. **Requirements** (Node version range; running Solo MCP server reachable via stdio command-spawn)
  3. **Installation** (npm install local, `npx` ad-hoc, global install)
  4. **MCP client setup** (one config snippet per supported client — start with Claude Desktop and Solo's own MCP client config; show the published package name in `command`/`args`)
  5. **Configuration** (`duo.config.yaml` fields, env vars `DUO_CONFIG`, `DUO_POLICY`, `SOLO_PROCESS_ID`, `SOLO_PROJECT_ID`)
  6. **Tools** — three subsections, each with input schema sketch and a worked example using a tier label only:
      - `list_agent_tiers` — example output showing `small`/`medium`/`large` availability
      - `resolve_agent_tool` — example invocation `{ "tier": "medium" }` and example response including `selected.token_source`, `matched_tokens` as `{ token, source }[]`
      - `spawn_agent` — example invocation `{ "tier": "large", "name": "step-05-coordinator" }` and example successful response
      All three examples MUST avoid passing `agent_tool_id`.
  7. **Tier policy overrides** (brief — point at `duo.policy.yaml` shape from Step 4; link to a fuller `docs/` page if one exists, otherwise inline the smallest useful example)
  8. **Logging** (three event types from Step 4 with one sample stderr JSON line each)
  9. **Direct `spawn_process` (when to skip the companion)** — short paragraph: "Solo's `spawn_process` remains available for direct use. Prefer the companion when you want tier-based selection, alternative listing, override-aware diagnostics, or structured resolution logs. Reach for direct `spawn_process` only for one-off explicit `agent_tool_id` overrides where tiers don't apply."
  10. **Releases & versioning** (semver; tag → publish flow; how to consume a specific version)
  11. **License**
- **Alternatives considered**:
  - Split into `README.md` (overview) + `docs/` pages: more structure than a v0 needs and less discoverable on the npm page.
  - Auto-generate the tools section from JSON schema: tempting but introduces a build step for docs that we don't need. Hand-written examples are clearer and easier to keep accurate.
- **Rationale**: a single page that the npm registry renders inline is the highest-leverage doc surface; the section ordering follows what a new user does in time order (decide if it's for them → install → wire it up → use it).
- **What human input unblocks**: any specific MCP clients the README should call out beyond Claude Desktop (e.g., a Solo-internal client config file format); whether to also include a `CHANGELOG.md` in v0 (default: defer to Step 6+).

### G. Companion-vs-Hypomnema framing language

- **LOCKED — REFRAMED**: Hypomnema is **not mentioned anywhere** in the README or project docs. The framing is:
  - Duo is a **Solo companion** (not a Hypomnema companion or a result of Hypomnema work).
  - No lineage references ("extracted from", "developed during", "complementary to Hypomnema", etc.).
  - New copy block (replaces the proposal in the workplan):

  > **Duo** is a standalone MCP server that surfaces a tier-based capability layer over Solo's process primitives. Any MCP client that wants to spawn Solo-managed agent processes by capability tier (`small` / `medium` / `large`) — instead of by hard-coded `agent_tool_id` — can install and run Duo directly.
  >
  > Solo's `spawn_process` tool remains directly available. Use Duo when you want tier-based selection, alternative listing, override-aware diagnostics, and structured resolution logs. Reach for direct `spawn_process` only for explicit one-off tooling overrides that don't fit the tier model.

- **Package `description` field** (from the workplan Task 1): short version of the above, e.g., "MCP server for tier-based Solo agent selection."
- **Researcher responsibility**: update this workplan section with the new framing language and ensure Task 3 (README) uses only this new framing. Confirm no Hypomnema mentions appear anywhere in the final deliverables.
- **Status**: human-signed (reframed); researcher to update workplan and proceed to build.

### H. Build pipeline (TypeScript → JavaScript, sourcemaps, types shipping)

- **Proposal**:
  - Keep `tsc` as the only build step (no bundler). Honest dependency graph; matches existing `package.json` `build` script.
  - Extend `tsconfig.json` with `"declaration": true`, `"declarationMap": true`, `"sourceMap": true`. Ship `.js`, `.d.ts`, `.js.map`, `.d.ts.map` from `dist/`.
  - Prepend `#!/usr/bin/env node` to `src/index.ts` so the compiled `dist/index.js` is directly executable. (`tsc` preserves leading shebangs.) npm sets the executable bit on bin targets at install time, so we do not need `chmod +x` in postbuild.
  - `package.json` adds `"prepublishOnly": "npm run build"` to guarantee `dist/` is regenerated immediately before publish.
  - `package.json` adds `"files": ["dist", "README.md", "LICENSE"]` so the published tarball is precisely what we want and nothing else (no `src/`, no fixtures, no tests, no `notes/`).
  - Drop `"private": true`.
- **Alternatives considered**:
  - Bundle with `tsup` or `esbuild` into a single file: smaller install, but obscures the dependency graph from `npm audit`/Dependabot and makes runtime errors harder to debug. Not worth the complexity in v0.
  - Skip `.d.ts` shipping: technically optional for an MCP server consumer (they call it as a binary, not a library). However, a published `.d.ts` lets future consumers import internals if they need to (e.g., to embed the resolver) and costs us nothing.
  - Skip sourcemaps: small disk footprint win, big debuggability loss.
- **Rationale**: minimal change to the existing build, ships everything a debugging or extending consumer might want, no new tooling to maintain.
- **What human input unblocks**: any pushback on shipping `.d.ts` (default: ship them); any preference on bundling.

---

## Tasks

### Task 1 — Package metadata for npm publication (`package.json`, `LICENSE`)

Make the package publishable. This task assumes blockers A, B, D, E, G, and H are resolved.

**Files**: `package.json`, new `LICENSE` file at repo root if one is not already present.

**Edits to `package.json`**:

```jsonc
{
  "name": "@duo-mcp/companion",                  // from Blocker A
  "version": "0.1.0",                            // unchanged; first published version
  "description": "Standalone MCP server that augments Solo's spawn primitives with tier-based agent selection ...", // from Blocker G, trimmed
  "license": "MIT",                              // assumes MIT; confirm with human
  "author": "...",                               // confirm with human
  "homepage": "https://github.com/<owner>/duo#readme",
  "repository": { "type": "git", "url": "git+https://github.com/<owner>/duo.git" },
  "bugs": { "url": "https://github.com/<owner>/duo/issues" },
  "keywords": ["mcp", "solo", "agent", "orchestrator", "model-context-protocol"],
  "type": "module",
  "main": "./dist/index.js",
  "types": "./dist/index.d.ts",
  "bin": { "duo-companion": "./dist/index.js" }, // from Blocker E
  "files": ["dist", "README.md", "LICENSE"],
  "engines": { "node": ">=20.0.0" },             // from Blocker D
  "scripts": {
    "build": "tsc -p tsconfig.json",
    "test": "vitest run",
    "typecheck": "tsc --noEmit",
    "prepublishOnly": "npm run build"
  },
  "dependencies": { /* unchanged */ },
  "devDependencies": { /* unchanged */ }
}
```

The `"private": true` flag is removed.

**Test strategy**: this task ships no runtime code; verification is artifact-shape:
1. `npm pack --dry-run` lists exactly `dist/**`, `README.md`, `LICENSE`, `package.json`. No `src/`, no `node_modules`, no `notes/`, no `*.test.ts`, no `__fixtures__`.
2. `npm publish --dry-run --access public` reports a publishable package and emits no warnings about missing required metadata.
3. `npm view <published-name>` (run before this task lands) returns 404 (collision check from Blocker A).

**Acceptance**:
- [ ] `package.json` matches the shape above with all blocker-resolved values populated.
- [ ] `LICENSE` exists at repo root with the chosen license text.
- [ ] `npm pack --dry-run` output contains `dist/index.js`, `dist/index.d.ts`, `dist/*.js.map`, `README.md`, `LICENSE`, and excludes `src/` and `node_modules/`.
- [ ] `npm publish --dry-run --access public` succeeds and prints no critical warnings.

---

### Task 2 — Build pipeline produces a runnable, typed distribution (`tsconfig.json`, `src/index.ts`)

Make the compiled `dist/` a thing that `node` (and therefore `npx`) can execute directly, and that downstream TypeScript consumers can read types from.

**Files**: `tsconfig.json`, `src/index.ts` (one-line shebang prepend).

**Edits to `tsconfig.json`** — add three options:

```jsonc
{
  "compilerOptions": {
    /* existing */
    "declaration": true,
    "declarationMap": true,
    "sourceMap": true
  }
}
```

**Edit to `src/index.ts`** — prepend exactly:

```ts
#!/usr/bin/env node
```

This must be the very first byte of the file (no preceding blank line) so `tsc` preserves it. The compiled `dist/index.js` retains the shebang verbatim; npm handles the executable bit at install time per the `bin` declaration.

**Test strategy**:
1. `npm run build` exits 0.
2. `head -1 dist/index.js` prints exactly `#!/usr/bin/env node`.
3. `dist/` contains `index.d.ts`, `index.d.ts.map`, `index.js.map` alongside `index.js`. Other `.ts` modules in `src/` produce parallel `.d.ts` and `.js.map`.
4. `node dist/index.js` runs (and exits non-zero with a config error message — that's the expected behavior with no `duo.config.yaml` present; we are verifying the entrypoint resolves, not that it succeeds).
5. `npm test` (vitest) remains green; sourcemaps must not perturb test output.

**Acceptance**:
- [ ] `dist/index.js` starts with the shebang.
- [ ] `dist/` contains parallel `.d.ts`, `.d.ts.map`, `.js.map` for every `.ts` source under `src/` (excluding tests; vitest does not enter the build).
- [ ] `node dist/index.js` exits non-zero with a structured config error, not a parse/syntax error.
- [ ] `npm test` green.

---

### Task 3 — README covering all seven shipping criteria (`README.md`)

This task is the documentation centerpiece. It satisfies criteria 1, 2, 3, 4, and 7 directly, and references the artifacts produced by Tasks 1, 2, and 4 (criteria 5 and 6).

**Files**: new `README.md` at repo root. Optional pointer: `docs/policy.md` (or extend an existing one) for fuller policy override docs if the README's policy section runs long.

**Section-by-section content checklist** (mirrors Blocker F outline):

1. **Title + one-line tagline + Overview** — must include the standalone-not-Hypomnema language (Blocker G block, verbatim or near-verbatim). Must include the "Solo `spawn_process` remains available; playbooks should prefer the companion" sentence. **Covers criteria 2 and 3.**
2. **Requirements** — Node version range exactly as stated in `package.json` engines; "a reachable Solo MCP server (stdio command-spawn)". **Covers criterion 7.**
3. **Installation** — three forms:
    - `npx <published-name>` (preferred; show the exact name)
    - `npm install -g <published-name>`
    - `npm install <published-name>` for embedding
   **Covers criterion 5.**
4. **MCP client setup** — at least one full JSON config snippet showing how to register the companion as an MCP server, using the published name. Example shape (subject to chosen blockers):
    ```json
    {
      "mcpServers": {
        "duo": {
          "command": "npx",
          "args": ["-y", "@duo-mcp/companion"],
          "env": { "DUO_CONFIG": "./duo.config.yaml" }
        }
      }
    }
    ```
   **Covers criterion 4 (MCP client setup).**
5. **Configuration** — show a minimal `duo.config.yaml`; list all env vars (`DUO_CONFIG`, `DUO_POLICY`, `SOLO_PROCESS_ID`, `SOLO_PROJECT_ID`); link to the policy section for overrides. **Covers criterion 4 (configuration).**
6. **Tools** — three subsections, each with one tier-label-only example:
    - `list_agent_tiers` — input `{}`; example response abbreviated to one tier per `small`/`medium`/`large` showing `selected.tool_id` and `alternatives`.
    - `resolve_agent_tool` — input `{ "tier": "medium" }`; response shows `selected` (with `agent_tool_id`, `tool_name`, `token_source`, `matched_tokens` as `{ token, source }[]` reflecting Step 4's contract change), `classification_source`, `alternatives`, and `diagnostics` (with `strategy`, `override_token_count`, `preference_applied`, `candidates_considered`).
    - `spawn_agent` — input `{ "tier": "large", "name": "step-05-coordinator" }`; response shows `process_id`, `name`, `tier`, `tool` summary.
   **No example may pass `agent_tool_id`.** **Covers criterion 1.**
7. **Tier policy overrides** — minimum viable example: a `duo.policy.yaml` snippet with one `command_tokens.large.tokens = ["pro"]` extend entry and one `selection.preference` entry. Explain the `extend` vs `replace` modes in two sentences each. Defer the deeper schema reference to a `docs/policy.md` page or a code comment in `src/types/policy.ts`.
8. **Logging** — three sample stderr JSON lines, one per event (`resolution.success`, `resolution.failure`, `spawn.success`). Brief note: "Logs go to stderr; stdout is reserved for MCP protocol traffic. Prompts and free-form task content are never logged by design."
9. **Direct `spawn_process` (when to skip the companion)** — second half of Blocker G's block.
10. **Releases & versioning** — semver; release flow ("create a `vX.Y.Z` git tag; CI publishes"); how to install a specific version (`npx @duo-mcp/companion@0.1.0`).
11. **License** — one-line pointer to `LICENSE`.

**Test strategy**: hand-verified, but with a concrete checklist a reviewer can run:
- Open the rendered README in a Markdown previewer; confirm every code fence is syntactically valid (TS, JSON, YAML).
- Grep the file:
  - `grep -c '"tier"' README.md` — must be ≥ 3 (one per tool example, possibly more).
  - `grep -c 'agent_tool_id' README.md` — should match only documentation references *describing* the field (e.g., "instead of `agent_tool_id`"), never an *example invocation*. Easier check: confirm no JSON code fence example shows `"agent_tool_id":` as input.
  - `grep -ci 'hypomnema' README.md` — must be ≥ 1, and the surrounding text must be the standalone disclaimer (visual check).
  - `grep -ci 'spawn_process' README.md` — must be ≥ 1, and the surrounding text must include the "remains available" framing.
  - `grep -E 'Node.*[0-9]+' README.md` — must produce a line stating the supported Node range matching `package.json` engines.

**Acceptance**:
- [ ] Single `README.md` at repo root, sections 1–11 present in the order above.
- [ ] All three tools have a tier-label-only example (criterion 1).
- [ ] Standalone-not-Hypomnema language present and unambiguous (criterion 2).
- [ ] Direct Solo `spawn_process` continued availability stated; companion preference for playbooks stated (criterion 3).
- [ ] Sections cover installation, configuration, MCP client setup, basic usage (criterion 4).
- [ ] MCP client config example uses the *published* package name (criterion 5 doc-side).
- [ ] Node version range stated and matches `package.json` engines (criterion 7).
- [ ] Logging section reflects Step 4's three event types and "stderr only / no prompts" invariant.
- [ ] All grep-able acceptance checks above pass.

---

### Task 4 — CI release pipeline (`.github/workflows/ci.yml`, `.github/workflows/release.yml`)

This task assumes blockers C and D are resolved.

**Files**: `.github/workflows/ci.yml` (new), `.github/workflows/release.yml` (new).

**`ci.yml` shape**:
- Triggers: `push` to `main`; `pull_request` for any branch.
- Single job, Node version matrix `[20, 22]` (per Blocker D).
- Steps: `actions/checkout@v4` → `actions/setup-node@v4` (with cache: npm) → `npm ci` → `npm run typecheck` → `npm test` → `npm run build`.

**`release.yml` shape**:
- Trigger: `push` with `tags: ['v[0-9]+.[0-9]+.[0-9]+', 'v[0-9]+.[0-9]+.[0-9]+-*']`.
- Permissions: `id-token: write`, `contents: read` (OIDC publish).
- Single job, Node version pinned to the highest LTS in the matrix (Node 22 at planning time).
- Steps:
  1. `actions/checkout@v4`.
  2. `actions/setup-node@v4` with `registry-url: 'https://registry.npmjs.org'` and the LTS Node version.
  3. `npm ci`.
  4. `npm test`.
  5. `npm run build`.
  6. **Version-equality check** — bash step:
      ```bash
      pkg_version="$(node -p "require('./package.json').version")"
      tag_version="${GITHUB_REF_NAME#v}"
      if [ "$pkg_version" != "$tag_version" ]; then
        echo "package.json version ($pkg_version) does not match tag ($tag_version)" >&2
        exit 1
      fi
      ```
  7. `npm publish --provenance --access public`. (No `NPM_TOKEN`; OIDC supplies the identity.)
- Optional follow-up step: create a GitHub Release attached to the tag, with auto-generated notes. Default: defer until a CHANGELOG exists; can be added in Step 6+.

**OIDC trusted publishing bootstrap (one-time, manual, by the human)**: the npm package's "Trusted Publishers" settings on npmjs.com must list this GitHub repository and the `release.yml` workflow path. This configuration happens *after* the first manual publish. See Task 5 for the complete manual bootstrap procedure.

**Bootstrap publish note**: the very first publish cannot use OIDC (the package doesn't exist yet, so the trusted-publisher mapping has nowhere to attach). The user will perform this manually using their personal account's credentials (with 2FA). See Task 5 (`docs/PUBLISHING.md`) for step-by-step instructions.

**Test strategy**:
1. Workflow YAML lints clean (`actionlint` if available locally; otherwise GitHub's UI validation).
2. `ci.yml` runs successfully on the PR that introduces it.
3. Verify the version-equality check logic via code inspection (the bash step in release.yml must compare `package.json` version to tag version exactly as shown above).
4. First real test: after Task 5 (docs/PUBLISHING.md), the human performs the manual bootstrap publish and configures trusted publishers. Then tag with `v0.1.1` and let `release.yml` execute; verify it succeeds and the package is re-published with provenance.

**Acceptance**:
- [ ] `ci.yml` runs on push and PR; passes typecheck, test, and build.
- [ ] `release.yml` triggers on `v*.*.*` tags only.
- [ ] `release.yml` includes the version-equality check (bash step comparing package.json to GITHUB_REF_NAME).
- [ ] `release.yml` publishes via OIDC with `--provenance --access public` (after the bootstrap publish has wired trusted publishers).
- [ ] First successful CI publish after trusted-publisher setup produces a tagged version on the registry that `npx @procrastivity/duo@<version>` resolves and runs.

---

### Task 5 — Bootstrap publish instructions (`docs/PUBLISHING.md`)

Human-facing guide for the one-time initial publish and OIDC trusted-publisher setup. This task is documentation-only and is added to Phase 2 per the user's requirement to have manual instructions rather than a script.

**Files**: new `docs/PUBLISHING.md`.

**Content checklist** (in this exact order, with command examples for each step):

1. **Org setup** — confirm or create the `@procrastivity` npm organization.
   - If not yet created: `npm org create @procrastivity` (requires 2FA on the user's account).
   - If already created: `npm org ls members @procrastivity` to verify access.

2. **Authentication** — log into npm locally.
   - `npm login` (prompted for username, password, 2FA).
   - Verify: `npm whoami` should print the authenticated user.

3. **Collision check** — verify the package name is unclaimed.
   - `npm view @procrastivity/duo` should return 404 (package not found).
   - If it exists unexpectedly, stop and escalate to the orchestrator.

4. **Trusted-publisher setup on npmjs.com** (to be done *after* first publish, but document upfront so the user knows the flow).
   - Log into npmjs.com account with 2FA.
   - Navigate to the `@procrastivity/duo` package's "Settings" → "Trusted publishers" or "Publishing" section (UI varies; npmjs.com docs have the exact path).
   - Add a trusted publisher entry:
     - **Repository**: `procrastivity/duo` (GitHub `<owner>/<repo>`)
     - **Workflow filename**: `.github/workflows/release.yml`
     - **Save**.
   - This step is blocked until the package exists on the registry (i.e., after the first `npm publish` below).

5. **First publish — v0.1.0** (manual, from local).
   - Ensure main branch is clean: `git status` should show no uncommitted changes.
   - Ensure `package.json` has `"version": "0.1.0"` (it should, unchanged from scaffold).
   - `npm ci` — install dependencies from lock.
   - `npm test` — run the test suite; all tests must pass.
   - `npm run build` — build the distribution.
   - `npm pack --dry-run` — verify the tarball contents (should list `dist/`, `README.md`, `LICENSE`, `package.json`; should NOT list `src/`, `node_modules/`, `notes/`).
   - `npm publish --access public --provenance` — publish to npm.
     - Note: `--provenance` will be silently ignored on this local publish (provenance only works under GitHub Actions OIDC). That is expected and correct.
     - Verify: `npm view @procrastivity/duo@0.1.0` should return package info within ~30 seconds (may take up to 1–2 minutes for registry propagation).

6. **Configure trusted publishers on npmjs.com** (do this after confirming v0.1.0 is live).
   - Complete step 4 above (add the trusted-publisher entry in the npmjs.com Settings UI).
   - Verify: `npm view @procrastivity/duo --json | jq '.publish_config'` should reflect the public visibility.

7. **Tag v0.1.0 and push** — let CI take over from here.
   - `git tag v0.1.0` (locally).
   - `git push origin v0.1.0` — push the tag to GitHub.
   - Watch GitHub Actions: the `release.yml` should trigger, run through ci stages, and execute `npm publish` again. This second publish will be a no-op (npm rejects duplicate versions) but will include the `--provenance` attestation (because it runs under GitHub Actions OIDC).
   - Verify: `npm view @procrastivity/duo@0.1.0 --json | jq '.dist.integrity'` should show the tarball integrity hash.

8. **Smoke test post-publish**.
   - Wait ~2 minutes for registry propagation.
   - From a clean directory (not the project repo): `mkdir /tmp/duo-smoke && cd /tmp/duo-smoke && npm init -y && npx @procrastivity/duo --help` (or just `node ./node_modules/.bin/duo --help`). 
   - The command should exit non-zero with a "config not found" structured error (not a syntax/parse error). That means the bin resolved and the MCP server code is intact.

**Success criteria**:
- [ ] `npm view @procrastivity/duo@0.1.0` returns valid package info.
- [ ] `npx @procrastivity/duo` resolves and runs (exits non-zero with config error, not parse error).
- [ ] GitHub Actions `release.yml` completed successfully after the tag push.
- [ ] Trusted publishers configured on npmjs.com pointing at the workflow (manual UI step, no automation).

**Acceptance**:
- [ ] `docs/PUBLISHING.md` exists with all 8 steps documented in plain language.
- [ ] Each step includes the exact command(s) to run or the exact UI path to navigate.
- [ ] The document makes it clear that step 4 and 6 (trusted-publisher setup) require the npmjs.com web UI (cannot be scripted).
- [ ] The document emphasizes that the first publish is a one-time, deliberate human action.

---

### Task 6 — Pre-publish smoke verification (`scripts/smoke-pack.sh`)

Lightweight; might be deferred if the human prefers manual smoke. Included for explicitness and to give CI a future home for the same check.

**Files**: `scripts/smoke-pack.sh` (new, executable).

**Behavior**:
1. `npm pack` in repo root — captures the produced tarball name.
2. Create a temp directory; `cd` into it.
3. `npm init -y` and `npm install <path-to-tarball>`.
4. Run `./node_modules/.bin/duo-companion --help` *or* `node ./node_modules/.bin/duo-companion` and assert the process starts (it will exit non-zero on missing config; the assertion is "exit code is not 127 / not a parse/syntax error" — the bin entry resolved).
5. Cleanup.

**Test strategy**: run the script locally before each release; optionally invoke it as a final step in `ci.yml` after `npm run build` to catch packaging regressions early.

**Acceptance**:
- [ ] `scripts/smoke-pack.sh` exits 0 against the current `dist/`.
- [ ] Removing the shebang from `src/index.ts` causes the script to fail (sanity: the smoke test actually exercises the bin path).

---

## Mocks vs. Live Considerations

Step 5 introduces **two real-world touchpoints**:

- **The npm registry** — real publish, real namespace claim. The bootstrap publish (Task 4 note) is the only point of no return. Once `@duo-mcp/companion@0.1.0` exists, the name is committed; subsequent publishes can be `npm unpublish`'d only within 72 hours and only if no dependents exist. Treat the bootstrap publish as a human-confirmed action.
- **GitHub Actions OIDC** — depends on npm's trusted-publisher configuration on npmjs.com and on `id-token: write` permissions in the workflow. This is configured by the human once via the npmjs.com UI; CI cannot self-bootstrap the trust relationship.

All other testing remains local: `npm pack`, `npm publish --dry-run`, `node dist/index.js`, vitest. No live Solo connection is required for any of Step 5's acceptance.

---

## Deferred Decisions Resolved Here

- **Distribution channel → npm only** (per roadmap; restated).
  Single-file binaries and Homebrew remain deferred. Revisit only if (a) non-Node installation friction is reported, or (b) Solo itself ships as a binary, or (c) a downstream MCP client cannot reasonably depend on Node. Source: roadmap-1 Step 5 deferred-decisions list.

- **Package naming → scoped (`@duo-mcp/companion`) public**.
  Scope claims room for future sibling packages without renames. Source: this workplan, Blocker A.

- **CI provider → GitHub Actions; tag pattern `v[MAJOR].[MINOR].[PATCH]` (with optional `-prerelease` suffix)**. Source: this workplan, Blocker C.

- **Publish credential → OIDC trusted publishing; `NPM_TOKEN` only as a one-shot bootstrap, removed immediately**. Source: this workplan, Blocker B/C.

- **Node range → `>=20.0.0`; CI matrix `[20, 22]`; no Node 18, no upper bound**.
  Source: this workplan, Blocker D.

- **Bin name → `duo-companion`**, distinct from the package name to keep the user-facing CLI identity stable across any future package-name evolution. Source: this workplan, Blocker E.

- **Build → unbundled `tsc`, ship `.js`, `.d.ts`, `.js.map`, `.d.ts.map`, plus `README.md` and `LICENSE`; no `src/` in tarball; `prepublishOnly` enforces fresh build**.
  Source: this workplan, Blocker H.

- **CHANGELOG.md and a GitHub Releases auto-notes step → deferred to Step 6+**.
  Reason: a CHANGELOG only earns its keep after the first 2–3 releases; in v0 the README's "Releases & versioning" pointer plus the git tag history are sufficient. Source: this workplan, Task 4 trailing note.

- **Telemetry / update-check / phone-home → out of scope**.
  The companion is a local stdio MCP server; it must not make outbound network calls except via Solo itself. No `update-notifier`, no anonymous usage pings. Source: this workplan, Scope.

- **Documentation surface → single `README.md`** (with optional pointer at `docs/policy.md` for the deeper policy schema).
  No website, no separately deployed docs site in v0. Source: this workplan, Blocker F.

- **Companion-vs-Hypomnema framing → exact copy block fixed in Blocker G**.
  Both halves (standalone disclaimer and direct-`spawn_process` note) live in the README and are paraphrased in the npm `description`. Source: this workplan, Blocker G.

- **`agent_tool_id` in examples → forbidden across all README tool examples**.
  Per criterion 1. The field is mentioned only as a *contrast* (the thing the companion lets you avoid). Source: this workplan, Task 3 acceptance.

- **Logging section in README**.
  Adds a doc surface for Step 4's three event types so external operators can interpret stderr lines without reading source. Not a roadmap criterion explicitly, but cheap to include and reduces support surface. Source: this workplan, Task 3 outline.

---

## Edge Cases Worth Pre-Fixing

These are pitfalls that show up only at publish time or at first-use time. Calling them out lets the builder design around them rather than rediscovering them.

- **Shebang erasure by editor settings**. If `src/index.ts` is opened in an editor that auto-trims trailing newlines or auto-prepends a BOM, the shebang can break. Verify after commit by piping `head -c 20 src/index.ts | xxd` (no BOM bytes; `#!/usr/bin/...` exact prefix).
- **Package size sanity**. `npm pack --dry-run | tail -20` should show a tarball under ~500KB. If it balloons past 1MB, something's wrong (`files` field too permissive, or a `dist/` contains a fixture mistakenly built into output).
- **`npx` cold-cache install latency**. First-time `npx @duo-mcp/companion` cold-installs everything in the dependency graph (`@modelcontextprotocol/sdk`, `pino`, `yaml`, `zod`, `execa`). Document expected install time in the README's Installation section so the user doesn't kill the process thinking it's hung.
- **MCP client config quoting**. JSON snippets in the README must use double-quoted JSON, not JS object literal syntax. A common copy-paste foot-gun.
- **OIDC + `--provenance` requires a public package**. Restricted scope publishing is incompatible with provenance attestation. Already covered by Blocker B's public-visibility decision; restate here so a future "let's restrict it" thread doesn't accidentally break the publish step.
- **Node 20.0.0 vs 20.6.0 native fetch**. We don't depend on native `fetch`; the Solo client uses `execa` over stdio. Safe to set `>=20.0.0` rather than `>=20.6.0`.
- **README example for `matched_tokens`** must use the post-Step-4 `{ token, source }[]` shape, not the pre-Step-4 `string[]` shape (Step 4 workplan, "Watch-for" note). Pre-1.0; no compatibility shim required.
- **Bin entry on Windows**. npm generates `.cmd` shims on Windows for bin entries; the shebang is preserved in the underlying `.js` but invoked through the shim. No code change required, but note that `npx duo-companion` on Windows works via the shim, not the shebang. Safe with our setup.
- **First publish is irreversible after 72h**. Make the bootstrap publish a deliberate, human-confirmed action. The workplan does not script it.

---

## Definition of Done

- [ ] `package.json` is publishable (no `"private": true`; `name`, `version`, `bin`, `files`, `engines`, `license`, `repository`, `description`, `keywords`, `prepublishOnly` script all present and accurate).
- [ ] `LICENSE` file exists at repo root and matches `package.json` `license` field.
- [ ] `npm pack --dry-run` lists only `dist/`, `README.md`, `LICENSE`, `package.json`.
- [ ] `npm publish --dry-run --access public` succeeds with no warnings.
- [ ] `dist/index.js` starts with `#!/usr/bin/env node`; `node dist/index.js` runs and exits with a structured config error (entry resolves).
- [ ] `dist/` contains parallel `.d.ts`, `.js.map`, `.d.ts.map` for every `src/*.ts` build target.
- [ ] `vitest` (Step 4 suite) remains green.
- [ ] `README.md` exists at repo root, covers all eleven sections from Blocker F, and passes every grep-able acceptance check from Task 3.
- [ ] All three MCP tool examples in the README use only `tier` labels (no example invocation passes `agent_tool_id`).
- [ ] README's standalone-not-Hypomnema language and direct-`spawn_process` note are present and verbatim from Blocker G (modulo formatting).
- [ ] Node version range stated in `package.json` `engines`, in CI matrix, and in README Requirements; all three match.
- [ ] `.github/workflows/ci.yml` exists and runs on push and PR; runs typecheck, test, and build.
- [ ] `.github/workflows/release.yml` exists, triggers only on `v*.*.*` tags, performs the version-equality check, and publishes via OIDC with `--provenance` and `--access public`.
- [ ] Bootstrap publish completed (human-driven, one-time); npmjs.com trusted-publisher config wired to this workflow.
- [ ] First CI-driven release tag produces a published version that `npx @procrastivity/duo@<version>` resolves and runs.
- [ ] `docs/PUBLISHING.md` exists with all 8 steps documented and each step includes exact command examples or UI paths.
- [ ] (Optional, recommended) `scripts/smoke-pack.sh` exits 0 against the build artifact.

---

## Suggested Build Batching

| Batch | Tasks | Notes |
|---|---|---|
| **Pre-batch (human)** | Resolve Blockers A–H | Coordinator surfaces blockers; human signs off before any task starts. All 8 blockers now locked. |
| Batch A | Task 1, Task 2 | Packaging + build pipeline. Task 2 is small (one tsconfig edit + one shebang line); Task 1 is metadata-heavy. Both leaf-ish, can be done by one builder sequentially or two in parallel. |
| Batch B | Task 3 | README. Long-form prose; needs all blockers resolved and Tasks 1+2 to be complete (so the README can reference accurate package name, bin name, Node range). Single builder, `medium` tier. |
| Batch C | Task 4 | CI pipelines. Depends on Task 1 (package name in workflow text). Single builder. |
| Batch D | Task 5 + Task 6 | Publishing guide (`docs/PUBLISHING.md`) + smoke verification script. Task 5 is prose (medium-tier comfortable); Task 6 is trivial. Can run parallel. |
| **Post-batch (human)** | Execute Task 5 steps + bootstrap publish | Human follows `docs/PUBLISHING.md` to perform the manual first publish and trusted-publisher setup. CI takes over from the next tag onwards. |

A → B, A → C, and C → D can overlap as noted. B is prose and won't conflict with workflow YAML; C/D are independent until the post-batch bootstrap.

**Risk assessment**:
- Lowest-risk: Tasks 1, 2, 6 (mechanical, locally verifiable).
- Medium-risk: Tasks 3, 5 (prose/documentation; Task 3 is high-visibility but easily correctable in a follow-up patch; Task 5 is one-time human instructions but must be crystal clear).
- Highest-risk: Task 4 + post-batch bootstrap publish (irreversible name claim; OIDC-config error mode is silent until the first publish attempt). Sequence the bootstrap deliberately after Task 5 is complete and human has read it once.

**Builder tier recommendation**: Tasks 1–6 are all comfortable on `medium`. None of them touch resolver/classifier invariants; the highest-cognitive-load pieces are Task 3 (README prose) and Task 5 (publishing guide prose). Task 5 in particular must be crystal clear for a human reader but is not code-heavy.

---

## Decision-Blockers Summary (for coordinator → human) — ALL LOCKED

The following eight items have been resolved by human sign-off. Documented here for build audit.

1. **A. npm package name** — **LOCKED**: `@procrastivity/duo` (scoped public, under the user's existing `@procrastivity` org). Collision check pre-Task-1: `npm view @procrastivity/duo` (should return 404). If taken, surface immediately.
2. **B. npm scope ownership and visibility** — **LOCKED**: public visibility. Published under `@procrastivity` org owned by the user. User has 2FA + OIDC enabled. CI uses OIDC trusted publishing (no `NPM_TOKEN` secret; user configures trusted publishers on npmjs.com after bootstrap publish).
3. **C. CI release pipeline shape** — **LOCKED**: GitHub Actions, two workflows (`ci.yml`, `release.yml`), tag pattern `v[0-9]+.[0-9]+.[0-9]+` and `v[0-9]+.[0-9]+.[0-9]+-*`. OIDC publish with `--provenance --access public`. Version-equality gate enforced.
4. **D. Node version range** — **LOCKED**: `engines.node = ">=22.0.0"`. CI matrix `[22, 24, 25, 26]` (user's explicit choice). README Requirements section states "Node ≥ 22.0.0".
5. **E. Bin name** — **LOCKED**: `duo` (not `duo-companion`). `npx duo` is the user-facing invocation. Awareness check pre-Task-1: `npm view duo` (informational; the package name itself is `@procrastivity/duo`, so registry collision is not a blocker).
6. **F. README outline** — **LOCKED**: 11 sections per Blocker F (original workplan). Single `README.md` at repo root. No additional MCP clients requested beyond Claude Desktop pattern.
7. **G. Companion-vs-Hypomnema framing language** — **LOCKED — REFRAMED**: Hypomnema is NOT mentioned anywhere in the README or project docs. Duo is framed purely as a Solo MCP companion. New copy block replaces the workplan proposal (see Blocker G section above). Researcher must verify no Hypomnema mentions appear in final deliverables.
8. **H. Build pipeline** — **LOCKED**: `tsc` only (no bundler). Ship `.js` + `.d.ts` + `*.js.map` + `*.d.ts.map`. Shebang prepended to `src/index.ts`. Files array configured. `prepublishOnly` script ensures fresh build at publish time. Drop `"private": true`.

All eight blockers locked. Proceed to Batch A.

---

## Source-of-Truth References

- Roadmap: `notes/roadmap/roadmap-1.md` lines 103–122 (Step 5 shipping criteria + deferred decisions)
- Intake: `notes/proposals/solo-orchestrator-companion-intake.md` (companion positioning vs. Hypomnema; original distribution-channel framing)
- Step 4 workplan: `notes/roadmap/archive/step-04-workplan.md` (the `matched_tokens` shape change noted under "Watch-for" — the README must reflect the post-Step-4 shape)
- Step 4 retro (when filed): `notes/project-planning-workflow-notes.md` (any documentation-relevant lessons)
- Existing artifacts the workplan touches:
  - `package.json` — current shape: `private: true`, no `bin`, no `engines`, no `files`, build script via `tsc`
  - `tsconfig.json` — current: ES2022 / NodeNext / `outDir: dist`; needs declaration + sourcemap flags
  - `src/index.ts` — current: top-level CLI bootstrap; needs shebang prepend
  - `src/server.ts` — unchanged by this step (referenced for tool/handler context)
  - `mcp-server-config.json` — example MCP-client config in repo; pattern reference for the README MCP-setup section
- External references the builder may need:
  - npm trusted publishing docs (npmjs.com → "Trusted publishers" UI; configured per package, post-bootstrap)
  - GitHub Actions `setup-node@v4` action (for `registry-url` and `node-version` matrix usage)
  - npm `--provenance` flag (publishes a sigstore attestation alongside the tarball; requires public packages)
