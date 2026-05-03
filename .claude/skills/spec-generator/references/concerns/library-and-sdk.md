# Library / SDK Concerns

## What this concern means

Features intended for other developers to consume — published libraries, SDKs, public API surfaces, code that ships to a package registry. The "user" is the integrator, not an end user. Concerns: developer experience (DX), API contract stability, semantic versioning discipline, deprecation policy.

## When this concern is in scope

- **Pre-enabled by profile**: `library-or-sdk`.
- **Optional**: `public-saas` if the spec exposes a public API surface (REST, GraphQL, gRPC) that external developers integrate with.
- **JIT triggers**: user mentions "deprecate," "breaking change," "API contract," "versioning," "semver," "consumer," "integrator," "publishes," "package."
- **N/A for**: `solo-local-cli` (no consumers), `internal-tool` (internal APIs change at deploy time without versioning ceremony).

## What to investigate during conversation

- **What's the public surface?** Every type, function, constant, error code, event name, configuration knob a consumer touches is part of the contract.
- **What's the semver discipline?** Which changes bump major (breaking), minor (additive), patch (bugfix)? Spell out the rules — they vary across projects.
- **What's the deprecation policy?** How long do deprecated APIs stay around before removal? What's the deprecation signal (compile-time warning, runtime warning, docs only)?
- **How do consumers discover the API?** README, generated docs, type hints, examples directory. "It's in the source" is not discoverability.
- **What's the contract test surface?** Tests that pin the public API shape (signatures, error types, return shapes). These are the *consumer-facing* tests, distinct from internal correctness tests.
- **What about ergonomics?** Common usage patterns should be terse; rare usage patterns can require more setup. If the common case requires 5 lines of boilerplate, the API is wrong.

## What rigor applies

- **Every public symbol is documented.** Every exported function, type, and constant has a doc comment with intent, params, returns, and at least one example.
- **The semver decision tree is explicit.** "Adding a new method = minor. Adding a parameter to an existing method (with a default) = minor. Renaming a method = major. Changing the type of a returned field = major (even if 'compatible')."
- **Deprecation has a published timeline.** "Deprecated in 0.5.0; removed in 1.0.0 (no earlier than 6 months)."
- **Contract tests pin the public surface.** A test that imports the library and asserts the function signature is what a consumer would actually use.
- **At least one usage example per common pattern**, in the docs and ideally in an `examples/` directory.

## Decomposition manifest entry

```markdown
### Library / SDK contract
- Public surface added: `<list of types / functions / events>`.
- Semver classification: `<list each new symbol with major/minor/patch impact>`.
- Deprecations: `<old API> -> <new API>`. Deprecation date: `<version>`. Removal date: `<version, no earlier than X months>`.
- Contract tests added: `<test file>`.
- Documentation updates: `<README / docs / examples directory>`.
```

## Examples of strong vs. weak coverage

**Strong**: "Adds `Hypomnema::search` returning `Result<SearchResult, SearchError>`, with `SearchResult { results: Vec<Hit>, truncated: bool }`. New error variants: `SearchError::IndexNotReady`, `SearchError::QueryTooBroad`. Semver: minor (additive). Deprecation: existing `Hypomnema::query` is deprecated in this release (0.5.0); compile-time warning via `#[deprecated]` attribute; will be removed no earlier than 1.0.0 (6+ months). Contract tests in `tests/api_contract.rs` pin the signature of `search` and the variant set of `SearchError`. Three usage examples added to `examples/` (basic, with-filters, error-handling)."

**Weak**: "Add a new search function. We'll deprecate the old one eventually. Update the docs."

The strong version pins the signature, classifies the semver impact, sets a removal timeline, and ensures contract tests will catch silent breakage. The weak version leaves every consumer-facing decision implicit.
