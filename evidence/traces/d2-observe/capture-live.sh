#!/usr/bin/env bash
set -eu

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
require_env "${RUNTIME:-}" RUNTIME
require_env "${XDG_CONFIG_HOME:-}" XDG_CONFIG_HOME
require_env "${XDG_DATA_HOME:-}" XDG_DATA_HOME
require_env "${XDG_STATE_HOME:-}" XDG_STATE_HOME
require_env "${XDG_RUNTIME_DIR:-}" XDG_RUNTIME_DIR

if [ "$RUNTIME" != "pi" ] && [ "$RUNTIME" != "claude" ]; then
  echo "RUNTIME must be pi or claude, got: $RUNTIME" >&2
  exit 2
fi

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

if ! "$DUO" version >/dev/null 2>&1; then
  echo "DUO binary failed: $DUO" >&2
  exit 2
fi

duo_cmd() { unset HERDR_SOCKET_PATH; "$DUO" "$@"; }
herdr_cmd() { env HERDR_SOCKET_PATH="$SOCK" herdr "$@"; }

R="$RUNTIME"
DUO_VERSION_LINE="$(duo_cmd version | head -1)"
NOW_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ISOLATION_LINE="isolation: HERDR_SESSION=${SESSION} scratch=${SCRATCH} host=herdr:${SOCK} runtime=${RUNTIME}"

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

path_like() {
  python3 - "$1" <<'PY'
import sys
v = sys.argv[1]
if not v:
    sys.exit(1)
if ".jsonl" in v or v.startswith("/"):
    sys.exit(0)
sys.exit(1)
PY
}

HOST_JSONL=""

# 03-version.txt
write_header "$EVIDENCE/${R}-03-version.txt" "tool versions for Stage C live gate (${R})"
append_cmd "$EVIDENCE/${R}-03-version.txt" 'duo_cmd version'
append_cmd "$EVIDENCE/${R}-03-version.txt" 'herdr --version'
if command -v pi >/dev/null 2>&1; then
  append_cmd "$EVIDENCE/${R}-03-version.txt" 'pi --version'
fi
if command -v claude >/dev/null 2>&1; then
  append_cmd "$EVIDENCE/${R}-03-version.txt" 'claude --version'
fi
append_cmd "$EVIDENCE/${R}-03-version.txt" 'git -C ~/Code/duo-vnext rev-parse HEAD'

# 04-launch.txt
write_header "$EVIDENCE/${R}-04-launch.txt" "live Herdr+${R} launch (builder --require agent_runtime=${R})"
LAUNCH_CMD="duo_cmd session launch builder --require agent_runtime=${R} --host \"herdr:${SOCK}\" --workspace ${SCRATCH}/ws --output json"
append_cmd "$EVIDENCE/${R}-04-launch.txt" "$LAUNCH_CMD"

SES="$(python3 -c "
import json, pathlib
text = pathlib.Path('${EVIDENCE}/${R}-04-launch.txt').read_text()
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

LAUNCH_EXIT="$(grep '^exit=' "$EVIDENCE/${R}-04-launch.txt" | tail -1 | cut -d= -f2)"
if [ "$LAUNCH_EXIT" != "0" ]; then
  echo "launch exit=$LAUNCH_EXIT" >&2
  exit 2
fi

# 05-show-after-launch.txt
write_header "$EVIDENCE/${R}-05-show-after-launch.txt" "session show immediately after launch return"
append_cmd "$EVIDENCE/${R}-05-show-after-launch.txt" "duo_cmd session show ${SES} --output json"

# Poll for live (05b-show-live.txt)
write_header "$EVIDENCE/${R}-05b-show-live.txt" "session show after live poll (up to 20s)"
ELAPSED_MS=0
LIVE_JSON=""
while [ "$ELAPSED_MS" -le 20000 ]; do
  LIVE_JSON="$(duo_cmd session show "$SES" --output json)"
  if python3 -c "import json,sys; d=json.loads(sys.argv[1]); print(d.get('result',{}).get('runtime_instance_state',''))" "$LIVE_JSON" | grep -qx live; then
    break
  fi
  sleep 0.2
  ELAPSED_MS=$((ELAPSED_MS + 200))
done

{
  echo "elapsed_ms=${ELAPSED_MS}"
  echo "$LIVE_JSON"
  echo
} >>"$EVIDENCE/${R}-05b-show-live.txt"

# 06-agent-list.txt
write_header "$EVIDENCE/${R}-06-agent-list.txt" "herdr agent list (${R} row fields)"
AGENT_LIST_OUT="$(herdr_cmd agent list)"
{
  echo "\$ herdr_cmd agent list"
  echo "$AGENT_LIST_OUT"
  echo "exit=0"
  echo
} >>"$EVIDENCE/${R}-06-agent-list.txt"

AGENT_FIELDS="$(python3 -c "
import json, sys
data = json.loads(sys.argv[1])
agents = data.get('result', {}).get('agents', [])
row = next((a for a in agents if a.get('agent') == sys.argv[2]), None)
if row is None:
    sys.exit(1)
sess = row.get('agent_session') or {}
print(sess.get('value', ''))
print(row.get('pane_id', ''))
print('launch_pending=' + str(row.get('launch_pending', False)))
print('agent_status=' + str(row.get('agent_status', '')))
" "$AGENT_LIST_OUT" "$R")" || {
  echo "${R} agent row not found in agent list" >&2
  exit 2
}

AGENT_SESSION_VALUE="$(echo "$AGENT_FIELDS" | sed -n '1p')"
AGENT_PANE_ID="$(echo "$AGENT_FIELDS" | sed -n '2p')"

if path_like "$AGENT_SESSION_VALUE"; then
  HOST_JSONL="$AGENT_SESSION_VALUE"
fi

{
  echo "agent_session_value=${AGENT_SESSION_VALUE}"
  echo "pane_id=${AGENT_PANE_ID}"
  echo "$AGENT_FIELDS" | sed -n '3,$p'
  echo "host_jsonl=${HOST_JSONL}"
  echo
} >>"$EVIDENCE/${R}-06-agent-list.txt"

# 08-correlations.txt
write_header "$EVIDENCE/${R}-08-correlations.txt" "store dump (correlations, commands, instance.state)"
DUMP_OUT="$(python3 "$EVIDENCE/dump-store.py" "$XDG_DATA_HOME/duo/duo.db")"
{
  echo "\$ python3 ${EVIDENCE}/dump-store.py ${XDG_DATA_HOME}/duo/duo.db"
  echo "$DUMP_OUT"
  echo "exit=0"
  echo
} >>"$EVIDENCE/${R}-08-correlations.txt"

TRANSCRIPT_FROM_DUMP="$(python3 -c "
import json, sys
text = sys.stdin.read()
in_corr = False
path = ''
for line in text.splitlines():
    if line == '## correlations':
        in_corr = True
        continue
    if line.startswith('## '):
        in_corr = False
    if not in_corr or not line.strip():
        continue
    row = json.loads(line)
    corr = row.get('correlation') or {}
    if corr.get('external_kind') == 'transcript':
        val = corr.get('external_value') or corr.get('value') or ''
        if '.jsonl' in val or val.startswith('/'):
            path = val
print(path)
" <<<"$DUMP_OUT")"

if [ -z "$HOST_JSONL" ] && [ -n "$TRANSCRIPT_FROM_DUMP" ]; then
  HOST_JSONL="$TRANSCRIPT_FROM_DUMP"
  {
    echo "host_jsonl_from_transcript_correlation=${HOST_JSONL}"
    echo
  } >>"$EVIDENCE/${R}-08-correlations.txt"
fi

if [ "$RUNTIME" = "pi" ]; then
  CLASSIFY="$(python3 -c "
import json, re, sys
text = sys.stdin.read()
in_corr = False
val = ''
for line in text.splitlines():
    if line == '## correlations':
        in_corr = True
        continue
    if line.startswith('## '):
        in_corr = False
    if not in_corr or not line.strip():
        continue
    row = json.loads(line)
    corr = row.get('correlation') or {}
    if corr.get('external_kind') == 'agent.session':
        val = corr.get('external_value') or corr.get('value') or ''
        break
if not val:
    print('agent.session_classification=other (missing)')
elif '.jsonl' in val or val.startswith('/'):
    print('agent.session_classification=path')
elif re.fullmatch(r'[0-9a-fA-F-]{36}', val):
    print('agent.session_classification=uuid')
else:
    print('agent.session_classification=other')
print(f'agent.session_value={val}')
" <<<"$DUMP_OUT")"
  {
    echo "$CLASSIFY"
    echo
  } >>"$EVIDENCE/${R}-08-correlations.txt"
fi

# 09-prompt-send.txt
write_header "$EVIDENCE/${R}-09-prompt-send.txt" "duo prompt send"
EXPIRES="$(date -u -d '+120 seconds' '+%Y-%m-%dT%H:%M:%S.000Z')"
SEND_CMD="duo_cmd prompt send ${SES} --text 'Reply with the single word pong.' --idempotency-key key-${SESSION}-${R}-1 --expires-at ${EXPIRES} --output json"
append_cmd "$EVIDENCE/${R}-09-prompt-send.txt" "$SEND_CMD"

# 15-observe-poll.txt
write_header "$EVIDENCE/${R}-15-observe-poll.txt" "observe poll after prompt send (up to 60s)"

FIRST_BYTES_MS=none
FIRST_IDLE_OR_WORKING_MS=none
FIRST_LIST_SUCCESS_MS=none
OUTCOME=TIMEOUT
POLL_ELAPSED_MS=0

while [ "$POLL_ELAPSED_MS" -le 60000 ]; do
  SHOW_JSON="$(duo_cmd session show "$SES" --output json)"
  set +e
  LIST_JSON="$(duo_cmd conversation list "$SES" --output json 2>&1)"
  LIST_EXIT=$?
  set -e

  POLL_RESULT="$(python3 - "$SHOW_JSON" "$LIST_JSON" "$LIST_EXIT" "$POLL_ELAPSED_MS" "$HOST_JSONL" <<'PY'
import json
import os
import sys

show = json.loads(sys.argv[1])
list_raw = sys.argv[2]
list_exit = int(sys.argv[3])
elapsed_ms = int(sys.argv[4])
host_jsonl = sys.argv[5]

result = show.get("result") or {}
state = result.get("runtime_instance_state", "")
view = result.get("view", "")
condition = result.get("condition") or {}
cond_val = condition.get("value", "")
reasons = condition.get("reasons") or []
if isinstance(reasons, str):
    reasons = [reasons]
reasons_joined = ";".join(str(r) for r in reasons)

host_exists = bool(host_jsonl) and os.path.isfile(host_jsonl)
host_bytes = os.path.getsize(host_jsonl) if host_exists else 0
host_lines = 0
if host_exists:
    try:
        with open(host_jsonl, "r", encoding="utf-8") as fh:
            host_lines = sum(1 for line in fh if line.strip())
    except OSError:
        host_lines = 0
host_has_turns = host_lines >= 2

def item_text(item):
    chunks = []
    for k in ("text", "body", "message"):
        chunks.append(str(item.get(k) or ""))
    content = item.get("content")
    if isinstance(content, str):
        chunks.append(content)
    elif isinstance(content, dict):
        chunks.append(str(content.get("text") or ""))
    for block in item.get("blocks") or []:
        if not isinstance(block, dict):
            continue
        bcontent = block.get("content")
        if isinstance(bcontent, dict):
            chunks.append(str(bcontent.get("text") or ""))
        elif isinstance(bcontent, str):
            chunks.append(bcontent)
        chunks.append(str(block.get("text") or ""))
    return " ".join(chunks)

list_items = 0
list_has_match = False
if list_exit == 0:
    try:
        list_data = json.loads(list_raw)
        items = (list_data.get("result") or {}).get("items") or []
        list_items = len(items)
        for item in items:
            tl = item_text(item).lower()
            if "pong" in tl or "reply with the single word pong." in tl:
                list_has_match = True
                break
    except json.JSONDecodeError:
        pass

has_condition = "condition" in result

print(f"elapsed_ms={elapsed_ms}")
print(f"state={state}")
print(f"view={view}")
print(f"condition.value={cond_val}")
print(f"condition.reasons={reasons_joined}")
print(f"list_exit={list_exit}")
print(f"list_items={list_items}")
print(f"host_jsonl_exists={str(host_exists).lower()}")
print(f"host_jsonl_bytes={host_bytes}")

action = "CONTINUE"
if state == "live":
    if not has_condition:
        action = "FAIL"
    elif any("transcript header does not match session" in str(r) for r in reasons):
        action = "FAIL"
    elif (
        any("missing transcript" in str(r) for r in reasons)
        and host_exists
        and host_has_turns
    ):
        action = "FAIL"
    elif (
        cond_val in ("idle", "working")
        and not any("transcript header does not match session" in str(r) for r in reasons)
        and not any("missing transcript" in str(r) for r in reasons)
        and list_exit == 0
        and list_has_match
        and has_condition
    ):
        action = "PASS"

print(f"action={action}")
print(f"bytes_positive={str(host_bytes > 0).lower()}")
print(f"idle_or_working={str(cond_val in ('idle', 'working')).lower()}")
print(f"list_success={str(list_exit == 0).lower()}")
PY
)"

  {
    echo "--- sample ---"
    echo "$POLL_RESULT"
    echo
  } >>"$EVIDENCE/${R}-15-observe-poll.txt"

  BYTES_POSITIVE="$(echo "$POLL_RESULT" | sed -n 's/^bytes_positive=//p' | tail -1)"
  IDLE_OR_WORKING="$(echo "$POLL_RESULT" | sed -n 's/^idle_or_working=//p' | tail -1)"
  LIST_SUCCESS="$(echo "$POLL_RESULT" | sed -n 's/^list_success=//p' | tail -1)"
  ACTION="$(echo "$POLL_RESULT" | sed -n 's/^action=//p' | tail -1)"

  if [ "$BYTES_POSITIVE" = "true" ] && [ "$FIRST_BYTES_MS" = "none" ]; then
    FIRST_BYTES_MS="$POLL_ELAPSED_MS"
  fi
  if [ "$IDLE_OR_WORKING" = "true" ] && [ "$FIRST_IDLE_OR_WORKING_MS" = "none" ]; then
    FIRST_IDLE_OR_WORKING_MS="$POLL_ELAPSED_MS"
  fi
  if [ "$LIST_SUCCESS" = "true" ] && [ "$FIRST_LIST_SUCCESS_MS" = "none" ]; then
    FIRST_LIST_SUCCESS_MS="$POLL_ELAPSED_MS"
  fi

  if [ "$ACTION" = "PASS" ]; then
    OUTCOME=PASS
    break
  fi
  if [ "$ACTION" = "FAIL" ]; then
    OUTCOME=FAIL
    break
  fi

  sleep 0.2
  POLL_ELAPSED_MS=$((POLL_ELAPSED_MS + 200))
done

{
  echo "first_bytes_ms=${FIRST_BYTES_MS}"
  echo "first_idle_or_working_ms=${FIRST_IDLE_OR_WORKING_MS}"
  echo "first_list_success_ms=${FIRST_LIST_SUCCESS_MS}"
  echo "outcome=${OUTCOME}"
  echo
} >>"$EVIDENCE/${R}-15-observe-poll.txt"

# 16-jsonl-excerpt.txt
write_header "$EVIDENCE/${R}-16-jsonl-excerpt.txt" "host transcript excerpt"
{
  echo "host_jsonl=${HOST_JSONL}"
  echo
  if [ -n "$HOST_JSONL" ] && [ -f "$HOST_JSONL" ]; then
    echo "\$ head -20 ${HOST_JSONL}"
    head -20 "$HOST_JSONL"
    echo "exit=0"
    echo
    echo "\$ tail -5 ${HOST_JSONL}"
    tail -5 "$HOST_JSONL"
    echo "exit=0"
  else
    echo "host JSONL missing or path unknown"
  fi
  echo
} >>"$EVIDENCE/${R}-16-jsonl-excerpt.txt"

# 17-show-final.txt
write_header "$EVIDENCE/${R}-17-show-final.txt" "session show final"
append_cmd "$EVIDENCE/${R}-17-show-final.txt" "duo_cmd session show ${SES} --output json"

# 18-list-final.txt
write_header "$EVIDENCE/${R}-18-list-final.txt" "conversation list final"
append_cmd "$EVIDENCE/${R}-18-list-final.txt" "duo_cmd conversation list ${SES} --output json"

if [ "$OUTCOME" = "PASS" ]; then
  exit 0
fi
exit 2
