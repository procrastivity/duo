#!/bin/sh
# 02-resolve-unsupported.sh — failure case for resolve_agent_tool.
# Referenced by 02-tier-tools.md §2 "Failure case".
#
# Expected stdout: a tools/call response whose payload contains
#   error_code: "unsupported_tier"
#   available_tiers: ["small","medium","large"]
# Expected stderr: a `resolution.failure` log event.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

{
  duo_handshake
  duo_tools_call 2 resolve_agent_tool '{"tier":"purple"}'
} | duo_drive
