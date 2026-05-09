#!/bin/sh
# 04-log-walkthrough.sh [tier] — fire a representative sequence of
# tool calls and capture stdout + stderr separately for inspection.
# Referenced by 04-logging.md §1.
#
# Args:
#   $1  tier  (default: medium). Used for the resolve_agent_tool
#             success-case call.
#
# Outputs (relative to repo root, the cwd `duo_drive` switches into):
#   /tmp/duo.out   one JSON-RPC response per line on stdout
#   /tmp/duo.err   one structured log event per line on stderr
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

tier="${1:-medium}"

OUT="${DUO_OUT:-/tmp/duo.out}"
ERR="${DUO_ERR:-/tmp/duo.err}"

{
  duo_handshake
  duo_tools_call 2 list_agent_tiers '{}'
  duo_tools_call 3 resolve_agent_tool '{"tier":"'"$tier"'"}'
  duo_tools_call 4 resolve_agent_tool '{"tier":"purple"}'
} | duo_drive >"$OUT" 2>"$ERR" || rc=$?

# Duo exits cleanly on stdin EOF (rc=0). Older builds relied on
# `timeout` killing it (rc=124); accept both for compatibility.
rc="${rc:-0}"
if [ "$rc" -ne 124 ] && [ "$rc" -ne 0 ]; then
  printf '04-log-walkthrough: duo exited rc=%s; see %s\n' "$rc" "$ERR" >&2
  exit "$rc"
fi

printf 'stdout: %s (%s lines)\n' "$OUT" "$(wc -l <"$OUT" | tr -d ' ')"
printf 'stderr: %s (%s lines)\n' "$ERR" "$(wc -l <"$ERR" | tr -d ' ')"
