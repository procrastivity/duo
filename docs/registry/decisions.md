# `registry` — decisions this step made

The design of record is `duo-vnext-go-architecture.md` §8 (the binary
contains the operation registry; CLI, MCP, route, manifest, and fixture
metadata derive from it) plus the registry field table in
`duo-vnext-public-contract.md` §6. This file records what building
`internal/registry` *forced* — the calls those documents left open, and the
places the synced contract set could not back what the prose promises.

## Derive-at-runtime over generate-at-build

The registry is one authored Go table (`table.go`), and every consumer view
— the manifest operations array, the initial MCP tool set, the fixture
inventory — is a function over that table, computed when called. No code
generation step, no generated artifact checked in or built.

Why: the table is small static data (a dozen-odd rows, growing to maybe
sixty across v1), and everything §8 asks to "generate" from it is itself
data, not code — a JSON array in the manifest, a list of tool names, a list
of fixture paths. A build-time generator would add a second artifact that
can drift from its source between regenerations, a Makefile step the nix
and CI environments both have to carry, and a diff surface where the
interesting review target (the row change) is duplicated by an
uninteresting one (the regenerated output). The chassis already made this
call twice in the same direction: manifest verbs walk the live Cobra tree
rather than a generated verb list, and surface kinds are annotations on the
command rather than a generated lookup. The registry follows the house
pattern. The escape hatch is real: if a consumer ever needs the table at a
time the binary is not running (say, a docs site build), a `duo manifest`
invocation *is* the generator, because the manifest operations array is the
table.

The invariants a generator would have enforced at build time are enforced
by unit tests instead (`registry_test.go`): name/CLI/MCP/route uniqueness,
schema-reference resolution against the embedded contracts, error codes
drawn from the registered stable set, permission and audit vocabulary
membership. `conformance_test.go` adds the checks that hold the table to the
*synced contract set* rather than to this package's own prose:

- `TestProjectionCasesMatchRegistry` replays every case in
  `fixtures/duo-external-v1/projection-cases.json` against the table — the
  case's CLI argument vector must start with the row's verb path, its MCP
  tool name must equal the row's, and its concrete presentation path must
  match the row's route template segment for segment (placeholders match any
  segment; the query string is per-request detail, not part of the resource
  template). This is the metadata half of §8's "a conformance test decodes
  all three projections to an equal domain value".
- `TestManifestOperationsMatchContractSchema` reads the required-key list and
  the closed enums out of the embedded `duo-manifest-v1.schema.json` and
  checks the derived view against them, rather than restating the schema in
  Go. The binary carries no JSON Schema validator, and adding a dependency
  for one shape is not worth it yet; if full validation lands later this test
  is the natural place for it.
- `TestLaunchConstraintsExhaustedIsRegistered` pins the one error code the
  step's acceptance names: class `invalid`, claimed by `session.launch`,
  and classed the same way by the `session-launch-exhausted.json` fixture.

`make check` is the enforcement point either way.

## The single-source rule and its enforcement seam

Nothing else in the binary may hand-maintain an operation table. The wiring
that proves consumption is `internal/manifest`: `Manifest.Operations` is
`registry.ManifestOperations()` verbatim, and
`TestBuildOperationsComeFromRegistry` pins it with a `DeepEqual` so a
future hand-edited copy fails the suite. CLI verb registration and any
future MCP server get the same treatment when they land: register from
`registry.All()` / `registry.MCPTools()`, and pin with a comparison test,
not a convention.

That seam only covers consumers that exist. The repo-wide half is
`TestNoDuplicateOperationTableOutsideRegistry`, which walks every `.go` file
outside `internal/registry` and fails any file that mentions three or more
registered operation names as string literals. The threshold encodes the
distinction: one or two literals is a call site naming the operation it
implements; three is an inventory, and an inventory must come from a view
function. It is a tripwire, not a proof — a file could still hand-maintain
metadata under names built by concatenation — but it fails the obvious way
this rule gets broken, which is someone pasting the operation list into a
CLI or MCP registration file. When a legitimate consumer needs many names at
once, the fix is always the same: derive from `registry.All()` and name
nothing.

At the time of writing the repo has no such consumer at all — `internal/cli`
registers only `duo version`, so the registry's CLI, MCP, and route columns
are contract data waiting for the Stage 0/1 wiring to consume them.

## Row policy: attested rows only, and what "attested" means

The external-surfaces inventory names far more operations than the synced
contract set can back — the full collaboration families, timers,
subscriptions, notifications, workspace, audit, integration, config. A row
enters the table only when at least one of these holds in
`contracts/` as synced:

- a fixture exemplifies the operation's envelope
  (`fixtures/duo-external-v1/*.json` names it in `operation`), or
- the `duo.external/v1` schema carries a conditional block or `$defs`
  entry for it, or
- `projection-cases.json` names it in a conformance case, or
- the milestone requires it (the dogfood set below), in which case the row
  may stand on the envelope schema alone.

Everything else is a **deliberately absent row**, not a stub: adding a
half-guessed descriptor would let a generator emit manifest metadata for an
operation whose request shape, permission split, or MCP confirmation
contract is still undecided (the timer and dead-letter G-12 decisions show
those contracts are load-bearing). The TODO is structural: when the
planning repo syncs a schema or fixture for one of the absent families, the
row is added in the same change, and the resolution test makes a row with a
dangling reference unmergeable.

**Attestation is necessary, not sufficient.** A row also needs a
determinable permission, CLI verb path, and projection shape. The synced set
already contains one case where those diverge: `version-conflict.json` names
`collaboration.value.replace`, so the operation is attested, but the
collaboration families' permission split (`collaboration.mutate` vs
`collaboration.lifecycle` vs `collaboration.remove` — a G-15 decision), their
CLI paths, and their v1 routes are nowhere in the synced set. Writing that
row means guessing four columns to gain one true name, and the guess would
ship in `duo manifest` output.

So the absence is recorded rather than left implicit: `deferredOperations`
in `table.go` maps such a name to the reason it has no row, and
`TestFixtureOperationsAreRegisteredOrDeferred` requires every operation a
synced fixture names to be one or the other. A newly synced family cannot
slip in unnoticed — the suite fails until someone decides *row* or
*deferred, because …*. The list is also held to be a record of real gaps,
not a graveyard: a name that gains a row must leave it, which the same test
checks.

Stream families are the other shape of this. `condition-stream-item.json`,
`usage-stream-item.json`, and the `working-mode-*.json` pair evidence stream
scopes whose *subscribe* operations the synced set never names — no
top-level `operation`, no conformance case. They are neither registered nor
deferred, because nothing in `contracts/` claims an operation name for them
to be measured against; when the planning repo names them, the coverage test
starts demanding a decision.

## Which rows are full and which are data

Full descriptors — the dogfood milestone set (2026-08-23 handoff 20):
`session.launch`, `session.list`, `session.inspect`, `session.enroll`,
`session.detach`, `session.reattach`, `manifest.show`, `doctor.run`.
These are what Stage 0/1 wiring consumes.

Data rows — contract-attested v1 surface beyond the milestone, recorded
ahead of their implementation: `conversation.list`, `conversation.subscribe`,
`runtime_configuration.subscribe`, `prompt.deliver`, `command.inspect`,
`prompt.lease.acquire`, `terminal.read` (all attested by fixtures and/or
`projection-cases.json`). The `Milestone` field is the boundary marker.

**Implemented 2026-08-25:** `session.reconcile`, `session.archive`, and
`session.remove` are wired in the CLI (`internal/registry/table.go` rows
with CLI paths `session reconcile` / `session archive` / `session remove`).
`session.remove` was previously a data-only row attested by `manifest.json`
with no cobra command; archive and reconcile were added in the same matter.
None of the three carry MCP tool names (`local_admin` lifecycle writes).

## A view exists only where the consumer needs a shape the row is not

§8 lists six things that derive from the registry: the manifest, CLI
metadata, MCP tool schemas, presentation route metadata, audit requirements,
and the fixture inventory. `views.go` holds three functions, not six, and
that is the deliberate line: a view exists where the consumer needs a
*different shape* from the descriptor — `ManifestOperations` renames and
flattens fields into the `duo.manifest/v1` item and turns a nil permission
slice into `[]`; `MCPTools` deduplicates the shared subscription tool across
rows; `FixtureFiles` flattens and dedupes per-row slices.

CLI paths, routes, and audit categories need none of that: they are one
field on the descriptor, and a consumer walking `registry.All()` reads them
directly. A `CLIPaths()` that returns "the CLI field of every row" would be
an exported alias for a range loop — API surface with no invariant behind
it, and a second name for the caller to keep straight. When the CLI wiring
lands and turns out to want a shape (a Cobra tree keyed by parent path, say),
that shape becomes a view then, with the transformation as its reason to
exist.

## Authored operation names: `manifest.show` and `doctor.run`

The planning set names the *commands* (`duo manifest`, `duo doctor` are
"the accepted administration commands") but never assigns them dotted
semantic operation names, and the envelope's operation pattern requires at
least one dot. `manifest.show` and `doctor.run` are authored here, on the
pattern the attested names already follow (`session.inspect` for
`duo session show`). If a later planning sync names them differently, the
rename is a one-row edit plus the milestone test.

## Schema references: family + file + optional `$defs` key

A `SchemaRef` is the schema family string, the backing file's path in
`contracts.FS`, and optionally a key under that file's `$defs`. An empty
`Def` means "the family's envelope, constrained by the schema's own
conditional blocks" — which is the truth for most operations, because
`duo-external-v1.schema.json` expresses per-operation result shapes as
`allOf`/`if` conditions on the envelope, not as named definitions. Only
`session.launch` (`launch_resolution_report`) and the subscribe operations
(`stream_item`) have a named definition to point at; `manifest.show`'s
result is the `duo.manifest/v1` schema file itself. Every reference must
resolve — file readable, `Def` present in `$defs` — against the embedded
contracts, enforced by test.

**Flagged, not resolved:** the `manifest.json` fixture writes references
like `duo.external/v1#prompt-deliver-request` and
`duo.external/v1#command-result` — fragments that exist nowhere in the
synced `duo-external-v1` `$defs`. The fixture's reference style is
aspiration, not resolvable truth. This registry renders `family#def` only
for defs that actually exist, so its manifest output will not reproduce the
fixture's dangling fragments. The discrepancy belongs to the planning repo
(either author the named request/result defs or amend the fixture); it is
not paperable here.

No request definition exists anywhere in the synced set — the external
schema describes results, errors, and stream items only — so every request
reference is currently envelope-level. When named request defs land, rows
tighten without changing shape.

## MCP exclusions are encoded as absence, plus a projectability guard

`session.launch` is `local_admin` and carries no MCP tool name
(`duo-vnext-public-contract.md` §6.1: self-activation and
resource-amplification loop; the G-12 analogue). Timer mutation and
dead-letter requeue stay out of MCP per G-12 — those operations have no
rows yet (row policy above), so their exclusion is currently vacuous, but
the test bans their would-be tool names by name so a future row cannot add
one casually, and `notification.requeue`'s row, when it lands, must be
`local_admin` per the ledger. A second guard is structural: only
`deterministic` descriptors may carry an MCP tool name at all, tested.

## The shared subscription tool

`duo_subscription_open` is one stable MCP tool multiplexing stream
families, so `conversation.subscribe` and `runtime_configuration.subscribe`
both name it. The uniqueness test's one sanctioned sharing shape: a shared
tool name requires each sharer to carry a distinct, non-empty
`StreamFamily`. Everywhere else, tool names are one-to-one with
operations.

## Permission gaps in the planning set, flagged

`duo-vnext-external-surfaces.md` §4.8 requires the permission model to
separate "runtime configuration read" and "working mode read", but the
normative permission-name table (`duo-vnext-access-errors-audit.md` §1.1)
contains no such names. Similarly, no table entry covers reading command
state (`command.inspect`). Both rows use `session.metadata.read` as the
closest registered grant rather than inventing names the normative table
would then have to ratify. Flagged for the planning repo; the rows carry
source comments.

## Idempotency is a single enum; dry-run nuance stays prose

The contract says `session.launch` idempotency is "required for spawn, not
applicable when `dry_run` is true". The manifest shape (and this registry)
records the single enum `required` — the strictest applicable answer — and
the dry-run exemption lives in the descriptor's doc comment and the launch
contract, not in a second field. A conditional-idempotency field would be
speculative structure with exactly one consumer sentence.
