# Store decisions

Decisions the code cannot show on its own. Normative sources:
`duo-vnext-go-architecture.md` §4 and `duo-vnext-decision-01-identity-lifecycle.md`
§4–5 in the planning repo.

## Writer lease: hybrid guard with in-transaction fencing

One `writer_lease` row (id fixed at 1) holds the current authority writer:
incarnation ID (minted fresh per acquisition), pid, hostname, and an
expiry. `OpenAuthority` acquires it; a second concurrent writer receives
the typed `*WriterActiveError` and no database handle.

Takeover of a held lease is allowed in exactly two cases:

1. **Expiry.** The lease has lapsed (the holder stopped renewing for
   `leaseTTL`). Crash, freeze, and power-loss recovery all reduce to this
   rule, and it works across hosts on a shared filesystem.
2. **Dead same-host holder.** The lease names this hostname and signal-0
   proves the pid is gone. This is a fast path so a restart after a crash
   does not wait out the TTL. It never fires cross-host, and pid liveness
   alone never *grants* a lease — it can only justify replacing one.

Neither guard is sufficient by itself: expiry alone makes every restart
wait, the process guard alone cannot see across hosts or survive pid
reuse. The pair is safe because takeover does not rely on the old writer
being gone: **every write transaction is fenced**. `withTx` reads the
lease row first — pinning the WAL snapshot — and refuses (`ErrLeaseLost`)
unless the incarnation is its own; a takeover committed after that
snapshot makes the stale writer's commit fail under SQLite's
snapshot-isolation rules instead of interleaving two writers. A usurped
but still-live process therefore fails safely on its next write; it can
never corrupt state, which is what makes expiry-based takeover of a
merely-frozen holder acceptable.

`Close` releases the lease (guarded by incarnation, so a usurped handle
cannot delete its successor's lease). Release is best-effort; a crash
skips it and the rules above recover.

## Incarnation ID

128-bit random hex, minted per acquisition, never reused. It stamps every
audit row and work claim, which is what lets recovery distinguish "my
attempt" from "a previous incarnation's attempt" without trusting clocks.

## Transaction boundaries

The eight §4.2 boundaries are the only exported transactions
(`EnrollmentTx`, `CommandAcceptTx`, `CommandTransitionTx`,
`ObservationTx`, `CollaborationTx`, `NotificationTx`,
`ProjectionInstallTx`, `LaunchResolutionTx`). §4.2 defines each as "one
transaction commits each of these logical changes", which does not permit
free composition, so there is no exported `WithTx`. Work-queue lifecycle
operations (claim, complete, fail, unknown_effect) run internally under
the command-transition boundary — §4.2's "one command transition and its
attempt or reconciliation metadata".

`Tx.EnqueueWork` exists only on the transaction handle because §4.2
admits a durable work item only inside command acceptance.

## unknown_effect

Per §4.3, an attempt row with no result does not distinguish crash-before
from crash-during the external call, so recovery never re-runs it blind:
a claimed item is not claimable, `UnresolvedAttempts` surfaces orphans of
other incarnations, and `MarkUnknownEffect` moves attempt →
`unknown_effect` and item → `indeterminate`, which `ClaimWork` never
selects. Retry happens only through `FailWork(retry=true)` when a
conformance proof establishes no effect.

## External attempt token

§4.3 permits a retry only where "the conformance record and retained
external attempt identity prove no effect or permit idempotent
reconciliation". Retained means retained across the crash, so the
identity has to be durable *before* the external call, not minted from
its response. `ClaimWork` therefore mints `work_attempt.external_token`
and commits it in the same transaction as the attempt row; the adapter
carries it into the call as whatever the conformance record names (an
idempotency key, a request ID), and `UnresolvedAttempts` hands it back to
the next incarnation.

The token is random 128-bit hex with a `duo-` prefix, not derived from
`(work_id, attempt)`: derivation would repeat across a restored or copied
database, and would let an outside party address an external effect by
guessing. Uniqueness is enforced in the schema.

A **retry gets a new token**. The token identifies one attempt, not the
work item — reusing it would invite the external system to dedup a call
that is meant to run, and a retry is legitimate only once the previous
attempt is proven to have had no effect. An adapter that instead needs a
key stable across attempts (true external idempotency) puts it in the
work item's payload, which is durable from enqueue.

Nothing in the substrate performs a result lookup. §4.3 makes that
conformance-defined per adapter, so the store's job ends at retaining the
identity and refusing to move an unreconciled item.

## Amending the substrate migration

`store.go` states the rule the migration register lives by: never edit a
shipped entry, append. `substrateV1` has not shipped — this is the
step that introduces it — so `external_token` was added to the v1 table
definition rather than as a v2 `ALTER TABLE`. The moment the first
release carries v1, that stops being allowed.

## What "crash injection" tests

`TestBoundaryCrashInjection` runs four injections against each of the
eight boundaries: an error returned mid-body, a failure between the last
write and the commit (`Store.beforeCommit`, a test-only hook), a panic
mid-body, and — after a clean commit — dropping the database handle
without `Close`, exactly as a killed process leaves it, then reopening
under a new incarnation. The first three assert that nothing partial
survives; the fourth asserts that what committed does. Both halves are
needed: a boundary that rolled everything back always would pass the
first three.

Process-level `SIGKILL` and power-loss injection are out of scope here.
They test SQLite's WAL durability rather than this package's boundaries,
and they cannot run deterministically in `go test`.

## Content-addressed large content: not yet

§4.2 says large content *can* live in content-addressed files beside
SQLite, with the digest committed only after durable placement and a GC
pass over unreferenced prepared content. The substrate does not implement
it: no Stage 0 caller has content too large for a SQLite row, and the
placement/GC protocol is a second durability problem (fsync ordering,
safe-age policy, orphan sweeps) that deserves its own step with its own
crash tests. Nothing here forecloses it — it arrives as a later migration
plus a `content` file, not a change to these boundaries.

## Why not the wip `runlock` flock pattern

wip guards a Run with a `flock(2)` on a host-local lock file, which the
kernel releases on process death — no TTL, no liveness probe. It was not
reused for the writer lease because the lock has to be visible wherever
the database is: advisory locks over a network filesystem are unreliable,
while the lease row travels with the database it protects. The flock
pattern remains the right shape for genuinely host-local, ephemeral
things (a socket, a runtime directory) if the chassis grows one.

## Timestamps

TEXT, fixed layout `2006-01-02T15:04:05.000Z` (UTC, millisecond). The
layout is fixed-width so lexicographic comparison in SQL equals time
comparison; expiry checks compare strings.

## Startup pragmas

§4.1's startup requirements (WAL, foreign-key checks, a bounded busy
timeout) are set as DSN `_pragma` parameters, which fail *silently* when
mistyped or when the driver stops honouring them. `TestStartupPragmas`
reads the effective values back and proves the foreign key actually
refuses a dangling `work_attempt.work_id`, so the guarantee is asserted
rather than assumed.
