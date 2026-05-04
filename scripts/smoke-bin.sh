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

# 2. version
echo "--- version"
"$BIN" version
echo "PASS: version"

echo "=== All smoke checks passed ==="
