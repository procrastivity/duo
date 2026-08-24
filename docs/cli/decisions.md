# `cli` — decisions this step made

The design of record for the `duo session` family is
`duo-vnext-external-surfaces.md` (the `duo session
list|show|enroll|launch|detach|reattach` verb set), the accepted
`session.*` rows already carried in `internal/registry` (CLI path, MCP
projection, permissions), and the dogfood Workplan's Step 21 spec (needs
Step 14's identity/lifecycle kernel and Step 15's launch resolver). This
file records what wiring the CLI directly against those two components
*forced* — the places the Workplan text, the chassis's existing doc
comments, and the synced contract set disagreed, and what Step 21
deliberately leaves for later steps.

## `--output json`, not the chassis's global `--json`

`internal/cliflags`'s own doc comment says the two global flags are
`--json` and `-v`/`--verbose`, "bind once at root, read via context by
every verb… a verb never redeclares either flag" — and `duo doctor` and
`duo manifest` both follow it. The Step 21 spec, and the already-embedded,
already-tested contract fixture
`contracts/fixtures/duo-external-v1/projection-cases.json`, both say
otherwise: every `session.list`/`session.inspect` `cli.arguments` example
in that fixture ends in `--output json`, never `--json`.

This is a real conflict between two things already in the repo, not a
typo to silently resolve one way. `projection-cases.json` is synced,
embedded (`contracts.FS`), and already the authority
`internal/registry/conformance_test.go`'s
`TestProjectionCasesMatchRegistry` holds the registry's CLI-path column
to — it outranks a doc comment two verbs wrote before this fixture
existed. `session.go` registers `--output text|json` scoped to the
`session` command tree rather than reusing the root's `--json` bool.

The one place this could have silently regressed behavior: `internal/cli
.Execute`'s error-envelope selection reads `cmd.Flags().GetBool("json")`
directly, a path a subcommand's `RunE` cannot redirect through context.
Rather than editing `execute.go`/`root.go`/`cliflags.go` — chassis-wide
surface this step has no mandate to redesign — `outputMode` in
`session.go` calls `cmd.Flags().Set("json", "true")` the moment
`--output json` is chosen. Cobra merges a parent's persistent flags into
every descendant's `Flags()` by reference, so this mutates the same
`*bool` root's `--json` flag already points at; `Execute`'s later read
sees it, and an erroring session verb still renders the JSON envelope
without any chassis file changing. `TestSessionLaunch_UnrelentingRequire
Exhausts` pins that this reaches a failure raised deep inside config
load/resolution, not just the RunE's own top-level checks.

**Flagged, not resolved:** `doctor`/`manifest` keep `--json`; the whole
CLI now has two spellings for "JSON output" depending on which verb
family you're in. Reconciling that — either widening `--output` chassis-
wide or amending the doc comment and the synced fixture — is a chassis-
level call this step does not have standing to make unilaterally.

**Resolved 2026-08-24 (dogfood Step 24).** That chassis-level call has now
been made, by the user, while authoring the first daily-driver config:
`--output text|json` is the single spelling, chassis-wide. It binds once as
a root persistent flag; `internal/cliflags` carries it to every verb through
context the way it already carried the global pair; and `session`,
`workspace`, and `provider` no longer redeclare it on their own command
trees. `doctor`, `manifest`, `version`, and `config migrate` read `--output`
instead of the retired boolean, and `internal/cli.Execute`'s error-envelope
selection reads the same flag off the failing command — which retires the
`cmd.Flags().Set("json", "true")` mirror described above together with the
conflict that forced it. Validation of the value now happens once, in root's
`PersistentPreRunE`, so no verb re-checks it and every verb refuses an
unrecognized format identically (`invalid.request`, exit 1).

The global `--json` bool is gone outright: no alias, no deprecation shim,
no hidden flag. `duo doctor --json` is an unknown flag and exits 2. Nothing
has shipped against the old spelling, so there was no compatibility surface
worth keeping and none was kept. The synced contract set needed no change —
`projection-cases.json` always said `--output json`.

## Exit codes: nothing to reserve, because exit 12 does not exist

The Workplan's Step 21 text says "exit codes per the registered
projection (exit 12 reserved)". `internal/exitcode.go` is a closed table
of exactly five values — `Success` (0), `UserFail` (1), `Usage` (2),
`Refusal` (3), `Internal` (4) — and its own doc comment says "a fifth code
gets added only when a real case doesn't fit one of these four — never
speculatively." No code above 4 exists anywhere in the codebase or in any
`docs/*/decisions.md` file. There is no exit 12 to avoid assigning, and
none was introduced.

Every session verb raises a `*duoerr.Error` and lets `exitcode.FromError`
read its `Code` prefix, the same as `doctor`/`manifest`: a `refusal.`-
prefixed code (a domain guard tripping — `duoerrFromDomain`'s mapping for
`ErrIllegalTransition`, `ErrSessionState`, `ErrInstanceExited`, and
similar) is exit 3; an `internal.`-prefixed code (I/O, an unexpected
kernel failure) is exit 4; everything else — including the registry's own
stable codes the launch resolver already raises
(`preset.not_found`, `launch.constraints_exhausted`,
`launch.no_eligible_candidate`, `config.composition_unresolved`) and this
package's own `object.not_found`/`invalid.request` — is the default exit
1. Cobra's own argument-parsing failures (a missing required flag, an
unknown verb) reach exit 2 through `Execute`'s existing, untouched second
return path.

## Recovery has no dedicated verb; the CLI surfaces the derived view

decision-01 §5.1 makes "recovering" a *derived* view
(`domain.Authority.View`/`SessionView`), never a durable session state —
the kernel does not store it, it computes it from whether the session's
current runtime instance is still in the in-memory `recovering` set
`Authority.Open` populates on replay. `internal/registry` has no
`session.recover` row, and none was added: a dedicated recovery verb would
be state the registry does not name and the kernel does not keep.
`duo session list` and `duo session show` project the derived view
directly (`ViewAttached`/`ViewDetached`/`ViewRecovering`/`ViewInactive`/
`ViewArchived`/`ViewRemoved`), and the existing `duo session
detach`/`reattach` verbs are what an operator already has to act on one —
`reattach`'s revalidation-against-the-held-claim path is exactly decision-
01 §4.4 rule 1's continuity proof, just not yet wired to a live host probe
(see below).

**A consequence worth flagging loudly, discovered by the round-trip test,
not designed in:** `duo` has no long-running daemon holding an
in-memory `domain.Authority` across invocations — every verb opens the
store fresh (`openReadAuthority`/`openWriteAuthority` →
`domain.Open`), and `Authority.Open` unconditionally marks every
nonterminal runtime instance "recovering" on load, per decision-01 §4.4.
That rule is written for a real authority restart, but a one-shot CLI
process triggers it on *every command after the one that created a
session* — not just a crash-and-restart. `internal/cli/session_test.go`'s
`TestSessionEnroll_ThenListShowDetachReattach` had to be written to expect
`view: recovering` (not `attached`, not `detached`) from the very first
`session list`/`session show` call after enrollment, and stays
`recovering` through detach and reattach, because nothing at this layer
ever calls `domain.Authority.ResolveRecovery` — that verb needs real host
evidence (a live Herdr probe proving the same process birth), which is
Step 22 and the host-adapter (17) seam's job, not Step 21's (14, 15).

Net effect for the dogfood checkpoint: today, `duo session list` will show
a session an operator just launched or enrolled as `recovering` on every
subsequent command, never `attached`, until something wires a
`ResolveRecovery` call against live Herdr evidence. This is flagged here
for Step 22 (which owns `ResolveRecovery`'s real caller) and Step 23 (the
re-targeted Stage 1 exit gate) to pick up explicitly — it is not a bug in
this step's CLI wiring, but it is a real gap between "the CLI surfaces
recovery" (done) and "recovery resolves back to normal use" (not yet
reachable from any verb this step registers).

## `session enroll` is flag-driven, not Discover-driven

The natural shape for `duo session enroll` is "ask a session-host adapter
to discover unclaimed candidates, then enroll the one the operator
picks." That shape does not exist to wire against: nothing in this
codebase bridges `host.Evidence` (`internal/host`'s
`IntegrationInstanceID`/`HostServerEpoch` *string*/`HostContainerID`/
`ProcessBirth`) into `domain.Fingerprint` (`internal/domain`'s
`IntegrationInstance`/`HostEpoch{Kind,Value,Scope}`/`Container`/
`Process`) — no production file anywhere constructs a
`domain.Fingerprint{}` from a `host.Evidence` value, checked directly
before writing this file. Building that bridge would mean inventing the
`herdr.terminal_id`-shaped epoch kind/scope convention on this step's own
authority, in a package (`internal/cli`) that owns neither the host
adapter's evidence shape nor the kernel's fingerprint shape — exactly the
kind of guessed cross-package glue `docs/registry/decisions.md`'s row
policy warns against for the registry table, applied here to the same
problem one layer down. Step 21's declared dependencies are 14 (identity
kernel) and 15 (launch resolver) only — not 17/18/19, the host- and
runtime-adapter steps that would own this bridge.

`duo session enroll` therefore takes the live-runtime fingerprint as
explicit flags (`--integration-instance`, `--epoch-kind`, `--epoch-value`,
`--epoch-scope`, `--container`, the optional `--process-*` triple, the
optional `--agent-integration-instance`/`--agent-session-id`/
`--transcript`), attested as `domain.SourceOwner` — decision-01 §3.4's
"explicit owner or administrator action," which an operator typing these
flags by construction is. This is a real, complete enrollment path (it
exercises the kernel's actual atomic-enrollment/idempotent-repeat/
conflict-enrolls-nothing rules end to end,
`TestSessionEnroll_ThenListShowDetachReattach` covers all three), just
not the ergonomic one a future Discover-then-enroll flow will replace it
with once the evidence bridge exists.

## The launch resolver, recorder, and Stage-1 support/host-set are real, not stubs

`duo session launch` is wired against the actual
`internal/launch.Resolver`, `internal/launchrecord.Recorder`, and
`internal/domain.Authority` — the same components Step 15/14 built, not a
CLI-local reimplementation. `--dry-run` calls `Resolver.Resolve` directly
and never opens the authority store or acquires the writer lease at all,
matching §6.10 ("no durable record… no session… no spawn"); a real launch
opens the writer lease, builds a `launchrecord.Recorder` over it, and goes
through `launch.Launcher.Launch`, so the pre-spawn ordering guarantee
(record committed before `HostLauncher.PrepareLaunch`) is the launcher's,
unmodified.

Two pieces this step *did* have to build, because nothing registered one
yet:

- **`stage1HostSet`** resolves a materialized launch tuple's session host
  to a live adapter by the declaration's `kind` field —
  `doc.SessionHosts[name]["kind"]` — the same config-map convention
  `internal/launch/catalog.go` already reads an agent-runtime
  declaration's `kind` with (`stringField(runtimeDecl, "kind")`), applied
  here to session hosts by analogy since no session-host equivalent was
  written yet. Stage 1 supports exactly one kind, `"herdr"` (building a
  real `internal/host/herdr.Host` from `kind`/`socket_path`); anything
  else — a declared but unsupported `tmux` entry, a typo — is refused by
  name (`"session host %q declares kind %q, which this Stage-1 build does
  not support"`), never guessed at. This is the CLI package's composition-
  root job (the same role `doctor.go`'s `registeredAdapters` already
  plays for the fake pair), not a new architectural seam.

- **`stage1Support`** is `internal/launch.Support`: it reports a tuple
  supported only when its session host is `kind: herdr` *and* its agent
  runtime is `claude` or `pi` (the narrowed Stage 1 slice, roadmap Stage
  E), citing a digest built from `herdr.Factory{}.Descriptor()`,
  `claude.Factory{}.Descriptor()`, and `pi.Factory{}.Descriptor()`'s
  `ConformanceRecordDigest` fields — read once, as package-level `var`s.
  Deliberately **never** `Factory.Probe()`: §7.1 forbids resolution from
  reading "current reachability, process presence, adapter health, or a
  probe run for this launch," and `Descriptor()` is a pure struct literal
  on every one of the three factories (confirmed by reading each), while
  `Probe()` dials out. `launch.AllSupported` (the deliberate
  "everything's fine" escape hatch `internal/launch/support.go` names
  precisely so it can't be reached for *silently*) was rejected for the
  same reason its own doc comment gives: defaulting to permissive would
  "collapse §7.1's accepted rung… into the configuration-only rung §7.5
  rejects, and it would do it invisibly." Every digest `stage1Support`
  cites traces to a real, checked-in adapter build.

## No normative default `duo.config/v2` path exists yet

Nothing in the planning set or the existing code fixes where the launch-
preset document (`duo.config/v2`, distinct from `internal/config`'s own
merged `config.yaml` tool config) lives on disk — `config.LoadV2` only
ever takes an explicit path. `duo session launch --config` defaults to
`$XDG_CONFIG_HOME/duo/duo.config.yaml`
(`defaultLaunchConfigPath`), following the same "resolve under
`$XDG_CONFIG_HOME/duo`" convention `internal/asset.OverrideDir` and
`internal/doctor.DefaultStorePath` (`$XDG_DATA_HOME/duo/duo.db`) already
established, rather than inventing a third convention. `--config`
overrides it outright.

**Flagged, not resolved:** Step 24 ("the user's real `duo.config/v2`… with
daily presets") is where the shipped dogfood document actually lands; it
should confirm this path or amend it in the same change that authors the
document, rather than this step guessing a name Step 24 then has to live
with silently.

**Resolved 2026-08-24 (dogfood Step 24).** The user confirmed the path
as-is while authoring the daily-driver document: the launch config lives
at `$XDG_CONFIG_HOME/duo/duo.config.yaml` (falling back to
`~/.config/duo/duo.config.yaml`), and `--config` still overrides it
outright. The authored document is `duo.config/v3`; a dated copy sits in
`evidence/dogfood/2026-08-24/`. No rename was taken to separate it from
the merged `config.yaml` tool config — the two names stay adjacent in
one directory, as this entry already described.

## `session launch` records the host attachment too (2026-08-24)

Found live on the first dogfood day: `duo session detach <id>` and `duo
session reattach <id>` refused every session `duo session launch` created,
with `domain: unknown duo object: session <id> has no host attachment`.
Only `duo session enroll` wrote a `HostAttachment`, so the two verbs were
reachable only for adopted runtimes — not for the ones Duo itself spawned.
The user's call, ratified the same day: **duo opened the pane, so duo
observes it.** A launched session is attached by default, exactly as an
enrolled one is.

**The verb is `domain.Authority.Bind` with a `Fingerprint`**, attested
`SourceLaunchPlan` — §3.4's second admissible source, and this is that
plan's own spawn reporting back. `Bind`'s fingerprint branch is the same
code path enrollment's `createEnrollment` uses for the attachment half: it
mints the `HostAttachment`, writes the `host.container` and `host.epoch`
correlations, adds the process-birth correlation on the runtime instance,
and seizes the live-runtime claim — which is what makes reattach possible
at all, since reattach revalidates the observed fingerprint against the
claim the session already holds. No domain change was needed.

**It is a post-spawn fact, and that does not weaken invariant I-1.** The
session, its runtime instance, and the launch-resolution record still
commit before anything spawns; the attachment cannot, because the evidence
it rests on — the pane's `terminal_id` and the agent's process birth — does
not exist until the spawn produced it. `recordLaunchAttachments`
(`internal/cli/hostbind.go`) therefore sits exactly where `bindFirstHost`
does, unreachable from a `Launch` that returned an error, and runs first of
the two because it is the session's own fact and asks nobody anything while
the first bind may stop to confirm an ambient-env deduction.

**The evidence bridge crosses two fields, and the crossing is the whole
risk.** `liveRuntimeFingerprint` is `hostFingerprint`'s sibling, aimed at
`domain.Fingerprint` instead of `domain.HostFingerprint`. The kernel wants
the *stable* container coordinate in `Container` and the *incarnation* in
`Epoch`; the Herdr adapter puts the incarnation (`terminal_id`) in
`host.Evidence.HostContainerID` and the stable coordinate (`pane_id`) in
`PaneID`. So the two cross over here, at `herdr.terminal_id` / pane scope —
the same spelling `duo session enroll --epoch-value <terminal_id>
--container <pane_id>` asks an operator to type, which is the only reason a
human-typed reattach can match a launch-recorded claim. An unrecognised
host kind gets the zero fingerprint and is refused: inventing an epoch kind
and scope for a host whose incarnation evidence nobody has probed is the
guess "`session enroll` is flag-driven, not Discover-driven" refused to
make, and it stays refused.

**A failed attachment write is loud, and never fails the command.** Same
rule `bindFirstHost` already applies to the workspace correlation, applied
to the object one level down, and for the same reason: by the time this
code runs an agent is running in a pane that nothing can un-spawn, and the
session and record are already durable (§7.4). Failing the command would
report "launch failed" about the only fact that matters being true, and
would leave the operator with a live pane and an error exit. So the write
is reported on stderr instead — and loudly, naming both halves ("the launch
itself succeeded", "`duo session detach` and `duo session reattach` will
refuse for this session"), because the session is then observable but not
detachable and no verb adds an attachment afterwards; an operator not told
here would find out only when detach refuses.

**Two limits this leaves standing**, both flagged rather than resolved:

- **One current attachment per session.** Every leaf of a launch gets its
  own attachment and its own live claim — two panes are two live runtimes
  and may never share one — but `Session.Attachment` holds a single
  current one, so the last leaf spawned is the one detach and reattach act
  on. Making that per-leaf is a kernel change to how a session points at
  its attachments, not a CLI one
  (`TestEveryLeafOfOneLaunchIsAttached` pins today's behaviour).
- **Reattach's fingerprint is not readable from any verb.** `duo session
  show` reports lifecycle, view, and instance state, not the attachment's
  epoch, container, or process birth, so an operator cannot look up the
  five flags `duo session reattach` requires. Detach now works from the
  session ID alone; reattach still needs values only the launch's own
  evidence knows. Surfacing them on `session show` is the obvious next
  step, and it is a projection-contract question this entry does not
  settle.
