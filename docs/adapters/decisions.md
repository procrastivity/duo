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

## Host is scoped to five of six §5.2 interfaces; runtime to four of seven

Stage 1's runtime surface stopped at `RuntimeCorrelator` and
`ConversationProvider` — "nothing beyond those two ... leaving them out
is the boundary, record it, do not scaffold them." The delegation-loop
slice then brought in `ConditionProvider` (steps 07–08) and
`RuntimePromptProvider` (steps 10 and 12). The host list started
the same way: Stage 1 shipped four of six (`HostDiscovery`, `HostLauncher`,
`HostAttachmentValidator`, `HostLifecycleSource`). Delegation-loop step 10
named `HostPromptProvider` and `PromptPathCandidate` on `internal/host`;
step 11 fills the interface (Herdr `agent.prompt`, plus the fake host).
`HostTerminalProvider` stays absent — no empty interface, no placeholder
type. Both cuts are stated in the package doc comment, not just here, so a
reader of the code sees the boundary without having to find this file.

(2026-08-26 amendment, delegation-loop step 11: the stale "four of six"
cut is superseded for `HostPromptProvider` only. `HostTerminalProvider`
remains out of scope.)

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
  end-of-turn bookkeeping have no equivalent here at all.
  `ConversationProvider` is a semantic-turn reader; turn-boundary
  signals belong to `ConditionProvider` (condition.go, step 08).
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
- `CLAUDE_CODE_MESSAGING_TOKEN` (notes/16 §10) is a distinct vendor
  primitive from the launch-env `ReporterCredential` this package
  correlates on. Step 12 (2026-08-26) sends NDJSON to the bound
  instance's messaging socket and still correlates only on
  `ReporterCredential`. Notes/10 admitted a frame with no token;
  notes/16 recorded the token's existence with unprobed semantics, so
  this adapter does not invent a token handshake and does not put
  either secret on the frame. If a later pinned claude requires the
  token to admit, pass `CLAUDE_CODE_MESSAGING_TOKEN` as itself — never
  the reporter credential as the socket secret.
- Generated-hook file generation/installation is out of scope by the Step
  18 spec; `Correlate` is built to accept a claim that already carries
  hook-reported identity, not to produce the hook configuration that
  reports it.

## Pi runtime adapter (`internal/runtime/pi`, Step 19)

Normative sources: terminal-multiplexers `notes/18-opencode-pi-refresh.md`
(the P6-Pi probe), `review/05-close-report.md` (the re-pin and the
refinements), `notes/07-pi.md` including its 2026-08-23 amendments, and the
ported parser in `apps/transcript-tail/adapter_pi.go`. Scope matches the
package's own Stage-1 cut: `RuntimeCorrelator` and `ConversationProvider`
only, plus a §5.1 factory. Version pin: pi **0.83.0**, the version every
piece of evidence below was gathered at.

### Branch: build the in-process extension, not the filesystem-skill fallback

Fixed by the P6 verdict — the probe built the extension and proved it, so
the fallback is not a live option here. The consequence for this package is
that correlation evidence originates *inside* the agent process
(`ctx.sessionManager.getSessionId()` / `.getSessionFile()`, `ctx.cwd`),
which is the only channel on Pi that can produce a session id nobody had to
guess from the filesystem. The generated extension is a harness component,
not part of the Go authority (conformance §7.4): its failure removes its own
paths and nothing else, which is why `Correlate` still has a credential-less
fallback grade and `ReadConversation` works without it entirely.

### The asset is guarded by string assertions, on purpose

`extension/duo-pi-reporter.ts` is embedded source Duo generates and never
executes, so no Go test can exercise its behavior. Three of its lines encode
findings that each cost a live probe session, and each is one plausible edit
away from a silently wrong reporter, so `extension_test.go` asserts them as
text:

- **`ctx.mode === "tui"`, never `ctx.hasUI`.** At 0.83.0 rpc mode installs a
  real extension UI context, so `ctx.hasUI` is true there too; a hasUI gate
  lets an rpc-driven pi present itself as the pane's session. The test allows
  `ctx.hasUI` only on the line that reports it as a diagnostic field, and in
  comments. The check is deliberately duplicated Duo-side in
  `ReportedClaim.Validate`, which refuses a non-`tui` claim: the extension
  file lives on the user's disk and can be edited, and a future rpc-driven
  launch variant should have to widen the gate deliberately, in both places.
- **`session_shutdown` is terminal only on `reason === "quit"`.** `/reload`,
  `/new`, `/resume`, and `/fork` tear down and rebind the extension runtime
  while the agent process lives on.
- **`agent_settled`, never `agent_end`.** After `agent_end` pi may still
  auto-retry, auto-compact, or run a queued follow-up. Stage 1 only stamps
  `lastSettledAt` on the claim record from that event; condition observation
  is Stage 2, and subscribing to `agent_end` at all is a test failure.

Two more asset rules are guarded the same way: the `session_start` dedupe (an
rpc rebind emits `session_start` twice in the same millisecond) and the
absence of any prompt-delivery call. A separate test extracts the key set
from the asset's `JSON.stringify` literal and compares it to
`ReportedClaim`'s JSON tags, because those two are one wire contract and the
decoder is strict (`DisallowUnknownFields`) — a field added on one side alone
must fail loudly rather than silently drop.

### Credential: environment at module load, then scrubbed

The verified shape (notes/18 §6). The spawn builder adds two variables
(`ReporterEnvironment`); the extension reads both at module load — before any
turn can run — holds them in module scope, and deletes them from
`process.env`. What that buys is exact and worth stating: no tool subprocess
pi spawns inherits the token. What it does not buy: the token authenticates
the *process*, not the extension; any code in that process can read it. The
scrub is guarded by an ordering test (read before delete, both before the
default export, and no `process.env` access inside any handler).

`ValidateSocketPath` enforces the 108-byte `sun_path` limit at
path-construction time. That is a live failure mode, not hygiene: the probe's
first `listen` died with `EINVAL` on a long scratchpad path, where the error
is least legible.

### Correlate: two grades, and a deliberate divergence from the Claude adapter

`ConfidenceExtensionExact` when the claim presents the credential issued to
this instance; `ConfidenceTranscriptHeuristic` when no credential was issued
at all (a pi Duo did not spawn) and the session id is taken at face value
from the transcript channel. No claim without an `ExternalAgentSessionID`
ever binds — checked first, so §5.3's "a transcript path or working directory
cannot bind a runtime instance" cannot be routed around. On Pi that rule has
teeth beyond the contract's letter: the cwd slug that names a transcript
directory is shared by every session ever run in that directory *and* is
lossy (a `-` in the cwd is indistinguishable from a separator), so a
path-or-cwd binding would be wrong in ordinary use, not just in principle.

**Divergence, flagged for the contract owner.** A `ReporterCredential` that
is present but wrong is `Bound: false` here, where `internal/runtime/claude`
raises an error for the same input. The reasoning here: the adapter did
attempt the claim and reached a verdict — "these identifiers do not resolve
to *this* instance" — which is what `Bound: false` documents, while §5.3
reserves the error channel for a claim that cannot be attempted at all. Both
readings are defensible and the two adapters should not stay split; this is a
conformance question to pin, not a difference either adapter should resolve
unilaterally inside the other's package.

A claim whose transcript path names a different session id than the claim
itself is contradictory evidence and does not bind, whatever the credential
says.

### TranscriptID is the transcript's absolute path, and may legitimately be empty

§5.3 fixes no `TranscriptID` shape. On Pi the path *is* the transcript's
identity: there is no transcript id anywhere else, and the file name's second
component is the session id. Unlike the Claude adapter, this one also
resolves a path from a bare session id — `<sessions-root>/<cwd-slug>/*_<id>.jsonl`,
narrowed by the slug when a cwd is known and swept across the tree when it is
not, refusing on more than one match. That is safe here only because the
resolution is verifiable: `ReadConversation` re-reads the header line of
every file it opens and refuses one whose `id` is not the session it was
asked for. The slug is a lookup hint; the header is the proof.

A bound correlation with an empty `TranscriptID` is a valid result: `pi
--no-session` writes no transcript at all, and a just-started session may not
have one yet. Binding is about identity; a missing transcript becomes an
error only when someone tries to read it.

### ConversationTurn: one turn per message entry that has text

The projection rule is one turn per pi `message` entry with text content,
with pi's own role as the turn's role (`user`, `assistant`, `toolResult`) —
no Duo-side role vocabulary is invented, because §5.3 fixes none and a second
unpinned vocabulary is a second thing to keep in sync. Two block kinds are
dropped: `thinking`, because `ConversationTurn` has no content-kind
discriminator and emitting reasoning as `Text` would present it as something
the agent said; and `toolCall`, which carries a tool name and arguments and
no text at all. Multiple `text` blocks in one entry are joined rather than
split into turns, since they share one entry id and one timestamp.

This diverges from `internal/runtime/claude`, which drops tool results
entirely. The difference is format-driven rather than a disagreement:
Claude's `tool_result` is a content block *inside a user message*, so
projecting it would label tool output as the user speaking, while Pi's
`toolResult` is a top-level message role that can be labeled correctly. The
ported `adapter_pi.go` also surfaces tool results (as their own event kind),
so keeping them is the smaller departure from the prior art. Flagged for the
same conformance pass as the credential divergence above.

**Missing field, reported not added.** Both drops exist because
`ConversationTurn` has no content-kind discriminator. That is a shared
contract shape (`internal/runtime/runtime.go`), which this step does not own
and did not edit; the observation is recorded here for whichever step owns
the eventual `conversation.subscribe` schema.

### Cursor is a line count, and every read rescans

`After` is an opaque decimal count of transcript lines. Pi appends — `pi -c`
resumes into the *same* file with no new header — so a line count stays valid
across a resume, which the `basic-with-resume` fixture pins. A fork is a
different file with a different session id into which pi *copies* the
parent's entries with their original entry ids, so reading a forked
transcript returns the inherited history too (pinned by the `forked`
fixture); the parent's session id will not open the child's file, because the
header check refuses it.

`ReadConversation` re-parses the whole file on every call, like the fake and
like the Claude adapter. Recorded as a known Stage-1 cost: a live tail is a
Stage 2 concern that belongs with the streaming contract that would justify
it.

### Unsupported at 0.83.0, recorded rather than worked around

- **Blocked condition evidence is structurally absent.** Pi has no permission
  system and emits no blocked-family event; the only channel is the
  cooperative `herdr:blocked` EventBus convention, and the 2026-08-23 sweep
  found one listener and zero emitters, with nothing in the pi dist. A
  reporter cannot lift evidence that is not produced, so a future
  `ConditionProvider` must degrade this facet explicitly. This is a stronger
  statement than "ungraded": the evidence does not exist to grade.
- **Working mode is unsupported.** Pi has no built-in working-mode concept.
  Its `--mode` flag is an execution surface (`tui`/`rpc`/`json`/`print`) —
  the same value this package gates the pane session on — and plan behavior
  is extension-only. A future `RuntimeConfigurationProvider` must report the
  facet unsupported rather than map `--mode` onto it, which would conflate
  two namespaces the conformance record keeps separate.

Both are also stated in the package doc comment, so a reader of the code
meets the constraint without finding this file.

### Native prompt delivery: proven, documented, not implemented

The probe proved that this same extension shape delivers text as a real user
turn labelled `input.source: "extension"` — native provenance the PTY channel
cannot produce — and interrupts a running tool call over the same socket.
That is Duo's Stage 2+ prompt surface. The asset documents the capability in
a comment and `TestExtensionHasNoPromptDeliveryCall` fails if a delivery call
appears in it. Note for whoever implements it: the transcript records an
injected turn as an ordinary `user` entry (the `injected-abort` fixture is
exactly that session), so provenance lives on the event channel only — the
conversation channel cannot tell an injected turn from a typed one.

### Fixture provenance

Recorded per file in `internal/runtime/pi/testdata/README.md`. Two
transcripts are the research repo's pinned 0.83.0 fixtures, unmodified,
re-verified against 0.83.0 on 2026-08-23. The third is new here: the actual
P6-Pi probe session that notes/18 §2 and §5 quote by id, which carries the
natively injected turn, an abort, and the zero-content aborted assistant
entry in one file. `reported-claim-tui.json` is the one constructed artifact
— probe-recorded field *values*, but this package's own wire framing, since
`duo.pi.reporter/v1` did not exist when the probe ran. It says so in its own
provenance note rather than presenting as captured evidence.

### Probe asks the binary, and claims nothing about the format

`Factory.Probe` runs `pi --version` and reports `supported` only for the
pinned 0.83.0, `unverified` for any other version (the format may well be
unchanged — but nothing here has been checked against it), and `unavailable`
when the binary is absent. It does not read a transcript, so it never claims
to have *observed* the on-disk format: `ProtocolOrFormatIdentity` names the
format this build parses, and every `ReadConversation` re-verifies the actual
header, including the schema version, on the file it opens.

## Herdr host adapter (`internal/host/herdr`, Step 17)

Normative sources: `duo-vnext-go-architecture.md` §5.1/§5.2 for the shapes,
and the 2026-08-23 P7 probe session for what Herdr actually does —
`notes/19-herdr-probes.md` plus `review/05-close-report.md` in the planning
repository. Where the two disagree, the probes decided; every such decision is
below.

### Version pin: 0.8.2, against a Workplan that says 0.7.5

The Step 17 text pins "Herdr 0.7.5 (digest pinned in
`notes/05-herdr-schema-0.7.5.json`)". That text predates P7 and is
**overridden**: the dogfood implementation targets Herdr 0.8.2, protocol 20,
schema digest `c48f1f54…b150`, which is what the close report re-pinned and
what is installed. The override is not cosmetic — four recorded 0.7.5
behaviors are refuted at 0.8.2 (full `pane.created` backfill, "retry a stalled
prompt once", `revision` tracking screen content, no `HERDR_BIN_PATH` in the
pane environment), so building against the 0.7.5 record would have encoded
three wrong duties. The 0.7.5 fixture stays historical; nothing in this
package reads it.

`PinnedSchemaDigest` was re-derived live while writing this adapter
(`herdr api schema --json | sha256sum`) rather than copied from the note, and
it matched. `--json` on stdout and `--output` to a file produce byte-identical
schemas, so the probe uses stdout and needs no temp file.

### Evidence mapping, and why `HostServerEpoch` is empty

There is no server epoch at 0.8.2 — `ping`, `herdr status server --json`, and
`session.snapshot` carry no instance ID, start time, or epoch. The temptation
is to fill `HostServerEpoch` with something rather than leave a contract field
blank. This package leaves it blank (`NoServerEpoch`) on purpose: writing a
pane-scoped value into a server-scoped field would make a guarantee Herdr does
not offer, and every later consumer would read it as one.

The incarnation lives in `HostContainerID`, which carries `terminal_id`. That
is a literal reading of "host-container ID" — the terminal is the container of
the process — and it is the field that actually distinguishes incarnations:
re-verified live during this step, a server stop/start restored both panes
with identical `pane_id`s (`w1:p1`, `w1:p6`) and *changed both*
`terminal_id`s. Pane coordinates alone would have re-enrolled two dead
runtimes.

`ProcessBirth` needs a second source: `pane.process_info` reports PIDs and
argv but no start time. The default resolver reads `/proc/<pid>/stat` field 22
against `/proc/stat`'s `btime`, tagged `procfs`; anything else is tagged
`unavailable`. `ProcessBirthResolver` is a `Config` field rather than a
build-tagged function so a non-Linux host or a remote Herdr can supply its
own without this package pretending procfs is universal.

The rule that falls out: **unprovable continuity resolves to "not the same
process"**. `sameBirth` returns false unless both sides are procfs-proven and
equal. That over-issues runtime-instance IDs on a host with no start-time
source, which is the safe direction — the opposite error silently merges two
different processes into one identity.

### Discovery reads the pane inventory, never the agent registry

The obvious implementation is `agent.list`: Herdr already knows which panes
hold agents. P7 refutes it. Agent registration drops **durably** when the
agent loses the pane foreground (ctrl-Z, a foreground child, a stopped
process) and never comes back, while the process keeps running — the pane
falls to `agent_status: unknown` and `agent.explain` answers
`agent_not_found`. A registry-driven discovery would therefore lose live
runtimes permanently, with no signal. `Discover` enumerates panes from
`session.snapshot` and leaves "is this an agent runtime, and which one" to
§5.3 correlation, where it belongs. `TestDiscoverEnumeratesPanesNotTheAgentRegistry`
asserts no agent surface is consulted.

### Lifecycle observation: events wake, snapshots decide

Three probed facts force one design. Backfill is partial (a pane restored from
session persistence never replays, and backfill events are shape-identical to
real creations); events between subscriptions are lost with no cursor or
replay token; and agent deregistration can happen spontaneously. So the event
stream carries exactly one bit of information in this adapter — *something
changed* — and every emission is derived from a fresh `session.snapshot` diff.
That is the "snapshot-diff duty" made structural rather than procedural: there
is no code path that could accidentally treat an event payload as inventory,
because no event payload is ever decoded. `TestObserveTreatsBackfillAsAWakeUpNotInventory`
replays two panes that do not exist and asserts silence.

The same reasoning sets the four subscriptions (`pane.created`, `pane.closed`,
`pane.exited`, `pane.updated`) as global rather than pane-scoped: a
pane-scoped subscription cannot report its own pane's disappearance any better
than the snapshot can, and resubscribing whenever the pane set changes (what
the earlier `herdr-orchestrate` probe did) adds gaps rather than removing
them.

`LifecycleDetached` maps to "the server stopped answering", not to a client
detaching: Herdr's client attach/detach is invisible to the API and does not
touch the pane. Reporting an unreachable server as `Exited` would be a claim
about a process nobody can see, so detach is the honest kind, and the next
successful snapshot re-derives attached or exited.

### `revision` is not decoded at all

At 0.8.2 `pane.revision` stopped tracking screen content (visible changes
leave it at zero; only agent-linked transitions bump it). Rather than write
"do not use revision" in a comment, `paneInfo` simply does not declare the
field, and `TestNoRevisionDependence` parses this package's own AST to fail
the build if any identifier or string literal here mentions it. The
writer-presence and prompt/terminal-scope guards work the same way
(`TestNoWriterPresenceSurface`, `TestNoTerminalSynthesisSurface`): writer
presence is refuted and final for 0.8.x, so the absence of that surface is
enforced mechanically rather than trusted to review. The prompt path is
no longer in that guard: `HostPromptProvider` is implemented (below).

### `PrepareLaunch` mutates nothing; `Start` waits for the handover

A Herdr pane cannot be staged — it exists or it does not — so all pane
creation belongs to `Start`, and a launch that fails validation leaves no
orphan pane. `PrepareLaunch` only resolves (kind, agent name, split target vs
new workspace, environment) and returns them in the opaque payload.

`Start` then does something the contract shape does not suggest and the live
check made necessary: it waits, bounded, for the pane's foreground process to
differ from the pre-start baseline. `agent.start` is asynchronous at 0.8.2 —
it returns a `launch_pending` record before the agent process exists — so
evidence read at that instant names the pane's *shell*. Observed live: the
foreground PID changed 10 ms after `Start` returned, and the attachment built
from that evidence failed its own first `ValidateAttachment`. The wait is for
the observable handover only; interactive readiness (`agent.wait`,
`interactive_ready`) is a different surface and out of Stage-1 scope. A
handover that never happens returns the baseline unchanged — weaker evidence,
not an error.

### Launch mapping is lossy, and the environment seam is the reason

Herdr takes no command line for a pane: `pane.split` and `workspace.create`
create a shell, and `agent.start` runs whatever its manifest defines as the
canonical executable for a `kind` (a fixed enum, not a free-form command). So
`ResolvedLaunchTuple.Command` selects the kind by base name (overridable
through `Config.KindByCommand`), and the tuple's absolute path is advisory on
this host.

Which makes `Env` load-bearing, and its limits worth recording. Verified live:
an ordinary variable set through Herdr's `env` map reaches the pane, but
`PATH` set the same way was overwritten by the user's shell startup files, and
Herdr's `env` map cannot unset an inherited variable at all. So the seam
carries the environment (Step 20 chooses the scrub list) while a scrub that
must *remove* a marker has to happen on the Herdr server process before Duo
starts it, or inside the pane command. This matters because the P7 session
reproduced the conformance §8 failure signature through exactly this path: a
Herdr server launched from an agent harness propagates `CLAUDECODE` and
friends into every pane and silently disables Claude Code transcripts.

### Compatibility verdicts

`supported` requires all three of version, protocol, and live schema digest to
match the pin — a drifted digest under an unchanged version number is exactly
the 0.7.5 → 0.8.2 gap, so it downgrades. `unverified` covers unknown versions,
digest drift, and "no schema export available", because *could not check* is
not *does not match* but it is also not verified. `incompatible` is reserved
for a protocol below `MinimumKnownProtocol` (17, the oldest this project has
observed); `New` refuses to build on that and on `unavailable`, but builds
happily on `unverified` — an unverified integration is one Duo records as
unverified, not one it hides.

`ConformanceRecordDigest` names the probe record (`notes/19-herdr-probes.md@2026-08-23`)
rather than a hash, because no digested Herdr conformance-record artifact
exists in this repository yet. When one lands, this constant becomes its
digest.

### Tests: a scripted socket, plus an opt-in live suite

`fakeserver_test.go` is a real Unix socket speaking 0.8.2's wire shapes, not a
transport stub, so the connection-per-request rule, the `agent_pane_busy`
retry, partial backfill, dropped subscriptions, and a server that stops
answering are all exercised end to end.

`live_test.go` runs the same surface against a real server and is skipped
unless `DUO_HERDR_LIVE_SOCKET` names one. It was run for this step against a
disposable server under an isolated `XDG_CONFIG_HOME`, using an agent kind
whose executable resolved to `sleep` rather than a real agent, and it exercised
probe (digest matched, verdict `supported`), launch, discovery, attachment
validation including a stale-terminal rejection, and observation through to a
pane exit.

### The spawn-environment gate lives here, and refuses (Step 22b, 2026-08-23)

The environment seam recorded above is one-directional, so this adapter
consumes `internal/scrub` as a **gate**, not as a scrub:
`internal/host/herdr/scrubgate.go` reads the new pane's pre-agent foreground
process environment between `createPane` and `agent.start`, and a surviving
marker refuses the launch (`*scrub.RefusalError`) and closes the pane.
`PrepareLaunch` separately refuses a launch request whose own `Env` map sets
a marker, before any server call.

Neither of `internal/scrub`'s two scrubbing shapes is reachable on this
path: Stage 1 attaches to a server Duo did not start, so there is no spawn
environment for `Guard` to build, and `agent.start` names a `kind` rather
than a command line, so there is no argv for `PaneCommand` to wrap. An
environment that cannot be read refuses too — `Config.ResolvePaneEnviron` is
a seam for a different source, not an off switch. The full reasoning, the
alternatives weighed, the known exec-time limit of `/proc/<pid>/environ`,
and the test list are in `docs/scrub/decisions.md`'s 2026-08-23 section.

### HostPromptProvider: `agent.prompt` (delegation-loop step 11, 2026-08-26)

Architecture §5.2 only names `PromptPath`. The seal tests need actual
`agent.prompt` I/O and an effect-certainty mapping, so delivery lives on
the same interface as `DeliverPrompt` rather than a companion. Splitting
would force every caller to type-assert a second interface this milestone
always implements together. Attempt-result types stay in `internal/host`
so adapters never import `internal/domain`.

The offer is exact / native / `ComposerSafe: false`. Exact is the path
grade (native complete-turn API, so a min-exact selector still has a host
fallback for Pi). It is not a claim that a success result is effect-
certain. notes/19 §2 verified the collision: `agent.prompt` appends into a
live composer draft and submits the merge with `agent_prompted`. This
adapter does not merge into a human draft and then claim composer-safety;
Herdr has none. Quiet-gate stays a later step.

`DeliverPrompt` sends `{target, text}` with no `wait` object and no retry
loop. Herdr wait is condition evidence, not acknowledgment (notes/19 §3);
a success result alone does not set `Acknowledged`. False success is
verified: until-matching can return while the text sits as an unsubmitted
composer draft.

Effect mapping (notes/19 §2–§3, conformance mapping at notes/19:397-402):

| Host condition | Outcome |
|---|---|
| Dial / pre-write transport failure | `no_effect` |
| `agent_not_ready`, `agent_blocked`, `empty_agent_prompt` | `no_effect` |
| `agent_not_running`, `agent_kind_mismatch`, `agent_not_found` (same pre-delivery family) | `no_effect` |
| No named agent on the pane (lookup, `agent.prompt` not called) | `no_effect` (`agent_not_ready`) |
| `agent_pane_busy` on **agent.start** | pre-delivery refusal, retried in `Start` (existing) |
| `agent_pane_busy` / stall / timeout on **agent.prompt** | `unknown_effect` (a write may already have happened) |
| `agent_prompt_stalled` | `unknown_effect`, never retried (decision-03 §7.1) |
| Transport failure after the request line was written | `unknown_effect` |
| `agent_prompted` | `delivered`, `acknowledged: false` |

`agent.list` is a name lookup for `target`, not an inventory: `Discover`
still enumerates panes from `session.snapshot`. The fake host scripts
`no_effect` / `delivered` (and treats `Disconnect` as `no_effect`) so the
step-14 fake pair can drive both outcomes.

## ConditionProvider (delegation-loop step 07, 2026-08-26)

Stage 1 left `ConditionProvider` unscaffolded so a stub would not lock
field shapes. The delegation-loop observation slice (handoff 25 D2) brings
the interface in. Public streams stay cut: no condition subscribe command.
The adapter method is still §5.3's stream:

```go
ObserveCondition(context.Context, ConditionObservationRequest) (ConditionObservationStream, error)
```

`ReadCondition` was the allowed alternative for a snapshot-only inspect.
It was not taken. The public cut is the subscribe *command*, not the
adapter contract. `session.inspect` reads one snapshot through
`SnapshotCondition` (latest observation available after the first
arrives). A later Stage 2 subscriber consumes the same stream. Transcript-
first adapters (step 08) satisfy the stream by emitting the current
snapshot on open and waiting until `Close`; they do not have to watch
files or install hooks.

Request fields follow `ConversationReadRequest`:
`ExternalAgentSessionID` and `TranscriptID`. Duo `session_id`,
`runtime_instance_id`, and view `revision` stay composer/projection
fields. Observation fields match `$defs.condition_view_data` (`value`,
`confidence`, `freshness`) plus adapter-fillable inspect extras
(`ObservationID`, `EffectiveAt`, `ComputedAt`, `Reasons`). Reasons are
degradation notes; conflict ranking is out of this milestone.

Completion (`exited`) is a closed value on the observation. Mapping
`HostLifecycleSource` process-exit onto `exited` is a caller
(inspect/composer) mapping, not a runtime→host import. The fake seeds
`exited` the same way it seeds `idle`, so later cross-composition tests
have a host+runtime pair without this package importing `internal/host`.

## ConditionProvider adapters (delegation-loop step 08, 2026-08-26)

D2 cuts the credentialed rank-2 reporter, so both dogfood runtimes
derive condition **transcript-first**. Confidence on every observation
is schema `inferred` (`runtime.ConditionConfidenceInferred`), never
`reported`. Correlate's per-adapter labels (`authoritative`,
`pi-extension-exact`, …) stay on the identity bind; they do not leak
into `ConditionObservation.Confidence`, whose vocabulary is closed by
the schema.

`done` is a closed value and is not emitted. Turn-end is idle, not
qualified completion (review/answers/x02 §3). `exited` is also not
emitted by these adapters: `internal/runtime` and the adapter packages
must not import `internal/host`. Step 09's inspect caller maps
`HostLifecycleSource` gone → `exited` as final (I-5). The fake already
seeds `exited` for cross-composition tests.

A missing or unreadable transcript degrades the observation to
`unknown` and does not fail `Correlate`. Bound is identity.

### Claude (`internal/runtime/claude`)

Scan the JSONL (not `ReadConversation`'s projected turns — those drop
`tool_use` / `turn_duration`). Last turn-boundary event wins:

- Human-prompt user entry (string content) opens a turn → `working`.
  Peer-origin prompts still open one: the agent is working. The
  UserPromptSubmit hook is not consulted (notes/16 §5: its payload is
  indistinguishable from a human prompt and must not be treated as
  draft evidence).
- Exact interrupt texts `[Request interrupted by user]` and
  `[Request interrupted by user for tool use]` close the turn → `idle`.
- `stop_reason: tool_use` and tool_result user envelopes keep the turn
  open → `working`.
- `system/turn_duration` (interactive) or `stop_reason: end_turn` /
  `stop_sequence` (print mode, which never writes `turn_duration`)
  settle → `idle`. A snapshot of a finished file treats last `end_turn`
  as settled; there is no silence wait.
- Sidechain lines are ignored. A sidechain-only file is `unknown`.

### Pi (`internal/runtime/pi`)

Scan the JSONL plus the lastSettledAt analog. Pi has no
`turn_duration`; `adapter_pi.go` already defined the edge: an assistant
message whose `stopReason` is anything but `toolUse` ends the turn.
That is the same edge `agent_settled` stamps as `lastSettledAt` on the
reporter claim. This step does not subscribe to `agent_end` (after
`agent_end` pi may still retry, compact, or run a queued follow-up;
`TestExtensionTerminalityAndLifecycleMarkers` stays red if someone
adds that subscription) and does not promote the claim's `idle` /
`lastSettledAt` fields to `reported`.

- User message or `toolResult` or `stopReason: toolUse` → `working`.
- Assistant `stopReason` other than `toolUse` (`stop`, `aborted`, …) →
  `idle`, with `EffectiveAt` the assistant entry's timestamp (the
  lastSettledAt analog). `reported-claim-tui.json` carries the live
  `idle` / `lastSettledAt` pair for the injected-abort session; the
  observation for that transcript is still `inferred`.
- `blocked` stays absent (zero `herdr:blocked` emitters at 0.83.0).

## RuntimePromptProvider delivery (delegation-loop step 12, 2026-08-26)

§5.3 names only `PromptPath`, which does not send. The seal tests need
socket I/O and effect certainty, so this step added `DeliverPrompt` on
the **same** `RuntimePromptProvider` interface rather than a companion.
`PromptPath` still does not dial. Attempt-result types
(`PromptEffect`, `PromptDeliveryRequest`, `PromptDeliveryResult`) live
in `internal/runtime` so adapters never import `internal/domain`.

Claude locates the target instance's inbox from
`~/.claude/sessions/<pid>.json`: `messagingSocketPath` when present,
else `$XDG_RUNTIME_DIR/cc-socks/<pid>.sock` (typical
`/run/user/<uid>/cc-socks/<pid>.sock`). Duo's own
`CLAUDE_CODE_MESSAGING_SOCKET` is some other session's inbox and is
not consulted — send only to the bound instance; hop-chain relay is
not a Duo feature. No live probe.

The frame is newline-delimited JSON, the object notes/10 documented:

```
{"type":"user","message":{"role":"user","content":"<text>"}}
```

Peer wrapping (`isMeta`, security preamble) is accepted. Socket accepts
the frame with the peer still there → `delivered` (exact/native,
`ComposerSafe: true`). Connection loss after write → `unknown_effect`.
Dial failure with no write → `no_effect`. Quiet-gate is the
`internal/delivery` composer (step 13).

Pi does not implement `RuntimePromptProvider`.
`TestExtensionHasNoPromptDeliveryCall` still forbids a delivery call
on the generated extension (notes/18 inject socket stays parked). The
fake runtime compile-asserts the interface with a scriptable
`SeedPromptEffect` stub.

### duo-pi-inject Stage A (2026-08-26)

Pi implements `RuntimePromptProvider` over the per-launch `-e` inject
asset (`internal/runtime/pi/inject/duo-inject.ts`), not the reporter.
`PromptPath` offers exact/native/`ComposerSafe: true` from
`InjectSocketPath(binding.ExternalAgentSessionID)` without dialing or
stat. Path-shaped Herdr identity is reduced via
`SessionIDFromTranscriptName` before naming the socket. Convention:
`$XDG_RUNTIME_DIR/duo/pi-inject/<id>.sock` (fallback
`/run/user/<uid>/duo/pi-inject/<id>.sock`). `DeliverPrompt` dials that
path, drains the connect-line greeting, and writes one frame
`{"text":"<prompt>"}\n`. Effect mapping matches Claude: peer still there
after write → `delivered`; connection loss after write →
`unknown_effect`; dial with no write → `no_effect`. `DUO_PI_SOCK` is an
extension-only listen override at module load; Go locate never reads it.
Abort (`{"abort": true}`, `ctx.abort(`) is out of Stage A.
`TestExtensionHasNoPromptDeliveryCall` still forbids delivery calls on
the reporter asset; delivery lives on the inject asset. Prefer-runtime
via `promptpath.Selector` is unchanged. D3 / `commitIdentityBind` is
unchanged. Stage B idle is the connect-line `idle` field
(`ctx.isIdle()`), not `agent_settled` on this socket.

## Prompt arbitration composer (delegation-loop step 13, 2026-08-26)

`internal/delivery` is the composer between the step-10 command kernel
and the step-11/12 adapters. It is not CLI. Order: revalidate the bound
runtime instance (I-5, never rebind) → draft hold → attributed quiet
period (default 30s) → ready boundary → `promptpath.Selector` →
`CreateAttempt` + adapter `DeliverPrompt` + `CommitDelivered` /
`ReconcileAttempt`. Adapter effect strings (`delivered`, `no_effect`,
`unknown_effect`) are copied onto kernel actions; adapters still do
not import `internal/domain`.

Human-attached cannot be determined on Herdr (notes/19,
`TestNoWriterPresenceSurface`). There is no composer lease and no
writer-presence surface. The D3 carve-out is therefore
**spawn-window-only**: Duo-created is the launch-plan Bind
`recordLaunchAttachments` already writes after Start (`SourceLaunchPlan`
on the attachment correlations). There is no later human-attach signal
to revoke it. Auto-release requires that stamp, launch-settled idle
(Herdr `LaunchSettleTimeout`, default 10s, elapsed since
`instance.StartedAt`), and no caller-supplied draft or human-attached
evidence. An enrolled / unstamped pane holds. `working` and `blocked`
hold. `unknown` does not make the operation unsupported; it prevents
automatic release unless the carve-out applies.

Draft evidence in this slice is caller-supplied positive evidence, not
keystroke attribution and not Claude `UserPromptSubmit` (step 08).
Herdr has no attributed input, so the quiet period cannot fire on
Herdr unless a later composition supplies `LastHumanInput`. The queued
hold code is `prompt.human_priority_hold`. No composer-lease product
surface, no `hold_for_release` verb, no tmux attribution.

## 2026-08-26 — Post-launch identity ingest (handoff 26)

The post-launch identity-bind milestone (handoff 26; not Stage 2) will
ingest host-reported agent-session identity after spawn and present it
as a `RuntimeClaim` for existing `Correlate`, then write through
`Authority.Bind`. Do not install Duo reporter hooks. Do not bind the
newest transcript in a directory.

**Where identity lives (evidence, unresolved contradiction).** notes/07
says `agent_session` surfaces on `pane.get`/`pane.list`. Live Herdr
0.8.2 (notes/19) shows the pane record without `agent_session`; the
agent record adds `{name, agent, agent_session, interactive_ready,
state_change_seq}` and carries `launch_pending`. Prefer the live 0.8.2
record for decode work; do not pretend the notes/07 vs notes/19
contradiction is resolved beyond naming it.

**Correlate is unchanged.** A claim still needs
`ExternalAgentSessionID` (see Correlate above). Host identity on the
claim is evidence for that field; `Correlate` does not discover an id
from a pane on its own.
