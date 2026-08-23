# Duo external v1 conformance fixtures

These files are normative examples for the Session 5 projection gate. Each
file uses `duo.external/v1` except four files that test other schema
families: `config.json` (`duo.config/v2`), `manifest.json`
(`duo.manifest/v1`), `projection-stamp.json` (`duo.projection-stamp/v1`),
and `projection-cases.json` (`duo.projection-conformance/v1`).

The integration-name neutrality gate applies to `duo.external/v1` documents
only. `config.json` and `projection-stamp.json` carry integration names
because the configuration and installation contracts sanction them there.

| File | Contract point |
|---|---|
| `session-list.json` | Opaque identity, current condition summary, and support links. |
| `session-inspect.json` | Runtime-instance identity, condition, and operation support. |
| `conversation-page.json` | Completed content blocks, page token, and stream barrier. |
| `condition-stream-item.json` | View revision and resume position remain separate. |
| `prompt-queued.json` | Queueing does not claim delivery, activity, or acknowledgment. |
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
| `config.json` | Strict successor configuration root: determined `review` composition with authored `model_line`, plus additive `presets.review`. |
| `session-launch.json` | Ordinary `session.launch` success: chosen leaf, resolved model line, and launch-resolution reference. |
| `session-launch-exhausted.json` | `launch.constraints_exhausted`: rejected candidate, elimination reason, and zero surviving assignments. |
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
