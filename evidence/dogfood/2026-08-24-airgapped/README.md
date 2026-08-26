# Airgapped dogfood day — 2026-08-24 (BSIMENSEN1)

Imported 2026-08-26 to close terminal-multiplexers `notes/47` §4.

This is the checkpoint day run on the airgapped machine (`bsimensen@BSIMENSEN1`,
Herdr session `duo`, binary `53ddced` then rebuilt to `4ced46d`). It is a
sibling of `evidence/dogfood/2026-08-24/`, which is the connected-machine
session the same calendar day and is not overwritten.

| file | what |
|---|---|
| `log.md` | verb-by-verb day log (copied from the package `records/day-log.md`) |
| `checkpoint.md` | 9/9 verdict after the same-day rebuild |
| `decisions.md` | authored roster (`pi_wyvern` / `qwen-3.6`) |
| `findings.md` | historical findings; two OPEN items were implemented by duo-dogfood-recovery |
| `duo.config.yaml` | the authored `duo.config/v3` document |

The authority-store dump from this machine is the recovery fixture
`internal/domain/testdata/duo-db-export.sql` (sha256 `ac5d7860…`). Do not
rewrite these records.
