<!-- Snapshot from the terminal-multiplexers archive at its 2026-09 freeze
     (handoff 27, repo-consolidation Stage B). Authored here from now on.
     Relative links that do not resolve in this repo refer to the archive;
     cite it by tag, not branch. -->

# Duo vNext public contract

> Status: **normative for the vNext external contract.**
>
> Schema family: `duo.external/v1`.

## 1. Scope

This specification defines the semantic and JSON wire rules that every public
projection uses. It does not define Go types, storage tables, or adapter wire
formats.

CLI JSON, MCP structured content, and the local presentation service use this
contract. A projection can omit an operation that does not apply to its role.
It cannot change the operation's validation, authorization, result, error, or
audit meaning.

## 2. Version and compatibility policy

Every external request, result, error, and stream document includes `schema:
"duo.external/v1"`. Installation documents use their named schema family. The
`v1` component is the public contract major version. Product versions use
semantic versioning and do not replace the contract version.

The following changes are compatible within `v1`:

- Add an optional field whose absence has the documented default meaning.
- Add a new operation, stream family, or object kind.
- Add a new error code under an existing error class.
- Add a new value only to a field that this specification marks as open.

The following changes require `duo.external/v2`:

- Remove or rename a field or operation.
- Change a field type, identity scope, guard, or default meaning.
- Add a required field without a negotiated feature.
- Change retry, effect-certainty, authorization, or ordering meaning.
- Add a value to a closed vocabulary.

Readers must ignore unknown optional fields. Readers must preserve unknown
content-block and stream-item payloads when they forward or store them. A
reader must reject an unknown value in a closed vocabulary. The error must use
`invalid.unsupported_schema_value`.

The core wire schema carries each closed vocabulary as a JSON Schema enum
(2026-08-14 review amendment, G-16). A misspelled closed value fails schema
validation. It does not validate as prose. Per-operation request and result
schemas in the operation registry must reference those shared enum
definitions instead of restating the values. Open vocabularies, such as
stream-item and content-block kinds, stay unenumerated.

Every response can include `features`, which contains open, namespaced feature
strings. A client must not infer support from the product version.

## 3. JSON representation

The canonical JSON rules are:

| Concern | Rule |
|---|---|
| Field names | Lowercase `snake_case`. |
| Unknown fields | Ignore unless a signed digest covers the complete document. |
| Absent value | Omit the field. Use `null` only when null has a stated domain meaning. |
| Time | RFC 3339 UTC with a `Z` suffix and up to nanosecond precision. |
| Duration | Integer milliseconds in a field with an `_ms` suffix. |
| Revisions and positions | Unsigned decimal strings. Clients compare them only in their declared scope. |
| Opaque IDs | Strings. Prefixes in examples help people and have no parsing contract. |
| Digests | Lowercase `sha256:<hex>`. |
| Byte content | Base64url without padding, with an explicit media type. |
| Text | UTF-8. Invalid source bytes use an opaque block or byte field. |

Closed wire values use lowercase `snake_case`. A hyphenated term in the
semantic decision notes maps directly to its snake-case wire spelling. For
example, `queue-until-safe` maps to `queue_until_safe`.

JSON requests use a canonical digest form for idempotency. The form sorts
object keys, preserves array order, excludes transport metadata, and includes
all semantic defaults after Duo resolves them.

## 4. Common envelopes

### 4.1 Success

```json
{
  "schema": "duo.external/v1",
  "request_id": "req_01KZ...",
  "operation": "prompt.deliver",
  "result": {},
  "warnings": [],
  "features": []
}
```

`request_id` identifies one projection request. A durable write result also
contains the ID of the accepted command, fact, or record. The request ID never
replaces that domain ID.

### 4.2 Failure

```json
{
  "schema": "duo.external/v1",
  "request_id": "req_01KZ...",
  "operation": "collaboration.value.replace",
  "error": {
    "class": "conflict",
    "code": "collaboration.version_mismatch",
    "message": "The object version changed.",
    "target": {"kind": "collaboration_object", "id": "obj_01KZ..."},
    "retry": {"safe": true, "action": "reread"},
    "effect": "no_effect",
    "details": {"expected_version": "4", "current_version": "5"}
  }
}
```

The error contract is normative in
[`duo-vnext-access-errors-audit.md`](./duo-vnext-access-errors-audit.md).

## 5. Public semantic records

### 5.1 References

A public reference has this form:

```json
{
  "kind": "conversation_page",
  "id": "page_01KZ...",
  "uri": "duo://local/conversations/page_01KZ...",
  "schema": "duo.external/v1",
  "media_type": "application/json",
  "expires_at": "2026-08-13T13:00:00Z"
}
```

The `id` and `uri` are opaque. A reference is stable for its declared lifetime.
It does not grant access. Duo checks authorization when a caller resolves it.
Large histories, protected collaboration content, terminal snapshots, and
audit exports use references when inline content would exceed the surface
limit.

### 5.2 Pages and barriers

Every history page contains `items`, `next_page`, and `barrier`. `next_page` is
an opaque short-lived page token. `barrier` contains a stream family, ordering
scope, and resume position. A live subscription starts strictly after the
barrier.

Page tokens are not stream positions. Stream positions are not object versions
or view revisions. A cursor-expired result uses
`history.cursor_expired` and returns safe instructions to read a new snapshot.

### 5.3 Stream items

```json
{
  "schema": "duo.external/v1",
  "stream": "session.condition",
  "scope": {"session_id": "ses_01KZ..."},
  "item_id": "sti_01KZ...",
  "position": "1842",
  "recorded_at": "2026-08-13T12:00:00Z",
  "kind": "view.changed",
  "subject": {"kind": "session_condition", "id": "ses_01KZ..."},
  "data": {}
}
```

`kind` is open. An unknown kind remains an opaque stream item. Consumers dedupe
by `item_id` and resume with `position`. Ordering exists only inside the stated
scope. Semantic streams use at-least-once delivery.

### 5.4 Session summary and inspection

A session summary contains:

- `session_id`, `workspace_id`, lifecycle, and current runtime-instance ID.
- The current condition summary and its view revision.
- Links or references for full condition, support, conversation, command, and
  terminal records.
- No external correlation unless the caller has diagnostics permission.

Session inspection adds ownership, attachment state, instance history,
operation-support views, the runtime-configuration view, and authorized
diagnostic references. Process IDs, paths, pane IDs, and agent-session IDs
remain correlations.

### 5.5 Current condition and operation support

The condition view contains `session_id`, `runtime_instance_id`, `value`,
`revision`, `confidence`, `freshness`, `effective_at`, `computed_at`,
determining observation references, and conflict or degradation reasons.

The closed condition values are `idle`, `working`, `blocked`, `done`, `exited`,
and `unknown`. Confidence and freshness use the closed Session 2 vocabularies.
`done` stays in the closed vocabulary and validates, but it is a reserved
value on the first slice. No initial composition has a qualified completion
source, so no conformant v1 producer emits it yet (2026-08-14 review
amendment, recorded in the Session 2 note's amendments).

One operation-support view contains:

- The semantic operation and scoped `revision`.
- `availability`, `quality`, and `realization`.
- Current constraints and safe diagnostic provider references.
- A separate caller authorization decision and policy version.

Static manifests never contain this live view.

(2026-08-26 handoff 25 amendment: the delegation-loop milestone's
condition view is a single determining source per composition. It does
not implement conflict ranking. The credentialed rank-2 reporter stays
out. See
[`handoffs/25-delegation-loop-scope.md`](./handoffs/25-delegation-loop-scope.md).)

(2026-08-26 handoff 26 amendment: the post-launch identity-bind
milestone writes `agent.session` / `transcript` correlations after
launch and advances `starting` → `live`, so this condition view can
bind on a live inspect. The credentialed rank-2 reporter stays out.
See
[`handoffs/26-launch-bind-scope.md`](./handoffs/26-launch-bind-scope.md).)

### 5.6 Conversation

A conversation record contains the Session 2 semantic envelope. Content blocks
use a required open `type`, ordered block position, provenance, and a typed or
opaque `content` value. Initial known types are `text`, `reasoning`,
`tool_call`, `tool_result`, `attachment`, `reference`, and `unknown`.

History contains completed blocks only. A copied fork record retains lineage.
Terminal repaint data never appears as conversation content.

(2026-08-26 handoff 25 amendment: this milestone implements conversation
history as a CLI `conversation.list` page. Conversation subscribe, MCP,
and presentation routes stay named and out of this milestone.)

### 5.7 Commands

All durable command requests contain:

- `idempotency_key` and the exact Duo target.
- Operation-specific payload and preconditions.
- A finite `expires_at` value.
- Requested minimum quality and allowed realizations when applicable.
- Queue policy when the operation permits queueing.

A command result contains the command ID, command revision, responsibility
state, target, deadlines, support decision, and current retry guidance. It also
contains effect certainty after an attempt.

The closed queue policies are `require_ready`, `queue_until_safe`, and
`hold_for_release`. The closed responsibility states are `accepted`, `queued`,
`attempting`, `delivered`, `expired`, `canceled`, and `failed`. Effect certainty
is `no_effect` or `unknown_effect`. A command omits effect certainty until an
attempt or terminal no-effect decision exists.

Duo binds the runtime instance at acceptance. An ordinary prompt request
targets the Duo session, and Duo binds the session's current live runtime
instance. An optional expected runtime-instance precondition makes a
replaced instance fail acceptance with `conflict`. A notification-created
prompt command records the Duo delivery authority as its caller and the
subscription owner as its policy principal.

The composer-lease operation family covers acquire, renew, release, and
inspect under a separate `prompt.lease` permission. A lease record contains
the lease ID, target runtime instance, holder subject, state, expiry, and
void reason. The closed lease states are `active`, `released`, `expired`,
`revoked`, and `void`. Issuance on a composition that cannot verify the
no-human-writer precondition fails with `unsupported`. Lease state appears
as a constraint on the prompt-delivery support view, and void transitions
publish stream items.

Prompt payloads contain semantic content. They never contain terminal keys,
paste modes, or adapter method names.

(2026-08-26 handoff 25 amendment: the delegation-loop milestone accepts
only `queue-until-safe` / `queue_until_safe` as the queue policy.
`require_ready` and `hold_for_release` stay in this closed vocabulary
and stay in full Stage 3. Composer leases stay out of this milestone.
Public projection is CLI only (`prompt.deliver`, `command.inspect`).
MCP and presentation routes stay named and out of this milestone.)

### 5.8 Collaboration

Every collaboration object contains its object ID, fixed kind, workspace ID,
owner subject, creator, lifecycle, object version, current-view revision,
policy reference, retention class, and times.

The closed object kinds are `shared_value`, `shared_document`, `inbox`, `timer`,
and `subscription`. The closed lifecycle values are `active`, `archived`, and
`removed`.

Whole-object writes use `expected_version`. Shared-value path writes use a JSON
Pointer in `path`, a `path_token`, and a `container_generation`. JSON Pointer
escaping follows RFC 6901. Arrays are atomic at the selected path.

Every path write requires `path_token` (2026-08-14 review amendment,
G-14). A read issues an opaque path token for each requested path. A token
for a present path asserts the observed value. A token for an absent path
asserts the observed absence, so put-if-absent sends that absence token in
the same `path_token` field. There is no distinct wire encoding and no
sentinel value. An omitted `path_token` on a path write is
`invalid.precondition`. It never means an unguarded write.

Document append and inbox post do not require an object-version guard. They do
require an idempotency key. Document correction, lifecycle changes, ownership
transfer, and subscription definition changes require an exact version.

Facts, notifications, delivery records, prompt commands, command attempts,
inbox entries, and acknowledgments keep separate IDs. A convenience response
can join their references but cannot collapse them.

### 5.9 Runtime configuration facet

The runtime configuration facet (2026-08-17 handoff 16 amendment) exposes the
configuration a runtime selected and the configuration that served a turn. It
is a built-in optional facet delivered through the condition channel. It is not
part of the usage facet.

A selected-configuration current view contains:

- `session_id` and `runtime_instance_id`.
- `revision`, `confidence`, `freshness`, `effective_at`, and `computed_at`.
- `determining_observations`.
- `selected`, a `model_identity` object with the fields below.
- `gaps` listing fields the source cannot supply, and `reasons`.

A `model_identity` object uses these optional fields. Absence means the source
does not supply that field:

| Field | Meaning |
|---|---|
| `source_provider_key` | The routing or provider backend key. |
| `source_model_key` | The exact source model key, opaque and preserved verbatim. |
| `vendor_key` | Optional model-creator identity, separate from the provider. |
| `source_display_name` | The source-owned user-facing name. |
| `normalized_family` | Optional `{name, mapping_version, evidence}` interpretation. |
| `context_limit` | A numeric context-window limit, only when the source states it. |
| `execution_modifiers` | A list of `{source_key, source_value, normalized_meaning}` entries. |
| `source_effort` | The exact native effort or thinking-level value. |
| `normalized_effort` | Optional `{level, vocabulary_version, unknown_behavior}` interpretation. |

`source_model_key` keeps vendor prefixes and embedded variants as opaque
strings. A normalized family or effort never replaces source identity. A source
alias can remain selected while the effective model changes per turn.

An effective-configuration record is immutable, scoped to one conversation
record. It carries `effective` (a `model_identity`), the `source_record`
reference, `observed_at`, and `freshness`.

`session.inspect` includes the selected-configuration view when the facet is
supported. The `session.runtime_configuration` stream family delivers both the
selected-view changes (`kind: view.changed`) and the effective-turn records
(`kind: effective.recorded`). Ordering, retention, and duplicate rules follow
the Session 2 note's Section 9.2 row.

### 5.10 Working mode

Working mode (2026-08-17 handoff 17 amendment) is a second namespace inside
the runtime-configuration facet. It is not part of the model identity. It uses
the same selected/effective split.

A selected-working-mode current view contains `session_id`,
`runtime_instance_id`, `revision`, `confidence`, `freshness`, `effective_at`,
`computed_at`, `determining_observations`, `selected` (a `working_mode`
object), `gaps`, and `reasons`.

A `working_mode` object uses these optional fields:

| Field | Meaning |
|---|---|
| `source_key` | The exact source field name, e.g. `collaboration_mode.mode` or `session.agent`. |
| `source_value` | The exact source value, preserved as an opaque string. |
| `components` | A list of `{source_key, source_value}` for compound selectors. |
| `normalized_class` | Optional `{class, vocabulary_version, evidence}` interpretation. |
| `source_display_name` | The source-owned user-facing name. |

Portable classes come from a versioned vocabulary, `duo.working_mode.v1`.
`plan` maps only on named source evidence; `review` and `act` are reserved.
Source values are preserved verbatim, and no class is inferred from tool use.

An effective-working-mode record is immutable, scoped to one conversation
record. It carries `effective` (a `working_mode` object), the `source_record`
reference, `observed_at`, and `freshness`.

`session.inspect` includes the selected-working-mode view when the facet is
supported. The `session.runtime_configuration` stream family also delivers
selected-working-mode view changes (`kind: working_mode.view_changed`) and
effective-working-mode records (`kind: working_mode.effective.recorded`).
Ordering, retention, and duplicate rules follow the Session 2 note's Section
9.2 working-mode row.

### 5.10 Launch request, result, and launch-resolution reference

`session.launch` (2026-08-18 handoff 18 amendment) is the open launch
operation. The caller names a preset, not a composition. The request and
result schemas are projection-neutral.

A launch request contains:

- `preset`: the requested preset name.
- `require`: at most one required value per axis. Accepted axes are
  `agent_runtime` and `model_line`. Different-axis requirements are
  conjunctive.
- `avoid`: zero or more avoid predicates on those same axes.
- `dry_run`: when true, Duo uses the same resolver, creates no session and
  no durable launch-resolution record, and marks the result as a preview.

Repeated equal requirements and avoids normalize while preserving provenance.
Contradictory same-axis requirements are `invalid.request`.

An ordinary successful result contains:

```json
{
  "session_id": "ses_...",
  "launch_resolution_id": "lrr_...",
  "selection": "ordered",
  "leaves": [
    {
      "name": "reviewer",
      "agent_runtime": "codex",
      "model_line": "gpt-5.6",
      "declared_kind": "open",
      "outcome": "selected",
      "relented_avoids": []
    }
  ]
}
```

Every successful leaf reports its resolved model-line label. A multi-leaf
result has one label per leaf and no fabricated session-wide label.
`declared_kind` is `determined` for a one-candidate leaf and `open` otherwise.
`relented_avoids` lists only the avoid predicates matched by the selected
assignment. A dry-run result omits `session_id` and `launch_resolution_id`,
sets a preview mark, and does not promise reuse of a random draw.

The launch-resolution record is the full explanation. It contains a Duo ID,
request, caller, time, requested preset, effective-config and declaration
digests, normalized constraints and provenance, selection mode and random
evidence, every per-leaf candidate and elimination reason, relation
rejections, avoid restoration and matched relents, the final selected tuple,
and eventual session and runtime-instance links. Config names appear as
`declaration_locator` values, never as Duo object IDs. Paths, environment,
raw adapter failures, and sensitive local details stay behind
`diagnostics.read`.

Resolution errors use `preset.not_found`, `invalid.request`,
`config.composition_unresolved`, `launch.constraints_exhausted`,
`launch.no_eligible_candidate`, and `internal.failure` as specified in
[`duo-vnext-access-errors-audit.md`](./duo-vnext-access-errors-audit.md).
Every resolution error has `effect: no_effect`.

**`duo.config/v3` launch surface (2026-08-24 handoff 22 amendment;
ratified in
[`notes/43-config-v3-change-control.md`](./notes/43-config-v3-change-control.md)).**
The request and result schemas grow by compatible `duo.external/v1` adds.
The prose above states the handoff-18 surface and keeps its provenance.

*Request.* `require` and `avoid` accept three axes: `agent_runtime`,
`model_line`, and `model_family`. At most one required value per axis still
holds, and different-axis requirements are still conjunctive. The request
also carries an optional session-host override that names the host kind. The
override is deduction rung 0: it outranks the workspace↔host correlation,
cwd-correlation, the ambient environment, and the policy default, and it
stamps the result `host_source: explicit-flag`. The override feeds resolution and never
bypasses elimination policy, so an override onto a disabled kind still
eliminates every join and the error says so. The session host stays off the
require and avoid axis list: it is deduced, never selected. `preset` and
`dry_run` are unchanged. (2026-08-26 handoff 24 amendment; notes/51.) The
request also carries optional `target` (`tab` or `pane`) and
`remain_on_exit` (boolean). Both are launcher inputs in the `--host` mold.
They never feed the resolver. Close-on-exit is the product default.
`--remain-on-exit` is the per-launch opt-out. An explicit `--target` naming
a container the deduced kind does not have is `invalid.request`.

*Result.* Each successful leaf reports its `model_family` beside its
`model_line`, and the composition minted for it — the join of the leaf's
launch variant with the one deduced host instance. A composition is never
authored in configuration; the result and the launch-resolution record are
where it is named. The result also names the deduced host instance and its
`host_source`, one of `explicit-flag`, `workspace-correlation`,
`cwd-correlation`, `ambient-env`, or `policy-default`, with the
captured-but-outranked evidence beside it. The result also names `target`
and `target_source` (`explicit-flag`, `config-default`, or `built-in`)
(2026-08-26 handoff 24 amendment; notes/51 record 2). Host and instance appear for visibility only. They are never
chaining identity: `session_id` and `launch_resolution_id` remain the
identities a caller chains on.

```json
{
  "host": {
    "kind": "herdr",
    "instance_id": "hin_...",
    "host_source": "workspace-correlation",
    "outranked_evidence": [{"source": "ambient-env"}, {"source": "cwd-correlation"}]
  },
  "target": "tab",
  "target_source": "config-default",
  "leaves": [
    {
      "name": "reviewer",
      "agent_runtime": "pi",
      "model_line": "gpt-5.6-codex",
      "model_family": "gpt",
      "composition": {"variant": "pi_gpt56_codex", "host_kind": "herdr"},
      "declared_kind": "open",
      "outcome": "selected",
      "relented_avoids": []
    }
  ]
}
```

*Errors.* `config.variant_unresolved` is the declaration-ambiguity code
under `duo.config/v3`. `config.composition_unresolved` stays registered and
is emitted for `duo.config/v2` documents only. `launch.host_unresolved`
reports a deduction that produced no host and carries the deduction trail.
Both new codes have class `unavailable` and `effect: no_effect`. The details
of `launch.constraints_exhausted` and `launch.no_eligible_candidate` grow
per-reason elimination tallies, with static-elimination reasons included in a
mixed case, the deduced host with its `host_source`, the evidence-bundle
references, and the pointer set: the host override flag,
`duo provider enable`, and `duo workspace host rebind`.

*Launch-resolution record.* The record keeps its name, authorship, and
immutability, and grows the minted composition per leaf, the `host_source`
with its outranked-evidence entries, the evidence-bundle references (the
correlation fact ID with its fingerprint set, the ambient captures, and the
provider fact IDs), per-leaf `model_family`, and the `provider_disabled`
elimination reason.

## 6. Operation registry

Each public semantic operation has one registry entry with these fields:

| Field | Meaning |
|---|---|
| `name` | Stable dotted operation name. |
| `projectability` | `deterministic`, `local_admin`, or `human_llm_porcelain`. |
| `request_schema` | Named schema reference and digest. |
| `result_schema` | Named schema reference and digest. |
| `permissions` | Required permission names. |
| `idempotency` | Required, optional, or not applicable. |
| `stream_family` | Result stream when one exists. |
| `audit` | Required audit category. |

Deterministic operations can project into CLI, MCP, and presentation forms.
Local administration operations project into CLI and an authorized local
administration client. Human-only LLM porcelain stays CLI-only. It is absent
from MCP tools and generated harness instructions.

### 6.1 `session.launch`

| Field | Value |
|---|---|
| `name` | `session.launch` |
| `projectability` | `local_admin` |
| `request_schema` | `duo.session.launch.request/v1` (digest assigned when the schema is authored) |
| `result_schema` | `duo.session.launch.result/v1` (digest assigned when the schema is authored) |
| `permissions` | `session.manage` |
| `idempotency` | Required for spawn. Not applicable when `dry_run` is true. |
| `stream_family` | None. There is no initial result stream. |
| `audit` | Required. Every spawn attempt and every resolution failure is audited. |

`projectability` and `permissions` answer different questions. `session.manage`
authorizes launch for a subject on an allowed scope. It does not decide which
projections may offer the operation. An in-session agent that can launch more
agents creates a self-activation and resource-amplification loop. The initial
generated MCP tool set therefore omits `session.launch`. CLI and separately
enrolled local-administration clients can launch. A later dated contract can
add deterministic MCP launch without changing the request and result schemas.

(2026-08-24 handoff 22 amendment: the `duo.config/v3` request and result
additions are compatible adds to `duo.session.launch.request/v1` and
`duo.session.launch.result/v1`. Every other field of this entry is
unchanged, and `session.launch` stays absent from the initial generated MCP
tool set.)

### 6.2 Provider state operations (2026-08-24 handoff 22 amendment)

Under `duo.config/v3` a provider is a launch-variant tag, and its toggle is
state, not configuration. There is no `providers` configuration block, and a
provider exists by being named on a variant. The standing state is the
latest `provider.disabled` or `provider.enabled` fact per provider name, and
the default is enabled. A disabled provider is a static launch elimination
with reason `provider_disabled`. It never relents. Toggling an unknown name
is allowed, and the result says that no variant names that provider.

| Field | Value |
|---|---|
| `name` | `provider.disable` |
| `projectability` | `local_admin` |
| `request_schema` | `duo.provider.disable.request/v1` (digest assigned when the schema is authored) |
| `result_schema` | `duo.provider.disable.result/v1` (digest assigned when the schema is authored) |
| `permissions` | `session.manage` |
| `idempotency` | Optional. A repeat accepts a new fact and leaves the same standing state. |
| `stream_family` | None. |
| `audit` | Required. Every toggle records the accepted fact ID and the caller. |

| Field | Value |
|---|---|
| `name` | `provider.enable` |
| `projectability` | `local_admin` |
| `request_schema` | `duo.provider.enable.request/v1` (digest assigned when the schema is authored) |
| `result_schema` | `duo.provider.enable.result/v1` (digest assigned when the schema is authored) |
| `permissions` | `session.manage` |
| `idempotency` | Optional. A repeat accepts a new fact and leaves the same standing state. |
| `stream_family` | None. |
| `audit` | Required. Every toggle records the accepted fact ID and the caller. |

| Field | Value |
|---|---|
| `name` | `provider.list` |
| `projectability` | `local_admin` |
| `request_schema` | `duo.provider.list.request/v1` (digest assigned when the schema is authored) |
| `result_schema` | `duo.provider.list.result/v1` (digest assigned when the schema is authored) |
| `permissions` | `session.manage` |
| `idempotency` | Not applicable. The operation never writes. |
| `stream_family` | None. |
| `audit` | Not required. The read returns standing policy and no sensitive local detail. |

`session.manage` authorizes these operations because a provider toggle
changes which candidates a launch may reach. `projectability` is
`local_admin` for the reason `session.launch` gives: an in-session agent that
can re-enable its own launch pool creates a self-activation loop. The initial
generated MCP tool set therefore omits all three operations. CLI and
separately enrolled local-administration clients can call them.

### 6.3 Workspace host correlation operations (2026-08-24 handoff 22 amendment)

A workspace carries a rebindable workspace↔host-instance correlation,
fingerprinted with the session name, pane ID, terminal ID, and process
information. It is written at a first bind behind `workspace.host_bound`, and
changed only behind `workspace.host_rebound`. The correlation routes new
spawns only. It never selects between live sessions.

| Field | Value |
|---|---|
| `name` | `workspace.host.show` |
| `projectability` | `local_admin` |
| `request_schema` | `duo.workspace.host.show.request/v1` (digest assigned when the schema is authored) |
| `result_schema` | `duo.workspace.host.show.result/v1` (digest assigned when the schema is authored) |
| `permissions` | `workspace.read`, plus `diagnostics.read` for the instance locator and the fingerprint set |
| `idempotency` | Not applicable. The operation never writes. |
| `stream_family` | None. |
| `audit` | Required. The result is a sensitive read of local instance detail. |

| Field | Value |
|---|---|
| `name` | `workspace.host.rebind` |
| `projectability` | `local_admin` |
| `request_schema` | `duo.workspace.host.rebind.request/v1` (digest assigned when the schema is authored) |
| `result_schema` | `duo.workspace.host.rebind.result/v1` (digest assigned when the schema is authored) |
| `permissions` | `workspace.mutate` |
| `idempotency` | Required. The verb accepts one `workspace.host_rebound` fact per accepted request. |
| `stream_family` | None. |
| `audit` | Required. The record names both instances, their fingerprints, and the evidence. |

The rebind verb requires an explicit target, records the old and the new
instance with fingerprints, names its evidence, and never runs implicitly. It
is the audited sibling of the workspace path rebind, so it takes
`workspace.mutate`. The read verb never writes. Both are `local_admin` and
are absent from the initial generated MCP tool set, because the correlation
decides where a later launch lands.

## 7. Conformance rule

The core wire schemas are in [`schemas`](./schemas). The fixtures in
[`fixtures/duo-external-v1`](./fixtures/duo-external-v1) are normative
examples. An implementation must validate each fixture against its schema. It
must also prove that CLI JSON, MCP structured content, and presentation JSON
decode to the same semantic request or result.
