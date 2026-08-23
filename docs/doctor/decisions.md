# `doctor` — decisions this step made

The design of record for the eventual `duo doctor` is scattered across
`duo-vnext-external-surfaces.md` ("`duo doctor` joins static and live
diagnostics"), `duo-vnext-installation-contract.md` (drift, re-trust, digest
comparison), and `duo-vnext-implementation-roadmap.md` ("`duo doctor`
reports store, socket, adapter, external-version, conformance..."). The
Step 10 spec deliberately narrows this step to *core* diagnostics only:
authority-store health and a registered-adapters section shaped for later
registration. This file records what building that narrow core forced, and
is explicit about what a fuller `duo doctor` still owes.

## What Step 10 does not build

None of the following exists in `internal/doctor` yet, and none of it is a
silent gap — each belongs to a component this step's boundary excludes or a
later stage's work:

- **Generated-artifact drift** (`duo.projection-stamp/v1` comparison,
  `current`/`missing`/`stale`/`modified` states). `internal/manifest`
  already carries `Drift`/`Stamp` machinery for this; nothing wires it into
  `doctor` yet because no harness renderer exists to generate anything to
  compare against.
- **Harness trust / re-trust reporting.** No harness projection exists yet.
- **Live socket / control-plane checks.** No local service exists yet
  (`internal/host` is a later step, and out of this step's boundary
  regardless).
- **Conformance-record digests, external adapter versions.** These require
  real session-host and agent-runtime registration
  (`internal/host`/`internal/runtime`), which this step does not touch.

`internal/doctor`'s `Report` shape (`Store` plus `Adapters`) is built to
grow additively — a later step adds sibling top-level fields, not a
breaking rename of these two.

## No normative default store path exists yet — this is a judgment call

Nothing in the planning set fixes where the authority store's SQLite file
lives on disk; `internal/store` itself only ever takes an explicit `path`
argument (see its tests, which all use `t.TempDir()`). `duo doctor` has to
resolve *some* default to have anything to probe, so `DefaultStorePath`
adopts the XDG base-directory convention already established for config
(`internal/asset.OverrideDir` uses `$XDG_CONFIG_HOME`): `$XDG_DATA_HOME/duo/
duo.db`, falling back to `~/.local/share/duo/duo.db`. This mirrors
`internal/asset`'s pattern without editing that package (out of this step's
boundary) — the XDG lookup is small enough to duplicate locally rather than
add a cross-package dependency for two lines of logic.

`duo-vnext-installation-contract.md` §1.2 explicitly licenses this: "Environment
variables can select configuration and data roots." `$XDG_DATA_HOME` is
exactly such a variable, already standard, so no duo-specific env var was
introduced. `duo doctor` currently has no `--store-path` flag — nothing in
Step 10's spec asks for one, and adding it would be scope creep ahead of
whatever later step wires a store path into normal operation.

## The writer-lease probe is a transient acquire-and-release, and that still counts as read-only

The spec names `internal/store`'s `OpenAuthority`/lease surface directly and
asks for "read-only health checks." Those two requirements are in tension:
`OpenAuthority` is the *only* way to observe whether the writer lease is
currently held, and observing "nothing holds it" necessarily means
`doctor` itself briefly becomes the holder — `store.Open` alone never
touches the `writer_lease` table at all.

The resolution: `probeWriter` calls `OpenAuthority`, and on success (no
prior holder) closes the handle immediately. `Store.Close`'s documented
behavior deletes the just-inserted lease row keyed to that handle's own
incarnation. Net effect on disk: the lease table is exactly as unclaimed
after the probe as before it. On the other branch — `*store.WriterActive
Error` — `OpenAuthority` never touches the table at all (the row it found
already belongs to someone else); the error's own fields (`Incarnation`,
`PID`, `Hostname`, `ExpiresAt`) are read directly, no separate query
needed. Either way, `doctor` leaves the writer-lease state exactly as it
found it. "Read-only" here means *net effect*, not *zero writes* — the
same sense in which `store.Open`'s own idempotent no-op migration path
(nothing changes when the schema is already current) is "read-only" for a
store already at the latest version.

## A missing store is healthy, not an error — and doctor never creates one

`probeStore` calls `os.Stat` before ever calling `store.Open`. When the
file does not exist, it reports `Present: false, Healthy: true` and stops —
deliberately never calling `store.Open`, which would create the file (and
run migrations) as a side effect of a mere health check. A `duo doctor`
run before any authority write has ever happened is expected and
uninformative, not broken; provisioning a store is `OpenAuthority`'s job at
first real use, not a diagnostic command's.

When the file *does* exist, `store.Open`'s migration step can still write
(bringing an out-of-date schema forward) — that is intentional and useful
doctor behavior (confirming the schema is current, or bringing it current),
not a violation of the "found it present, don't create it" rule above.

## Two sequential opens, not one

`probeStore` calls `store.Open` (to read `Version()`) and, separately,
`probeWriter`'s `store.OpenAuthority` (to probe the lease) — two distinct
`*store.Store` handles, opened and closed in sequence, never concurrently.
This is simpler than trying to get both signals from one handle:
`OpenAuthority` returns no store handle at all on the `*WriterActiveError`
path (by `internal/store`'s own contract), so a single-open design would
have no way to still read `Version()` when a writer is active. Two
sequential, fully-closed opens against the same SQLite file (WAL mode,
`busy_timeout` set) impose no contention risk worth avoiding for a
diagnostic command.

## Exit codes: doctor almost always succeeds

A diagnostic *finding* (store missing, writer active, store unhealthy) is
not a command *failure* — `duo doctor` reports it and exits 0 either way,
the same way `kubectl` or `brew doctor`-shaped tools report problems as
data, not as a nonzero exit. Only a genuine chassis-level failure (the
store path cannot be resolved at all, or JSON encoding of the report fails)
raises a `*duoerr.Error`, and both use the `internal.` prefix
`internal/exitcode.FromError` maps to exit 4 — an unexpected chassis
failure, not a user mistake or a guard refusal.
