#!/bin/sh
# Run a command in the Toolbridge environment without modifying caller's shell
# Usage: ./scripts/run.sh make test
#        ./scripts/run.sh python -m pytest
#
# Automatically uses nix develop if not already in nix shell

set -eu

# Get the directory containing this script
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
export PROJECT_ROOT

# Check if a command exists
check_tool() {
    command -v "$1" >/dev/null 2>&1
}

# If not in nix shell and nix is available, re-exec inside nix develop
if [ -z "${IN_NIX_SHELL:-}" ] && [ -f "$PROJECT_ROOT/flake.nix" ] && check_tool nix; then
    # Re-execute this script inside nix develop
    exec nix develop "$PROJECT_ROOT" --command "$0" "$@"
fi

# Source activation (nix env is now active, or we're on a non-nix system)
. "$SCRIPT_DIR/activate.sh"

# Execute the provided command
exec "$@"
