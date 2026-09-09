#!/usr/bin/env bash
set -eu

# Provisions a disposable Herdr session (its own socket, its own isolated
# XDG dirs, under /tmp), builds the duo-under-test from the working tree,
# runs capture-live.sh against it, and tears the disposable session down.
#
# Never touches the operator's real Herdr session ("duo") or its socket,
# and never edits the operator's real ~/.config/devin or
# ~/.local/share/devin — those are only read and copied INTO the isolated
# XDG dirs this script creates.
#
# Usage: run-live.sh
# (no args; session name and scratch dir are fixed below so repeat runs
# reuse the same disposable session name and overwrite the same scratch
# dir — matching the model scripts' "captures overwrite per run" style)

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
EVIDENCE="$REPO_ROOT/evidence/traces/devin-mint-claim-release"
SESSION="dmcrl01"
SCRATCH="/tmp/duo-${SESSION}"

if [ -n "${HERDR_SOCKET_PATH:-}" ]; then
  echo "refusing to run: HERDR_SOCKET_PATH is set in this shell (${HERDR_SOCKET_PATH});" >&2
  echo "this script must not inherit the operator's herdr socket." >&2
  exit 2
fi

echo "== provisioning disposable herdr session ${SESSION} under ${SCRATCH} =="
rm -rf "$SCRATCH"
mkdir -p "$SCRATCH/xdg-config" "$SCRATCH/xdg-data" "$SCRATCH/xdg-state" "$SCRATCH/xdg-runtime" "$SCRATCH/ws"
chmod 700 "$SCRATCH/xdg-runtime"

XDG_CONFIG_HOME="$SCRATCH/xdg-config"
XDG_DATA_HOME="$SCRATCH/xdg-data"
XDG_STATE_HOME="$SCRATCH/xdg-state"
XDG_RUNTIME_DIR="$SCRATCH/xdg-runtime"

# Copy in only what the devin runtime needs to authenticate: the
# operator's devin config + credentials, copied INTO the isolated XDG
# dirs (the real ~/.config/devin and ~/.local/share/devin are read-only
# here, never edited).
mkdir -p "$XDG_CONFIG_HOME/devin" "$XDG_DATA_HOME/devin"
if [ -d "$HOME/.config/devin" ]; then
  cp -r "$HOME/.config/devin/." "$XDG_CONFIG_HOME/devin/"
fi
if [ -d "$HOME/.local/share/devin" ]; then
  cp -r "$HOME/.local/share/devin/." "$XDG_DATA_HOME/devin/"
fi

# duo's own config: a copy of the operator's duo.config.yaml with the
# devin_swe17 launch variant enabled as a builder candidate (the
# operator's real config keeps it commented out; this isolated copy is
# ours to edit freely — the real one under ~/.config/duo is untouched).
mkdir -p "$XDG_CONFIG_HOME/duo"
python3 - "$HOME/.config/duo/duo.config.yaml" "$XDG_CONFIG_HOME/duo/duo.config.yaml" <<'PY'
import sys
src, dst = sys.argv[1], sys.argv[2]
text = open(src, encoding="utf-8").read()
lines = text.splitlines(keepends=True)
out = []
in_builder = False
for line in lines:
    stripped = line.strip()
    if stripped == "builder:":
        in_builder = True
    elif in_builder and stripped.endswith(":") and not line.startswith((" ", "\t")):
        in_builder = False
    if in_builder and stripped == "# - {variant: devin_swe17}":
        line = line.replace("# - {variant: devin_swe17}", "- {variant: devin_swe17}")
    out.append(line)
open(dst, "w", encoding="utf-8").write("".join(out))
PY
if ! grep -q '^\s*- {variant: devin_swe17}' "$XDG_CONFIG_HOME/duo/duo.config.yaml"; then
  echo "failed to enable devin_swe17 in the isolated duo.config.yaml" >&2
  exit 2
fi

echo "== building duo under test from the working tree =="
go -C "$REPO_ROOT" build -o "$SCRATCH/duo" ./cmd/duo
DUO="$SCRATCH/duo"
"$DUO" version

echo "== starting headless disposable herdr server (session ${SESSION}) =="
setsid env -i \
  HOME="$HOME" \
  XDG_CONFIG_HOME="$XDG_CONFIG_HOME" \
  XDG_DATA_HOME="$XDG_DATA_HOME" \
  XDG_STATE_HOME="$XDG_STATE_HOME" \
  XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" \
  HERDR_SESSION="$SESSION" \
  PATH="$PATH" \
  herdr server >"$SCRATCH/herdr-server.log" 2>&1 < /dev/null &
disown

SOCK="$XDG_CONFIG_HOME/herdr/sessions/$SESSION/herdr.sock"
for _ in $(seq 1 50); do
  [ -S "$SOCK" ] && break
  sleep 0.2
done
if [ ! -S "$SOCK" ]; then
  echo "disposable herdr server did not create its socket at $SOCK" >&2
  cat "$SCRATCH/herdr-server.log" >&2 || true
  exit 2
fi
echo "socket ready: $SOCK"
env HERDR_SOCKET_PATH="$SOCK" herdr agent list

RUN_STATUS=0
echo "== running capture-live.sh =="
SCRATCH="$SCRATCH" SOCK="$SOCK" SESSION="$SESSION" DUO="$DUO" EVIDENCE="$EVIDENCE" \
  XDG_CONFIG_HOME="$XDG_CONFIG_HOME" XDG_DATA_HOME="$XDG_DATA_HOME" \
  XDG_STATE_HOME="$XDG_STATE_HOME" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" \
  bash "$EVIDENCE/capture-live.sh" || RUN_STATUS=$?

echo "== tearing down disposable herdr session ${SESSION} =="
# `herdr session stop <name>` resolves sessions under the real
# $HOME/.config, not our isolated XDG_CONFIG_HOME, so it cannot find this
# disposable session by name. Instead, find the server process that was
# actually started with this session's XDG_CONFIG_HOME (matched via its
# environ, never by a bare process-name match) and stop only that one —
# this never touches the operator's real herdr server(s).
KILLED=0
for pid in $(pgrep -f "herdr server" 2>/dev/null || true); do
  if tr '\0' '\n' <"/proc/$pid/environ" 2>/dev/null | grep -qx "XDG_CONFIG_HOME=${XDG_CONFIG_HOME}"; then
    kill "$pid" 2>/dev/null || true
    KILLED=1
  fi
done
sleep 1
if [ "$KILLED" = "1" ] && [ ! -S "$SOCK" ]; then
  echo "disposable session ${SESSION} stopped; socket gone"
elif [ -S "$SOCK" ]; then
  echo "socket still present after stop attempt; leaving it — do not touch operator sessions" >&2
else
  echo "no matching disposable herdr server process found to stop (already gone?)" >&2
fi

echo "== disposable scratch dir kept for inspection: ${SCRATCH} (not referenced by captures; safe to remove) =="
exit "$RUN_STATUS"
