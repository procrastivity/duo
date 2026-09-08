<!-- Snapshot from the terminal-multiplexers archive at its 2026-09 freeze
     (handoff 27, repo-consolidation Stage B). Authored here from now on.
     Relative links that do not resolve in this repo refer to the archive;
     cite it by tag, not branch. -->

# Duo vNext first vertical slice

> Status: **accepted Session 6 end-to-end slice and acceptance contract.**

## 1. Objective

The first vertical slice proves one public contract across two materially
different session hosts and agent runtimes. It also proves one durable
collaboration reaction across those compositions.

The slice is complete only when all components use the same accepted semantics.
The components include the durable store, adapters, projections, Chat View,
and diagnostics.

## 2. Fixed compositions

| Label | Session host | Agent runtime | Purpose |
|---|---|---|---|
| A | Herdr 0.7.5 | Claude Code 2.1.228 | Rich native host prompt path, transcript and hook evidence, and repaint terminal read. |
| B | tmux 3.4 | Codex 0.147.0 | Conventional host, synthesized prompt, transcript or conformed app-server evidence, and an incremental terminal conformance target. |

Pin notation: Herdr 0.7.5 and tmux 3.4 are **pinned-tested** — recorded probes
ran against these exact versions. Claude Code 2.1.228 and Codex 0.147.0 are
**pinned-target** — each version is installed, but the recorded probes ran
against earlier versions. Claude Code probes ran on 2.1.226 and 2.1.227. Codex
transcript probes ran on 0.146.1, and only the 0.147.0 app-server schema shape
is verified. A pinned-target version needs its conformance record before a
supported claim.

**2026-08-23 dogfood amendment (handoff 20).** A dogfood milestone precedes
this slice and ends at the Stage 1 exit gate, not the slice completion gate.
Its compositions are:

| Label | Session host | Agent runtime | Purpose |
|---|---|---|---|
| A | Herdr 0.7.5 | Claude Code 2.1.228 | Unchanged from the slice table above. |
| B′ | Herdr 0.7.5 | Pi 0.83.0 | Second runtime on the same host: proves runtime abstraction and the hook-authoritative evidence path (integration matrix, "Herdr + Pi" row). |

Composition B (tmux 3.4 + Codex 0.147.0) is **deferred, not dropped**. Its
reinstatement duty: composition B's rows, including the tmux pane-respawn and
process-fingerprint gate items, must pass before the slice completion gate in
§9 is claimed. The dogfood milestone makes no cross-host generality claim,
and every cross-composition gate retains the Stage 0 fake-host +
fake-runtime pair while Herdr is the only real host. Pi 0.83.0 is
**pinned-target**: recorded probes ran against 0.83.0 for selected behavior,
and the review P6 refresh must produce its conformance record before a
supported claim. The two-composition scenario meaning below is unchanged;
for the dogfood milestone, read composition B as B′ for §4.1 and the Stage 1
subset of §5, and the remainder of this document stays scoped to the full
slice.

2026-08-23 re-pin (P4/P6/P7, notes/16, notes/18, notes/19): the dogfood
implementation targets the probed versions — Herdr 0.8.2 (protocol 20,
schema digest `c48f1f54…b150`), Claude Code 2.1.240 (evidence floor;
2.1.228 was never probed and is no longer installable), and Pi 0.83.0
(unchanged). Herdr 0.7.5 and Claude Code 2.1.228 rows above are
historical pins. Pinned probes set `DISABLE_AUTOUPDATER=1`.

**2026-08-26 delegation-loop amendment (handoff 25).** A second milestone
follows the dogfood checkpoint. It ends at a real dogfood day that
drives one handoff through launch → instruct → result-back, not at the
slice completion gate. Compositions stay A and B′. The Stage 2 and
Stage 3 work for this milestone is the subset in
[`handoffs/25-delegation-loop-scope.md`](./handoffs/25-delegation-loop-scope.md).
The remainder of this document stays scoped to the full slice. The
milestone makes no cross-host generality claim, and the fake-host +
fake-runtime pair stays in every cross-composition gate.

**2026-08-26 identity-bind amendment (handoff 26).** A third milestone
follows the sealed §3b gate. It ends when live launch binds
`agent.session` / `transcript`, advances `starting` → `live`, and the
CLI observe/send path works through those correlations. Compositions
stay A and B′. The work is the subset in
[`handoffs/26-launch-bind-scope.md`](./handoffs/26-launch-bind-scope.md).
The nested D1 orchestrator day stays a use-day after this milestone.
The remainder of this document stays scoped to the full slice.

The conformance record can lower a quality or terminal grade. It cannot change
the scenario meaning. If one fixed version becomes unavailable, the
implementation team records the replacement version and runs its complete
record before it changes this specification.

Solo is not a slice dependency because no current Solo binary is available.
OpenCode and Pi retain full conformance plans. OwnPTY and protocol-owned
sessions retain advancement tests but no initial implementation requirement.

## 3. Initial state

- One local Duo authority owns a new temporary store and Unix socket.
- An administrator enrolls one trusted gateway and one Chat View client.
- One CLI subject and one MCP subject have only their named grants.
- One workspace has two durable agent actors and one human actor.
- Composition A binds Agent A. Composition B binds Agent B.
- Both runtime instances use scrubbed spawn environments and exact reporter
  credentials.
- The tmux composition uses an exclusive Duo composer lease or an authorized
  release. The lease precondition holds: the pane's session runs detached, or
  attached only through a Duo-controlled client. Plain tmux does not claim
  that human input is observable.
- The Herdr composition has no composer lease until the writer-presence
  probe passes. Its queued prompts release only through an authorized manual
  release or the explicit heuristic release policy.

## 4. End-to-end flow

### 4.1 Start and correlate

1. The authority launches composition A and composition B, or enrolls a
   disposable host session that the test created.
2. Each result contains one workspace ID, Duo-session ID, runtime-instance ID,
   actor binding, host attachment, and time-bounded external correlations.
3. Repeating enrollment returns the same Duo session for the same live-runtime
   fingerprint.
4. CLI and Chat View list both sessions without exposing an external ID as the
   primary handle.

### 4.2 Read and resume

1. Each runtime produces one user block, one agent text block, one tool call,
   and one tool result in the disposable probe.
2. CLI and Chat View read normalized conversation pages and retain their
   barriers.
3. The clients subscribe strictly after the barriers. A forced disconnect and
   reconnect produces no lost logical record. Duplicate stream delivery
   deduplicates by stream-item ID.
4. Chat View shows the normalized condition, confidence, freshness, operation
   support, and terminal grade.
5. Composition A uses the proved repaint grade. Composition B reports
   `incremental` only after the tmux snapshot handoff probe passes. Otherwise it
   reports its weaker proved grade.

### 4.3 Deliver the same prompt command

1. The CLI submits one prompt to composition A. Chat View submits the equal
   canonical request to composition B.
2. Each request includes an exact runtime-instance target, caller-scoped
   idempotency key, finite expiry, `queue_until_safe`, minimum quality, and
   allowed realization.
3. Duo commits the command before the adapter attempt.
4. The Herdr path can use native realization after arbitration. The tmux path
   can use degraded synthesized realization only when the caller
   permits it.
5. Each client shows `accepted`, `queued`, or `attempting` without claiming
   activity.
6. Delivery and a later qualifying activity observation appear separately.
   No adapter acceptance, transcript output, or condition change becomes an
   acknowledgment.
7. Repeating each request with the same key returns the original command.

### 4.4 Collaborate and activate

1. Agent A and the human read one shared value at version 1.
2. Agent A changes `/review/status`. The human concurrently changes
   `/review/owner`. Both nonoverlapping guarded writes succeed.
3. Both agents append attributed text to one shared document. The human submits
   a correction from a stale version, and Duo returns the accepted conflict.
4. The human posts one immutable entry to Agent B's inbox.
5. An exact-object subscription creates one notification for Agent B. A
   notification delivery record resolves B's exact runtime instance and
   creates one prompt command containing references, not protected content.
6. Agent B follows the reference, reads the original inbox entry, and explicitly
   acknowledges the inbox entry and notification as separate operations.

### 4.5 Diagnose

1. `duo doctor` reports store, socket, adapter versions, conformance digests,
   schema compatibility, generated projection state, and current degradation.
2. An authorized diagnostic view can name Herdr, tmux, Claude Code, and Codex.
   Ordinary Chat View behavior does not use those names.
3. The static manifest describes implemented adapters and record digests. Live
   operation support remains in session views.

## 5. Required restart and failure cases

The slice repeats relevant steps with these fault injections:

| Fault | Required result |
|---|---|
| Authority stops after command acceptance and before attempt creation. | Recovery resumes the same command after target and arbitration checks. |
| Authority stops after attempt creation and before the external call. | Neither composition can reconcile the attempt. Recovery marks it `unknown_effect`, and the command fails as `indeterminate` with no automatic retry. A retryable result requires a conformance record that proves a no-effect check. |
| Connection fails after a possible prompt write. | The command fails with unknown effect and no automatic retry. |
| Target exits while a prompt waits. | The command fails against that runtime instance and never follows a replacement. |
| Reporter sends a late live condition after process exit. | Exit remains final. The report can enrich only valid pre-exit history. |
| Semantic client loses its connection. | Resume after the retained position is at least once and logically complete. |
| Semantic cursor expires. | The client reads a new snapshot and barrier. |
| Terminal delta has a gap. | The client receives a replacement snapshot and terminal epoch. |
| Adapter parser receives a malformed record. | Only that source degrades. The authority and other composition remain live. |
| Inbox entry and notification are pending at authority stop. | Both remain durable and resume without duplicate logical records. |
| Notification prompt proves no effect. | Bounded policy can retry the same delivery and command identity. |
| Notification prompt has unknown effect. | No retry occurs. Inspectable dead-letter work retains causal links. |
| Subscription would react to its own causal lineage. | Duo suppresses it and records the suppression. |

## 6. Spawn-environment case

Before the normal composition A run, the suite performs the negative and
positive transcript-loss test in the integration conformance specification.
The negative launch deliberately inherits `CLAUDE_CODE_CHILD_SESSION` and
must reproduce the silent semantic-channel failure. The positive Duo launch
must remove classified agent markers, bind SessionStart to the exact runtime
instance, and publish the test conversation once.

This test is a Stage 1 gate. A warning-only assertion is insufficient. The
test must inspect the hook and transcript results.

## 7. Projection equality

For list, inspect, conversation read, subscribe, prompt delivery,
collaboration read, guarded mutation, and acknowledgment, the test records:

- CLI arguments and JSON result.
- MCP tool arguments and structured result.
- Presentation request and JSON result.
- The decoded canonical request or result digest.

The three canonical digests must match for the same operation. Transport
status, CLI exit, and MCP error indication can differ only as the accepted
projection mapping permits.

## 8. Acceptance evidence bundle

One successful run retains:

- Duo build identity, schema digests, operation-registry digest, store schema,
  and configuration digest.
- External versions, protocol and format digests, adapter record digests, and
  generated projection stamps.
- Scrubbed fixture and live-probe IDs.
- Session, runtime-instance, actor, command, attempt, fact, notification,
  delivery-record, inbox-entry, and acknowledgment IDs.
- Stream barriers, resume results, terminal epochs, crash-point results, and
  degradation decisions.
- The integration-name branch static check for Chat View.
- Cleanup results for every disposable external resource.

Protected prompt and collaboration content does not enter the public evidence
bundle. Digests and authorized retained references preserve traceability.

## 9. Slice completion gate

The slice passes when every normal flow and fault case succeeds on packaged
binaries. The same public operations must work across both compositions. The
clients can react to support, quality, condition, authorization, cursor, and
terminal grade only.

The slice fails if it requires a public adapter method or moves a command to a
replacement runtime. It also fails if it retries an unknown effect, infers
acknowledgment, guesses a transcript, or requires a terminal for the domain.
