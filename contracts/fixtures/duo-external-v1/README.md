# Duo external v1 conformance fixtures

These files are normative examples for the Session 5 projection gate. Each
file uses `duo.external/v1` except four files that test other schema
families: `config.json` (`duo.config/v3`), `manifest.json`
(`duo.manifest/v1`), `projection-stamp.json` (`duo.projection-stamp/v1`),
and `projection-cases.json` (`duo.projection-conformance/v1`).

The integration-name neutrality gate applies to `duo.external/v1` documents
only. `config.json` and `projection-stamp.json` carry integration names
because the configuration and installation contracts sanction them there.

| File | Contract point |
|---|---|
| `session-list.json` | Opaque identity, current condition summary, and support links. |
| `session-inspect.json` | Runtime-instance identity, condition, operation support, and per-attachment show records (`attachments[]` with optional `process_birth` and pasteable `reattach_command`). |
| `session-inspect-starting.json` | Honest `session.inspect` while the runtime instance is `starting`: `runtime_instance_state: starting`, `condition` omitted, host `attachments` still present. |
| `conversation-page.json` | Completed content blocks, page token, and stream barrier. |
| `condition-stream-item.json` | View revision and resume position remain separate. |
| `prompt-queued.json` | Queueing does not claim delivery, activity, or acknowledgment. |
| `prompt-delivered.json` | `prompt.deliver` success with `responsibility_state: delivered` under `queue_until_safe`; effect omitted until an attempt records it. |
| `prompt-idempotency-conflict.json` | Same idempotency key with a different canonical digest fails as `command.idempotency_conflict`. |
| `prompt-expired.json` | Queued command past `expires_at` reaches terminal `expired` as timeout `command.expired`. |
| `command-inspect.json` | Command state, attempts, and separate delivery and activity milestones. |
| `command-result-stream-item.json` | A delivery result on the `command.results` stream. Delivered is not consumed. |
| `lease-acquired.json` | Composer-lease record with holder, state, expiry, and verified precondition. |
| `lease-refused.json` | Lease issuance fails as `unsupported` on an unverifiable topology. |
| `usage-stream-item.json` | Usage sample with source, measurement scope, freshness, and explicit gaps. |
| `runtime-configuration-selected.json` | A selected-configuration view change carrying a source alias and explicit gaps. |
| `runtime-configuration-effective.json` | An effective-turn record with a vendor-prefixed, embedded-variant source key preserved verbatim and no unproved parse. |
| `working-mode-selected.json` | A selected-working-mode view change with a compound selector (`session.agent` plus `data.mode`) kept as separate components. |
| `working-mode-effective.json` | An effective-working-mode record: a raw `plan` source value mapped to `duo.working_mode.v1`. |
| `terminal-snapshot.json` | Terminal fidelity, epoch, and snapshot reference. |
| `version-conflict.json` | Shared concurrency error across all projections. |
| `cursor-expired.json` | Reconnect requires a new snapshot and barrier. |
| `manifest.json` | Static operation projectability without live support. |
| `projection-stamp.json` | Generated ownership and drift inputs. |
| `config.json` | `duo.config/v3` root: policy-only `session_hosts` (no instance, no socket path), per-kind `launch_target` and `close_on_exit`, `deduce.cwd`, `agent_runtimes`, `model_family`-carrying `launch_variants`, and variant-targeting `presets.review` / `presets.build_and_verify` (no authored compositions; the host late-binds at launch). |
| `session-launch.json` | Ordinary `session.launch` success: chosen leaf, resolved model line, launch-resolution reference, and informational `target` / `target_source`. |
| `session-launch-exhausted.json` | `launch.constraints_exhausted`: rejected candidate, elimination reason, and zero surviving assignments. |
| `session-launch-model-line-relent.json` | Model-line soft avoid relent: avoid eliminated all candidates, relent restores pre-avoid pool, and chosen candidate reported with relented constraint. |
| `session-launch-runtime-relent.json` | Agent-runtime soft avoid relent: avoid eliminated all candidates, relent restores pre-avoid pool, and chosen candidate reported with relented constraint. |
| `session-launch-random-mode.json` | Random selection: explicit `"random"` selection mode, picked assignment, and no relented constraints on ordered chosen candidate. |
| `session-launch-mixed-leaf.json` | Multi-leaf atomic resolution: two leaves, distinct compositions, both selected and reported with atomic whole-plan semantics. |
| `session-launch-distinct-model-line.json` | Cross-leaf distinct-model-line relation: two leaves with different model lines satisfy the relation, rejected tuples match same-line pairs. |
| `session-launch-distinct-model-family.json` | Cross-leaf distinct-model-family relation: two leaves with different model families (`builder` claude, `verifier` gpt) satisfy the relation. |
| `session-launch-model-family-avoid.json` | `model_family` avoid closes the model-line silent miss: `--avoid model_family=gpt` removes every gpt-family candidate and `claude_sonnet` is selected. |
| `session-launch-provider-disabled.json` | `launch.no_eligible_candidate`: three candidates eliminated by a disabled provider fact, with the deduced host, the consulted provider fact ID, and the pointer set. |
| `session-launch-mixed-exhausted.json` | `launch.constraints_exhausted` with a mixed cause: one candidate falls to `require_unmatched`, the other to `provider_disabled`, both tallied because a caller constraint contributed. |
| `session-launch-host-unresolved.json` | `launch.host_unresolved`: host deduction consults every rung of the fixed ranking and yields no host; the deduction trail is the diagnostic. |
| `session-launch-explicit-host.json` | `--host` names a disabled kind: deduction rung `explicit-flag` wins, but every join is eliminated by `session_host_disabled`. |
| `session-enroll.json` | Ordinary `session.enroll` success: opaque session and runtime-instance IDs assigned. |
| `session-enroll-conflict.json` | `session.target_exited`: enrollment conflict with safe retry guidance and no_effect result. |
| `projection-cases.json` | Equivalent canonical requests through CLI, MCP, and presentation forms. |

An implementation test must decode each applicable external result through the
CLI, MCP, and presentation adapters. The decoded canonical values must be
equal. Transport status metadata is not part of that equality.

## Staged fixture authoring

This set covers the Stage 0 projection gate plus the control-surface
additions from the 2026-08-14 review follow-up. Two groups are deliberately
absent and have assigned authoring stages in the implementation roadmap:

- Launch and enrollment fixtures: author them at Stage 1 entry, before the
  Stage 1 identity gate runs. Ordered success (`session-launch.json`) and
  require exhaustion (`session-launch-exhausted.json`) are present; remaining
  launch-resolution cases still land at Stage 1 entry.
- Collaboration fixtures (read, guarded mutation, acknowledgment): author
  them at Stage 4 entry, before the Stage 4 projection-equality gate runs.

A stage gate that names an operation without a fixture here does not pass on
prose alone.
