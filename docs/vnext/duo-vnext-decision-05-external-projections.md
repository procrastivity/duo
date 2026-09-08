<!-- Snapshot from the terminal-multiplexers archive at its 2026-09 freeze
     (handoff 27, repo-consolidation Stage B). Authored here from now on.
     Relative links that do not resolve in this repo refer to the archive;
     cite it by tag, not branch. -->

# Duo vNext decision 05: external projections

> Status: **locked by Session 5 on 2026-08-13.**
>
> Completion gate: **passed by schema and fixture walkthrough.** The roadmap
> retains live adapter gates before release readiness.

## 1. Problem and boundary

Duo needs one public meaning across CLI, MCP, presentation, configuration,
manifest, harness, permission, error, and audit surfaces. Surface-specific
syntax must not create different identities, prompt outcomes, concurrency
rules, or retry behavior.

This decision fixes the v1 external contract and local deployment boundary. It
does not choose Go packages, storage tables, adapter implementation order, or
release stages. The Session 6 specifications own those choices and live gates.

## 2. Prerequisites and evidence

Sessions 1 through 4 are normative prerequisites. Each decision note reports a
passed planning gate. Session 5 preserves their identity, observation,
command, stream, collaboration, and authorization meanings.

| Status | Evidence | Consequence |
|---|---|---|
| **Locked** | The installed Go product is the authored source of truth. Generated harness files use manifest and content stamps. | The system-wide `duo` binary owns manifest emission, generation, installation, and drift checks. |
| **Locked** | Chat View is a separate application on a normalized presentation boundary. | Duo owns local semantic service and policy. The trusted gateway owns browser security and assets. |
| **Locked** | Sessions 1 through 4 separate Duo IDs, correlations, facts, observations, views, stream positions, commands, notifications, and acknowledgments. | Public records keep those identities and scopes separate. |
| **Finding** | Filesystem skills are the common harness mechanism in the planning evidence. | Filesystem projection is the initial baseline for all four harness targets. |
| **Gap** | Solo live behavior, Codex live ordering, OpenCode harness installation, and the tmux snapshot handoff remain unverified. | The manifest can report packaged adapters and conformance digests. Runtime support stays degraded, unsupported, or unverified until live evidence exists. |

No gap can change a domain identity, guard, or result meaning. The gaps reduce
adapter claims. They do not justify an optimistic schema or a second operation.

## 3. Accepted specification set

The following documents are normative parts of this decision:

- [`duo-vnext-public-contract.md`](./duo-vnext-public-contract.md) defines the
  semantic and JSON wire contract, versioning, envelopes, records, and
  projectability.
- [`duo-vnext-projection-contracts.md`](./duo-vnext-projection-contracts.md)
  defines CLI, MCP, local presentation, streams, and deployment.
- [`duo-vnext-installation-contract.md`](./duo-vnext-installation-contract.md)
  defines configuration, manifest, assets, harness generation, stamps, and
  drift detection.
- [`duo-vnext-access-errors-audit.md`](./duo-vnext-access-errors-audit.md)
  defines permissions, enrollment, browser gateway duties, errors, and audit.
- [`duo-vnext-chat-view-contract.md`](./duo-vnext-chat-view-contract.md)
  defines the consumer contract and conformance suite.
- [`schemas`](./schemas) contains the five machine-readable core wire and
  fixture schemas.
- [`fixtures/duo-external-v1`](./fixtures/duo-external-v1) contains normative
  JSON examples for the cross-projection gate.

## 4. Accepted model and invariants

### 4.1 One versioned contract

The initial public schema family is `duo.external/v1`. JSON uses lowercase
snake-case fields, RFC 3339 UTC times, opaque IDs, and decimal-string revisions
or stream positions.

Compatible v1 changes can add optional fields, operations, codes, or values in
explicitly open vocabularies. A change to identity, guards, default behavior,
authorization, ordering, retry, or effect certainty requires a new major
contract.

Every projection returns the same success or error envelope. The projection
request ID remains separate from a command ID, fact ID, stream-item ID, or
acknowledgment ID.

### 4.2 Operation projection

The operation registry classifies each operation as `deterministic`,
`local_admin`, or `human_llm_porcelain`.

Deterministic operations can appear through CLI, MCP, and presentation forms.
Local administration stays on the CLI and separately enrolled local clients.
Human-only LLM porcelain stays CLI-only. MCP definitions and generated harness
instructions omit it by construction.

The CLI uses stable noun families and structured output. MCP uses stable
task-level tools plus authorized references for large content. The local
presentation service uses HTTP over a Unix socket, SSE for semantic streams,
and a separate terminal transport.

### 4.3 Configuration and manifest

Strict `duo.config/v1` YAML uses explicit layers. Duo does not load repository
configuration automatically. Configuration names portable agent definitions,
host launch variants, determined compositions, policy, grants, assets, and
projection targets. It does not become a runtime record or an ID authority.

The successor configuration family `duo.config/vN` (2026-08-18 handoff 18
amendment) adds a required composition `model_line` and an additive `presets`
root. A preset is a selector over determined compositions. It does not replace
those compositions as declaration atoms. Launch resolution is Duo-authored
pre-spawn evidence. It is not configuration, not selected configuration, and
not effective configuration. Migration never infers a model line and never
runs at daemon startup.

**2026-08-24 handoff 22 amendment (`duo.config/v3`).** The successor family is
now named and ratified (`notes/43-config-v3-change-control.md`;
`duo-vnext-planning-foundation.md` §6, same date). `duo.config/v3` retires
authored compositions as declaration atoms. Configuration declares
`session_hosts` policy (preferred kinds, per-kind enable, deduction sources),
`agent_runtimes` (kind, executable, base arguments), `launch_variants` (agent
runtime, required `model_line`, required `model_family`, optional provider and
appended arguments), and `presets` whose candidates target launch variants
directly. There is no `compositions` block, and no session-host instance or
socket path is authored anywhere. A composition is minted at launch resolution
as the variant × deduced-host join and is named only in the launch-resolution
record. Host instances, workspace↔host correlations, and provider toggles are
state, not configuration: configuration declares what can exist and how it is
built, and state records what is currently true. The require/avoid grammar has
three axes: `agent_runtime`, `model_line`, and `model_family`. `duo.external/v1`
grows by compatible adds only — the `model_family` axis value, the
`distinct_model_family` relation kind, the `provider_disabled` elimination
reason, the `model_family` result-leaf field, the `host_source` vocabulary, and
the stable codes `config.variant_unresolved` and `launch.host_unresolved`.
(2026-08-26 handoff 24 amendment; notes/51 record 6: `host_source` gained
`cwd-correlation`. Record 2: `session.launch` results grow `target` and
`target_source`.)
`config.composition_unresolved` stays registered and is deprecated on the same
date: it is emitted for `duo.config/v2` documents only, and
`config.variant_unresolved` is its declaration-ambiguity successor. Migration to
v3 reports `model_family` as `manual`, infers nothing, and still never runs at
daemon startup. Launch resolution is still Duo-authored pre-spawn evidence, and
it is still neither configuration, selected configuration, nor effective
configuration. `duo.config/v2` remains the shipped schema until the roadmap's
v3 stage gate passes.

The installed binary emits `duo.manifest/v1`. The manifest describes static
implemented contracts, packaged adapters, assets, schemas, tools, commands,
and renderer formats. It never contains current session capabilities,
connections, grants, or conditions.

### 4.4 Generated harness projections

Claude Code, Codex, OpenCode, and Pi receive a filesystem instruction
projection as the initial common component. A renderer adds hooks, MCP
configuration, or a small bridge only when a target-version conformance record
supports that component.

Each projection has a `duo.projection-stamp/v1` ownership record. The stamp
contains the Duo version, manifest digest, format version, target assumptions,
asset digests, and file digests.

Installation replaces only unchanged files that a valid prior stamp owns. It
never silently overwrites a user file, user asset override, or modified
generated file. `duo doctor` reports `current`, `missing`, `stale`, `modified`,
`incompatible`, or `unowned_conflict`.

### 4.5 Permissions, errors, and audit

Permissions name domain operations. Grants bind an authenticated subject to
opaque workspace, session, collaboration-object, or agent-actor IDs. Prompt
delivery, prompt release, terminal read, terminal input, session management,
workspace mutation, collaboration mutation, activation, acknowledgment,
diagnostics, and audit remain separate.

One error envelope carries a closed class, stable code, operation, target,
effect certainty, retry guidance, and safe details. CLI exit, MCP error status,
and HTTP status are transport mappings. They do not change the error.

Duo records security decisions and sensitive effects in a separate audit
history. Audit access and external diagnostic correlations use their own
permissions.

### 4.6 Browser and deployment boundary

The initial deployment uses one local Duo authority on a Unix socket and one
separate local app server on a random loopback port. The app server is the
trusted presentation gateway.

The gateway owns browser login, cookies, CSRF, origin and Host validation,
HTTP translation, terminal upgrade, rate limits, and static assets. Duo owns
the domain grant decision and audit record. A browser never receives a Duo
credential or connects directly to Duo.

The design defers desktop packaging and a remote personal-server mode. Remote
use must keep a trusted gateway beside Duo. SSH forwards the gateway. The
design does not support direct static-browser access.

## 5. Decisive scenarios

### 5.1 List, inspect, history, and subscribe

A caller lists sessions and receives an opaque Duo-session ID. Inspection
returns condition and support views. A conversation page returns completed
blocks and a stream barrier. The caller subscribes strictly after that barrier.

CLI, MCP, and presentation forms use different syntax. Each form produces the
same session ID, page, barrier, stream-item IDs, resume scope, and
cursor-expired behavior.

### 5.2 Prompt delivery

A caller submits one complete prompt, exact runtime-instance target,
idempotency key, finite expiry, and queue policy. Every projection creates or
replays the same durable command.

An `accepted` or `queued` result does not claim activity. Delivery, activity,
and acknowledgment arrive as separate records. An unknown-effect failure maps
to `indeterminate` and forbids automatic retry on all surfaces.

### 5.3 Terminal read

The caller reads operation support before it opens terminal read. The result
contains terminal fidelity and a snapshot reference. Incremental, repaint-only,
snapshot-only, and absent paths keep their Session 2 meanings.

Terminal input is a different operation and permission. Chat View can have
conversation and prompt access without terminal input.

### 5.4 Harness install and upgrade

The installed Duo binary renders a target into a staging directory and writes
an ownership stamp. After an upgrade, the manifest digest changes and `duo
doctor` reports `stale`.

Repair replaces only unchanged, owned files. A modified generated file or
unowned destination causes a visible conflict. A user asset override remains
untouched.

### 5.5 Browser denial

Duo exposes no TCP listener. A random browser cannot reach its Unix socket.
The local gateway rejects an invalid origin, Host, browser session, or CSRF
token before it calls Duo. The gateway credential remains server-side.

### 5.6 Shared concurrency conflict

A caller submits a stale shared-value replacement. Duo returns class
`conflict`, code `collaboration.version_mismatch`, effect `no_effect`, current
version when authorized, and action `reread`.

The CLI exits 4, MCP sets its error indicator, and presentation HTTP returns
409. Each surface carries the same Duo error value.

## 6. Rejected alternatives

| Alternative | Reason rejected |
|---|---|
| Give each surface its own result types. | Identity, conflict, retry, and audit meaning would drift. |
| Put live capabilities in the manifest. | Support depends on session composition, connection, policy, condition, and caller authorization. |
| Expose one generic MCP operation tool. | It would bypass typed operation bounds and make future privileged operations callable by name. |
| Generate LLM porcelain for an MCP caller. | The harness already supplies language judgment. Generated duplication would drift. |
| Bind Duo to loopback TCP for browser convenience. | Browser origin and credential risks belong in a trusted gateway. |
| Load workspace configuration automatically. | An untrusted checkout could change launch, policy, or control behavior. |
| Overwrite destination files with a force flag. | Duo cannot prove ownership of user-authored content. |
| Encode revisions as JSON numbers. | Long-lived counters can exceed exact integer ranges in common clients. |
| Freeze adapter-specific support claims into v1. | The known live evidence gaps would turn guesses into compatibility promises. |
| Capability-matched preset leaves. | Current session capabilities cannot live in the static manifest or in configuration. See §10. |
| Replace determined compositions with constraint-native open compositions. | It would replace the Locked scalar meaning of every composition and merge tighten-only layered policy with relenting request avoids. See §10. |
| Generalized requested/default precedence ladder as the launch organizer. | An unrelated default candidate can win before the requested pool relents, falsifying adversarial avoid-relent. See §10. |
| Configuration-only launch resolution. | A well-formed declaration is not proof of installed support and does not honor a disabled session host. See §10. |
| Live-state launch resolution. | Current reachability, processes, probes, and runtime observations make launch choice timing-dependent and mix resolution with selected or effective observation. See §10. |
| A `duo preset` noun family for resolve, list, or show. | Launch is a session operation. `--dry-run` previews that same operation. Configuration inspection stays on `duo config show`. |
| Project `session.launch` through the initial generated MCP tool set. | An in-session agent that can launch more agents creates a self-activation loop. G-12 keeps the operation `local_admin`. |

## 7. Consequences and remaining triggers

The Go implementation must generate JSON schemas, CLI metadata, MCP tool
schemas, route metadata, manifest entries, and fixture validators from one
operation registry where practical. Hand-maintained projection types require a
conformance comparison against the same canonical model.

The integration conformance plan and roadmap retain these evidence gaps and
gates:

- Probe Solo discovery, prompt boundaries, effect certainty, identity,
  terminal fidelity, and reconnect behavior.
- Verify Codex app-server ordering, replay, identity, blocked edges, and
  disconnect behavior against the selected version.
- Probe the current OpenCode filesystem, plugin, hook, and MCP installation
  paths before the manifest declares optional components.
- Prove the tmux snapshot-to-live handoff before it reports incremental
  fidelity.
- Run the live Chat View gate across two materially different compositions.
- Run the Session 4 collaboration recovery and conflict gate.

These gaps do not reopen `duo.external/v1` unless a probe disproves a domain
meaning. They can change adapter conformance ranges, runtime support, or
optional generated components without changing that schema.

## 8. Session 6 accepted inputs

Session 6 receives:

- Accepted schemas: `duo.external/v1`, `duo.config/v1`, `duo.manifest/v1`, and
  `duo.projection-stamp/v1`. The conformance case file uses
  `duo.projection-conformance/v1`.
- Compatibility rules: additive optional changes inside v1, closed semantic
  vocabularies, open stream and content kinds, and a new major for changed
  meaning.
- Projection rules: one operation registry, deterministic CLI/MCP/presentation
  mappings, local administration bounds, and LLM-porcelain exclusion.
- Conformance fixtures: the JSON set under `fixtures/duo-external-v1` and the
  three-encoder equality test in the Chat View contract.
- Security boundary: Unix socket Duo service, enrolled local clients, and a
  trusted browser gateway.
- Installation contract: installed-binary generation, filesystem baseline,
  ownership stamps, asset shadowing, and non-destructive drift repair.
- Remaining evidence gaps: Solo, Codex live ordering, OpenCode harness paths,
  tmux terminal handoff, and the two live cross-composition gates.

Session 6 chose the Go layout and implementation order without weakening the
accepted identities, guards, permissions, error meanings, or retry rules.

## 9. Completion-gate assessment

**Passed by schema and fixture walkthrough.** The representative external
surface scenario has one semantic value through CLI, MCP, and presentation
forms. The specifications trace list, inspect, history, subscribe, prompt, and
terminal read. They also map one collaboration conflict without changing its
meaning.

The installed product owns harness generation. The projection stamp makes
missing, stale, modified, incompatible, and unowned artifacts testable. User
assets remain separate.

The completion gate is a planning gate. Roadmap Stages 2 through 5 require the
live adapter, Chat View, control, and collaboration gates before release
readiness.

## 10. Amendments

**2026-08-18 (handoff 18: preset selection).** Additive `presets` select
determined compositions after each composition declares a `model_line`. The
successor configuration family is `duo.config/vN`. Validating `duo.config/v1`
documents are not silently reinterpreted. `session.launch` is registered as
`local_admin` with `session.manage`, omitted from the initial generated MCP
tool set, and required to complete launch resolution before any process
spawns. The ordinary result is a bounded per-leaf projection. The
launch-resolution record owns the full explanation. `duo.external/v1` gains
the operation and three new error codes under existing classes:
`preset.not_found`, `launch.constraints_exhausted`, and
`launch.no_eligible_candidate`. `config.composition_unresolved` still means
declaration ambiguity.

Rejected alternatives that carry reopen triggers:

- **Capability-matched leaves.** Reopen in a successor Matter only after a
  dated amendment permits current session capabilities in the manifest, or
  names a non-config, non-manifest evidence channel as launch-resolution
  input, or permits configuration to become a runtime record. The live
  operation-support view is not that permission.
- **Constraint-native compositions.** Reopen only after a dated successor
  schema and Locked amendment retract the determined-composition contract, or
  a new required scenario needs constraints, selection mode, and multi-leaf
  topology on one named object after the installation contract distinguishes
  non-relenting ceilings from relenting request policy, or migration evidence
  proves that preserving determined compositions plus additive presets cannot
  maintain referential integrity.
- **Generalized precedence ladder.** Reopen only if a new dated acceptance
  scenario requires a named default pool to win before avoid-relent, or one
  launch-time requirement must search more than the named preset, or a dated
  Locked amendment independently retires determined compositions and a
  successor schema defines merge, mixed-leaf fallback, and bounded search.
  Root retirement alone does not justify default-before-relent.
- **Configuration-only resolution.** Reopen only if the installation contract
  makes declaration validation sufficient proof of launch support and defines
  post-resolution launch failure as the sole availability boundary.
- **Live-state resolution.** Reopen only if a future requirement needs
  availability-aware scheduling among candidates and a dated contract change
  defines the authorized live evidence, freshness and snapshot boundary,
  deterministic tie behavior, privacy and diagnostics rules, and
  resolve-before-spawn atomicity.
