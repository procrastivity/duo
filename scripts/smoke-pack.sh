#!/usr/bin/env bash
set -euo pipefail

# Lightweight smoke test for npm pack output.
# Run locally or in CI to catch packaging issues before publishing.

echo "=== Smoke Test: npm pack ==="

# Bail early if the build hasn't run — npm pack will produce misleading output
# (or fail outright) when files declared in `files:` don't exist on disk.
if [[ ! -f dist/duo.mjs ]]; then
  echo "✗ dist/duo.mjs not found. Run 'npm run build' first."
  exit 1
fi

# Use --json for structured, parseable output. The shape is:
#   [{ files: [{ path, size, mode }, ...], ... }]
pack_json=$(npm pack --dry-run --json)
files=$(node -e '
  const data = JSON.parse(require("fs").readFileSync(0, "utf8"));
  for (const f of data[0].files) console.log(f.path);
' <<<"$pack_json")

check_present() {
  local file="$1"
  if grep -Fxq "$file" <<<"$files"; then
    echo "✓ $file"
  else
    echo "✗ $file missing from tarball"
    exit 1
  fi
}

check_absent() {
  local pattern="$1"
  local label="$2"
  if grep -Eq "$pattern" <<<"$files"; then
    echo "✗ $label leaked into tarball:"
    grep -E "$pattern" <<<"$files" | sed 's/^/    /'
    exit 1
  fi
  echo "✓ $label excluded"
}

# --- Required files ---
check_present "package.json"
check_present "LICENSE"
check_present "README.md"
check_present "dist/duo.mjs"

# --- Files that must not ship ---
check_absent '^src/'               "src/"
check_absent '^scripts/'           "scripts/"
check_absent '^tests?/'            "tests/"
check_absent '^\.github/'          ".github/"
check_absent '\.test\.'            "test files"
check_absent '__fixtures__'        "fixtures"
check_absent 'tsconfig.*\.json$'   "tsconfig"
check_absent 'vitest\.config\.'    "vitest config"
check_absent '\.npmignore$'        ".npmignore"

# --- Validate bin entry in package.json ---
bin_target=$(node -p 'require("./package.json").bin?.duo ?? ""')
if [[ "$bin_target" != "./dist/duo.mjs" ]]; then
  echo "✗ bin.duo should be './dist/duo.mjs', got: '$bin_target'"
  exit 1
fi
echo "✓ bin.duo → $bin_target"

# --- Validate shebang in the entry point ---
if head -1 dist/duo.mjs | grep -q '^#!/usr/bin/env node'; then
  echo "✓ shebang present in dist/duo.mjs"
else
  echo "✗ shebang missing or wrong in dist/duo.mjs:"
  head -1 dist/duo.mjs
  exit 1
fi

echo ""
echo "✅ All smoke tests passed."
