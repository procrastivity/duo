#!/usr/bin/env bash
set -eu

# Live spike probe for the threads-new mint hypothesis (matter
# duo-amp-threads-new-spike; workplan on that matter carries the why).
#
# Answers four questions, live, against the operator's real Amp account:
#   1. What exactly does `amp threads new` print (URL shape, extractable
#      T-... id, exit code), under --settings-file isolation?
#   2. What does an EMPTY thread's export look like (message count 0,
#      lastKnownAgentState, title, default visibility)?
#   3. Does the FIRST `threads continue <tid> -x` on an EMPTY thread
#      deliver a normal turn? (The sealed exclusive-writer mint always
#      seeded the thread via `amp -x`; first-continue-on-empty is the
#      unprobed delta the mint redesign rests on.)
#   4. In-band billing signal: `amp threads usage <tid>` before and
#      after the first turn.
#
# No herdr, no duo binary: this probe is pure `amp` CLI in an isolated
# XDG home, mirroring evidence/traces/amp-exclusive-writer/run-live.sh's
# credential and settings recipe (notes/61 §1: the account credential
# lives at $HOME/.local/share/amp/secrets.json; copied into the isolated
# XDG_DATA_HOME, never edited in place).
#
# ============================================================================
# OPERATOR PRECONDITIONS — this script BILLS THE OPERATOR'S AMP ACCOUNT:
# one real turn (capture 02) plus exports/usage reads. Only the operator
# runs it; the agent that authored it must never execute it.
#
#   - `amp` on PATH. The script never execs `amp --version` (same I-D7
#     pin-hazard rationale as the exclusive-writer pair); capture 02's
#     export self-records the version in env.initial.platform.clientVersion.
#   - Real credentials at $HOME/.local/share/amp/secrets.json.
#   - Network reachable to ampcode.com.
#
# Usage: probe-live.sh
# (no args; fixed scratch dir /tmp/duo-ampnew01, captures overwrite per
# run in this script's own evidence directory)
# ============================================================================

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
EVIDENCE="$REPO_ROOT/evidence/traces/amp-threads-new-spike"
SCRATCH="/tmp/duo-ampnew01"

AMP_CRED_SRC="$HOME/.local/share/amp"
if [ ! -f "$AMP_CRED_SRC/secrets.json" ]; then
  echo "refusing to run: no Amp credentials at ${AMP_CRED_SRC}/secrets.json" >&2
  echo "(authenticate with a real amp run once, then retry; this script" >&2
  echo "only copies credentials into an isolated XDG_DATA_HOME)" >&2
  exit 2
fi

echo "== provisioning isolated amp home under ${SCRATCH} =="
rm -rf "$SCRATCH"
mkdir -p "$SCRATCH/xdg-config/amp" "$SCRATCH/xdg-data/amp" "$SCRATCH/xdg-state" "$SCRATCH/ws"
XDG_CONFIG_HOME="$SCRATCH/xdg-config"
XDG_DATA_HOME="$SCRATCH/xdg-data"
export XDG_CONFIG_HOME XDG_DATA_HOME

cp "$AMP_CRED_SRC/secrets.json" "$XDG_DATA_HOME/amp/secrets.json"
if [ -f "$AMP_CRED_SRC/device-id.json" ]; then
  cp "$AMP_CRED_SRC/device-id.json" "$XDG_DATA_HOME/amp/device-id.json"
fi

# Same document run-live.sh writes: no self-update mid-probe, no
# terminal animation in tee'd output.
AMP_SETTINGS_PATH="$XDG_CONFIG_HOME/amp/settings.json"
cat >"$AMP_SETTINGS_PATH" <<'JSON'
{
  "amp.updates.mode": "disabled",
  "amp.terminal.animation": false
}
JSON

amp_cmd() { amp --settings-file "$AMP_SETTINGS_PATH" "$@"; }

NOW_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ISOLATION_LINE="isolation: scratch=${SCRATCH} settings=${AMP_SETTINGS_PATH} (no herdr, no duo)"

write_header() {
  local file="$1" title="$2"
  {
    echo "# $title"
    echo "$NOW_ISO"
    echo "$ISOLATION_LINE"
    echo
  } >"$file"
}

append_cmd() {
  local file="$1"
  local cmd="$2"
  local st=0
  {
    echo "\$ $cmd"
    set +e
    eval "$cmd" 2>&1
    st=$?
    set -e
    echo "exit=$st"
    echo
  } >>"$file"
}

# Belt-and-braces: whatever exit path this script takes, never leave the
# disposable thread unarchived on the operator's account.
TID=""
ARCHIVED=0
cleanup() {
  if [ -n "$TID" ] && [ "$ARCHIVED" != "1" ]; then
    echo "== belt-and-braces: archiving disposable thread ${TID} ==" | tee "$SCRATCH/teardown-archive.log"
    amp_cmd threads archive "$TID" >>"$SCRATCH/teardown-archive.log" 2>&1 \
      || echo "teardown archive attempt failed; archive ${TID} by hand" >&2
  fi
}
trap cleanup EXIT

# ---------------------------------------------------------------------------
# 01-new.txt — `amp threads new`: raw output, extracted thread id, usage
# on the fresh thread, and the empty-thread export shape. PASS when a
# T-... id is extractable and the export answers with message_count=0.
# ---------------------------------------------------------------------------
write_header "$EVIDENCE/01-new.txt" "amp threads new: creation output, fresh-thread usage, empty-thread export"

NEW_OUT_FILE="$SCRATCH/threads-new-out.txt"
set +e
amp_cmd threads new >"$NEW_OUT_FILE" 2>&1
NEW_STATUS=$?
set -e
{
  echo "\$ amp_cmd threads new"
  cat "$NEW_OUT_FILE"
  echo "exit=$NEW_STATUS"
  echo
} >>"$EVIDENCE/01-new.txt"

# The id, from the printed output: first T-<hex/dash> token. Recorded
# next to the verbatim output above, so a changed print format is
# visible in the capture even when this extraction breaks.
TID="$(grep -oE 'T-[0-9a-f][0-9a-f-]+' "$NEW_OUT_FILE" | head -1 || true)"
echo "thread_id=${TID}" >>"$EVIDENCE/01-new.txt"

if [ "$NEW_STATUS" -ne 0 ] || [ -z "$TID" ]; then
  {
    echo "bound_check=FAIL (threads new exit=${NEW_STATUS}, extracted thread_id='${TID}' — no disposable thread to probe further)"
    echo
  } >>"$EVIDENCE/01-new.txt"
  echo "probe-live.sh: threads new did not yield a thread id; aborting" >&2
  exit 2
fi

append_cmd "$EVIDENCE/01-new.txt" "amp_cmd threads usage ${TID}"

EXPORT0_JSON="$(amp_cmd threads export "$TID" 2>&1 || true)"
EXPORT0_SUMMARY="$(python3 -c "
import json, sys
raw = sys.argv[1]
try:
    d = json.loads(raw)
    meta = d.get('meta') or {}
    print(f\"message_count={len(d.get('messages') or [])}\")
    print(f\"title={d.get('title', '')!r}\")
    print(f\"visibility={meta.get('visibility', '')}\")
    print(f\"lastKnownAgentState={(meta.get('lastKnownAgentState') or {}).get('state', '')}\")
    print(f\"createdOnServer={meta.get('createdOnServer', '')}\")
except Exception as e:
    print(f'export_parse_error={e}')
" "$EXPORT0_JSON")"
{
  echo "\$ amp_cmd threads export ${TID} (empty-thread shape; summary below, full JSON in ${SCRATCH}/export-empty.json)"
  echo "$EXPORT0_SUMMARY"
  echo
} >>"$EVIDENCE/01-new.txt"
printf '%s' "$EXPORT0_JSON" >"$SCRATCH/export-empty.json"

EMPTY_COUNT="$(echo "$EXPORT0_SUMMARY" | sed -n 's/^message_count=//p')"
{
  if [ "$EMPTY_COUNT" = "0" ]; then
    echo "bound_check=PASS (threads new printed an extractable id; export shows an empty thread, message_count=0)"
  else
    echo "bound_check=FAIL (message_count='${EMPTY_COUNT}', expected 0 — creation was not turn-free, or export did not parse)"
  fi
  echo
} >>"$EVIDENCE/01-new.txt"

if [ "$EMPTY_COUNT" != "0" ]; then
  echo "probe-live.sh: empty-thread check failed; aborting before the billed turn" >&2
  exit 2
fi

# ---------------------------------------------------------------------------
# 02-first-continue.txt — the billed heart of the spike: the FIRST
# `threads continue <tid> -x` on the empty thread, same recipe shape
# DeliverPrompt uses (internal/runtime/amp/prompt.go: prompt on stdin,
# --stream-json, --no-archive-after-execute). PASS when the stream log
# carries session_id=<tid> plus a result/success record and the export
# lands at message_count=2.
# ---------------------------------------------------------------------------
write_header "$EVIDENCE/02-first-continue.txt" "first threads continue -x on the empty thread (billed turn)"

STREAM_LOG="$SCRATCH/first-continue-stream.jsonl"
CONT_CMD="printf '%s\n' 'Reply with the single word pong.' | amp_cmd threads continue ${TID} -x --no-archive-after-execute --stream-json | tee ${STREAM_LOG}"
append_cmd "$EVIDENCE/02-first-continue.txt" "$CONT_CMD"

STREAM_SUMMARY="$(python3 -c "
import json, sys
path = sys.argv[1]
sid = ''
success = False
try:
    with open(path, encoding='utf-8') as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            try:
                rec = json.loads(line)
            except Exception:
                continue
            if rec.get('session_id') and not sid:
                sid = rec['session_id']
            if rec.get('type') == 'result' and rec.get('subtype') == 'success':
                success = True
except FileNotFoundError:
    pass
print(f'stream_session_id={sid}')
print(f'stream_result_success={str(success).lower()}')
" "$STREAM_LOG")"
{
  echo "$STREAM_SUMMARY"
  echo
} >>"$EVIDENCE/02-first-continue.txt"
STREAM_SID="$(echo "$STREAM_SUMMARY" | sed -n 's/^stream_session_id=//p')"
STREAM_OK="$(echo "$STREAM_SUMMARY" | sed -n 's/^stream_result_success=//p')"

append_cmd "$EVIDENCE/02-first-continue.txt" "amp_cmd threads usage ${TID}"

# Retried for the documented export sync lag (notes/63 §1).
EXPORT1_COUNT=0
EXPORT1_LAST=""
EXPORT1_VERSION=""
for _ in 1 2 3 4; do
  EXPORT1_JSON="$(amp_cmd threads export "$TID" 2>&1 || true)"
  EXPORT1_SUMMARY="$(python3 -c "
import json, sys
raw = sys.argv[1]
try:
    d = json.loads(raw)
    msgs = d.get('messages') or []
    plat = ((d.get('env') or {}).get('initial') or {}).get('platform') or {}
    print(len(msgs))
    print(msgs[-1].get('role', '') if msgs else '')
    print(plat.get('clientVersion', ''))
except Exception:
    print(0); print(''); print('')
" "$EXPORT1_JSON")"
  EXPORT1_COUNT="$(echo "$EXPORT1_SUMMARY" | sed -n 1p)"
  EXPORT1_LAST="$(echo "$EXPORT1_SUMMARY" | sed -n 2p)"
  EXPORT1_VERSION="$(echo "$EXPORT1_SUMMARY" | sed -n 3p)"
  if [ "${EXPORT1_COUNT:-0}" -ge 2 ] 2>/dev/null; then
    break
  fi
  sleep 3
done
printf '%s' "$EXPORT1_JSON" >"$SCRATCH/export-after-turn.json"
{
  echo "\$ amp_cmd threads export ${TID} (retried up to 4x/3s for sync lag; full JSON in ${SCRATCH}/export-after-turn.json)"
  echo "export_message_count=${EXPORT1_COUNT}"
  echo "export_last_message_role=${EXPORT1_LAST}"
  echo "export_client_version=${EXPORT1_VERSION}"
  echo
  if [ "$STREAM_OK" = "true" ] && [ "$STREAM_SID" = "$TID" ] && [ "${EXPORT1_COUNT:-0}" -ge 2 ] 2>/dev/null; then
    echo "bound_check=PASS (first continue on the empty thread delivered: stream result/success with session_id=${TID}; export shows ${EXPORT1_COUNT} messages, last role=${EXPORT1_LAST})"
  else
    echo "bound_check=FAIL (stream_result_success=${STREAM_OK}, stream_session_id=${STREAM_SID}, export_message_count=${EXPORT1_COUNT}; expected success, matching id, >=2 messages)"
  fi
  echo
} >>"$EVIDENCE/02-first-continue.txt"

# ---------------------------------------------------------------------------
# 03-archive.txt — disposable thread cleanup. PASS on a clean exit 0.
# ---------------------------------------------------------------------------
write_header "$EVIDENCE/03-archive.txt" "amp threads archive (disposable thread cleanup)"
set +e
ARCHIVE_OUT="$(amp_cmd threads archive "$TID" 2>&1)"
ARCHIVE_STATUS=$?
set -e
{
  echo "\$ amp_cmd threads archive ${TID}"
  echo "$ARCHIVE_OUT"
  echo "exit=$ARCHIVE_STATUS"
  echo
  if [ "$ARCHIVE_STATUS" -eq 0 ]; then
    echo "bound_check=PASS (amp threads archive exited 0)"
  else
    echo "bound_check=FAIL (amp threads archive exited ${ARCHIVE_STATUS})"
  fi
  echo
} >>"$EVIDENCE/03-archive.txt"
if [ "$ARCHIVE_STATUS" -eq 0 ]; then
  ARCHIVED=1
fi

echo
echo "== done. read every PASS/FAIL line in ${EVIDENCE}/0*.txt =="
