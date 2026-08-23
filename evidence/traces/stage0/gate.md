# Stage 0 exit gate — 2026-08-23

Roadmap: `duo-vnext-implementation-roadmap.md` §2 (terminal-multiplexers).
Binary under test: `duo version 348a794` (branch `go`). Full suite:
`00-make-check.txt` (lint 0 issues, all packages pass).

Each gate bullet maps to a command transcript or test name in this
directory.

| Gate bullet | Evidence | Result |
|---|---|---|
| A second authority writer fails safely | `01-second-writer.txt`: `TestSecondWriterRefused` (typed `*WriterActiveError`), plus `TestExpiredLeaseTakeover`, `TestDeadHolderTakeover`, `TestUsurpedWriterCannotCommit` | PASS |
| Crash injection preserves every transaction boundary | `02-crash-injection.txt`: `TestBoundaryCrashInjection` — all eight §4.2 boundaries × four injections (mid-body error, pre-commit failure, mid-body panic, crash-after-commit + reopen); `TestBoundaryListMatchesArchitecture` pins the count at 8 | PASS |
| CLI, MCP, and presentation fixture encoders round-trip to equal canonical values | `03-projection-roundtrip.txt`: `TestFixtureRoundTrip` (14 applicable fixtures, 9 skipped with printed reasons), `TestProjectionCasesValueEquality`, neutrality gate tests (fake pair included) | PASS |
| `duo manifest` reports schema and conformance digests; `duo doctor` reports the fake adapters and store state | `04-manifest-doctor.txt`: manifest carries `manifest_digest`, asset sha256 rows, and the `contracts` block (source SHA `bda26c4…` + per-file sha256 for every schema and fixture, `projection-cases.json` included); doctor reports `fake-host` (session_host) and `fake-runtime` (agent_runtime) as `supported`, plus store path/present/healthy | PASS |
| A hostless fake session proves that the domain does not require a terminal | `05-hostless-session.txt`: `TestHostlessFakeSessionReachesNonterminalState` (sessioncore); `TestRoleSeparation` both directions (adapter) | PASS |

Stage 0 work-list note: fixture validation covers the synced contract set
(`duo.external/v1`, `duo.config/v2`, `duo.manifest/v1`,
`duo.projection-stamp/v1`, `duo.projection-conformance/v1`). The roadmap's
original list names `duo.config/v1`; the handoff-20 amendment and Step 10's
strict resolver target v2, and a v1 document refuses with
`config.schema_v1_unsupported` rather than validating.

Verdict: every bullet passes. Stage 0 exits; Stage 1 (narrowed, handoff 20)
may start.
