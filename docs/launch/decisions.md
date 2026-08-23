# `launch` — decisions this step made

The design of record is `notes/30-preset-selection-design.md` §6.3–§6.9 and
§7 in the planning repository, projected by `duo-vnext-go-architecture.md`
§5.2 (the launch resolver sits before `HostLauncher.PrepareLaunch`) and §4.2
(the launch-resolution transaction boundary), with the wire shapes fixed by
`contracts/schemas/duo-external-v1.schema.json` and the two
`session-launch*.json` fixtures.

This file records what building the resolver *forced*: the calls those
documents left open, the one place the implementation deviates from their
wording, and what it deliberately leaves for a later step.

## The pipeline, as implemented

`Resolver.Resolve` is §6.7 steps 1–9 in order, with one stage per function
so the order is legible at the call site: `normalize` → `materialize` →
`applyInstalledEvidence` → `applyRequire` → `applyAvoid` → `enumerate` →
(relent) `relentAvoids` + `enumerate` → `selectAssignment` → `buildRecord`.
Step 10 — durably recording the resolution before any spawn — is
`Launcher.Launch`, because it is the only step that needs anything outside
the package.

Two orderings inside that are load-bearing and are not obvious from the
prose:

- **The installed-evidence drop runs before every constraint.** That is what
  keeps `launch.no_eligible_candidate` (unavailable: this installation
  cannot launch any of these) from ever being reported as
  `launch.constraints_exhausted` (invalid: your narrowing was too tight).
  `TestNoEligibleCandidateOutranksConstraints` pins it with a request where
  both would otherwise apply.
- **The relent re-runs over the post-require pools, never the declared
  ones.** §6.6's "relenting a request avoid does not undo a system
  requirement" is a property of *which* pool the second enumeration reads,
  and nothing else in the code enforces it.

## Deviation: leaf order is lexical, not declaration order

§6.7 enumerates complete assignments "in leaf declaration order and
candidate array order". Candidate array order is preserved end to end.
**Leaf declaration order is not available**: `internal/config`'s resolved
`Preset.Leaves` is a `map[string]config.PresetLeaf`, so YAML declaration
order is gone before a document reaches this package.

`materialize` therefore sorts leaf names lexically. That is deterministic,
replayable, and stable across re-reads of the same document, which is what
§7.2's determinism contract actually requires of leaf order — but it is
*not* the same rule, and a declaration whose leaves are authored in a
meaningful order gets a different enumeration than §6.7 describes.

Closing it needs an ordered leaf list in `internal/config` (a
`[]PresetLeaf` with names, or a parallel `LeafOrder []string`), which is
that package's call, not this one's. `launch`'s own materialized shape is
already an ordered slice, so it needs no change when that lands. Until then
the deviation is visible in one place — the `sort.Strings(names)` in
`materialize` — and documented on it.

## The pre-spawn gate is a type, not a comment

"Record before spawn" is the whole point of the component, so it is
enforced the way an invariant should be: `Launcher.spawn` accepts only a
`*committed`, an unexported type with no exported constructor that only
`Launcher.commit` produces, and only after `Recorder.CommitLaunchResolution`
returned without error. An edit that tried to prepare a launch before (or
without) the commit does not compile.

`TestRecordCommitsBeforePrepareLaunch` asserts the observable half over a
shared call log — the recorder and the host adapter append to the same
slice, so the assertion is on the real interleaving rather than on two
counters — and `TestCommitFailureLaunchesNothing` and
`TestFailedResolutionCommitsNothingAndLaunchesNothing` cover the two
failure directions.

## The launch-resolution record has no home in the domain kernel yet

`internal/domain` is where launch facts commit: `Authority.Launch` mints the
session and the starting runtime instance and commits them through
`Repository.CommitLaunchResolution` → `store.LaunchResolutionTx`. That is
§4.2's boundary, and it is the right transaction for the record.

**The kernel has no way to carry the record.** Its fact vocabulary
(`internal/domain/fact.go`) has `session.launched` but no launch-resolution
fact, and `domain.LaunchRequest` has no field for a resolution ID or a
resolution body — only a free-text `Reason`. So a domain-backed recorder
today would have to either drop the record's content on the floor or smuggle
it through a reason string.

This step therefore defines the seam (`launch.Recorder`, returning the
`Commit` links the same transaction created) and stops at the package
boundary rather than editing the kernel. What the kernel needs, when
somebody owns that change:

- a fact kind (`launch.resolved` or similar) carrying the record's ID and
  serialized body;
- a `LaunchRequest.Resolution` field (ID plus body) so `Authority.Launch`
  emits that fact in the same `Change` as `session.created`,
  `instance.started`, and `session.launched`; and
- nothing else — the session and instance links flow back through the
  existing `LaunchResult`.

Until then the durable record is whatever a caller wires behind `Recorder`,
and the ordering guarantee holds regardless of which implementation that is.

## `Support` is required, because a permissive default would change the rung

§7.1 accepts the middle rung: configuration **plus installed evidence**. A
resolver that fell back to "everything declared is launchable" when no
evidence source was wired would silently be on the configuration-only rung
§7.5 rejects.

So `NewResolver` refuses a nil `Support`, and the permissive case is a named
type an installation opts into — `AllSupported{RecordDigest: ...}` — which
still has to name the record digest that says so. The digest is recorded on
the resolution either way, so a replay can tell which evidence was
consulted.

`Support` implementations must be pure lookups over immutable,
digest-addressed records. An implementation that called an adapter would put
launch resolution on the rejected live-state rung without the dated contract
change §7.5 requires for it, and no signature in this package would notice.

## Randomness is injected, with no package-level fallback

`RandomSource` is required only for a preset whose `selection` is `random`,
and a resolver without one refuses such a preset (`invalid.request`) rather
than reaching for `math/rand`'s global generator. Two implementations ship:
`SeededSource` (reproducible, records its seed — what tests and
replay-minded installations use) and `CryptoSource` (`crypto/rand`, records
the drawn index).

Entropy failure before the selection is recorded is `internal.failure` with
a diagnostics reference, per §6.8's last row — never a narrowing error a
caller could act on.

## Where provenance went, and why the fixture won

§6.8 asks the exhaustion failure's safe detail to carry "constraints with
provenance". The normative fixture's `constraints` object carries `axis` and
`value` only, and `launch_constraint_predicate` is
`additionalProperties: false`, so a provenance field there would be
schema-invalid.

The fixture wins. Provenance is normalized and preserved
(`NormalizedConstraint.Sources`, merged across repeated equal constraints in
first-request order) and lives on the launch-resolution record, which is
where §6.9 puts everything else that explains a resolution.

Similarly, the safe details' `survivors` lists are flat across leaves —
leaf order, then candidate order, a locator two leaves share listed once —
because the fixture fixes them as arrays of locators. Which leaf declared a
locator stays recoverable from the `candidates` rows, and the true per-leaf
pools are on the record's leaves.

## Declaration facts this step had to name

The v2 schema leaves `session_hosts`, `agent_runtimes`, and
`launch_variants` as free-form named objects, so materialization had to
decide what it reads out of them:

- **The public agent-runtime value is `agent_runtimes.<name>.kind`.** It is
  what a `--require agent_runtime=…` compares against, and §6.4 graft 7
  forbids a config key from standing in as identity — so a runtime
  declaration with no `kind` is `config.composition_unresolved`, never
  silently named after its declaration key.
- **One declared session host is one integration instance.** The session
  host's declaration name becomes `Tuple.IntegrationInstanceID`, which is
  the identifier host adapters scope evidence by.
- **`enabled: false` is the disabled-host declaration.** Anything else,
  including the field's absence, leaves the host enabled: a declaration that
  says nothing about being enabled is not a declaration that it is off. This
  is installed policy, not a reachability claim (§7.1).
- **A launch variant must declare an `executable`.** It is checked during
  materialization rather than at spawn time, so that *every* declaration
  defect is found before the record commits. A defect discovered after the
  pre-spawn commit would be a resolution that recorded a plan it could not
  carry out.

## Enumeration is exhaustive even in ordered mode

Ordered selection needs only the first surviving assignment, but the
enumeration walks the whole (validated, bounded) product anyway, because the
record wants the eligible count and the full relation-rejection table. The
bound is the point: §6.7 requires finite maxima for leaves, candidates per
leaf, and the assignment product, and an oversized declaration is a
declaration error rather than a truncated search — truncating would silently
change which candidate wins, and candidate order is a semantic input.

`DefaultLimits` is 16 leaves, 64 candidates per leaf, 4096 assignments:
generous next to anything a person writes by hand, small enough that the
product stays an in-memory walk.

## Error classes come from the registry

`internal/registry`'s `StableErrorCodes` already binds every code to its
closed class (`duo-vnext-access-errors-audit.md` §4.1). `newError` reads the
class from there rather than restating it, so a code and its class cannot
drift apart, and `TestErrorCodesAreRegistered` holds every code this package
raises to that set and to the `session.launch` descriptor's own list.

Message style follows the synced fixtures: one capitalized sentence ending
in a period, no package or verb prefix. The chassis adds `duo: <verb>: ` in
human mode.

## Small calls worth recording

- **A dry run's report carries no `launch_resolution_id`.** §6.10 makes a
  dry run commit no record; an ID referencing a record that was never
  written is worse than no ID. The result is marked `preview` instead.
- **`Resolve` takes no `context.Context`.** It performs no I/O and cannot
  block, so there is nothing to cancel; the absence is a signal about what
  the component is. `Launcher.Launch` takes one, because commit and spawn
  do.
- **The configuration digest covers only the five launch-relevant
  declaration families** (presets, compositions, launch variants, agent
  runtimes, session hosts). An edit to an unrelated section must not
  invalidate the replay of a launch it could not have changed. It is
  computed over `encoding/json`'s key-sorted encoding of the resolved
  document, so it is stable for a given document — but it is derived from Go
  field names on `internal/config`'s types, so renaming one of those fields
  changes historical digests. A digest over the source bytes would be
  stabler and would also cover the sections resolution ignores; that
  trade-off can be revisited when digests are compared across versions.
- **A spawn failure keeps the record.** §7.4: once a resolution succeeds and
  its record is durable, a later spawn failure does not erase that evidence.
  `Launcher.Launch` returns the partial result *with* the error so the
  caller can record the failure against the session the commit created.

## What this step deliberately does not do

- **No configuration layering or ceilings.** §6.6's rules for what a
  configuration layer may do (remove candidates, restrict labels, force
  `ordered`, add requirements, never add candidates or reorder) are about
  producing the effective document. This resolver consumes one already-
  resolved document; layering belongs to configuration.
- **No `duo config migrate`.** §6.4 graft 5 and §6.10's migration draft are
  a separate verb.
- **No CLI, MCP, or envelope.** `--require`/`--avoid` grammar, `--dry-run`,
  `--output json`, and the `duo.external/v1` envelope are Step 21's. This
  package produces the `Report` and the `Error` that go *inside* an
  envelope; the test builds the envelope itself to compare against the
  fixtures.
- **No audit linkage or idempotency key.** The registry marks
  `session.launch` `audit: privileged_write` with `idempotency: required`.
  Both are the operation layer's, and both need the store's audit and work
  tables that the recorder implementation will already be holding open.
- **No permission check.** `session.manage` is enforced where a request
  enters, not in the decision component.
