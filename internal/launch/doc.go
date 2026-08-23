// Package launch is duo's launch resolver: the component that turns one
// requested preset plus explicit launch constraints into one complete,
// already-validated assignment of leaves to launch tuples, records that
// choice durably, and only then lets a session host prepare a spawn.
//
// Its normative source is `notes/30-preset-selection-design.md` §6.3–§6.9
// and §7 in the planning repository, projected by
// `duo-vnext-go-architecture.md` §5.2 ("A launch resolver completes launch
// resolution and records the launch-resolution record before any
// HostLauncher.PrepareLaunch call") and §4.2's launch-resolution
// transaction boundary.
//
// Four rules shape every type here:
//
//   - **Resolution is static.** §7.1 fixes the consulted-input list at the
//     resolved configuration document, the normalized constraints and their
//     provenance, the digests of accepted immutable conformance evidence,
//     and — for explicit random mode only — the recorded draw. Nothing in
//     this package reads current reachability, process state, selected or
//     effective runtime configuration, provider, vendor, source model, or
//     normalized family, and nothing probes a host or a runtime. Support is
//     an injected [Support] oracle over installed evidence, never a live
//     call.
//
//   - **Resolution is atomic and whole-plan.** Every leaf and every
//     cross-leaf relation resolves before anything spawns; ordered and
//     random selection operate on complete assignments, not per leaf. A
//     failure launches nothing (§6.7 step 10, §6.8 "Every row has effect:
//     no_effect").
//
//   - **The record commits before the spawn.** [Launcher.Launch] resolves,
//     commits the launch-resolution record through the injected [Recorder],
//     and only then calls HostLauncher.PrepareLaunch. The ordering is a
//     property of the types, not of the reading order: the value
//     PrepareLaunch needs can only be produced by a successful commit (see
//     launcher.go).
//
//   - **The ordinary result is thin, the record is complete.** [Report] is
//     §6.9's safe projection — per-leaf agent runtime, declared model line,
//     declared kind, outcome, and actually-relented avoids. Candidate
//     tables, elimination reasons, consulted digests, and random evidence
//     live in [Record], the durable evidence object (§7.3).
//
// The package deliberately owns no I/O: no file reads, no SQL, no adapter
// protocol, and no clock or entropy of its own that a test cannot replace.
// Durability arrives through [Recorder], host spawning through [HostSet],
// randomness through [RandomSource], and time and identity through
// [Options].
//
// See docs/launch/decisions.md for what building this forced.
package launch
