# duo-db-export.sql — sidecar

Export of the duo authority store from the airgapped dogfood machine.

| field | value |
|---|---|
| source path | `/home/bsimensen/.local/share/duo/duo.db` |
| source size / sha256 | 389120 bytes / `cbebd1da5a2732086397286cf1479c9a270ab173b0ff8ce68ee189e9560c7063` |
| export file | `duo-db-export.sql` (SQLite `iterdump`, complete schema + data) |
| export sha256 | `ac5d786096ed89767a31f514b6758a8a6c05afa08daca0decccc120c134f0aba` |
| exported at | 2026-08-25T17:37:31Z |
| exported by | bsimensen@BSIMENSEN1 using `duo version a91a0ff (built 2026-08-25T16:11:54Z)`'s store, read with Python `sqlite3` |
| store schema | version 1 (`substrate`), applied 2026-08-24T18:05:15.086Z |
| first / last fact | 2026-08-24T18:05:15.093Z / 2026-08-24T22:47:25.629Z |

## Build provenance of the writes

The store carries no build stamp per row (the `incarnation` column is a
per-process ID, not a build). Provenance is reconstructed from
`records/day-log.md`, the audit timestamps, and the repo's `go` branch:

- **created and written 2026-08-24T18:05–18:08Z by `53ddced`** (built
  2026-08-24T17:24:27Z): the schema migration and the first five
  sessions (dogfood package run, including the failed `build_and_verify`
  and the enroll).
- **18:22Z onwards by `4ced46d`** (built 2026-08-24T18:21:02Z): the
  `build_and_verify` retest and the first launch-recorded attachments
  (`enrollment … bind` audit rows begin here; commit `8c8deac`).
- **18:29Z–22:47Z by intermediate `go`-branch builds between `4ced46d`
  and `a91a0ff`**: the hand-typed launches. `--close-on-exit` launches
  at 22:46Z prove at least `c2cab35` was built by then. Exact
  intermediate SHAs were not recorded.
- **read (not written) on 2026-08-25 by `a91a0ff`** for this export.

## Contents

Row counts: `audit_log`=52, `schema_migrations`=1, `stream_log`=270, `work_attempt`=0, `work_queue`=0, `writer_lease`=0.

`stream_log.payload` is one JSON fact per row (`duo.domain.fact/v1`);
kinds: correlation.claimed=70, session.created=25, instance.started=25, credential.issued=25, session.launched=24, launch.resolved=24, live-runtime=23, claim.held=23, attachment.created=23, attachment.state=3, workspace.host_bound=2, workspace.created=2, session.enrolled=1.

## Sensitivity

`credential.issued` facts and `instance.credential_fingerprint` hold the
**SHA-256 fingerprint** of each reporter credential, never the secret
(`internal/domain/ids.go` `mintCredential`/`credentialFingerprint`; the
secret is printed once at issue and not stored). Paths under
`/home/bsimensen` and Herdr socket paths are present. No other secrets.

## Restore

```
sqlite3 duo-restored.db < duo-db-export.sql
# or, without the sqlite3 CLI:
python3 -c "import sqlite3;c=sqlite3.connect('duo-restored.db');c.executescript(open('duo-db-export.sql').read());c.commit()"
```

Then point a build at it (`duo doctor` names the store path it opens) or
query it directly. It is the regression fixture named in
`handoff-session-exit-reconciliation.md`: 24 instances `starting` + 1
`live`, all expected to resolve to `exited` once reconciliation exists.
