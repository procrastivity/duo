# Pi adapter fixtures — provenance

Every transcript here is a real pi session file, unmodified, captured from a
disposable probe session. Filenames keep pi's own on-disk naming
(`<ISO timestamp with - for : and .>_<uuidv7>.jsonl`) behind a descriptive
prefix, because the file name is load-bearing: its second component is the
session id the adapter resolves and re-checks.

## Transcripts

- `basic-with-resume_2026-08-08T18-52-22-125Z_019fe2b8-….jsonl`
  Captured 2026-08-08 against pi 0.83.0. A throwaway `pi -p` run (user →
  assistant toolCall → toolResult → assistant "DONE") followed by a second
  `pi -c -p` run in the same cwd. Shows that `--continue` appends to the
  *same* file with no new header, which is what makes a line-count cursor
  valid across a resume. Complete file, unmodified.
  Source: `apps/transcript-tail/fixtures/pi/` in the research repo
  (terminal-multiplexers), where it is a pinned adapter fixture.

- `forked_2026-08-08T18-53-06-019Z_019fe2b8-….jsonl`
  Captured 2026-08-08 against pi 0.83.0, from `pi --fork 019fe2b8 -p …`. New
  file, new session id, header carries `parentSession`; every parent entry is
  *copied* in with its original entry id, then the new pair is appended.
  Complete file, unmodified. Same research-repo source.

- `injected-abort_2026-08-23T00-53-45-631Z_01a02c1b-….jsonl`
  Captured 2026-08-23 against pi 0.83.0 by the P6-Pi probe
  (`notes/18-opencode-pi-refresh.md` §2 and §5 — this is the session those
  sections quote by id). It holds, in one file: a prompt delivered natively
  through the extension socket, persisted as an ordinary `user` entry
  (injected turns are indistinguishable on the transcript channel); an
  assistant entry with a `thinking` block and a text refusal; an assistant
  entry with `thinking` + `toolCall` and no text; a `toolResult` with
  `isError: true` and text "Command aborted"; and a final assistant entry
  with zero content blocks and `stopReason: "aborted"`. Complete file,
  unmodified, disposable probe content (no secrets; the token probe's output
  is in a different session file, deliberately not copied here).

The 2026-08-08 pair was re-verified against 0.83.0 on 2026-08-23: the
research repo's `transcript-tail` adapter, from which this package's parser
is ported, still parses both unchanged (notes/18 §1).

## Reporter claim

- `reported-claim-tui.json`
  **Constructed, not captured.** The field *values* are the ones the probe's
  extension read live from `ctx` at 0.83.0 (session id, session file, cwd —
  notes/18 §5), and the token is synthetic. The wire framing —
  `duo.pi.reporter/v1` and this field set — is this package's own, defined in
  `claim.go` and written by `extension/duo-pi-reporter.ts`; the probe
  extension had its own ad-hoc shape. The `sessionFile` path is the historical
  probe path and does not exist on a test machine, which is deliberate: it
  exercises the resolver's fallback from a stale reported path to the sessions
  tree.
