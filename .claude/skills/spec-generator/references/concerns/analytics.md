# Analytics & Instrumentation

## What this concern means

Whether and how the feature emits telemetry — events, counters, traces — that lets the team answer questions like "is this being used," "is it working as intended," and "what's degrading." Distinct from logging (which is for diagnosing failures); analytics is for understanding behavior and outcomes.

## When this concern is in scope

- **Pre-enabled by profile**: `public-saas` only.
- **JIT triggers**: user mentions a metric, KPI, conversion, "how would we know it's working," "are people actually using this," dashboards, A/B testing, or anything that implies measuring user behavior.
- **Off by default for**: `solo-local-cli` (no real users to instrument; the user IS the operator and observes by direct use), `internal-tool` (analytics often added later, not at spec time), `library-or-sdk` (SDK consumers instrument themselves).

## What to investigate during conversation

- **What question would the metric answer?** Press for the decision the data informs. "Track usage" is not a question; "Did adoption of the new search mode flatten or grow over the first two weeks?" is.
- **What events does the feature need to emit?** Each event needs a name, a set of properties, and a clear emission point.
- **What's the privacy / PII boundary?** Especially for `public-saas`: what fields are safe to log, what gets hashed, what doesn't get captured.
- **What dashboards or queries does the team need at launch?** A spec that defers all dashboard work to "later" rarely produces them.
- **What's the rollback signal?** What metric, if it moves the wrong way, indicates the feature is causing harm and should be disabled?

## What rigor applies

- **Every event must have a documented schema.** Property names, types, allowed values. Same discipline as the spec's Data Schema section.
- **Every event must have a documented emission point** in Implementation Notes. The implementer should not have to guess where the event fires.
- **Privacy decisions are load-bearing.** If a property could contain PII, the spec must state explicitly whether it's hashed, redacted, or excluded.
- **At least one rollback signal** must be named — the metric whose movement would justify pulling the feature.

## Decomposition manifest entry

Add to the manifest under "New CLI / config to add to `docs/reference/`" if the feature exposes new analytics-config knobs. Add separately:

```markdown
### Instrumentation work
- New events: `<event_name>` with properties `<list>`. Emission point: `<spec section / module>`.
- New dashboard: `<dashboard name>` answering `<question>`. Owner: `<team / human>`.
- Rollback signal: `<metric>` — pull the feature if `<threshold>` is breached for `<window>`.
```

## Examples of strong vs. weak coverage

**Strong**: "Emit `search_executed` event on every search request with properties `{mode: filesystem|content|semantic, query_length: int, result_count: int, latency_ms: int}`. Query string is NOT captured. The dashboard `search-adoption` answers 'what fraction of requests use the new semantic mode in week 2?'. Rollback signal: `search_executed` count drops by >40% week-over-week (suggests we broke something rather than gained adoption)."

**Weak**: "Track search usage so we can see if people use the new feature. Add some metrics. Build a dashboard."

The strong version names the events, names the question, names the rollback signal, and respects privacy. The weak version is a TODO disguised as a requirement.
