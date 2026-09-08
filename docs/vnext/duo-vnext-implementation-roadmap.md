<!-- Snapshot from the terminal-multiplexers archive at its 2026-09 freeze
     (handoff 27, repo-consolidation Stage B). Authored here from now on.
     Relative links that do not resolve in this repo refer to the archive;
     cite it by tag, not branch. -->

# Duo vNext staged implementation roadmap

> Status: **accepted Session 6 implementation sequence.**

## 1. Sequencing rule

Each stage starts from a passing prior gate. Each stage ends with an executable
conformance result or a user-visible operation. A later stage cannot repair a
weakened identity, guard, permission, retry, or error contract from an earlier
stage.

(2026-08-26 handoff 25 amendment: the delegation-loop milestone in §3b
is an in-sequence exception of the same kind as the dogfood milestone.
It may start a Stage 2 subset and a Stage 3 slice without those stages'
full exit gates. It does not weaken the locked contracts. Full Stage 2
and Stage 3 remain before the slice completion gate.)

(2026-08-26 handoff 26 amendment: the post-launch identity-bind
milestone in §3c is a further in-sequence exception of the same kind.
It may start a Stage 2 identity-bind subset without the Stage 2 exit
gate. It does not weaken the locked contracts. Full Stage 2 remains
before the slice completion gate.)

The first implementation entry point is Stage 0. The first end-to-end product
slice is the cross-composition slice in
[`duo-vnext-first-vertical-slice.md`](./duo-vnext-first-vertical-slice.md).

## 2. Stage 0: executable contract and authority store

### Entry

- Sessions 1 through 6 have passed their planning gates.
- The normative schemas and external fixtures are present.

### Work

- Create one Go module and one `duo` binary composition root.
- Implement the operation registry and generate CLI, MCP, route, manifest, and
  fixture metadata from it where practical.
- Add the SQLite authority store, migrations, one-writer lease, transaction
  helper, durable work queue, semantic stream log, and audit envelope.
- Implement configuration validation, `duo manifest`, and the diagnostic core
  of `duo doctor`.
- Add fake host, fake runtime, and hostless protocol-owned test adapters.
- Validate all `duo.external/v1`, `duo.config/v1`, `duo.manifest/v1`,
  `duo.projection-stamp/v1`, and `duo.projection-conformance/v1` fixtures.

### Exit gate

- A second authority writer fails safely.
- Crash injection preserves every transaction boundary in the architecture
  specification.
- CLI, MCP, and presentation fixture encoders round-trip to equal canonical
  values.
- `duo manifest` reports schema and conformance digests. `duo doctor` reports
  the fake adapters and store state.
- A hostless fake session proves that the domain does not require a terminal.

## 3. Stage 1: identity, launch, enrollment, and recovery

### Entry

Stage 0 passes. The Herdr, tmux, Claude Code, and Codex probe recipes can run in
disposable namespaces. (2026-08-23 handoff 20 amendment: for the dogfood
milestone the entry set is Herdr, Claude Code, and Pi — review probes P7, P4,
and the Pi half of P6, consolidated by a scoped P12. The tmux and Codex
recipes join at composition B reinstatement.)

### Work

- Implement workspaces, Duo sessions, runtime instances, agent actors, host
  attachments, correlations, active claims, and lifecycle history.
- Implement the separate Herdr and tmux host adapters. (2026-08-23 handoff 20
  amendment: the dogfood milestone implements the Herdr adapter only; the
  tmux adapter defers with composition B.)
- Implement Claude Code and Codex runtime correlation and transcript adapters.
  (2026-08-23 handoff 20 amendment: the dogfood milestone implements Claude
  Code and Pi adapters; the Codex adapter defers with composition B.)
- Implement preset materialization, ordered and random selection, soft
  avoid-relent, require exhaustion, multi-leaf atomic assignment, and the
  pre-spawn launch-resolution record. Determined compositions remain launch
  atoms. Implement the scrubbed spawn environment. (2026-08-18 handoff 18
  amendment)
- Add local service and CLI list, show, enroll, launch, detach, reattach, and
  recovery paths. (2026-08-23 handoff 20 amendment: the dogfood milestone
  ships the CLI paths only. The live local presentation service defers past
  the dogfood checkpoint; Stage 0 projection equality already runs over the
  CLI, MCP, and presentation encoders against fixtures.)
- Author the launch, launch-resolution, and enrollment fixtures under
  `fixtures/duo-external-v1` before this stage's gate runs (2026-08-14 review
  amendment, G-13; 2026-08-18 handoff 18 amendment).
- Run the spawn-environment transcript-loss test.

### Exit gate

- Herdr + Claude Code and tmux + Codex each launch or enroll one disposable
  session and return opaque Duo IDs. (2026-08-23 handoff 20 amendment: for
  the dogfood milestone the pair is Herdr + Claude Code and Herdr + Pi.)
- Two concurrent sessions in one directory remain distinct.
- tmux pane respawn ends the old runtime instance and creates a new one only
  under explicit restart policy. Replacement detection composes tmux pane
  coordinates with a kernel process fingerprint recorded at enrollment. If
  the tmux probe cannot prove pane-process fidelity, the composition enrolls
  with unverified continuity and exact-target write paths stay unavailable.
  This row then passes only through explicit operator-driven restart policy.
  (2026-08-23 handoff 20 amendment: this tmux row defers with composition B
  and reinstates with it.)
- Authority restart preserves a proved-live runtime and quarantines conflicting
  claims.
- A late reporter cannot revive an exited runtime instance.
- The negative spawn test loses the transcript, and the scrubbed positive test
  publishes it once.
- Ordered and random preset resolution, model-line and runtime avoid-relent,
  require exhaustion, mixed-leaf and distinct-model-line fixtures, and a
  failed resolution that launches nothing, all complete before any host
  launcher prepares a process (2026-08-18 handoff 18 amendment).

## 3a. Stage 1v3: `duo.config/v3` late-bound session hosts

**2026-08-24 handoff 22 amendment.** This stage is added by the ratified
`duo.config/v3` design: `notes/42-config-v3-late-binding.md` as the worked
example, `notes/43-config-v3-change-control.md` as the adjudication, and the
`duo-vnext-planning-foundation.md` §6 clauses of the same date. It is placed
between the Stage 1 exit gate above and the dogfood checkpoint, so the
daily-driver configuration is authored against v3 rather than migrated twice.
`duo.config/v2` remains the shipped schema until this gate passes.

### Entry

- The Stage 1 exit gate passes.
- The normative artifacts exist and validate: `duo.config/v3`, the compatible
  `duo.external/v1` adds (the `model_family` axis value, the
  `distinct_model_family` relation kind, the `provider_disabled` elimination
  reason, the `model_family` result-leaf field, the `host_source` vocabulary,
  and the codes `config.variant_unresolved` and `launch.host_unresolved`), and
  the re-authored launch fixtures under `fixtures/duo-external-v1`.

### Work

- Implement the `duo.config/v3` loader and its strict validation: candidates
  target launch variants, `model_line` and `model_family` are required on every
  variant, there is no `compositions` block, and no session-host instance or
  socket path is authored.
- Implement `duo config migrate --to duo.config/v3`. Migration reports
  `model_family` as `manual`, infers nothing, and never runs at daemon startup.
- Implement the two standing fact families and their audited verbs:
  `workspace.host_bound` / `workspace.host_rebound` behind
  `duo workspace host rebind` and `duo workspace host show`, and
  `provider.disabled` / `provider.enabled` behind
  `duo provider disable|enable|list`, with default enabled.
- Implement launch materialization: M1 resolves the workspace and deduces the
  one host instance (explicit flag > workspace↔host correlation > ambient
  environment > policy default), recording the deduced instance, its
  `host_source`, and every captured-but-outranked piece of evidence. M2
  snapshots the standing provider facts. The cold-start first bind is hybrid: a
  bind whose `host_source` is `ambient-env` asks for confirmation, and binds
  from `explicit-flag` or `policy-default` write silently with loud output.
- Extend launch resolution: mint the composition as the variant × deduced-host
  join, add `provider_disabled` to the closed elimination-reason set with
  installed-policy semantics, re-key conformance evidence on (host kind, host
  version, runtime kind), carry the third require/avoid axis, and grow the
  exhaustion payloads with per-reason tallies, the deduced host, the evidence
  bundle references, and the pointer set. Avoid-relent still never crosses a
  hard static elimination, and the launch-resolution record still commits
  before any host launcher prepares a process.
- Name the deduced instance and its `host_source` in launch output and in
  `duo doctor`.
- Re-sync the contract tree in the implementation repository against the
  normative schemas and fixtures.

### Exit gate

- `duo config migrate --to duo.config/v3` migrates a `duo.config/v2` document.
  `--write` refuses until every variant carries an authored `model_family`.
  The migrated result then loads.
- A cold start in a fresh workspace, launched from a session-host pane, deduces
  the host at the `ambient-env` rung, asks for confirmation, writes the bind,
  and `duo workspace host show` and `duo doctor` agree on the result.
- A second launch in that workspace from an unrelated pane lets the correlation
  beat the ambient capture, and the output names the outranked capture.
- `duo workspace host rebind` to another instance records both the old and the
  new instance with their fingerprints.
- Disabling a provider exhausts a preset whose only candidates carry it, under
  `launch.no_eligible_candidate`, with per-reason tallies and the pointer set.
  Enabling the provider restores the launch.
- `--avoid model_family=<label>` and `--host <disabled kind>` behave exactly as
  the `fixtures/duo-external-v1` launch fixtures state, including the mixed
  case that reports step-3 tallies under `launch.constraints_exhausted` and the
  deduction failure that reports `launch.host_unresolved`.
- Herdr + Claude Code and Herdr + Pi each launch through deduction rather than
  through an authored socket path. The Stage 1 gate rows that touch launch
  re-run green, and the launch-resolution record still commits before any
  `HostLauncher.PrepareLaunch` call.
- The implementation build is green and the synced contract tree is clean
  against the normative schemas and fixtures.

(2026-08-26 handoff 24 amendment; notes/51.) This stage's historical work
bullets keep the four-rung ranking they were written against. The living
ranking is five rungs: explicit flag > workspace↔host correlation >
cwd-correlation > ambient environment > policy default. A first bind
whose `host_source` is `cwd-correlation` asks for confirmation. Kind
stanzas may name `launch_target` and `close_on_exit`. Close-on-exit is
the product default.

## 3b. Delegation-loop milestone

**2026-08-26 handoff 25 amendment.** This milestone follows the dogfood
checkpoint. It is recorded in
[`handoffs/25-delegation-loop-scope.md`](./handoffs/25-delegation-loop-scope.md).
It does not claim the Stage 2 or Stage 3 exit gates. Full Stage 2 and
Stage 3 remain before the slice completion gate.

### Entry

- The dogfood checkpoint (notes/47) has passed at feature scope.
- notes/51 dispositions are recorded. Ratified schema edits have synced
  into duo-vnext.

### Work

- Implement the Stage 2 subset: condition and completion observation,
  plus semantic transcript read, for Herdr + Claude Code and Herdr + Pi.
  Build on `session.inspect` attachments (notes/48–50).
- Implement the Stage 3 slice: durable prompt commands, caller
  idempotency, finite expiry, `queue-until-safe`, crash points at
  attempt create and before terminal commit, CLI projection only.
- Prefer Claude Code's messaging socket. Use Herdr `agent.prompt` as
  the host path. Pi uses that host path in this milestone.
- Land the notes/51 code debts that the launch path hits
  (`target` / `target_source`, `--remain-on-exit`, five-rung ranking,
  harness-dir reaping, doctor scrub-gate warning).
- Author a hand-authored interim skill in the planning repository.
  Install it by hand into Claude Code, then Cursor.

### Exit gate

- One real dogfood day drives one real handoff through the loop: a
  Duo-launched orchestrating agent, using the skill, instructs a
  builder session launched via preset, delivers the instruction
  durably, and observes completion and the result through Duo CLI
  surfaces.
- The two compositions show condition and transcript read on the CLI.
- A half-written human prompt in an attached pane holds automation
  when Duo has draft evidence. A Duo-created pane with no human attach
  may auto-release on launch-settled idle.
- Lost caller responses replay one command. Changed requests conflict.
- Delivery stays post-launch. Composer leases, tmux synthesis, MCP,
  and Chat View stay out.

**(2026-08-28 notes/58.)** The D1 use-day passed on operator
attestation with one named deviation: the orchestrating agent was a
human-started `claude` with the D5 skill, not `duo session launch
orchestrator`. Two agents, not three. See
[`notes/58-d1-use-day.md`](./notes/58-d1-use-day.md). Does not claim
the Stage 2 or Stage 3 exit gates.

## 3c. Post-launch identity-bind milestone

**2026-08-26 handoff 26 amendment.** This milestone follows the sealed
§3b gate. It is recorded in
[`handoffs/26-launch-bind-scope.md`](./handoffs/26-launch-bind-scope.md).
It does not claim the Stage 2 exit gate. Full Stage 2 remains before
the slice completion gate.

### Entry

- The §3b implementation gate has passed at named scope.
- `duo-delegation-loop` is sealed. This milestone does not reopen it.

### Work

- Decode Herdr pane `agent_session` identity (id and/or path) and the
  agent-record fields this bind needs (`launch_pending` and, if
  present, interactive readiness). Do not use the agent list as an
  inventory of existence.
- After spawn, present that identity as a `RuntimeClaim`, run existing
  `Correlate` for the transcript locator, write through
  `Authority.Bind` with `launch-plan` attestation, then `MarkLive` when
  D3 holds (bound identity and the pane past `launch_pending`).
- **2026-08-27 duo-pi-inject Stage B.** D3 is amended, not
  replaced. Default MarkLive remains bound identity and the pane
  past `launch_pending`. An additional ready arm is a
  runtime-offered idle signal (Pi: inject connect-line `idle`).
  Claude's D3 is unchanged. Do not MarkLive from `settledBirth`.
  Do not claim the Stage 2 exit gate. Live Herdr+Pi is Stage C,
  not this note.
- Keep `session show` honest while `starting`. Make `prompt send` wait
  or stay queued until live. Prefer implicit wait on send. An explicit
  `session.settle` lands only if that wait needs a named verb.
- Keep the fake-host + fake-runtime pair on the same bind path. Do not
  inject `MarkLive` as the only way tests become live.
- Do not install Duo reporter hooks. Do not bind the newest transcript
  in a directory. Do not add a fourth `BindingSource`.

### Exit gate

- Herdr + Claude Code and Herdr + Pi each launch via preset. The
  runtime instance leaves `starting` and becomes `live`. Active
  `agent.session` and `transcript` correlations are present on the
  instance.
- `duo session show` reports condition. `duo conversation list`
  returns transcript turns on both compositions.
- Post-launch `duo prompt send` delivers. Claude uses the messaging
  socket. Same idempotency key replays; a changed request conflicts.
- Fake-host + fake-runtime still pass every cross-composition test.
- `nix develop --command make check` is green, and
  `git diff --exit-code contracts/` is clean.
- Pi remaining `no_effect` after bind and ready is a named remainder,
  not a silent pass.
- The nested D1 orchestrator day is out of this gate. It is a use-day
  after this milestone ships.

## 4. Stage 2: observations, views, history, and streams

### Entry

Stage 1 identity and recovery tests pass. Required read-path conformance records
exist for both compositions. (2026-08-26 handoff 25 amendment: the
delegation-loop milestone may start its Stage 2 subset without this full
entry. It does not claim this stage's exit gate.) (2026-08-26 handoff 26
amendment: the post-launch identity-bind milestone may start its bind
subset without this full entry. It does not claim this stage's exit
gate.)

### Work

- Implement observation ingestion, provenance, confidence, freshness, conflict
  ranking, and current-view revisions.
- Implement the runtime-configuration facet (2026-08-17 handoff 16 amendment):
  the selected-configuration current view, the per-turn effective-configuration
  records, and the `session.runtime_configuration` stream. Preserve source
  model keys verbatim; never normalize vendor prefixes or embedded variants
  destructively.
- Implement the working-mode namespace inside that facet (2026-08-17 handoff
  17 amendment): the selected-working-mode view and the per-turn
  effective-working-mode records, with compound selectors and verbatim source
  values. Map portable classes only through the versioned vocabulary.
- Implement conversation normalization, deduplication, copied-history lineage,
  pages, barriers, and semantic reconnect.
- Implement composed operation-support views.
- Implement Herdr repaint terminal reads and tmux snapshot plus control-mode
  handoff. Report only the grade that the probe proves.
- Add CLI and local-service history, condition, support, wait, and subscription
  operations.
- Build the minimal credentialed resident reporter for the spawned-command
  runtimes, with synchronous terminal-state hooks, and register its
  conformance record. The slice's exact reporter credentials name this
  deliverable. Without it, rank-2 reported condition evidence has no
  inhabitants.
- Connect a minimal trusted gateway and Chat View fixture client.

### Exit gate

- CLI and Chat View list both compositions, read completed conversation blocks,
  resume semantic streams, and show normalized conditions.
- CLI and Chat View read the selected configuration and correlate a completed
  turn with its effective configuration, without parsing a display string.
- CLI and Chat View read the selected working mode and correlate a completed
  turn with its effective working mode, without conflating any other mode
  namespace.
- A hook, transcript, and host conflict follows the accepted ranking rules.
- Semantic cursor expiry causes a new snapshot. A terminal gap causes a new
  terminal epoch and snapshot.
- The two compositions show two proved terminal fidelity grades.
- The Chat View client contains no ordinary integration-name branch.

(2026-08-26 handoff 25 amendment: the delegation-loop milestone takes
only condition and completion observation plus semantic transcript
read. Streams, Chat View, the runtime-configuration and working-mode
facets, conflict ranking, the credentialed rank-2 reporter, and the
gateway stay in this full Stage 2.)

(2026-08-26 handoff 26 amendment: the post-launch identity-bind
milestone takes host-reported agent-session identity bind after spawn,
`MarkLive`, and the CLI observe/send path that bind unblocks. The
remainder of this stage stays in this full Stage 2.)

## 5. Stage 3: durable control and failure recovery

### Entry

Stage 2 support views and stream recovery pass. Herdr native prompt and tmux
synthetic prompt records define their exact quality and effect boundaries.
(2026-08-26 handoff 25 amendment: the delegation-loop milestone may start
its Stage 3 slice from the §3b Stage 2 subset. It does not claim this
stage's exit gate. tmux synthetic prompt stays deferred with composition B.)
(2026-08-26 handoff 26 amendment: the post-launch identity-bind
milestone does not add Stage 3 work. `--prompt` stays parked.)

### Work

- Implement durable prompt commands, caller idempotency, exact runtime targets,
  finite expiry, queue policy, human priority, attempts, and audit history.
- Implement Herdr native prompt and tmux safe-paste prompt candidates.
- Implement exclusive composer leases and authorized manual release. Lease
  issuance requires a host-verified no-human-writer precondition. Herdr
  issues no lease until the writer-presence probe passes.
- Implement stop, interrupt, terminal input, cancellation, and wait as separate
  operations and permissions.
- Add crash points before attempt creation, after attempt creation, after every
  possible write boundary, and before terminal transition commit.
- Project prompt delivery through CLI, MCP, local service, and Chat View.

### Exit gate

- CLI and Chat View submit the same canonical prompt command and observe its
  lifecycle on both compositions.
- A half-written human prompt holds automation. A native path cannot bypass the
  hold.
- Lost caller responses replay one command. Changed requests conflict.
- A proved-no-effect attempt can retry. A possible write becomes
  `unknown_effect` and never retries automatically.
- Target exit fails a queued command and never moves it to a replacement
  runtime instance.
- Delivery, activity, and acknowledgment remain separate in every projection.

(2026-08-26 handoff 25 amendment: the delegation-loop milestone takes
durable prompt commands, idempotency, expiry, `queue-until-safe`, the
named crash points, and the CLI projection. Composer leases, tmux
synthesis, MCP, Chat View, and stop / interrupt / terminal input stay
in this full Stage 3.)

## 6. Stage 4: collaboration first slice

### Entry

Stage 3 passes its restart and effect-certainty gate. Both agent actors have
one valid live binding.

### Work

- Implement shared values, shared documents, inboxes, exact-object
  subscriptions, notifications, delivery records, acknowledgments, and dead
  letters.
- Implement object versions, path tokens, container generations, attributed
  append, immutable inbox entries, idempotency, causal limits, and dynamic
  authorization.
- Project deterministic collaboration operations through CLI, MCP, and the
  local service.
- Author the collaboration fixtures (read, guarded mutation, and
  acknowledgment) under `fixtures/duo-external-v1` before this stage's
  projection-equality gate runs (2026-08-14 review amendment, G-13).
- Run the full live Session 4 walkthrough through both compositions.

### Exit gate

- Two nonoverlapping value-path writes succeed from one read. An overlapping
  write conflicts.
- Concurrent document appends retain attribution. A stale correction
  conflicts.
- An inbox entry and pending notification survive authority restart.
- A resumed actor reads and explicitly acknowledges the original entry.
- A no-effect delivery retries. An unknown-effect delivery becomes inspectable
  dead-letter work.
- Duo suppresses one causal self-reaction, and fan-out limits hold.
- No client branches on a host or runtime name.

## 7. Stage 5: installation, presentation, and release hardening

### Entry

The cross-composition control and collaboration gates pass. The public schema
has no implementation-discovered semantic contradiction.

### Work

- Complete the stable CLI, MCP tool set, Unix-socket service, trusted browser
  gateway, and Chat View conformance suite.
- Implement filesystem-first harness renderers, ownership stamps, atomic
  staging, drift detection, repair, and uninstall.
- Support the required baseline renderer for Claude Code, Codex, OpenCode, and
  Pi. Enable optional hooks, plugins, extensions, or MCP fragments only when
  their records pass.
- Add bounded resources, adapter circuits, health metrics, redacted structured
  diagnostics, audit export, backup, and restore tests.
- Run install, upgrade, compatibility, rollback, and package tests.

### Exit gate

- `duo doctor` reports store, socket, adapter, external-version, conformance,
  schema, asset, and projection-stamp compatibility.
- Install and repair never overwrite a modified or unowned file.
- Browser origin, Host, CSRF, credential, and permission-isolation checks pass.
- The complete Chat View and collaboration gates pass on packaged binaries.
- Unsupported Solo, OwnPTY, protocol-owned, and optional harness paths are
  honest in the manifest and runtime support.
- The release evidence bundle contains every conformance digest and retained
  live-probe result.

(2026-08-26 handoff 25 amendment: a hand-authored interim skill
substitutes for generated skill and MCP projections until this stage
runs. See handoffs/25 D5.)

## 8. Stage 6: timers and post-slice work

Timer implementation can start only after Stage 4 recovery passes. It must
implement the already accepted timer contract, including the Session 4
evidence-sufficiency arm gate: each selected session needs a conformed
level-read condition source at the timer's minimum confidence. Stage 6 entry
also requires one live demonstration of a sustained quiet interval on each
supported composition, at the confidence that the release configuration
uses. An authority outage breaks a quiet interval unless retained evidence
proves continuity. One arm generation fires at most once.

Text patches, array-element concurrency, task products, remote deployment,
OwnPTY, and protocol-owned sessions remain trigger-driven work. They do not
enter Stage 5 by convenience.

## 9. Old-Duo data handling

vNext makes a clean public and store break. It does not open or upgrade an old
Duo runtime database in place. It does not preserve old session IDs, command
IDs, capability names, public schemas, or adapter method names.

The supported aid is explicit and non-destructive:

- `duo config migrate` can read a recognized old configuration and emit a
  proposed `duo.config/v1` document plus a migration report.
- The command writes to stdout unless the operator names a new destination. It
  never replaces the source automatically.
- Operators can discover and enroll existing external sessions under new Duo
  IDs. Their stable external IDs and transcripts become scoped correlations
  and conversation sources after normal validation.
- A later explicit, idempotent importer can import historical transcripts. The
  first release does not import old commands,
  mutable state, permissions, audit records, or collaboration data.

An operator can run old Duo and vNext during evaluation only with separate
stores, sockets, reporter credentials, and generated projection roots. Both
authorities must not control the same live runtime.

## 10. Rollout and feature negotiation

The rollout uses these rules:

1. Ship Stage 0 diagnostics before enabling live adapters.
2. Require explicit configuration for each workspace, composition, and
   generated projection. Do not auto-enroll a checkout or tmux server.
3. Enable read paths before write paths for each adapter record.
4. Enable a write path only after its exact version and effect tests pass.
5. Keep experimental adapters and optional harness components out of the
   supported manifest range.
6. Freeze an implementation release candidate only after the Stage 4 and
   packaged Stage 5 live gates pass.

Clients negotiate the `duo.external/v1` major and open feature strings. They do
not infer support from a product version. Static manifest entries identify
implemented contracts and conformance digests. Runtime operation-support views
identify current availability, quality, realization, constraints, and caller
authorization.

An unavailable optional feature returns the accepted error or support view. It
does not change the meaning or default of an existing operation.

## 11. Observability and failure containment

The authority exposes local health and metrics for the store, work queue,
streams, adapter probes, and circuits. Metrics also cover observation
freshness, command age, attempts, notifications, dead letters, and projection
drift.

Each diagnostic record carries the relevant authority incarnation, session,
runtime instance, operation, command or fact, attempt, adapter, probe, and
conformance digest. Content and credentials remain redacted. Audit history and
operational telemetry stay separate.

An adapter crash, malformed record, or protocol mismatch opens only that
integration circuit. The authority stays available for unaffected sessions and
durable reads. Queue, stream, and terminal limits are independent. A storage
integrity failure stops new writes and permits only verified read-only recovery
operations.

## 12. Advancement triggers

### 12.1 OwnPTY

Start OwnPTY product work only when at least one trigger is true:

- A required deployment has no acceptable external session host.
- A required presentation needs a structured terminal feed that available
  hosts cannot supply.
- A protocol-owned integration needs terminal service.
- Herdr risk becomes unacceptable because of license posture, protocol churn,
  project stall, security, or operational reliability.
- Arbitration or condition-evidence quality on every available host stays
  below the product bar: the product needs attributed human input, interrupt
  visibility, or blocked-exit evidence, and no host or runtime source can
  supply it. Evaluate a Duo-controlled attach client before OwnPTY.

The first review evaluates the headless-only shape. It includes a real PTY,
single pane, session persistence, socket security, terminal-query answers,
structured snapshots, modest read-only screen updates, and control write. It
also includes the blocked-permission escape hatch from the conformance plan.

### 12.2 Protocol-owned integration

Start protocol-owned implementation work when either condition is true:

- Users require a natively protocol-owned agent for which no maintained
  attach-shaped or vendor adapter can meet the accepted contract.
- A required product experience needs typed permission, tool, or token-stream
  semantics that attached sessions cannot provide, and product owners accept
  Duo-owned process mortality.

Before implementation enters the roster, a version-pinned probe must prove
process ownership, initialization, identity, permission mediation,
cancellation, stream ordering, disconnect, mortality, and any resume claim.
The design review must accept the new permission grants and audit events. A
terminal capability invokes the OwnPTY trigger and does not smuggle terminal
authority into the protocol adapter.
