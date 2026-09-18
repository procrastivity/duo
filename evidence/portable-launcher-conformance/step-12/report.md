# Step 12 — Complete Codex launcher suite prerequisite gate

- **Observed:** 2026-09-18T08:40:51Z–08:43:04Z
- **Checkout:** `$CHECKOUT`, branch `feat/portable-launcher-conformance`, commit `4c47e63c400184a091ecc6f09aab31fde6fe9937`
- **Outcome:** **BLOCKED before fixture setup, Codex session/thread, model turn, or Duo launch**
- **Step 12 seal condition:** **not met**
- **Matter seal condition:** **not met**

The complete launcher-neutral Codex suite cannot run honestly under the repository's exact accepted pins in this checkout. The accepted Codex native executable is exact, but no eligible runnable Pi 0.83.0 installation was found, the local Herdr 0.8.2 executables fail the accepted API-schema identity, and suite revision 1 still declares its admitted-then-blocked producer unavailable and rejects an overall or blocked-case pass. The newer installed Pi/Herdr artifacts and the same-version/wrong-schema Herdr artifacts were not relabeled as accepted.

## Exact required and observed identities

| Component | Required | Observed | Verdict |
|---|---|---|---|
| Outer Codex | CLI `0.154.0`; native package `0.154.0-linux-x64`; native SHA-256 `3188814c35471432d4123203e0eb38e5bddc60226e3d7ddf0e59e649ea140022` | Exact native ELF, 262,858,016 bytes, exact digest; wrapper is separately SHA-256 `61b0194f3bb6534439c8d26a3ed57d0805f84b884588b761795323eeb92fcf70` | eligible |
| Runtime Pi | `0.83.0`; `pi-session-jsonl/v3`; adapter `pi` build `stage1`; record `pi-0.83.0-2026-08-23`; inject SHA-256 `2708a3435821fb2e72302ee56d8a07fe62c265214ae8fd342e385c03ef245a23` | Installed executable is `0.84.4`, SHA-256 `5406c369954516fb56879d685e082ff9095cd6e06e41af406f394942377fd4bf` | **ineligible** |
| Local Pi cache | Runnable exact artifact with a verified dependency tree | Exact `0.83.0` package archive is cached (4,992,066 bytes; SHA-256 `7097fe4b38762dda7ec78001e7b90430c849fbaf717325bfe8109744e32255e6`), but it is not a provisioned dependency-complete executable | **insufficient** |
| Host Herdr | `0.8.2`; `herdr-socket-api/20`; API-schema SHA-256 `c48f1f54ee0150ca27e11fd44455fe94aeadb20fdf4e4a62393ed822a4e5b150`; adapter `herdr` build `stage1`; record `notes/19-herdr-probes.md@2026-08-23` | PATH is `0.9.0`/schema `226d4ecb…`; two local `0.8.2` executables both export schema `24e62c2d8be455ba7872c0e66bfbbf77e8403c29c3f5b1ff10c8ff3c112f4e63` | **ineligible** |
| Skill | Current `duo.skill/duo-delegation-loop/v1` and exact current digest | SHA-256 `6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182`, 6,284 bytes; manifest agrees | eligible |
| Suite | `portable-launcher-delegation`, revision 1 | Scenario SHA-256 `01e8e6bd80ceb441be6dc128aca023a1bede31b655e74b12ee3da0c2fc6cc204`; oracle `a305744fc34af96b5bbd916b9de3aa1bc94f59ce4ea9316b62bb0a8dd5dd22c5`; task `5949809812a3ada98c95e77e9942af848e3080e4984df43a2d96fe5a24d5eab2`; source/fixture tests passed | exact, but incapable of an overall pass while the blocked producer is unavailable |

The accepted Codex identity is the native ELF, not the npm JavaScript entrypoint. Both were independently resolved and hashed. The native package metadata reports `0.154.0-linux-x64`, the native executable itself reports `codex-cli 0.154.0`, and only its `3188814c…0022` digest satisfies the suite pin.

The Herdr candidates were rejected on the complete accepted identity rather than filename or version text. Their executable SHA-256 values are `7e1a54d98765f91eca532752369d7c01f91b971883599b393a1f014d332bb85e` (debug) and `c1cb6b1338d0f9e70cd8aa35828cec0635d64865aa90535d4350168750cf4ccf` (release dependency artifact). Both independently exported schema `24e62c2d…f4e63`, so neither can stand in for `c48f1f54…b150`. No Herdr server was started to probe protocol after that decisive mismatch.

## Exact checkout-built Duo identity

A prerequisite-only Duo binary was built inside this evidence directory, queried only with `version` and `manifest`, hashed, and removed. It did not prepare a fixture or invoke a session.

- version: `4c47e63-dirty`
- full commit: `4c47e63c400184a091ecc6f09aab31fde6fe9937`
- build date: `2026-09-18T08:42:27Z`
- executable: 14,369,268 bytes, SHA-256 `c89484c7ef0bbd1c7c1b282961e8ca7686ac7064a1ea052cf99c834753affa82`
- manifest: `duo.manifest/v1`, semantic digest `sha256:2a3f73835d2d2a03bed9af33d9e63880fbe64430692181dd26ba771a318413dd`
- portable target: `portable_launchers`, status `unverified`, carrying the exact skill and Codex pins above

The artifact was deliberately not retained. Any resumed live run must rebuild and retain a new exact per-run identity from the then-current checkout.

## Complete-pass blocker independent of local artifacts

Revision 1 declares `supported_admitted_then_blocked_producer` as `unavailable`. Pi 0.83.0 has no permission system or blocked-family producer, and its condition adapter never emits blocked. The suite validator forbids both an overall pass and any blocked-case stage marked pass. Consequently, provisioning both exact binaries would still not make the current complete suite pass. The honest prerequisite is a supported, version-pinned production condition source that deterministically produces `blocked` after prompt admission, with a non-terminal controller and exact runtime-instance evidence. Fake events, PTY input, model persuasion, a different runtime for one case, or launcher-driver patches remain ineligible.

## Stop boundary and assumptions

No fixture root, fixture workspace, authority store, Herdr server, Codex session/thread, model turn, Duo session launch, billed action, credential inspection, network download, production/shared runtime access, or WIP lifecycle transition occurred. Only local installation/cache reads, version/hash/schema inspection, focused offline tests, and the prerequisite-only Duo build occurred. No external or mutable product/runtime action occurred.

Assumptions:

1. A cached npm package archive is not a runnable Pi artifact; its runtime dependencies were neither provisioned nor verified, and the installed command remains 0.84.4.
2. A Herdr binary reporting `0.8.2` is insufficient when its exported schema digest differs from the accepted pin. Protocol was intentionally not inferred or live-probed after that decisive failure.
3. Credentials and timeout induction were left uninspected because exact identity and canonical-suite prerequisites already forced a pre-setup stop.
4. The `-dirty` suffix honestly records the user-declared, pre-existing out-of-scope checkout residue. That residue was not inspected, staged, edited, removed, or incorporated as evidence.
5. If every prerequisite later becomes available, the run must still stop before a model turn or Codex session/thread until the parent reconciles the user's no-thread constraint.

## Exact resume condition

Resume only after all of the following are true:

1. A runnable, dependency-complete Pi 0.83.0 artifact is locally provisioned and its provenance and executable SHA-256 are captured without widening the pin.
2. A Herdr artifact simultaneously proves version 0.8.2, protocol 20, and API-schema SHA-256 `c48f1f54…b150`; the current `24e62c2d…` candidates remain rejected.
3. Owning runtime/host capability work supplies supported, version-pinned admitted-then-blocked evidence and the canonical suite no longer marks that prerequisite unavailable.
4. Duo is rebuilt and retained from the then-current exact checkout; Codex native, skill, scenario, oracle, and fixture identities are independently reverified.
5. Isolated `openai-codex` credentials are confirmed without recording secret material, and the timeout socket probe succeeds in the disposable fixture.
6. Before any model turn or Codex session/thread, the parent explicitly reconciles the user's no-thread constraint with Step 12's model-turn requirement.

Until then, neither Step 12 nor the Matter seal condition is met.

## Verification

- `go test ./internal/conformance/portablelauncher -run 'TestCanonicalScenario|TestCollectorMandatoryBlockedPrerequisitePreventsPass' -count=1` — passed.
- `evidence/portable-launcher-conformance/step-12/validate.sh` — asserts the blocked result, exact accepted Codex native identity, rejected runtime/host substitutes, unavailable blocked producer, and no-action boundary.
- `sh -n evidence/portable-launcher-conformance/step-12/validate.sh` — required final check.
- `git diff --check -- evidence/portable-launcher-conformance/step-12` — required final check.

Cross-repo impact: **no cross-repo impact**. This evidence independently confirms on 2026-09-18 the existing `duo-lab` prerequisite boundary at Codex `0.154.0` native SHA-256 `3188814c…0022`, Pi `0.83.0`, and Herdr `0.8.2`/protocol `20`/schema `c48f1f54…b150`; it neither unblocks work there nor invalidates an accepted assumption. No handoff is proposed.
