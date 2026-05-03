#!/bin/sh
# 01-tools-list.sh — handshake + tools/list. Referenced by
# 01-running-duo.md §"Option B".
#
# Expected stdout: two JSON-RPC responses (initialize, tools/list),
# the latter with a length-3 `result.tools` array.
set -eu
HERE="$(cd "$(dirname "$0")" && pwd)"
. "$HERE/lib.sh"

{
  duo_handshake
  duo_tools_list 2
} | duo_drive
