# Dogfood day log — 2026-08-25

Recovery gate for matter `duo-dogfood-recovery` (step 12). Product binary:
`/home/dev/Code/duo-vnext/bin/duo` built from `go` @ `a91a0ff-dirty`.
PATH `duo` is TypeScript 0.2.2 — not used.

Isolated store: `XDG_DATA_HOME=evidence/dogfood/2026-08-25/xdg-data`.
Live `~/.local/share/duo/duo.db` was backed up first and not mutated
(sha256 `7fddde2f23592a4ed1b8dd9fb35b4387c38d0d3e18f13eabef43656c1c7d75d7`
matches `00-live-store-backup.db` after the gate).

Host: Herdr 0.8.2, session `terminal-multiplexers`. Launch used
`--host herdr:<socket-path>` (session-name form is not a socket path).

| capture | verb / claim |
|---|---|
| 00-live-store-backup.db | backup of the live 25-row ledger before any gate mutation |
| 01-dry-run-orchestrator.txt | launch --dry-run orchestrator with `--host herdr:<session-name>` |
| 02-launch-orchestrator.txt | FAILURE: `--host herdr:terminal-multiplexers` treated the session name as a socket path (exit 4). Left `ses_09a40a00…` with no attachments (sharp edge 10) |
| 03-launch-orchestrator.txt | launch (real, single-leaf Claude): `--host herdr:<full-socket>` `--target tab` `--close-on-exit` → `ses_523b6770…` |
| 04-show-orchestrator.txt / 05-show-orchestrator-json.txt | show: process tuple + pasteable `reattach with:` / JSON `attachments[0].reattach_command` |
| 06-detach-orchestrator.txt / 07-reattach-orchestrator.txt | detach, then the printed command → `attachment attached` |
| 08-show-after-reattach.txt | show after reattach: claim held, command still printed |
| 09-pane-close.txt | `herdr pane close w3:p2` after agent/pane exit |
| 10-reconcile-orchestrator.txt | BUG: `unreachable` — locator was the socket path; `SessionForInstanceID` rejects path separators |
| 11-show-after-reconcile.txt / 12-list-after-reconcile.txt | still recovering; no exit recorded (correct given unreachable) |
| 13-reconcile-after-fix.txt | after `herdrSocketForIntegration` accepts locator paths: `exited (pane absent)` |
| 14-show-after-reconcile-fix.txt | `lifecycle=active`, `view=inactive`, `instance state=exited`, claim released, command omitted |
| 15-archive.txt / 16-remove.txt | prune ladder: archived then removed |
| 17-list-after-remove.txt | list omits the tombstone; leftover failed launch still recovering |
| 18-show-tombstone.txt | show still answers: `lifecycle=removed`, `view=removed` |
| 19-dry-run-build-and-verify.txt | launch --dry-run: builder claude/sonnet-5 + verifier pi/gpt-5.6-terra |
| 20-launch-build-and-verify.txt | launch (real, multi-leaf) → `ses_35919b6c…` |
| 21-show-build-and-verify.txt / 22-show-build-and-verify-json.txt | two attachments, two process tuples, two pasteable commands; JSON `attachments[]` |
| 23-detach-build-and-verify.txt | detach (last-leaf pointer, as locked) |
| 24-reattach-builder.txt / 25-reattach-verifier.txt | each printed command → `attachment attached` |
| 26-show-after-multi-reattach.txt | both attachments attached, claims held |
| 27-unreachable-no-state-change.txt | hide `herdr.sock`, reconcile → `unreachable` on both leaves; view stays recovering, instance stays starting; socket restored |
| 28-fixture-reconcile.json | copy of `evidence/traces/recovery/fixture-reconcile.json` (25-session export, isolated: 22 exited, 3 replaced) |
| 29-cleanup-isolated-sessions.txt | close BAV panes; reconcile exited/replaced; archive+remove isolated sessions; list empty. Live store untouched |

Finding recorded on the step: launch `--host herdr:<socket-path>` must
resolve that path at reconcile time; treating it as a session name produced
false Unreachable. Fix is `herdrSocketForIntegration` (session-name layout
or existing socket path; missing file stays Unreachable, never pane-absent).
