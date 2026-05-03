#!/bin/sh
# 02-list-agent-tiers.sh — exercise list_agent_tiers.
# Referenced by 02-tier-tools.md §1 and 03-policy-overrides.md §2.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

{
  duo_handshake
  duo_tools_call 2 list_agent_tiers '{}'
} | duo_drive
