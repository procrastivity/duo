# Roadmap decisions

Stage-gate outcomes and the roadmap-level calls made while executing the
staged implementation plan (`duo-vnext-implementation-roadmap.md` in the
planning repository, as amended by handoff 20's dogfood narrowing).

## 2026-08-23 — Stage 0 exit gate: PASS

Evidence: `evidence/traces/stage0/` (gate.md maps every roadmap §2 gate
bullet to a command transcript or test name). Binary `348a794`.

Notes recorded at gate time:

- The gate ran against the handoff-20 narrowed scope: config validation is
  the strict `duo.config/v2` resolver, and `duo.config/v1` documents refuse
  with `config.schema_v1_unsupported` instead of validating. The roadmap's
  §2 work list predates that amendment where it names v1.
- Two gate bullets needed wiring the parallel Step 10/11/12 builds could
  not do alone (their package boundaries kept them apart): `duo doctor`
  registers the fake adapter pair through the CLI composition root, and
  `duo manifest` reports the contract digests from the embedded
  `contracts/SOURCE` as a chassis extra, because the `duo.manifest/v1`
  contract's `public_schemas` carries family names only. Both are recorded
  in their commits; neither changes a contract shape.
- Deliberately not in Stage 0, per the step decisions records:
  content-addressed large content (docs/store), launch resolution and
  layered config precedence (docs/config), the five Stage-2+ runtime
  interfaces and two host provider interfaces (docs/adapters), and rows
  for unattested operation families (docs/registry).
