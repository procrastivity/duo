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

## Claude Code runtime adapter (`internal/runtime/claude`, Step 18)

Normative sources: terminal-multiplexers/notes/16-claude-refresh.md (the
P4 probe refresh) and terminal-multiplexers/review/05-close-report.md (the
re-pin verdict), read against the ported quirks in
apps/transcript-tail/adapter_claude.go. Scope matches the package's own
Stage-1 cut: `RuntimeCorrelator` and `ConversationProvider` only, plus a
§5.1 factory. Version floor: Claude Code **2.1.240** (2.1.241 corroborates
the tmux-coordinate registry field only) — the close report's re-pin, not
the Workplan's 2.1.228, which no probe ever ran against.

### Correlate: credential-graded, not a single evidence channel

`RuntimeClaim` has no explicit "this came from a hook" field — the only
signal is which fields are populated. This package reads that
structurally: a claim whose `ReporterCredential` matches this runtime
instance's own configured credential (the Duo secret notes/16 §10 proved
arrives verbatim in every hook process's environment via launch-env
passthrough) binds at `ConfidenceAuthoritative`. A claim with an
`ExternalAgentSessionID` but no credential falls back to a lookup in the
optional `~/.claude/sessions/<pid>.json` registry
(`registry.go:registryHasSession`) and, if found, binds at
`ConfidenceInferred` — never higher, by construction: only the credential
branch can ever return `ConfidenceAuthoritative`, so there is no
comparison for a caller to get backwards. A claim with no
`ExternalAgentSessionID` at all never binds, regardless of
`TranscriptPath` or `WorkingDirectory` — enforced as the very first check
in `Correlate`, ahead of the credential/registry branches, to make §5.3's
"a transcript path or working directory cannot bind a runtime instance"
rule structurally impossible to route around.

A `ReporterCredential` that is present but does not match this instance's
own (or is present while this instance has none configured) is treated as
an error, not a false `Bound` — the same way a mismatched
`IntegrationInstanceID` is: the claim asserts a specific runtime instance
via a value only that instance's own hooks could produce, so a mismatch
is a definite claim about a different instance, not ambiguous evidence.

### TranscriptID is a resolved absolute file path

§5.3 fixes no `TranscriptID` shape. This adapter is disk-backed, so
`TranscriptID` is the JSONL file's absolute path:
`claim.TranscriptPath` trusted verbatim when present (a generated hook's
own payload reports it, notes/16 §4), else derived from
`claim.WorkingDirectory` and the session id using Claude Code's
project-slug convention — cwd with `/` and `.` replaced by `-`
(notes/06-claude.md:10-11) — under this instance's configured
`claudeDir`. `ReadConversation` then treats `req.TranscriptID` purely as a
path to open; it never re-derives one from a session id on its own. With
neither `TranscriptPath` nor `WorkingDirectory` on the claim,
`resolveTranscriptID` returns `""` — a bound correlation with no resolved
transcript location yet is a valid, if degraded, result, since `Bound` is
about identity, not about `ReadConversation` being immediately callable.

### ConversationTurn: text only, no turn-boundary modeling

`runtime.ConversationTurn`'s three fields (`ID`, `Role`, `Text`, `At`) are
already documented above as deliberately minimal. Building against that
minimality forced concrete drops, all ported straight from
`adapter_claude.go`'s existing filtering behavior:

- `tool_use` and `tool_result` blocks, and assistant `thinking` blocks,
  never become turns — there is no field to carry tool-call bookkeeping
  or to flag reasoning-vs-response, and inventing one would be exactly
  the kind of eventual-schema guess the runtime package's own decisions
  above already ruled out for this type.
- `system/turn_duration` and the ported adapter's silence-armed
  end-of-turn bookkeeping have no equivalent here at all. Stage 1's
  `ConversationProvider` is a semantic-turn reader, not a turn-boundary
  signal; that belongs to `ConditionProvider`, out of this step's scope
  by the package doc comment's own cut.
- `isSidechain: true` entries are dropped unconditionally. A subagent's
  transcript is a physically separate file
  (`<slug>/<session-id>/subagents/agent-<id>.jsonl`), so a correctly
  resolved `TranscriptID` should never contain one — the filter is
  defense in depth, verified directly by reading
  `testdata/claude/subagent-sidechain-truncated.jsonl` (all-sidechain)
  and asserting zero turns. Reading a subagent's own transcript as its
  own conversation is a documented gap, not attempted.
- A human-prompt user entry is dropped when it carries a non-nil `origin`
  whose `kind` isn't `"human"` — ported unchanged from
  `adapter_claude.go`'s "task notifications etc." filter. This also drops
  notes/16 §5's peer-injected turns (`origin.kind:"peer"`), whose
  `UserPromptSubmit` hook payload is byte-shaped like a human prompt but
  whose transcript entry alone carries the provenance. `-p` mode's
  `origin: null` (2.1.240's churn against the 2.1.226 census, notes/16
  §1) and interactive's `origin: {kind:"human"}` both pass unchanged,
  since a nil `Origin` never trips the `kind != "human"` branch.
- `ReadConversation` re-parses the whole transcript file on every call and
  paginates over the resulting ordered slice exactly like
  `internal/runtime/fake` does (`After` is a decimal offset, round-tripped
  through `NextCursor`) — chosen to exercise the same pagination contract
  the fake already proves, not because it is the efficient design for a
  live, growing transcript. Recorded as a known Stage-1 cost, not solved
  here.

### Fixture provenance

Ported verbatim (same bytes) from
`apps/transcript-tail/fixtures/claude/` into
`internal/runtime/claude/testdata/claude/`: `print-tool-use-resume.jsonl`,
`print-forked-session.jsonl`, `interactive-truncated.jsonl`,
`subagent-sidechain-truncated.jsonl`, and its sibling
`subagent-sidechain.meta.json`. These fixtures predate the 2.1.240 refresh
(captured against 2.1.224–2.1.226) but still parse cleanly — no field this
package reads was removed, only added to, between those versions and
2.1.240.

`testdata/claude/new-entry-types.jsonl` is new, added to cover notes/16
§1's 2.1.240 churn without running a live `claude` session (this step's
boundary rules out live probing). Its five lines carry different
provenance and that distinction matters for the debt this leaves open:

- The `atis-latch` and `bridge-session` lines are transcribed verbatim
  from notes/16 §1's own captured payloads, not fabricated — the note
  embeds the exact raw JSON. `bridge-session`'s `bridgeSessionId`,
  `ownerAccountUuid`, and `ownerOrganizationUuid` are scrubbed to
  `"<redacted>"` per the Step 18 spec (notes/16 §1 itself only called out
  the two owner-UUID fields for scrubbing; this fixture scrubs
  `bridgeSessionId` too, per the spec's explicit instruction, since it is
  also a live session pointer).
- The two interrupt-shape user entries
  (`"[Request interrupted by user]"` / `"[Request interrupted by user for
  tool use]"`) reuse the already-known, already-tested user-entry
  envelope shape (every field in it is already exercised by the ported
  fixtures) with the verbatim message text notes/16 §2 records. Nothing
  about their *structure* is guessed — only the envelope's incidental
  values (uuid, timestamp, sessionId) are synthesized placeholders, which
  is safe because `parseLine` does not branch on any of them.
- The `total_tokens_reminder` line is genuine debt: notes/16 §1 records
  only that this attachment type exists ("joins the 2.1.226 list"), never
  its internal field shape. The fixture line is a minimal
  `{"type":"attachment","attachment":{"type":"total_tokens_reminder"}}`
  stub — enough to prove this adapter's drop-everything-unrecognized
  default case is safe regardless of payload shape (which is all
  `ReadConversation` needs), but the real internal shape is unverified
  and left as debt for whichever step first needs to read that
  attachment's contents.

Not ported or regenerated at all: the full 2.1.240 fixture regeneration
review/05-close-report.md assigns to this step ("Claude transcript
fixture regeneration at the frozen version") is only partially discharged
by the above — a complete re-capture (a fresh `print-tool-use-resume`-
style session actually run at 2.1.240, not a hand-transcribed supplement)
still requires a live `claude` session and stays open debt, owed to
whichever step is allowed to run one.

### Probe does not launch `claude`

`Factory.Probe` checks only whether this instance's configured Claude
Code home directory (`ClaudeDir`, default `$HOME/.claude`) is reachable on
disk; it never execs `claude --version` or otherwise launches the
runtime, matching this step's boundary. `DetectedVersion` on the returned
`adapter.Probe` is therefore always empty and `Compatibility` is
`CompatibilityUnverified` when the directory exists, never
`CompatibilitySupported` — reachability of `~/.claude` proves nothing
about which Claude Code version wrote it. A caller that needs the
detected version has to get it from elsewhere (a hook payload's own
`version` field, per notes/16 §1) until a later step allows this
package's `Probe` to launch the binary.

### Open items this step leaves for later steps

- No named `DiagnosticRedactionPolicy` or `ConformanceRecordDigest`
  registry exists yet anywhere in this codebase for a non-fake adapter;
  this package's `Descriptor` names the duty and the evidence inline
  (`"redact-credentials-and-transcript-content"`,
  `"notes16-claude-2.1.240"`) rather than inventing a registry this step
  has no mandate to design.
- `SupportedExternalVersions` is the literal set `{"2.1.240", "2.1.241"}`,
  not a range expression — §5.1 leaves the version-rule syntax to a
  future evaluator, so this package does not invent one unilaterally.
- The messaging-socket credential (`CLAUDE_CODE_MESSAGING_TOKEN`,
  notes/16 §10's "first native, vendor-issued credentialed-reporter
  primitive") is a distinct primitive from the launch-env
  `ReporterCredential` this package correlates on; its semantics are
  unprobed and this package does not use it. Recorded so a later step
  does not conflate the two credentials.
- Generated-hook file generation/installation is out of scope by the Step
  18 spec; `Correlate` is built to accept a claim that already carries
  hook-reported identity, not to produce the hook configuration that
  reports it.
