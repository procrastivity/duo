# `conformance` — decisions this step made

The design of record is `duo-vnext-go-architecture.md` §8 ("CLI and MCP
projections use the same generated request and result codecs where
practical. A conformance test decodes all three projections to an equal
domain value") and §10's acceptance check ("CLI, MCP, and presentation
fixtures decode to equal canonical values"). This file records what
building `internal/conformance` forced, and where it deliberately stops
short of what a later stage will replace.

## Families are derived from filenames, not hand-listed

`Families(contracts.FS)` (`family.go`) parses every `contracts/schemas/*
.schema.json` filename against `duo-(.+)-v(\d+)\.schema\.json` and groups
by the base. Five families fall out of the six synced schema files: the two
`duo-config-v*` files collapse into one `duo.config` family, alongside
`duo.external`, `duo.manifest`, `duo.projection-conformance`, and
`duo.projection-stamp`. Nothing in the package hard-codes the family list —
a newly synced schema file changes the result of `Families` and
`SchemaFiles` without a code edit, the same "derive, don't duplicate"
posture `internal/registry`'s decisions record already commits to.

A fixture's own family is read the same structural way:
`FamilyOf(fixture["schema"])` splits on `/`. `fixtures.go`'s `classify`
function uses that split to decide, per fixture, whether it is
`duo.external` (candidate for the three-projection round trip) or one of
the other four families (categorically skipped — see below).

## What "applicable" means, and the skip reasons

Every fixture under `contracts/fixtures/duo-external-v1/*.json` is
classified once (`fixtures.go`). Fourteen are applicable — decoded and
round-tripped through all three projection encoders — the rest carry an
explicit, non-empty skip reason surfaced by `t.Skipf` in
`TestFixtureRoundTrip`. Skips fall into four shapes:

1. **Wrong family.** `config.json` (`duo.config/v2`), `manifest.json`
   (`duo.manifest/v1`), `projection-stamp.json`
   (`duo.projection-stamp/v1`), and `projection-cases.json`
   (`duo.projection-conformance/v1`) are not public-operation envelopes at
   all — a local config document, a manifest.show CLI result body, a
   harness install stamp, and the projection-cases container itself
   (consumed by `TestProjectionCasesValueEquality` instead).
2. **Deferred operation.** `version-conflict.json` names
   `collaboration.value.replace`, which `internal/registry`'s
   `deferredOperations` carries with a reason (the collaboration
   permission split is undecided) rather than a row. The harness reads
   `registry.DeferredOperations()` and repeats that reason rather than
   inventing its own.
3. **Registered but CLI-only.** `session-launch.json`,
   `session-launch-exhausted.json` (`session.launch`, `local_admin`) have
   registry rows with `MCPTool == ""` and `Route == nil` by design
   (self-activation/resource-amplification guard, per
   `docs/registry/decisions.md`) — there is no MCP or presentation
   projection to compare against the CLI one, so a three-way round trip
   cannot be performed. `manifest.json` falls in this bucket too, folded
   into the family skip above since it never carries an `operation` field
   to test against the registry row.
4. **Unregistered stream family.** `condition-stream-item.json` (stream
   `session.condition`) and `usage-stream-item.json` (stream `usage`) have
   no descriptor anywhere in the table with a matching `StreamFamily` —
   `docs/registry/decisions.md` records that their subscribe operations are
   named nowhere in the synced set yet. `classifyStream` walks
   `registry.All()` looking for a `StreamFamily` match and reports this
   reason when none exists.

The other two stream-item fixtures for `session.runtime_configuration`
(`runtime-configuration-selected.json`, `runtime-configuration-effective
.json`) *and* the two `working-mode-*.json` fixtures all carry
`"stream": "session.runtime_configuration"`, which matches
`runtime_configuration.subscribe`'s row — all four are applicable, not
just the two whose filename says "runtime-configuration".

Fourteen applicable, nine skipped, twenty-three fixtures total (the
`README.md` fixture-directory file is not JSON and is filtered by
extension before classification, not skipped-with-reason).

## Canonical equality is `reflect.DeepEqual` over parsed JSON

`Canonical` (`canonical.go`) is `map[string]any` — exactly what
`encoding/json` produces by default. Every projection decoder in this
package returns that same shape (never a typed struct), so
`reflect.DeepEqual` is a sound equality check: both operands are always
"whatever `encoding/json` does with `any`," so there is no risk of a
struct's zero-value defaults or an omitted field papering over a real
divergence. This is also why fixtures parse straight into `Canonical`
rather than into `duo.external/v1`-shaped Go structs — the harness compares
*values*, not a hand-maintained schema mirror that could itself drift from
the synced contract.

## Stage 0 encoders: identity where no codec exists yet, real framing where transport shape differs

The step's boundary is explicit: build the minimal canonical-form
encoder/decoder inside `internal/conformance`, and do not wire it into
`internal/cli`. Concretely:

- `EncodeCLI`/`DecodeCLI` and `EncodeHTTPBody`/`DecodeHTTPBody` are the
  identity codec over `Canonical` (`json.Marshal`/`Unmarshal`, nothing
  else). There is no generated CLI result codec or presentation result
  codec yet — `internal/cli` registers only `duo version`
  (`docs/registry/decisions.md`) — so Stage 0 has nothing non-trivial to
  do here. The seam exists so a later stage's real codec drops in without
  changing a caller of `EncodeCLI`/`EncodeHTTPBody`.
- `EncodeMCP`/`DecodeMCP` (`mcp.go`) is **not** identity: it wraps the
  envelope in a minimal `CallToolResult` shape
  (`{"content":[{"type":"text","text":"<json>"}],"isError":…}`), the
  harness's own encoding choice (no MCP server exists in this repo yet to
  cite instead). This is deliberately exercised as a real wrap/unwrap
  round trip, not asserted as identity, because it is the one projection
  whose wire shape is not "print the envelope" even at Stage 0.
- `EncodeSSE`/`DecodeSSE` (`presentation.go`) frames a stream item
  (detected by the top-level `"stream"` key) as one Server-Sent Event
  (`event: message\ndata: <json>\n\n`), per §8's "HTTP, SSE, and terminal
  transports." A request/result envelope uses the plain-body codec instead;
  `TestFixtureRoundTrip` picks the encoding by that same key check.

Both non-identity choices are the harness's own decisions, not contract
citations, and are recorded here so a later stage that replaces them with
generated codecs knows what behavior it is taking over, not inventing.

## `projection-cases.json`: the VALUE half, deliberately separate from the registry's metadata half

`internal/registry/conformance_test.go`'s `TestProjectionCasesMatchRegistry`
already proves each case's CLI argument vector starts with the registered
verb path, its MCP tool name matches the row, and its presentation path
matches the route template — the *metadata* half. This package's
`TestProjectionCasesValueEquality` (`projection_cases_test.go`) does not
repeat that; it decodes the CLI arguments, MCP tool arguments, and
presentation request (path captures + query + body) each case carries into
`Canonical`, and asserts every one equals `semantic.input` — the *value*
half the step assigns to this harness.

Decoding CLI arguments back to a domain value needs to know, per case,
which positional slot fills which key and which semantic key a flag name
aliases (`--runtime-instance` → `runtime_instance_id`, not the
kebab-to-snake default `runtime_instance`). `internal/cli` has no flag
parser to reuse for this yet (only `duo version` is wired), so `cli.go`
defines `CaseArgSpec`, a small explicit table keyed by each case's own
stable `name`. This is request-shape data, scoped to interpreting nine
fixture cases — not a general CLI grammar, and not an integration-name
branch (it never inspects a session-host or agent-runtime name).

Two dispatch keys are dropped when decoding MCP tool arguments
(`DecodeMCPRequestArgs`, `mcp.go`): `stream` (which stream family
`duo_subscription_open` should open) and `operation` (which sub-action
`duo_lease_manage` should perform). Both select among a *shared tool's*
behaviors — `docs/registry/decisions.md`'s "shared subscription tool"
section is the citation — and have no counterpart in `semantic.input`;
comparing them against the semantic value would be comparing apples to a
key that does not exist on the other side.

The presentation decoder (`DecodePresentationRequest`) merges route-template
path captures, parsed query parameters, and the JSON body into one value.
The `Idempotency-Key` header is deliberately not merged in: it duplicates
`prompt_deliver`'s body `idempotency_key`, a transport-level replay guard,
not a second domain field the canonical value should carry twice.

## The integration-name-neutrality gate: static tripwire plus dynamic proof, fake pair included

The gate (`neutrality_test.go`) has two independent parts, because either
alone is dodgeable in a different way:

1. **Static.** `TestEncodersContainNoIntegrationNameLiteral` scans every
   non-test `.go` file in this package for a fixed token list (`tmux`,
   `codex`, `herdr`, `solo`, `opencode`, `ownpty`, `claude_code`, and this
   package's own fake pair `fake_host`/`fake_runtime`). `go test` always
   runs with the package directory as the working directory, so scanning
   `.` is exactly `internal/conformance`'s own non-test source. This is
   the same tripwire shape as `internal/registry`'s
   `TestNoDuplicateOperationTableOutsideRegistry` — it catches the direct,
   obvious way this rule breaks (someone writes `if sessionHost ==
   "tmux"`), not every possible evasion.
2. **Dynamic.** `TestIntegrationNameNeutralityGate` builds the same
   envelope shape three times, varying only the `session_host` /
   `agent_runtime` (or, for the SSE sub-test, the stream item's identity
   fields) across three pairs: two realistic pairs, and the fake host +
   fake runtime pair the step names explicitly. It runs all three through
   each encoder, round-trips each back to its own exact input (functional
   correctness per pair, fake pair included), then masks each pair's own
   name values out of the encoded bytes and requires the masked output to
   be byte-identical across all three pairs. A branch keyed on the name
   would survive the masking as a structural difference — an added key, a
   different wrapper shape, reordered content — so this is a real proof
   that the encoder treats the name as opaque data, not a discriminator.

**Why the fake pair is a literal string constant here, not an import.** The
step boundary reserves `internal/host/fake` and `internal/runtime/fake` for
a concurrently-working agent and asks this package to take the fake pair
"from fixture data/constants of your own rather than importing their
packages." `integrationNamePairs` in `neutrality_test.go` does exactly
that: `fake_host`/`fake_runtime` are plain strings, indistinguishable to
every encoder from any other session-host/agent-runtime name. No import of
either fake package exists in this tree change, and none is needed — the
gate's claim is about the *encoders'* behavior, which only ever sees an
opaque string, never a package identity.

**Negative-test result (performed manually during this step, not left in
the tree):** a temporary branch of the form `if host == "fake_host" {
out["quirk"] = true }` inserted into `EncodeMCP` made
`TestIntegrationNameNeutralityGate/MCP` fail immediately (the masked
`fake_host`/`fake_runtime` output gained an extra key the other two pairs'
output did not have), confirming the dynamic check actually detects a
branch rather than passing vacuously. The edit was reverted before
finishing the step; `git diff` over this change carries no trace of it.

## Boundary note: `internal/registry` is read-only, everything else in this package is owned

This package imports `github.com/procrastivity/duo/internal/registry` (for
`Lookup`, `All`, `DeferredOperations`) and
`github.com/procrastivity/duo/contracts` (for the embedded fixture/schema
tree) — both explicitly allowed as read-only consumption by the step's
boundaries. It imports nothing from `internal/cli`, `internal/config`,
`internal/manifest`, `internal/host`, or `internal/runtime`, and defines no
CLI wiring — the encoders here are consumed only by this package's own
tests, per the step's "do not wire it into the CLI" instruction.
