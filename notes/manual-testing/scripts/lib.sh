# notes/manual-testing/scripts/lib.sh — POSIX /bin/sh helpers
# sourced by every driver in this directory.
#
# Conventions:
# - Drivers `cd` to the Duo repo root so `./duo.config.yaml` (the
#   tester's local config, not the fixture) is what gets loaded.
# - Drivers emit a sequence of JSON-RPC lines on stdout, then pipe
#   that into `duo_drive`, which holds stdin open for $DUO_SLEEP
#   seconds (so Duo can flush all responses) and bounds the run
#   with `timeout $DUO_TIMEOUT`.
# - Override any tunable by exporting it in the calling shell:
#     DUO_TIMEOUT=20 ./02-spawn-agent.sh
#
# Tunables:
#   DUO_REPO_ROOT       Auto-derived from this file's path.
#   DUO_DIST            Path to dist/duo.mjs (default $DUO_REPO_ROOT/dist/duo.mjs).
#   DUO_NODE            `node` binary (default `node`).
#   DUO_TIMEOUT         Seconds before timeout(1) kills duo (default 10).
#   DUO_SLEEP           Seconds to keep stdin open after last request (default 5).
#   DUO_PROTOCOL        MCP protocol version string (default 2024-11-05).
#   DUO_CLIENT_NAME     Client-info name reported in initialize (default runbook).

# Resolve the repo root from this file's location once.
__lib_dir() {
  # POSIX-y: dirname of this file. Sourced files don't expose $0
  # reliably across shells, so callers pass HERE in. Fallback: cwd.
  if [ -n "${HERE:-}" ]; then
    printf '%s\n' "$HERE"
  else
    pwd
  fi
}

: "${DUO_REPO_ROOT:=$(cd "$(__lib_dir)/../../.." && pwd)}"
: "${DUO_DIST:=$DUO_REPO_ROOT/dist/duo.mjs}"
: "${DUO_NODE:=node}"
: "${DUO_TIMEOUT:=10}"
: "${DUO_SLEEP:=5}"
: "${DUO_PROTOCOL:=2024-11-05}"
: "${DUO_CLIENT_NAME:=runbook}"

# Emit the MCP handshake prelude (initialize + notifications/initialized).
# id=1 is reserved for initialize; downstream calls should start at id=2.
duo_handshake() {
  printf '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"%s","capabilities":{},"clientInfo":{"name":"%s","version":"0"}}}\n' \
    "$DUO_PROTOCOL" "$DUO_CLIENT_NAME"
  printf '{"jsonrpc":"2.0","method":"notifications/initialized"}\n'
}

# Emit a tools/list request. $1 = id (default 2).
duo_tools_list() {
  id="${1:-2}"
  printf '{"jsonrpc":"2.0","id":%s,"method":"tools/list"}\n' "$id"
}

# Emit a tools/call request. $1 = id, $2 = tool name, $3 = arguments JSON.
# Caller is responsible for arguments-JSON validity. Tier and name values
# are passed through verbatim — keep them alphanumeric for the runbook.
duo_tools_call() {
  id="$1"
  name="$2"
  args="$3"
  printf '{"jsonrpc":"2.0","id":%s,"method":"tools/call","params":{"name":"%s","arguments":%s}}\n' \
    "$id" "$name" "$args"
}

# Run the JSON-RPC stream from stdin against `dist/duo.mjs`.
# - cd to DUO_REPO_ROOT so duo.config.yaml resolves.
# - Hold stdin open via `sleep` so Duo can flush async responses.
# - Bound the run via `timeout` as a safety net. Duo exits cleanly
#   on stdin EOF, so a healthy run returns 0; 124 means Duo failed
#   to shut down on EOF and is a regression worth filing.
duo_drive() {
  cd "$DUO_REPO_ROOT" || {
    printf 'lib.sh: cannot cd to DUO_REPO_ROOT=%s\n' "$DUO_REPO_ROOT" >&2
    return 2
  }
  if [ ! -f "$DUO_DIST" ]; then
    printf 'lib.sh: %s not found — run `npm run build` first\n' "$DUO_DIST" >&2
    return 2
  fi
  {
    cat
    sleep "$DUO_SLEEP"
  } | timeout "$DUO_TIMEOUT" "$DUO_NODE" "$DUO_DIST" mcp
}
