<!-- Snapshot from the terminal-multiplexers archive at its 2026-09 freeze
     (handoff 27, repo-consolidation Stage B). Authored here from now on.
     Relative links that do not resolve in this repo refer to the archive;
     cite it by tag, not branch. -->

# Duo vNext design record

> Status: **canonical accepted design entry point as of 2026-08-13.**
>
> This record gives the reading order and accepted system shape. Each concept
> has one focused normative home in Section 2. Decision notes 01 through 06
> remain the provenance for the accepted design.

## 1. Product contract

Duo vNext is one local Go authority for durable interactive-agent sessions and
collaboration. The authority assigns opaque Duo IDs, accepts commands and
facts, normalizes external evidence, enforces policy, and publishes current
views. External hosts and agent runtimes remain authorities for their own
runtime facts.

The public contract has these invariants:

- A durable Duo session is the primary runtime handle. A process execution is
  one runtime instance. Process exit is final for that instance.
- Duo treats host IDs, process IDs, agent-session IDs, transcript paths, and
  workspace paths as scoped correlations. None is a primary Duo identity.
- A terminal and a session host are optional. A future protocol-owned runtime
  remains the same public Duo-session species.
- Conversation, condition, command, terminal, workspace, usage, and
  collaboration are separate semantic channels.
- Facts, observations, current views, stream items, commands, notifications,
  delivery records, and acknowledgments keep separate identities and
  lifecycles.
- One semantic operation keeps the same validation, authorization, result,
  error, effect, and audit meaning through CLI, MCP, and presentation forms.
- Runtime support is per operation. Availability, quality, realization,
  constraints, authorization, and terminal fidelity remain independent.
- Duo does not retry a possible external write unless it can reconcile the
  same idempotent attempt.

### 1.1 Identity and lifecycle

One workspace contains Duo sessions and collaboration objects. A Duo session
can have sequential runtime instances, one current host attachment, and one
bound durable agent actor. Enrollment atomically claims the strongest verified
live-runtime fingerprint. Weak or conflicting evidence never causes an
automatic merge.

Detach does not stop a process. Restart or resume creates a new runtime
instance when process birth changes. Authority restart can retain a
runtime-instance ID only when conformance evidence proves that the same process
execution remains live.

### 1.2 Observation and streams

Observations keep their exact runtime-instance target, provenance, confidence,
freshness, source time, receive time, and source order. Accepted process exit
ranks first. Equal-rank unresolved conflicts produce `unknown`. Last arrival
does not win by itself.

The closed session-condition values are `idle`, `working`, `blocked`, `done`,
`exited`, and `unknown`. Conversation publishes completed content blocks.
Semantic streams provide at-least-once delivery, stable item IDs, scoped resume
positions, snapshot barriers, and explicit cursor expiry. Terminal paint uses
a separate bounded stream and fidelity grade.

### 1.3 Commands and delivery

A durable command binds one semantic request to one exact target,
caller-scoped idempotency key, finite expiry, preconditions, queue policy, and
audit history. Its responsibility states are `accepted`, `queued`,
`attempting`, `delivered`, `expired`, `canceled`, and `failed`.

Prompt delivery requests one complete agent turn. It is not terminal input.
Attributed human input has priority, and a known human draft creates a hard
hold. Delivery, observed activity, command acknowledgment, notification
acknowledgment, and inbox-entry acknowledgment remain separate evidence.

### 1.4 Collaboration

The collaboration domain contains shared values, shared documents, inboxes,
timers, and subscriptions. Authoritative facts produce guarded current views.
Whole-object writes use exact versions. Nonoverlapping shared-value member
writes use path and container guards. Document appends and inbox posts accept
safe concurrent authority ordering.

The first collaboration slice includes values, documents, inboxes,
exact-object subscriptions, notifications, delivery records, explicit
acknowledgments, causal suppression, dead letters, and restart recovery. Timer
implementation follows the first-slice recovery gate. Tasks remain a later
typed product.

### 1.5 External contract and security

CLI JSON, MCP structured content, and the local presentation service use
`duo.external/v1`. Configuration uses `duo.config/v1`. The installed binary
emits `duo.manifest/v1` and owns stamped, non-destructive harness generation.

Duo serves enrolled local clients through a Unix socket. A separate trusted
gateway owns browser authentication, origin protection, HTTP translation, and
assets. Permissions name domain operations. Prompt delivery, terminal input,
session management, collaboration mutation, activation, acknowledgment,
diagnostics, and audit have separate grants.

## 2. Normative design map

This record summarizes the design. The following files are the single
normative homes for detailed rules:

| Concept | Normative home |
|---|---|
| Canonical vocabulary | [`duo-vnext-domain-language.md`](./duo-vnext-domain-language.md) |
| Identity, correlation, ownership, and lifecycle | [`duo-vnext-decision-01-identity-lifecycle.md`](./duo-vnext-decision-01-identity-lifecycle.md) |
| Channels, observations, views, streams, and terminal fidelity | [`duo-vnext-decision-02-channels-observations.md`](./duo-vnext-decision-02-channels-observations.md) |
| Commands, arbitration, effect certainty, and delivery | [`duo-vnext-decision-03-control-delivery.md`](./duo-vnext-decision-03-control-delivery.md) |
| Collaboration objects, concurrency, subscriptions, and notifications | [`duo-vnext-decision-04-collaboration-domain.md`](./duo-vnext-decision-04-collaboration-domain.md) |
| External-projection decision and provenance | [`duo-vnext-decision-05-external-projections.md`](./duo-vnext-decision-05-external-projections.md) |
| Public semantic and JSON contract | [`duo-vnext-public-contract.md`](./duo-vnext-public-contract.md) |
| CLI, MCP, presentation, streams, and deployment | [`duo-vnext-projection-contracts.md`](./duo-vnext-projection-contracts.md) |
| Configuration, manifest, assets, and harness installation | [`duo-vnext-installation-contract.md`](./duo-vnext-installation-contract.md) |
| Permissions, enrollment, errors, and audit | [`duo-vnext-access-errors-audit.md`](./duo-vnext-access-errors-audit.md) |
| Chat View consumer behavior | [`duo-vnext-chat-view-contract.md`](./duo-vnext-chat-view-contract.md) |
| Integration and sequencing decision and provenance | [`duo-vnext-decision-06-integration-contracts-sequencing.md`](./duo-vnext-decision-06-integration-contracts-sequencing.md) |
| Go components, storage, adapter roles, and effects | [`duo-vnext-go-architecture.md`](./duo-vnext-go-architecture.md) |
| Adapter evidence, conformance records, and degradation | [`duo-vnext-integration-conformance.md`](./duo-vnext-integration-conformance.md) |
| First cross-composition acceptance contract | [`duo-vnext-first-vertical-slice.md`](./duo-vnext-first-vertical-slice.md) |
| Implementation order and measurable gates | [`duo-vnext-implementation-roadmap.md`](./duo-vnext-implementation-roadmap.md) |

The JSON schemas in [`schemas`](./schemas) and examples in
[`fixtures/duo-external-v1`](./fixtures/duo-external-v1) are normative machine
artifacts for the external projection gate.

## 3. Integration promises and evidence

An adapter name does not promise runtime support. Every support claim must cite
an immutable conformance-record digest. The compatibility states have these
meanings:

- `supported` identifies an exact tested version.
- `unverified` identifies an unknown or incompletely tested version.
- `incompatible` identifies a contradictory version.
- `unavailable` identifies a supported but disconnected integration.

The planning program accepts the following conformance obligations. The
evidence links describe current knowledge. The plan links define the required
record and degradation before a runtime claim can become supported.

| Integration | Evidence | Required conformance record and degradation |
|---|---|---|
| Herdr | [Evidence matrix](./duo-vnext-integration-matrix.md#31-summary) | [Herdr plan](./duo-vnext-integration-conformance.md#61-herdr) |
| Solo | [Evidence matrix](./duo-vnext-integration-matrix.md#31-summary) | [Solo plan](./duo-vnext-integration-conformance.md#62-solo) |
| tmux | [Evidence matrix](./duo-vnext-integration-matrix.md#31-summary) | [tmux plan](./duo-vnext-integration-conformance.md#63-tmux) |
| Future OwnPTY | [Prototype evidence and triggers](./duo-vnext-integration-matrix.md#33-required-host-research) | [Reserved record](./duo-vnext-integration-conformance.md#64-ownpty-reservation) |
| Claude Code | [Evidence matrix](./duo-vnext-integration-matrix.md#41-summary) | [Claude Code plan](./duo-vnext-integration-conformance.md#71-claude-code) |
| Codex | [Evidence matrix](./duo-vnext-integration-matrix.md#41-summary) | [Codex plan](./duo-vnext-integration-conformance.md#72-codex) |
| OpenCode | [Evidence matrix](./duo-vnext-integration-matrix.md#41-summary) | [OpenCode plan](./duo-vnext-integration-conformance.md#73-opencode) |
| Pi | [Evidence matrix](./duo-vnext-integration-matrix.md#41-summary) | [Pi plan](./duo-vnext-integration-conformance.md#74-pi) |
| Cursor CLI *(candidate; 2026-08-15 amendment)* | [Evidence matrix](./duo-vnext-integration-matrix.md#41-summary) | No accepted plan yet. Draft record inputs: `notes/25-cursor-full-sweep.md` §6. A candidate carries no conformance obligation until a session accepts it. |
| Devin CLI *(candidate; 2026-08-29 amendment)* | [Evidence matrix](./duo-vnext-integration-matrix.md#41-summary) | No accepted plan yet. Draft record inputs: `notes/59-devin-full-sweep.md` §7. A candidate carries no conformance obligation until a session accepts it. |
| Amp CLI *(candidate; 2026-08-30 amendment)* | [Evidence matrix](./duo-vnext-integration-matrix.md#41-summary) | No accepted plan yet. Draft record inputs: `notes/63-amp-full-sweep.md` §7. A candidate carries no conformance obligation until a session accepts it. |
| Future protocol-owned role | [Accepted trigger](./duo-vnext-integration-matrix.md#8-deferred-protocol-owned-integrations) | [Architecture seam and mandatory proof](./duo-vnext-go-architecture.md#7-protocol-owned-integration-seam) |

Solo, OwnPTY, and protocol-owned adapters can remain outside the supported
release roster. Their absence does not change public semantics. Missing or
failed evidence selects a proved weaker path or reports the operation as
temporarily unavailable or unsupported.

## 4. Go architecture

One `duo` binary starts one local authority. An operation registry supplies
CLI, MCP, local-service, manifest, and generated metadata. Application services
call identity, observation, control, collaboration, and policy components.
Public projections never call adapters directly.

The first durable store is SQLite in WAL mode with one writer lease. A logical
transaction commits the accepted record, idempotency decision, current view,
stream item, audit link, and durable work identity that the operation needs.
External calls run outside database transactions through a durable prepare,
attempt, and reconcile sequence.

Session-host and agent-runtime adapters use separate Go contracts and
registries. A composer selects one conformed operation path for an exact target.
A possible effect prevents fallback to another path until reconciliation.

## 5. First vertical slice

The first slice uses Herdr 0.7.5 with Claude Code 2.1.228 and tmux 3.4 with
Codex 0.147.0. The two compositions differ in host semantics, runtime evidence,
prompt realization, and terminal fidelity.

CLI, MCP, and Chat View list and inspect both sessions, read normalized
conversation, resume semantic streams, show condition and support, and submit
equal prompt commands. The collaboration flow proves guarded value writes,
attributed document append, stale correction, and inbox durability. It also
proves exact-object subscription, notification delivery, explicit
acknowledgment, causal suppression, bounded fan-out, and authority restart
recovery.

The slice also proves two terminal grades, unknown-effect containment,
projection equality, a scrubbed spawn environment, and no ordinary
integration-name branch.

## 6. Implementation roadmap

| Stage | Accepted result | Measurable exit gate |
|---|---|---|
| 0. Executable contract | Operation registry, SQLite authority, fixtures, manifest, doctor, and fake adapters. | Transaction crash tests, one-writer test, hostless test, and three-projection equality pass. |
| 1. Identity and recovery | Herdr and tmux hosts, Claude Code and Codex correlations, launch, enrollment, and environment scrubbing. | Both compositions preserve correct identity across restart and pass transcript-loss tests. |
| 2. Observation and streams | Conversation, condition, support, semantic resume, and terminal read. | CLI and Chat View consume both compositions with two proved terminal grades. |
| 3. Control | Durable prompt delivery, arbitration, attempts, effect certainty, and control projections. | Both paths pass idempotency, human-priority, crash, target-exit, and unknown-effect tests. |
| 4. Collaboration | Values, documents, inboxes, subscriptions, notifications, acknowledgments, and dead letters. | The complete live cross-composition collaboration gate passes. |
| 5. Release hardening | Full projections, gateway, harness installation, packaging, diagnostics, and resource limits. | Packaged live gates, security tests, and non-destructive installation tests pass. |
| 6. Post-slice | Timers first, then trigger-driven features. | Each feature has an accepted contract and conformance gate. |

The exact work, entry conditions, and exit tests are in the
[`implementation roadmap`](./duo-vnext-implementation-roadmap.md).

### 6.1 First implementation entry point

Start at **Stage 0, “executable contract and authority store.”** Create the Go
module and one `duo` binary composition root. Implement the operation registry
and SQLite authority boundaries before any live adapter. The first gate is the
one-writer, crash-boundary, projection-equality, manifest, doctor, and hostless
fake-session test set.

## 7. Program completion gate

**Passed by synthesis on 2026-08-13.**

- The accepted glossary has no unresolved high-impact overloaded term.
- The locked decision ledger has no open item that can change Stage 0.
- `duo.external/v1` contains no session-host or agent-runtime name.
- Every initial integration has an evidence entry, conformance plan, and
  degradation policy.
- The Chat View contract has a static two-composition walkthrough. Stages 2 and
  5 require live cross-composition validation.
- The collaboration slice defines identity, concurrency, authorization,
  delivery, acknowledgment, causal limits, and restart behavior. Stage 4
  repeats the complete flow live.
- CLI, MCP, presentation, configuration, manifest, and generated harnesses
  derive from one operation and domain model.
- The Go roadmap has decision-complete entry conditions and measurable gates.

The gate closes the planning program. It does not claim release readiness or a
passing live adapter suite.

**2026-08-17 (handoff 16 amendment).** The runtime-configuration facet is
accepted as an additive, v1-compatible change. It adds a third built-in
optional facet delivered through the condition channel, a selected-
configuration current view, and immutable per-turn effective-configuration
records. It reopens no locked channel taxonomy and changes no Stage 0
condition; the above gate statements remain true.

**2026-08-17 (handoff 17 amendment).** Working mode is accepted as a second
namespace inside the runtime-configuration facet envelope. It reuses the
selected/effective split, keeps source values verbatim, and maps portable
classes only on named source evidence through a versioned vocabulary. It
merges working mode with no other mode namespace, adds no new channel family,
and adds no mode-switch command. The Stage 0 conditions remain true.

## 8. Non-blocking risks and change control

The remaining risks come from version-pinned conformance work. The
authoritative backlog is the
[integration matrix](./duo-vnext-integration-matrix.md): section 3.3 lists the
required host research, and sections 4.3 and 4.4 list the required runtime and
harness research. The headline items are:

- Herdr prompt collision, writer evidence, stall effect, reconnect, wait
  behavior, event resubscription, terminal classification, and server
  continuity.
- tmux snapshot-to-live handoff, source attribution, partial writes, server
  restart, linking, multi-client behavior, history limits, resize ownership,
  and enrollment policy.
- Solo discovery, identity, prompt, terminal, and restart behavior.
- Claude Code 2.1.228 transcript, hook, environment, and generated-layout
  probes.
- Codex 0.147.0 ordering, replay, identity, reconnect, blocked-edge, effect
  boundary, and harness-installation probes.
- OpenCode 1.18.16 fixture and harness refresh, plus Pi 0.83.0 optional
  component refresh.

A failed probe lowers quality, chooses an independent proved path, or makes an
operation temporarily unavailable or unsupported. Reopen the relevant semantic
decision only when new evidence disproves its identity, permission, ordering,
effect, or lifecycle contract.

The planning foundation, program, integration matrix, and all six decision
notes remain the provenance record. This canonical record supersedes the
historical [`duo-design-record.md`](./duo-design-record.md) for vNext.

**Portability rule (recorded 2026-08-14).** The vNext document set cites
`notes/*` evidence inline. `notes/09-review.md` requires a citation-freezing
pass before any design document moves out of this repository. That
requirement now covers the whole vNext set: before any of these documents
moves to the Duo repository, freeze or inline its `notes/*` citations. Until
that pass runs, the planning set stays in this repository.
