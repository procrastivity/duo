# Business Viability

## What this concern means

The economic and strategic case for the feature: marginal cost per use, pricing model, revenue impact, alignment with the org's commercial strategy. Distinct from "value to the user" (Cagan's value risk) — viability asks whether the *business* can sustain offering the feature, not whether users will adopt it.

## When this concern is in scope

- **Pre-enabled by profile**: `public-saas` only.
- **JIT triggers**: user mentions cost, pricing, monetization, "is this worth doing for the money," "what does this cost to run," "tier," "free vs paid," "margin."
- **Off by default for**: `solo-local-cli` (no commercial concerns), `internal-tool` (typically internal cost is borne by the org as overhead; revisit only if a manager is asking), `library-or-sdk` (open source has different viability concerns; commercial SDKs need this — handle as a JIT trigger if it comes up).

## What to investigate during conversation

- **What's the marginal cost per use?** Compute, storage, third-party API calls, human-in-the-loop. A feature that costs $0.50 in API calls per use cannot be free for unlimited use without a margin model.
- **Which pricing tier(s) does this feature belong to?** Free-tier feature, paid-tier-only, paid-add-on, usage-priced.
- **Is there a usage cap that protects margins?** Per-user-per-day quota, per-org rate limit, hard ceiling at the abuse threshold.
- **What's the strategic case?** Acquisition driver (gets people in the door), retention driver (reduces churn), monetization driver (sells more seats/upgrades), defensive (table-stakes; competitors have it).
- **What's the kill criterion?** Under what business conditions would we *retire* this feature? (Margins thin, low adoption, competitor caught up, strategic pivot.)

## What rigor applies

- **Marginal cost is computed, not guessed.** "Each call to the embedding API costs $0.0001 per 1k tokens; average note is 800 tokens; expected p99 user usage is 50 notes/day; therefore: ~$0.40 per power user per month."
- **Pricing tier assignment is explicit.** "Free tier, capped at 10 searches/day. Paid tier ($X/mo), uncapped." Not "we'll figure out pricing later."
- **Quotas have explicit values.** "10 per day per user, 100 per hour per org, hard fail above 1000 per minute org-wide" — each number tied to a budget rationale.
- **The strategic case is written down.** One sentence, no marketing fluff: "Acquisition driver — the feature is the demo we use in sales calls" or "Retention driver — power users churn at 8%/quarter; this should drop them to 4%."

## Decomposition manifest entry

```markdown
### Business viability
- Marginal cost per use: `<computed value>` (computed in `<location / spreadsheet>`).
- Pricing tier: `<tier(s)>`. Quotas: `<per-user / per-org / hard cap>`.
- Strategic case: `<one sentence>`.
- Kill criterion: `<condition under which we retire>`.
- Open question routed to `<finance / pricing team>`: `<question>`.
```

## Examples of strong vs. weak coverage

**Strong**: "Semantic search calls the embeddings API at index time and at query time. Index-time amortizes ($0.0001 per 1k tokens, ~$0.08 per power user one-time); query-time is per-call (~$0.0002/query). At 100 queries/day per power user, marginal cost is ~$0.60/mo/user. Pricing tier: included in paid tier ($20/mo); free tier capped at 10 queries/day per user. Strategic case: differentiates from grep-only competitors. Kill criterion: marginal cost > $5/mo per power user at observed usage."

**Weak**: "Semantic search costs some money to run. We'll probably charge for it."

The strong version names the unit cost, the cap, the tier, the strategic case, and the kill criterion. The weak version is a placeholder.
