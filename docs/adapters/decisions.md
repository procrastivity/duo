# Adapter decisions

Decisions the code cannot show on its own. Normative source:
`duo-vnext-go-architecture.md` §5 (Step 11: fake host, fake runtime,
hostless adapters, role separation), read alongside
`duo-vnext-decision-01-identity-lifecycle.md` §5 for the lifecycle model.
§5 gives interface signatures and prose invariants, not struct field
shapes; this file records what building against it forced.

## Fourth package: `internal/adapter`, beyond the named ownership list

The Step 11 spec names three trees to own: `internal/host`,
`internal/runtime`, and an optional `internal/sessioncore`. Building the
§5.1 shared adapter envelope and the §5.4 mechanical dependency-direction
test needed a fourth, neutral home:

- The envelope (`Descriptor`, `Probe`, `CompatibilityState`, the generic
  `Factory[A any]`) is genuinely shared — both host and runtime factories
  declare one — so it cannot live inside `internal/host` or
  `internal/runtime` without making the other import it from a
  role-specific package, which is exactly what §5.4 forbids.
- The `go list -deps` role-separation test (§5.4: "Host adapters compile
  without importing an agent-runtime adapter package" and the reverse)
  has to inspect both `internal/host` and `internal/runtime` without
  itself becoming part of either package's import graph. A test file
  living inside `host` or `runtime` would still technically satisfy that
  (it shells out; it does not `import` the other package), but putting it
  next to the contract it enforces reads better than tucking it into
  whichever of the two happened to get the fake first.

`internal/adapter` holds both. Nothing else in the "do not touch" list
claims this name, and it imports neither `host` nor `runtime`.

## Host is scoped to four of six §5.2 interfaces; runtime to two of seven

The spec is explicit that the runtime surface stops at `RuntimeCorrelator`
and `ConversationProvider` — "nothing beyond those two ... leaving them out
is the boundary, record it, do not scaffold them." The host list
(`HostDiscovery`, `HostLauncher`, `HostAttachmentValidator`,
`HostLifecycleSource`) reads the same way once you count it: §5.2 defines
six host interfaces, and the spec names four. `HostTerminalProvider` and
`HostPromptProvider` are absent from `internal/host/host.go` by the same
discipline as the runtime cut — no empty interface, no placeholder type,
because a placeholder signature would commit a later step to field shapes
(a `TerminalReadCandidate`, a `PromptPathCandidate`) nobody has designed
yet. Both cuts are stated in the package doc comment, not just here, so a
reader of the code sees the boundary without having to find this file.

## Struct fields the architecture doc leaves unspecified

§5 gives Go interface signatures verbatim but never a field-level shape for
`DiscoveryRequest`, `HostCandidate`, `HostLaunchRequest`,
`PreparedHostLaunch`, `HostLaunchEvidence`, `HostAttachmentClaim`,
`HostContinuityEvidence`, `HostObservationRequest`,
`HostObservationStream`, `RuntimeClaim`, `RuntimeCorrelationEvidence`,
`ConversationReadRequest`, or `ConversationBatch`. Every field on these
types in this step is inferred from the surrounding prose (§5.2's "host
evidence uses host-server epochs, host-container IDs, and process-birth
evidence," §5.3's "external agent-session and transcript identifiers as
scoped correlations," decision-01's launch/attach/exit verb table) rather
than copied from a normative table, because none exists yet. Notable
choices:

- `ProcessBirthEvidence` and `Evidence` are pulled out as named types
  used across `HostCandidate`, `HostLaunchEvidence`, and
  `HostContinuityEvidence` precisely because §5.2 repeats "a pane ID or PID
  alone cannot prove runtime continuity" — one shared type means a bare PID
  literally cannot satisfy any of these signatures; a caller has to
  construct the fuller evidence shape.
- `ResolvedLaunchTuple` stands in for the launch resolver's output, which
  is a later step's responsibility (component table: "Launch resolver ...
  Preset materialization, require/avoid narrowing, bounded
  complete-assignment search"). It carries only what `HostLauncher` needs
  to start a process (command, args, env, workspace path, a resolution ID)
  and makes no claim about how the resolver reaches that assignment.
- `PreparedHostLaunch.Opaque any` follows the same opacity rule the
  registry step used for schema references it couldn't yet resolve: real
  adapters will need adapter-private staging data between `PrepareLaunch`
  and `Start` (a staged command line, an allocated pane handle), and
  nothing in §5 says what that looks like across integrations, so the type
  stays open rather than guessing one integration's shape into the shared
  contract.
- `RuntimeCorrelationEvidence.Bound bool` plus a separate error return
  encodes §5.3's implicit distinction between "the claim was insufficient
  to bind" (a legitimate, common result — a working directory alone) and
  "the call itself failed" (wrong integration instance, transport error).
  Collapsing these into one error channel would make ordinary
  not-yet-correlated claims indistinguishable from adapter failures.
- `ConversationTurn`'s three fields (`ID`, `Role`, `Text`, `At`) are
  deliberately minimal. §5.3 does not define a Stage 1 wire shape for
  conversation content — `docs/registry/decisions.md` records
  `conversation.subscribe` as a data row, attested but not yet
  implemented — so this is only enough to exercise
  `ConversationProvider.ReadConversation` and its cursor/pagination
  contract, not a claim on that eventual schema.

## `internal/sessioncore`: runtime-instance lifecycle only, not the session state machine

decision-01 §5.1 actually defines two state machines: the durable session
states (active with an attached or detached host, inactive, archived,
removed) and the runtime-instance states (starting, live, stop_requested,
exited). The hostless-session test only needs to show a session can
advance with no host adapter present — it does not need the full session
state machine, which requires workspace, correlation, and host-attachment
concepts §3's component table assigns to "Domain kernel," a package this
step does not own and that does not exist yet in this tree.

`sessioncore.HostlessSession` therefore implements only the
runtime-instance subset (`Starting -> Live -> StopRequested -> Exited`,
with `Exited` terminal) and nothing else — no workspace field, no
correlation ID, no optional host-attachment field left conspicuously
`nil`. The type cannot express a host attachment at all, which is a
stronger proof of "no session-host adapter" than a field that merely
happens to be unset in one test. This is a deliberate narrowing, not an
oversight: when the domain kernel lands, it almost certainly needs the
five-state session machine as its own type, and this package does not
try to anticipate that shape.

## Fakes are permanent adapters, not `_test.go`-only stubs

The spec calls for the fakes to be "first-class adapters, not test
stubs, since every cross-composition gate runs them permanently." Both
`internal/host/fake` and `internal/runtime/fake` are ordinary (non-`_test`)
packages for that reason: `Host` and `Runtime` hold real state (candidates,
attachments, open observation streams, seeded transcripts) behind a mutex,
and their behavior is driven by method calls (`SeedCandidate`, `Start`,
`Kill`, `SeedTranscript`) rather than canned return values baked into the
struct literal. A gate can compose `hostfake.Factory` and
`runtimefake.Factory` through the same `adapter.Factory[A]` shape a real
adapter would use, probe them, and drive a full
discover/launch/attach/observe or correlate/read cycle exactly as it would
against Herdr or Solo once those land.

Two behavioral choices worth flagging:

- `Host.Kill` simulates the host-side process exiting: it drops the
  attachment record (so a later `ValidateAttachment` reports
  `SameProcess: false`, forcing a new runtime-instance ID per decision-01
  §5.3) and emits a `host.LifecycleExited` event on any open observation
  stream for that attachment. This exists so a gate can exercise the exit
  path without a second, hand-rolled fake.
- `observationStream.emit` and `Close` share one mutex specifically to
  avoid a send-on-a-closed-channel panic: an `atomic.Bool` "closed" flag
  checked before a `select`-with-`default` send is not safe here, because
  `Close` can complete between the flag check and the send. The mutex
  makes emit-vs-close atomic instead of racing two independent
  synchronization primitives.

## `go list -deps` over `go/packages` for the role-separation test

The spec offers both. `go list -deps`, shelled out via `os/exec` from
`internal/adapter/roleseparation_test.go`, avoids adding
`golang.org/x/tools` as a new module dependency for one test — consistent
with this repo's existing minimalism (four direct dependencies before this
step). The test locates the module root via `go env GOMOD` rather than
assuming a working directory, so it is not sensitive to how `go test` is
invoked. Verification for this change temporarily added a blank
`internal/runtime` import to `internal/host/host.go`, confirmed
`TestRoleSeparation/host_does_not_import_runtime` failed with the exact
transitively-imports message, and reverted it — the import does not appear
in the committed diff.

## `//nolint:revive` on the stuttering names §5 specifies verbatim

`golangci-lint`'s revive `exported` check flags every type whose name
repeats its package name (`host.HostCandidate`, `runtime.RuntimeClaim`,
...) as a stutter. Most of the flagged names in `internal/host/host.go`
and `internal/runtime/runtime.go` are copied character-for-character from
§5.2's and §5.3's Go code blocks — the interfaces themselves
(`HostDiscovery`, `HostLauncher`, `HostAttachmentValidator`,
`HostLifecycleSource`, `RuntimeCorrelator`) and the parameter/result types
those interfaces name in the doc (`HostCandidate`, `HostLaunchRequest`,
`HostLaunchEvidence`, `HostAttachmentClaim`, `HostContinuityEvidence`,
`HostObservationRequest`, `HostObservationStream`, `RuntimeClaim`,
`RuntimeCorrelationEvidence`). Renaming any of these to satisfy the linter
would mean this package no longer implements the interfaces the spec
requires by name. Each such declaration carries a
`//nolint:revive // name is §5.2's [or §5.3's] Go code block verbatim`
comment rather than a blanket package- or repo-level exclusion, so the
reason is visible at the declaration and a future genuinely-stuttering
name this package invents still gets caught.

Types this step had to invent that were *not* named in either code block
(`HostEvidence`, `HostAttachment`, `HostLifecycleEventKind`,
`HostLifecycleEvent`, and their constants) were renamed to drop the
prefix (`Evidence`, `Attachment`, `LifecycleEventKind`, `LifecycleEvent`,
`LifecycleAttached`/`LifecycleDetached`/`LifecycleExited`) instead of
suppressing the warning, since nothing in §5 fixes those names.
