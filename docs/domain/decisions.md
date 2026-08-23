# Identity and lifecycle domain decisions

Decisions the code cannot show on its own. Normative sources:
`duo-vnext-decision-01-identity-lifecycle.md` §4–§5 and
`duo-vnext-go-architecture.md` §3–§4 in the planning repository, as amended
by the Herdr 0.8.2 probe (`notes/19-herdr-probes.md` §5,
`review/05-close-report.md` §1).

## A new package, not an extension of sessioncore

`internal/sessioncore` stays what its own package doc says it is: Step 11's
hostless-session proof, which implements the runtime-instance state machine
and nothing else. The kernel lives in `internal/domain` instead, for one
hard reason: sessioncore's contract permits it to import `internal/host` and
`internal/runtime`, and the architecture's §3 table forbids the domain kernel
to depend on adapter packages. Importing sessioncore would put that
dependency one edge away.

The cost is a duplicated transition table. `TestInstanceTransitionsMatchSessioncore`
pays for it: the test (not the kernel) imports sessioncore and asserts that
both tables allow exactly the same transitions, so they cannot drift while
both exist.

## No domain tables: the fact log is the durable model

The domain adds **no store migration**. Its durable form is an append-only
lifecycle fact log plus an active-claim index, both written through the
store's existing stream-log primitive, and the whole model is rebuilt in
memory by replaying that log at startup.

That is not the shape `docs/store/decisions.md` anticipated ("Domain tables
… are a Stage 1 migration"), so the reasoning matters:

- The store's public API admits exactly three writes inside a boundary:
  `AppendAudit`, `EnqueueWork`, `AppendStreamItem`. There is no exported
  general-purpose exec, deliberately — "SQL never leaves this package". A
  migration that added domain tables would therefore add schema that nothing
  outside `internal/store` can write. The table needs a repository
  implementation *inside* the store package, which this step does not own
  (three other agents are working in the same tree; the step's boundary
  allows appending a migration entry and nothing else in that package).
- A second database handle for domain tables was rejected outright. Two
  handles means two transactions, and §4.2's enrollment boundary requires the
  session, runtime instance, correlations, and active claim to commit
  together. Atomicity is the whole point of this step.
- The stream log turns out to fit the requirement rather than merely tolerate
  it. `AppendStreamItem` is an insert-or-return keyed on `(stream, item_id)`,
  which is exactly the atomic test-and-set the active-claim index needs, and
  it is enforced by SQLite inside the enrollment transaction.

What it costs, recorded so the next owner can price the migration:

- Reads are replay-only. The kernel holds every workspace, session, runtime
  instance, correlation, and claim in memory. That is right for one local
  authority at dogfood scale and wrong for a store that grows without bound.
- The domain's streams (`duo.domain.fact/v1`, `duo.domain.claim/v1`) are
  **retention-exempt**. The stream log's eventual retention and cursor-expiry
  policy must never delete from them: for these two streams the log is the
  record of truth, not a projection of one.
- A query such as "list sessions in this workspace" is a scan of memory
  rather than an index.

The migration path is additive: a later step that owns `internal/store` adds
the domain tables and a second `domain.Repository` implementation, and
`Load` becomes a read of those tables. Nothing above the `Repository`
interface changes, because no SQL, JSON, or transaction handle crosses it
today.

## One more constraint that shaped this: no reads inside a boundary

`store.Store` sets `SetMaxOpenConns(1)`. An open `*store.Tx` holds that one
connection, so calling a `*Store` read method (`ReadStream`) from inside a
boundary body would wait for a connection that only the transaction can
release. A boundary body can therefore write but not read.

That is why the enrollment *decision* is made from the in-memory index before
the transaction opens, and the transaction only enforces it. See the next
section.

## The active-claim index: decide in memory, enforce in the store

Enrollment looks up claims in the kernel's index, which is a replay of the
same durable facts and is authoritative for a single writer (the store's
writer lease guarantees there is one). The transaction then seizes each claim
by inserting its token; a token that already exists fails the whole
transaction with `ErrClaimTaken` and commits nothing.

Both halves are load-bearing. The index is what lets the kernel distinguish
"already enrolled → return the same session" from "overlapping evidence →
conflict", which needs a read. The durable token is what makes a *double
claim* impossible even when the index is wrong.
`TestDurableClaimIndexOutranksAStaleKernel` proves the backstop with two
kernels over one store: the stale one believes the fingerprint is free, and
still commits nothing.

### Claim generations

A claim token's durable ID is `kind/key#generation`. Exit releases a claim,
and the next hold of the same key uses generation + 1.

The generation exists for degraded fingerprints. Where process-birth evidence
is present, a claim key can never legitimately recur — the same PID with the
same start time is the same process. Where it is absent (a host that cannot
report process birth), the key is pane-scoped and *will* recur when a new
process appears in that pane. Without generations that pane could never be
enrolled again after its first execution exited. With them, the re-taken
claim is visibly a new generation held by a new runtime instance, and the
exited instance is never reopened — §6.4's rule, made mechanical.

## Fingerprints: an epoch-*equivalent* with a scope

decision-01 §4.2 asks a terminal-hosted fingerprint for "the host server epoch
or equivalent server identity". **The probe wins over the literal reading, and
the difference is recorded in the type.** Herdr 0.8.2 has no server-scoped
epoch on any surface, and its epoch-equivalent is the per-pane `terminal_id`:
a restarted Herdr server restores a pane under its old `pane_id` with a new
`terminal_id`.

`HostEpoch` therefore carries a kind, a value, and an `EpochScope` of
`server` or `pane`. A pane-scoped epoch is accepted: it distinguishes
incarnations of the one container the claim is about, which is all the claim
key needs. `TestHerdrRestartIsNotTheSameLiveRuntime` pins the probe finding —
same `pane_id`, new `terminal_id`, different claim key.

The scope is not cosmetic. A recovery rule that wants "the same host object"
across a *server* restart can only be satisfied by a server-scoped epoch;
with a pane-scoped one, only the process-birth half of the fingerprint can
carry that weight. Recording the scope keeps that visible to whoever writes
the next recovery policy.

`Fingerprint` has no working-directory, command-name, agent-name, or
transcript-modification-time field at all. §4.2 says those are never
sufficient, so the claim path cannot see them; they live on `Candidate.Hints`
and nothing reads them.

## Degraded fingerprints are claimable, but never prove continuity

A fingerprint with no process-birth identity still enrolls: §4.2 includes
process birth "when it is available", and one container holds one foreground
execution, so the claim still separates two live runtimes. The claim records
`Degraded`.

It cannot satisfy §4.4 rule 1. Same-live recovery requires process-birth
evidence, because the Herdr probe shows container coordinates surviving a
restart that the process did not
(`TestContinuityNeedsProcessBirth`). Nor can it satisfy reattach, which
revalidates against the exact claim the session holds.

## Attestation and the reporter credential

§3.4 admits three binding sources and no others. `Attestation.Source` is
those three; an external agent-session ID, transcript path, PID, or pane ID
has no representation as a source at all.

Enrollment and launch mint an instance-scoped credential, return it once, and
store only its SHA-256 fingerprint. A late `SessionStart` report reaches its
runtime instance by presenting that credential (`SourceInstanceClaim`), which
is §6.1's and §6.2's normal path. An unattested agent-runtime report is
**refused**, durably, rather than recorded as a weak correlation: §4.3's "Duo
must not guess" makes refusing the safe failure, and §7 wants rejected
attempts in history.

## Verb-to-boundary map

§4.2 fixes eight boundaries and the store exposes exactly those eight. The
domain uses four of them:

| Boundary | Verbs |
|---|---|
| `EnrollmentTx` | enroll, bind, detach, reattach, resume, restart, archive, restore, remove, and every recorded conflict or rejected report |
| `LaunchResolutionTx` | launch |
| `CommandAcceptTx` | stop |
| `ObservationTx` | exit, live verification, recovery decisions, late reports |

Two of those need a word. Archive, restore, and remove are not enrollments;
they take the enrollment boundary because it is §4.2's identity-and-lifecycle
boundary and the set of eight is closed. Stop takes command acceptance
because §5.2 makes stop a *request* — the command and delivery domain owns
its work item and its result, and this kernel records only the accepted
request and the `stop_requested` transition.

Exit takes the observation boundary because §5.3 makes exit an accepted fact
from process or host evidence, whatever verb records it. `Restart` therefore
demands explicit exit evidence for the instance it ends; it does not get to
assume the old process is gone because someone asked for a restart.

## Conflicts commit exactly one thing

§4.2 step 5 says to "record a conflict and leave the candidate unenrolled".
A refused enrollment therefore commits a conflict fact and an audit row and
nothing else — no session, no runtime instance, no correlation, no claim.
That is not a partial enrollment; it is the whole of what a conflict is
supposed to leave behind. `TestConflictingClaimEnrollsNothing` asserts both
halves.

Nothing merges automatically, ever. An owner resolves conflicts, and the
kernel has no verb that would let it do otherwise.

## The recovering view is not stored

§5.1 makes `Recovering` "a current view during authority startup", so it is
in-memory state derived at `Open` from any runtime instance that was not
terminal, and it is cleared by a recovery decision — except for
`RecoveryUnreachable`, which by §4.4 rule 4 keeps the reservation and infers
nothing. `Candidate` is likewise not a persisted state: `Discover` writes
nothing at all.

## What this step deliberately does not do

- **No adapter calls.** The kernel names no host or runtime; the Herdr shape
  appears only in test fixtures. Integrations supply `Fingerprint` values.
- **No observation ranking, condition, or usage.** Session 2 owns those; this
  step keeps only exit finality, which §5.3 assigns here.
- **No collaboration objects or inbox delivery.** Session 4 owns them. The
  agent actor exists here because §3.4 makes binding an identity rule, and
  because durable delivery has to survive sequential sessions.
- **No path rebind flow.** §3.3 requires an explicit, audited rebind when
  filesystem evidence cannot prove continuity. The fact kind exists
  (`workspace.rebound`) and replay handles it; the verb that decides *when* a
  rebind is warranted needs filesystem identity evidence, which is I/O, and
  belongs with the application layer.
- **No workspace tombstones.** Session removal keeps its tombstone (the
  record stays, in the `removed` state, so the ID is never reissued);
  workspace removal has no verb yet.
