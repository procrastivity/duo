#!/usr/bin/env bash
set -eu

# Provisions a disposable Herdr session (its own socket, its own isolated
# XDG dirs, under /tmp), builds the duo-under-test from the working tree,
# runs capture-live.sh against it, and tears the disposable session down.
# Mirrors evidence/traces/devin-mint-claim-release/run-live.sh's isolation
# discipline exactly (same rm -rf/mkdir/chmod 700 scratch layout, same
# setsid-headless-server pattern, same environ-matched teardown), adapted
# for the Amp runtime's own credential and settings shape.
#
# Never touches the operator's real Herdr session ("duo") or its socket,
# and never edits the operator's real ~/.local/share/amp or
# ~/.config/amp — those are only read and copied INTO the isolated XDG
# dirs this script creates; the settings file this script writes is a
# fresh generated document, never an edit of the operator's own settings.
#
# ============================================================================
# OPERATOR PRECONDITIONS — read before running. This script AUTHORS AND
# RUNS a live seal capture: every Amp turn it triggers (the mint, the
# instruct turn, the collision probe, the `amp threads export`/`archive`
# reads) BILLS THE OPERATOR'S AMP ACCOUNT. Only the operator runs this
# script (step-07 of this matter); an agent authoring the pair must never
# execute it.
#
#   - `amp` on PATH, at the pinned version 0.0.1788048110-g570348
#     (docs/adapters/decisions.md, 2026-09-09, "Amp exclusive-writer scope
#     is per-turn, not per-session", "Pin"). This script does not exec
#     `amp --version` to check it — same I-D7 pin-hazard rationale as
#     Factory.Probe's own doc comment (internal/runtime/amp/amp.go): Amp's
#     build train ships hourly and auto-updates by default, so a version
#     exec here would only ever name a version this evidence was not
#     necessarily verified against. Confirm the pin by hand
#     (`amp --version`) before running.
#   - Real Amp credentials present at $HOME/.local/share/amp/secrets.json
#     (see "Amp credentials" below for how this was found).
#   - `herdr` on PATH, and no herdr server already bound to the disposable
#     session name below.
#   - Network reachable to ampcode.com (every Amp read and write in this
#     capture is a real network call against the operator's account).
#
# Usage: run-live.sh
# (no args; session name and scratch dir are fixed below so repeat runs
# reuse the same disposable session name and overwrite the same scratch
# dir — matching the model scripts' "captures overwrite per run" style)

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
EVIDENCE="$REPO_ROOT/evidence/traces/amp-exclusive-writer"
SESSION="ampxw01"
SCRATCH="/tmp/duo-${SESSION}"

if [ -n "${HERDR_SOCKET_PATH:-}" ]; then
  echo "refusing to run: HERDR_SOCKET_PATH is set in this shell (${HERDR_SOCKET_PATH});" >&2
  echo "this script must not inherit the operator's herdr socket." >&2
  exit 2
fi

# --- Amp credentials -------------------------------------------------------
#
# The brief that opened this step guessed ~/.config/amp; that guess does
# not hold. notes/61-amp-feasibility.md §1 (terminal-multiplexers, Tier A,
# verified) swept ~/.config/amp, ~/.local/share/amp, and ~/.cache/amp and
# found the account credential at $HOME/.local/share/amp/secrets.json
# (device identity beside it at device-id.json); ~/.config/amp holds only
# settings.json and plugins — no credential. secrets.json lives under
# $HOME/.local/share, i.e. under $XDG_DATA_HOME by the same base-directory
# convention amp.DefaultHarnessDir and amp.MintLogPath already key off
# (internal/runtime/amp/materialize.go, mintlog.go); copying it into this
# script's own isolated $XDG_DATA_HOME/amp is therefore what makes the
# isolated `amp` binary see the operator's real account, exactly the way
# copying $HOME/.local/share/devin into the isolated XDG_DATA_HOME/devin
# authenticates the Devin pair. Never write into $HOME/.local/share/amp
# itself — read-only source, copied, never edited.
AMP_CRED_SRC="$HOME/.local/share/amp"
if [ ! -f "$AMP_CRED_SRC/secrets.json" ]; then
  echo "refusing to run: no Amp credentials found at ${AMP_CRED_SRC}/secrets.json" >&2
  echo "(notes/61-amp-feasibility.md, \"Provenance and security\": the CLI" >&2
  echo "authenticates with AMP_API_KEY / stored credentials at" >&2
  echo "~/.local/share/amp/secrets.json). This script only copies the" >&2
  echo "operator's real credentials into an isolated XDG_DATA_HOME; it" >&2
  echo "never edits ~/.local/share/amp itself. Authenticate with a real" >&2
  echo "\`amp\` run once, then retry." >&2
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

# Copy in only what the amp runtime needs to authenticate: secrets.json
# (required, checked above) and device-id.json (present alongside it in
# every sweep notes/61/62 ran; copied when present, not required on its
# own — losing it does not lose the account credential). Neither
# history.jsonl nor session.json is copied: both are non-credential local
# UI/prompt-history state the isolation has no need to inherit, and
# copying prompt history the isolation did not itself write would be the
# same hygiene violation notes/61's own probe called out against copying
# another probe's prompts.
mkdir -p "$XDG_DATA_HOME/amp"
cp "$AMP_CRED_SRC/secrets.json" "$XDG_DATA_HOME/amp/secrets.json"
if [ -f "$AMP_CRED_SRC/device-id.json" ]; then
  cp "$AMP_CRED_SRC/device-id.json" "$XDG_DATA_HOME/amp/device-id.json"
fi

# Amp settings for every *direct* `amp` call this capture makes (the
# TUI collision holder, `threads export`, `threads archive`) — disable
# the CLI's own auto-update check (never silently re-exec a newer binary
# mid-capture) and its terminal animation (noise a headless/tee'd or
# scripted run never needs). This is the exact document
# amp.RenderSettings (internal/runtime/amp/materialize.go) generates; it
# is duplicated here in shell because this settings file's job is
# different from the one Duo's own launch leaf augmenter materializes —
# that one is baked into the mint wrapper script's own `--settings-file`
# flag (amp.MaterializeSettings, called from stage1LeafAugmenter's Amp
# leg), by Duo, per launch, and this script never touches it. This one is
# for the direct `amp` invocations capture-live.sh itself makes outside
# any Duo-materialized wrapper.
mkdir -p "$XDG_CONFIG_HOME/amp"
AMP_SETTINGS_PATH="$XDG_CONFIG_HOME/amp/settings.json"
cat >"$AMP_SETTINGS_PATH" <<'JSON'
{
  "amp.updates.mode": "disabled",
  "amp.terminal.animation": false
}
JSON

# duo's own config: a copy of the operator's duo.config.yaml with an Amp
# agent runtime, launch variant, and a dedicated "amp_mint" preset added.
# Unlike the Devin pair (which only had to un-comment an existing
# `devin_swe17` placeholder), the operator's real duo.config.yaml carries
# no Amp scaffolding at all — not even commented out — so there is no
# placeholder line to toggle. This uses PyYAML to load the real config,
# add the three Amp keys, and dump the merged document, rather than
# string-splicing a document with nothing to splice onto. The added shape
# mirrors internal/cli/session_launch_amp_augment_test.go's
# ampScenarioYAML exactly (agent_runtimes.<name>.kind: amp, executable:
# bash — stage1LeafAugmenter's doc comment: the mint wrapper script's own
# path is the leaf's sole appended argument, so the leaf needs a shell
# able to run it, never `amp` itself as the declared executable).
mkdir -p "$XDG_CONFIG_HOME/duo"
set +e
python3 - "$HOME/.config/duo/duo.config.yaml" "$XDG_CONFIG_HOME/duo/duo.config.yaml" <<'PY'
import sys
import yaml

src, dst = sys.argv[1], sys.argv[2]
with open(src, encoding="utf-8") as f:
    doc = yaml.safe_load(f)

doc.setdefault("agent_runtimes", {})["amp_default"] = {
    "kind": "amp",
    "executable": "bash",
}
doc.setdefault("launch_variants", {})["amp_default"] = {
    "agent_runtime": "amp_default",
    "model_family": "amp",
    "model_line": "amp-default",
}
doc.setdefault("presets", {})["amp_mint"] = {
    "selection": "ordered",
    "leaves": {"main": {"candidates": [{"variant": "amp_default"}]}},
}

with open(dst, "w", encoding="utf-8") as f:
    yaml.safe_dump(doc, f, sort_keys=False, default_flow_style=False)

# Verify the merge actually landed before this script trusts it — a
# silent no-op here would surface much later as a confusing launch
# failure instead of an honest refusal now.
with open(dst, encoding="utf-8") as f:
    check = yaml.safe_load(f)
assert check.get("agent_runtimes", {}).get("amp_default", {}).get("kind") == "amp"
assert check.get("launch_variants", {}).get("amp_default", {}).get("agent_runtime") == "amp_default"
assert check["presets"]["amp_mint"]["leaves"]["main"]["candidates"][0]["variant"] == "amp_default"
PY
PY_STATUS=$?
set -e
if [ "$PY_STATUS" -ne 0 ]; then
  echo "failed to author the isolated duo.config.yaml's amp_mint preset" >&2
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
echo "== running capture-live.sh (bills the Amp account from here on) =="
SCRATCH="$SCRATCH" SOCK="$SOCK" SESSION="$SESSION" DUO="$DUO" EVIDENCE="$EVIDENCE" \
  XDG_CONFIG_HOME="$XDG_CONFIG_HOME" XDG_DATA_HOME="$XDG_DATA_HOME" \
  XDG_STATE_HOME="$XDG_STATE_HOME" XDG_RUNTIME_DIR="$XDG_RUNTIME_DIR" \
  AMP_SETTINGS_PATH="$AMP_SETTINGS_PATH" \
  bash "$EVIDENCE/capture-live.sh" || RUN_STATUS=$?

# Belt-and-braces archive: capture-live.sh's own 05-archive.txt is the
# primary archive evidence. If it never ran (an earlier capture failed
# and the script exited before reaching it) or did not leave a clean
# result, this teardown makes one more attempt so a failed capture run
# never leaves a billed thread sitting open on the operator's account —
# billing hygiene, not evidence: its outcome is logged to
# $SCRATCH/teardown-archive.log, not into the numbered EVIDENCE files.
THREAD_ID_FILE="$SCRATCH/thread-id.txt"
ARCHIVE_CAPTURE="$EVIDENCE/05-archive.txt"
if [ -f "$THREAD_ID_FILE" ]; then
  TID="$(cat "$THREAD_ID_FILE")"
  ALREADY_ARCHIVED=0
  if [ -f "$ARCHIVE_CAPTURE" ] && grep -q '^exit=0$' "$ARCHIVE_CAPTURE" 2>/dev/null; then
    ALREADY_ARCHIVED=1
  fi
  if [ "$ALREADY_ARCHIVED" != "1" ] && [ -n "$TID" ]; then
    echo "== belt-and-braces: archiving disposable thread ${TID} (capture-live.sh did not confirm it) ==" | tee "$SCRATCH/teardown-archive.log"
    env HOME="$HOME" XDG_CONFIG_HOME="$XDG_CONFIG_HOME" XDG_DATA_HOME="$XDG_DATA_HOME" \
      PATH="$PATH" amp threads archive "$TID" --settings-file "$AMP_SETTINGS_PATH" \
      >>"$SCRATCH/teardown-archive.log" 2>&1 || echo "teardown archive attempt failed; see $SCRATCH/teardown-archive.log" >&2
  fi
fi

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
