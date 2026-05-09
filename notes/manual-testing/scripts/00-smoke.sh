#!/bin/sh
# 00-smoke.sh — boot Duo with a closed stdin and confirm no early
# stderr errors. Referenced by 00-setup.md §6.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

# No requests; just open the process. Duo exits cleanly on stdin
# EOF, so a healthy run returns exit 0 with no stderr output.
: | duo_drive
