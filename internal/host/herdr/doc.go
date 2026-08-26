// Package herdr is the session-host adapter for Herdr: the five §5.2 host
// interfaces this project scoped (HostDiscovery, HostLauncher,
// HostAttachmentValidator, HostLifecycleSource, HostPromptProvider) plus
// the §5.1 factory envelope, spoken over Herdr's NDJSON socket API.
//
// Everything below that reads like a rule is probe evidence, not a
// preference: it comes from the 2026-08-23 P7 probe session against the
// installed binary (terminal-multiplexers notes/19-herdr-probes.md, closed
// in review/05-close-report.md). Where the older Workplan text and the
// probes disagree, the probes win and the disagreement is recorded in
// docs/adapters/decisions.md.
//
// # Version pin
//
// This build targets Herdr 0.8.2, protocol 20, schema digest
// c48f1f54…b150 (SHA-256 of `herdr api schema --json`). The 0.7.5 fixture
// the Workplan named (notes/05-herdr-schema-0.7.5.json,
// 1ef4eb9e…88c7) is historical: it predates protocol 20 and its recorded
// behavior is refuted in four places below. Probe compares a live schema
// export against PinnedSchemaDigest and reports:
//
//   - supported: version, protocol, and digest all match the pin.
//   - unverified: reachable, protocol at or above the floor, but any of
//     the three differs (including "no schema export available"). An
//     unknown newer version starts here — conformance's stance, confirmed
//     by the probes for 0.8.2 against the then-pinned 0.7.5 record.
//   - incompatible: protocol below MinimumKnownProtocol, i.e. older than
//     anything this project has ever observed.
//   - unavailable: the socket did not answer.
//
// # Identity: there is no server epoch
//
// Probed and refuted at 0.8.2: ping, `herdr status server --json`, and
// session.snapshot carry no instance ID, start time, or epoch. The server
// also *restores* workspace, tab, and pane IDs across a restart, so pane
// coordinates alone collide across incarnations — the duplicate-enrollment
// hazard. The epoch-equivalent is per-pane: a pane's terminal_id changes
// whenever a new terminal backs that pane, including a restored pane after
// a server restart.
//
// This package therefore maps host.Evidence as:
//
//	IntegrationInstanceID <- caller-supplied instance ID, which folds in
//	                         the Herdr session name (see InstanceIDForSession);
//	                         one Herdr server = one integration instance.
//	HostServerEpoch       <- always NoServerEpoch (""), because none exists.
//	                         Filling it with a pane-scoped value would claim
//	                         a server-wide guarantee Herdr does not make.
//	HostContainerID       <- pane.terminal_id: the incarnation of the
//	                         terminal that contains the process. This is the
//	                         epoch-equivalent, at pane granularity.
//	PaneID                <- pane.pane_id: stable addressing, reused across
//	                         restarts, never continuity proof on its own.
//	ProcessBirth          <- pane.process_info's foreground process (or the
//	                         pane shell when the pane is idle) plus a kernel
//	                         start time read from procfs.
//
// ValidateAttachment reports a typed ContinuityClass (and SameProcess
// only for ContinuitySameLive) when the live terminal_id still matches
// the claim *and* both sides carry proven process birth (PID plus a
// kernel start time from the same source). Herdr's process_info returns
// no start time, so an unproven birth is ContinuityUnproven —
// "cannot prove" resolves to "new runtime instance", never to a silent
// merge. Dial, timeout, and missing-socket failures are host.ErrUnreachable,
// not a class and not pane absence. Empty foreground is not a class:
// pidFor falls back to ShellPID.
//
// # No writer presence, no composer lease
//
// Refuted at 0.8.2 and final for 0.8.x: no Herdr surface exposes writer
// presence, per-source input attribution, or "a human is typing". The
// schema-wide scan of all 91 methods and 26 events found nothing, and no
// lease is issuable. This package exposes no such claim, and
// TestNoWriterPresenceSurface fails the build if an exported identifier
// here ever suggests one. Re-run the probe only on a documented input-side
// surface change.
//
// # Repaint revision is not a change detector
//
// At 0.8.2 pane.revision stopped tracking screen content: visible screen
// changes leave it at zero, and only agent-linked transitions bump it.
// Nothing in this package decodes or keys on `revision`, enforced by
// TestNoRevisionDependence. Terminal reads stay out of scope;
// HostTerminalProvider is deliberately not implemented.
//
// # Lifecycle observation is snapshot-diff, never backfill
//
// Three probed facts force one design:
//
//   - pane.created backfill is partial at 0.8.2. It replays only panes
//     created through the API during the current server run; a pane
//     restored from session persistence never replays. Backfill events are
//     shape-identical to fresh creations.
//   - Events between subscriptions are lost. There is no cursor and no
//     replay token, so every (re)subscribe leaves a gap.
//   - A durable agent deregistration happens spontaneously on foreground
//     loss: a pane whose agent process is still running can drop out of
//     the agent registry permanently, leaving agent_status "unknown" and
//     agent.explain returning agent_not_found.
//
// So: events are only a wake-up. ObserveHostLifecycle never derives state
// from an event payload. Each wake — and each SnapshotInterval tick, and
// each resubscribe after a dropped stream — triggers a session.snapshot
// diff against the last observed state, and only the diff emits. The
// snapshot is the inventory; the event stream is a latency optimization.
// For the same reason Discover enumerates panes from session.snapshot
// rather than the agent registry: the registry is not an inventory of what
// is running.
//
// A server that stops answering emits LifecycleDetached (observation lost,
// no claim about the process) and re-emits LifecycleAttached or
// LifecycleExited from the first successful snapshot after recovery.
//
// # Environment scrub duty (seam for Step 20)
//
// A Herdr pane inherits the Herdr *server's* environment. A server
// launched from inside an agent harness propagates that harness's markers
// (CLAUDECODE, CLAUDE_CODE_CHILD_SESSION, …) into every pane, which
// silently disables Claude Code transcript writing — the conformance §8
// failure signature, reproduced live through a host Duo did not spawn.
// Duo-spawned Herdr servers need the same scrub policy as direct runtime
// spawns.
//
// The seam: PrepareLaunch copies ResolvedLaunchTuple.Env into the prepared
// launch's Env, and Start passes it verbatim as the `env` map on
// workspace.create / pane.split, so it applies to the pane's shell before
// any agent starts (verified live). Two limits on that seam, both verified
// live at 0.8.2:
//
//   - Herdr's `env` map can only *set* variables. It cannot unset an
//     inherited one, so a scrub that must remove a variable has to run on
//     the Herdr server process before Duo starts it, or inside the pane
//     command.
//   - The pane's interactive shell re-runs its own startup files, which
//     can overwrite what was set. A PATH passed this way was clobbered by
//     the user's rc files while an ordinary marker variable survived, so
//     the seam is reliable for variables a shell profile does not
//     recompute and unreliable for the ones it does.
//
// Step 20 owns choosing the scrub list and its placement; this package
// owns carrying it and stating what carrying it can and cannot achieve.
//
// # Launch mapping and its lossy edge
//
// Herdr does not take a command line for a pane: pane.split and
// workspace.create create a shell, and agent.start runs the canonical
// executable its manifest defines for a `kind`. So a ResolvedLaunchTuple's
// Command selects the *kind* (by base name, or through Config.KindByCommand),
// and PATH inside Env selects which binary that kind resolves to. The
// tuple's absolute Command path is advisory on this host, which is
// exactly why the Env seam above is load-bearing.
//
// agent.start returns immediately at 0.8.2 with launch_pending true; it
// does not block on interactive readiness. agent_pane_busy means the
// pane's shell is not at its prompt yet and is safe to retry (Start does,
// bounded by Config.StartRetryLimit); every other typed error is surfaced
// unchanged.
//
// Because agent.start is asynchronous, evidence read the instant it
// returns names the pane's *shell*, not what was launched — verified live
// while writing this adapter: the pane's foreground PID changed 10 ms
// after Start returned, so an attachment stored from that evidence failed
// its own first validation. Start therefore waits, bounded by
// Config.LaunchSettleTimeout, for the pane's foreground process to differ
// from the pre-start baseline, and returns the baseline unchanged if it
// never does. That wait is about the observable handover only; interactive
// readiness lives in agent.wait and interactive_ready, which Stage 1 does
// not implement.
//
// # HostPromptProvider: agent.prompt, not composer-safe, not effect-certain
//
// The path is Herdr's native `agent.prompt`. PromptPath reports it as
// exact/native and ComposerSafe false. DeliverPrompt sends one request
// with no wait object and no retry loop.
//
// Effect mapping follows notes/19 §2–§3 (conformance mapping at
// notes/19:397-402):
//
//   - Transport or admission failure that proves the pane accepted no
//     input is no_effect. So are the pre-delivery refusal codes
//     agent_not_ready, agent_blocked, empty_agent_prompt, plus the rest of
//     that family (agent_not_running, agent_kind_mismatch, agent_not_found).
//   - agent_pane_busy on agent.start is a pre-delivery refusal and is
//     retried there. On agent.prompt a write may already have happened, so
//     busy, stall, and timeout map to unknown_effect. agent_prompt_stalled
//     is never retried (decision-03 §7.1).
//   - agent_prompted success is delivered with Acknowledged false. Herdr
//     wait is condition evidence, not acknowledgment. False success is
//     verified: until-matching can return while the text sits as an
//     unsubmitted composer draft. This adapter does not wait, and does not
//     claim composer-safety or merge into a human draft.
//
// Quiet-gate lives in the prompt composer (internal/delivery); this adapter
// reports what the host did.
//
// # Not implemented, deliberately
//
// No HostTerminalProvider. Terminal reads are snapshot-only at 0.8.2, and
// revision is not a change detector (above). Leaving that interface out is
// the boundary; a stub would imply a design nobody has made.
//
// This package must never import internal/runtime; internal/adapter's
// TestRoleSeparation enforces that mechanically.
package herdr
