// Package materialize is launch materialization: the M1/M2 pass that runs
// in the CLI layer, before launch resolution, and hands the resolver one
// immutable evidence bundle.
//
// `duo.config/v3` stops authoring the session host in configuration, so the
// host is no longer a name the resolver can dereference — it is late-bound
// at launch (notes/42-config-v3-late-binding.md §3.1–§3.4 and §5, notes/43
// item 13, 2026-08-24 handoff 22). Two passes do that binding:
//
//   - M1 resolves the workspace (--workspace, else the working directory)
//     and deduces exactly one host instance by a fixed four-rung ranking:
//     explicit flag > workspace↔host correlation > ambient environment >
//     policy default. It emits the deduced instance, its host_source, and
//     every captured-but-outranked piece of evidence.
//   - M2 snapshots the standing provider facts by fact ID, so the resolver
//     eliminates a provider-disabled variant against the same facts the
//     record later cites.
//
// The ranking is fixed in code. `session_hosts.deduce` only enables or
// disables the rungs below the flag; it never reorders them. A disabled
// rung still appears in the deduction trail, with consulted = false.
//
// # Boundaries
//
// This package performs no reachability check of any kind: it never dials a
// socket and never validates that a deduced instance is alive (invariant
// I-3). A dead socket fails at spawn, which is the only place that failure
// is honest. Instance *discovery* at the policy-default rung is the one
// piece of I/O here, it is injected, and it runs only when no evidence rung
// yielded a host.
//
// It also never writes. The first-bind write that follows a successful
// spawn is the launcher's (step 14), through the domain's audited verbs.
//
// This package must never import internal/launch: the resolver imports the
// evidence bundle, not the other way round.
package materialize
