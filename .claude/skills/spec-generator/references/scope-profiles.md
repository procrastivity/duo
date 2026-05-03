# Scope Profiles & JIT Triggers

The skill asks one Phase 0 question — *"What's the rough shape of the project this spec is for?"* — and the answer drives investigation defaults for the rest of the session. This file documents the four profiles, the concern-to-investigation matrix, and the just-in-time trigger table.

**Important: profiles do NOT toggle spec sections.** Every spec emits all 10 sections of `docs/specs/_template.md`. Profiles control:

- Which **concerns the skill investigates** during conversation (which questions the skill presses on)
- Which **`concerns/<name>.md`** files the skill loads from `references/concerns/`
- The **rigor** applied to acceptance criteria in the user-stories peer artifact
- What surfaces in the **decomposition manifest** (e.g., Analytics → manifest item, not a spec section)

---

## Profile Definitions

### `solo-local-cli`

Single-user local tools. The user is also the operator and the only stakeholder. No tenancy, no multi-user concerns, no rollout, no GTM, no business case beyond "this is useful to me."

Examples: Hypomnema, personal scripts, a CLI you wrote for your own workflow, a local-only desktop tool with no telemetry.

Pre-enabled concerns: feasibility (always on for any spec). Everything else is off by default unless a JIT trigger fires.

### `internal-tool`

Small team / internal-only product. Multiple users (typically 2-50), single org, no external customers. May have light tenancy (per-user data, but no external trust boundary). Rollout is typically "deploy and announce in the company chat."

Examples: an internal admin dashboard, a team-wide CLI for a shared workflow, an HR app, an internal API gateway.

Pre-enabled concerns: feasibility, usability, success metrics (mixed qualitative/quantitative). Multi-tenancy is *optional* — gated on whether the feature touches per-user state in a way that could leak across users.

### `public-saas`

Externally-facing, multi-tenant, paid or free public product. Real attackers, real customers, real revenue or adoption stakes. Tenancy is non-negotiable. Rollout, analytics, and business viability are first-class.

Examples: a B2B SaaS app, a developer platform, any product where customers expect SLAs.

Pre-enabled concerns: feasibility, usability, value, business viability, multi-tenancy, success metrics (quantitative required), analytics, launch & rollout.

### `library-or-sdk`

Code intended for other developers to consume. The "user" is the integrator, not an end user. Tenancy is N/A in the traditional sense (the SDK doesn't manage users); but DX, API contract stability, versioning, and deprecation policy are critical.

Examples: an open-source library, an SDK for an API, a developer tool published to a package registry.

Pre-enabled concerns: feasibility, library-and-sdk (DX + versioning + deprecation), success metrics (adoption / DX-focused).

---

## Concern-to-Investigation Matrix

Each row is a concern. Each cell is the active stance for that concern under that profile. `on` means investigate by default; `off` means do not press unless a JIT trigger fires; `optional` means ask the user once whether the concern applies.

| Concern | `solo-local-cli` | `internal-tool` | `public-saas` | `library-or-sdk` | File to load when active |
|---|---|---|---|---|---|
| Feasibility | on | on | on | on | (covered by Phase 1 LDS research; no separate file) |
| Usability | off | on | on | on | (covered by user-story-guide; no separate file) |
| Value (will users adopt?) | off | optional | on | optional | (no separate file; press in conversation) |
| Multi-tenancy / personas / adversarial ACs | off | optional | on | n/a | `concerns/multi-tenancy.md` |
| Success metrics (quantitative) | off | optional | on | optional | `concerns/success-metrics.md` |
| Analytics / instrumentation | off | off | on | off | `concerns/analytics.md` |
| Launch & rollout | off | optional | on | n/a (versioning instead) | `concerns/launch-and-rollout.md` |
| Business viability (cost/pricing) | off | off | on | off | `concerns/business-viability.md` |
| Library/SDK concerns (DX, versioning, deprecation) | n/a | n/a | optional | on | `concerns/library-and-sdk.md` |

**`optional` means: ask the user once at start of Phase 2** ("Does this feature touch multi-user state that could cross user boundaries? If yes, I'll pull in the multi-tenancy guide."). Single yes/no. If yes, load the file and press. If no, drop it for the rest of the session.

---

## Acceptance-Criteria Rigor by Profile

The user-stories peer artifact applies different defaults for each profile. The full discipline lives in `references/user-story-guide.md`; this is which slices of it are mandatory:

| AC discipline | `solo-local-cli` | `internal-tool` | `public-saas` | `library-or-sdk` |
|---|---|---|---|---|
| Observable from outside the database/store | required | required | required | required |
| Discriminating (would-it-pass-with-a-constant) | required | required | required | required |
| Negative-fingerprint greps for anti-patterns | required | required | required | required |
| Boundary-graph hops enumerated for cross-module values | required | required | required | required |
| Adversarial / cross-tenant denial criteria | off | conditional (if multi-tenancy is on) | required | n/a |
| API contract tests (semver-stable surface) | n/a | n/a | optional | required |

The first four are language-agnostic correctness work and apply to every profile. The last two are profile-gated.

---

## JIT Trigger Table

When a signal appears in the conversation, make a single yes/no offer to pull in the named concern. If the user accepts, read the corresponding `concerns/<name>.md` file before pressing further on that concern. If the user declines, **do not offer the same concern again in this session**.

| Signal in conversation | Concern to offer | File to load on yes |
|---|---|---|
| User mentions a metric, KPI, conversion, or "how would we know it's working" | Success metrics (and Analytics if telemetry is implied) | `concerns/success-metrics.md` (+ `concerns/analytics.md` if telemetry) |
| User mentions multiple roles, "admin vs user," tenancy, "per-user" state, "share with another user" | Multi-tenancy | `concerns/multi-tenancy.md` |
| User mentions "ship," "release," "rollout," "feature flag," "staged" | Launch & rollout | `concerns/launch-and-rollout.md` |
| User mentions cost, pricing, business case, monetization, "is this worth doing for the money" | Business viability | `concerns/business-viability.md` |
| User mentions "what could break," "risks," "what could go wrong" | Risks (cross-cutting; press in conversation, no separate file) | (no file; investigate in conversation) |
| User mentions "deprecate," "breaking change," "API contract," "versioning" | Library/SDK | `concerns/library-and-sdk.md` |
| User references an existing spec by name | Amendment-vs-new-spec branch (handled in Phase 1 / Phase 3, no concern file) | (no file; route per Output Contract in SKILL.md) |

The trigger phrasings above are starting points. The skill's first real session will surface phrasings that should fire but don't, or false positives — refine the table after the first session.

### Offer phrasing

Keep the offer lightweight:

> *"You mentioned [signal]. That sounds like it might warrant the [concern] guide — want me to pull it in and press on [example question]? (yes/no)"*

Single yes/no. Do not interrogate ("Tell me more about your metrics goals before I decide whether to pull in the concern"). The user's answer is enough information to choose.

### Suppression rule

Track which concerns the user has declined this session. Do not re-offer a declined concern. If the user *spontaneously* introduces a stronger version of the same signal later ("actually, let's talk about metrics"), you can offer once more.

---

## How to use this file

In Phase 0, the skill reads this file once to understand which concerns the active profile pre-enables. For each pre-enabled or `optional` concern, the skill reads the corresponding `concerns/<name>.md` file before Phase 2 — those files contain the actual investigation guidance.

Concerns marked `off` are not loaded unless a JIT trigger fires and the user accepts the offer. This is the core of the progressive-disclosure design: a `solo-local-cli` session with no triggers loads zero `concerns/` files, keeping the skill's working context lean.
