# `config` — decisions this step made

The design of record is `duo-vnext-installation-contract.md` §1 (the
configuration boundary, strict YAML, the v1/vN schema succession, and
launch-resolution rules) and `contracts/schemas/duo-config-v2.schema.json`.
This file records what building the strict `duo.config/v2` resolver
(Step 10) *forced* — the calls those documents left open, and what Step 10
deliberately leaves for a later step.

## Scope: the resolver validates document shape, not launch feasibility

`duo-vnext-installation-contract.md` §1.1 describes a great deal beyond
document shape: layered precedence (shipped defaults → system policy → user
config → `--config` flags → command flags), `duo config migrate`, and full
launch resolution (require/avoid narrowing, `distinct_model_line`,
`config.composition_unresolved` for an unresolved composition or preset
reference). None of that is Step 10's job. The Step 10 spec scopes this
step to exactly one question: does a `duo.config/v2` document have the
shape the schema demands, with every composition's `launch_variant` and
`model_line` present and never inferred? `internal/config.ParseV2` answers
only that question, against one in-memory document. Layering, migration,
and launch resolution are later steps' work (the "Launch resolver" is its
own component in `duo-vnext-go-architecture.md` §3, explicitly separate
from configuration validation).

Concretely, `ParseV2` does **not**:

- Merge multiple configuration layers, or read `/etc/duo/config.yaml` /
  `$XDG_CONFIG_HOME/duo/config.yaml` itself (`LoadV2` reads one file from
  one path; a caller supplies the path).
- Implement `duo config migrate`.
- Resolve a preset's composition references against the `compositions` map
  (that cross-reference check — a missing or ambiguous link — is exactly
  what `config.composition_unresolved` names in the registered stable set,
  and it belongs to the launch resolver, not this step).
- Enforce every structural constraint the schema expresses: preset
  `minProperties`/`minItems` on leaves and candidates, `uniqueItems` on
  relation leaves and preset-leaf candidates, or duplicate-key detection in
  the source document (`gopkg.in/yaml.v3`'s `KnownFields(true)` mode
  rejects unknown keys but not repeated ones — catching a duplicate key
  would need a lower-level `yaml.Node` walk, which is more machinery than
  this step's acceptance asks for). A document with a genuinely duplicate
  key today resolves using YAML's own last-key-wins behavior rather than
  failing.

These gaps are real, not resolved fictions — a future step (the launch
resolver, or a `duo config validate` CLI verb) either closes them or
inherits `ParseV2` as its first pass and adds the rest.

## Why a hand-rolled decode, not a JSON Schema library

`internal/registry`'s conformance tests already set the house rule: "no new
JSON Schema library" (`docs/registry/decisions.md`). `ParseV2` follows the
same call: it decodes into a strict-enough Go shape by hand
(`gopkg.in/yaml.v3`, already a dependency) rather than adding a validator
dependency to check a dozen-odd properties. Two mechanisms carry the
schema's actual strictness requirements:

- **Root strictness** (`additionalProperties: false` on the document root,
  and on `preset`/`preset_leaf`/`preset_candidate`/`preset_relation`):
  `yaml.Decoder.KnownFields(true)` against a Go struct whose field set
  mirrors the schema property list exactly. An unrecognized key at any of
  those levels fails the decode with `config.decode_failed`.
- **Composition looseness** (`additionalProperties: true` on `composition`):
  compositions decode into `map[string]any` instead of a struct — a strict
  struct here would *wrongly* reject a composition carrying an extra field
  the schema explicitly permits. `resolveCompositions` then checks
  `launch_variant`/`model_line` by hand, because that is the one place the
  schema's `required` list and this step's "never infer" requirement both
  land on the same two fields.

## JSON fixture, YAML resolver: no second code path

`contracts/fixtures/duo-external-v1/config.json` is JSON, but
`duo-vnext-installation-contract.md` §1 says "Duo reads strict YAML 1.2."
JSON is a syntactic subset of YAML 1.2, and `gopkg.in/yaml.v3` parses it
directly — `TestParseV2_Fixture` decodes the embedded JSON fixture through
the exact same `ParseV2` a `.yaml` file would go through. No separate JSON
path exists or is needed.

## Error codes: plain `config.*`, not the registry's stable set

`internal/registry.StableErrorCodes()` is the set operation-level failures
draw from (`invalid.request`, `config.composition_unresolved`, and so on) —
codes a *running operation* can raise, attested against
`duo-vnext-access-errors-audit.md` §4.1. A document-shape failure this
resolver raises happens before any operation runs; reusing, say,
`invalid.request` for "composition missing launch_variant" would blur two
different failure sources under one code, and inventing new entries in
`internal/registry`'s stable set is out of bounds for this step
(`internal/registry` is not owned here, and the row policy in
`docs/registry/decisions.md` requires contract attestation this step
cannot provide).

So `ParseV2` raises its own small, stable, namespaced vocabulary instead —
six named constants (`ErrCodeSchemaMissing`, `ErrCodeSchemaV1Unsupported`,
`ErrCodeSchemaUnrecognized`, `ErrCodeDecodeFailed`,
`ErrCodeCompositionLaunchVariantRequired`,
`ErrCodeCompositionModelLineRequired`), every one prefixed `config.` to
mark it as a document-shape failure. None carries the `refusal.` or
`internal.` prefix `internal/exitcode.FromError` special-cases, so they all
map to the plain user-failure exit (1) — correct, since a malformed
configuration document is the caller's mistake, not a guard tripping or a
store failure.

## The "never silently reinterpreted" case gets its own code

The spec calls out one scenario by name: "a v1 config is never silently
reinterpreted as v2." `ParseV2` peeks the `schema` field before attempting
the full v2 decode (`peekSchema`), and branches three ways: missing (no
`schema` key at all), exactly `duo.config/v1` (a real, nameable
possibility — the document may be a perfectly valid v1 document), or
anything else unrecognized. The v1 case gets its own code
(`config.schema_v1_unsupported`) distinct from the generic
`config.schema_unrecognized`, because it is not a garbage value — it is a
schema this binary understands the *name* of and refuses on purpose, which
deserves a more specific refusal message (pointing at
`duo config migrate --to duo.config/v2`) than "unrecognized."

Peeking the schema field with a separate, non-strict decode (rather than
running the full `KnownFields(true)` decode first and inspecting whatever
error comes back) matters: an unrelated unknown field in a v1 document
would otherwise surface as a generic decode error and mask the more useful
"this is v1" diagnosis.

## `internal/manifest`'s wire shape (Step 10 also extended this)

Step 10's boundary also covers `internal/manifest`, and `duo manifest`'s
whole purpose is to emit the wire shape
`contracts/schemas/duo-manifest-v1.schema.json` describes — a close cousin
of this file's "strict document shape" theme, so its decisions land here
rather than in a fourth decisions file the Step 10 boundary does not name.

**`Product` is new; `Tool` stays.** The contract requires a `product` object
with exactly `name`/`version`. The existing `Tool` struct (name, version,
commit, date) predates this step and was already reused by `duo version`;
rather than repurpose it to match the contract's narrower shape (and lose
commit/date from the manifest), `Manifest` now carries both — `Product` for
the contract's required key, `Tool` for the richer chassis-internal extra.
The contract's root has `additionalProperties: true`, so the coexistence
costs nothing.

**`manifest_digest` is computed over the manifest itself.** No document
specifies what the digest commits to. `manifestDigest` (`digest.go`) hashes
the manifest's own canonical JSON encoding with the digest field held
empty, which is the only self-consistent choice available (a digest that
included itself could never be verified). Determinism rests on every slice
`Build` assembles already being in a fixed order before this runs
(`registry.ManifestOperations()`: table order; `walkAssets()`: sorted by
path; Go's `encoding/json` renders struct fields in declared order, not map
order) — `TestManifestDigestIsDeterministic` pins exactly this.

**`public_schemas`, `configuration_schemas`, `projection_formats` are
authored data, not a registry view.** Unlike `Operations`, none of the
three families involved (`duo.config/v1`, `duo.config/v2`,
`duo.projection-stamp/v1`) is referenced by any `registry.Descriptor` — the
registry table's `SchemaRef` values only ever name `duo.external/v1` and
`duo.manifest/v1`. There is nothing in `internal/registry` to derive these
three lists from without inventing a second, unrelated table inside it, so
they are authored directly in `internal/manifest/schemas.go` and
cross-checked against the embedded contract set by
`TestSchemaFamilyListsResolve` (same shape as `registry.SchemaRef.Resolve`,
applied to a plain string→file map instead of a descriptor field) — a typo
in either list fails the suite rather than shipping silently.

`configuration_schemas` lists both `duo.config/v1` and `duo.config/v2`. The
installation contract states v1 documents "remain valid under that schema"
even though this binary's only resolver (`internal/config.ParseV2`) never
loads one — declaring the family here is a schema-recognition claim (the
file is embedded and structurally understood), not a claim that every v1
operation is implemented. If that reads as overclaiming once a real
`duo config validate`/`migrate` surface lands, narrowing the list to just
`duo.config/v2` is a one-line change with no schema-shape consequence.

**Empty arrays, never `null`.** The four required array-typed root fields a
running binary might trivially have nothing for (`adapters`,
`harness_targets`, and `assets` when the shipped tree is empty) are always
built as `[]T{}`, never a nil slice left to serialize as `null` — the
schema types them `array`, and `TestManifestMatchesContractSchema` checks
this by round-tripping the encoded manifest and asserting each is a JSON
array.

**`adapters` and `harness_targets` stay structurally empty in this step.**
`internal/host` and `internal/runtime` own real adapter registration and
are out of this step's boundary; `Adapter` and `HarnessTarget` exist as
shapes only, so that wiring real registration later is additive to the
manifest's JSON output rather than a breaking rename.

## One named default, deliberately not extended to compositions

`duo-vnext-installation-contract.md` §1.1 states `selection` defaults to
`ordered` when a preset omits it. `resolvePresets` applies exactly that —
a single, explicitly named contractual default. This is not in tension with
"no inference of missing fields": that rule is scoped to `launch_variant`
and `model_line` on a composition, which the same document is explicit are
never defaulted. The two rules read the same source paragraph correctly
applied to two different fields.

## A present-but-wrong-typed required field reads as absent

`stringField` treats a `launch_variant` (or `model_line`) that decodes to a
non-string value (a number, a nested object, `null`) the same as a missing
key — both raise the same required-field code. The schema's own
`type: string, minLength: 1` constraint would, under a real JSON Schema
validator, distinguish "wrong type" from "absent," but collapsing the two
here is a deliberate simplification: the caller-facing outcome is identical
either way (no usable value), and inventing a seventh error code for "right
key, wrong type" was not worth the vocabulary given nothing in this step's
acceptance exercises that distinction.
