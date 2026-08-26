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

## 2026-08-24 — Stage 1v3 (duo.config/v3) exit gate: PASS

Evidence: `evidence/traces/config-v3/` (gate.md maps every roadmap §3a
gate bullet to a command transcript or test name). Binary `a1e1cf9`.
Live set: herdr 0.8.2, claude 2.1.241, pi 0.83.0, two disposable
servers, isolated XDG homes, no live user session touched.

v3 is the shipped schema from this gate on: the loader refuses
`duo.config/v2` with the `duo config migrate --to duo.config/v3`
pointer (`config.schema_v2_unsupported`), and `contracts/SOURCE` pins
the normative repo at `210bfd3` (contains the fixture seal `32e01fe`).
The dogfood checkpoint (duo-dogfood step 24) re-targets to v3 and
authors `model_family` by hand — migration reports `manual` and infers
nothing. Carried forward from the gate run: the no-ambient-variables
visibility caveat on wrong bindings (step 14 finding), and the
fingerprint-flag requirement on `duo workspace host rebind`.

## 2026-08-26 — Delegation-loop milestone (§3b) exit gate: PASS at named scope

Evidence: `evidence/traces/delegation-loop/` (gate.md maps every workplan
step-17 bullet to a command transcript or test name). Binary `3ebaf6a`.
Live set: herdr 0.8.2, claude 2.1.246, pi 0.84.3, one disposable server
(`ddl17`), isolated XDG homes, no live user session touched.

This is the §3b exit, not Stage 2 and not Stage 3. Launch reports
`target` / `target_source`; close-on-exit artifacts ship with the
harness dir; Claude post-launch `prompt send` delivered (pane showed
`pong`) with same-key replay and `command.idempotency_conflict` on a
changed request. Doctor sweep reaped an orphan harness dir and kept
live ones; scrub-gate was silent on the clean server. `make check`
green; `contracts/SOURCE` at terminal-multiplexers `3c9c2ba`.

Limits recorded at gate time, not folded into a Stage 2/3 claim:
launch writes no `agent.session` correlation (condition and
`conversation.list` stay unwired live; Claude used Herdr `agent.prompt`
rather than the messaging socket); Pi `agent.prompt` returned
`no_effect` while Herdr listed `launch_pending`; draft-hold is
test-only on Herdr; D1's nested Duo-launched orchestrator was not
stood up. tmux + Codex remain deferred.
