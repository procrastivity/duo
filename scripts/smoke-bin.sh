#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

BIN="${1:-$REPO_ROOT/dist/bin/duo-darwin-arm64}"

if [[ ! -x "$BIN" ]]; then
  echo "ERROR: binary not found or not executable: $BIN" >&2
  exit 1
fi

# Stage the binary in a hermetic tmpdir with no package.json and no .git
# above it. Running version/git_sha checks from that CWD defeats the runtime
# fallbacks (the package.json walk anchored on `import.meta.url`, and
# `git rev-parse` using CWD), so a passing assertion can only come from the
# build-time `--define __DUO_VERSION__/__DUO_GIT_SHA__` substitutions.
HERMETIC="$(mktemp -d)"
trap 'rm -rf "$HERMETIC"' EXIT
HERMETIC_BIN_NAME="$(basename "$BIN")"
HERMETIC_BIN="$HERMETIC/$HERMETIC_BIN_NAME"
cp "$BIN" "$HERMETIC_BIN"
chmod +x "$HERMETIC_BIN"

echo "=== smoke-bin.sh: testing $BIN (hermetic CWD: $HERMETIC) ==="

# 1. --help
echo "--- --help"
"$HERMETIC_BIN" --help
echo "PASS: --help"

# 2. version — must match package.json. Runs from the hermetic CWD so a
# broken `--define __DUO_VERSION__=...` cannot be masked by the runtime
# package.json walk.
echo "--- version"
expected=$(cd "$REPO_ROOT" && node -p "require('./package.json').version")
actual=$(cd "$HERMETIC" && "./$HERMETIC_BIN_NAME" version --quiet)
actual=${actual%$'\n'}
if [[ "$actual" != "$expected" ]]; then
  echo "FAIL: '$BIN version --quiet' returned '$actual', expected '$expected'" >&2
  exit 1
fi
echo "PASS: version ($actual)"

# 3. git_sha — must match HEAD when smoke-testing inside a git checkout.
# Same hermetic-CWD reasoning as the version check: a missing
# `--define __DUO_GIT_SHA__=...` cannot fall back to the runtime
# `git rev-parse` because there is no git repo at or above the CWD.
echo "--- git_sha"
if expected_sha=$(git -C "$REPO_ROOT" rev-parse --short HEAD 2>/dev/null); then
  actual_sha=$(cd "$HERMETIC" && "./$HERMETIC_BIN_NAME" version | awk '/^git_sha/ {print $2}')
  if [[ "$actual_sha" != "$expected_sha" ]]; then
    echo "FAIL: '$BIN version' reported git_sha '$actual_sha', expected '$expected_sha'" >&2
    exit 1
  fi
  echo "PASS: git_sha ($actual_sha)"
else
  echo "SKIP: git_sha (no git checkout at $REPO_ROOT)"
fi

# 4. MCP stdio handshake. Writes a real config that points at a non-existent
# Solo binary so the connection deterministically fails — `solo_connection_failed`
# is the path under test. Without this, `loadConfig()` would treat a missing
# config as "use defaults" and `resolveTransportCommand()` would auto-detect
# any host-installed Solo, making pass/fail depend on the runner image.
echo "--- mcp stdio handshake"
BIN="$HERMETIC_BIN" node --input-type=module <<'EOF'
import { spawn } from "node:child_process";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

const bin = process.env.BIN;
const configDir = mkdtempSync(join(tmpdir(), "duo-smoke-"));
const configPath = join(configDir, "duo.yaml");
writeFileSync(
  configPath,
  "solo:\n  transport:\n    type: stdio\n    command: /nonexistent\n",
);
const child = spawn(bin, ["mcp"], {
  stdio: ["pipe", "pipe", "pipe"],
  env: { ...process.env, DUO_CONFIG: configPath },
});

const responses = new Map();
let stdout = "";
let stderr = "";
let settled = false;

const finish = (code, message) => {
  if (settled) return;
  settled = true;
  clearTimeout(timer);
  child.kill();
  if (code !== 0) {
    if (stdout.trim()) console.error(`stdout:\n${stdout.trim()}`);
    if (stderr.trim()) console.error(`stderr:\n${stderr.trim()}`);
    console.error(message);
  }
  process.exit(code);
};

const handleLine = (line) => {
  if (!line.trim()) return;
  let message;
  try {
    message = JSON.parse(line);
  } catch (err) {
    finish(1, `MCP probe received invalid JSON: ${err.message}`);
    return;
  }
  if (message.id !== undefined) {
    responses.set(message.id, message);
  }
  if (responses.has(1) && responses.has(2) && responses.has(3)) {
    const init = responses.get(1);
    const tools = responses.get(2);
    const call = responses.get(3);
    if (init.error) finish(1, `initialize failed: ${JSON.stringify(init.error)}`);
    if (tools.error) finish(1, `tools/list failed: ${JSON.stringify(tools.error)}`);
    if (call.error) finish(1, `tools/call returned JSON-RPC error: ${JSON.stringify(call.error)}`);
    const toolNames = tools.result?.tools?.map((tool) => tool.name) ?? [];
    for (const name of ["list_agent_tiers", "resolve_agent_tool", "spawn_agent"]) {
      if (!toolNames.includes(name)) {
        finish(1, `tools/list missing ${name}`);
      }
    }
    if (call.result?.isError !== true) {
      finish(1, "tools/call should return isError when Solo is unreachable");
    }
    const text = call.result.content?.[0]?.text;
    const payload = text ? JSON.parse(text) : {};
    if (payload.code !== "solo_connection_failed") {
      finish(1, `unexpected tools/call error payload: ${text}`);
    }
    finish(0, "PASS: mcp stdio handshake");
  }
};

child.stdout.on("data", (chunk) => {
  stdout += chunk;
  const lines = stdout.split("\n");
  stdout = lines.pop() ?? "";
  for (const line of lines) handleLine(line);
});

child.stderr.on("data", (chunk) => {
  stderr += chunk;
});

child.on("error", (err) => finish(1, `failed to start MCP server: ${err.message}`));
child.on("exit", (code, signal) => {
  if (!settled) finish(1, `MCP server exited before handshake completed: code=${code} signal=${signal}`);
});

const timer = setTimeout(() => finish(1, "MCP handshake timed out"), 5000);

child.stdin.write(JSON.stringify({
  jsonrpc: "2.0",
  id: 1,
  method: "initialize",
  params: {
    protocolVersion: "2024-11-05",
    capabilities: {},
    clientInfo: { name: "smoke-bin", version: "0" },
  },
}) + "\n");
child.stdin.write(JSON.stringify({
  jsonrpc: "2.0",
  method: "notifications/initialized",
}) + "\n");
child.stdin.write(JSON.stringify({
  jsonrpc: "2.0",
  id: 2,
  method: "tools/list",
}) + "\n");
child.stdin.write(JSON.stringify({
  jsonrpc: "2.0",
  id: 3,
  method: "tools/call",
  params: { name: "list_agent_tiers", arguments: {} },
}) + "\n");
EOF
echo "PASS: mcp stdio handshake"

echo "=== All smoke checks passed ==="
