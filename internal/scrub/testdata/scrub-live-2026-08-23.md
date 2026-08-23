# Step 20 live evidence — 2026-08-23

Scope: conformance §8's paired negative/positive spawn-environment test
(terminal-multiplexers `duo-vnext-integration-conformance.md` §8, 2026-08-23
amendment), run against a real, disposable Herdr server and a real `claude`
binary. Referenced from `docs/scrub/decisions.md`.

Versions: herdr 0.8.2 (protocol 20), claude 2.1.241 (installed; the pinned
evidence floor is 2.1.240 per conformance §8's 2026-08-23 amendment —
2.1.241 is the first patch above it and was not separately probed by any
prior handoff). `DISABLE_AUTOUPDATER=1` set on every disposable server per
the pinned-probe convention.

Method: every server below is disposable — its own `XDG_CONFIG_HOME`
(a fresh directory removed on teardown) and a uniquely-labelled
`HERDR_SESSION`, with the ambient `HERDR_SESSION`/`HERDR_SOCKET_PATH` this
shell already carries (it runs inside a live Herdr session itself)
explicitly stripped before each spawn — never inherited by accident. No
live user Herdr server or real project was touched. Each server was
stopped (`server.stop`, then SIGTERM as a backstop) and its scratch
directory removed after use. Disposable Claude Code transcripts land under
the real `~/.claude/projects/<encoded-cwd>/` (there is no supported
override that does not itself collide with a scrub marker — `CLAUDE_CONFIG_DIR`
is one), each under a unique scratch `cwd`, and each was removed after
recording — the same residual-artifact shape terminal-multiplexers
notes/19-herdr-probes.md records as accepted precedent, except here nothing
was left behind.

## §sanity — Claude Code project-directory encoding

Before any Herdr involvement: `claude -p "Reply with exactly the word
SANITYOK and nothing else." --model claude-sonnet-5 --output-format json`
in a scratch cwd. Result: `session_id` `8e637fa4-e58a-4fb8-a64a-64a6d559709a`,
transcript at `~/.claude/projects/-tmp-duo-scrub-sanity-PG2JMz/8e637fa4-e58a-4fb8-a64a-64a6d559709a.jsonl`
for cwd `/tmp/duo-scrub-sanity.PG2JMz`. A second, longer cwd containing
underscores (`/tmp/TestLiveScrubPairedTranscriptLosspositive_leg_Guard-scrubbed_s.../001`)
encoded to `-tmp-TestLiveScrubPairedTranscriptLosspositive-leg-Guard-scrubbed-s..-001`.
Rule derived from both: every character that is not a letter, digit, or
hyphen becomes `-` (`internal/scrub/live_test.go`'s `encodeClaudeProjectDir`).
Both scratch transcripts and directories were removed after recording.

## §0 — Manual wire-level walkthrough (before the Go test existed)

Established the mechanics `internal/scrub/live_test.go` later automates,
and surfaced two findings that changed this package's design:

1. **Herdr has no `pane.run` wire method.** The full method list at 0.8.2
   (91 methods) has no method that spawns a process in a pane from a
   structured argv. The CLI convenience `herdr pane run <PANE_ID>
   <COMMAND>...` is documented as "sends text and Enter in one call" and
   is built on `pane.send_text` (one string param) + a key press. A
   multi-word `COMMAND` element (e.g. this package's `sh -c '<script>'`)
   does **not** survive that path as one token — verified live: passing
   `env -u ... sh -c '<script with spaces>' duo-scrub claude` through
   `herdr pane run` typed each word separately into the pane's shell and
   produced a syntax error (`bash: syntax error near unexpected token
   'do'`). This is why `PaneCommand` (shape (b)) returns a single,
   individually-quoted shell command **line**, not an argv slice — see
   `docs/scrub/decisions.md`.
2. **The transcript-loss signature is invocation-shape-specific, not just
   version-specific.** A `claude -p` (headless one-shot) spawn through a
   marker-carrying Herdr pane wrote a transcript normally, every time it
   was tried (three separate attempts, real and full marker sets both).
   Only an **interactive** claude session in the same pane showed the
   "Transcript saving is off" warning and produced no transcript. See the
   comparison table below.

### Negative-leg invocation-shape comparison (marker-carrying disposable server)

| Attempt | Invocation | Markers present | Result |
|---|---|---|---|
| 1 | `claude -p` via `herdr pane run` (prompt had spaces; only the first word "Reply" survived the pane-run word-splitting — see finding 1 above) | synthetic (`CLAUDECODE=1`, `CLAUDE_CODE_CHILD_SESSION=parent-marker-sess`, `AI_AGENT=1`) | Transcript written (`d02e8a23-…jsonl`) |
| 2 | `claude -p SAYNEGLEGSIGNATURE` (single-token prompt, no splitting issue) via `herdr pane run` | synthetic, same as above | Transcript written (`40d00757-…jsonl`) |
| 3 | interactive `herdr agent start` + `herdr agent prompt` | synthetic, same as above | **No warning, transcript written** (`8bf15f06-…jsonl`) — synthetic markers alone did not reproduce the signature |
| 4 | `claude -p SAYNEGLEGPMODE` via `herdr pane run` | **full real marker set** inherited from this session's own live Claude Code process (`CLAUDECODE=1`, `CLAUDE_CODE_CHILD_SESSION=1`, `AI_AGENT=claude-code_2-1-241_agent`, `CLAUDE_CODE_SESSION_ID`, `CLAUDE_PID`, `CLAUDE_CODE_MESSAGING_SOCKET`, `CLAUDE_CODE_MESSAGING_TOKEN`, `CLAUDE_CODE_BRIDGE_SESSION_ID`, `CLAUDE_CODE_ENTRYPOINT`, `CLAUDE_EFFORT`) | Transcript written (`73b07012-…jsonl`) — confirms finding 2: `-p` mode is not susceptible even with the authentic marker set |
| 5 | interactive `herdr agent start` + `herdr agent prompt` | full real marker set (same as attempt 4) | **"⚠ Transcript saving is off — inherited CLAUDE_CODE_CHILD_SESSION marker" shown; no transcript directory created at all** — the loss signature, reproduced |

Attempt 3 (synthetic markers, interactive, no loss) versus attempt 5 (full
real markers, interactive, loss) shows the synthetic marker set used in
attempt 3 was insufficient by itself — plausibly because Claude Code's
detection checks more than presence of the two named markers (e.g. a live,
reachable parent via `CLAUDE_PID`/`CLAUDE_CODE_MESSAGING_SOCKET`), not
because interactive-vs-`-p` was the only variable. `internal/scrub`'s
Markers list still targets exactly the markers conformance §8 and this
project's own live session actually export (`CLAUDECODE`,
`CLAUDE_CODE_CHILD_SESSION`, `AI_AGENT`, plus the `CLAUDE_*` wildcard); the
automated live test (below) carries a representative subset of the real
set for the negative leg, which was sufficient to reproduce the signature.

### Shape (b) supplementary check (manual, wire-level)

On the same marker-carrying disposable server, a fresh pane was started
directly via `PaneCommand`'s shape (the corrected, single-line form) typed
through `herdr pane run`/`pane.send_text` + `pane.send_keys enter`:
`env -u CLAUDE_CODE_CHILD_SESSION -u CLAUDECODE -u AI_AGENT sh -c
'<wildcard sweep>' duo-scrub 'claude'`. No warning appeared; a submitted
turn produced exactly one transcript (`cded7203-…jsonl`). This validates
shape (b) — the pane-command wrapper — independently of shape (a), on the
same marker-carrying server the shape (a) positive leg below does not use.

## §1 — Automated paired test (`go test ./internal/scrub/... -run
TestLiveScrubPairedTranscriptLoss`, `DUO_SCRUB_LIVE=1`)

Run twice while developing the test (first run to reach a working
implementation, second to confirm the fix for the transcript-directory
encoding bug below); the second run is the recorded result. Both runs used
real, disposable resources; all were torn down.

**Design note on invocation shape:** this automated test uses *interactive*
claude sessions for both legs (`agent.start` + `agent.wait` + `agent.prompt`),
not `claude -p`, per §0's finding 2 above — a `-p` spawn does not reproduce
the loss signature at the installed version regardless of scrub state, so
it cannot serve as the negative leg's positive control. This is a
deliberate, evidence-driven departure from the Step 20 prompt's "`claude
-p`" framing, recorded here rather than silently substituted.

### Negative leg: marker-carrying server, interactive spawn — PASS

- Server spawn environment: real ambient markers from this project's own
  live session (`CLAUDECODE=1`, `CLAUDE_CODE_CHILD_SESSION=1`,
  `AI_AGENT=claude-code-live-scrub-test`, `CLAUDE_CODE_ENTRYPOINT=cli`),
  plus `HOME`, `PATH`, `TERM`, `DISABLE_AUTOUPDATER=1`. `scrub.Verify`
  against this environment (logged, not asserted — the negative leg is
  *supposed* to carry markers) reported all four present.
- Session id: `085d7ea2-f63c-4405-975a-721d266bb644`.
- Screen text after the turn contained `"Transcript saving is off"`: **true**.
- Transcripts found under the pane's scratch project directory: **0**.
- Verdict: the failure signature reproduced.

### Positive leg: `scrub.Guard(os.Environ())`-scrubbed server, interactive spawn — PASS

- Server spawn environment: `scrub.Guard(os.Environ())`'s output (every
  marker removed and re-verified before use) plus `HOME` and
  `DISABLE_AUTOUPDATER=1`. `scrub.Verify` on the exact environment handed
  to `exec.Command` passed (no error) before the server was even started —
  the fail-closed check the launcher is required to perform.
- Session id: `08151d74-1b1a-483b-9c5a-f1b9856c8104`.
- Screen text after the turn contained `"Transcript saving is off"`: **false**.
- Transcripts found under the pane's scratch project directory: **1**
  (`08151d74-1b1a-483b-9c5a-f1b9856c8104.jsonl` — the transcript's filename
  names the exact fresh session id Herdr reported for the same pane).
- Verdict: SessionStart bound the exact runtime instance and conversation
  normalization would see exactly one transcript, per conformance §8 step 6.

### Mechanical bug found and fixed while automating this

The first automated run's positive leg reported 0 transcripts despite a
real turn completing. Cause: `encodeClaudeProjectDir` (this test's mirror
of Claude Code's cwd→project-directory encoding) only replaced `/` and
`.`; the real encoding also replaces `_` (and, by the general rule §sanity
above derives, every non-alphanumeric, non-hyphen character). Fixed before
the second, passing run. Also fixed in the same pass: a Unix domain socket
path-length overflow (`t.TempDir()`'s nested subtest-name path exceeded
`sun_path`'s ~108-byte cap once `/herdr/sessions/<session>/herdr.sock` was
appended — switched to a short `os.MkdirTemp("", "duo-scrub-")`), and a
duplicate-environment-variable hazard (`Guard(os.Environ())` carries this
process's own ambient `HERDR_SESSION`/`HERDR_SOCKET_PATH` through
unchanged — Guard only scrubs the `CLAUDE_*`/`CLAUDECODE`/`AI_AGENT`
markers — so appending a fresh `HERDR_SESSION` without first stripping the
ambient one produced two entries in `exec.Cmd.Env`; the disposable
positive-leg server it started on the first buggy run attached to *this
repository's own live Herdr session* instead of a fresh one, reported as
`"herdr server is already running"`. Fixed by stripping
`XDG_CONFIG_HOME`/`HERDR_SESSION`/`HERDR_SOCKET_PATH` from the caller-supplied
environment before appending the disposable values.)

## Debt

None: both `herdr` (0.8.2) and `claude` (2.1.241) were available, and both
legs of the automated test passed. The one open item is evidentiary, not a
gap in this step's own work: `claude -p`'s non-susceptibility to the
inherited-marker signature (§0 findings 2, attempts 1/2/4) is new,
version-pinned evidence that narrows conformance §8's assumption beyond
what notes/16 and notes/19 established (they only tested interactive
sessions through Herdr panes). It is not this step's place to edit the
conformance document; it is recorded here and in `docs/scrub/decisions.md`
for the review program to fold in.
