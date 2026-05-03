# Success Metrics

## What this concern means

How the team will know the feature succeeded — the measurable outcomes that distinguish "this worked" from "this is bit-rot." Distinct from analytics (which is the *instrumentation* that captures data); success metrics are the *targets* the data will be compared against.

## When this concern is in scope

- **Pre-enabled by profile**: `public-saas` (quantitative required).
- **Optional**: `internal-tool` (mixed qualitative + quantitative; depends on whether the feature has measurable behavior), `library-or-sdk` (adoption / DX-focused metrics).
- **JIT triggers**: user mentions a metric, KPI, conversion, "how would we know it's working," "did it succeed," "outcome," "measure."
- **Off by default for**: `solo-local-cli` — qualitative success criteria are accepted ("the feature works the way I want when I use it"). Trigger this concern only if the user spontaneously raises a metric.

## What to investigate during conversation

- **What outcome distinguishes success from non-success?** Press past output metrics ("we shipped it") to outcome metrics ("users do X more often / faster / more reliably").
- **What's the baseline?** Without a baseline, "improved" is meaningless. Sometimes the baseline is "0" because the behavior didn't exist; that's still a baseline worth naming.
- **What's the target?** Specific number with a window. "10% adoption among power users within 30 days of GA" — not "good adoption."
- **How will it be measured?** Cross-reference `concerns/analytics.md` if both are in scope — the metric needs an instrumentation source.
- **What's the guardrail?** At least one metric that should NOT get worse. "Search latency p99 stays under 500ms" while we're optimizing for adoption.

## What rigor applies

### Quantitative profile (`public-saas`)

- **Every primary metric has baseline + target + measurement source + window.** A metric without all four is incomplete.
- **At least one guardrail metric.** Something that should not regress while we move the primary.
- **Outcome metrics, not output metrics.** "Feature shipped" is output; "Median time-to-first-result drops from X to Y" is outcome.

### Mixed profile (`internal-tool`, `library-or-sdk`)

- Quantitative where it's measurable; qualitative ("internal team reports easier debugging") where it isn't.
- The qualitative criterion still needs to name *who* will report it and *what* would count as success.

### Qualitative profile (`solo-local-cli`)

- One sentence is enough: "When I open a vault and search for a term I half-remember, I find the right note in under 30 seconds." The user IS the measurement instrument.
- No baseline / target / window required, but the criterion should still be falsifiable — the user should be able to say "no, this doesn't work yet."

## Decomposition manifest entry

```markdown
### Success metrics
- Primary: `<metric>`. Baseline: `<value>`. Target: `<value>`. Source: `<instrumentation>`. Window: `<duration>`.
- Guardrail: `<metric>`. Acceptable range: `<bounds>`. Source: `<instrumentation>`.
- Qualitative (if applicable): `<criterion>`. Reporter: `<who>`.
- Open question routed to `<analytics / data team>`: `<question>`.
```

## Examples of strong vs. weak coverage

**Strong (`public-saas`)**: "Primary metric: 30-day retention of users who use semantic search at least once. Baseline: 60% (current 30-day retention for paid users). Target: 70% (10pp lift for users who try the feature). Source: `search_executed` events joined to user activity in the analytics warehouse. Window: 30 days post-feature-trial. Guardrail: search latency p99 stays under 500ms. Acceptable range: ±10% of pre-launch baseline."

**Strong (`solo-local-cli`)**: "Success criterion: When I save a new note in `notes/`, I can find it via `hmn search` within 5 seconds of saving (the watcher's debounce window plus indexing time). I'll know this is broken if I have to wait noticeably or if `hmn search` returns stale results."

**Weak**: "We'll know it works when users like it."

The strong versions name the criterion specifically enough to falsify. The weak version is a wish.
