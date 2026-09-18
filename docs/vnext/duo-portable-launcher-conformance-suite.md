# Portable launcher conformance suite and supported builder fixture

> Status: **Stage 2 offline suite implemented; live launcher evidence remains
> for Stages 3–5.** The blocked case has an explicit prerequisite in section 8
> and cannot be reported as passing today.

This suite tests Amp, OpenCode, and Codex as **outer launchers** of the same
Duo CLI workflow. It does not use any of them as the launched agent runtime.
All three receive the same task, use the same projected
`duo-delegation-loop` skill, and operate one identically constructed local
fixture. A launcher-specific driver may start its launcher and preserve its
events; it may not know how Duo launches, binds, sends, observes, retries, or
recovers.

The selected inner pair is the supported **Pi 0.83.0 runtime adapter on Herdr
0.8.2**. OpenCode is not selected: its adapter is intentionally not available
through the production CLI lookup and its current work is read-only. Amp is
not selected: its runtime adapter does not provide conversation or condition
observation. Devin remains a candidate runtime. Claude Code is a supported
alternative, but Pi has the stronger fixture boundary for this suite: an
exact version probe, a versioned transcript format, a Duo-materialized native
prompt socket, and `RuntimeReadyProvider`.

## 1. Existing product support and exact pins

### 1.1 Accepted fixture matrix

The inner fixture, Duo/schema/skill identities, and every run's captured
identities are exact, not minimum versions or compatible ranges. Outer
launcher eligibility is different: the recognized names `amp`, `opencode`,
and `codex` use `launcher_eligibility: capability_evidence`. Their historical
observations are not a compiled allowlist; a fresh version is eligible to run
the current suite when its exact version and executable SHA-256 are captured.

| Component | Required identity | Source in this checkout |
|---|---|---|
| Duo source/build | one per-run exact 40-character lowercase commit, exact version/build date, and executable SHA-256; setup artifact, recorder executable, setup evidence, and result pin must all agree | captured checkout, `internal/buildinfo`, and the validated command journals |
| Public Duo wire | `duo.external/v1`; schema SHA-256 `8b055ff60f0ca5f3da46302671591aa0dae61185cd9f1f15fcceb36d841e7497` | `contracts/schemas/duo-external-v1.schema.json` |
| Effective config | `duo.config/v3`; schema SHA-256 `d1a2fd1be5339c5699a21cf65616a4d09ff932a5495111d02a422b7656086842` | `contracts/schemas/duo-config-v3.schema.json` |
| Authority store | schema version `1` | `internal/store/migrations.go` and `internal/store/store.go` |
| Runtime | Pi `0.83.0`; adapter `pi`, build `stage1`, conformance record `pi-0.83.0-2026-08-23` | `internal/runtime/pi/pi.go` |
| Runtime semantic format | `pi-session-jsonl/v3` (header `version: 3`) | `internal/runtime/pi/pi.go` and `internal/runtime/pi/transcript.go` |
| Runtime delivery asset | `duo-inject.ts`, SHA-256 `2708a3435821fb2e72302ee56d8a07fe62c265214ae8fd342e385c03ef245a23` | `internal/runtime/pi/inject/duo-inject.ts` and `internal/runtime/pi/inject.go` |
| Host | Herdr `0.8.2`, socket protocol `20`, API-schema SHA-256 `c48f1f54ee0150ca27e11fd44455fe94aeadb20fdf4e4a62393ed822a4e5b150` | `internal/host/herdr/factory.go` |
| Host adapter | adapter `herdr`, build `stage1`, conformance record `notes/19-herdr-probes.md@2026-08-23` | `internal/host/herdr/factory.go` |
| Skill baseline | `duo.skill/duo-delegation-loop/v1@sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182` | `skills/duo-delegation-loop/SKILL.md` and the companion installation contract |
| Inner model selection | provider `openai-codex`, model line `gpt-5.6-luna`; this is captured as fixture input, not represented as an adapter compatibility claim | existing v3 dogfood configuration in `evidence/dogfood/2026-08-24/duo.config.yaml` |
| Outer launchers | recognized names Amp, OpenCode, and Codex; exact name/version/executable SHA-256 immutable per run; ordered historical observations are Amp `0.0.1789675234-g2899fe`, Amp `0.0.1789724374-g0d2ed0`, OpenCode `1.18.31`, and Codex CLI `0.154.0` | current common-suite capability evidence; historical metadata originated in `evidence/portable-launcher-conformance/step-01/probe-summary.json` and the Amp follow-up in `evidence/portable-launcher-conformance/step-07/attempt-02/probe-summary.json` |

The skill digest is a baseline. If Stage 2 changes its obsolete hand-install
wording, the installed projection, manifest, doctor output, scenario
manifest, and capture must all use the new digest as required by
`duo-portable-launcher-conformance-contract.md`. A capture must never silently
substitute the baseline digest.

The runtime is a supported product fixture rather than research-only
behavior:

- `stage1Support.Supported` in `internal/cli/session_launch.go` accepts the
  `(herdr, 0.8.2, pi)` tuple.
- `internal/runtime/pi.Runtime` implements `RuntimeCorrelator`,
  `ConversationProvider`, `ConditionProvider`, `RuntimePromptProvider`, and
  `RuntimeReadyProvider`.
- every Pi launch goes through `stage1LeafAugmenter`, which materializes the
  embedded `duo-inject.ts` and passes it with `-e`; prompt delivery is a
  native Unix-socket user turn, not terminal paste.
- the parser refuses a transcript whose header is not schema version 3, and
  the repository contains scrubbed, captured Pi 0.83.0 transcripts under
  `internal/runtime/pi/testdata/`.
- the Herdr adapter implements launch, attachment validation, lifecycle,
  binding identity, and prompt fallback against its pinned protocol. The live
  adapter test requires a disposable server and closes the pane it creates
  (`internal/host/herdr/live_test.go`).

The permanent fake adapters are valuable offline test infrastructure, but
they are not this live fixture. `internal/host/fake` and
`internal/runtime/fake` are registered in memory by tests and are not the
supported production launch tuple. A capture obtained with either fake must
be rejected.

### 1.2 Current-host mismatch is a prerequisite, not permission to widen support

The local observations made while authoring this document on 2026-09-17 are:

| Component | Installed observation | Fixture verdict |
|---|---|---|
| Pi | `0.84.4` | unverified; not the required `0.83.0` |
| Herdr | `0.9.0`, protocol `22`, API-schema SHA-256 `226d4ecbd128d2e6bc84e4c8ddcec21ba9c7e51a0aafffcf087111ead3f1fa9a` | unverified; not the required `0.8.2`/`20`/`c48f…b150` |
| Claude Code | `2.1.274` | outside its adapter's literal `2.1.240`, `2.1.241` set; not a fallback fixture |
| checked-out Duo | must be captured as an exact full commit for the run | eligible only when it matches the built and recorded executable identity |
| `duo` on PATH | not implicitly trusted | eligible only when copied into the fixture and bound to the same per-run version, commit, build date, and digest |

Stage 2 therefore needs locally installed, digest-pinned Pi 0.83.0 and Herdr
0.8.2 artifacts and a Duo binary built from the captured source commit. It
must not relabel the installed newer binaries as supported, download an
unpinned latest version, or fall back to Claude, fake, Amp, Devin, or
OpenCode. The artifact source and SHA-256 become additional capture pins;
this document cannot truthfully invent those binary digests before the exact
artifacts are provisioned.

## 2. Disposable local fixture

### 2.1 Roots and deterministic content

The common fixture controller creates a mode-`0700` run root with a random
unpredictable pathname. Paths in evidence are rewritten to the stable tokens
below; the random host pathname is never an oracle input.

```text
$RUN/
├── home/                         # HOME
├── xdg/
│   ├── config/                   # XDG_CONFIG_HOME
│   ├── data/                     # XDG_DATA_HOME; Duo store + harnesses
│   ├── state/                    # XDG_STATE_HOME
│   ├── cache/                    # XDG_CACHE_HOME
│   └── runtime/                  # XDG_RUNTIME_DIR; Pi inject sockets
├── bin/                          # pinned duo, herdr, pi and an allowlisted PATH
├── secrets/                      # 0700, omitted from evidence
├── herdr/                        # config, session state, socket and logs
├── workspace/                    # local Git worktree and projected skill
├── capture/                      # raw local events before scrub
└── result/                       # scrubbed export staging before atomic copy-out
```

`PI_CODING_AGENT_DIR`, `PI_CODING_AGENT_SESSION_DIR`, `HERDR_CONFIG_PATH`,
and every Duo/config/store/harness path resolve below `$RUN`. The fixture
passes the explicit workspace and explicit Herdr socket to preflight and
launch; it does not allow ambient host deduction to select a user's server.
`PATH` resolves the pinned fixture binaries before a small immutable system
allowlist. The controller fails setup if any resolved executable escapes
`$RUN/bin` (apart from that declared system allowlist).

The workspace is an initialized local Git repository with no remote. Its
fixture-owned files are byte-for-byte deterministic:

```text
fixture/request.txt:
fixture_id=portable-launcher-v1
expected_reply=DUO_PORTABLE_LAUNCHER_OK_V1
```

The file, including its final newline, has SHA-256
`b0a32f59630d6398da8d8913744dd7cd50a313d86bf7e833309aad0d47cc9c3b`.
The expected assistant text is exactly `DUO_PORTABLE_LAUNCHER_OK_V1` (SHA-256
`b3300f2f6e0338d2b672891be66827d95779a2eabf4a75dcae1b6f56c9a4dc98`).

The fixture config has one enabled host kind, one runtime, one variant, and
one `builder` preset with one `main` leaf. It selects Pi, the provider/model
row in section 1.1, close-on-exit, and an explicit Herdr host. The canonical
prompt is the exact UTF-8 string:

```text
Read fixture/request.txt and reply with exactly the value of expected_reply, with no surrounding text.
```

Its prompt-only digest, matching `promptCanonicalDigest` in
`internal/cli/prompt.go`, is
`sha256:b40d3a68eb33083b92199d734da2dece28395a6acc6d5e2cb9d7032dae4d11c7`.
The fixed idempotency key is `portable-launcher-v1-primary`. Because every
run has a fresh authority store, fixed keys cannot collide across runs.
Product-minted IDs (`ses_…`, runtime instance, command, attempt, attachment,
and launch-resolution IDs) remain opaque and nondeterministic; the oracle
compares their relationships, never hard-codes their values.

The conflict text is exactly `This text must conflict and must never be
delivered.` Its digest is
`sha256:dbe904a42a9d9d9cdf7908508fa393ce43c9c53ea16a8cb8c9b82e7005f602bc`.

### 2.2 Credentials and extension boundary

The suite does not own credentials. Before setup, an operator or CI secret
provider stages only the chosen inner provider's minimum credential material
into `$RUN/secrets` or supplies the corresponding secret environment value.
The controller copies it mechanically into the isolated Pi config with
directory mode `0700` and file mode `0600`. It records only
`credentials_present: true`, the provider name, and a non-secret source kind
(`ci_secret`, `operator_copy`, or `environment`); it never records a value,
token fingerprint, source pathname, or auth-file bytes. Missing credentials
fail `preflight`; they do not select another provider.

Pi starts with `--no-extensions`; the pinned 0.83.0 semantics for that flag
and the explicit Duo-owned `-e <duo-inject.ts>` exception are recorded in the
embedded inject asset itself. Its isolated roots contain no other skills,
prompt templates, context files, themes, or project packages. Stage 2 may add
the corresponding disable flags only after the pinned binary's captured help
confirms them. No other Pi extension is allowed. The outer launchers use their
isolated, plugin-free options from the Step 01 pattern; their temporary auth
files are treated by the same secret rule. No MCP server is configured or
reachable. A capture records `plugins_enabled: false`, `mcp_enabled: false`,
and the exact outer arguments.

### 2.3 Host lifecycle

The controller, not an outer launcher, starts one clean-environment Herdr
0.8.2 server whose socket and persistence are below `$RUN/herdr`. It verifies
ping version `0.8.2`, protocol `20`, and a fresh `herdr api schema --json`
digest before making the server available. The server environment is built
from an allowlist and is checked by the existing scrub policy so outer-agent
markers such as `CLAUDECODE`, `CLAUDE_CODE_CHILD_SESSION`, `CLAUDE_CODE_ENTRYPOINT`,
and `AI_AGENT` cannot leak into launched panes. This is required because
Herdr panes inherit the server environment (`internal/host/herdr/doc.go` and
`internal/scrub`).

Startup is complete only after the socket answers the pinned probe and its
snapshot is empty. Cleanup addresses objects by IDs captured from Duo/Herdr,
closes every fixture pane, stops only this Herdr server through its normal
local control surface, waits for its PID and socket to disappear, and then
removes `$RUN`. It never runs a broad `pkill`, follows a path outside
`$RUN`, contacts a user Herdr socket, or deletes an unrecognized object.

Before deletion the controller checks that no fixture PID remains, no fixture
socket remains, the Herdr snapshot has no fixture pane, and all files are
under `$RUN`. It atomically copies the scrubbed result and referenced blobs to
the caller-selected evidence directory outside `$RUN`, then removes `$RUN`.
Cleanup failure is retained as a failed `cleanup` stage; it is not hidden by a
shell trap's exit status. A best-effort emergency cleanup may run after
recording that failure, but may not rewrite it as success.

## 3. Ownership: one scenario and thin launcher drivers

The canonical scenario is `portable-launcher-delegation/v1`, current revision
`2`. It declares `launcher_eligibility: capability_evidence` and the three
recognized names, with no exact outer version/digest allowlist. Its task text,
fixture manifest, state machine, executable action instructions, deadlines,
assertions, and oracle live in the common suite. Every launcher receives the
same bytes. Each stage names its executor, operation, argv template, allowed
auxiliary polling operations, and invocation cardinality. The task tells the
outer agent only to load the shared `duo-delegation-loop` skill and execute
the canonical scenario manifest in order. It does not ask the launcher to
author a result or verdict; the common collector is the sole final-result
authority. The assertion oracle remains revision `1`: its evidence derivation
and assertion semantics did not change; revision `2` is required only for the
scenario's launcher-policy wire change.

A per-launcher driver may do exactly four things:

1. start the exact run-pinned outer launcher in `$RUN/workspace` with its isolated
   HOME/config and plugin/MCP-disabled flags;
2. make the already installed canonical project skill visible through
   `.agents/skills/duo-delegation-loop` (the common installer owns its bytes);
3. submit the exact common task bytes once; and
4. preserve the launcher's raw event stream and stderr.

It may not construct Duo commands, parse Duo IDs for the launcher, poll,
retry, induce fixture states, alter deadlines, add hints after a failure,
paste into a terminal, call Herdr, author or edit a result, or translate a
launcher-specific success into a suite pass. Launcher-native event envelopes
may differ, but all semantic evidence and the authoritative result are
assembled by the common collector.

The fixture controller is separate from every driver. It may perform declared
fault induction (sections 7–8) only after a durable checkpoint is observed.
It cannot answer the model, issue a replacement Duo command, or repair a
missed step. The oracle reads captured Duo envelopes, the read-only authority
projection, process/host evidence, and controller checkpoints. The launcher's
self-reported `pass` field is never trusted.

Setup validates the source launcher identity, copies the executable into
`$RUN/bin`, hashes the copy after placement to close the rolling self-update
race, and records that exact run pin for the thin driver. The driver rejects an
unknown name, malformed pin, name disagreement, or copied-byte digest mismatch
before execution. The authoritative result repeats the same pin, and common
orchestration rejects any setup/result disagreement.

## 4. Versioned result contract

Stage 2 must add a JSON Schema whose identity is
`duo.portable-launcher-conformance-result/v1`. The following is the normative
shape; prose fields may be added only by a v2 schema, not opportunistically.

```json
{
  "schema": "duo.portable-launcher-conformance-result/v1",
  "suite": {
    "name": "portable-launcher-delegation",
    "revision": 2,
    "manifest_digest": "sha256:<64 lowercase hex>",
    "oracle_digest": "sha256:<64 lowercase hex>"
  },
  "run": {
    "run_id": "<opaque>",
    "observed_at": "<RFC3339 UTC>",
    "host_os": "linux",
    "host_arch": "x86_64",
    "fixture_root": "$RUN",
    "plugins_enabled": false,
    "mcp_enabled": false,
    "terminal_input_used": false
  },
  "pins": {
    "launcher": {"name": "amp|opencode|codex", "version": "<exact>", "executable_sha256": "sha256:<hex>"},
    "duo": {"version": "<exact>", "commit": "<exact>", "build_date": "<exact>", "executable_sha256": "sha256:<hex>"},
    "skill": {"name": "duo-delegation-loop", "format_version": "duo.skill/duo-delegation-loop/v1", "content_digest": "sha256:<hex>", "installation_id": "<opaque>"},
    "config": {"schema": "duo.config/v3", "effective_digest": "sha256:<hex>"},
    "authority": {"schema_version": 1, "store_digest_before": "sha256:<hex>", "store_digest_after": "sha256:<hex>"},
    "host": {"name": "herdr", "version": "0.8.2", "protocol": "herdr-socket-api/20", "schema_digest": "sha256:c48f1f54ee0150ca27e11fd44455fe94aeadb20fdf4e4a62393ed822a4e5b150", "executable_sha256": "sha256:<hex>"},
    "runtime": {"name": "pi", "version": "0.83.0", "format": "pi-session-jsonl/v3", "adapter_build": "stage1", "conformance_record": "pi-0.83.0-2026-08-23", "executable_sha256": "sha256:<hex>", "delivery_asset_sha256": "sha256:2708a3435821fb2e72302ee56d8a07fe62c265214ae8fd342e385c03ef245a23"},
    "model": {"provider": "openai-codex", "model_line": "gpt-5.6-luna"},
    "external_schema": {"identity": "duo.external/v1", "digest": "sha256:8b055ff60f0ca5f3da46302671591aa0dae61185cd9f1f15fcceb36d841e7497"}
  },
  "stages": [
    {
      "sequence": 1,
      "stage": "setup",
      "case": "run",
      "verdict": "pass",
      "outcome": "success",
      "started_offset_ms": 0,
      "duration_ms": 1,
      "assertions": [{"id": "<closed oracle assertion>", "expected": {}, "actual": {}, "matched": true}],
      "evidence": ["blob:sha256:<hex>"],
      "error": null
    }
  ],
  "summary": {"verdict": "pass", "first_failed_stage": null, "first_failed_case": null},
  "scrub": {"status": "pass", "policy": "portable-launcher-evidence/v1", "findings": []}
}
```

The closed stage enum is:

```text
setup | discovery | preflight | launch | bind | send | observe |
command_inspection | restart | cleanup
```

The closed case enum is:

```text
run | happy | blocked | exited | timeout | restart |
same_key_same_text | same_key_different_text
```

The required stage/case matrix is also closed:

| Case | Required stages in scenario order |
|---|---|
| `run` | `setup`, `discovery`, `preflight`, `cleanup` |
| `happy` | `launch`, `bind`, `send`, `observe`, `command_inspection` |
| `restart` | `restart`, `bind`, `observe`, `command_inspection` |
| `same_key_same_text` | `send`, `command_inspection` |
| `same_key_different_text` | `send`, `command_inspection` |
| `blocked` | `launch`, `bind`, `send`, `observe`, `command_inspection` |
| `exited` | `launch`, `bind`, `send`, `observe`, `command_inspection` |
| `timeout` | `launch`, `bind`, `send`, `observe`, `command_inspection` |

The manifest fixes the global interleaving shown in section 5. If an earlier
failure prevents execution, the common collector still emits each remaining
required record as `verdict: fail`, `outcome: error` with
`prerequisite.not_reached`. Such a record references the originating failure
checkpoint and has no fabricated operation assertion. This makes a failed
capture complete and stage-addressable without adding a `skipped` escape
hatch.

`verdict` is `pass | fail`. `outcome` is
`success | blocked | exited | timeout | error`. An expected blocked, exited,
or timeout observation has `verdict: pass` only when the oracle independently
confirms that exact outcome. It is not encoded as a failed process. Every
failed stage requires `error` with `code`, safe `message`, `effect`, and
`retry`; every passing stage has `error: null`. Overall pass requires all
required stage/case pairs, so there is no `skipped`, `unsupported`, or
`assumed_pass` value.

`assertions[].id` is a closed list owned by the oracle revision. `expected`
comes from the signed scenario manifest; `actual` comes from evidence. A
launcher-authored expected value is invalid. Opaque IDs may appear in
`actual`, but assertions compare referential equality across records.

Each referenced blob is content-addressed and listed in a sidecar index with
media type, byte count, SHA-256, scrub status, and producer. The capture keeps
normalized Duo envelopes, selected fixture-owned conversation records,
launcher tool events, controller checkpoints, and process identities. It
does not keep raw vendor transcripts, reasoning, auth files, environment
dumps, Herdr screen contents, or arbitrary launcher state.

The v1 result schema remains structurally backward-readable for archived
revision-1 fixtures, while current collection and semantic validation require
canonical scenario revision 2 and its recomputed digest.

Offline validation requires all pin fields above, the exact per-run outer
executable identity, the post-Stage-2 skill and manifest identities, every
required stage, monotonically increasing `sequence`, valid timing fields,
blob hashes, schema-valid `duo.external/v1` envelopes, and a passing scrub
record. Absolute fixture paths in blobs are rewritten to `$RUN`, `$HOME`,
and `$WORKSPACE`; the rewrite map itself is not retained.

## 5. Canonical scenario body

The common scenario executes this order. Drivers cannot omit or reorder it.

1. **Setup:** create the isolated roots, deterministic workspace/config,
   copied stamped skill, empty authority, pinned binary PATH, credential
   boundary, and clean Herdr server.
2. **Discovery:** prove the launcher reports the projected skill's name,
   source, format, and digest through launcher evidence, not merely filesystem
   existence.
3. **Preflight:** run the Stage 2 `duo doctor ... --output json` contract with
   the exact workspace/config/host and require `launcher_preflight.status ==
   ready`, all seven required checks passing, and host compatibility
   supported.
4. **Happy launch/bind:** run `duo session launch builder --require
   agent_runtime=pi --workspace <workspace> --host herdr:<fixture-socket>
   --output json`; capture the session ID, then show/list until bind evidence
   is present within the bind boundary.
5. **Happy send/observe/inspect:** send the canonical prompt and key, observe
   the exact assistant text, and inspect the command.
6. **Authority restart:** end the process that executed the successful send,
   open the same store in a new Duo process, explicitly reconcile the still
   live attachment, and re-read session, conversation, and command state.
7. **Idempotency pair:** in the new Duo process, resend the same key/text,
   then the same key with the conflict text; inspect after each.
8. **Blocked case:** run the blocked induction and assertions in section 8.1.
   Today this deterministically fails at its prerequisite checkpoint; no
   launcher can pass the complete suite until that prerequisite exists.
9. **Exited case:** launch a fresh session, suspend its exact process before
   send, send a fixture prompt, induce host-proved exit after the durable
   delivery checkpoint, reconcile, and observe exited.
10. **Timeout case:** launch/bind a fresh session, suspend the exact Pi process
    under controller verification, durably send, and prove the 20-second
    assistant observation timeout without changing command delivery.
11. **Cleanup:** close all fixture panes and processes, stop the fixture
    server, scrub/export evidence, and remove the run root.

The outer agent invokes each Duo poll as a distinct command, approximately
once per second, exactly as the normative skill requires. The capture rejects
`duo wait`, `--block-for`, `seq`/shell polling loops, hidden stderr, terminal
paste, stop/interrupt, or a `duo command` family. `command.inspect` must be
reached through `duo prompt show`.

## 6. Independently derived assertions

The oracle below is generated from fixture constants and product contracts
before the outer launcher starts. Neither launcher instructions nor model
output can redefine an expectation.

### 6.1 Launch and bind

For each launched case:

- the launch envelope has `schema: duo.external/v1`, operation
  `session.launch`, a non-empty new `session_id` and `launch_resolution_id`,
  ordered selection, exactly one selected `main` leaf, `agent_runtime: pi`,
  the pinned model line, host kind `herdr`, the fixture socket identity, and
  no warning that changes support;
- no pre-launch prompt exists and the captured argv has no `--prompt`;
- the session and launch-resolution facts precede the host spawn, and the
  attachment refers to the one fixture pane;
- bind produces exactly one current runtime instance in state `live`, one
  active `agent.session` correlation for Pi, a transcript locator below the
  isolated Pi sessions root, and one claimed attachment with non-zero PID and
  start time;
- `session.inspect` names the same session/runtime IDs, lifecycle `active`,
  exposes `prompt.deliver` and `conversation.list` as `available`, and never
  claims blocked or exited before their induction. A fresh CLI process may
  show the derived view `recovering` until `session.reconcile`; the oracle
  therefore requires the explicit same-live reconcile rather than falsely
  equating `view` with durable instance state.

Failure is attributed to `launch` until a successful `session.launch`
envelope exists, then to `bind` until all bind assertions hold. Filesystem
discovery cannot satisfy either stage.

### 6.2 Durable send

The first send must return operation `prompt.deliver` with:

- one non-empty `command_id`, target session/runtime equal to the bound IDs;
- `responsibility_state: delivered`, `queue_policy: queue_until_safe`;
- `activity_observed: false`, `acknowledged: false`;
- `retry: {safe:false, action:"observe_existing_command"}`;
- no `effect` field on the success envelope. Current `duo.external/v1`
  defines effect certainty for unsuccessful attempts; inventing
  `effect: delivered` is invalid.

The durable authority record must have the independently calculated prompt
digest, fixed key, finite expiry, and exactly one attempt with path kind
`runtime`. The Pi socket path and captured extension digest must match the
bound session. `command.inspect` must report `delivered`, revision at least
`3`, one attempt with `realization: native` and `recorded_result: delivered`,
non-empty accepted/delivered times, false activity/acknowledgment, and the
same retry advice. This is the effect assertion: one native complete-turn
delivery attempt, not merely “send did not crash.”

### 6.3 Assistant text observation

Starting from the pre-send conversation baseline, exactly one newly observed
assistant text block must equal `DUO_PORTABLE_LAUNCHER_OK_V1`. User or `peer`
records cannot satisfy the assertion. Its record names the same current
runtime instance and is source-complete. Extra assistant prose, a terminal
snapshot, launcher final text, or a copied expected value outside
`conversation.list` fails `observe`.

The runtime may write internal thinking/tool records, but only semantic
`conversation.list` assistant text is evidence. Raw transcript content is
not exported.

### 6.4 Blocked

Once the prerequisite in section 8.1 exists, the blocked case requires:

- the same bound runtime instance remains nonterminal and its condition is
  exactly `blocked` with non-empty determining evidence and fresh or
  explicitly bounded freshness;
- polling stops on blocked before the 20-second timeout and no expected
  assistant text appears after the case baseline;
- if the case prompt was already admitted, its command remains `delivered`
  with exactly one native attempt and retry
  `{safe:false, action:"observe_existing_command"}`. Blocked does not rewrite
  transport delivery or fabricate `no_effect`;
- if induction is proved before any send, no command may exist. The scenario
  manifest must select one of these two shapes; it may not accept either at
  runtime.

For v1 the selected shape is **admitted-then-blocked**. Until supported
evidence can force that edge, the stage is `verdict: fail`, `outcome: error`,
code `prerequisite.blocked_induction_unavailable`, effect `no_effect`, retry
`{safe:false, action:"add_supported_blocked_evidence"}`.

### 6.5 Exited

The controller first suspends the exact process-birth PID and verifies the
stopped state, preventing a model response from racing the lifecycle edge.
After it sees the exited-case command durably `delivered`, it closes that
case's exact pane through Herdr's `pane.close` API and invokes `duo session
reconcile <session-id> --output json`. Assertions are:

- controller target terminal ID/PID/start time equals the session attachment;
- reconcile reports `exited` for the same runtime-instance ID;
- subsequent inspect keeps lifecycle `active`, has derived view `inactive`,
  runtime instance state `exited`, and condition `exited` with reported/fresh
  evidence;
- the already delivered command remains delivered, one attempt, native,
  `recorded_result: delivered`, with retry
  `{safe:false, action:"observe_existing_command"}`; exit cannot erase or
  replay its possible effect;
- no assistant success is required after the case baseline, and polling
  stops on exited rather than timing out.

The controller's Herdr call is declared fault induction, not a driver repair.
No keys, text, shell command, stop/interrupt Duo verb, or terminal bytes are
sent to the runtime.

### 6.6 Timeout

Timeout is the skill's **assistant observation timeout**, not process timeout
or command expiry. On a fresh bound Pi session, the controller sends `SIGSTOP`
to the exact process-birth PID and verifies Linux reports it stopped before
the outer agent sends. The already listening Unix socket remains the supported
Pi native delivery boundary: the command must still become delivered with one
native attempt and the standard observe-existing retry advice. The frozen
process cannot create an assistant turn.

The observer must run for at least 20,000 ms and no more than 22,500 ms on a
monotonic clock, with roughly one show/list pair per second. It passes with
`outcome: timeout` only when no new assistant text exists, session/runtime IDs
remain unchanged, condition is not falsely reported blocked/exited, and
command inspection remains delivered/one-attempt. The launcher process must
remain successful; an outer timeout, killed shell, command expiry, or CLI
nonzero exit is not this outcome.

The signal is sent by the common fixture controller, never by a launcher or
inside the pane. It changes no adapter, transcript, socket protocol, or
runtime bytes. Cleanup closes the disposable pane while the process is
stopped (and escalates only to that exact captured PID if the normal pane
close cannot finish). It must not resume the queued prompt into a shared or
production runtime.

### 6.7 Authority restart

“Restart” in this suite means an **authority-process restart**, not an outer
launcher restart, Herdr restart, or Pi restart. Duo is a command-per-process
CLI: the process that executed the first successful `prompt send` exits and
releases its store lease; a separately exec'd Duo process opens the same
SQLite file and runs `session reconcile`, then read operations. The capture
proves distinct OS PIDs and one unchanged store inode/path token.

The durable Duo session ID, runtime-instance ID (after same-live host
validation), command ID, idempotency key/digest record, attachment claim,
conversation correlation, and one delivery attempt survive. Authority
incarnation and process PID do not. Required recovered values are:

- reconcile outcome `same_live` for the original runtime instance;
- the same assistant record/text remains readable;
- `prompt show` returns the same delivered command, revision, target, attempt
  ID/count/result, milestones, and retry advice;
- no adapter call and no new user or assistant turn occurs merely because the
  new authority opened the store.

The forbidden replay is any second delivery attempt or second inner user turn
for the primary prompt. Restart must not create a replacement runtime
instance, because Herdr proves the original process birth is still live.

### 6.8 Same key and same text

After restart, resending the fixed key with the canonical text must return
the original command ID, delivered state, unchanged target and attempt list,
and retry `{safe:false, action:"observe_existing_command"}`. The success
envelope again has no `effect` member. The oracle compares pre/post authority
facts and semantic turns: command count, attempt count, primary user-turn
count, and matching assistant-turn count must not increase.

### 6.9 Same key and different text

Sending the fixed key with the conflict text must exit as a schema-valid
`prompt.deliver` failure:

```text
class:  conflict
code:   command.idempotency_conflict
effect: no_effect
retry:  {safe:false, action:"use_new_idempotency_key"}
target: {kind:"prompt_command", id:<original command ID>}
```

`details.idempotency_key` equals the fixed key; `existing_digest` equals
`sha256:b40d3a68eb33083b92199d734da2dece28395a6acc6d5e2cb9d7032dae4d11c7`;
`request_digest` equals
`sha256:dbe904a42a9d9d9cdf7908508fa393ce43c9c53ea16a8cb8c9b82e7005f602bc`.
The original command remains delivered with one attempt. No new command,
attempt, queue entry, inner user turn, or assistant turn may appear.

## 7. State induction rules

Only the common fixture controller may induce a state, and only against IDs
derived from the fixture's durable evidence. Its control log is part of the
capture. The allowed v1 controls are:

| State | Induction | Why it preserves the support claim |
|---|---|---|
| exited | Verified `SIGSTOP`, then Herdr 0.8.2 `pane.close` for the exact disposable attachment after durable delivery, followed by public `session.reconcile` | Suspension removes the response race; host lifecycle and continuity validation are implemented supported surfaces, and no runtime input is synthesized. |
| timeout | Linux `SIGSTOP` for the exact Pi process birth, verified before send; observe for 20 seconds; close the pane during cleanup | The pinned Pi binary, transcript parser, native delivery socket, and Herdr attachment remain unchanged. The fault is scheduler suspension, not a fake adapter or random model timing. |
| blocked | unavailable today; section 8.1 | Pi 0.83.0 has no permission system or blocked producer, so a fabricated event would weaken the claim. |

No induction may use PTY writes, `pane.send_text`, `pane.send_keys`, shell
paste, slash commands, model persuasion, sleep-based races, altered
transcripts, fake sockets, proxy responses, plugins, MCP, an outer-launcher
API, or another runtime adapter. A checkpoint is event-driven (durable state
or verified process state), never “sleep N and hope.”

## 8. Known prerequisites and conservative failures

### 8.1 Blocked is not inducible on the selected supported runtime

`internal/runtime/pi/doc.go` explicitly records that Pi 0.83.0 has no
permission system and no blocked-family producer. Its only known convention,
`herdr:blocked`, had one listener and zero emitters in the pinned sweep.
`internal/runtime/pi/condition.go` consequently emits idle, working, or
unknown, never blocked. Although `duo.external/v1` and
`runtime.ConditionBlocked` include the value, vocabulary is not evidence.
Herdr's `agent_blocked` prompt refusal is an adapter no-effect mapping, not a
Duo session-condition observation, and manufacturing that protocol reply in a
fake server would not prove the Pi × Herdr product tuple.

The smallest honest prerequisite is a version-pinned, supported production
condition source that can deterministically place the selected live runtime
in blocked **after prompt admission**, plus a non-terminal control that
induces and clears it without PTY input. The adapter/composer must project
that source as fresh `condition.value: blocked` for the exact runtime instance
and carry conformance evidence for its external version. If Pi cannot supply
that source, selecting a different inner runtime requires reopening this
Stage 1 decision and rerunning every scenario for all launchers; Stage 2 may
not silently switch only the blocked case.

This prerequisite belongs to runtime/host capability work, not to a thin
launcher driver. Until it lands, the common suite is useful for development
but **cannot issue an overall pass**, and Stage 6 cannot seal portable
launcher support.

### 8.2 Pinned artifacts and credentials

The exact Pi 0.83.0 and Herdr 0.8.2 binaries are not currently available on
PATH, and their executable SHA-256 values are not established by this
checkout. Stage 2 must provision them locally and record provenance/digests
before live runs. The selected provider also needs a disposable isolated
credential. Absence fails setup/preflight; it does not weaken pins or choose
another runtime/model.

### 8.3 Timeout fault-control proof

Current code proves the Pi adapter's delivery boundary (successful full-frame
write to a live peer), socket naming, and native attempt semantics, but this
checkout has no live test that freezes a pinned Pi process between bind and
send. Before relying on section 6.6, Stage 2 must run that exact disposable
probe and retain evidence that the stopped listener yields one delivered
attempt and no assistant turn. If the pinned socket does not admit the frame
while stopped, `timeout` fails with
`prerequisite.timeout_induction_unavailable`; the suite may not replace it
with model persuasion, a timing race, command expiry, or a fake listener.

## 9. Deadlines and cleanup are oracle results

All bounds use monotonic deadlines in the common runner, not shell polling
loops:

| Boundary | Limit | Stage result |
|---|---:|---|
| fixture setup and host probe | 30 s | `setup/error` |
| launcher discovery | 30 s | `discovery/error` |
| Duo preflight | 15 s | `preflight/error` |
| each launch process | 30 s | `launch/error` |
| post-launch bind | 12 s (covers the product's 8 s identity bind) | `bind/error` |
| each send process except the intentional observation wait | 30 s | `send/error` |
| ordinary assistant observation | 20 s | `observe/timeout` and overall failure when success was expected |
| intentional timeout observation | 20.0–22.5 s | `observe/timeout` and case pass |
| command inspection/restart reconcile | 15 s each | `command_inspection/error` or `restart/error` |
| cleanup | 30 s | `cleanup/error` |
| complete outer run | 10 min | failure at the last common checkpoint, never an unclassified driver timeout |

Cleanup is mandatory after every prior outcome and appears last exactly once.
An earlier failure does not authorize omission: unrun semantic cases are
recorded by the common collector as failures at their first required stage,
then cleanup runs. A capture with no cleanup stage is invalid rather than
“failed but acceptable.”

## 10. Rejection rules

The offline validator rejects the entire capture if any of these is true:

1. **Terminal evidence:** any PTY/terminal input, paste, send-keys, terminal
   snapshot used as semantic evidence, or fallback after a Duo failure.
2. **Missing stages:** a required run/case stage, cleanup, assertion, error
   stage, or evidence blob is absent, duplicated contrary to the scenario
   manifest, out of order, or outside the closed enum.
3. **Launcher semantics:** a driver contains Duo command names, stage logic,
   expected text/digests, polling, retries, state induction, or result repair;
   task bytes or scenario/oracle digests differ between launchers.
4. **Unpinned inputs:** the launcher name is unknown; its exact run version or
   digest is malformed; setup, driver, copied bytes, and result disagree; or an
   exact Duo, skill, config, host triple, runtime version/format/asset, model
   selection, external schema, or binary digest is absent or differs. Absence
   from historical `tested_versions` is not a rejection reason.
5. **Forbidden integrations:** plugins or MCP are enabled, a fake/unfinished
   adapter appears, a user/production socket/store/workspace is touched, or
   credentials are missing and a fallback provider was selected.
6. **Unsanitized content:** a credential value/fingerprint/path, private raw
   transcript, reasoning, absolute user/run path, environment dump, vendor
   state, or non-fixture conversation appears in the result or blobs.
7. **Fabricated pass:** a launcher verdict lacks matching raw Duo envelopes,
   process/controller checkpoints, authority relationships, or independently
   recomputed assertions; a nonzero/missing command was converted to pass; an
   expected non-happy outcome was represented by killing the outer command.
8. **Idempotency drift:** a replay changes command ID, target, revision,
   attempt list, or turn counts; a conflict lacks exact no-effect/retry/digest
   fields.
9. **Timeout drift:** timeout is inferred from a shell exit, wall-clock sleep,
   command expiry, or model timing instead of the bounded monotonic observer
   and verified stopped process.
10. **Cleanup drift:** fixture resources remain, cleanup exceeds its deadline,
    or emergency cleanup hides the failed cleanup stage.

## 11. Stage 2 implementation boundaries

The implemented offline slice keeps ownership as follows:

`assembly.go` and `orchestrate.go` are Linux-only because their trusted
command journal and process-control sources are Linux-only. The scenario,
schemas, collector, oracle, and offline validator remain platform-neutral.

```text
internal/conformance/portablelauncher/
├── scenario.go          # one manifest/state machine and task bytes
├── oracle.go            # assertions; no launcher imports
├── fixture.go           # roots, pinned host/runtime, controller, cleanup
├── capture.go           # scrubbed content-addressed evidence
├── assembly.go          # typed raw facts -> canonical observations/stages
├── orchestrate.go       # prepare/run/load/assemble/validate/atomic-write API
└── validate.go          # offline schema/pin/rejection validation

contracts/schemas/
└── duo-portable-launcher-conformance-result-v1.schema.json

contracts/fixtures/duo-portable-launcher-conformance-v1/
├── scenario.json        # deterministic inputs and required stage/case matrix
└── result-*.json        # passing structural/non-happy and rejected examples

contrib/portable-launcher-conformance/
├── amp                  # thin process/event adapter only
├── opencode             # thin process/event adapter only
└── codex                # thin process/event adapter only
```

No helper binary is added until the live normalization/provisioning inputs
exist; exporting an entry point that invents those inputs would be misleading.
Any future helper imports the common package and must not become a second Duo
protocol or a launcher-specific scenario owner. Runtime fixture
bytes remain owned by `internal/runtime/pi`; Herdr behavior remains owned by
`internal/host/herdr`; operation names remain owned by
`internal/registry/table.go`; public operation envelopes remain
`duo.external/v1`; skill installation/preflight remain owned by the companion
contract's `internal/manifest`, `internal/doctor`, and `internal/cli` seams.

Stage 2 offline tests cover schema closure, every expected non-happy
outcome, stage attribution for every deadline, cleanup after each failure
point, scrub rejection, binary/version drift, missing evidence, terminal
paste, launcher-semantic contamination, duplicate effects, and fabricated
passes. Live launcher evidence remains for Stages 3–5; Stage 2 must not embed
launcher-specific inference in ordinary unit tests. It does test the common
capability policy uniformly for Amp, OpenCode, and Codex.

## 12. Decision summary

- One inner fixture: supported Pi 0.83.0 on supported Herdr 0.8.2/protocol
  20/schema `c48f…b150`, with Pi transcript format v3 and Duo's native inject
  asset pinned.
- One isolated local authority/workspace/host/runtime per run; no shared user
  state, plugins, MCP, terminal input, or credential capture.
- One scenario/oracle/result schema for all outer launchers; drivers only
  launch, submit, and capture raw events/stderr. The common collector alone
  authors the final result.
- One outer-launcher policy: recognized names are admitted by current
  capability evidence, while exact version and copied-executable digest remain
  immutable run provenance and historical observations remain metadata.
- Exact delivery, semantic observation, exit, timeout, authority-restart, and
  asymmetric idempotency assertions are grounded in current operations and
  fixtures, including effect and retry semantics.
- Exited is induced by exact-pane host lifecycle; timeout by verified process
  suspension around the native socket. Both are event-driven and disposable.
- Blocked cannot honestly be induced by the selected supported runtime today.
  It is a named prerequisite and an overall-suite blocker, never a fabricated
  pass.
