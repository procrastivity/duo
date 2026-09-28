# Own-agent-first increments toward Duo's adopted harness profile

**Decision (2026-09-28):** The owner chose the middle-ground own-agent route,
incrementally, rather than making Devin, OpenCode, or Amp qualification a
prerequisite to building an agent. This amends *implementation order*, not the
accepted [Session 6 first vertical slice](duo-vnext-first-vertical-slice.md),
the semantic contract, or a current runtime support claim. The original
two-composition, collaboration, and three-projection acceptance gate remains
open. The owner-adopted Duo consumer profile (WIP matter
`first-public-binding-duo-slice-profile`, 2026-09-26) remains the target for
the own-agent loop: base observations, control,
binding-owned `session.create`, `events.follow`, and S1 writer policy. The
public contract's `AHC-STEP6-2026-09-26-R1` decision in
`agent-harness-contract/decisions/2026-09-26-first-public-binding.md` still
DECLINES a first public binding.

An own agent is a separate headless session-owning process with its own
durable store, not a Devin wrapper, a renamed Duo adapter, or a fabricated
conformance driver. Its repository/name, process binding, and external
model/tool credentials are not selected by this decision. Choose a provisional
private process-external binding for development and pin the contract snapshot
it implements; neither that binding nor the agent's own tests select a public
wire or prove unlike-topology conformance. Duo remains the controller and
retains `duo.external/v1` northbound. Do not equate a Duo `session.launch` or
`prompt.deliver` field with the harness contract operation of a similar name.

## Checkpoints, each with a runnable exit

1. **Boundary and bootstrap.** Select the separate working repository/name,
   private test transport and contract-source pin. Run a headless process from
   a disposable home/store with an external client; establish authenticated
   owner identity and `protocol.describe` without claiming other operations.
   A fake/suite-private driver is not evidence of an agent process.
2. **Create and observe.** Persist owner-scoped `session.create` under its
   original authority/key; return the same session across an identical retry
   and process restart, and reject a changed request under that key.
   Implement `command.inspect` by ID and original authority/key alongside
   this optional write, plus `session.inspect` and per-operation,
   authority/target-scoped `support.inspect`. Test with an independently
   started client/process and inspect the durable record after restart.
   No turn or follow claim yet.
3. **One useful turn and inspect.** Implement `turn.submit`, immutable
   turn commands using the established `command.inspect` path, and a bounded
   `conversation.snapshot`. Test a deliberately lost reply after admission,
   target-incarnation replacement, and an independently observed downstream
   effect: never resend an attempt of unknown effect. First use a deterministic
   disposable worker to test the lifecycle; a real model-backed turn is a
   distinct, credential-scoped gate and is required before calling this a
   useful agent. Do not represent the deterministic worker as that gate.
4. **Resumable reads.** Implement and test `events.follow`: strict
   post-snapshot barrier, reconnect, deduplication, explicit gap/expired
   cursor recovery, and restart. A snapshot alone does not claim replay.
5. **S1 shared-writer policy.** Implement finite `queue_until_safe` with
   admission/release guards and an actual mediated ordinary human/native
   writer path; changed writer state must hold or expire, not infer safety
   from silence. Test restart and ambiguous effect without automatic retry.
   Do not claim safe release until the writer boundary is proved.
6. **Duo consumption and wider validation.** Exercise the claimed profile
   through Duo without an integration-name branch. Run process-external
   conformance against independently derived expectations and the later
   unlike-topology review before any public-binding or full-profile verdict.
   Revisit the original Session 6 two-composition acceptance gate separately.

Each checkpoint reports **only its tested operations and caller scope**.
Unimplemented or unwitnessed operations are unclaimed/UNVERIFIED; no fake
worker, single caller, unit test or self-owned store proves cross-authority
isolation, ordinary external writer fencing, a public binding, or the full
Session 6 slice. Two independent authorities and exhaustive third-party
native qualification are deferred, not declared impossible. The first two
owner PATs are one principal, not two. No new credential, provider turn,
session mutation, live probe, or deployment is authorized by this sequence.

**Checkpoint 1 result (2026-09-28):** A provisional local Git repository at
`~/Code/duo-agent` (initial commit `0d53e19`, private
`github.com/procrastivity/duo-agent` remote) contains the
separate Python process and a private newline-JSON/Unix-socket development
binding. Its README pins the contract-source digests. Two process-external
tests passed: stable owner ID across restart, single live owner, token refusal,
unsupported operations, and unsafe directory refusal. An independent
`jsonschema` check accepted its authenticated `describe` record. It advertises
`profiles: []`; no session, turn, event or model call occurred. Checkpoint 2
was next. The bootstrap is pushed to the private remote; cloning Duo or
duo-lab alone does not include the separate agent repository.

**Checkpoint 2 result (2026-09-28):** `duo-agent` local commit `f034d27`
(not yet pushed) persists the original owner/authority/key and immutable
create command with its session. Five process-external tests covered a lost
reply, changed-key meaning, SIGKILL/restart, and a mismatched owner file.
`command.inspect`, `session.inspect`, and operation-scoped `support.inspect`
are available to the single local development caller. Eight result/refusal
records passed an independent pinned-schema check. This is not a complete
observation profile or a multi-authority claim.

**Checkpoint 3 result (2026-09-28):** `duo-agent` local commits `c612f8b`
and `9202b08` (not yet pushed) add a deterministic disposable worker, durable
`turn.submit` and original-key inspection, exact incarnation replacement on
restart, bounded frozen `conversation.snapshot` pages, and an opt-in
OpenCode Go model worker. Ten offline tests passed; the observed fixture
effect followed by owner SIGKILL remained `unresolved`, without reissue or a
fabricated snapshot item. A separate disposable process-external turn used
the owner-authorized OpenCode Go `deepseek-v4.1-flash` route: one coding prompt
returned a correct Python `clamp` function and expected boundary examples,
command inspection reported delivery, the private worker marker matched the
two-record snapshot, and the records passed pinned-schema validation. The
runner's `GET /zen/go/v1/models` returned that ID; singular `/model` returned
404. The key file was narrowed locally from mode 0664 to 0600 before use;
no key value was logged or committed.

Provider routes are explicitly registered and selected per session; two
different registered key files and missing-route refusal were tested without
network calls. Only this one model adapter is implemented: there is no
multi-turn context, tools or human-writer policy, no replay, and no proof of
another provider type. `profiles: []` remains truthful. One useful model turn
closes *only* checkpoint 3. The official
[Devin model documentation](https://docs.devin.ai/desktop/models) still has
not established a direct SWE-2 inference API for the first-party process;
the owner's DeepSeek selection let this gate proceed without Devin CLI. No
public binding, full-profile or original Session 6 verdict changes. At that
point, event follow/replay remained the next checkpoint.

**Checkpoint 4 result (2026-09-28):** The separate `duo-agent` process now
commits session-scoped semantic events with create, turn admission, completed
conversation/input-output, unresolved outcomes and incarnation replacement.
Snapshots pin an event-stream barrier distinct from conversation-row count;
`events.follow` pages strictly after it, with stable IDs and a repeatable
cursor. A bounded 16-event retention window refuses pruned cursors, epoch
mismatches and detected gaps with `retry: new_snapshot`. Older snapshot
page tokens that lack an event barrier expire rather than silently acquiring
the new epoch. Thirteen offline process-external tests passed, including
replay/reconnect, deduplication, restart, expiry and a deliberately removed
middle event; the snapshot, four event records and an epoch-mismatch refusal
also passed independent pinned-schema validation. No new provider/model call
was made for this checkpoint. This is a narrow single-caller private-binding
result, not a proof of public replay compatibility or of an adopted profile;
`profiles: []` remains. At that point the mediated S1 human/native writer
policy was the next distinct gate.

**Checkpoint 5 result (2026-09-28):** `duo-agent` local commit `734dc51`
(not yet pushed) adds a first-party process-external terminal client as the
only mediated ordinary human writer. It holds a renewable 90-second presence
lease, sends its text through the same durable owner and selected session
provider, and never silently retries a lost reply. One API turn per session
may queue under `queue_until_safe` for at most 30 seconds and never past its
original deadline; release rechecks incarnation, original revision,
readiness, registered route, and writer absence. A human turn changes the
revision so a held API turn fails without an attempt. Eighteen offline
process-external tests passed: two human turns from a real client process,
hold and priority, expiry/release, restart-stale refusal, and crash after a
released fixture effect with no reissue. Pinned-schema checks accepted a
queued command, a no-attempt failed command and `turn.submit` support.
No new model call was made. This qualifies only the single local caller and
that mediated console: unmanaged terminals, malicious same-UID processes,
distinct authority grants and a general S1 shared-writer policy are
**unverified**. The private `local.human.*` operations are not public contract
operations; `profiles: []` remains. Duo consumption and independent
adjudication are the next checkpoint, not a public binding decision.

**Checkpoint 6 partial development result (2026-09-28):** Duo's
`internal/protocolowned/devclient` test starts the separate `duo-agent`
process from a four-source-file SHA-256 pin
`b1692fda4b47313efe47689338a6a2f85c3edc3c2752c803c7be07bd3d8d1f91`.
The private Unix-socket client is generic to the authenticated operation
envelope; it does not dispatch by implementation name or translate harness
writes into Duo prompt commands. Against one disposable owner and one local
development caller, Duo mints a hostless instance and explicitly binds its
own correlation to the independently observed owner/session IDs. This is a
test-only `SourceOwner` attestation, **not** a subject-issued Duo instance
credential or production role integration.

The test checks `protocol.describe` (including `profiles: []`), scoped
`session.create` with an identical retry and changed-key refusal,
`session.inspect`, `support.inspect`, an initial snapshot barrier, one
deterministic `turn.submit` with an independently read effect marker,
original-key `command.inspect`, two frozen snapshot pages, and ordered
post-barrier `events.follow` replay without duplicate effect. Run with
`DUO_AGENT_PROGRAM=/absolute/path/to/duo-agent/own_agent.py go test -count=1
./internal/protocolowned/devclient` from the Duo development shell; without
the separate checkout it explicitly skips. The test does not call a model,
exercise cross-authority grants or shared-writer fencing from Duo, pass the
external two-authority conformance runner, or close the adopted profile.
Step 06 and the original two-composition Session 6 slice remain open; the
public-binding decision remains DECLINE.
