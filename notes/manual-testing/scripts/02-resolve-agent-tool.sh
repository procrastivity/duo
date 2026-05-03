#!/bin/sh
# 02-resolve-agent-tool.sh [tier] — exercise resolve_agent_tool.
# Referenced by 02-tier-tools.md §2 and 03-policy-overrides.md §1, §3.
#
# Args:
#   $1  tier (default: medium). One of small | medium | large.
#       Pass `purple` (or any other string) to exercise the
#       unsupported_tier failure path — see 02-resolve-unsupported.sh
#       for the dedicated failure-case driver.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

tier="${1:-medium}"

{
  duo_handshake
  duo_tools_call 2 resolve_agent_tool '{"tier":"'"$tier"'"}'
} | duo_drive
