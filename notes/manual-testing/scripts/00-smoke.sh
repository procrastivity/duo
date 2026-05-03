#!/bin/sh
# 00-smoke.sh — boot Duo with a closed stdin and confirm no early
# stderr errors. Referenced by 00-setup.md §6.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

# No requests; just open the process for $DUO_TIMEOUT and let
# `timeout` kill it. Exit 124 = healthy.
: | duo_drive
