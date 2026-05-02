#!/bin/sh
# Full environment activation for Duo
# Usage: . scripts/activate.sh  (from project root)
#    or: PROJECT_ROOT=/path/to/project . scripts/activate.sh
#
# Automatically bootstraps nix environment if:
#   - Not already in a nix shell
#   - nix is available
#   - flake.nix exists

set -eu

# Project root: use override, or assume current directory
DUO_ROOT="${PROJECT_ROOT:-$(pwd)}"
export DUO_ROOT

# Source environment variables
. "$DUO_ROOT/scripts/env.sh"

# Check if a command exists
check_tool() {
    command -v "$1" >/dev/null 2>&1
}

# Bootstrap nix environment if not already active
if [ -z "${IN_NIX_SHELL:-}" ] && [ -f "$DUO_ROOT/flake.nix" ]; then
    if check_tool nix; then
        echo "Bootstrapping nix environment..."
        # nix print-dev-env outputs shell code that sets up the environment
        # This is what direnv uses under the hood
        eval "$(nix print-dev-env "$DUO_ROOT" 2>/dev/null)" || {
            echo "Warning: nix print-dev-env failed, continuing without nix bootstrap" >&2
        }
    fi
fi

# Validate tools (now should be available via nix or system)
check_required_tool() {
    if ! check_tool "$1"; then
        echo "Error: $1 is required but not found in PATH" >&2
        echo "Install via nix (recommended) or your system package manager" >&2
        return 1
    fi
}

check_required_tool uv
check_tool python3.12 || check_required_tool python3

# Create virtual environment if needed
if [ ! -d "$UV_PROJECT_ENVIRONMENT" ]; then
    echo "Creating virtual environment with uv..."
    uv venv "$UV_PROJECT_ENVIRONMENT"
fi

# Activate virtual environment
. "$UV_PROJECT_ENVIRONMENT/bin/activate"

# Install dependencies
if [ -f "$DUO_ROOT/pyproject.toml" ]; then
    echo "Installing dependencies from pyproject.toml with uv..."
    uv pip install -e ".[dev]" 2>/dev/null || uv pip install -e .
fi

# Show banner only in interactive terminals
if [ -t 1 ] && [ -z "${DUO_BANNER_SHOWN:-}" ]; then
    DUO_BANNER_SHOWN=1
    export DUO_BANNER_SHOWN

    echo ""
    echo "Duo Development Environment"
    echo ""
    echo "Python: $(python --version) ($(command -v python))"
    echo "UV: $(uv --version)"
    echo "Virtual environment: $VIRTUAL_ENV"
    if [ -n "${IN_NIX_SHELL:-}" ]; then
        echo "Nix: active (${IN_NIX_SHELL})"
    fi
    echo ""
    echo "Quick start:"
    echo "  make run        Run the proxy"
    echo "  make test       Run tests"
    echo "  make check      Run all checks"
    echo "  make help       Show all commands"
    echo ""
fi
