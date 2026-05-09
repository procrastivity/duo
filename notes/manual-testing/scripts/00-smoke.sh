#!/bin/sh
# 00-smoke.sh — boot Duo with no requests (stdin held open
# briefly via `duo_drive`'s sleep, then EOF) and assert clean
# startup. Referenced by 00-setup.md §6.
#
# Healthy = exit 0 with empty stderr. Stderr is captured to a
# temp file and the script fails if it's non-empty, so the check
# is enforceable in CI / scripting (not just visual).
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

err="$(mktemp -t duo-smoke-err.XXXXXX)"
trap 'rm -f "$err"' EXIT

rc=0
: | duo_drive 2>"$err" >/dev/null || rc=$?

if [ "$rc" -ne 0 ]; then
  printf '00-smoke: duo exited rc=%s\n' "$rc" >&2
  cat "$err" >&2
  exit "$rc"
fi

if [ -s "$err" ]; then
  printf '00-smoke: stderr was non-empty (expected silent boot):\n' >&2
  cat "$err" >&2
  exit 1
fi
