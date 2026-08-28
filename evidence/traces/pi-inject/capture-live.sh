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

if ! "$DUO" version >/dev/null 2>&1; then
  echo "DUO binary failed: $DUO" >&2
  exit 2
fi

duo_cmd() { unset HERDR_SOCKET_PATH; "$DUO" "$@"; }
herdr_cmd() { env HERDR_SOCKET_PATH="$SOCK" herdr "$@"; }

DUO_VERSION_LINE="$(duo_cmd version | head -1)"
NOW_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ISOLATION_LINE="isolation: HERDR_SESSION=${SESSION} scratch=${SCRATCH} host=herdr:${SOCK}"

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

session_id_from_transcript_name() {
  python3 - "$1" <<'PY'
import os
import sys

path = sys.argv[1]
base = os.path.basename(path)
if not base.endswith(".jsonl"):
    sys.exit(0)
base = base.removesuffix(".jsonl") if hasattr(str, "removesuffix") else (base[:-6] if base.endswith(".jsonl") else base)
i = base.rfind("_")
if i < 0 or i == len(base) - 1:
    sys.exit(0)
print(base[i + 1:])
PY
}

# 03-duo-version.txt
write_header "$EVIDENCE/03-duo-version.txt" "tool versions for Stage C live gate"
append_cmd "$EVIDENCE/03-duo-version.txt" 'duo_cmd version'
append_cmd "$EVIDENCE/03-duo-version.txt" 'herdr --version'
append_cmd "$EVIDENCE/03-duo-version.txt" 'pi --version'
append_cmd "$EVIDENCE/03-duo-version.txt" 'git -C ~/Code/duo rev-parse HEAD'

# 04-launch-pi.txt
write_header "$EVIDENCE/04-launch-pi.txt" "live Herdr+Pi launch (builder --require agent_runtime=pi)"
LAUNCH_CMD="duo_cmd session launch builder --require agent_runtime=pi --host herdr:${SOCK} --workspace ${SCRATCH}/ws --output json"
append_cmd "$EVIDENCE/04-launch-pi.txt" "$LAUNCH_CMD"

SES="$(python3 -c "
import json, pathlib
text = pathlib.Path('${EVIDENCE}/04-launch-pi.txt').read_text()
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

LAUNCH_EXIT="$(grep '^exit=' "$EVIDENCE/04-launch-pi.txt" | tail -1 | cut -d= -f2)"
if [ "$LAUNCH_EXIT" != "0" ]; then
  echo "launch exit=$LAUNCH_EXIT" >&2
  exit 2
fi

# 05-session-show-after-launch.txt
write_header "$EVIDENCE/05-session-show-after-launch.txt" "session show immediately after launch return"
append_cmd "$EVIDENCE/05-session-show-after-launch.txt" "duo_cmd session show ${SES} --output json"

# Poll for live (05b-session-show-live.txt)
write_header "$EVIDENCE/05b-session-show-live.txt" "session show after live poll (up to 20s)"
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
} >>"$EVIDENCE/05b-session-show-live.txt"

# 06-herdr-agent-list.txt
write_header "$EVIDENCE/06-herdr-agent-list.txt" "herdr agent list (Pi row fields)"
AGENT_LIST_OUT="$(herdr_cmd agent list)"
{
  echo "\$ herdr_cmd agent list"
  echo "$AGENT_LIST_OUT"
  echo "exit=0"
  echo
} >>"$EVIDENCE/06-herdr-agent-list.txt"

PI_FIELDS="$(python3 -c "
import json, sys
data = json.loads(sys.argv[1])
agents = data.get('result', {}).get('agents', [])
pi = next((a for a in agents if a.get('agent') == 'pi'), None)
if pi is None:
    sys.exit(1)
sess = pi.get('agent_session') or {}
print(sess.get('value', ''))
print(pi.get('pane_id', ''))
print('launch_pending=' + str(pi.get('launch_pending', False)))
print('agent_status=' + str(pi.get('agent_status', '')))
print('screen_detection_skipped=' + str(pi.get('screen_detection_skipped', False)))
" "$AGENT_LIST_OUT")" || {
  echo "Pi agent row not found in agent list" >&2
  exit 2
}

PI_TRANSCRIPT_PATH="$(echo "$PI_FIELDS" | sed -n '1p')"
PI_PANE_ID="$(echo "$PI_FIELDS" | sed -n '2p')"

{
  echo "pi_transcript_path=${PI_TRANSCRIPT_PATH}"
  echo "pi_pane_id=${PI_PANE_ID}"
  echo "$PI_FIELDS" | sed -n '3,$p'
  echo
} >>"$EVIDENCE/06-herdr-agent-list.txt"

# 07-inject-socket.txt
write_header "$EVIDENCE/07-inject-socket.txt" "inject socket under XDG_RUNTIME_DIR"
UUID="$(session_id_from_transcript_name "$PI_TRANSCRIPT_PATH")"
INJECT_SOCK="${XDG_RUNTIME_DIR}/duo/pi-inject/${UUID}.sock"
{
  echo "uuid_from_transcript=${UUID}"
  echo "inject_sock=${INJECT_SOCK}"
  echo
  if [ -n "$UUID" ]; then
  echo "\$ ls -l ${INJECT_SOCK}"
  ls -l "$INJECT_SOCK" 2>&1 || true
  echo "exit=$?"
  echo
  echo "\$ stat ${INJECT_SOCK}"
  stat "$INJECT_SOCK" 2>&1 || true
  echo "exit=$?"
  else
    echo "no uuid from transcript path; socket path not derived"
  fi
  echo
} >>"$EVIDENCE/07-inject-socket.txt"

# 08-correlations.txt
write_header "$EVIDENCE/08-correlations.txt" "store dump (correlations, commands, instance.state)"
{
  echo "\$ python3 ${EVIDENCE}/dump-store.py ${XDG_DATA_HOME}/duo/duo.db"
  python3 "$EVIDENCE/dump-store.py" "$XDG_DATA_HOME/duo/duo.db"
  echo "exit=0"
  echo
} >>"$EVIDENCE/08-correlations.txt"

# 09-prompt-send.txt
write_header "$EVIDENCE/09-prompt-send.txt" "duo prompt send (runtime inject path)"
EXPIRES="$(date -u -d '+120 seconds' '+%Y-%m-%dT%H:%M:%S.000Z')"
SEND_CMD="duo_cmd prompt send ${SES} --text 'Reply with the single word pong.' --idempotency-key key-${SESSION}-pi-1 --expires-at ${EXPIRES} --output json"
append_cmd "$EVIDENCE/09-prompt-send.txt" "$SEND_CMD"

CMD="$(python3 -c "
import json, pathlib
text = pathlib.Path('${EVIDENCE}/09-prompt-send.txt').read_text()
for line in text.splitlines():
    if line.startswith('{'):
        data = json.loads(line)
        print((data.get('result') or {}).get('command_id', ''))
        break
" || true)"

sleep 8

# 10-prompt-show.txt
write_header "$EVIDENCE/10-prompt-show.txt" "duo prompt show after send"
if [ -n "$CMD" ]; then
  append_cmd "$EVIDENCE/10-prompt-show.txt" "duo_cmd prompt show ${CMD} --output json"
else
  echo "command_id missing from send JSON; prompt show skipped" >>"$EVIDENCE/10-prompt-show.txt"
fi

# 11-store-path-kind.txt
write_header "$EVIDENCE/11-store-path-kind.txt" "store commands section (path_kind runtime vs host)"
DUMP_OUT="$(python3 "$EVIDENCE/dump-store.py" "$XDG_DATA_HOME/duo/duo.db")"
{
  echo "\$ python3 ${EVIDENCE}/dump-store.py ${XDG_DATA_HOME}/duo/duo.db (commands section)"
  echo "$DUMP_OUT" | awk '/^## commands$/{show=1; next} /^## /{show=0} show'
  echo
  python3 -c "
import json, sys
text = sys.stdin.read()
in_cmds = False
for line in text.splitlines():
    if line == '## commands':
        in_cmds = True
        continue
    if line.startswith('## '):
        in_cmds = False
    if not in_cmds or not line.strip():
        continue
    row = json.loads(line)
    cmd = row.get('command') or {}
    attempt = cmd.get('attempt') or {}
    pk = attempt.get('path_kind')
    if pk:
        print(f'attempt path_kind={pk} kind={row.get(\"kind\")} recorded_result={attempt.get(\"recorded_result\")}')
" <<<"$DUMP_OUT"
  echo
} >>"$EVIDENCE/11-store-path-kind.txt"

# 12-pi-jsonl-excerpt.txt (host path only, I-6)
write_header "$EVIDENCE/12-pi-jsonl-excerpt.txt" "pi transcript excerpt (host-named path only)"
{
  echo "transcript_path=${PI_TRANSCRIPT_PATH}"
  echo
  if [ -n "$PI_TRANSCRIPT_PATH" ] && [ -f "$PI_TRANSCRIPT_PATH" ]; then
    echo "\$ head -20 ${PI_TRANSCRIPT_PATH}"
    head -20 "$PI_TRANSCRIPT_PATH"
    echo "exit=0"
    echo
    echo "\$ tail -5 ${PI_TRANSCRIPT_PATH}"
    tail -5 "$PI_TRANSCRIPT_PATH"
    echo "exit=0"
  else
    echo "transcript file missing at host path"
  fi
  echo
} >>"$EVIDENCE/12-pi-jsonl-excerpt.txt"

# 13-pane-read.txt
write_header "$EVIDENCE/13-pane-read.txt" "herdr pane read (prompt text + extensions)"
if [ -n "$PI_PANE_ID" ]; then
  append_cmd "$EVIDENCE/13-pane-read.txt" "herdr_cmd pane read ${PI_PANE_ID}"
else
  echo "pi_pane_id missing; pane read skipped" >>"$EVIDENCE/13-pane-read.txt"
fi

# 14-herdr-launch-pending-remainder.txt
write_header "$EVIDENCE/14-herdr-launch-pending-remainder.txt" "herdr launch_pending remainder after deliver"
AGENT_LIST_AFTER="$(herdr_cmd agent list)"
{
  echo "\$ herdr_cmd agent list"
  echo "$AGENT_LIST_AFTER"
  echo "exit=0"
  echo
  python3 -c "
import json, sys
data = json.loads(sys.argv[1])
agents = data.get('result', {}).get('agents', [])
pi = next((a for a in agents if a.get('agent') == 'pi'), None)
if pi is None:
    print('pi_row=missing')
else:
    print('launch_pending=' + str(pi.get('launch_pending', False)))
    print('agent_status=' + str(pi.get('agent_status', '')))
    print('screen_detection_skipped=' + str(pi.get('screen_detection_skipped', False)))
" "$AGENT_LIST_AFTER"
  echo "Herdr still reporting launch_pending after deliver is the expected named remainder, not a fail."
  echo
} >>"$EVIDENCE/14-herdr-launch-pending-remainder.txt"
