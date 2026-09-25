# Duo vNext decision 07: contract-native harness strategy

> Status: **accepted by owner interview on 2026-09-25.** Strategy and policy
> decision, not a public harness specification, live support claim, or release
> gate result. Contract state machines and a binding remain to be authored.

## 1. Boundary and precedence

The independently versioned public harness contract is Duo's **sole long-term
southbound integration surface for new agent harnesses**. A harness owns its
sessions and execution commands, runs headlessly without a required central
model-traffic relay, and can serve more than one Duo authority directly. A
first-party agent is a separately named, released, session-owning process in
its own repository; its exact name is deferred. Other harness authors do not
depend on Duo. Existing in-process host and runtime adapters can support a
transition but are not a permanent co-equal integration path.

Duo remains the authority for its own sessions, actor and collaboration
identities, launch/provider policy, client authorization, client-facing
commands, audit, and northbound `duo.external/v1` projections. The harness
contract does not reuse `duo.external/v1` as its wire or make Duo's Go types
normative. Duo correlates its session/runtime-instance/command IDs with
distinct harness session/incarnation/command IDs, scoped to the authenticated
harness instance and supported by continuity evidence. No external ID becomes
a Duo primary key; two Duos may issue distinct Duo IDs for the same harness
session. Each Duo's store has one local writer, **not** a lock on the shared
harness session.

This decision supersedes [decision 06](./duo-vnext-decision-06-integration-contracts-sequencing.md)
and its [Go architecture](./duo-vnext-go-architecture.md) where they prescribe
in-process host/runtime adapters, adapter-composer selection and the fixed
two-composition first slice as Duo's lasting southbound path. Their transaction
and effect-safety principles remain useful. The earlier
[implementation roadmap](./duo-vnext-implementation-roadmap.md) and
[first vertical slice](./duo-vnext-first-vertical-slice.md) retain historical
planning and completed milestone provenance, not a gate that can establish
contract-native release readiness. Decisions 01–05 and `duo.external/v1`
continue to govern Duo-owned meanings except where the cross-authority scope
is qualified below. This is an explicit strategy amendment, not a rewrite of
those dated records or a claim that current code has migrated.

## 2. Shared session safety and local ledgers

The **session-owning harness** is the authoritative shared per-session
coordination boundary. It atomically admits and deduplicates semantic writes
from authenticated Duo authorities and other allowed writers, establishes its
own per-session admission order, and fences the exact execution incarnation.
It checks operation-relevant preconditions at admission **and before releasing
queued work**, arbitrates writer and human safety, and refuses or holds a write
when it cannot prove safe control. Native transport or an apparent ready
condition alone is not that proof. This does not imply that admission order
proves downstream execution order. Duo checks and audits its own caller grants
before sending; a Duo grant does not override harness-side authorization or
shared revocation. No separate shared Duo-wide coordinator is required for
the first slice. Add one only for a concrete cross-session or global policy
that per-session harness coordination cannot express; it would not replace
harness execution fencing.

Each Duo durably owns a **client-facing command** with its own caller scope,
idempotency, audit, exact Duo target, deadline, and retry/reconciliation
responsibility. The harness independently owns the execution command and
downstream attempts. Duo durably records a stable harness submission key and
immutable canonical request before a call that could have an external effect;
it correlates the two commands without assuming an atomic transaction across
their stores. The harness key is scoped to an **authenticated durable Duo
authority** and one logical command, not a Duo process incarnation or network
attempt. It binds the operation, harness target and incarnation, semantic
payload, preconditions, and deadline. Same key and request must recover the
original harness command; changed request conflicts. Authentication, not a
caller-provided namespace string alone, establishes the authority scope.
An authority restored into two independent active writers cannot reuse one
namespace without fencing or re-establishing distinct identities.

For example, Duo A prepares and submits local command `D` using stable key
`(A, D)`. The harness admits command `H`, but the response is lost. Duo A
recovers by the **same key and canonical request**, then inspects `H`; it does
not create a fresh harness command or assume no effect. A prepared local
intent and its key must remain recoverable even if a crash falls between
harness admission and Duo's final local acceptance/correlation. The exact
staging and client response state machine belong to contract/implementation
design; it must not describe a possible harness effect as an unavailable
`no_effect` refusal. Harness acceptance, runtime admission, submission or
delivery, activity, and downstream effect remain separate boundaries. An
unknown downstream effect forbids a fresh automatic execution attempt, even
when harness admission itself is idempotent. Duo publishes only outcomes the
harness actually proves.

Two Duos' equal text or local keys are **independent intents**: `(A, D)` and
`(B, E)` may produce two effects. A manual retry through another Duo is not
deduplicated automatically. No content- or time-based deduplication is
permitted. The public contract must leave a compatible route to a future
explicit, authenticated shared-principal/client intent ID, under which two
local Duo commands could refer to one harness command, but the first slice
does not promise it. Global FIFO by Duo-local acceptance time and a globally
shared Duo command ledger are likewise **not** promised. The harness orders
its own admission and safe release; each Duo reports the scope of its local
queue and history honestly. A local `require_ready` decision cannot claim
shared readiness without an actual harness-side ready reservation.

## 3. Disconnection, authorization, and recovery

In the first slice Duo **refuses new semantic-command acceptance** when it
cannot reach the harness for authoritative current support, target and
admission. A refusal made before any possible harness write is audited and
reported as unavailable with `no_effect`. A connection lost *after* a
possible submission is not that case: Duo retains the stable key and reports
unresolved admission/effect honestly until inspection or reconciliation proves
more. Replaying an already-recorded Duo idempotency key is not new acceptance;
it returns that command's recorded status even while the harness is unreachable.
There is no optimistic offline queue or silent context rebase. An
already-accepted command retains its recorded expiry, recovery, and
reconciliation obligations. Before any later dispatch, recheck the exact
incarnation, operation-relevant original preconditions, grants, quality,
deadline, and shared safety; a changed context cannot be silently accepted
as renewed intent. `require_ready` requires authoritative reservation at its
acceptance boundary, not a stale Duo-local ready view.

Privileged or shared queued writes need **fresh, short-lived, verifiable
authorization at execution/release**. The harness holds or refuses when that
authorization expires or cannot be renewed and honors shared revocation.
Local grant revisions from different Duos are not a shared version clock.
A Duo-local revocation cannot be instant at an unreachable harness, and a
started effect cannot be undone. The maximum stale-authorization window and
renewal mechanism must be explicit, finite, and tested before this path can
claim support; **no numeric TTL is approved here**. Likewise, competing
one-shot interaction responses and safety leases need one harness-side
resolution/fence, not two Duo-local claims. Exact interaction policy and
operation registry remain public-contract work.

## 4. Lean adoption and cross-host scope

Preserve Duo's existing identity, fact/store transaction, command/attempt,
launch-policy, registry, and northbound projection work where it serves Duo
ownership. Add a pinned public-contract client at the application/southbound
boundary. Map Duo IDs and commands to harness-owned ones and consume proved
operation support and semantic observations; do not move Duo's in-process
adapter interfaces or `contracts/MANIFEST` into the public contract as-is.
Keep old adapters isolated for transitional use; do not expand them as the
lasting integration architecture. No existing user data or uncommitted work
is reset by this decision.

Before implementation, the independent contract repository authors semantic
state machines, schemas, fixtures, and a process-external black-box suite with
expected outcomes independent of the first-party agent. Check unlike
topologies before selecting a first binding. The separate first-party agent
then passes that same suite. Duo's lean first slice uses one contract client
for session creation, turn submission, observation, and reconnect against
that agent, with local command-to-harness recovery and cross-Duo race tests;
it does not branch on the harness name. A passing standalone agent alone
cannot certify an unlike runtime. Later per-runtime harnesses consume the
same pinned contract and report per-operation support honestly.

Remote control by multiple Duos of one harness-owned session is in scope for
the shared-safety contract; it is **not** migration. A remote Duo connects to
the owning harness under its own authenticated authority identity and local
client policy. Harness locators and stream positions must retain their owner
and ordering scopes; Duo cannot infer ownership, authorization, continuity,
or a globally routable address from `duo://local`, a path, or a bare session
ID. No browser credential or required model-traffic relay follows from this.
Cross-host migration/federation of session ownership is deferred: if later
required, it needs a separate continuity, incarnation, command-reconciliation
and fencing design rather than silently reusing an old execution target.

## 5. Open implementation details and gates

This decision does **not** choose a transport, first-party name, numeric
authorization TTL, key encoding, cross-Duo shared-principal namespace,
global collaboration replication, migration protocol, concrete state-machine
spelling, or the normative MUST/SHOULD/MAY strength of the research-side
step-1 proposals. Their owners decide them in later contract and product
steps. In particular, the contract must make accepted-command recovery and
key retention span the advertised retry horizon; an expired/dropped key may
not silently turn a retry into a new command. A Duo release cannot claim
support for a shared write until both stores' crash windows, shared writer
races, revocation expiry, failed renewal, and ambiguous downstream effects
have been exercised through the independent process-external suite and Duo
integration tests. Reject a stale incarnation, changed canonical request,
lost reply, changed precondition, human collision, and second Duo authority
with the outcomes promised above. Preserve Duo-local audit attribution and
avoid global FIFO, cross-Duo dedup, instantaneous revocation, or downstream
effect claims that the evidence does not establish.

## 6. Provenance and freshness

This Duo decision follows duo-lab matters
`contract-native-harness-invariants` and `contract-native-harness-packaging`,
the step-1 research artifact
`duo-lab:docs/2026-09-25-contract-native-harness-invariants.md` and step-2
packaging artifact
`duo-lab:docs/2026-09-25-contract-native-harness-packaging.md`,
published on duo-lab `main` at
`5434306e0a8c43cfbae8dd36b12c3934bdae0acc` on 2026-09-25. The latter
records the owner's independent-repo and release preferences, not an earlier
approval of this Duo strategy. The [revised roadmap](https://gist.github.com/simensen/feb57d17f55f6f787363f0bcd94be9a9)
supplies step 3. Historical witnesses include Herdr 0.8.2/protocol 20
(2026-08-23), OpenCode 1.18.31 (2026-09-19) and 2.0.12 (2026-09-22–23),
and Devin 3000.10.21 (pin dated 2026-09-15); none is a current supported
runtime claim. **The first implementation step re-verifies every live pin it
consumes** and records its date and supersession without rewriting older
evidence. No live probe ran for this strategy decision.
