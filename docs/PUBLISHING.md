# Publishing @procrastivity/duo to npm

This guide walks through a one-time manual bootstrap publish of `@procrastivity/duo@0.1.0` to the npm registry, followed by trusted-publisher configuration for automated CI releases via GitHub Actions OIDC.

**Why two publish methods?** The first publish (manual, local) establishes the package name on the registry. Subsequent releases use GitHub Actions OIDC trusted publishing, which removes the need for long-lived credentials and reduces attack surface. The trusted-publisher binding happens on npmjs.com after the first package exists.

---

## Step 1: Verify Organization Access and Prerequisites

Confirm that you own or have owner access to the `@procrastivity` npm organization, and that your local environment is ready for publishing.

**Org setup**:
- If the `@procrastivity` organization does not yet exist, create it:
  ```bash
  npm org create @procrastivity
  ```
  (Requires 2FA on your account; you will be prompted for an OTP code.)

- If the organization already exists, verify your owner access:
  ```bash
  npm org ls members @procrastivity
  ```
  Your username should appear in the members list.

**Other prerequisites**:
1. **2FA enabled** on your npm account (verify at npmjs.com account settings).
2. **Node.js ≥ 22.0.0** installed locally:
   ```bash
   node --version
   ```
   Should output `v22.x.x` or higher.
3. **Git working directory is clean**:
   ```bash
   git status
   ```
   Should show "nothing to commit, working tree clean".
4. **Build and tests pass**:
   ```bash
   npm run build && npm test
   ```
   Both commands must exit 0.
5. **package.json version is `0.1.0`**:
   ```bash
   node -p "require('./package.json').version"
   ```

If any prerequisite is unmet, address it before proceeding.

---

## Step 2: Authenticate with npm Locally via Web Browser

Log in to npm using your credentials. This creates a local authentication token in `~/.npmrc` that npm will use for publishing.

```bash
npm login
```

You will be prompted for:
- **Username**: your npm account username
- **Password**: your npm account password
- **One-time password (OTP)**: a 6-digit code from your 2FA device or app

After successful login, verify authentication:

```bash
npm whoami
```

This should print your npm username, confirming the token was saved.

**Token scope and location**: The token created by `npm login` is scoped to your user account and the organizations you own or are a member of. It is stored in `~/.npmrc` (your home directory). This token grants full publish rights for `@procrastivity/*` packages. The token will remain valid indefinitely unless you manually revoke it.

---

## Step 3: Verify the Package Name is Unclaimed

Before publishing, confirm that `@procrastivity/duo` does not already exist on the npm registry:

```bash
npm view @procrastivity/duo
```

This should return an HTTP 404 error (package not found). If the package exists unexpectedly, stop here and escalate to the orchestrator before proceeding.

---

## Step 4: Configure Trusted Publishers on npmjs.com (Documentation Upfront)

This step must be done *after* the first publish (Step 5), but it is documented here so you understand the complete flow before publishing.

After publishing v0.1.0, you will configure GitHub Actions OIDC as a trusted publisher on npmjs.com. This allows `release.yml` to publish future versions without storing an `NPM_TOKEN` secret.

**You will need to**:
1. Log in to https://npmjs.com with your account (2FA required).
2. Navigate to the `@procrastivity/duo` package page.
3. Go to the **Settings** tab and find the **Trusted Publishers** or **Publishing** section (UI wording varies; see https://docs.npmjs.com/creating-and-viewing-access-tokens for exact paths).
4. Add a trusted publisher entry with:
   - **Repository**: `procrastivity/duo` (GitHub `<owner>/<repo>`)
   - **Workflow filename**: `.github/workflows/release.yml`
5. Save the entry.

This step is blocked until the package exists on the registry (i.e., after Step 5 below).

---

## Step 5: First Publish — v0.1.0 (Manual, Local)

Now publish the package to the npm registry for the first time.

**Pre-publish verification**:
1. Ensure main branch is clean: `git status` → "nothing to commit, working tree clean".
2. Verify `package.json` version: `node -p "require('./package.json').version"` → `0.1.0`.
3. Install dependencies: `npm ci`.
4. Run tests: `npm test` → all tests pass.
5. Build: `npm run build` → `dist/` created with no errors.
6. Verify tarball contents:
   ```bash
   npm pack --dry-run
   ```
   Should list: `dist/`, `README.md`, `LICENSE`, `package.json`.
   Should **NOT** list: `src/`, `node_modules/`, `notes/`, test files.

**Perform the publish**:
```bash
npm publish --access public
```

**What happens**:
- npm packages the files specified in `package.json` `files` list into a tarball.
- The tarball is uploaded to the registry as `@procrastivity/duo@0.1.0`.
- Output should include: `npm notice 📦  @procrastivity/duo@0.1.0 published`.

**Note on `--provenance`**: The workplan specifies `--provenance` as part of the release flow, but on this local publish it is silently ignored (provenance only works under GitHub Actions with OIDC). This is expected and correct.

**Verify the publish**:
```bash
npm view @procrastivity/duo@0.1.0
```

Within 30 seconds to 2 minutes, this should return package metadata. Registry propagation may take up to 2 minutes; if 404 persists, wait and retry.

---

## Step 6: Configure Trusted Publishers on npmjs.com (Actually Do It Now)

Now that `@procrastivity/duo@0.1.0` exists on the registry, complete the trusted-publisher setup outlined in Step 4.

**Web UI steps**:
1. **Log in to npmjs.com** with 2FA.
2. **Navigate to the package**: Click the `@procrastivity/duo` package name or search for it.
3. **Go to Settings**: Click the **Settings** tab.
4. **Find Trusted Publishers**: Look for **Trusted Publishers**, **Publishing**, or **Trusted Automation** section (exact wording varies by npmjs.com version).
5. **Add GitHub Actions**:
   - Click **Add trusted publisher** (or similar).
   - Select **GitHub Actions** as the publisher type.
   - Enter:
     - **GitHub Organization**: `procrastivity` (or your personal GitHub username if the repo is under a personal account).
     - **GitHub Repository**: `duo`.
     - **Workflow filename**: `.github/workflows/release.yml`.
   - Click **Save** or **Add**.

**Verify the configuration** (optional):
```bash
npm view @procrastivity/duo --json | jq '.publish_config'
```

Output may be empty (that is correct for a public package) or may show access levels. The important thing is that the config exists and is accessible to the workflow.

**Result**: The `release.yml` workflow can now publish to npm using OIDC without a stored `NPM_TOKEN` secret. GitHub's OIDC token, verified by the trusted-publisher config, serves as the identity.

---

## Step 7: Tag v0.1.0 and Trigger CI Release Workflow

Create a git tag for v0.1.0 and push it to GitHub. This will trigger the `release.yml` workflow.

**Create and push the tag**:
```bash
git tag v0.1.0
git push origin v0.1.0
```

**Watch GitHub Actions**:
1. Go to https://github.com/procrastivity/duo/actions (adjust the URL for your GitHub username if necessary).
2. You should see a `release.yml` workflow run triggered by the `v0.1.0` tag within seconds.
3. Click into the workflow and watch it execute:
   - Checkout source code.
   - Set up Node.js.
   - `npm ci` installs dependencies.
   - `npm test` runs and passes.
   - `npm run build` runs and passes.
   - **Version-equality check**: compares `package.json` version to the tag name (`v0.1.0` → `0.1.0`). Both must match.
   - `npm publish --provenance --access public` runs.

**Expected outcome**: The workflow completes successfully. The publish is a re-publish of the same version (npm rejects duplicate version numbers), but this time it includes a `--provenance` attestation signed by GitHub's OIDC token.

**If the workflow fails**: Review the logs in GitHub Actions. Common issues:
- Trusted-publisher config not saved on npmjs.com (Step 6).
- `release.yml` missing `permissions: { id-token: "write" }`.
- `release.yml` missing `registry-url: 'https://registry.npmjs.org'` in `setup-node` step.

---

## Step 8: Final Validation — Smoke Test and Registry Verification

Wait 1–2 minutes for the registry to propagate, then verify that the package works end-to-end.

**Smoke test in a fresh directory**:
```bash
mkdir -p /tmp/duo-smoke-test
cd /tmp/duo-smoke-test
npm init -y
npx @procrastivity/duo
```

**Expected behavior**:
- `npx` downloads and caches `@procrastivity/duo`.
- The `duo` bin entrypoint (from `dist/index.js`) is invoked.
- With no `duo.config.yaml`, the process exits non-zero with a structured error message (e.g., "config not found" in JSON or plain text).
- The exit code is **not** 127 (file not found) or a syntax/parse error; it is a normal application error, which is correct.

**Verify both versions on the registry** (optional but recommended):
```bash
npm view @procrastivity/duo --json | jq '.versions | .[-2:]'
```

You should see `0.1.0` (from Step 5, manual) and potentially the same `0.1.0` re-published from CI (Step 7). Both should be listed on https://www.npmjs.com/package/@procrastivity/duo/v/0.1.0.

**Check for provenance** (advanced):
```bash
npm view @procrastivity/duo@0.1.0 --json | jq '.dist'
```

On the manual publish (Step 5), provenance is absent (expected). On the CI re-publish (Step 7), provenance attestation metadata should appear (if configured correctly).

**Clean up**:
```bash
cd ~
rm -rf /tmp/duo-smoke-test
```

---

## Troubleshooting

### `npm login` fails with "403 Forbidden"

- Verify your username and password are correct.
- Ensure 2FA is enabled and you are entering the current OTP code, not an expired one.
- Check that your npm account has access to the `@procrastivity` organization.

### `npm view @procrastivity/duo` shows "Could not find a package.json file in the current directory"

- This error is misleading; it usually means the package doesn't exist on the registry (which is what you want). Ignore it for the collision check.

### `npm publish` fails with "403 Forbidden"

- Verify you ran `npm login` and the token is valid.
- Confirm the `package.json` `name` is exactly `@procrastivity/duo`.
- Check that you have owner permissions on the `@procrastivity` npm organization.

### `release.yml` workflow fails after trusted-publisher setup

- Verify the workflow path in the trusted-publisher config matches `.github/workflows/release.yml` exactly.
- Check that the GitHub organization and repository names in the config match your repo.
- Ensure `release.yml` has `permissions: { id-token: "write", contents: "read" }` (required for OIDC).
- Review the workflow logs on GitHub Actions for the specific error.

### `npm publish` on CI (step 7) reports "You must be logged in to publish"

- Verify the trusted-publisher config on npmjs.com is correct (Step 6).
- Confirm `release.yml` runs `actions/setup-node@v4` with `registry-url: 'https://registry.npmjs.org'` before `npm publish`.
- Check that the workflow has permission to request an OIDC token: `permissions: { id-token: "write" }`.

---

## Summary

After completing all 8 steps:

1. ✅ `@procrastivity/duo@0.1.0` is live on npm.
2. ✅ GitHub Actions OIDC trusted publishing is configured.
3. ✅ `release.yml` can automatically publish future releases without a stored `NPM_TOKEN` secret.
4. ✅ Any future `vX.Y.Z` tag pushed to the repo will trigger `release.yml`, which will:
   - Run typecheck, tests, and build.
   - Compare `package.json` version to the tag version.
   - Publish with `--provenance --access public`.

From this point forward, the bootstrap is complete. To release a new version, simply:
1. Bump `package.json` version (e.g., to `0.1.1`).
2. Commit the change.
3. Tag the commit: `git tag v0.1.1`.
4. Push the tag: `git push origin v0.1.1`.
5. CI takes over; the package is published automatically.
