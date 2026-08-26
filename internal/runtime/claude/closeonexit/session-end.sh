#!/bin/sh
# duo close-on-exit SessionEnd hook.
#
# Close-on-exit is the product default (notes/51 record 7). Duo writes this
# exact file, embedded verbatim (see closeonexit.go), into a per-launch
# harness directory and points a generated `claude --settings <path>`
# document at it — it never ships as part of the user's own Claude Code
# configuration.
#
# Herdr always runs the user's shell in a pane; `agent.start` types the
# launch command into that shell, and the shell survives the agent's own
# exit. Live probing (herdr 0.8.2 + claude 2.1.241, 2026-08-24) found
# exactly one way to close the pane itself without a watcher, send-keys, or
# any injection from outside the pane: a synchronous action taken from
# *inside* the agent's own SessionEnd hook. This script is that action.
#
# Synchronous is load-bearing, not a style choice. Claude Code runs a
# SessionEnd hook and waits for it before the process actually exits; an
# async terminal-state hook races that exit and its effect gets lost
# (duo-vnext-installation-contract.md:369-374). So this script never
# backgrounds anything (no trailing `&`, no disown) and always runs to
# completion before it exits.

# TERMINAL_REASONS is the one-line, easily-edited list of SessionEnd
# `reason` values this hook treats as "the agent process actually exited."
# Keep it a bare space-separated list on one line — whoever edits this list
# later (the orchestrator, after a pending probe corroborates another
# reason) should need to touch nothing else in this script.
#
# "clear" and "resume" are known to fire SessionEnd WITHOUT the process
# exiting and MUST NOT be added here: adding either would close the pane
# out from under a still-live agent. The full reason vocabulary observed so
# far is: clear, resume, logout, prompt_input_exit,
# bypass_permissions_disabled, other.
TERMINAL_REASONS="prompt_input_exit logout"

# Read the whole hook payload before doing anything else with it. Claude
# Code writes the SessionEnd payload to this hook's stdin in one shot, but
# a script that started parsing before the write finished could still see
# a truncated read; `cat` blocks until stdin closes, so this is
# incremental-read-safe by construction.
input=$(cat)

# Guard: act only inside a Herdr pane, and only if this script can actually
# invoke the herdr binary the pane's environment named. HERDR_ENV, an
# empty HERDR_PANE_ID, or a non-executable HERDR_BIN_PATH are each reasons
# to do nothing quietly — "not a Herdr pane" (or "herdr changed its
# contract") means "leave the pane alone," never "close it anyway."
if [ "$HERDR_ENV" != "1" ] || [ -z "$HERDR_PANE_ID" ] || [ -z "$HERDR_BIN_PATH" ] || [ ! -x "$HERDR_BIN_PATH" ]; then
    exit 0
fi

# Extract the top-level "reason" string field without a jq dependency —
# jq is not something Duo can assume is on a launched agent's PATH, and
# this hook needs exactly one field out of the payload. This sed is a
# narrow, deliberately non-general JSON parse: it matches a simple
# "reason":"value" string member (true of every SessionEnd payload Duo has
# observed) and does not handle nesting or an escaped double quote inside
# the value. A payload shape that broke this parse would be a claude-runtime
# evidence update, not a bug in this one-field extraction.
reason=$(printf '%s' "$input" | sed -n 's/.*"reason"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p')

for candidate in $TERMINAL_REASONS; do
    if [ "$candidate" = "$reason" ]; then
        # Synchronous, on purpose (see above): this call itself must
        # return before the script exits and before Claude Code's own
        # process exit proceeds.
        "$HERDR_BIN_PATH" pane close "$HERDR_PANE_ID"
        break
    fi
done

# Always exit 0. A non-zero exit from a SessionEnd hook has no defined
# close-on-exit meaning here — the pane's fate is entirely the "did we
# call pane close" branch above, not this script's own exit status — and a
# nonzero exit risks Claude Code surfacing hook-failure noise on every
# ordinary session end, terminal reason or not.
exit 0
