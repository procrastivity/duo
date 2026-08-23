# Scrub decisions

Normative sources: `duo-vnext-integration-conformance.md` §8 (spawn
environment policy and transcript-loss test, including its 2026-08-23
amendment) and `review/05-close-report.md` §1 in the planning repository;
`docs/adapters/decisions.md`'s Herdr host adapter section (Step 17) for the
environment-seam limits this package works around; and the live evidence in
`internal/scrub/testdata/scrub-live-2026-08-23.md`, gathered while writing
this package.

## One deny list, in `markers.go`

`Markers` (`CLAUDE_CODE_CHILD_SESSION`, `CLAUDECODE`, `AI_AGENT`) and
`WildcardPrefixes` (`CLAUDE_`) are the entire scrub policy. Everything else
in this package — `Environ`, `Verify`, `Guard`, `PaneCommand` — is derived
from those two slices, never a second copy of a name.
`TestNoDuplicateMarkerLiteralsOutsideScrub` (`singlesource_test.go`) is
`internal/registry`'s `TestNoDuplicateOperationTableOutsideRegistry`
tripwire pattern applied here: it walks every `.go` file outside
`internal/scrub`, greps for the exact quoted marker literals, and fails the
build on any match. Unlike registry's operation names (which tolerate one
or two legitimate call-site mentions before they read as a duplicate
inventory), this tripwire is zero-tolerance — nothing outside this package
has a legitimate reason to spell `"CLAUDECODE"`, `"AI_AGENT"`, or
`"CLAUDE_"` as a literal, so any occurrence at all is a duplicate of the
one place this policy lives.

`AI_AGENT` is deliberately an *exact-name* marker, not a wildcard prefix:
`AI_AGENT_FOO` is not scrubbed unless a future change adds an `AI_AGENT_`
entry to `WildcardPrefixes` (`TestAIAgentExactNameOnly` documents this as a
choice, not an oversight — the Step 20 spec names `AI_AGENT` as a single
generic flag, and widening its scope silently would scrub variables no
runtime has ever been observed to set).

## Two shapes, because Herdr's environment seam has one irreversible half

Verified live at Step 17 (`docs/adapters/decisions.md`, Herdr section,
"Launch mapping is lossy, and the environment seam is the reason"): a
Herdr pane inherits the *server's* environment, and Herdr's
`workspace.create`/`pane.split` `env` map can only **set** a variable in
the pane's shell — it cannot **unset** one the pane already inherited. So
there are exactly two sound places to remove a marker for a Herdr-hosted
spawn, and this package expresses both:

- **Shape (a), `Guard`** — scrub the spawn environment *before* something
  starts: a direct child process, or a Duo-owned Herdr server (whose
  panes then inherit an already-clean environment). `Guard` scrubs
  (`Environ`) and immediately re-verifies its own output (`Verify`)
  before returning it, so a future bug in the scrub logic becomes a
  refused launch — `Guard` returns `(nil, error)` — instead of a leak.
  This is the fail-closed entry point conformance §8 requires: nothing in
  this package logs a warning and continues past a surviving marker.
- **Shape (b), `PaneCommand`** — when Duo does not own the server's
  launch (attaching to a server someone else started, which still
  carries markers), the only remaining place to remove one is inside the
  pane's own command, before the real command execs.

## `PaneCommand` returns a shell **line**, not an argv — a live finding, not a starting assumption

The original design (mid-implementation) returned `[]string` — an argv —
on the assumption that whatever transport carries "a command for a pane"
would preserve argument boundaries the way `exec.Cmd.Args` does. A live
probe against a real Herdr 0.8.2 server refuted that: the full 91-method
wire vocabulary has **no method that spawns a process in a pane from a
structured argv**. The CLI convenience `herdr pane run <PANE_ID>
<COMMAND>...` (the only "run this in a pane" surface) is documented as
"sends text and Enter in one call" and is built entirely on
`pane.send_text` — one string parameter — plus a key press. Verified live:
passing this package's `env -u ... sh -c '<script>' duo-scrub claude`
argv through that path typed each word as a separate space-delimited
token into the pane's interactive shell, which broke the `-c` script's
internal spaces apart and produced a shell syntax error
(`internal/scrub/testdata/scrub-live-2026-08-23.md` §0, finding 1).

There is no argv-preserving boundary to design against. `PaneCommand`
therefore returns a single POSIX shell command line, with every token
after the outer `env -u ...` individually single-quoted
(`'\''`-escaped) so the pane's own shell reconstructs the same argument
boundaries `PaneCommand` started with, regardless of what a command or
argument contains — including a value with spaces, or one an attacker
fully controls. `shellQuote` uses single quotes specifically because they
are the only POSIX quoting form with no interior exceptions: no character
inside them is special, not even backslash. `TestShellQuoteRoundTripsThroughSh`
proves this against `sh` itself, including injection-shaped inputs
(`$(echo injected)`, `; rm -rf /`, backticks), and
`TestPaneCommandActuallyScrubsAtExecTime` proves the whole generated line
survives a real `sh -c <line>` evaluation with a multi-word argument
intact and every marker gone.

## The wildcard is a live sweep, not a snapshot

`env -u NAME` (used for the three exact-name `Markers`) needs a name in
hand at the point `PaneCommand` is called. `CLAUDE_*` cannot be handled
that way: the set of `CLAUDE_`-prefixed variables a harness exports has
already changed across the Claude Code versions this project has tracked
(`CLAUDE_CODE_MESSAGING_TOKEN` and `CLAUDE_CODE_BRIDGE_SESSION_ID`, both
absent from earlier captures, showed up in this step's own live
environment — see the negative-leg marker list in
`testdata/scrub-live-2026-08-23.md`), so it is not enumerable once and for
all. `wildcardSweepScript` instead builds a small POSIX `sh` fragment that
lists the *pane's own, actual* environment at exec time
(`env | sed -n 's/^\(CLAUDE_[^=]*\)=.*/\1/p'`) and unsets whatever it
finds, then `exec`s its positional parameters. This is strictly more
correct than a caller-supplied snapshot: it is right even when the caller
building the command line has no visibility at all into what the target
environment actually carries, which — for a pane inheriting a server
process Duo may not have started — is exactly the common case. `sed`, not
a shell builtin, because POSIX `sh` has no portable "list names by
prefix" primitive and bash's `${!prefix@}` is not guaranteed on every
`/bin/sh` a Herdr pane might run (notably Debian/Ubuntu's default `dash`).

## Fail-closed is a real refusal path, not a slogan

`Verify` is the single primitive both the direct-spawn shape and (in
spirit) the pane-command shape answer to: it returns a non-nil error
naming every marker still present, and a caller — the launcher, not built
in this step — must treat that as a refused launch. `Guard` demonstrates
what "fail closed" costs in practice: it does not trust `Environ`'s
output, it re-verifies it. Because `Environ` and `Verify` share `IsMarker`
as their only source of truth, they can never actually disagree today —
so `TestGuardRefusesWhenItsOwnScrubStepIsIncomplete` swaps in a
deliberately broken scrub step (a package-private `environFunc` variable,
test-only) to prove the re-verification fires rather than being a no-op
that happens to never trigger.

## Live evidence: the transcript-loss signature is shape-specific, not just version-specific

The Step 20 prompt's plan text says the negative leg spawns `claude -p`.
A live walkthrough (`testdata/scrub-live-2026-08-23.md` §0, three separate
attempts, both a synthetic and the full real marker set) found that a
`claude -p` (headless one-shot) spawn through a marker-carrying Herdr pane
does **not** lose its transcript at the installed version (claude
2.1.241) — only an **interactive** claude session in the same pane does,
showing the exact `"Transcript saving is off — inherited
CLAUDE_CODE_CHILD_SESSION marker"` warning and producing no transcript at
all. This is new evidence, not a contradiction of conformance §8: §8's
2026-08-23 amendment already frames the Herdr-pane signature as
"version-pinned evidence, not a universal law," and every prior probe of
it (notes/16, notes/19) used interactive sessions, never `-p`. The
automated live test (`live_test.go`) therefore uses interactive sessions
for both legs — the shape that actually exercises the hazard — and this
departure from the plan text's literal `-p` framing is recorded here
rather than silently substituted. The `-p` finding itself is flagged as
open evidentiary debt for the review program in
`testdata/scrub-live-2026-08-23.md`'s Debt section; this step does not
edit the conformance document.

## Live paired test: opt-in, real resources, full teardown

`TestLiveScrubPairedTranscriptLoss` (`live_test.go`) is gated on
`DUO_SCRUB_LIVE=1` because it spawns `claude` (real API turns, real cost)
and a disposable `herdr server` process. It follows
`internal/host/herdr/live_test.go`'s isolation shape — its own
`XDG_CONFIG_HOME` and a uniquely-labelled `HERDR_SESSION` per server — with
one addition the plain isolation shape does not need: this process's own
ambient `HERDR_SESSION`/`HERDR_SOCKET_PATH` (real values, because this
repository's own Claude Code session runs inside a live Herdr pane) are
explicitly stripped before every disposable spawn, never left to collide
with the fresh ones this test sets. A first attempt at automating this
found exactly that collision live: `Guard(os.Environ())` correctly leaves
`HERDR_SESSION` alone (it is not a scrub marker), so appending a fresh
`HERDR_SESSION=...` without first removing the ambient one produced a
duplicate entry in `exec.Cmd.Env`, and the disposable "positive leg"
server attached to this repository's own live session instead of a fresh
one (`"herdr server is already running"`). Fixed by stripping
`XDG_CONFIG_HOME`/`HERDR_SESSION`/`HERDR_SOCKET_PATH` from the caller
environment before appending the values this test controls
(`withoutNames` in `live_test.go`).

The test talks to Herdr over a hand-rolled NDJSON client local to
`live_test.go`, not `internal/host/herdr`'s unexported one: the Step 20
boundary is read-only access to that package, and the wire shape
(`{"id","method","params"}` request, `{"id","result","error"}` response,
one connection per request) is exactly what `internal/host/herdr/wire.go`
documents and what the manual §0 walkthrough exercised by hand first.

Both legs — negative (marker-carrying server, interactive spawn: loss
signature observed) and positive (`Guard`-scrubbed server, interactive
spawn: no warning, exactly one transcript naming the fresh session id) —
passed on the recorded run. Full session ids, screen-text findings, and
the two mechanical bugs found and fixed while automating this (a Unix
domain socket path-length overflow from `t.TempDir()`'s nested subtest
path, and a project-directory encoding bug — Claude Code replaces every
non-alphanumeric, non-hyphen character, not just `/` and `.`) are in
`testdata/scrub-live-2026-08-23.md`.
