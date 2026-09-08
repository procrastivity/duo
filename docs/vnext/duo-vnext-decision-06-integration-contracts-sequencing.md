<!-- Snapshot from the terminal-multiplexers archive at its 2026-09 freeze
     (handoff 27, repo-consolidation Stage B). Authored here from now on.
     Relative links that do not resolve in this repo refer to the archive;
     cite it by tag, not branch. -->

# Duo vNext decision 06: integration contracts and implementation sequencing

> Status: **locked by Session 6 on 2026-08-13.**
>
> Completion gate: **passed by architecture, conformance, and roadmap
> walkthrough.** Live implementation gates remain required before release.

## 1. Problem and boundary

Sessions 1 through 5 fixed the public identity, observation, command,
collaboration, projection, permission, error, and installation meanings. The
implementation now needs Go boundaries, adapter contracts, evidence policy, a
first product slice, and a safe build order.

This decision fixes those implementation contracts. It does not implement
product code, widen an adapter support claim, or change `duo.external/v1`.
External method names and IDs remain implementation details and correlations.

## 2. Prerequisites and evidence

All prerequisite decision notes exist and report passed completion gates:

| Prerequisite | Gate result | Session 6 use |
|---|---|---|
| Session 1 identity and lifecycle | Passed | Durable Duo session, final runtime instance, scoped correlations, optional terminal. |
| Session 2 channels and observations | Passed by contract walkthrough | Seven channels, ranked evidence, current views, semantic resume, separate terminal stream. |
| Session 3 control and delivery | Passed by contract walkthrough | Durable exact-target commands, human priority, effect certainty, no unknown-effect retry. |
| Session 4 collaboration | Passed by contract walkthrough | Fact authority, guarded objects, notification separation, mandatory live recovery gate. |
| Session 5 external projections | Passed by schema and fixture walkthrough | One external v1 contract, projection equality, Unix service, manifest, stamps, and Chat View gate. |

Session 6 repeated safe local probes on 2026-08-13. Herdr 0.7.5 exported the
same pinned schema digest. tmux 3.4, Claude Code 2.1.228, Codex 0.147.0,
OpenCode 1.18.16, and Pi 0.83.0 remain installed. Codex regenerated its
experimental app-server schemas with the recorded structured status and turn
shapes. Solo remains unavailable.

These checks verify versions and schema shape only. They do not upgrade live
ordering, effect, reconnect, or installation gaps into support claims. The
conformance plan gives every remaining claim a live probe and degradation.

## 3. Accepted specification set

The following focused specifications are normative parts of this decision:

- [`duo-vnext-go-architecture.md`](./duo-vnext-go-architecture.md) defines Go
  components, SQLite transaction boundaries, separate adapter roles,
  composition, local transport, and failure containment.
- [`duo-vnext-integration-conformance.md`](./duo-vnext-integration-conformance.md)
  defines records, fixtures, probes, supported-version policy, per-integration
  plans, the spawn-environment test, and degradation.
- [`duo-vnext-implementation-roadmap.md`](./duo-vnext-implementation-roadmap.md)
  defines stages, entry and exit gates, migration, rollout, observability,
  OwnPTY triggers, and protocol-owned triggers.
- [`duo-vnext-first-vertical-slice.md`](./duo-vnext-first-vertical-slice.md)
  defines the first cross-composition product and collaboration slice.

## 4. Accepted architecture

### 4.1 Components and storage

One Go binary starts one local authority. Its operation registry supplies the
CLI, MCP, local service, manifest, and generated metadata. Application
services call identity, observation, control, collaboration, and policy domain
components. Projections do not call adapters.

The initial authority store is local SQLite in WAL mode with one writer lease.
Repository interfaces keep SQL outside the domain and public projections. The
store commits the accepted record, idempotency decision, view, stream item,
audit link, and durable work identity in the logical transaction that requires
them.

External calls run outside store transactions. A durable work sequence records
acceptance, creates an attempt, calls one selected path, and commits its
qualified result. Recovery reconciles an in-progress attempt before retry.

### 4.2 Separate integration roles

A session-host adapter supplies host discovery, launch, attachment validation,
process lifecycle, terminal behavior, or host prompt paths. An agent-runtime
adapter supplies agent correlation, conversation, condition, usage, native
prompt paths, or harness rendering.

Both roles can offer an operation path candidate for one semantic operation.
They use separate Go contracts and registries. The composer selects one
candidate. It does not collapse the roles into one backend interface or
publish their method names.

Architecture tests compile and fake each role independently. A hostless fake
session remains a required test.

### 4.3 Path selection and diagnostics

The composer filters operation path candidates by compatibility, conformance,
exact target, freshness, policy, semantic preconditions, requested quality,
and allowed realization. It ranks eligible candidates by semantic coverage, effect
certainty, availability, freshness, and configured preference.

The selected path and rejected reasons enter privileged diagnostics. A support
view changes when its availability, quality, realization, source, constraints,
or revision changes. A client uses that view and never an integration-name
branch.

A path failure after a possible effect does not fall through to another path.
The command becomes an inspectable unknown-effect result until reconciliation
proves more.

## 5. Accepted conformance policy

Every adapter support claim cites one immutable conformance-record digest. The
record names exact external versions, protocol or format digests, channels,
operations, identity evidence, effect boundaries, limits, fixtures, live
probes, and degradation.

An exact tested version starts as `supported`. A recognized version without
required evidence is `unverified`. A contradictory version is `incompatible`.
A supported integration that cannot connect is `unavailable`.

An unknown newer version does not inherit support. A range expands only after
boundary versions and changed schema digests pass. Static manifest entries
describe packaged implementations and record digests. Runtime support reports
current composition and connection facts.

The initial live plans cover Herdr, Solo, tmux, Claude Code, Codex, OpenCode,
and Pi. OwnPTY keeps a reserved record. Every open claim has a live probe and a
weaker path or unavailable result. Solo does not block the first slice.

## 6. Accepted first vertical slice

The first slice uses Herdr 0.7.5 + Claude Code 2.1.228 and tmux 3.4 + Codex
0.147.0. These compositions differ in host semantics, runtime evidence,
prompt realization, and terminal fidelity.

One authority launches or enrolls both sessions. CLI, MCP, and Chat View list
the sessions and read normalized conversation. They also resume streams, show
condition and support, submit equal prompts, and observe delivery and activity.

The collaboration portion uses two agent actors and one human actor. It proves
nonoverlapping value writes, attributed document append, stale correction,
inbox durability, exact-object subscription, notification delivery, explicit
acknowledgment, causal suppression, bounded fan-out, and restart recovery.

The slice also proves `duo doctor`, two terminal grades, semantic reconnect,
unknown-effect containment, and no ordinary integration-name branch.

## 7. Accepted implementation sequence

| Stage | Result | Exit gate |
|---|---|---|
| 0. Executable contract | Go binary, operation registry, SQLite authority store, fixtures, manifest, doctor, fake adapters. | Transaction crash tests and three-projection equality pass. |
| 1. Identity and recovery | Herdr and tmux hosts, Claude Code and Codex correlations, launch, enrollment, scrubbed environment. | Both compositions keep correct IDs across restart and pass transcript-loss testing. |
| 2. Observation and streams | Conversation, condition, support, semantic resume, and terminal read. | CLI and Chat View consume both compositions with two proved terminal grades. |
| 3. Control | Durable prompt delivery, arbitration, attempts, effect certainty, and control projections. | Both paths pass idempotency, human-priority, crash, exit, and unknown-effect tests. |
| 4. Collaboration | Values, documents, inboxes, subscriptions, notifications, acknowledgments, and dead letters. | The full Session 4 live cross-composition gate passes. |
| 5. Release hardening | Full projections, gateway, harness installation, packaging, diagnostics, and resource limits. | Packaged live gates, security checks, and non-destructive install tests pass. |
| 6. Post-slice | Timers first, then trigger-driven features. | Each later feature has its own accepted contract and conformance gate. |

Each stage has a user-visible result or executable conformance gate. No stage
waits until release to test restart or external-effect safety.

## 8. Spawn environment

Every Duo launch builds an explicit child environment. The default policy does
not inherit a parent environment wholesale. Runtime-specific marker
classifiers remove known parent-agent and harness markers, including
`CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_*`, `CLAUDECODE`, and `AI_AGENT` as
applicable.

The mandatory test first launches a disposable Claude Code runtime with the
marker. It proves terminal warning, missing transcript, and missing expected
hook evidence. It then launches the same version through the scrubbed builder
and proves exact SessionStart binding plus one normalized transcript record.

The historical 2.1.224 probe proves the failure exists. The current 2.1.228
claim stays unverified until the stage test passes. A launch cannot claim
conversation support when the positive test fails.

## 9. Migration, rollout, and compatibility

vNext does not migrate an old Duo runtime store in place. It does not preserve
old public IDs, commands, permissions, capability names, or schemas.

`duo config migrate` can emit a proposed v1 configuration and report. Existing
external sessions can receive new Duo IDs through normal discovery and
enrollment. The first release does not import old mutable state, commands,
audit history, or collaboration data.

Rollout enables diagnostics first, read paths second, and exact-version write
paths only after their effect tests pass. Clients negotiate the external major
and feature strings. They do not infer operation support from the product
version. Old Duo and vNext can run side by side only with separate resources
and no shared live-runtime control.

## 10. OwnPTY and protocol-owned triggers

OwnPTY remains a future session-host adapter. Its product work starts only when
one of these accepted triggers fires:

- No acceptable external session host is available for a required deployment.
- A required presentation needs a structured terminal feed that available
  hosts cannot supply.
- A protocol-owned integration requires terminal service.
- Herdr risk becomes unacceptable.

The smallest revival shape is headless-only, single-pane, persistent,
read-mostly, and structured-snapshot capable. It keeps the terminal-query,
socket-security, process-lifecycle, and control-write duties. It must include a
narrow authorized blocked-permission escape hatch or require a non-prompting
permission mode.

A protocol-owned integration becomes a third adapter role, not a new public
Duo-session species. Work starts for a natively protocol-owned agent without a
viable maintained attach adapter, or for required typed semantics that justify
the ownership inversion.

Its record must prove process ownership, permission mediation, stream ordering,
cancellation, disconnect, mortality, and any resume claim. The session normally
dies with the protocol process unless a proved protocol resume contract says
otherwise. A terminal capability separately fires the OwnPTY trigger.

## 11. Decisive scenarios

### 11.1 Authority restart during a prompt

The command and attempt already exist before the external call. Recovery
revalidates the exact runtime instance and reconciles the attempt. It retries
only a proved no-effect attempt. A possible write becomes an unknown-effect
terminal result.

### 11.2 One host adapter fails

A malformed Herdr event opens the Herdr circuit and revises affected support
views. The tmux composition and durable reads remain available. No public error
or condition value changes its meaning.

### 11.3 Runtime evidence disappears

A missing Claude Code hook degrades its supplied condition edges. Host
process-exit evidence remains final. Transcript silence never becomes idle,
done, or exit.

### 11.4 Two different clients deliver prompts

CLI and Chat View submit the same canonical operation to different
compositions. Each durable command retains the same target, idempotency,
arbitration, state, error, effect, and evidence meaning. The adapters can use
different realizations.

### 11.5 Collaboration survives restart

An inbox entry, notification, delivery record, and prompt command keep separate
IDs. Restart resumes pending work without duplicating a logical record. An
unknown-effect prompt becomes dead-letter work and does not acknowledge the
entry or notification.

### 11.6 Hostless future session

A fake protocol-owned adapter creates a runtime instance without a terminal or
host attachment. The same Duo-session identity, condition, command,
collaboration, permission, and error contracts apply. Protocol mortality does
not revive an exited instance.

## 12. Rejected alternatives

| Alternative | Reason rejected |
|---|---|
| One broad backend interface for hosts and runtimes. | It combines host lifecycle with agent semantics and blocks hostless sessions. |
| Let each projection call adapters. | Validation, policy, idempotency, results, and errors would drift. |
| Use an in-memory store for the first slice. | Restart, effect certainty, notification durability, and stream barriers are acceptance requirements. |
| Hold a database transaction during an adapter call. | A slow or failed integration would block authority progress and still would not make the external effect atomic. |
| Support all newer external versions optimistically. | Perishable formats and behaviors can silently corrupt identity, state, or writes. |
| Make Solo a prerequisite. | No live Solo surface is available. The seam can remain mandatory without inventing evidence. |
| Build OwnPTY first. | No accepted trigger currently requires it, and two external hosts can prove the architecture. |
| Model ACP as a different public session. | The accepted Duo session already permits no terminal. The difference belongs in an integration role, permissions, and mortality. |
| Import the old Duo database and IDs. | vNext permits a clean break, and old records do not satisfy the new identity and effect contracts. |
| Retry through a fallback after a possible write. | A second path can duplicate the user-visible effect. |

## 13. Consequences and remaining triggers

The first code must establish the operation registry, durable store, fake role
seams, and crash tests before it builds a live adapter. This order makes the
public contract and effect boundaries executable from the start.

The main remaining risks are live evidence gaps. Herdr arbitration, stall
effects, tmux handoff, and source attribution need their planned probes.
Solo's surface and Codex ordering also need probes. The same rule applies to
OpenCode 1.18.16, Pi optional components, and Claude Code 2.1.228 markers.

These risks are non-blocking for this planning gate. A failed probe lowers
quality, selects an independent proved path, or makes the operation temporarily
unavailable or unsupported.
It does not reopen a public semantic contract unless it disproves that contract.

Timers advance after the Stage 4 recovery gate. OwnPTY and protocol-owned work
advance only on the triggers in Section 10. A newly required permission,
identity scope, effect meaning, or ordering guarantee requires a design review
before implementation.

## 14. Completion-gate assessment

**Passed by architecture, conformance, and roadmap walkthrough.** An
implementation team can start Stage 0 without new public or domain decisions.
The team also has fixed adapter roles, transaction boundaries, compatibility
policy, first compositions, and acceptance criteria.

Every initial integration has a record shape, fixture plan, live probe, and
degradation policy. The first slice covers two materially different hosts and
runtimes, both public clients, prompt delivery, collaboration, restart, and
diagnostics. OwnPTY and protocol-owned advancement triggers preserve both
future seams without making them initial implementation requirements.

The gate is a planning gate. The roadmap makes live adapter, Chat View,
collaboration, spawn-environment, security, and package gates mandatory before
release readiness.

## 15. Synthesis inputs

The synthesis pass treated these inputs as canonical:

- Decision notes 01 through 06.
- The Session 5 public, projection, installation, access, error, audit, and Chat
  View specifications and machine-readable schemas.
- The four focused Session 6 specifications in Section 3.
- The updated planning foundation, domain language, external-surface map, and
  integration matrix as provenance and status records.

The first implementation entry point is Stage 0 in the roadmap. The synthesis
record publishes the public domain contract before Go architecture and the
roadmap. It keeps live evidence gaps as conformance risks, not semantic
questions.

## 16. Amendments

**2026-08-14 (review follow-up).** Two wording corrections, with no semantic
change:

- Section 4 now qualifies bare "candidate" as "operation path candidate" per
  the glossary's qualified-use rule.
- Section 13 now says "temporarily unavailable or unsupported" for a failed
  probe's effect on an operation. The bare word "unavailable" named a
  compatibility state, not an operation-availability value.
