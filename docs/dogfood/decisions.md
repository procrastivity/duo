# Dogfood decisions

Decisions about the dogfood checkpoint itself: what daily use must
exercise, where evidence lands, and how friction routes back.

## 2026-08-24 — Checkpoint definition (duo-dogfood step 24)

The dogfood checkpoint is real daily use of the authored
`$XDG_CONFIG_HOME/duo/duo.config.yaml` (`duo.config/v3`): sessions come
up via `duo session launch <preset>` onto Herdr+Claude Code or
Herdr+Pi, and one day of that use exercises every checkpoint verb —
launch via preset, list, show, detach, reattach — plus one
authority-restart recovery. Prompt delivery and collaboration stay
Stage 2+, and the checkpoint makes no cross-host generality claim
(handoff 20).

Evidence convention: one directory per day,
`evidence/dogfood/<YYYY-MM-DD>/`, holding raw captures as
`NN-<what>.txt` (the command line echoed first, exit status last) and a
`log.md` that names which checkpoint verb each capture exercises. The
day that completes the checkpoint is the one whose `log.md` covers
every verb.

Friction routing: an implementation-repo decision goes to the owning
`docs/<area>/decisions.md` as a dated entry; anything that contradicts
the locked planning set becomes a ledger contradiction record in the
planning repo per the handoff-22 change control. Nothing routes through
chat.

## 2026-08-25 — Recovery and prune verbs (duo-dogfood-recovery)

Checkpoint verbs now include **`reconcile`**, **`archive`**, and
**`remove`** in addition to launch, list, show, detach, and reattach.

Fingerprints for the detach→reattach drill come from **`duo session show`**
(copy the `reattach with:` line), not from `herdr pane list`. Process birth
on launched sessions is part of the claim.

Operator procedure: `docs/dogfood/procedure.md`. Evidence convention is
unchanged — one directory per day under `evidence/dogfood/<YYYY-MM-DD>/`.
Recovery fixture traces live under `evidence/traces/recovery/`.

Reconcile is explicit; list and show do not write recovery facts.
