# Dogfood operator procedure — session recovery and prune

One sitting. Assumes Herdr is running, `duo.config/v3` is in place, and
you launch from the project root. This procedure does not promise automatic
reconciliation — you must run `duo session reconcile` explicitly.

## 1. Show attachments and copy reattach

```bash
duo session show <session-id>
```

Text mode prints one block per attachment (multi-leaf launches get several).
Each block lists integration instance, epoch, container, optional process
birth, and — when the session holds the rebuilt claim — a single-line
`reattach with:` command.

JSON mode (`--output json`) exposes the same data under `result.attachments[]`
with `reattach_command` when available.

**Do not** build reattach flags from `herdr pane list` alone. Launched
sessions record process birth on the claim; the tuple in `session show` is
the authoritative fingerprint.

## 2. Detach and reattach (authority-restart drill)

```bash
duo session detach <session-id>
# copy the reattach with: line from show (or re-run show)
duo session reattach <session-id> --integration-instance … --epoch-kind … \
  --epoch-value … --epoch-scope … --container … \
  [--process-pid … --process-started-at …]
```

Detach needs only the session ID. Reattach must match the printed command.
Across two CLI invocations this is also the authority-restart recovery drill.

Until you reconcile (next section), `session list` may still show
`view=recovering` even after a successful reattach — that is expected on
the one-shot CLI.

## 3. Reconcile after agent or pane exit

When agents have exited or panes are gone, instances stay in the derived
`recovering` view until you reconcile:

```bash
duo session reconcile              # every recovering instance
duo session reconcile <id> [<id>…] # selected sessions only
```

Reconcile opens the write authority, probes each attachment against the
live Herdr host, and calls `ResolveRecovery` once per instance. It prints
one line per instance: session ID, instance ID, outcome, reason.

Outcomes include `same-live`, `exited`, `replaced`, and `unreachable`.
An unreachable host (missing socket, dial failure, timeout) changes
nothing — retry when Herdr is back.

`session list` and `session show` never write; they may hint that
reconcile is pending. `duo doctor` reports how many instances are awaiting
reconciliation.

## 4. Archive and remove (prune ladder)

After reconcile leaves a session **inactive** (no live instance):

```bash
duo session archive <session-id>   # lifecycle → archived
duo session remove <session-id>    # lifecycle → removed (tombstone)
```

Order is strict: inactive → archive → remove. Archive refuses a live
session; remove refuses a non-archived session.

- `duo session list` omits removed sessions by default.
- `duo session show <id>` still answers for removed tombstones.

Failed spawns may still leave stray Herdr panes; close those with
`herdr pane close <pane-id>` — `session remove` only tombstones the
ledger row.

## Quick reference

| Goal | Command |
|---|---|
| See fingerprints | `duo session show <id>` |
| Pause observation | `duo session detach <id>` |
| Resume observation | paste `reattach with:` from show |
| Record exit/replacement | `duo session reconcile` |
| Retire from active use | `duo session archive <id>` |
| Hide from list | `duo session remove <id>` |
