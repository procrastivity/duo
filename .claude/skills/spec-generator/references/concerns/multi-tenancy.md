# Multi-Tenancy, Personas, and Adversarial Acceptance Criteria

## What this concern means

Any feature that touches per-user state, ownership-scoped resources, role-based permissions, or data that should be invisible to other users. Multi-tenancy means every request is implicitly attributed to a user/tenant, and every read/write is implicitly scoped to that attribution. The most common failure mode: trusting the client to scope correctly.

## When this concern is in scope

- **Pre-enabled by profile**: `public-saas`.
- **Optional**: `internal-tool` (gated on whether the feature touches per-user state in a way that could leak across users).
- **JIT triggers**: user mentions multiple roles, "admin vs user," tenancy, "per-user" state, "share with another user," "permissions," "ownership."
- **N/A for**: `solo-local-cli` (no tenancy by definition; only one user), `library-or-sdk` (the SDK doesn't manage users; consumers do).

## What to investigate during conversation

- **What are the personas / roles?** For multi-user features, "user" is not enough. Press for specific roles (admin, member, viewer; authenticated, anonymous; owner, collaborator).
- **What's the ownership boundary on every resource the feature touches?** Every readable / writable / mutatable resource keyed by `user_id` / `tenant_id` / `org_id` needs an explicit ownership rule.
- **What's the attacker model?** The adversary is not the user — it's another *legitimate* user attempting to access resources that aren't theirs. (Unauthenticated attackers are a separate concern; this is specifically about cross-tenant attacks from authenticated users.)
- **Where does ownership get checked?** Trust boundaries: at the validator, at the controller, at the query, at the policy layer? Inconsistency is where bugs live.
- **What about transitive ownership?** If a resource references another resource (e.g., a comment on a thread), is the comment's ownership derived from the thread's ownership or independent?

## What rigor applies

### Personas in user stories

Multi-tenant features need specific personas in their stories. "As a thread owner" / "As a thread collaborator" / "As a non-collaborator" — each persona's stories cover their distinct journey.

### Adversarial acceptance criteria — non-negotiable

**Every story that touches an ownership-scoped resource MUST include at least one adversarial criterion** proving cross-tenant access is denied:

```
- [ ] Given a [resource] belonging to another user, when the authenticated user submits a request that references it, then the request is rejected with 403/422 and no rows are written.
```

Implementations whose ownership-scoped stories lack a cross-tenant denial criterion should be rejected at review.

A common gap: a request validates `exists:table,id` *without* constraining by `user_id`, and no test exercises the cross-tenant case. The validation passes, the controller trusts the validated id, and any user can write into another user's data.

### Auth-related Global Invariants (when in scope)

For multi-tenant specs, declare load-bearing rules in Implementation Notes that every story must uphold:

- **Every endpoint accepting a `<resource>_id` must reject `<resource>_id`s that do not belong to the authenticated user.** *Why:* prior implementation had a cross-tenant write hole. *How to verify:* validation rule must be `Rule::exists(...)->where('user_id', auth()->id())`, AND a feature test must assert another user gets 403/422.
- **Authorization for `<Resource>` lives in `<single canonical location>`, not scattered across requests/controllers.** *Why:* enforcement consistency. *How to verify:* `<location>` exists; downstream sites delegate via `$user->can(...)`.
- **No user-wide query exists in `<Service>`.** Every method takes `<scope_id>` as a required parameter — no nullable defaults. *Why:* scope-unaware queries silently leak across tenants. *How to verify:* grep for `?int $<scope_id> = null` returns zero matches.

Each invariant should be specific and *enforceable* — a reviewer must be able to grep for a violation or write a test that proves it.

### Boundary-graph for ownership values

When an ownership-scoped value crosses module boundaries (e.g., `thread_id` flows from request → validator → controller → query → response), enumerate every hop and state the type guarantee at each. An unauthenticated `String` somewhere in the chain is a hole.

## Decomposition manifest entry

```markdown
### Multi-tenancy invariants
- Add adversarial AC patterns to all stories touching `<resource>` (see story file).
- Authorization location: `<canonical place>`. Downstream call sites must delegate.
- Negative-fingerprint grep: `<grep>` returns zero matches when invariants are upheld.

### Personas added
- `<persona>` — `<role description>`. Distinct stories: `<list>`.
```

## Examples of strong vs. weak coverage

**Strong**:

> Story: As a thread owner, I want to add a collaborator to my thread so that they can post messages.
>
> ACs include:
> - [ ] When the authenticated user is the thread's owner, the collaborator is added and a 200 is returned with the updated thread.
> - [ ] **Adversarial:** Given a thread belonging to another user, when the authenticated user submits a request to add a collaborator to it, then the request is rejected with 403 and no rows are written.
> - [ ] **Adversarial:** Given a thread the authenticated user is a collaborator on (but not owner), when they attempt to add another collaborator, then the request is rejected with 403.

**Weak**:

> Story: As a user, I want to add a collaborator to my thread.
>
> ACs:
> - [ ] User can add a collaborator to a thread.
> - [ ] System validates that the thread exists.

The weak version trusts the client (`thread_id` validated for existence but not ownership). Any user can add themselves as a collaborator to any thread.
