# Project auto-scoping in Duo — design & history

**Status:** shipped. The resolver lives in `src/solo-client/scope.ts`; it is
wired in at connect time in `src/solo-client.ts`.
**Audience:** Duo contributors and future maintainers — *why* this exists and
*how* it is shaped.
**See also:** the portable, self-contained recommendation we wrote for the
upstream Solo maintainer lives in
[solo-cli-project-auto-scoping.md](./solo-cli-project-auto-scoping.md). That
document owns the canonical algorithm listing and the full edge-case spec; this
one is the rationale and does not duplicate them.

---

## The problem

Solo is project-scoped: nearly every Solo operation needs a `project_id`, and
Solo (both the MCP server and the first-party `solo` CLI) requires the caller to
supply it explicitly. Duo, however, is run *from inside a project's working
tree* — you `cd` into a repo and run `duo whoami`, `duo project status`, and so
on. Forcing the human to pass `--project-id 5` (or look the id up first) on
every single command, while standing in the very directory that uniquely
identifies the project, is pure friction. It also invites a worse failure mode:
copy-pasting a stale id and acting on the wrong project.

We wanted Duo to "just know" which project you're in, the same way `git` knows
which repository you're in.

## What we wanted (goals and non-goals)

Goals:

- **Zero-flag scoping.** From any directory inside a registered project (or a
  subdirectory of it), project-scoped commands resolve the project with no flag
  and no lookup.
- **Predictable and safe over clever.** A wrong-but-confident answer is worse
  than no answer. Resolution must be explainable in one sentence.
- **No new infrastructure.** No marker/sentinel files (`.duo`, `solo.yml`), no
  Duo-side project registry, no persisted state, and no change to the Solo wire
  protocol. Duo already learns project roots from Solo itself.

Non-goals:

- **No "auto-select the only project."** Even when exactly one project exists,
  an unmatched cwd stays unresolved (see *Tradeoffs*).
- **Not a path-canonicalization engine.** We chose the simplest matcher that is
  correct for the common case and documented its limits rather than chase every
  symlink/case edge.

## The design we landed on

The whole mechanism is a small pure function plus one well-placed call site.

- **Project roots come from Solo, not from Duo.** Duo does not maintain its own
  registry. At connect time it calls Solo's `list_projects`
  (`SoloClient.listProjects`, `src/solo-client.ts:165-172`), which returns
  `{ id, name, path }` records (`SoloProjectSchema`, `src/types/solo.ts:15-23`).
  `path` is the project's absolute root — that single field is the linchpin of
  the entire feature.

- **Longest-prefix match against the cwd.** `longestPathMatch`
  (`src/solo-client/scope.ts:18-25`) keeps every project where the cwd *is* the
  root or is inside it, then picks the deepest match. The `+ "/"` in the
  prefix test is a deliberate sibling-safety guard so `/Users/me/Code/duo-x`
  cannot match a project rooted at `/Users/me/Code/duo`. When project roots
  nest, the deepest root wins — running from `…/Code/duo/src` resolves to the
  `…/Code/duo` project, not `…/Code`.

- **A small, strict precedence.** `resolveProjectIdAtConnect`
  (`src/solo-client/scope.ts:27-41`) layers an explicit override on top of the
  cwd match: a valid `SOLO_PROJECT_ID` env var wins; otherwise the cwd match;
  otherwise unresolved. `parseId` (`src/solo-client/scope.ts:12-16`) accepts
  only non-negative integers, so a garbage env var is *ignored* (falls through
  to cwd) rather than turned into a hard error — a stale env var should never
  brick the CLI.

- **Resolved once, at connect.** `SoloClient._resolveScope`
  (`src/solo-client.ts:98-150`) runs the resolver during `connect()`. When
  `SOLO_PROJECT_ID` pins the answer it skips the `list_projects` round-trip
  entirely (it cannot change the outcome). When the env var and the cwd match
  *disagree*, it keeps the env winner but emits an info-level
  `project_scope_disagreement` log so the divergence is debuggable without
  changing behavior; it also logs `project_resolved` / `project_unresolved`.

- **Auto-injection is what makes commands flag-free.** Once resolved, the id is
  held on the client (`SoloClient.projectId`, `src/solo-client.ts:65-67`) and
  `callTool` (`src/solo-client.ts:180-200`) threads it onto every downstream
  Solo tool call *unless the caller already supplied one*. This is the reason
  `duo project status` (`src/cli/commands/project.ts:33-58`) and `duo whoami`
  (`src/cli/commands/whoami.ts`) need no `--project-id`: they just read the
  already-resolved scope. `whoami` re-looks-up the name/path purely for display.

- **The cwd itself** comes from `connectSolo`
  (`src/cli/connect.ts:23-24`) as `opts.cwd ?? process.cwd()`; the `--cwd` flag
  is only an override for tests and non-interactive callers.

## Tradeoffs and what we deliberately skipped

- **No auto-select-single-project.** Tempting, but it means `cd`-ing *out* of
  your project and running a write command would silently hit the one project
  that happens to exist. We chose "unresolved → actionable error" over that
  class of surprise. This is a behavioral contract, not an oversight.

- **Raw string prefix match — no canonicalization (yet).** There is
  intentionally no `realpath`/symlink resolution, no trailing-slash
  normalization, and no case folding. Consequences: a symlinked cwd whose real
  path is inside a project won't match, and on case-insensitive macOS/Windows
  filesystems a case-mismatched cwd won't match. We accepted this for
  simplicity; it is the most likely future hardening and is called out as such
  in the Solo handoff doc.

- **`SOLO_PROCESS_ID` is a separate axis.** `resolveProcessIdFromEnv`
  (`src/solo-client/scope.ts:43-44`) → `bind_session_process` binds *process
  identity*, not project scope. It is orthogonal to everything above and is only
  mentioned here so the two are never conflated.

## How it behaves

The behavior is pinned by tests, which double as the executable spec — see
`src/solo-client/scope.test.ts` (the nested / exact / parent / sibling-safety /
no-match / env-wins / invalid-env matrix) and `src/solo-client.test.ts:106-132`
(connect-time wiring). The full human-readable case table is reproduced in
[solo-cli-project-auto-scoping.md § 5](./solo-cli-project-auto-scoping.md); it
is intentionally kept in one place so the two documents cannot drift.

## Outcome

From any subdirectory of a registered project, `duo whoami` and
`duo project status` resolve the right project with no flags, no lookup, and no
new files on disk. Explicit `--project-id` and `SOLO_PROJECT_ID` still override
when you need them.

## Relationship to Solo

The same approach is directly portable to Solo's own `solo` CLI with no
protocol change — Solo already knows its cwd and `solo projects list` already
returns each project's `path`. We wrote that up as a standalone, self-contained
recommendation for the upstream maintainer:
[solo-cli-project-auto-scoping.md](./solo-cli-project-auto-scoping.md).
