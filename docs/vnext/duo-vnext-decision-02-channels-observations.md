<!-- Snapshot from the terminal-multiplexers archive at its 2026-09 freeze
     (handoff 27, repo-consolidation Stage B). Authored here from now on.
     Relative links that do not resolve in this repo refer to the archive;
     cite it by tag, not branch. -->

# Duo vNext decision 02: channels, observations, and current views

> Status: **locked by Session 2 on 2026-08-12.**
>
> Completion gate: **passed by contract walkthrough.** A live implementation
> stage gate remains required.

## 1. Problem and boundary

Duo receives external evidence through sources with different semantics,
freshness, and reliability. Duo must turn that evidence into stable current
views without treating every report as a fact. Duo must also publish history
without confusing source order, view revision, and delivery position.

This decision defines semantic channels, conversation records, observations,
current views, operation support, stream behavior, and terminal fidelity. It
does not define Go packages, wire fields, HTTP routes, MCP tools, or final
serialization. Session 3 owns command arbitration and delivery outcomes.
Session 4 owns collaboration-object payloads. Session 5 owns public schemas.

Session 1 is a normative prerequisite. Its completion gate passed. This
decision preserves Duo-session identity, runtime-instance identity,
instance-scoped routing, active-claim uniqueness, and process-exit finality.

## 2. Evidence used

| Status | Evidence | Consequence |
|---|---|---|
| **Locked** | Session 1 made process exit final for one runtime instance. It also required authenticated, instance-scoped observation routing. | No observation can revive an exited instance or attach itself by working directory. |
| **Finding** | Herdr 0.7.5 remains installed. Its exported API schema has the same SHA-256 digest as `notes/05-herdr-schema-0.7.5.json`. | The recorded Herdr status and `pane.read` shapes remain version-pinned evidence. |
| **Prototype** | The Herdr Chat View adapter polls `pane.read` and emits a complete snapshot only when the pane revision changes. | The tested presentation path is repaint-only. It is not an incremental terminal stream. |
| **Finding** | A disposable tmux 3.4 probe returned a styled `capture-pane` snapshot and pane-scoped `%output` updates from control mode. | tmux can supply an initial screen and incremental output. A production adapter must still close the snapshot-to-live race. |
| **Finding** | Installed agent versions are Claude Code 2.1.228, Codex CLI 0.147.0, OpenCode 1.18.16, and Pi 0.83.0. | Older fixtures remain useful, but each adapter needs version checks and current fixtures before implementation conformance. |
| **Finding** | Codex 0.147.0 generated its current experimental app-server schema with thread-status, turn, item, approval, and token-usage notifications. Thread status distinguishes idle, active, waiting on approval, waiting on user input, and system error. | Codex now has a promising structured observation source. Schema generation verifies shape, not live delivery or ordering. |
| **Finding** | Claude Code 2.1.228 advertises partial stream messages only for print-mode stream JSON. | Token deltas exist in an agent-owned execution mode. They do not change the initial attached-session, block-at-once conversation model. |
| **Gap** | No Solo command is installed, and the planned Solo live-surface probe is incomplete. | Solo state, ordering, terminal fidelity, prompt submission, and reconnect behavior remain unverified. Duo must publish unavailable or degraded support instead of assuming the reported surface. |
| **Finding** | Claude and Pi forks copy prior source entries with identical entry IDs. | Source-entry identity needs a session and lineage scope. Cross-session deduplication would incorrectly erase valid fork history. |

## 3. Accepted channel model

### 3.1 Channel families

Duo keeps the seven channel families. A channel is a semantic flow between Duo
and an integration or domain participant. A channel is not a transport, public
stream, file, socket, or process.

| Channel | Direction at Duo | Accepted content |
|---|---|---|
| **conversation channel** | Into Duo | Agent-authored conversation records and content blocks. |
| **condition channel** | Into Duo | Instance-scoped observations about runtime lifecycle, activity, blocking, and external identity. |
| **command channel** | Out of Duo, with results returning to Duo | Semantic requests to an integration. Session 3 defines attempts and outcomes. |
| **terminal channel** | Both | Screen snapshots or deltas, resize requests, and separately authorized input. |
| **workspace channel** | Both | Worktree and VCS observations, with separately authorized mutations. |
| **usage channel** | Into Duo | Context occupancy, token use, cost, and service-window observations. |
| **collaboration channel** | Both | Duo-owned collaboration facts, commands, current views, and delivery observations. |

The direction describes the primary semantic payload. It does not hide
acknowledgments, errors, or diagnostics that return on a transport.

Workspace, usage, and runtime configuration are built-in optional facets.
Runtime configuration is delivered through the condition channel; it does not
add an eighth channel family (2026-08-17 handoff 16 amendment). Each facet's
meaning and support record are stable across integrations. A composition can
report any of them as unsupported or temporarily unavailable. Duo does not
require a general facet registration system for the initial model. A future
extension can add a namespaced facet without changing these built-in meanings.

### 3.2 Separation rules

These records remain distinct:

- A **domain fact** is a Duo-accepted change to Duo-owned durable state.
- An **observation** is external evidence received by Duo.
- A **current view** is Duo's normalized result from facts and observations.
- A **stream item** delivers a fact, observation, or view change to a consumer.
- Terminal repaint data represents a screen. It is not a conversation record.
- A command result is not evidence that an agent changed condition unless Duo
  receives a separate qualifying observation.

An adapter can derive a condition observation from terminal data. The derived
observation must identify its heuristic method. The terminal bytes do not then
become condition facts.

## 4. Conversation records

### 4.1 Semantic shape

A normalized conversation record has these semantic parts:

- A Duo-issued record ID scoped to one Duo session.
- The target Duo-session and runtime-instance IDs.
- The source agent-session and transcript correlations, when available.
- One author role and one logical source-entry identity.
- One or more ordered content blocks.
- Source time, receive time, source order, and provenance.
- A completion marker that states why Duo considers the record complete.
- Optional copied-history lineage.

The initial content-block vocabulary includes text, reasoning, tool call, tool
result, attachment or reference, and an opaque unknown block. An adapter keeps
unknown blocks so that schema drift is visible. A presentation can omit an
unknown block from ordinary rendering, but diagnostics must preserve its type
and provenance.

A source content block remains one normalized content block unless the source
specification proves that multiple fragments are one completed block. Duo does
not merge blocks because they arrived close together. Tool calls and tool
results keep their source correlation. Duo does not infer a result from later
text.

### 4.2 Delivery boundary

The initial model publishes a content block only when the adapter has evidence
that the block is complete. Conversation delivery is therefore block-at-once.
A long block can appear later than terminal paint from the same turn.

Token or character deltas remain outside the initial session-ownership shape.
A future integration can add partial content only after it defines ownership,
cancellation, replacement, ordering, retention, and reconnect behavior. A
print-mode or protocol-owned token stream does not silently weaken the block
contract for attached sessions.

### 4.3 Duplicate and copied history

Duo separates source deduplication from fork history:

1. Within one Duo session, a stable source key combines the source integration
   scope, transcript lineage, source entry ID, and source block locator.
2. Re-reading the same source key does not create a second conversation record.
3. A forked Duo session can contain copied parent entries. Duo includes those
   entries once in the fork's history and marks them as copied when ancestry is
   known.
4. Duo does not deduplicate copied entries across Duo sessions. Each fork has a
   valid conversation history of its own.
5. An aggregate view can collapse copied history by explicit lineage. It must
   not use matching text or timestamps as proof.
6. When an adapter cannot prove fork ancestry, it includes the entry once in
   the target session and reports lineage as unknown.

Stream-item IDs are delivery identities. They are not conversation-record IDs
or source-entry IDs. Replaying one record can create a new delivery attempt
without creating a new record.

## 5. Observation model

### 5.1 Semantic envelope

Every observation carries these semantic fields:

| Part | Requirement |
|---|---|
| Target | Exact Duo-session and runtime-instance IDs. An unbound report remains unresolved evidence. |
| Identity | A Duo-issued observation ID and the source's stable ID or sequence when available. |
| Kind and value | The external condition or measurement that the source claims. |
| Provenance | Integration instance, adapter version, reporter identity, source mechanism, and relevant correlation record. |
| Time | Source-observed time, Duo receive time, and source sequence or source revision when available. |
| Confidence | `reported`, `inferred`, `heuristic`, or `unknown`. |
| Freshness | A validity deadline and the derived state `fresh`, `stale`, `expired`, or `unknown`. |
| Scope | The transition or condition values that the source conformance record covers. |

`reported` means that an authenticated structured source directly states the
semantic value and its conformance record covers that transition. It does not
mean that every claim from that source is authoritative.

`inferred` means that Duo derives the value from evidence with different source
semantics. `heuristic` means that the method depends on timing, screen text, UI
patterns, or another fallible rule. `unknown` means that Duo cannot assign one
of the other grades.

Freshness is independent of confidence. A reported observation can become
stale. A recent heuristic remains heuristic. Receive time does not replace
source time, and last arrival does not win by itself.

### 5.2 Evidence ranking and conflicts

Duo ranks evidence by semantic authority, not by integration name:

1. An accepted runtime-instance exit fact is final.
2. A fresh, authenticated, instance-scoped direct report wins for the exact
   transition that its conformance record covers.
3. A fresh explicit source marker wins for the transition that the marker
   defines, such as a transcript turn end.
4. A fresh inference from multiple signals can supply a value when no stronger
   valid evidence conflicts.
5. A heuristic can supply an advisory value only when no stronger valid
   evidence conflicts.

Exact instance scope is a prerequisite, not a ranking bonus. An observation
that cannot prove its target does not enter a session's current view.

Within one rank, Duo prefers a source sequence over wall-clock arrival order.
Duo then uses source time and the declared validity interval. When two fresh
observations of equal rank conflict and neither supersedes the other, the
current condition becomes `unknown` with conflict diagnostics. Duo does not
use last arrival as an automatic tie breaker.

When stronger evidence becomes stale, Duo can select a newer lower-ranked
observation. The current view must expose the new confidence and the superseded
evidence. An expired observation cannot determine the current value.

### 5.3 Semantic current-view envelope

Every current view carries these semantic parts:

- Its Duo object and view kind.
- Its current normalized value.
- A monotonic revision scoped to that object and view kind.
- The facts and observations that determine the value.
- Confidence and freshness when the value describes external reality.
- Effective time, computation time, and any expiry deadline.
- Conflict, degradation, and unavailable reasons.

A current view does not copy a stream resume position into its revision. A
consumer can read the view without subscribing, and a stream can deliver a
change more than once without changing the view revision.

## 6. Session condition

### 6.1 Vocabulary

The accepted session-condition values are:

| Value | Meaning |
|---|---|
| `idle` | The current runtime instance is live, and available evidence says that it is waiting for a new turn. |
| `working` | The current runtime instance is live and is processing a turn or tool activity. |
| `blocked` | The current runtime instance is live and is waiting for an external decision or input that is necessary for the current turn. |
| `done` | A qualified source explicitly reports that the intended agent activity completed. The process can remain live. |
| `exited` | The current runtime instance has a final accepted exit fact. |
| `unknown` | Duo cannot select one current condition from valid evidence. |

`done` is not inferred from a quiet terminal or transcript silence. `idle` does
not mean that a task succeeded. `blocked` does not include an ordinary idle
composer. Connection loss is operation availability and observation freshness,
not a new agent condition.

### 6.2 Transition invariants

- Process exit changes the condition to `exited` for that runtime instance.
  No later observation can reverse the transition.
- A new runtime instance starts with `unknown`. It does not inherit the prior
  instance's current condition.
- `idle`, `working`, and `blocked` can transition among each other when newer
  qualifying evidence supersedes the prior evidence.
- `done` can transition to `idle` when a qualified source reports a ready
  composer. It can transition to `working` when a new turn starts. It can also
  transition to `exited`.
- A stale view keeps its last value and reports `stale` freshness. When its
  determining evidence expires, Duo recomputes the value. The result can be
  `unknown`.
- Transcript silence does not create an observation.
- When a source reports blocked entry but not blocked exit, its conformance
  covers blocked entry only. Another signal must release the condition.

### 6.3 Current condition view

The current condition view includes the selected value, view revision,
runtime-instance ID, determining observation references, confidence,
freshness, effective time, and conflict or degradation reasons. It can also
include non-selected evidence for privileged diagnostics.

The view never contains presentation activity wording. A presentation maps
`working`, elapsed time, and other normalized data to text such as a spinner
caption. The wording is presentation rendering, not agent-authored data.

## 7. Current-view revisions

Each independently readable current view has its own scope and monotonic Duo
revision. Condition, runtime configuration, selected working mode, operation
support, usage, workspace, and collaboration views do not share a revision
counter.

Duo increments a view revision when its externally visible semantic value,
determining evidence, confidence, freshness class, constraints, or degradation
reason changes. A duplicate observation does not increment the revision.
Crossing a freshness deadline can increment the revision without a new source
observation.

Revisions survive an authority-process restart. A recomputation after restart
keeps the revision when the semantic view has not changed. It increments the
revision when recovery changes the view. Revisions are comparison tokens for
one view scope. They are not stream resume positions.

## 8. Operation support and quality

### 8.1 Composed support

Duo publishes support for each semantic operation at the Duo-session level.
It evaluates each operation independently. Duo does not average the host and
agent-runtime integrations into one capability score.

Each operation-support view contains these dimensions:

| Dimension | Accepted values or meaning |
|---|---|
| Availability | `available`, `temporarily unavailable`, or `unsupported`. |
| Quality | `exact`, `degraded`, or `heuristic`. |
| Realization | `native`, `adapted`, or `synthesized`. |
| Source | The selected provider and any eligible fallback providers, for diagnostics. |
| Constraints | Current lifecycle, policy, fidelity, freshness, and mode limits. |
| Authorization | A separate decision for the current subject. Lack of a grant does not make an operation unsupported. |
| Revision | The monotonic revision of this operation's composed support, excluding the caller-specific authorization decision. |

`exact` means that the selected path meets the operation's semantic contract.
`degraded` means that it works with a declared semantic loss. `heuristic` means
that its result or safety depends on a heuristic. Native describes where an
operation runs, not how well it meets the contract.

Duo selects the best currently allowed path for one operation. A fallback can
change realization or quality without changing the operation name. Ordinary
clients branch on availability, quality, constraints, and authorization. They
do not test integration names.

An external projection joins the composed support view with the caller's
authorization decision. The support revision does not change when a different
caller reads the same operation. Authorization uses its own policy or grant
version.

### 8.2 Independent support example

An integration can support prompt delivery while it supplies no reliable
condition evidence. In that case:

- Prompt delivery can be `available` with its own quality and realization.
- The session condition can be `unknown`, stale, or heuristic.
- Duo can report a wait-for-condition operation as degraded, temporarily
  unavailable, or unsupported.
- The control contract requires a conservative queue policy when a safe
  release signal is absent.

The prompt operation does not disappear only because condition observation is
weak. The support view makes the missing evidence visible.

## 9. Semantic stream rules

### 9.1 Common delivery contract

Semantic streams use at-least-once delivery. Each stream item has a stable item
ID and a resume position within one declared ordering scope. Consumers dedupe
by stream-item ID. Domain-record IDs remain separate.

A snapshot-to-live read uses one Duo-owned barrier:

1. Duo reads a snapshot or history page through a declared stream position.
2. Duo returns that boundary with the snapshot.
3. The live subscription starts strictly after the boundary.
4. Source replay enters Duo's observation or fact store before stream
   publication, and Duo deduplicates the source replay there.

When a resume position remains retained, Duo replays the missing items in
order. When it has expired, Duo returns a cursor-expired result. The consumer
must load a new snapshot and boundary before it resumes live delivery.

No global sequence spans all streams. Cross-stream causal references use
domain IDs and revisions, not sequence-number comparison.

Duo assigns delivery order only after source normalization and deduplication.
Within a conversation source, explicit source sequence and causal links define
record order. When independent sources have no causal relation, Duo preserves
each source order and records a deterministic acceptance order without claiming
external causality. A resume position follows Duo delivery order, not a vendor
sequence.

### 9.2 Per-stream requirements

| Stream | Ordering and resume scope | Retention | Duplicate rule |
|---|---|---|---|
| Conversation | One Duo session. Runtime-instance boundaries remain visible. | Durable session history, subject to declared retention and redaction. | Stable session-scoped source key prevents parser replay. Copied fork history remains once in each fork. |
| Session condition | One Duo session. Items carry condition-view revisions, but the resume position is separate. | Current view plus retained transition history. Exit facts remain durable. | One item per published revision. Duplicate observations do not create revisions. |
| Runtime configuration | One Duo session. Selected-view items carry the facet-view revision; effective-turn records carry their source-record identity. The resume position is separate. | Selected view plus retained effective-turn history under the conversation retention rules. | One item per published selected-view revision. Effective records dedupe by source-record identity. |
| Working mode | One Duo session. The selected-working-mode view carries its own revision; effective-turn records carry their source-record identity. | Selected view plus retained effective-turn history under the conversation retention rules. | One item per published selected-working-mode revision. Effective records dedupe by source-record identity. |
| Operation support | One Duo session and semantic operation. | Current view plus a bounded change history. | One item per support revision. |
| Usage facet | One Duo session and runtime instance, with a named measurement scope. | Current value plus bounded samples or aggregates. | Source measurement identity or source revision. |
| Workspace facet | One workspace and named worktree view. | Current view plus bounded invalidation or change history. Durable VCS facts keep their own history. | Source revision or normalized change identity. |
| Command results | One command, with an optional Duo-session aggregate stream. | The control contract sets command retention and terminal outcomes. | Command identity and attempt identity remain separate under the Session 3 rules. |
| Collaboration | One collaboration object or subscription scope. | Durable by default under the collaboration retention rules. | Object fact identity and subscription delivery identity remain separate. |

Slow semantic consumers use bounded queues. Duo disconnects a consumer before
it silently drops ordered semantic items. The consumer then resumes from its
last accepted position or reloads after cursor expiry.

## 10. Terminal fidelity and stream behavior

### 10.1 Fidelity grades

Terminal fidelity is independent of operation quality and observation
confidence.

| Grade | Meaning |
|---|---|
| `incremental` | Duo can supply a coherent initial screen and ordered terminal deltas for one terminal epoch. Gaps force a new snapshot. |
| `repaint-only` | Duo supplies complete replacement screens when revisions change. Intermediate paint can be lost. |
| `snapshot-only` | Duo can read a coherent screen on demand but cannot supply a live change flow. |
| `absent` | No terminal read surface is available. |

Representation is orthogonal to fidelity. ANSI, structured rows, cell grids,
or another encoding can realize a grade. A structured snapshot is not an
incremental stream by itself.

The tested Herdr Chat View path is `repaint-only`. The tmux 3.4 probe supports
an `incremental` conformance target because `capture-pane` and control-mode
output both worked. A production tmux adapter must prove an atomic or repaired
snapshot-to-live handoff before it advertises that grade.

### 10.2 Terminal stream

Terminal data uses a separate stream with a terminal epoch and a sequence
scoped to one runtime instance and host attachment. A snapshot establishes a
base. Every incremental delta identifies that base or its predecessor. A gap,
resize ambiguity, host reconnect, or epoch change requires a replacement
snapshot.

Terminal delivery has bounded retention. Reconnect normally starts with a new
snapshot. Duo does not promise durable replay of terminal paint. A slow
repaint-only consumer receives the latest complete screen. A slow incremental
consumer receives a new snapshot after Duo discards unsafe deltas.

Terminal scrollback is a separate optional read operation. It is not
conversation history. Terminal input also remains separately authorized from
terminal read.

## 11. Required scenario walkthroughs

### 11.1 Hook reports blocked, transcript is silent, host reports working

An authenticated instance-scoped hook reports `blocked`. Its conformance
record covers blocked entry, so the observation is fresh and `reported`. The
transcript contributes no observation because silence has no meaning. The host
reports `working` through a screen rule, so Duo records a conflicting
`heuristic` observation.

The current condition is `blocked`, with the hook as determining evidence. The
host report remains visible in diagnostics. It cannot become authoritative.
When the hook cannot report blocked exit, Duo keeps `blocked` until another
qualified signal supersedes it or the evidence expires.

### 11.2 Process exits while a reporter has queued observations

Duo accepts the process exit as a runtime-instance fact and publishes
`exited`. Queued reports keep their source and receive times. Duo can add them
to pre-exit observation history when their source order supports that
placement. They cannot change the current condition or create a new live
claim.

### 11.3 A transcript fork copies prior entry IDs

The fork receives a new Duo-session ID and its own conversation records. The
adapter recognizes copied source entries from explicit fork lineage and marks
them as copied. It emits each entry once in the fork. It does not remove the
parent's records and does not duplicate records when it rereads the fork file.

When lineage is unavailable, Duo includes the entries once with unknown
lineage. It does not infer copying from text equality.

### 11.4 A presentation reconnects after condition updates

The presentation reconnects with its last session-condition resume position.
When the position remains retained, Duo replays every later view revision and
then continues live. Stream-item IDs make a repeated final item harmless.

When retention has expired, Duo returns cursor expiry. The presentation reads
the current condition snapshot and its boundary, then starts after that
boundary. It does not compare condition revisions with conversation cursors.

### 11.5 Herdr repaint data and a richer tmux path

Both compositions advertise terminal read under one semantic operation. The
Herdr composition reports `repaint-only`. A proven tmux composition can report
`incremental`. Chat View uses the same terminal component contract and changes
buffering behavior from the fidelity grade. It does not test `herdr` or `tmux`.

Terminal screen data remains separate from normalized conversation in both
compositions.

### 11.6 Prompt support without reliable condition evidence

The support view reports prompt delivery independently from condition
observation. Chat View can enable prompt submission when authorization and
prompt availability allow it. The condition view reports `unknown` or its
declared heuristic value. A wait or automatic queue release can be unavailable
or degraded.

The accepted control policy queues the command, requires authorized human
release when no other safe boundary exists, or rejects `require-ready`. Prompt
capability does not supply condition evidence.

## 12. Completion-gate contract walkthrough

The selected validation form is a contract walkthrough over two materially
different compositions.

| Presentation need | Herdr 0.7.5 plus Claude Code | tmux 3.4 plus Codex 0.147.0 | Chat View behavior |
|---|---|---|---|
| Conversation | Claude transcript adapter emits completed content blocks. | Codex transcript adapter emits completed content blocks. Current app-server events are a future adapted source. | Render normalized blocks by role and block type. Do not test agent names. |
| Condition | A qualified hook can determine blocked. Herdr screen or manifest status can remain heuristic. | Current Codex schema can report idle, active, and waiting flags when live conformance passes. Transcript inference remains a fallback. | Render value, freshness, and quality. Show unknown or stale data directly. |
| Terminal | Repaint-only through the tested `pane.read` adapter. | Unproved incremental target from snapshot plus control-mode output. | Select buffering and reconnect behavior from terminal fidelity. |
| Operation support | Each operation selects its Herdr or runtime path independently. | Each operation selects its tmux or runtime path independently. | Enable behavior from availability, quality, constraints, and authorization. |

The walkthrough passes because one presentation model renders both normalized
conversations and conditions. It also explains stale, heuristic, repaint-only,
and unavailable behavior without an integration-name branch.

Implementation conformance repeats this check as a live stage gate. The
gate must use at least two materially different compositions. It must include
one degraded condition source, one reconnect, and two different terminal
fidelity grades. Stages 2 and 5 place this gate before release readiness.

## 13. Rejected alternatives

| Alternative | Reason for rejection |
|---|---|
| Use one event stream and one global sequence for all domains. | It creates false ordering across records with different authorities, rates, retention, and resume needs. |
| Treat every integration report as a domain fact. | External sources can be stale, partial, heuristic, or contradictory. |
| Let the newest received condition always win. | Queueing, clock skew, replay, and late reports can reverse stronger evidence. |
| Give one integration name a permanent rank. | Authority differs by transition and version. Conformance must rank semantic evidence, not brands. |
| Treat transcript silence as idle or done. | A silent transcript can mean running, blocked, crashed, disconnected, or quiet. |
| Deduplicate copied transcript entries globally. | A fork legitimately contains the copied history. Global deduplication would erase the fork's context. |
| Put terminal frames on the semantic stream. | Terminal paint has different volume, retention, backpressure, authorization, and reconnect behavior. |
| Use `native` as the highest quality grade. | A native operation can have weaker semantics than an adapted path. Realization and quality are independent. |
| Add presentation spinner text to agent observations. | Agent runtimes do not supply stable activity wording. The wording belongs to rendering. |
| Enable token streaming for attached sessions because some owned modes expose deltas. | Owned token streams have different lifecycle, cancellation, and reconnect semantics. |
| Make workspace and usage arbitrary registered extensions initially. | Both have known cross-integration meanings and security boundaries. Built-in optional facets give clients one stable model. |

## 14. Consequences and remaining triggers

Adapters need conformance records that state which transitions they can report,
their confidence method, freshness policy, source order, and duplicate key.
Duo needs durable semantic history and view revisions before it can offer
reliable resume. Terminal adapters need an explicit epoch and resnapshot path.

These evidence gaps remain:

- Complete the Solo live-surface probe. Record observation schemas, terminal
  fidelity, prompt semantics, source ordering, and reconnect behavior.
- Run Codex 0.147.0 app-server notifications end to end. Verify ordering,
  replay, thread identity, blocked edges, and disconnect behavior.
- Refresh Claude Code 2.1.228, OpenCode 1.18.16, and Pi 0.83.0 fixtures for
  conversation, blocked, interrupt, exit, resume, and fork cases.
- Prove a race-free tmux snapshot-to-control-mode handoff before advertising
`incremental` in a product conformance record.
- Reverify Herdr terminal and status behavior against any version after 0.7.5.

A new source can add stronger evidence only for transitions that its live
conformance test proves. A new token source does not change block-at-once
delivery until the session-ownership and reconnect trigger fires.

## 15. Session 3 accepted inputs

Session 3 accepted and used these rules:

- Commands travel on the command channel and remain separate from condition
  observations and stream delivery.
- Operation support has independent availability, quality, realization,
  constraints, authorization, and revision.
- Missing condition evidence does not make prompt delivery unsupported. It can
  constrain arbitration, queue release, waiting, and outcome quality.
- Process exit is final. A command or late observation cannot revive an exited
  runtime instance.
- Command and condition streams have separate ordering and resume scopes.
- Activity observed requires a separate qualifying observation. Delivery does
  not imply activity, and activity does not imply acknowledgment.
- Heuristic evidence can never satisfy a precondition that requires reported
  evidence unless policy explicitly accepts heuristic quality.

## 16. Completion-gate assessment

**Passed by contract walkthrough.** The walkthrough normalizes conversation and
condition views across Herdr plus Claude Code and tmux plus Codex. It exposes
repaint-only, unproved incremental, heuristic, stale, unknown, and unavailable
behavior through semantic support data. Chat View never tests an integration
name for ordinary behavior.

The implementation roadmap requires the same check as live Stage 2 and Stage 5
gates. Solo remains a material evidence gap, but the gap does not require
an invented semantic contract. Solo must degrade through the accepted support,
freshness, and evidence rules until a live conformance record exists.

## 17. Amendments

**2026-08-17 (handoff 16: runtime configuration facet).** This decision
gains a third built-in optional facet, runtime configuration, delivered
through the condition channel. It does not change the seven channel
families. It adds one instance-scoped current view and one immutable
per-turn record family.

The facet keeps two states separate:

- **Selected configuration** is a current view scoped to one Duo session
  and runtime instance. It carries its own monotonic revision,
  determining observations, confidence, freshness, effective and compute
  times, and explicit unknown fields. It can hold a source alias while
  the served model changes per turn.
- **Effective configuration** is immutable historical evidence scoped to
  the exact turn, response, or conversation record that the source can
  prove. It records the configuration that actually served the turn and
  points at its source record. It never replaces source identity.

The field vocabulary is:

| Field | Rule |
|---|---|
| `source_provider_key` | The routing or provider backend key (`openai`, `opencode-go`, `nous`). Optional. |
| `source_model_key` | The exact source model key, always retained as an opaque string. Vendor-prefixed keys and embedded variants stay valid. Optional. |
| `vendor_key` | Optional model-creator identity, separate from the routing provider. |
| `source_display_name` | The source-owned user-facing name, when available. |
| `normalized_family` | An optional Duo interpretation. It names its mapping version and retains the source values. It never replaces source identity. |
| `context_limit` | A numeric context-window limit, present only when a source states it. Never inferred from punctuation such as `1m` unless a conformed parser proves that syntax for the exact source version. |
| `execution_modifiers` | Source values such as fast mode, service tier, extended context, or thinking enabled, plus optional normalized meanings. Never hidden inside a display name. |
| `source_effort` | The exact native effort or thinking-level value. Unknown native values are preserved, never erased. |
| `normalized_effort` | An optional portable interpretation with a vocabulary, mapping rules, and unknown behavior. `high` is not assumed equal across providers. |

Provider routing identity stays separate from model-vendor identity. A
source alias can remain selected while the effective model changes on
each turn. The model supports unavailable, stale, conflicting, and
partially known state without fabricating defaults.

Evidence, ranking, freshness, and the current-view envelope follow
Sections 5.1 through 5.3. The facet has its own revision counter under
Section 7 and its own stream row under Section 9.2. Effective-turn
records follow the conversation deduplication rule by source-record
identity. This change is read-only. It does not add a model-switch
command and does not imply a native configuration mutation contract.

**2026-08-17 (handoff 17: working mode).** Working mode becomes a second
namespace inside the runtime-configuration facet envelope, delivered
through the condition channel. It reuses the selected/effective split but
never merges working mode with model identity or effort.

- **Selected working mode** is a current view scoped to one Duo session
  and runtime instance. It carries its own monotonic revision,
  determining observations, confidence, freshness, effective and compute
  times, and explicit unknown fields. It can hold a compound selector.
- **Effective working mode** is immutable historical evidence scoped to
  the exact turn, response, or conversation record that the source can
  prove. It points at its source record.

A working-mode value carries `source_key`, `source_value`, optional
`components` (a list of `{source_key, source_value}` for compound
selectors), optional `normalized_class` (`{class, vocabulary_version,
evidence}`), and optional `source_display_name`. Source values are
preserved verbatim.

Portable classes are a versioned interpretation named
`duo.working_mode.v1`. `plan` maps only from named source evidence
(Codex `collaboration_mode.mode: "plan"`, OpenCode `agent`/`mode:
"plan"`). `review` is reserved pending a live Codex review-mode probe.
`act` is reserved. `default`, `build`, and `normal` stay source values
and are never mapped by assumption. No class is inferred from tool use.

Confidence, freshness, conflict, and unknown rules follow Sections 5.1
through 5.3. The selected-working-mode view has its own revision under
Section 7 and its own stream row under Section 9.2. Effective records
dedupe by source-record identity. This change is read-only; it adds no
mode-switch command.

**2026-08-14 (review follow-up, G-03: unresolved-evidence retention).** An
observation that cannot prove its target, including a report that arrives
before enrollment, becomes parked unresolved evidence. Duo retains parked
evidence durably under a configured retention window with bounded age and
count. When enrollment later binds a runtime instance whose correlations
match the parked report exactly, Duo binds the report retroactively as
pre-enrollment history. The bound report keeps its source time, enters
ranking normally, and derives freshness from that source time. A stale
bound report can enrich history without determining the current view.
Evidence that never matches expires. Expiry discards the payload and keeps
a counted diagnostic record, so `duo doctor` can report the loss.
Retroactive binding uses the exact-match rule from the Session 1 note. It
never merges sessions and never revives an exited instance.

**2026-08-14 (review follow-up, X-02: `done` is reserved on the first
slice).** No initial composition has a qualified completion source. `done`
is a reserved vocabulary value on the first slice. Both first-slice
compositions report `idle` after a clean turn end and never report `done`.
The value becomes reachable only through a credentialed reporter
convention or a protocol-owned typed turn result. This amendment does not
weaken the Section 6.1 definition. It records that no current source can
satisfy the definition.

**2026-08-14 (review follow-up, E-01: credentialed resident reporter).**
The Section 14 gap list gains this item: build the minimal credentialed
resident reporter for the spawned-command runtimes and register its
conformance record. Until it exists, no condition view can exceed rank 3
evidence (a fresh explicit source marker), and rank 2 of the evidence
ranking has no inhabitants. The reporter's terminal-state hooks must stay
synchronous, because asynchronous hooks lose events at process exit. The
same probe series tests the in-process credential shape on OpenCode or
Pi.

**2026-08-14 (review follow-up, G-25: fork and dedup evidence scope).**
The copied-fork rule rests on verified same-entry-ID copies for Claude
Code (`--fork-session`) and Pi (`--fork`) only. Codex fork behavior was
explicitly not tested, and OpenCode fork rows were never inspected. For
those two runtimes, per-session deduplication rests on the general
scoping rule alone until their fork probes run. Each conformance record
must verify fork-copy behavior before it claims copied-history lineage
support. This amendment also records a missing rationale: per-session
dedup deliberately overrode the Claude probe note's cross-file entry-ID
dedup recommendation. Global dedup would collapse forked histories that
this design treats as valid distinct sessions. Cross-file entry-ID
equality is lineage metadata, not a duplicate signal.
