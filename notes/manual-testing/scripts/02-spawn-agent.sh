#!/bin/sh
# 02-spawn-agent.sh [tier] [name] [project_id] — exercise spawn_agent.
# Referenced by 02-tier-tools.md §3.
#
# Args (all optional):
#   $1  tier        (default: medium). small | medium | large.
#   $2  name        (default: duo-runbook-test).
#   $3  project_id  integer Solo project ID. If omitted, the request
#                   sends no project_id; SoloClient injects whatever
#                   it resolved at connect (SOLO_PROJECT_ID env or
#                   pwd→list_projects longest-match).
#
# Stop the spawned process with `mcp__solo__stop_process` once the
# response has been verified.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

tier="${1:-medium}"
name="${2:-duo-runbook-test}"
project_id="${3:-}"

if [ -n "$project_id" ]; then
  # project_id is an integer (Solo i64); intentionally unquoted in JSON.
  args='{"tier":"'"$tier"'","name":"'"$name"'","project_id":'"$project_id"'}'
else
  args='{"tier":"'"$tier"'","name":"'"$name"'"}'
fi

{
  duo_handshake
  duo_tools_call 2 spawn_agent "$args"
} | duo_drive
