# Decisions — 2026-08-24

Config authored at `~/.config/duo/duo.config.yaml` (`XDG_CONFIG_HOME`
unset, fallback path). Binary probed: `duo version 53ddced (commit
53ddced, built 2026-08-24T17:24:27Z)` — Go build, installed as `duo` at
`~/.local/bin/duo` (no `duo-go` alias on this machine). Mode: **live**
(Herdr session `duo` running, server env verified clean of
agent-harness markers via /proc/<pid>/environ; claude 2.1.241 and
pi 0.83.0 both on PATH).

## Variant roster

| variant | runtime | model_line | model_family | provider | arguments |
|---|---|---|---|---|---|
| claude_fable | claude | fable-5 | claude | anthropic | `--model fable` |
| claude_opus | claude | opus-5 | claude | anthropic | `--model opus` |
| claude_sonnet | claude | sonnet-5 | claude | anthropic | `--model sonnet` |
| claude_haiku | claude | haiku-4.5 | claude | anthropic | `--model haiku` |
| pi_wyvern | pi | qwen-3.6 | qwen | x-es | `--provider x-es --model anthropic/Wyvern` |

- Claude roster: user confirmed "same as example" — four lines selected
  by `--model <alias>`, family label `claude` covering the whole
  Anthropic subscription as one exclusion group.
- Pi roster: exactly one variant. Arguments given verbatim by the user:
  `--provider x-es --model anthropic/Wyvern`. Family label **`qwen`**
  and line label **`qwen-3.6`** are the user's hand-authored labels
  (per the hard rule, labels are never inferred from the model string —
  the `anthropic/Wyvern` model name does not drive either label; the
  user names the family after the underlying model lineage as they
  think about exclusions).
- No base `arguments` on either runtime (user gave none).

## Provider tags

| tag | subscription | variants |
|---|---|---|
| anthropic | Anthropic (Claude Code) | claude_fable, claude_opus, claude_sonnet, claude_haiku |
| x-es | the user's x-es pi provider | pi_wyvern |

`duo provider disable anthropic` turns off all four claude variants at
once; `disable x-es` turns off the only pi variant.

## Presets

| preset | leaves | rationale |
|---|---|---|
| orchestrator | main: [claude_fable] | single pane, top model, no fallback |
| builder | main: [claude_sonnet, pi_wyvern] | wyvern is the fallback slot the example gave gpt-5.6-luna |
| reviewer | main: [claude_opus] | user: only luna's former slots keep multiple candidates, so the example's gpt-5.6-sol fallback is dropped, not substituted |
| build_and_verify | builder: [claude_sonnet, pi_wyvern]; verifier: [claude_sonnet]; relation distinct_model_family(builder, verifier) | user's exact shape: "I definitely do not want pi wyvern to be a verifier over sonnet" |

**Deliberate consequence, confirmed at dry-run:** in
`build_and_verify`, the verifier leaf is pinned to family `claude`, so
the distinct_model_family relation forces the builder leaf onto
`pi_wyvern` every time. Sonnet always verifies, Wyvern always builds —
builder's sonnet-first candidate order is real but unreachable while
the relation stands. This matches the user's stated intent (never
Wyvern as verifier); flagged here so the orchestrator knows it is by
design, not an authoring slip.

## Not expressible

- "Prefer sonnet as builder but only when the verifier can be
  non-claude" — with a single non-claude variant, candidate preference
  cannot express this; the relation overrides builder's order (see
  above). Accepted by the user.
- Nothing else requested fell outside duo.config/v3.
