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
`~/Code/duo-agent` (local `main` `0d53e19`, **no remote**) contains the
separate Python process and a private newline-JSON/Unix-socket development
binding. Its README pins the contract-source digests. Two process-external
tests passed: stable owner ID across restart, single live owner, token refusal,
unsupported operations, and unsafe directory refusal. An independent
`jsonschema` check accepted its authenticated `describe` record. It advertises
`profiles: []`; no session, turn, event or model call occurred. Checkpoint 2
is next. The Git commit is local to this runner: cloning Duo or duo-lab does
not recover that new repository until it is assigned a remote and pushed.
