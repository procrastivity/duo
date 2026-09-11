#!/usr/bin/env bash
set -eu

# Live seal evidence for the Amp exclusive-writer change:
#
#   Duo is the only writer on a Duo-owned Amp thread, per turn, not per
#   session (docs/adapters/decisions.md, 2026-09-09, "Amp exclusive-writer
#   scope is per-turn, not per-session"). A `duo session launch` against
#   an Amp leaf materializes a Duo-owned mint wrapper script
#   (internal/runtime/amp/materialize.go) that pipes the mint prompt into
#   `amp -x --no-archive-after-execute --settings-file <path>
#   --stream-json` and tees the stream-JSON to a mint log
#   (internal/runtime/amp/mintlog.go). Duo observes that spawned process's
#   exit inside the shared identity wait (the same continuity probe the
#   Devin print-mint pair proved: 1s cadence, 2s per-probe timeout,
#   pane_absent confirms immediately) and, for Amp, recovers the thread id
#   from the mint log's own session_id/result-success record
#   (internal/cli/identity_bind.go's per-runtime mintRecoveries table),
#   binding it as the session's agent-session identity and releasing the
#   launch claim. `duo prompt send` then delivers per turn via `amp
#   threads continue <tid> -x --stream-json`, one spawned process per
#   turn — the server-side single-executor lock is held only for that
#   process's lifetime, not across the whole session. A second writer
#   (an interactive Amp TUI attached to the same thread) holding that
#   lock when Duo's spawn tries to connect is refused server-side,
#   first-connect-wins, no queue; Duo maps that refusal to a typed
#   `operation.temporarily_unavailable` envelope
#   (effect:"unknown_effect", retry.action:"retry_after_holder_release")
#   read from the thread's own debug log, never from generic CLI stderr
#   text alone (internal/runtime/amp/prompt.go).
#
# This script only captures. It does not provision or tear down the
# disposable Herdr session, and it does not write or copy Amp
# credentials/settings — see run-live.sh for all of that. Every `amp`
# invocation below (the mint, triggered indirectly through `duo session
# launch`; the collision TUI holder; `threads export`/`threads archive`)
# is a real call against the operator's Amp account and bills it — this
# script is authored to be run only by the operator (step-07), never
# executed by the agent that authored it.
#
# Observation only: this script never sends a signal to any Amp process
# it did not itself spawn as a disposable holder, and even that holder is
# only ever stopped through Herdr's own pane control (`herdr pane close`,
# a Duo/Herdr-level teardown action, not a signal aimed at the runtime) —
# never `kill`, never an interrupt keystroke sent to influence Amp's own
# turn execution.

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
require_env "${AMP_SETTINGS_PATH:-}" AMP_SETTINGS_PATH

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

if ! command -v amp >/dev/null 2>&1; then
  echo "amp not found in PATH" >&2
  exit 2
fi

if ! "$DUO" version >/dev/null 2>&1; then
  echo "DUO binary failed: $DUO" >&2
  exit 2
fi

duo_cmd() { unset HERDR_SOCKET_PATH; "$DUO" "$@"; }
herdr_cmd() { env HERDR_SOCKET_PATH="$SOCK" herdr "$@"; }
amp_cmd() { amp --settings-file "$AMP_SETTINGS_PATH" "$@"; }

DUO_VERSION_LINE="$(duo_cmd version | head -1)"
NOW_ISO="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ISOLATION_LINE="isolation: HERDR_SESSION=${SESSION} scratch=${SCRATCH} host=herdr:${SOCK} runtime=amp settings=${AMP_SETTINGS_PATH}"

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

# append_cmd merges stderr into the same capture block (unlike the Devin
# pair's append_cmd, which never needed to): --output json failures
# (session.target_exited-shaped envelopes, and here the
# operation.temporarily_unavailable collision envelope) are written to
# stderr by internal/cli/prompt.go's writePromptFailure, never stdout, so
# a capture that only redirected stdout would silently lose exactly the
# evidence a collision capture exists to record. Merging is a no-op for
# every successful call (nothing here writes to stderr on success).
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

# ---------------------------------------------------------------------------
# 01-launch.txt — duo session launch against the amp_mint preset
# (agent_runtime=amp; run-live.sh's isolated duo.config.yaml declares the
# amp leaf's executable as "bash" so the appended mint-wrapper script path
# runs, per stage1LeafAugmenter's doc comment, internal/cli/
# session_launch.go). PASS when the mint log the wrapper tees to exists at
# its computed path and carries a thread id: a session_id line plus a
# result/success line (mirrors amp.ThreadIDFromMintLog's own two-part
# proof, internal/runtime/amp/mintlog.go — a session_id line alone proves
# only that Amp opened a thread, not that the mint prompt was answered).
# ---------------------------------------------------------------------------
write_header "$EVIDENCE/01-launch.txt" "live Herdr+amp mint launch (amp_mint --require agent_runtime=amp)"
LAUNCH_CMD="duo_cmd session launch amp_mint --require agent_runtime=amp --host \"herdr:${SOCK}\" --workspace ${SCRATCH}/ws --output json"
append_cmd "$EVIDENCE/01-launch.txt" "$LAUNCH_CMD"

LAUNCH_EXIT="$(grep '^exit=' "$EVIDENCE/01-launch.txt" | tail -1 | cut -d= -f2)"
if [ "$LAUNCH_EXIT" != "0" ]; then
  echo "launch exit=$LAUNCH_EXIT" >&2
  exit 2
fi

LAUNCH_FIELDS="$(python3 -c "
import json, pathlib
text = pathlib.Path('${EVIDENCE}/01-launch.txt').read_text()
for line in text.splitlines():
    if line.startswith('{'):
        data = json.loads(line)
        r = data['result']
        print(r['session_id'])
        print(r.get('launch_resolution_id', ''))
        break
")"
SES="$(echo "$LAUNCH_FIELDS" | sed -n 1p)"
LRID="$(echo "$LAUNCH_FIELDS" | sed -n 2p)"

if [ -z "$SES" ] || [ -z "$LRID" ]; then
  echo "launch did not yield both session_id and launch_resolution_id" >&2
  exit 2
fi

echo "session_id=${SES}" >>"$EVIDENCE/01-launch.txt"
echo "launch_resolution_id=${LRID}" >>"$EVIDENCE/01-launch.txt"

# The mint log's computed locator: $XDG_DATA_HOME/duo/amp-mint/<launch-
# resolution-id>/<leaf>.jsonl (amp.MintLogPath). "main" is the amp_mint
# preset's only leaf, matching run-live.sh's isolated duo.config.yaml —
# a fixed value, not discovered, mirroring MintLogPath's own contract
# (locator, never a directory-newest scan, I-6).
MINT_LEAF="main"
MINT_LOG="${XDG_DATA_HOME}/duo/amp-mint/${LRID}/${MINT_LEAF}.jsonl"
echo "mint_log_path=${MINT_LOG}" >>"$EVIDENCE/01-launch.txt"

# Observation only: poll the mint log file for a session_id line plus a
# result/success line, up to 30s. By the time `duo session launch`
# returned above, its own shared identity wait (bindStartingIdentity)
# will normally have already observed the mint process exit and read this
# same file back — this poll only covers the rare case where the identity
# wait's own deadline fired first, and reads a local file Duo's wrapper
# script already tees to; it never touches amp or the network itself.
POLL_START_NS=$(date +%s%N)
ELAPSED_MS=0
MINT_TID=""
MINT_SUCCESS=false
while [ "$ELAPSED_MS" -le 30000 ]; do
  MINT_SAMPLE="$(python3 -c "
import json

path = '${MINT_LOG}'
thread_id = ''
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
            sid = rec.get('session_id', '')
            if sid and not thread_id:
                thread_id = sid
            if rec.get('type') == 'result' and rec.get('subtype') == 'success':
                success = True
except FileNotFoundError:
    pass
print(f'thread_id={thread_id}')
print(f'mint_success={str(success).lower()}')
")"
  MINT_TID="$(echo "$MINT_SAMPLE" | sed -n 's/^thread_id=//p')"
  MINT_SUCCESS_FLAG="$(echo "$MINT_SAMPLE" | sed -n 's/^mint_success=//p')"
  if [ -n "$MINT_TID" ] && [ "$MINT_SUCCESS_FLAG" = "true" ]; then
    MINT_SUCCESS=true
    break
  fi
  sleep 0.5
  NOW_NS=$(date +%s%N)
  ELAPSED_MS=$(( (NOW_NS - POLL_START_NS) / 1000000 ))
done

{
  echo "elapsed_ms_to_mint_log_complete=${ELAPSED_MS}"
  echo "thread_id=${MINT_TID}"
  echo "mint_success=${MINT_SUCCESS}"
  if [ -n "$MINT_TID" ] && [ "$MINT_SUCCESS" = "true" ]; then
    echo "bound_check=PASS (mint log names a thread id and closed with a successful result record)"
  else
    echo "bound_check=FAIL (mint log at ${MINT_LOG} never named a thread id with a successful result record within 30s)"
  fi
  echo
} >>"$EVIDENCE/01-launch.txt"

if [ -z "$MINT_TID" ] || [ "$MINT_SUCCESS" != "true" ]; then
  echo "capture-live.sh: mint did not complete; aborting before any further amp calls" >&2
  exit 2
fi

TID="$MINT_TID"
echo "$TID" >"$SCRATCH/thread-id.txt"

# ---------------------------------------------------------------------------
# 02-show-bound.txt — duo session show right after launch. Two states are
# in-bound here, mirroring devin-mint-claim-release's 02-show-starting.txt
# (which notes either outcome and fails on neither):
#
#   - "live" with claim_held=false: the launch's own identity wait
#     (bindStartingIdentity) observed the mint exit and recovered the
#     thread id in-window (internal/cli/identity_bind.go's
#     handleMintExit/commitIdentityBind).
#   - "starting" with claim_held=true: caught mid-flight. The launch wait's
#     deadline (identityBindTimeout, 8s) can fire before the exit is
#     observable — the continuity probe only runs once Herdr's durable
#     agent deregistration (foreground loss) has removed the AgentOnPane
#     row, and a real `amp -x` mint turn plus that deregistration lag can
#     outlast 8s. The same shared identity wait re-runs inside `duo prompt
#     send` with the command's own expires_at as its deadline
#     (waitPromptIdentity), performs the identical recovery, and 03's
#     post-send show below is the hard proof of live/claim-released.
#
# Anything else — notably "exited" (handleMintExit's generic leg ran:
# recovery found no thread id and Authority.Exit released the claims) —
# fails the capture: no send can recover an exited instance.
# ---------------------------------------------------------------------------
write_header "$EVIDENCE/02-show-bound.txt" "session show after launch: amp thread bound as agent session, claim released"
SHOW1_CMD="duo_cmd session show ${SES} --output json"
append_cmd "$EVIDENCE/02-show-bound.txt" "$SHOW1_CMD"

SHOW1_JSON="$(duo_cmd session show "$SES" --output json)"
SHOW1_SUMMARY="$(python3 -c "
import json, sys
d = json.loads(sys.argv[1])
r = d.get('result', {})
state = r.get('runtime_instance_state', '')
atts = r.get('attachments') or []
claim_held = atts[0].get('claim_held') if atts else None
ops = r.get('operations') or []
prompt_deliver_availability = ''
for op in ops:
    if op.get('operation') == 'prompt.deliver':
        prompt_deliver_availability = op.get('availability', '')
print(f'runtime_instance_state={state}')
print(f'claim_held={claim_held}')
print(f'prompt_deliver_availability={prompt_deliver_availability}')
" "$SHOW1_JSON")"

{
  echo "$SHOW1_SUMMARY"
  echo
} >>"$EVIDENCE/02-show-bound.txt"

SHOW1_STATE="$(echo "$SHOW1_SUMMARY" | sed -n 's/^runtime_instance_state=//p')"
SHOW1_CLAIM="$(echo "$SHOW1_SUMMARY" | sed -n 's/^claim_held=//p')"
SHOW1_AVAIL="$(echo "$SHOW1_SUMMARY" | sed -n 's/^prompt_deliver_availability=//p')"

BOUND_LIVE=false
[ "$SHOW1_STATE" = "live" ] && BOUND_LIVE=true
CLAIM_RELEASED=false
[ "$SHOW1_CLAIM" = "False" ] || [ "$SHOW1_CLAIM" = "None" ] && CLAIM_RELEASED=true

MID_FLIGHT=false
if [ "$SHOW1_STATE" = "starting" ] && [ "$SHOW1_CLAIM" = "True" ]; then
  MID_FLIGHT=true
fi

{
  if [ "$BOUND_LIVE" = "true" ] && [ "$CLAIM_RELEASED" = "true" ]; then
    echo "bound_check=PASS (runtime_instance_state=live — the amp thread id is bound as the agent session — claim_held=false, prompt_deliver_availability=${SHOW1_AVAIL})"
  elif [ "$MID_FLIGHT" = "true" ]; then
    echo "bound_check=MID-FLIGHT (runtime_instance_state=starting, claim_held=True — the launch wait's 8s deadline fired before the mint exit was observable; in-bound, matching devin-mint-claim-release's 02. The send path's shared identity wait performs the recovery; 03's post-send show is the hard live/claim-released proof)"
  else
    echo "bound_check=FAIL (runtime_instance_state=${SHOW1_STATE}, claim_held=${SHOW1_CLAIM} — neither live/released nor mid-flight starting/held; an exited instance cannot be recovered by the send path)"
  fi
  echo
} >>"$EVIDENCE/02-show-bound.txt"

if [ "$MID_FLIGHT" != "true" ] && { [ "$BOUND_LIVE" != "true" ] || [ "$CLAIM_RELEASED" != "true" ]; }; then
  echo "capture-live.sh: session neither bound live nor mid-flight after mint; aborting before any prompt send" >&2
  exit 2
fi

# ---------------------------------------------------------------------------
# 03-instruct.txt — duo prompt send delivers one real turn via `amp
# threads continue <tid> -x --stream-json` (internal/runtime/amp/
# prompt.go's DeliverPrompt). PASS when the command reports
# responsibility_state=delivered, well under the command's own
# expires_at (same bound-check shape as devin-mint-claim-release's
# 04-prompt-send.txt), AND a follow-up `amp threads export <tid>` (a
# billed read; noted here rather than skipped) shows the turn landed —
# the mint alone leaves a 2-message thread (mint prompt + reply,
# notes/61 §1 export shape), so a successful instruct turn should bring
# it to 4. notes/63's own acquisition recipe documents export sync lag
# (~3s) after a `continue -x` return, so this retries the export rather
# than trusting one immediate read.
# ---------------------------------------------------------------------------
write_header "$EVIDENCE/03-instruct.txt" "duo prompt send on the bound amp session (real turn)"
EXPIRES="$(date -u -d '+120 seconds' '+%Y-%m-%dT%H:%M:%S.000Z')"
IDEMPOTENCY_KEY="key-${SESSION}-amp-instruct-1"
SEND_BEFORE_ISO="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
SEND_BEFORE_NS=$(date +%s%N)
{
  echo "wall_clock_before=${SEND_BEFORE_ISO}"
} >>"$EVIDENCE/03-instruct.txt"

SEND_CMD="duo_cmd prompt send ${SES} --text 'Reply with the single word pong.' --idempotency-key ${IDEMPOTENCY_KEY} --expires-at ${EXPIRES} --output json"
append_cmd "$EVIDENCE/03-instruct.txt" "$SEND_CMD"

SEND_AFTER_ISO="$(date -u +%Y-%m-%dT%H:%M:%S.%3NZ)"
SEND_AFTER_NS=$(date +%s%N)
SEND_ELAPSED_MS=$(( (SEND_AFTER_NS - SEND_BEFORE_NS) / 1000000 ))

SEND_JSON="$(python3 -c "
import json, pathlib
text = pathlib.Path('${EVIDENCE}/03-instruct.txt').read_text()
for line in text.splitlines():
    if line.startswith('{'):
        print(line)
        break
")"

SEND_SUMMARY="$(python3 -c "
import json, sys
raw = sys.argv[1]
leg = 'unknown'
resp_state = ''
try:
    d = json.loads(raw)
    resp_state = (d.get('result') or {}).get('responsibility_state', '')
    leg = 'delivered' if resp_state == 'delivered' else 'other_result'
except Exception:
    leg = 'parse_error'
print(f'leg={leg}')
print(f'responsibility_state={resp_state}')
" "$SEND_JSON" 2>/dev/null || echo "leg=parse_error")"

{
  echo "wall_clock_after=${SEND_AFTER_ISO}"
  echo "send_elapsed_ms=${SEND_ELAPSED_MS}"
  echo "expires_at=${EXPIRES}"
  echo "idempotency_key=${IDEMPOTENCY_KEY}"
  echo "$SEND_SUMMARY"
  if [ "$SEND_ELAPSED_MS" -lt 120000 ]; then
    echo "elapsed_bound_check=PASS (returned in ${SEND_ELAPSED_MS}ms, far under the 120000ms expiry; no sleep to deadline)"
  else
    echo "elapsed_bound_check=FAIL (took ${SEND_ELAPSED_MS}ms, did not return well under the 120000ms expiry)"
  fi
  echo
} >>"$EVIDENCE/03-instruct.txt"

SEND_LEG="$(echo "$SEND_SUMMARY" | sed -n 's/^leg=//p')"

# Billed read: `amp threads export <tid>` — retried for the documented
# server sync lag (notes/63 §1: an export taken immediately after a
# `continue -x`'s own result/success line can still be missing the
# newest message; converges within a few seconds).
EXPORT_CMD="amp_cmd threads export ${TID}"
{ echo "\$ $EXPORT_CMD (retried up to 4x/3s for documented server sync lag)"; } >>"$EVIDENCE/03-instruct.txt"
EXPORT_JSON=""
EXPORT_MSG_COUNT=0
EXPORT_LAST_ROLE=""
for _ in 1 2 3 4; do
  set +e
  EXPORT_JSON="$(eval "$EXPORT_CMD" 2>&1)"
  EXPORT_STATUS=$?
  set -e
  if [ "$EXPORT_STATUS" -eq 0 ]; then
    EXPORT_SUMMARY="$(python3 -c "
import json, sys
try:
    d = json.loads(sys.argv[1])
    msgs = d.get('messages') or []
    print(len(msgs))
    print(msgs[-1].get('role', '') if msgs else '')
except Exception:
    print(0)
    print('')
" "$EXPORT_JSON" 2>/dev/null || echo -e "0\n")"
    EXPORT_MSG_COUNT="$(echo "$EXPORT_SUMMARY" | sed -n 1p)"
    EXPORT_LAST_ROLE="$(echo "$EXPORT_SUMMARY" | sed -n 2p)"
    if [ "${EXPORT_MSG_COUNT:-0}" -ge 4 ] 2>/dev/null; then
      break
    fi
  fi
  sleep 3
done
{
  echo "export_exit=${EXPORT_STATUS}"
  echo "export_message_count=${EXPORT_MSG_COUNT}"
  echo "export_last_message_role=${EXPORT_LAST_ROLE}"
} >>"$EVIDENCE/03-instruct.txt"

TURN_LANDED=false
if [ "$EXPORT_STATUS" -eq 0 ] && [ "${EXPORT_MSG_COUNT:-0}" -ge 4 ] 2>/dev/null; then
  TURN_LANDED=true
fi

# Post-send show: the hard live/claim-released proof 02 defers to when it
# records MID-FLIGHT. Whichever wait ran the recovery — the launch's own
# (02 already PASS) or this send's waitPromptIdentity — by now the amp
# thread id must be bound (runtime_instance_state=live), the exited mint
# process's claim released (claim_held=false), and prompt.deliver
# "available" (operationsForInspect: live instance + bound adapter).
SHOW2_JSON="$(duo_cmd session show "$SES" --output json)"
SHOW2_SUMMARY="$(python3 -c "
import json, sys
d = json.loads(sys.argv[1])
r = d.get('result', {})
state = r.get('runtime_instance_state', '')
atts = r.get('attachments') or []
claim_held = atts[0].get('claim_held') if atts else None
ops = r.get('operations') or []
prompt_deliver_availability = ''
for op in ops:
    if op.get('operation') == 'prompt.deliver':
        prompt_deliver_availability = op.get('availability', '')
print(f'post_send_runtime_instance_state={state}')
print(f'post_send_claim_held={claim_held}')
print(f'post_send_prompt_deliver_availability={prompt_deliver_availability}')
" "$SHOW2_JSON")"
{
  echo "$SHOW2_SUMMARY"
} >>"$EVIDENCE/03-instruct.txt"
SHOW2_STATE="$(echo "$SHOW2_SUMMARY" | sed -n 's/^post_send_runtime_instance_state=//p')"
SHOW2_CLAIM="$(echo "$SHOW2_SUMMARY" | sed -n 's/^post_send_claim_held=//p')"

BOUND_AFTER_SEND=false
if [ "$SHOW2_STATE" = "live" ] && { [ "$SHOW2_CLAIM" = "False" ] || [ "$SHOW2_CLAIM" = "None" ]; }; then
  BOUND_AFTER_SEND=true
fi

{
  if [ "$SEND_LEG" = "delivered" ] && [ "$TURN_LANDED" = "true" ] && [ "$BOUND_AFTER_SEND" = "true" ]; then
    echo "bound_check=PASS (responsibility_state=delivered; export shows ${EXPORT_MSG_COUNT} messages, last role=${EXPORT_LAST_ROLE} — the turn landed; post-send state=live, claim released)"
  else
    echo "bound_check=FAIL (leg=${SEND_LEG}, export_message_count=${EXPORT_MSG_COUNT}, post_send_state=${SHOW2_STATE}, post_send_claim_held=${SHOW2_CLAIM}; expected delivered, >=4 messages, and live/claim-released after the send)"
  fi
  echo
} >>"$EVIDENCE/03-instruct.txt"

echo "$EXPORT_MSG_COUNT" >"$SCRATCH/msg-count-after-instruct.txt"

if [ "$SEND_LEG" != "delivered" ] || [ "$TURN_LANDED" != "true" ] || [ "$BOUND_AFTER_SEND" != "true" ]; then
  echo "capture-live.sh: instruct turn did not land bound-live; aborting before the collision capture" >&2
  exit 2
fi

# ---------------------------------------------------------------------------
# 04-collision.txt — a second writer (an interactive Amp TUI attached to
# the same thread inside the isolated Herdr session) holds the thread's
# executor connection; `duo prompt send` in JSON mode then attempts a
# real turn against the same thread and is refused server-side. PASS
# when: the envelope's error.code is operation.temporarily_unavailable,
# error.effect is unknown_effect, error.retry.action is
# retry_after_holder_release, error.details.error_kind is thread_locked
# (internal/cli/prompt.go's amp.ErrThreadLocked mapping); the command's
# own state is terminal "failed" (no requeue — read via `duo prompt
# show`, the CLI's command.inspect verb, since the Devin pair's captures
# never needed to read command state back out this way — Devin's own
# session.target_exited leg is a synchronous return with nothing further
# to inspect); AND a follow-up `amp threads export <tid>` (a second
# billed read; noted) shows no trace of the refused turn — message count
# unchanged from 03-instruct.txt's and the collision prompt's own
# distinctive text absent from every message (notes/62 §5: "no trace ...
# refusals are proven-no-effect").
#
# Opening the TUI holder is `herdr pane split` (a new pane in this
# disposable session, split off the launch pane) plus `herdr pane run`
# (types `amp threads continue <tid> --settings-file ...` — no `-x`,
# since `-x`/execute mode is what switches Amp to headless batch mode;
# omitting it is what leaves the interactive TUI attached and holding the
# thread's executor connection — inferred from the CLI's own flag
# semantics; the archive at fixtures/amp/README.md documents `-x` only
# for headless recipes and never shows a bare `threads continue <tid>`
# without it). Confirming it is up is observation only: polling `herdr
# pane process-info` for the pane's foreground process, never a signal
# sent to it.
# ---------------------------------------------------------------------------
write_header "$EVIDENCE/04-collision.txt" "second-writer collision: TUI holder + duo prompt send (JSON mode)"

printf '%s' "$SHOW1_JSON" >"$SCRATCH/show1.json"
LAUNCH_PANE="$(python3 -c "
import json, pathlib
d = json.loads(pathlib.Path('${SCRATCH}/show1.json').read_text())
atts = (d.get('result') or {}).get('attachments') or []
print(atts[0].get('container', '') if atts else '')
")"
{
  echo "launch_pane=${LAUNCH_PANE}"
} >>"$EVIDENCE/04-collision.txt"

if [ -z "$LAUNCH_PANE" ]; then
  echo "capture-live.sh: no launch pane container to split the collision holder from; aborting" >&2
  exit 2
fi

# One split, captured and parsed from the same invocation. (Run 3 used
# append_cmd — which executes its command — plus a second execution for
# the parse, creating two panes: the captured JSON named w1:p2 while the
# holder ran in w1:p3, and the extra pane was never closed.)
SPLIT_CMD="herdr_cmd pane split \"${LAUNCH_PANE}\" --direction down --cwd \"${SCRATCH}/ws\" --no-focus"
set +e
SPLIT_JSON="$(herdr_cmd pane split "$LAUNCH_PANE" --direction down --cwd "$SCRATCH/ws" --no-focus 2>&1)"
SPLIT_STATUS=$?
set -e
{
  echo "\$ $SPLIT_CMD"
  echo "$SPLIT_JSON"
  echo "exit=$SPLIT_STATUS"
  echo
} >>"$EVIDENCE/04-collision.txt"

HOLDER_PANE="$(python3 -c "
import json, sys
try:
    d = json.loads(sys.argv[1])
    print(d['result']['pane']['pane_id'])
except Exception:
    print('')
" "$SPLIT_JSON")"
echo "holder_pane=${HOLDER_PANE}" >>"$EVIDENCE/04-collision.txt"

if [ -z "$HOLDER_PANE" ]; then
  echo "capture-live.sh: pane split did not yield a holder pane id; aborting" >&2
  exit 2
fi

RUN_TUI_CMD="herdr_cmd pane run \"${HOLDER_PANE}\" amp threads continue ${TID} --settings-file ${AMP_SETTINGS_PATH}"
append_cmd "$EVIDENCE/04-collision.txt" "$RUN_TUI_CMD"

# Observation only: poll process-info on the holder pane for its
# foreground process to become "amp", up to 15s at 500ms — no signal.
TUI_POLL_START_NS=$(date +%s%N)
TUI_ELAPSED_MS=0
TUI_UP=false
while [ "$TUI_ELAPSED_MS" -le 15000 ]; do
  PROC_INFO_OUT="$(herdr_cmd pane process-info --pane "$HOLDER_PANE" 2>&1 || true)"
  FG_NAME="$(python3 -c "
import json, sys
raw = sys.argv[1]
name = ''
try:
    procs = (json.loads(raw).get('result') or {}).get('process_info', {}).get('foreground_processes') or []
    if procs:
        name = procs[0].get('name', '')
except Exception:
    pass
print(name)
" "$PROC_INFO_OUT" 2>/dev/null || echo "")"
  if [ "$FG_NAME" = "amp" ]; then
    TUI_UP=true
    break
  fi
  sleep 0.5
  NOW_NS=$(date +%s%N)
  TUI_ELAPSED_MS=$(( (NOW_NS - TUI_POLL_START_NS) / 1000000 ))
done
{
  echo "tui_holder_up=${TUI_UP} (elapsed_ms=${TUI_ELAPSED_MS}, foreground_process=${FG_NAME:-})"
  echo
} >>"$EVIDENCE/04-collision.txt"

if [ "$TUI_UP" != "true" ]; then
  echo "capture-live.sh: collision TUI holder never came up in the pane; aborting" >&2
  herdr_cmd pane close "$HOLDER_PANE" >/dev/null 2>&1 || true
  exit 2
fi

# A visible "amp" foreground process is not yet proof the executor
# handshake finished — give it a settling margin before attempting the
# collision write (still no signal sent; this is only a wait).
sleep 2

COLLISION_TEXT="COLLISION-PROBE: reply with the word blocked."
COLLISION_KEY="key-${SESSION}-amp-collision-1"
COLLISION_EXPIRES="$(date -u -d '+120 seconds' '+%Y-%m-%dT%H:%M:%S.000Z')"
COLLISION_CMD="duo_cmd prompt send ${SES} --text '${COLLISION_TEXT}' --idempotency-key ${COLLISION_KEY} --expires-at ${COLLISION_EXPIRES} --output json"
append_cmd "$EVIDENCE/04-collision.txt" "$COLLISION_CMD"

# The envelope is the LAST duo prompt.deliver line in the capture file —
# never simply the first '{' line: this file's herdr pane-split JSON
# precedes it, and run 3's first-'{' parse grabbed that split result,
# emptying every envelope field and skipping the command-state leg.
COLLISION_ENVELOPE="$(python3 -c "
import json, pathlib
text = pathlib.Path('${EVIDENCE}/04-collision.txt').read_text()
envelope = ''
for line in text.splitlines():
    if not line.startswith('{'):
        continue
    try:
        d = json.loads(line)
    except Exception:
        continue
    if d.get('schema') == 'duo.external/v1' and d.get('operation') == 'prompt.deliver':
        envelope = line
print(envelope)
")"

COLLISION_SUMMARY="$(python3 -c "
import json, sys
raw = sys.argv[1]
code = ''
effect = ''
retry_action = ''
error_kind = ''
command_id = ''
try:
    d = json.loads(raw)
    err = d.get('error') or {}
    code = err.get('code', '')
    effect = err.get('effect', '')
    retry_action = (err.get('retry') or {}).get('action', '')
    error_kind = (err.get('details') or {}).get('error_kind', '')
    command_id = (err.get('target') or {}).get('id', '')
except Exception:
    pass
print(f'code={code}')
print(f'effect={effect}')
print(f'retry_action={retry_action}')
print(f'error_kind={error_kind}')
print(f'command_id={command_id}')
" "$COLLISION_ENVELOPE" 2>/dev/null || echo -e "code=\neffect=\nretry_action=\nerror_kind=\ncommand_id=")"

{
  echo "$COLLISION_SUMMARY"
  echo
} >>"$EVIDENCE/04-collision.txt"

COLL_CODE="$(echo "$COLLISION_SUMMARY" | sed -n 's/^code=//p')"
COLL_EFFECT="$(echo "$COLLISION_SUMMARY" | sed -n 's/^effect=//p')"
COLL_RETRY="$(echo "$COLLISION_SUMMARY" | sed -n 's/^retry_action=//p')"
COLL_KIND="$(echo "$COLLISION_SUMMARY" | sed -n 's/^error_kind=//p')"
COLL_CMDID="$(echo "$COLLISION_SUMMARY" | sed -n 's/^command_id=//p')"

ENVELOPE_OK=false
if [ "$COLL_CODE" = "operation.temporarily_unavailable" ] \
  && [ "$COLL_EFFECT" = "unknown_effect" ] \
  && [ "$COLL_RETRY" = "retry_after_holder_release" ] \
  && [ "$COLL_KIND" = "thread_locked" ]; then
  ENVELOPE_OK=true
fi

# Command state: terminal failed, no requeue. Read via `duo prompt show`
# (command.inspect) rather than assumed from the envelope alone.
CMD_STATE=""
if [ -n "$COLL_CMDID" ]; then
  SHOW_CMD_CMD="duo_cmd prompt show ${COLL_CMDID} --output json"
  append_cmd "$EVIDENCE/04-collision.txt" "$SHOW_CMD_CMD"
  CMD_SHOW_JSON="$(duo_cmd prompt show "$COLL_CMDID" --output json)"
  CMD_STATE="$(python3 -c "
import json, sys
d = json.loads(sys.argv[1])
print((d.get('result') or {}).get('responsibility_state', ''))
" "$CMD_SHOW_JSON")"
  echo "command_responsibility_state=${CMD_STATE}" >>"$EVIDENCE/04-collision.txt"
fi
COMMAND_TERMINAL_FAILED=false
[ "$CMD_STATE" = "failed" ] && COMMAND_TERMINAL_FAILED=true

# Second billed read: a follow-up export must show no trace of the
# refused turn.
FOLLOWUP_EXPORT_CMD="amp_cmd threads export ${TID}"
append_cmd "$EVIDENCE/04-collision.txt" "$FOLLOWUP_EXPORT_CMD"
FOLLOWUP_EXPORT_JSON="$(amp_cmd threads export "$TID" 2>&1 || true)"
FOLLOWUP_SUMMARY="$(python3 -c "
import json, sys
raw = sys.argv[1]
needle = sys.argv[2]
try:
    d = json.loads(raw)
    msgs = d.get('messages') or []
    count = len(msgs)
    contains = any(needle in json.dumps(m) for m in msgs)
except Exception:
    count = -1
    contains = None
print(f'message_count={count}')
print(f'contains_collision_text={contains}')
" "$FOLLOWUP_EXPORT_JSON" "$COLLISION_TEXT")"
{
  echo "$FOLLOWUP_SUMMARY"
} >>"$EVIDENCE/04-collision.txt"

FOLLOWUP_COUNT="$(echo "$FOLLOWUP_SUMMARY" | sed -n 's/^message_count=//p')"
FOLLOWUP_CONTAINS="$(echo "$FOLLOWUP_SUMMARY" | sed -n 's/^contains_collision_text=//p')"
BASELINE_COUNT="$(cat "$SCRATCH/msg-count-after-instruct.txt" 2>/dev/null || echo "")"

NO_TRACE=false
if [ -n "$BASELINE_COUNT" ] && [ "$FOLLOWUP_COUNT" = "$BASELINE_COUNT" ] && [ "$FOLLOWUP_CONTAINS" = "False" ]; then
  NO_TRACE=true
fi

{
  if [ "$ENVELOPE_OK" = "true" ] && [ "$COMMAND_TERMINAL_FAILED" = "true" ] && [ "$NO_TRACE" = "true" ]; then
    echo "bound_check=PASS (envelope=operation.temporarily_unavailable/unknown_effect/retry_after_holder_release/thread_locked; command terminal failed, no requeue; export message_count=${FOLLOWUP_COUNT} unchanged from baseline ${BASELINE_COUNT}, no trace of the refused turn)"
  else
    echo "bound_check=FAIL (envelope_ok=${ENVELOPE_OK} [code=${COLL_CODE} effect=${COLL_EFFECT} retry=${COLL_RETRY} kind=${COLL_KIND}], command_terminal_failed=${COMMAND_TERMINAL_FAILED} [state=${CMD_STATE}], no_trace=${NO_TRACE} [count=${FOLLOWUP_COUNT} vs baseline=${BASELINE_COUNT}, contains=${FOLLOWUP_CONTAINS}])"
  fi
  echo
} >>"$EVIDENCE/04-collision.txt"

# Teardown of the disposable TUI holder pane — Herdr pane control, not a
# signal against the Amp process itself (run-live.sh's own session-level
# teardown would eventually reap it regardless; this is early billing
# hygiene, same spirit as archiving the thread promptly in 05 below).
herdr_cmd pane close "$HOLDER_PANE" >/dev/null 2>&1 || true
echo "holder_pane_closed=true" >>"$EVIDENCE/04-collision.txt"

# ---------------------------------------------------------------------------
# 05-archive.txt — amp threads archive <tid>, the same cleanup every
# fixtures/amp probe performs at the end of its own run. PASS on a clean
# archive (exit 0, no error text in the response).
# ---------------------------------------------------------------------------
write_header "$EVIDENCE/05-archive.txt" "amp threads archive (disposable thread cleanup)"
ARCHIVE_CMD="amp_cmd threads archive ${TID}"
append_cmd "$EVIDENCE/05-archive.txt" "$ARCHIVE_CMD"

ARCHIVE_EXIT="$(grep '^exit=' "$EVIDENCE/05-archive.txt" | tail -1 | cut -d= -f2)"
{
  if [ "$ARCHIVE_EXIT" = "0" ]; then
    echo "bound_check=PASS (amp threads archive exited 0)"
  else
    echo "bound_check=FAIL (amp threads archive exited ${ARCHIVE_EXIT})"
  fi
  echo
} >>"$EVIDENCE/05-archive.txt"

echo "capture-live.sh: done. session=${SES} thread=${TID}"
