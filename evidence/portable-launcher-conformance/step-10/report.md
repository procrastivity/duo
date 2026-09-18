# Step 10 — Complete OpenCode launcher suite prerequisite gate

- **Observed:** 2026-09-18T08:19:25Z
- **Checkout:** `$CHECKOUT`, branch `feat/portable-launcher-conformance`, commit `99d48b8d2c5fd01db970566bf004185448b9f311`
- **Outcome:** **BLOCKED before fixture setup, launcher session, model turn, or Duo launch**
- **Step 10 seal condition:** **not met**
- **Matter seal condition:** **not met**

The complete launcher-neutral OpenCode suite cannot run honestly under the repository's exact accepted pins in this checkout. OpenCode itself is exact, but no eligible runnable Pi 0.83.0 installation was found, the local Herdr 0.8.2 executables fail the accepted API-schema identity, and the canonical suite still declares its admitted-then-blocked producer unavailable and rejects any blocked-case pass. A newer installed runtime or a same-version/different-schema host was not relabeled as accepted.

## Exact required and observed identities

| Component | Required | Observed | Verdict |
|---|---|---|---|
| Outer OpenCode | `1.18.31`, executable SHA-256 `f9dab32248695e9ebd56b16a1921798fd85112cf5a69c7dfd0cabc1e17be4a11` | Exact version and digest; 185,030,784 bytes | eligible |
| Runtime Pi | `0.83.0`; `pi-session-jsonl/v3`; adapter `pi` build `stage1`; record `pi-0.83.0-2026-08-23`; inject SHA-256 `2708a3435821fb2e72302ee56d8a07fe62c265214ae8fd342e385c03ef245a23` | Installed executable is `0.84.4`, SHA-256 `5406c369954516fb56879d685e082ff9095cd6e06e41af406f394942377fd4bf` | **ineligible** |
| Local Pi cache | runnable exact artifact and dependency tree | Exact `0.83.0` package archive is cached (4,992,066 bytes; SHA-256 `7097fe4b38762dda7ec78001e7b90430c849fbaf717325bfe8109744e32255e6`), but it is not an installed runnable dependency-complete executable | **insufficient** |
| Host Herdr | `0.8.2`; `herdr-socket-api/20`; API-schema SHA-256 `c48f1f54ee0150ca27e11fd44455fe94aeadb20fdf4e4a62393ed822a4e5b150`; adapter `herdr` build `stage1`; record `notes/19-herdr-probes.md@2026-08-23` | PATH is `0.9.0`/schema `226d4ecb…`; two local `0.8.2` executables both export schema `24e62c2d8be455ba7872c0e66bfbbf77e8403c29c3f5b1ff10c8ff3c112f4e63` | **ineligible** |
| Skill | `duo.skill/duo-delegation-loop/v1` plus exact current digest | `sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182`, 6,284 bytes; manifest agrees | eligible |
| Suite | `portable-launcher-delegation`, revision 1 | scenario SHA-256 `01e8e6bd80ceb441be6dc128aca023a1bede31b655e74b12ee3da0c2fc6cc204`; oracle `a305744f…`; task `59498098…`; fixture/source tests passed | exact, but cannot pass while blocked producer is unavailable |

The local Herdr candidates were rejected on the accepted three-part identity, not on filename or version text. Their executable SHA-256 values are `7e1a54d98765f91eca532752369d7c01f91b971883599b393a1f014d332bb85e` (debug) and `c1cb6b1338d0f9e70cd8aa35828cec0635d64865aa90535d4350168750cf4ccf` (release dependency artifact). Both independently exported schema `24e62c2d…`, so neither can stand in for `c48f1f54…`. No Herdr server was started merely to probe protocol after this mismatch was established.

## Checkout-built Duo identity

A prerequisite-only build was made inside this evidence directory, queried with the no-store `version` and `manifest` commands, hashed, and removed. It did not prepare a fixture or invoke a session.

- version: `99d48b8-dirty`
- full commit: `99d48b8d2c5fd01db970566bf004185448b9f311`
- build date: `2026-09-18T08:18:59Z`
- executable: 14,369,268 bytes, SHA-256 `2cd183e9f99855a13c8c66864d1926947d922f9b39f1e889f66c5d3c4cd48ca6`
- manifest: `duo.manifest/v1`, semantic digest `sha256:aeafb5502724a9d68d33a1ea43ee67f65f3f76477a4e115a9babd35ca817a003`
- portable target: `portable_launchers`, status `unverified`, with the exact skill identity above

The temporary executable was deliberately not retained. A resumed live run must build and retain a new exact per-run identity from the then-current checkout rather than reuse this observation as an artifact.

## Canonical suite blocker independent of local binaries

Revision 1 declares `supported_admitted_then_blocked_producer` as `unavailable`. The offline validator rejects any blocked-case stage marked pass, and the runner has no supported blocked control. Consequently, even provisioning both exact binaries would not permit every named scenario to pass today. The honest prerequisite is the contract's supported, version-pinned production condition source that deterministically produces `blocked` after prompt admission, with a non-terminal controller and exact runtime-instance evidence. A fake event, different runtime for one case, PTY input, model persuasion, or launcher-driver patch would be ineligible.

## Stop boundary and assumptions

No fixture root, fixture workspace, authority store, Herdr server, OpenCode session/thread, model turn, Duo session launch, billed action, or credential inspection occurred. No production/account/shared runtime state was accessed or mutated, no binary was downloaded, and no fixture authority/workspace was created or changed. Only the expressly allowed local installation/cache reads, local version/hash/schema inspection, focused offline Go tests, and the prerequisite-only Duo build were performed.

Assumptions:

1. A cached npm package archive is not treated as a runnable Pi artifact. Its exact runtime dependencies were not provisioned or verified, and the installed command remains 0.84.4.
2. A Herdr binary saying `0.8.2` is insufficient when its exported schema digest differs from the accepted pin. Protocol was intentionally not inferred or live-probed after that decisive mismatch.
3. Credentials were left uninspected because earlier exact-identity prerequisites already forced a pre-setup stop.
4. The Duo `-dirty` suffix honestly reflects known pre-existing out-of-scope residue. Its contents were not inspected or incorporated; full commit, date, size, digest, and manifest identify the prerequisite build.

## Exact resume condition

Resume only after all of the following are true:

1. A runnable, dependency-complete Pi 0.83.0 artifact is locally provisioned and its provenance and executable SHA-256 are captured without widening the version.
2. A Herdr artifact simultaneously proves version 0.8.2, protocol 20, and API-schema SHA-256 `c48f1f54…b150`; the current `24e62c2d…` candidates remain rejected.
3. Owning runtime/host capability work supplies supported, version-pinned admitted-then-blocked evidence and the canonical suite no longer marks that prerequisite unavailable.
4. The current checkout's Duo is rebuilt and retained for the run; OpenCode, skill, scenario, oracle, and fixture identities are reverified.
5. Isolated `openai-codex` credentials are confirmed without recording secret material, and the timeout socket probe succeeds in the disposable fixture.
6. Before any model turn or launcher session, the parent explicitly reconciles the user's no-thread constraint with Step 10's model-turn requirement.

Until then, neither Step 10 nor the Matter seal condition is met.

## Verification

- `go test ./internal/conformance/portablelauncher -run 'TestCanonicalScenario|TestCollectorMandatoryBlockedPrerequisitePreventsPass' -count=1` — passed.
- `./evidence/portable-launcher-conformance/step-10/validate.sh` — asserts the exact accepted identities, rejects the installed/newer and same-version/wrong-schema substitutes, and requires the blocked/no-action result.
- `git diff --check -- evidence/portable-launcher-conformance/step-10` — required final check.

Cross-repo impact: **no cross-repo impact**. This capture confirms, rather than changes, the existing `duo-lab` prerequisite boundary for Pi `0.83.0` and Herdr `0.8.2`/protocol `20`/schema `c48f1f54…b150` as of 2026-09-18; it neither unblocks work there nor invalidates its accepted pin. No handoff is proposed.
