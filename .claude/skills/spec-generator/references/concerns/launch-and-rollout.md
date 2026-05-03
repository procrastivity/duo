# Launch & Rollout

## What this concern means

How the feature reaches users — staged rollouts, feature flags, dark launches, beta cohorts, dogfooding plans, kill switches. Distinct from "release engineering" (which is about how code gets deployed); rollout is about how the *behavior* gets exposed to users, and how it gets pulled if it goes wrong.

## When this concern is in scope

- **Pre-enabled by profile**: `public-saas`.
- **Optional**: `internal-tool` (sometimes a flag is warranted, sometimes "deploy on Friday and announce in chat" is enough).
- **JIT triggers**: user mentions "ship," "release," "rollout," "feature flag," "staged," "dogfood," "beta," "behind a flag," "gradual," "kill switch."
- **Off by default for**: `solo-local-cli` (you are the user; rollout = `cargo install`), `library-or-sdk` (rollout = a release; see `library-and-sdk.md` for versioning).

## What to investigate during conversation

- **Is a feature flag warranted?** If the feature is small, reversible, and low-blast-radius, a flag adds ceremony without value. If it's large, behaviorally observable, or could degrade other features, a flag is cheap insurance.
- **What are the rollout stages?** Typical: dogfood (internal users) → beta (opt-in cohort) → 1% / 10% / 50% / 100% (or named segments). Each stage needs a duration and a graduation criterion.
- **What's the rollback procedure?** Concrete steps. If it's "set flag to off and redeploy," that's fine. If rollback requires a database migration, that needs to be explicit because it's no longer "instant."
- **What metrics watch the rollout?** Cross-reference `concerns/analytics.md` if both are in scope — the rollback signal from analytics IS the rollout's go/no-go gate.
- **Who needs to know it's launching?** Support, docs, sales, on-call. A rollout that surprises support is a rollout that generates tickets.

## What rigor applies

- **Every stage names a graduation criterion.** Time-based ("48 hours without P0 issues") or metric-based ("rollback signal stays within ±5% of pre-launch baseline").
- **The rollback procedure is documented in the spec or its appendices**, not left for incident-time discovery.
- **The kill switch is testable.** A flag that's "supposed to" turn off the feature, but has never been exercised, is not a kill switch.
- **Communication plan is explicit.** Names the channel and audience for each stage's announcement.

## Decomposition manifest entry

```markdown
### Launch & rollout work
- Feature flag: `<flag name>` controls `<behavior>`. Default: off. Owner: `<team / human>`.
- Rollout plan: <stage> → <stage> → <stage>. Graduation criteria: <list>.
- Rollback procedure: <numbered steps>. Tested via: <test or runbook>.
- Communication: <stage> → <audience> via <channel>.
```

## Examples of strong vs. weak coverage

**Strong**: "Flag `semantic_search_enabled` (default off) controls the new search mode. Stage 1: dogfood internal team for 1 week, graduate if no P0 issues. Stage 2: 10% of free-tier users for 1 week, graduate if `search_executed` rate stays within ±10% of pre-launch baseline AND error rate stays under 0.5%. Stage 3: 100%. Rollback: set `semantic_search_enabled=false` in the runtime config; takes effect within 60 seconds (config polled every 30s). Rollback tested in staging on 2026-04-20. Notify `#search`, `#support`, and `#oncall` at each stage."

**Weak**: "Use a feature flag and roll it out gradually. Roll back if there are issues."

The strong version names the flag, the stages, the graduation criteria, the rollback mechanics, and the comms. The weak version is a checkbox.
