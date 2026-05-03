#!/bin/sh
# 02-spawn-unsupported.sh — failure case for spawn_agent.
# Referenced by 02-tier-tools.md §3 "Failure case".
#
# Expected stdout: a tools/call response whose payload contains
#   error_code: "unsupported_tier"
# Expected stderr: a `spawn.failure` log event (and possibly a
# preceding `resolution.failure`).
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

{
  duo_handshake
  duo_tools_call 2 spawn_agent '{"tier":"purple"}'
} | duo_drive
