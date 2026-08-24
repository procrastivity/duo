# Dogfood day log — 2026-08-24

Config authored and validated this day (step 24, with the user).
Binary: duo built from `go` @ c534067 (--output chassis-wide).

| capture | verb / claim |
|---|---|
| 01-doctor.txt | doctor: config recognized as duo.config/v3 at the default path; ambient host deduction visible |
| 02-dry-runs.txt | launch --dry-run: all four presets resolve; build_and_verify honors distinct_model_family |
| 03-dry-run-json.txt | launch --dry-run --output json: duo.external/v1 envelope |
| duo.config.yaml | the authored document (copy) |
| 04-launch-orchestrator.txt | launch (real, Herdr+Claude): fable-5 spawned; ambient-env bind asked, read no tty, skipped by design — launch still succeeded |
| 05-list.txt / 06-show.txt | list, show: active/recovering, the documented Stage-1 view |
| 07-launch-build-and-verify.txt | FAILURE, kept as evidence: multi-leaf agent-name collision (agent_name_taken on the second leaf); also run from the wrong cwd, minting a workspace for the evidence dir |
| 08-host-show.txt | workspace host show on the accidental workspace: unbound, correct detail line |
| 09-launch-pi-require.txt | launch (real, Herdr+Pi) with --require agent_runtime=pi and explicit --host: gpt-5.6-luna spawned; first bind recorded silently (host_source=explicit-flag) with the pre-filled rebind line |
| 10-host-show-bound.txt | workspace host show: full provenance + fingerprints for the new correlation |
| 11-detach-reattach.txt | FAILURE, kept as evidence: detach/reattach refuse on a LAUNCHED session — no host attachment is recorded at launch commit; enroll-only today |
| 12-enroll.txt | enroll: this Claude Code pane adopted as a session (credential redacted) |
| 13-detach-reattach-enrolled.txt | detach, reattach, authority-restart recovery: each CLI invocation reopens the authority, so the detach→reattach pair across processes is the restart drill; view stays recovering per the known limit |
| 14-launch-build-and-verify-fixed.txt | launch (real, multi-leaf) after fix a52c39c: builder and verifier spawn with DISTINCT agent names (duo-builder-…, duo-verifier-…); correlation-outranks-ambient note shown |
| 15-launch-attach.txt | launch (real) after fix 8c8deac: attachment recorded from the spawn's own evidence |
| 16-detach-reattach-launched.txt | detach on a LAUNCHED session now works; reattach refuses on strict fingerprint match — the claim's process birth is unreadable from any verb (confirmed follow-on: surface it on session show) |

Checkpoint verbs all exercised at least once this day: launch-via-preset
(claude and pi legs), list, show, enroll, detach, reattach, recovery.
Open from this day: the multi-leaf name-collision fix (landed same day, a52c39c, proven by capture 14), the
launch-has-no-attachment seam gap (surfaced), stray-pane teardown debt
(live reproduction), and the full-day daily-use log that completes the
checkpoint.

## Evening addendum — launch placement (finding + provisional fix)

Finding: every launch opened a right-split pane; expected a tab, with a
per-host default and a per-launch override. Cause was the adapter's
hardcoded `pane.split {direction: "right"}`. Design sketch:
terminal-multiplexers `notes/44-launch-placement-sketch.md`.

Provisional fix landed same day (`go` @ 2258c63, installed as
`~/.local/bin/duo-go`): Herdr launches default to a **background tab**;
`--target=tab|pane` overrides per launch (an override like `--host`,
not a constraint axis). Config-authored default, record fields, and
contract updates are deferred to change control per the sketch.

Bonus catches while live-proving the tab path (both fixed in 2258c63,
both placement-independent): the pre-agent baseline could fingerprint a
shell-startup transient, and the handover wait could crown a
prompt-helper transient as the agent — either way a launched session
recorded an attachment that failed its own first validation (the
capture-16 family of pain). Baseline now settles on the shell; handover
requires a stable PID across two polls. `TestLiveHerdr` passed 3× per
placement against a disposable 0.8.2 server.
