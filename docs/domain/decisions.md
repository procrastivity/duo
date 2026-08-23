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

## Step 22: authority restart, quarantine, and degraded continuity

Step 22 completed §4.4. The five recovery rules and the recovering view
already existed; what follows is what the step added and why.

### The authority incarnation is stamped by the kernel, not the store

§7 requires every fact to record the authority incarnation. The store mints
one per `OpenAuthority` and stamps it on **audit rows**, but the fact log is a
stream payload the store does not interpret, so before this step no fact
carried it: history could not answer "which authority run recorded this",
which is the first question §4.4 recovery raises.

`Fact.Incarnation` now carries it, stamped once in `changeBuilder.fact`. The
kernel does not mint it — it reads it back through
`domain.IncarnationReporter`, an **optional** interface the repository may
implement, rather than a fifth `Repository` method. Incarnation is a property
of the durable writer: the writer lease is what actually enforces one
authority per store, and a repository with no lease (a test double, a later
read-through implementation) has nothing honest to report. An optional
interface says that; a required method would force it to invent a value.

### Degraded continuity is one triple, implemented as one rule

§4.4 names five outcomes but leaves the consequence implicit: what may a
session still *do* while the host has not proven that the execution behind
its container is the one Duo claimed? The locked "Identity-evidence
degradation" ledger row and `duo-vnext-integration-conformance.md` §10 answer
it as a triple — mark attachment continuity unverified, park instance-scoped
reports as unresolved evidence, disable write paths that need an exact live
target — and `internal/domain/degraded.go` implements all three from one
durable fact. The three cannot drift apart, because the parking check and the
write gate both read the same `HostAttachment.Continuity`.

Which outcomes degrade: `unreachable` (rule 4), `replaced` (rule 3),
`conflicted` (rule 5), and a **refused** same-live proof. That last one is the
step's most consequential addition: a host that answers but cannot prove
process birth used to produce a bare error, which left no durable trace of the
degradation. It now records the finding and still refuses, resolving nothing —
the instance stays in the recovering view with its claim reserved.

Rule 4's "does not infer exit" and this degradation are not in tension. Rule 4
forbids inferring anything about the *runtime*; the instance state, the claim,
and the correlations all stay exactly where they were. What degrades is Duo's
own attachment evidence, which is a statement about what Duo knows rather than
about what the process is doing.

### Degradation is instance-scoped

`Continuity` is stored on the host attachment, as §10 words it, but it names
the runtime instance it is about (`ContinuityInstance`). That is load-bearing,
not bookkeeping. A degraded fingerprint can never re-prove continuity: rule 1
demands process birth, and a fingerprint that gains process birth is a
different claim key. §6.4 already says what happens next — an explicit restart
or resume — and a session-scoped mark would park the *new* instance's evidence
too, leaving a host that cannot report process birth with no way back at all.
Scoping the mark to the execution generation that lost continuity makes §6.4's
escape hatch actually work. `TestRefusedContinuityProofDegradesTheAttachment`
walks that whole path.

A session with no host attachment (the hostless case) has no host continuity
to lose and never enters the triple.

### Parked reports are durable and inert

A parked report is a `report.parked` fact carrying the whole offered payload.
Replay puts it in a per-session list and nowhere else: no correlation, no
claim, no binding. It keeps the scoped agent-runtime identity structurally
because decision-02's G-03 retroactive-binding rule matches on it; everything
else is evidence text.

Two entry points park: `Bind`, and a repeat enrollment that carries
agent-runtime evidence. `Bind` parks the **whole** request rather than only
its agent-runtime half, because §10 also forbids keeping "a live claim on
weaker evidence" and a bind that carried a fingerprint would do exactly that.
Host process-lifecycle evidence (`Exit`, `MarkLive`, `ResolveRecovery`) never
parks: that is the evidence degraded mode is waiting for, and rule 2 still
lets a host prove exit while its continuity is unverified.

Exit finality outranks parking. A report about an exited instance is recorded
as a late report and refused, whatever the attachment's continuity says.

**Not implemented, deliberately:** the retention window, the age and count
bounds, expiry-to-a-counted-diagnostic, and retroactive binding of a parked
report when a later enrollment matches it exactly. Decision-02 files those as
a Stage 2 ingestion concern; this kernel owns the identity half. The backlog
therefore stays inert after continuity is re-proven — the test asserts that
rather than leaving it undefined.

### One gate decides exact-target writes

`Authority.ExactTargetWrites` is the single place that answers whether
identity evidence permits a write to an exact live target. Its refusals are a
closed vocabulary (`WriteRefusal`) so a caller can map one to an error code
without matching on prose, and they are ordered broadest-first.

Continuity outranks the recovering view on purpose: an unverified attachment
is a durable finding about evidence that *was* examined, while recovering only
means no integration has answered yet. An unreachable host leaves both true,
and the caller deserves the finding.

The gate deliberately does **not** consider a detached attachment. Detach
disables Duo's attachment while "the external runtime can continue" (§5.2),
and choosing a delivery path — host-mediated, agent-native, none — belongs to
the control and delivery domain. This gate answers only the identity question.
It does refuse a `starting` instance: §5.1's starting is "launched, not yet
live", and an exact-target write needs the target to exist.

### Quarantine: sessions, not claims, and an owner verb to leave it

§4.4 rule 5 says Duo "quarantines both claims from automatic control until an
owner resolves the conflict". The quarantine is recorded per **session**. A
claim is not a control target; every verb the rule withholds is addressed to a
session, and the flag has to be readable by the thing that refuses control.

No claim is released. Releasing one would let the contested live runtime be
enrolled again, which is precisely the automatic merge §4.2 forbids. The
claims stay held and inert.

"Until an owner resolves" needed a verb, so `ResolveQuarantine` exists, and
§3.4 makes an explicit owner action its only admissible source. It lifts the
quarantine and proves **nothing** about the runtime: the conflicted sessions
also had their continuity marked unverified, so a resolved session still
cannot take exact-target writes until a host proves the same live execution.
Two independent gates, lifted by two different kinds of evidence — an owner
decides which session survives, and only a host can prove a runtime.

### What Step 22 did not need

No store migration and no store change: the new facts ride the existing
stream log, and the incarnation comes from the store's existing public
`Incarnation()`. Nothing outside `internal/domain` and
`internal/domain/storerepo` moved.

## The launch-resolution record: carried, never read

`docs/launch/decisions.md` recorded the gap this closes — `Authority.Launch`
committed through the right boundary but had nowhere to put the
launch-resolution record, so a domain-backed `launch.Recorder` would have had
to drop the record body or smuggle it through `Reason`.

The fact is `launch.resolved`, carrying `LaunchResolution{ID, Body}`. It is
emitted by `Launch` when `LaunchRequest.Resolution` is set, in the same
`Change` as `session.created`, `instance.started`, and `session.launched` —
one boundary, one transaction, §7.4's "a failed resolution creates no session
and therefore no launch-resolution record" enforced by there being nothing
else to fail.

**The body is opaque, and that is a rule rather than a convenience.** The
kernel stores the bytes it was handed and returns the same bytes; it does not
decode, validate, re-encode, or interpret them. Two reasons:

- The record's schema is `internal/launch`'s (§6.9), and the §3
  responsibility table forbids the kernel to own another component's format.
  A kernel that decoded the record would have to be edited whenever the
  resolver's record grew a field.
- Re-encoding would break the one property the fact exists for. The record is
  immutable evidence a spawn was gated on; a digest computed over it, or a
  replay compared against it, is only meaningful if the bytes are the ones
  that were committed. `wireLaunchResolution.Body` is therefore a `[]byte`
  (base64 on the wire), not nested JSON, so no encoder ever walks the
  document.

Immutability is enforced in three places, because "immutable" is otherwise
just a comment: no verb alters a record, replay keeps the **first** body
recorded for an ID, and every accessor returns a deep copy so a caller cannot
write through the returned slice into the index.

Half a record is refused. `ErrLaunchResolutionIncomplete` rejects an ID with
no body (evidence that was never written) and a body with no ID (evidence
nothing can cite), before the boundary opens.

### The links go on the fact, not in the body

§6.9 wants the record to carry its "eventual session/runtime-instance links",
and the body cannot: it commits in the same transaction that mints those IDs,
so they do not exist when it is serialized. The links live on the fact
envelope (`Fact.SessionID`, `Fact.InstanceID`) and are queryable through
`Authority.SessionLaunchResolution`; `launch.Launcher` fills the same links
into its own in-memory copy from the `Commit` the recorder returns, which is
what reaches the caller and the ordinary result. Nothing rewrites the durable
body afterwards, which is the point.

### The glue is its own package

`internal/launchrecord` holds the only `launch.Recorder` backed by the
kernel. It imports both `internal/launch` and `internal/domain`, and neither
imports it, so the resolver stays a pure decision component and the kernel
stays free of the record's schema.

Two shapes it forced:

- **The workspace path is a construction input.** `launch.Recorder`'s method
  takes the record and nothing else — placement is not part of a resolution
  (§3.3: a path is a placement input, never an identity) — so one `Recorder`
  belongs to one workspace root, which is the granularity a `launch.Launcher`
  is built at anyway.
- **`Options.OnLaunch` exists for the reporter credential.** `launch.Commit`
  is a fixed contract carrying only the session and instance IDs, while
  `domain.Launch` also returns the instance-scoped reporter credential the
  kernel returns exactly once and never stores in the clear. Without a way
  out it would be dropped at the seam, so a caller that will accept a later
  `SessionStart` report takes it through this callback.

One known narrowing: the kernel's launch verb mints **one** starting runtime
instance, so a multi-leaf resolution reports one instance ID in its `Commit`
today. Per-leaf executions correlate to it as they report in. A record whose
assignment covers several leaves is committed whole regardless — the body is
the resolver's, complete.

This step likewise needed no store migration: the new fact rides the same
stream log, through the `LaunchResolutionTx` boundary `launch` already used.
