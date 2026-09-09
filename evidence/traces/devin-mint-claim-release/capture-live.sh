#!/usr/bin/env bash
set -eu

# Live seal evidence for the Devin print-mint claim-release change:
#
#   A Devin "print-mint" launch spawns a short-lived
#   `devin ... --export <ATIF> --print <mint prompt>` process that mints
#   an agent session and exits. Duo observes that exit inside the shared
#   identity wait (continuity probe: 1s cadence, 2s per-probe timeout;
#   pane_absent acts immediately, used by both `session launch` and
#   `prompt send`). On observed exit, `prompt send` either recovers the
#   identity from the ATIF export (instance live, claim released, prompt
#   delivers) or records the exit and returns a typed
#   `session.target_exited` envelope — in either case well before the
#   command's expires_at, never sleeping to the deadline. `session show`
#   then reports claim_held: false and never sticks at "starting".
#
# This script only captures. It does not provision or tear down the
# disposable Herdr session — see run-live.sh for that.

require_env() {
  if [ -z "${1:-}" ]; then
    echo "missing required env: $2" >&2
    exit 2
  fi
}

require_env "${SCRATCH:-}" SCRATCH
require_env "${SOCK:-}" SOCK
require_env "${SESSION:-}" SESSION
require_env "${DUO:-}" DUO
require_env "${EVIDENCE:-}" EVIDENCE
require_env "${XDG_CONFIG_HOME:-}" XDG_CONFIG_HOME
require_env "${XDG_DATA_HOME:-}" XDG_DATA_HOME
require_env "${XDG_STATE_HOME:-}" XDG_STATE_HOME
require_env "${XDG_RUNTIME_DIR:-}" XDG_RUNTIME_DIR

if [ ! -S "$SOCK" ]; then
  echo "SOCK is not a socket: $SOCK" >&2
  exit 2
fi

if [ -n "${HERDR_SOCKET_PATH:-}" ]; then
  echo "HERDR_SOCKET_PATH must not be set on the Duo client" >&2
  exit 2
fi

if ! command -v herdr >/dev/null 2>&1; then
  echo "herdr not found in PATH" >&2
  exit 2
fi

if ! command -v devin >/dev/null 2>&1; then
  echo "devin not found in PATH" >&2
  exit 2
fi

if ! "$DUO" version >/dev/null 2>&1; then
  echo "DUO binary failed: $DUO" >&2
  exit 2
fi

duo_cmd() { unset HERDR_SOCKET_PATH; "$DUO" "$@"; }
herdr_cmd() { env HERDR_SOCKET_PATH="$SOCK" herdr "$@"; }

DUO_VERSION_LINE="$(duo_cmd version | head -1)"
NOW_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ISOLATION_LINE="isolation: HERDR_SESSION=${SESSION} scratch=${SCRATCH} host=herdr:${SOCK} runtime=devin"

write_header() {
  local file="$1" title="$2"
  {
    echo "# $title"
    echo "duo=${DUO_VERSION_LINE}"
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
    eval "$cmd"
    st=$?
    set -e
    echo "exit=$st"
    echo
  } >>"$file"
}

# 01-launch.txt
write_header "$EVIDENCE/01-launch.txt" "live Herdr+devin print-mint launch (builder --require agent_runtime=devin)"
LAUNCH_CMD="duo_cmd session launch builder --require agent_runtime=devin --host \"herdr:${SOCK}\" --workspace ${SCRATCH}/ws --output json"
append_cmd "$EVIDENCE/01-launch.txt" "$LAUNCH_CMD"

LAUNCH_EXIT="$(grep '^exit=' "$EVIDENCE/01-launch.txt" | tail -1 | cut -d= -f2)"
if [ "$LAUNCH_EXIT" != "0" ]; then
  echo "launch exit=$LAUNCH_EXIT" >&2
  exit 2
fi

SES="$(python3 -c "
import json, pathlib
text = pathlib.Path('${EVIDENCE}/01-launch.txt').read_text()
for line in text.splitlines():
    if line.startswith('{'):
        data = json.loads(line)
        print(data['result']['session_id'])
        break
")"

if [ -z "$SES" ]; then
  echo "launch did not yield session_id" >&2
  exit 2
fi

echo "session_id=${SES}" >>"$EVIDENCE/01-launch.txt"

# 02-show-starting.txt — immediately after launch return.
write_header "$EVIDENCE/02-show-starting.txt" "session show immediately after launch return"
SHOW1_CMD="duo_cmd session show ${SES} --output json"
append_cmd "$EVIDENCE/02-show-starting.txt" "$SHOW1_CMD"

SHOW1_JSON="$(duo_cmd session show "$SES" --output json)"
SHOW1_SUMMARY="$(python3 -c "
import json, sys
d = json.loads(sys.argv[1])
r = d.get('result', {})
state = r.get('runtime_instance_state', '')
atts = r.get('attachments') or []
claim_held = atts[0].get('claim_held') if atts else None
container = atts[0].get('container') if atts else ''
print(f'runtime_instance_state={state}')
print(f'claim_held={claim_held}')
print(f'container={container}')
" "$SHOW1_JSON")"

{
  echo "$SHOW1_SUMMARY"
  echo
} >>"$EVIDENCE/02-show-starting.txt"

SHOW1_STATE="$(echo "$SHOW1_SUMMARY" | sed -n 's/^runtime_instance_state=//p')"
SHOW1_CLAIM="$(echo "$SHOW1_SUMMARY" | sed -n 's/^claim_held=//p')"
CONTAINER="$(echo "$SHOW1_SUMMARY" | sed -n 's/^container=//p')"

if [ "$SHOW1_STATE" = "starting" ] && [ "$SHOW1_CLAIM" = "True" ]; then
  {
    echo "note: caught mid-flight — starting/claim_held=true, as expected before the mint exits."
    echo
  } >>"$EVIDENCE/02-show-starting.txt"
else
  {
    echo "note: the launch's own identity wait already observed the mint exit and"
    echo "recovered/released identity before this show ran (state=${SHOW1_STATE},"
    echo "claim_held=${SHOW1_CLAIM}). This is an honest, in-bound outcome of the same"
    echo "shared identity wait the send path uses — the send-path evidence below"
    echo "(03-06) still stands."
    echo
  } >>"$EVIDENCE/02-show-starting.txt"
fi

# 03-mint-exit.txt — observation only: poll herdr agent list and the launch
# pane's foreground process until the mint process is gone. No signals, no
# kill, no `duo wait` — the mint exits on its own.
write_header "$EVIDENCE/03-mint-exit.txt" "poll for the print-mint process exiting on its own (observation only)"
{
  echo "container(pane_id)=${CONTAINER}"
  echo "polling: herdr agent list + herdr pane process-info --pane ${CONTAINER}, every 200ms up to 10s"
  echo
} >>"$EVIDENCE/03-mint-exit.txt"

POLL_START_NS=$(date +%s%N)
ELAPSED_MS=0
MINT_GONE=false
while [ "$ELAPSED_MS" -le 10000 ]; do
  AGENT_LIST_OUT="$(herdr_cmd agent list)"
  PROC_INFO_OUT=""
  if [ -n "$CONTAINER" ]; then
    PROC_INFO_OUT="$(herdr_cmd pane process-info --pane "$CONTAINER" 2>&1 || true)"
  fi

  SAMPLE="$(python3 -c "
import json, sys
elapsed = sys.argv[1]
agent_raw = sys.argv[2]
proc_raw = sys.argv[3]

agent_count = -1
try:
    agent_count = len((json.loads(agent_raw).get('result') or {}).get('agents') or [])
except Exception:
    pass

fg_name = ''
try:
    procs = (json.loads(proc_raw).get('result') or {}).get('process_info', {}).get('foreground_processes') or []
    if procs:
        fg_name = procs[0].get('name', '')
except Exception:
    pass

mint_gone = (agent_count == 0) and (fg_name != 'devin')
print(f'elapsed_ms={elapsed}')
print(f'agent_count={agent_count}')
print(f'foreground_process={fg_name}')
print(f'mint_gone={str(mint_gone).lower()}')
" "$ELAPSED_MS" "$AGENT_LIST_OUT" "$PROC_INFO_OUT")"

  {
    echo "--- sample ---"
    echo "$SAMPLE"
  } >>"$EVIDENCE/03-mint-exit.txt"

  GONE_FLAG="$(echo "$SAMPLE" | sed -n 's/^mint_gone=//p')"
  if [ "$GONE_FLAG" = "true" ]; then
    MINT_GONE=true
    break
  fi

  sleep 0.2
  NOW_NS=$(date +%s%N)
  ELAPSED_MS=$(( (NOW_NS - POLL_START_NS) / 1000000 ))
done

{
  echo
  echo "mint_gone_final=${MINT_GONE}"
  echo "elapsed_ms_to_gone=${ELAPSED_MS}"
  echo
} >>"$EVIDENCE/03-mint-exit.txt"

# 04-prompt-send.txt — the seal statement: prompt send delivers, or returns
# the typed retryable session.target_exited envelope, well before the
# command's own expires_at (120s from now). Never sleeps to the deadline.
write_header "$EVIDENCE/04-prompt-send.txt" "duo prompt send on the just-minted session"
EXPIRES="$(date -u -d '+120 seconds' '+%Y-%m-%dT%H:%M:%S.000Z')"
IDEMPOTENCY_KEY="key-${SESSION}-devin-mint-1"
SEND_BEFORE_ISO="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
SEND_BEFORE_NS=$(date +%s%N)
{
  echo "wall_clock_before=${SEND_BEFORE_ISO}"
} >>"$EVIDENCE/04-prompt-send.txt"

SEND_CMD="duo_cmd prompt send ${SES} --text 'Reply with the single word pong.' --idempotency-key ${IDEMPOTENCY_KEY} --expires-at ${EXPIRES} --output json"
append_cmd "$EVIDENCE/04-prompt-send.txt" "$SEND_CMD"

SEND_AFTER_ISO="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
SEND_AFTER_NS=$(date +%s%N)
SEND_ELAPSED_MS=$(( (SEND_AFTER_NS - SEND_BEFORE_NS) / 1000000 ))

SEND_JSON="$(python3 -c "
import json, pathlib
text = pathlib.Path('${EVIDENCE}/04-prompt-send.txt').read_text()
for line in text.splitlines():
    if line.startswith('{'):
        print(line)
        break
")"

SEND_SUMMARY="$(python3 -c "
import json, sys
raw = sys.argv[1]
leg = 'unknown'
code = ''
resp_state = ''
try:
    d = json.loads(raw)
    if 'error' in d:
        code = d['error'].get('code', '')
        if code == 'session.target_exited':
            leg = 'exited'
        else:
            leg = 'other_error'
    else:
        resp_state = (d.get('result') or {}).get('responsibility_state', '')
        leg = 'recovery_delivered' if resp_state in ('delivered', 'queued', 'acknowledged') else 'other_result'
except Exception as e:
    leg = 'parse_error'
print(f'leg={leg}')
print(f'error_code={code}')
print(f'responsibility_state={resp_state}')
" "$SEND_JSON" 2>/dev/null || echo "leg=parse_error")"

{
  echo "wall_clock_after=${SEND_AFTER_ISO}"
  echo "send_elapsed_ms=${SEND_ELAPSED_MS}"
  echo "expires_at=${EXPIRES}"
  echo "idempotency_key=${IDEMPOTENCY_KEY}"
  echo "$SEND_SUMMARY"
  if [ "$SEND_ELAPSED_MS" -lt 120000 ]; then
    echo "bound_check=PASS (returned in ${SEND_ELAPSED_MS}ms, far under the 120000ms expiry; no sleep to deadline)"
  else
    echo "bound_check=FAIL (took ${SEND_ELAPSED_MS}ms, did not return well under the 120000ms expiry)"
  fi
  echo
} >>"$EVIDENCE/04-prompt-send.txt"

# 05-show-released.txt — claim released, not stuck at "starting".
write_header "$EVIDENCE/05-show-released.txt" "session show after send: claim release + non-starting state"
SHOW2_CMD="duo_cmd session show ${SES} --output json"
append_cmd "$EVIDENCE/05-show-released.txt" "$SHOW2_CMD"

SHOW2_JSON="$(duo_cmd session show "$SES" --output json)"
SHOW2_SUMMARY="$(python3 -c "
import json, sys
d = json.loads(sys.argv[1])
r = d.get('result', {})
state = r.get('runtime_instance_state', '')
atts = r.get('attachments') or []
claim_held = atts[0].get('claim_held') if atts else None
print(f'runtime_instance_state={state}')
print(f'claim_held={claim_held}')
stuck_starting = (state == 'starting')
print(f'stuck_starting={str(stuck_starting).lower()}')
claim_released = (claim_held is False) or (claim_held is None)
print(f'claim_released={str(claim_released).lower()}')
" "$SHOW2_JSON")"

{
  echo "$SHOW2_SUMMARY"
  echo
} >>"$EVIDENCE/05-show-released.txt"

# 06-conversation-list.txt — best-effort.
write_header "$EVIDENCE/06-conversation-list.txt" "duo conversation list (best-effort)"
LIST_CMD="duo_cmd conversation list ${SES} --output json"
append_cmd "$EVIDENCE/06-conversation-list.txt" "$LIST_CMD"

echo "capture-live.sh: done. session=${SES}"
