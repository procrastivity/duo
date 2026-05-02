#!/bin/bash
set -e

# Lightweight smoke test for npm pack output.
# Run locally or in CI to catch packaging issues before publishing.

# Step 1: Run npm pack --dry-run and extract tarball info
echo "=== Smoke Test: npm pack ==="
pack_output=$(npm pack --dry-run 2>&1)

# Step 2: Validate key files are present in tarball
echo "$pack_output" | grep -q "package.json" || { echo "✗ package.json missing"; exit 1; }
echo "✓ package.json"

echo "$pack_output" | grep -q "LICENSE" || { echo "✗ LICENSE missing"; exit 1; }
echo "✓ LICENSE"

# Step 3: Validate dist/ artifacts exist
echo "$pack_output" | grep -q "dist/index.js" || { echo "✗ dist/index.js missing"; exit 1; }
echo "✓ dist/index.js"

echo "$pack_output" | grep -q "dist/index.d.ts" || { echo "✗ dist/index.d.ts missing"; exit 1; }
echo "✓ dist/index.d.ts"

echo "$pack_output" | grep -q "dist/__fixtures__" || { echo "✗ dist/__fixtures__ missing"; exit 1; }
echo "✓ dist/__fixtures__ present"

# Step 4: Validate README is included
echo "$pack_output" | grep -q "README.md" || { echo "✗ README.md missing"; exit 1; }
echo "✓ README.md"

# Step 5: Validate dev files are NOT included
if echo "$pack_output" | grep -E "(src/|vitest.config|\.npmignore|tsconfig)" > /dev/null; then
  echo "✗ Dev files leaked into tarball"
  exit 1
fi
echo "✓ Dev files excluded"

# Step 6: Validate bin entry is in package.json
if grep -q '"bin"' package.json && grep -q '"duo"' package.json; then
  echo "✓ Bin entry 'duo' in package.json"
else
  echo "✗ Bin entry 'duo' missing from package.json"
  exit 1
fi

# Step 7: Validate shebang in dist/index.js (if dist/ exists locally)
if [ -f dist/index.js ]; then
  if head -1 dist/index.js | grep -q "#!/usr/bin/env node"; then
    echo "✓ Shebang present in dist/index.js"
  else
    echo "✗ Shebang missing from dist/index.js"
    exit 1
  fi
fi

echo ""
echo "✅ All smoke tests passed."
exit 0
