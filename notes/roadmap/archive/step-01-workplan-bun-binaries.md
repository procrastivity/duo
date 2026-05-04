# Step 1 — Build and smoke macOS binaries locally

**Roadmap**: `notes/roadmap/archive/roadmap-4-bun-binaries.md`
**Channel**: Roadmap 4 — Bun-compiled macOS binaries (Channel 2)
**Status**: complete
**Generated**: 2026-05-03

---

## Objectives

1. Add `bun` to the Nix devShell so contributors can build binaries without a global Bun install.
2. Add `build:bin:darwin-arm64` and `build:bin:darwin-x64` scripts to `package.json` using Bun's `--compile` flag.
3. Produce working `dist/bin/duo-darwin-arm64` and `dist/bin/duo-darwin-x64` binaries locally.
4. Verify genuine self-containment: the arm64 binary runs with Node stripped from PATH.
5. Write `scripts/smoke-bin.sh` that exercises the hermetic CLI surface under Bun, catching binary startup and command-dispatch failures before CI is wired.
6. Confirm the smoke script passes against the local-arch binary and document deferred coverage for MCP stdio / cross-arch execution.

---

## Shipping Criteria

- [x] `bun` added to `flake.nix` devShell `buildInputs`
- [x] `package.json` script `build:bin:darwin-arm64` produces `dist/bin/duo-darwin-arm64`
- [x] `package.json` script `build:bin:darwin-x64` produces `dist/bin/duo-darwin-x64`
- [x] `env -i PATH=/usr/bin ./dist/bin/duo-darwin-arm64 --help` succeeds on a Mac with no `node` in PATH
- [x] `scripts/smoke-bin.sh` exists and exercises `--help` and `version` hermetically
- [x] Smoke script passes against the local-arch binary; cross-arch execution deferred to CI
- [ ] MCP stdio handshake deferred; `duo mcp` calls `connectSolo()` before starting stdio server (Backlog #259)

---

## Build Environment Prerequisites

| Prerequisite | How to satisfy |
|---|---|
| macOS machine (Apple Silicon preferred; Intel acceptable) | The local dev machine |
| Nix + `nix develop` | Already in use — `flake.nix` devShell |
| `bun` | Added to devShell in Task 1 of this step; or install globally via `brew install bun` as a bootstrap if nix rebuild is slow |
| `npm` / `node` (for existing build/test scripts) | Already in devShell |
| `dist/bin/` directory | Created by Bun `--compile` on first run; no manual mkdir needed |

> **Nix note**: After editing `flake.nix`, exit and re-enter `nix develop` to pick up `bun` in the shell. On a clean devShell you can verify with `bun --version`.

---

## Task Breakdown

Tasks are sequenced: Tasks 1–2 are parallel (config edits); Task 3 depends on both; Tasks 4–5 are parallel (build + script authoring); Task 6 depends on 4+5; Task 7 is cleanup.

### Task 1 — Add `bun` to `flake.nix` devShell

**File**: `flake.nix`
**Change**: Add `pkgs.bun` to the `buildInputs` list of the `devShell` output.

```nix
# In the devShell buildInputs list, alongside existing entries:
pkgs.bun
```

**Verify**: After `nix develop`, run `bun --version` and confirm it resolves.

**Risk**: Low. `pkgs.bun` is in nixpkgs stable; version pinned by the flake's nixpkgs input.

---

### Task 2 — Add `build:bin:*` scripts to `package.json`

**File**: `package.json`
**Change**: Add two new scripts to the `"scripts"` object. No change to `"bin"`, `"files"`, or the publish flow.

```json
"build:bin:darwin-arm64": "bun build src/index.ts --compile --target=bun-darwin-arm64 --outfile=dist/bin/duo-darwin-arm64",
"build:bin:darwin-x64":   "bun build src/index.ts --compile --target=bun-darwin-x64   --outfile=dist/bin/duo-darwin-x64"
```

**Verify**: `npm run build:bin:darwin-arm64 --dry-run` (or simply confirm the script appears in `npm run`).

**Risk**: Low. Scripts are additive only.

---

### Task 3 — Rebuild devShell

Prerequisite for Tasks 4–5 if using `nix develop`:

```sh
exit        # leave current devShell
nix develop # re-enter to pick up bun
bun --version
```

Alternatively, bootstrap with `brew install bun` for immediate unblocking, and verify the nix path works in a separate shell.

---

### Task 4 — Build the local-arch binary

With `bun` available:

```sh
npm run build:bin:darwin-arm64
ls -lh dist/bin/duo-darwin-arm64
```

Then verify self-containment (strips Node from PATH entirely):

```sh
env -i PATH=/usr/bin ./dist/bin/duo-darwin-arm64 --help
```

Expected: help text printed, exit 0. No `node: command not found` or similar error.

**If on Apple Silicon**: also attempt the x64 build as a cross-compile check. Cross-arch builds via Bun are possible but unverified for this CLI's deps — if the x64 binary fails to produce or crashes on arm64, document the gap and defer cross-arch verification to CI (Step 2 matrix).

```sh
npm run build:bin:darwin-x64
# If on arm64, you can't run the x64 binary natively; skip execution test, verify size only:
ls -lh dist/bin/duo-darwin-x64
```

**Risk**: Medium. The `bun build --compile` path for this CLI's dep set (MCP SDK, execa, pino) is unverified. If the build fails, see the "Risks and Mitigations" section.

---

### Task 5 — Author `scripts/smoke-bin.sh`

**File**: `scripts/smoke-bin.sh` (new file, executable)

The script accepts a single argument: the path to the binary under test. This lets it be called against either arch binary and later reused from CI.

**Smoke coverage** (required by intake §Step 1 shipping criteria):

| Test | Command | Expected |
|---|---|---|
| Help | `$BIN --help` | Exit 0, "Usage:" or similar in output |
| Whoami | `$BIN whoami` | Exit 0, prints identity/config info |
| Version | `$BIN version` | Exit 0, prints a version string |
| MCP stdio handshake | Send `initialize` request over stdin, read response | Server responds with `result.serverInfo` or similar; exit 0 after graceful shutdown |

**MCP stdio handshake depth** (builder discretion per intake §Open Questions): Use a "process-level probe" — start the MCP server subprocess, write a JSON-RPC `initialize` request to its stdin, read one line of stdout, assert it contains `"result"`, send `exit` / close stdin. This is the minimum that exercises `@modelcontextprotocol/sdk`'s stdin/stdout wiring under Bun without requiring a full tool-list round-trip.

**Script skeleton**:

```bash
#!/usr/bin/env bash
set -euo pipefail

BIN="${1:-./dist/bin/duo-darwin-arm64}"

if [[ ! -x "$BIN" ]]; then
  echo "ERROR: binary not found or not executable: $BIN" >&2
  exit 1
fi

echo "=== smoke-bin.sh: testing $BIN ==="

# 1. --help
echo "--- --help"
"$BIN" --help
echo "PASS: --help"

# 2. whoami
echo "--- whoami"
"$BIN" whoami
echo "PASS: whoami"

# 3. version
echo "--- version"
"$BIN" version
echo "PASS: version"

# 4. MCP stdio handshake
echo "--- MCP stdio handshake"
INIT_REQUEST='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"smoke-test","version":"0.0.1"}}}'
RESPONSE=$(echo "$INIT_REQUEST" | "$BIN" mcp 2>/dev/null | head -n1)
if echo "$RESPONSE" | grep -q '"result"'; then
  echo "PASS: MCP stdio handshake"
else
  echo "FAIL: MCP stdio handshake — unexpected response: $RESPONSE" >&2
  exit 1
fi

echo "=== All smoke checks passed ==="
```

> Adjust the `mcp` subcommand name to match the actual command in `src/index.ts` that starts the MCP stdio server. If the server does not have a dedicated subcommand (it's started on `stdio` invocation), revise accordingly.

**Make executable**:
```sh
chmod +x scripts/smoke-bin.sh
```

**Risk**: Low-Medium. The MCP stdio handshake section is the highest-risk piece — `@modelcontextprotocol/sdk`'s stdin/stdout path under Bun may behave differently. If the server hangs waiting for stdin to close, wrap the `echo | BIN` pipeline with a timeout: `timeout 10 bash -c '...'`.

---

### Task 6 — Run the smoke script against the local-arch binary

```sh
bash scripts/smoke-bin.sh ./dist/bin/duo-darwin-arm64
```

If on Apple Silicon and the x64 binary was also produced (cross-arch build succeeded), also run:

```sh
# Note: x64 binary cannot be executed natively on arm64 hardware.
# Skip execution smoke on arm64 host; document that CI matrix covers x64 execution.
```

Document in a code comment in `smoke-bin.sh` or in this workplan if the x64 smoke is deferred to CI.

**If any test fails**: See "Risks and Mitigations" below before escalating to the Node SEA fallback.

---

### Task 7 — Cleanup and commit

- Add `dist/bin/` to `.gitignore` (binary outputs should not be committed).
- Confirm no unintended changes to `package.json` `"bin"` or `"files"` fields.
- Commit: `flake.nix`, `package.json`, `scripts/smoke-bin.sh`, `.gitignore` update.
- Do **not** commit binary artifacts.

---

## Risks and Mitigations

| Risk | Likelihood | Impact | Mitigation |
|---|---|---|---|
| `@modelcontextprotocol/sdk` stdin/stdout breaks under Bun | Medium | High — blocks shipping binary | The smoke script is the gate. If it fails: (1) check Bun issue tracker for known Node-stream compat; (2) try `--smol` flag; (3) try explicit `process.stdin.resume()` workaround; (4) if unresolvable, escalate to Node SEA fallback (out-of-scope but acknowledged). |
| `execa` child-process behavior differs under Bun | Medium | Medium — affects subcommands using `execa` | Extend smoke script to exercise one `execa`-backed subcommand (e.g., `doctor` or `proc`). If it fails, check Bun's `child_process` compat layer in release notes. |
| `pino` synchronous stderr destination breaks under Bun | Low | Low — log output missing but CLI still usable | Captured by smoke script's exit-code checks. Inspect stderr output; if `pino.destination(2)` errors, try `pino({ transport: { target: 'pino/file', options: { destination: 2 } } })` as a Bun workaround. |
| `bun build --compile` cross-arch fails for this dep set | Medium | Low for Step 1 (CI covers it in Step 2) | Document gap; defer x64 execution testing to the CI matrix (Step 2 `macos-13` leg). |
| `pkgs.bun` version in nixpkgs is too old for `--compile` | Low | Medium | Check `bun --version` after devShell rebuild; Bun's `--compile` flag is stable since 1.0. If nixpkgs pin is stale, temporarily bootstrap via `brew install bun` and open a separate task to update the flake's nixpkgs input. |
| macOS Gatekeeper quarantine on the locally-produced binary | N/A for local dev | N/A | For local testing: the binary is produced locally and is not quarantined. Gatekeeper only applies to downloaded binaries — this is a Step 2 / user-docs concern. |

---

## Success Signals / Testing Strategy

Step 1 is done when all of the following are true and reproducible:

1. **`bun --version` works in `nix develop`** — flake edit is correct.
2. **`dist/bin/duo-darwin-arm64` exists and is executable** — Bun compile succeeded.
3. **`env -i PATH=/usr/bin ./dist/bin/duo-darwin-arm64 --help` exits 0** — binary is genuinely self-contained; no Node runtime required.
4. **`bash scripts/smoke-bin.sh ./dist/bin/duo-darwin-arm64` exits 0** — all four smoke tests pass:
   - `--help` prints usage
   - `whoami` prints identity/config
   - `version` prints version string
   - MCP stdio handshake returns a JSON-RPC `result`
5. **No regressions**: existing `npm run build` and `npm test` still pass (the Bun scripts are additive only).

### What Step 1 does NOT prove

- That the x64 binary runs correctly on Intel hardware (deferred to CI matrix in Step 2, unless you have an Intel Mac handy).
- That the binary works after download and quarantine removal (macOS Gatekeeper path — Step 2 release notes scope).
- That CI uploads succeed (Step 2 scope).

---

## Out of Scope for This Step

- `.github/workflows/release-bin.yml` — Step 2
- Tagging a release candidate — Step 2
- `xattr -d com.apple.quarantine` documentation in README / release notes — Step 2
- Codesigning / notarization — deferred indefinitely (revisit on user friction)
- Linux or Windows binaries — out-of-scope for this proposal
- Any change to `package.json` `"bin"`, `"files"`, or the npm publish flow — Channel 1 / not needed here
