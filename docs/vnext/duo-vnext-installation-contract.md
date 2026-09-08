<!-- Snapshot from the terminal-multiplexers archive at its 2026-09 freeze
     (handoff 27, repo-consolidation Stage B). Authored here from now on.
     Relative links that do not resolve in this repo refer to the archive;
     cite it by tag, not branch. -->

# Duo vNext configuration, manifest, assets, and harness projections

> Status: **normative for vNext installation projections.**

## 1. Configuration boundary

Configuration declares operator intent. It does not contain Duo object IDs as
an identity source, accepted facts, observations, commands, current views, or
audit history.

Duo reads strict YAML 1.2. Each root document has a named configuration
schema. Documents with `schema: duo.config/v1` remain valid under that
schema. The successor family `duo.config/vN` (the next integer is assigned
when the schema is authored; 2026-08-18 handoff 18 amendment) adds a
required composition `model_line` and a `presets` root. (2026-08-24
handoff 22 amendment: the successor schema is `duo.config/v3`. It
late-binds the session host, retires the `compositions` root (2026-08-24
handoff 22 amendment), and adds a required variant `model_family`. See the `duo.config/v3` clause at the end
of section 1.1.) Duplicate keys and
unknown fields are errors. Duo validates the complete effective
configuration before it changes running state. A failed reload keeps the
prior valid configuration.

### 1.1 Objects

The root contains these optional named objects. `presets` and required
composition `model_line` belong to `duo.config/vN`. They are unknown fields
under `duo.config/v1`.

| Object | Purpose |
|---|---|
| `authority` | Store, socket, retention, and safe local resource limits. |
| `workspaces` | Discovery roots and enrollment policy. Paths remain correlations. |
| `session_hosts` | Named Herdr, Solo, tmux, or future OwnPTY integration instances. (2026-08-24 handoff 22 amendment: under `duo.config/v3` this object is host-kind policy only — enabled kinds, preference order, and deduction sources. Host instances and socket paths are state, never authored.) |
| `agent_runtimes` | Portable agent definitions and observation or harness settings. (2026-08-24 handoff 22 amendment: under `duo.config/v3` the agent runtime also declares the resolved `executable` and the base `arguments`.) |
| `launch_variants` | Host-specific command, environment, and mode for an agent definition. (2026-08-24 handoff 22 amendment: under `duo.config/v3` a launch variant is host-free and declares a required `model_line`, a required `model_family`, an optional `provider` tag, and optional `append_arguments`.) |
| `compositions` | Named determined selections of one session host, agent runtime, launch variant, and declared model-line label. (`duo.config/v2` only — 2026-08-24 handoff 22 amendment: `duo.config/v3` retires this root object. A composition is minted at resolution and named only in the launch-resolution record.) |
| `presets` | Named launch intents. Each preset has one or more uniquely named leaves. Each leaf holds an ordered list of composition references. (2026-08-24 handoff 22 amendment: under `duo.config/v3` each leaf holds an ordered list of launch-variant references.) |
| `control` | Queue expiry, attempt deadline, quiet interval, backpressure, and retry ceilings. |
| `collaboration` | Retention, size, causal, fan-out, rate, retry, backoff, and dead-letter ceilings. |
| `presentation_clients` | Enrollment policy and grant templates for local clients or gateways. |
| `assets` | Explicit asset roots and named override selections. |
| `projections` | Enabled harness targets and managed installation roots. |

A portable agent definition never contains a host program path. A launch
variant binds one portable definition to one host integration and declares the
resolved executable, arguments, working-directory policy, environment allow
list, and mode.

A composition still determines exactly one session host, agent runtime, and
launch variant. Under `duo.config/vN` it also declares exactly one non-empty
model-line label. A preset candidate is only a composition reference.
Materialization obtains agent runtime, model line, launch variant, and session
host from that one determined declaration. A referenced composition must
resolve without ambiguity. A missing or ambiguous composition or variant
link, a malformed preset, or an exceeded declaration-complexity limit returns
`config.composition_unresolved` with no partial launch. That code remains
declaration ambiguity. It does not mean launch-time constraint exhaustion.

A preset has at least one uniquely named leaf. Each leaf has at least one
composition reference and no duplicate reference. `selection` defaults to
`ordered`; `random` is an explicit per-preset opt-in. Candidate array order is
preference order. `distinct_model_line` is the only cross-leaf relation in
this contract. There is no reserved `default`, fallback preset, recursive
fallback, capability predicate, pin, or launch-time `prefer`. A one-candidate
leaf is the declared determined case. A larger declared list remains an open
leaf even when `require` later narrows its surviving pool to one.

Duo completes launch resolution for the requested preset before it creates a
runtime instance. A failed resolution launches nothing. Launch resolution
consults the merged launch-configuration digest, normalized launch constraints,
accepted immutable adapter-conformance record digests, and enabled session-host
declarations. It does not consult live reachability, process presence, adapter
health, selected or effective configuration, source provider, vendor, source
model, normalized family, executable-path state, or a probe run for this
launch. In explicit random mode it also consults entropy and draw evidence.

For one requested preset, Duo validates the declaration, materializes each
leaf's ordered candidates, removes candidates unsupported by accepted
immutable conformance evidence or disabled host declarations, applies every
non-relenting `require`, applies every `avoid`, and enumerates complete
assignments in leaf declaration order and candidate array order. It rejects
tuples that violate `distinct_model_line`. If a strict complete assignment
exists, ordered mode selects the first and explicit random mode selects one
complete assignment. If no strict complete assignment exists and at least one
avoid contributed to that absence, Duo repeats assignment over the
post-require, pre-avoid pools and reports the matched relents. If no complete
assignment survives after avoid restoration, the result is
`launch.constraints_exhausted` when a valid require or relation left the pool
empty, or `launch.no_eligible_candidate` when installed evidence left no
complete assignment before request constraints ran. An unknown preset is
`preset.not_found`. Contradictory same-axis requirements are
`invalid.request`. Every resolution error has `effect: no_effect`.

Duo authors the launch-resolution record after the complete plan resolves and
before any host launcher prepares or starts a leaf. Configuration locators in
that record are declaration locators, not Duo object identities. A failed
resolution creates no session and no launch-resolution record in session
history.

**`duo.config/v3`: late-bound session hosts (2026-08-24 handoff 22
amendment).** Ratified in
[`notes/43-config-v3-change-control.md`](./notes/43-config-v3-change-control.md)
from the sketch in
[`notes/42-config-v3-late-binding.md`](./notes/42-config-v3-late-binding.md).
The clauses below state the successor schema. The prose above states
`duo.config/v1` and `duo.config/v2` and keeps its provenance.

*Objects.* `session_hosts` declares host-kind policy only: an ordered
`prefer` list of kinds, per-kind `enabled` stanzas, and per-source `deduce`
stanzas over the closed source set `workspace`, `env`, and `default`. It
never names a host instance and never carries a socket path.
`agent_runtimes` declares the resolved `executable` and the base
`arguments`. A launch variant is host-free: it declares its
`agent_runtime`, a required non-empty `model_line`, a required non-empty
`model_family`, an optional `provider` tag, and optional
`append_arguments`. The `compositions` root object is retired (2026-08-24
handoff 22 amendment). A preset candidate is a launch-variant reference.

*Host-kind policy.* An absent kind stanza and an absent `enabled` flag both
mean enabled. `prefer` is the only ordered field in the block, and it is
consumed only at the policy-default rung. The `deduce` stanzas enable or
disable deduction sources. They never rank them. The deduction ranking is
fixed and is not configurable: explicit launch override flag > workspace↔host
correlation > cwd-correlation > ambient environment > policy default
(2026-08-26 handoff 24 amendment; notes/51 record 6). Each kind stanza may
name `launch_target` (`tab` or `pane`, absent means the host's built-in
default) and `close_on_exit` (boolean, absent means true). Both are launcher
inputs, not resolution inputs.

*Materialization.* Launch resolution gains a materialization phase that runs
before validation. M1 resolves the workspace (`--workspace`, else the working
directory, unchanged) and deduces exactly one session-host instance for this
launch by the fixed ranking. It emits the deduced instance, a `host_source`
tag with the closed values `explicit-flag`, `workspace-correlation`,
`cwd-correlation`, `ambient-env`, and `policy-default`, and every captured-but-outranked piece
of evidence. M2 snapshots the standing `provider.disabled` and
`provider.enabled` facts by fact ID. After M2 nothing reads the environment,
the store, or the filesystem. M1 reads the ambient environment once, as
recorded evidence with provenance; the resolver never probes it; and nothing
checks the reachability of the deduced instance. A dead socket fails at
spawn, not at resolution. When no rung produces a host, materialization
returns `launch.host_unresolved` with the deduction trail and launches
nothing.

*Resolution over the materialized inputs.* Resolution consults five
immutable inputs: the merged launch-configuration digest, normalized launch
constraints with their provenance, accepted immutable adapter-conformance
record digests, draw evidence in explicit random mode, and the materialized
evidence bundle (the correlation fact ID with its fingerprint set, the
ambient captures, and the provider fact IDs). Materialization joins each
candidate variant with the one deduced host, and that join mints the
composition. Static elimination removes joins whose host kind is disabled
(`session_host_disabled`), joins whose variant carries a disabled provider
(`provider_disabled`, installed policy exactly as host-kind disable, and an
untagged variant is never affected), and joins without accepted conformance
evidence, which is keyed on host kind, host version, and runtime kind and no
longer on a config-named instance. None of these relent. `require` and
`avoid` range over three axes: `agent_runtime`, `model_line`, and
`model_family`. Cross-leaf relations are `distinct_model_line` and
`distinct_model_family`. Whole-plan atomic selection, candidate array order
as preference order, and avoid-relent that never crosses a static
elimination are unchanged. The session host is deduced, never selected: it
is not a require or avoid axis, and the explicit override flag covers the
remaining need without bypassing elimination policy.

*Errors.* `config.variant_unresolved` is the declaration-ambiguity code
under `duo.config/v3`: a missing or ambiguous variant or runtime reference, a
malformed preset, or an exceeded declaration-complexity limit, with no
partial launch. It does not mean launch-time constraint exhaustion.
`config.composition_unresolved` stays registered and is emitted for
`duo.config/v2` documents only. The causal split decides the exhaustion row:
with no caller constraint contributing, the row is
`launch.no_eligible_candidate`; with a `require`, an `avoid`, or a relation
contributing, the row is `launch.constraints_exhausted`, and a mixed case
carries both tallies in its details. Every resolution error keeps
`effect: no_effect`.

*Configuration and state.* Configuration declares what can exist and how it
is built. State records what is currently true. Host instances and socket
paths are state: they are workspace↔host-instance correlation records in
`duo.db`, fingerprinted with the session name, pane ID, terminal ID, and
process information, written behind the `workspace.host_bound` and
`workspace.host_rebound` facts, and changed only by the explicit audited
`duo workspace host rebind` verb, with `duo workspace host show` as the read
verb. A first bind whose `host_source` is `ambient-env` or
`cwd-correlation` asks for confirmation; a bind from `explicit-flag` or
`policy-default` writes silently with loud output (2026-08-26 handoff 24
amendment; notes/51 record 6). Provider toggles are state: there is no `providers`
configuration block, a provider exists by being named on a variant, and the
toggle is a `provider.disabled` or `provider.enabled` fact behind
`duo provider disable`, `enable`, and `list`, with default enabled.
Compositions are neither configuration nor state. They are minted at
resolution and named only in the launch-resolution record.

*Resolve before spawn (unchanged).* Duo completes launch resolution for the
requested preset before it creates a runtime instance, and authors the
launch-resolution record before any host launcher prepares or starts a leaf.
The deduced instance and its `host_source` are explicit in launch output and
in `duo doctor`.

### 1.2 Precedence

Configuration layers apply from lowest to highest priority:

1. Shipped defaults under the installed package data directory.
2. Optional system policy at `/etc/duo/config.yaml`.
3. User configuration at `$XDG_CONFIG_HOME/duo/config.yaml`.
4. Files named by repeatable `--config` flags, in flag order.
5. Explicit flags for the current command.

Environment variables can select configuration and data roots. They cannot set
semantic policy one field at a time. Duo does not load a repository or
workspace configuration file automatically. This rule prevents an untrusted
checkout from granting control or changing launch behavior.

An administrator can set non-overridable system ceilings. A later layer can
tighten a ceiling but cannot expand it. Secrets use OS keyring references or
rooted credential-file references. General environment interpolation and
inline secret values are invalid.

Configuration layers may remove preset candidates, restrict allowed preset,
composition, runtime, or model-line labels, force `ordered` instead of
`random`, and add non-relenting requirements (2026-08-18 handoff 18
amendment). (2026-08-24 handoff 22 amendment: under `duo.config/v3` the
restrictable label list is preset, launch variant, agent runtime, model
line, and model family. The composition label leaves the list with its
referent. The tighten-only layering semantics are unchanged.) They cannot add candidates or enable random outside a
higher-authority ceiling. Candidate order is inherited from the
highest-authority declaration that is permitted to set it; later layers that
lack that permission may only remove entries without reordering. Soft `avoid`
is request policy, not a non-overridable installation ceiling. Launch flags
run after effective configuration and narrow further. They cannot add or
rewrite a candidate. Relenting a request avoid therefore does not expand the
configured candidate ceiling or undo a system requirement.

### 1.3 Migration

`duo config validate` reports every error without modifying a file.
`duo config migrate --to duo.config/vN` writes to stdout by default. An
explicit `--write PATH` uses create-new or validated replacement semantics. It
never overwrites an unrecognized file.

A migration report lists renamed, defaulted, rejected, and manual fields. Duo
never migrates configuration silently during daemon startup.

Migration to the successor schema (2026-08-18 handoff 18 amendment):

1. Preserves existing host, runtime, variant, and composition intent.
2. Adds the new `model_line` field as `manual` when the source did not author
   one and never infers it from arguments, runtime kind, observed provider,
   vendor, source model, or normalized family.
3. Proposes one same-name preset with leaf `main` and one reference to each
   migrated determined composition after its model line is supplied.
4. Writes the draft and migration report to stdout by default.
5. Refuses `--write` while any manual field remains or validation fails.
6. Uses create-new or validated-replacement semantics for `--write`.
7. Never migrates silently during daemon startup.

`compositions` are v2-only (2026-08-24 handoff 22 amendment) and remain
determined declaration atoms under `duo.config/v2`. Presets supersede direct
composition naming only as the open `session.launch` request surface. For
`fixtures/duo-external-v1/config.json`, `compositions.review` (v2 only,
2026-08-24 handoff 22 amendment) was that determined atom. Migration to
`duo.config/v2` reports `compositions.review.model_line` (v2 only,
2026-08-24 handoff 22 amendment) as a manual field and adds
`presets.review.leaves.main.candidates[0]` as a reference to that
composition. The draft is not writable until an author supplies the model
line.

Migration to `duo.config/v3` (2026-08-24 handoff 22 amendment).
`duo config migrate --to duo.config/v3` transforms a valid `duo.config/v2`
document:

1. Moves each composition's `model_line` onto its launch variant.
2. Moves `executable` and the base `arguments` onto the agent runtime, and
   leaves per-variant additions as `append_arguments`.
3. Drops `session_host` and `socket_path` from the variant and reports them
   as state to bind. The report prints the socket path so the operator can
   bind or rebind the workspace↔host correlation.
4. Rewrites every preset candidate as a launch-variant reference and
   retires the `compositions` root (2026-08-24 handoff 22 amendment).
5. Derives host-kind policy, with `prefer` in order of first appearance
   among the migrated declarations.
6. Reports `model_family` as `manual` on every variant. It infers the label
   from nothing: not from runtime kind, executable, arguments, observed
   provider, vendor, source model, or normalized family.
7. Refuses `--write` while any `model_family` is still manual or validation
   fails, and keeps create-new or validated-replacement semantics.
8. Refuses a `duo.config/v1` input. There is no v1 to v3 path.
9. Never runs during daemon startup.

## 2. Static manifest

`duo manifest --output json` emits `duo.manifest/v1`. The installed binary is
the manifest authority.

The root contains:

- Product version, build identity, manifest version, and manifest digest.
- Supported public, configuration, and projection-format versions.
- Operation registry entries and CLI command metadata.
- MCP tool definitions derived from deterministic projectable operations.
- Presentation route and stream metadata.
- Harness target descriptors and generated component types.
- Shipped asset paths, media types, and digests.
- Configuration migrations.
- Packaged adapter identities, version ranges, and conformance-record digests.

The manifest can declare what an adapter implementation knows how to do. It
cannot claim that an operation is live, connected, authorized, exact, or
available for a current Duo session. Those claims belong to runtime
operation-support views.

Every operation uses one projectability class:

| Class | Projection |
|---|---|
| `deterministic` | Eligible for CLI, MCP, presentation, and generated harness metadata. |
| `local_admin` | Eligible for CLI and separately enrolled local administration clients. |
| `human_llm_porcelain` | CLI-only. Omitted from MCP and generated harness artifacts. |

Manifest signatures are optional in v1. Package-manager integrity remains the
distribution trust root. Digests detect drift and accidental modification.
They do not prove publisher identity.

## 3. Asset resolution

Shipped assets live below `<prefix>/share/duo/assets/` and are read-only.
User-authored overrides live below `$XDG_CONFIG_HOME/duo/assets/`.

For one declared logical asset path, Duo resolves an exact user override first
and the shipped asset second. A missing required asset is an error. Resolution
rejects absolute paths, parent traversal, and symlinks that escape the selected
root.

The manifest records shipped digests. Diagnostics record the effective source
and digest. An upgrade can replace a shipped asset. It never modifies a user
override. `duo doctor` reports an override whose shipped base changed so the
user can review it.

## 4. Harness projection contract

The installed system-wide `duo` binary generates and installs every harness
projection. A source checkout, plugin marketplace entry, or presentation does
not become a second generator authority.

Filesystem projection is the v1 common denominator for Claude Code, Codex,
OpenCode, and Pi. Each target receives a generated instruction asset that
describes only deterministic, authorized Duo plumbing. A target renderer can
also install a hook declaration, MCP configuration fragment, or small bridge
when a pinned conformance record proves that component.

The known target posture is:

| Target | Required v1 component | Optional conformed component |
|---|---|---|
| Claude Code | Filesystem skill. | Hook declarations and MCP configuration. |
| Codex | Filesystem skill. | MCP configuration and supported hook declarations. |
| OpenCode | Filesystem instruction projection. | Plugin, hook, or MCP configuration after a current installation probe. |
| Pi | Filesystem skill. | Small lifecycle extension or generic MCP bridge. |

(2026-08-26 handoff 25 amendment: the delegation-loop milestone's
interim skill lives in this repository at
[`skills/duo-delegation-loop/SKILL.md`](./skills/duo-delegation-loop/SKILL.md)
as the normative source. Install is by hand into the orchestrating
harness. There is no Stage 5 renderer in this milestone. Generated MCP
configuration fragments stay in the table above and stay out of this
milestone. See
[`handoffs/25-delegation-loop-scope.md`](./handoffs/25-delegation-loop-scope.md).)

The manifest declares exact target paths and supported harness-version ranges
only after the renderer has version-pinned fixture or live evidence. A missing
OpenCode or Codex hook fact cannot become an optimistic path. The renderer
installs the proven common component and reports optional components as
`unverified`.

Generated instructions preserve notification IDs, reference URIs,
idempotency keys, and causal context. They do not copy protected collaboration
content into activation prompts. They do not include terminal-input authority
or human-only LLM porcelain.

Generated hook declarations must keep terminal-state hooks synchronous
(2026-08-14 review amendment, G-18). Stop and SessionEnd hooks declared
asynchronous lose their event at process exit, as the hook probes verified.
A renderer must not emit an asynchronous terminal-state hook, and the
conformance suite verifies the generated declarations.

Some harnesses gate hooks behind a trust registration. Codex records a
`trusted_hash` for each hook definition and silently disables an edited hook
until an operator re-trusts it (2026-08-14 review amendment, G-19). When a
conformance record proves the target's documented trust mechanism, the
installer completes that registration for each generated or regenerated
hook. Otherwise the installer reports the hook as `untrusted` and names the
manual re-trust step. A regenerated hook without a matching trust
registration is a silent event-channel loss, not a working installation.

## 5. Artifact stamp and ownership

Each managed projection root contains `.duo-generated.json` with
`schema: duo.projection-stamp/v1`. The stamp records:

- Duo product version and manifest digest.
- Projection-format version.
- Target harness and tested version range.
- Component list and source asset digests.
- Relative generated file paths and content digests.
- Generation time and installation ID.

Every text file also starts with a short generated marker when its format
permits comments. The stamp is the ownership record.

Installation renders into a private staging directory and validates every
digest before an atomic placement. The installer follows these rules:

1. Create a missing target root.
2. Replace files only when a valid prior stamp owns them and their current
   digests still match that stamp.
3. Report a modified generated file as `projection.modified`.
4. Report an unowned destination as `projection.user_file_conflict`.
   The shared error specification registers both codes under class
   `conflict`, and both carry `effect: no_effect`.
5. Never replace an unowned or modified file through an ordinary install.
6. Remove only files that the current valid stamp owns.

An operator must move or rename a conflicting user file before installation.
There is no broad force flag that silently overwrites user work.

## 6. Drift states

`duo doctor` regenerates expected metadata from the installed binary and
compares it with each stamp. It reports one of these closed states:

| State | Meaning |
|---|---|
| `current` | Stamp, files, target range, and current manifest agree. |
| `missing` | A stamped file or required projection is absent. |
| `stale` | Another manifest or projection-format version generated the files. |
| `modified` | A stamped file digest differs. |
| `incompatible` | The detected harness version is outside the tested range. |
| `unowned_conflict` | An unowned file blocks the expected destination. |
| `untrusted` | The target harness gates the component behind a trust registration, and the current registration does not match the installed file. |

`duo install TARGET --repair` repairs `missing` or `stale` files only when the
valid stamp proves ownership. It does not repair `modified` or
`unowned_conflict` files. For `untrusted`, repair re-runs the trust
registration only when the conformance record proves the mechanism.
Otherwise `duo doctor` reports the manual re-trust step.

## 7. Upgrade scenario

After a Duo upgrade, the manifest digest changes. `duo doctor` reports the old
harness projection as `stale`. The user runs `duo install TARGET --repair`.
Duo replaces only unchanged, stamped files. User asset overrides and unowned
harness files remain unchanged.
