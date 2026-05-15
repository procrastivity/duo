# Suggestion for the Solo maintainer: CWD-based project auto-scoping for the `solo` CLI

**From:** the Duo project (an independent Solo MCP client + CLI)
**To:** the Solo maintainer
**Topic:** how to make project-scoped `solo` commands work without an explicit
project id when invoked from inside a project's directory tree.

Background and Duo-side rationale: see [project-auto-scoping.md](./project-auto-scoping.md).

This document is self-contained. You do not need to read any Duo source to
implement this — the entire algorithm is reproduced verbatim below.

---

## 1. The ask

The `solo` CLI today requires an explicit project id for every project-scoped
command, even when run from inside a project's directory:

```text
solo projects get 5
solo processes list --project-id 5
solo todos list --project-id 5
```

But the CLI process already knows its own working directory, and
`solo projects list` already returns each project's absolute root `path`. So the
project id is almost always derivable with **zero new flags, zero protocol
changes, and no server work**: match the current working directory against the
known project roots.

Duo already does exactly this. From anywhere inside a project tree,
`duo whoami` and `duo project status` resolve the right project automatically.
This handoff explains precisely how, so the same behavior can be added to the
first-party `solo` CLI.

The proposed end state:

```text
cd ~/Code/duo/src/cli      # a subdirectory of a registered project
solo projects get          # → resolves to the project rooted at ~/Code/duo
solo processes list        # → same, no --project-id needed
solo whoami                # → prints the resolved project + how it was resolved
```

---

## 2. The key enabler

Two facts make this a pure function with no infrastructure:

1. **The CLI knows its cwd.** It is a local process; `process.cwd()` (or the OS
   equivalent) is free.
2. **Solo already exposes project roots.** `solo projects list` (and the
   `list_projects` MCP tool) returns a list of
   `{ id: number, name: string, path: string }`, where `path` is the project's
   absolute filesystem root.

Everything else is a ~20-line pure function over `(cwd, projects[])`. No
daemon, no marker files, no new persisted state, no API change.

---

## 3. The algorithm (reference implementation)

This is the exact code Duo runs, reproduced verbatim from
`src/solo-client/scope.ts`. It is plain TypeScript but trivially portable to
any language.

```typescript
type EnvSource = Record<string, string | undefined>;

// A project as returned by `list_projects` / `solo projects list`.
interface SoloProject {
  id: number;
  name: string;
  path: string; // absolute project root
}

// Parse an env override. Accepts only non-negative integers; anything
// else (empty, non-numeric, negative, undefined) → undefined (ignored).
const parseId = (raw: string | undefined): number | undefined => {
  if (raw === undefined || raw === "") return undefined;
  const n = Number(raw);
  return Number.isInteger(n) && n >= 0 ? n : undefined;
};

// Longest-prefix match of cwd against registered project roots.
export const longestPathMatch = (
  projects: SoloProject[],
  cwd: string,
): SoloProject | undefined => {
  const matches = projects.filter(
    (p) => cwd === p.path || cwd.startsWith(p.path + "/"),
  );
  matches.sort((a, b) => b.path.length - a.path.length);
  return matches[0];
};

// Full resolution: env override wins, else cwd match, else undefined.
export const resolveProjectIdAtConnect = (
  env: EnvSource,
  cwd: string,
  projects: SoloProject[],
): { projectId?: number; envProjectId?: number; pwdProjectId?: number } => {
  const envProjectId = parseId(env.SOLO_PROJECT_ID);
  const pwdMatch = longestPathMatch(projects, cwd);
  const pwdProjectId = pwdMatch?.id;

  return {
    envProjectId,
    pwdProjectId,
    projectId: envProjectId ?? pwdProjectId,
  };
};
```

Why each line matters:

- **`cwd === p.path || cwd.startsWith(p.path + "/")`** — a project matches if
  the cwd *is* the project root or is *inside* it. The `+ "/"` is a
  deliberate sibling-safety guard: without it, cwd `/Users/me/Code/duo-sibling`
  would falsely match a project rooted at `/Users/me/Code/duo` (string prefix
  collision). With it, it does not.
- **sort by `path.length` descending, take `[0]`** — when project roots are
  nested (a project inside another project's tree), the **deepest** root wins.
  Running from `/Users/me/Code/duo/src` picks the project rooted at
  `/Users/me/Code/duo`, not the one at `/Users/me/Code`.
- **`envProjectId ?? pwdProjectId`** — the env override always wins when
  present and valid; otherwise fall back to the cwd match; otherwise the result
  is `undefined` (unresolved). There is intentionally **no** "if there is
  exactly one project, auto-select it" fallback — see §5.

---

## 4. Precedence

When determining the effective project for a project-scoped command, resolve
in this strict order (first hit wins):

| Priority | Source | In `solo` CLI terms |
|---|---|---|
| 1 (highest) | Explicit id on the command | `--project-id <id>` or a positional id like `solo projects get 5` |
| 2 | Environment override | `SOLO_PROJECT_ID` env var (parsed as a non-negative integer; invalid values are *ignored*, not errors) |
| 3 | CWD longest-prefix match | `longestPathMatch(projects, cwd)` |
| 4 (lowest) | Unresolved | Emit a clear, actionable error — do **not** guess |

Notes:

- Duo uses the env var name `SOLO_PROJECT_ID`. Reusing the same name in the
  CLI keeps the two tools consistent for users who run both.
- An **invalid** `SOLO_PROJECT_ID` (e.g. `not-a-number`) is silently ignored
  and resolution falls through to the cwd match. It is not a hard error. This
  is intentional so a stale/garbage env var never bricks the CLI.
- When the env override and the cwd match resolve to **different** project ids,
  the env override wins. Duo additionally logs this disagreement (an info-level
  `project_scope_disagreement` event with both ids and which was chosen) so it
  is debuggable without changing behavior. Recommended to mirror this — surface
  it in `solo whoami` / verbose output rather than failing.

---

## 5. Edge cases and behavior spec

These are taken directly from Duo's test suite
(`src/solo-client/scope.test.ts`) and double as an acceptance spec. Given
projects:

```text
id 1  outer  /Users/me/Code
id 2  duo    /Users/me/Code/duo
id 3  other  /Users/me/elsewhere
```

| cwd | env | Expected resolved id | Why |
|---|---|---|---|
| `/Users/me/Code/duo/src` | (unset) | `2` | Longest prefix wins (nested) |
| `/Users/me/Code/duo` | (unset) | `2` | Exact root match |
| `/Users/me` | (unset) | *unresolved* | A parent of a project root is not inside any project |
| `/Users/me/Code/duo-sibling` | (unset) | `1` | Sibling-safety: must NOT match id 2; it *is* inside `/Users/me/Code` so id 1 matches |
| `/tmp/x` | (unset) | *unresolved* | No project contains this path |
| `/Users/me/Code/duo` | `SOLO_PROJECT_ID=99` | `99` | Env override wins over cwd match |
| `/Users/me/Code/duo` | `SOLO_PROJECT_ID=not-a-number` | `2` | Invalid env ignored, falls back to cwd |

**Deliberate non-features (do not "fix" silently):**

- **No auto-select-single-project.** Even if exactly one project exists, an
  unmatched cwd stays unresolved. This avoids surprising cross-project writes
  when a user `cd`s out of their project tree.
- **Unresolved → error, not guess.** When nothing resolves and the command is
  project-scoped, fail with a message that lists the fix options
  (`--project-id`, `cd` into the project, set `SOLO_PROJECT_ID`).

**Hardening Duo does NOT do (you may want to):** the match is a raw string
prefix comparison. There is intentionally:

- no `realpath`/symlink canonicalization (a symlinked cwd whose real path is
  inside a project, or a project root stored as a symlink, will not match);
- no trailing-slash normalization on either side;
- no case folding (on case-insensitive macOS/Windows filesystems, a
  case-mismatched cwd will not match).

For a first-party CLI it is reasonable to canonicalize both `cwd` and each
`project.path` (resolve symlinks, strip trailing slashes, and optionally
case-fold on case-insensitive filesystems) **before** running
`longestPathMatch`. Duo's tests assume non-canonicalized inputs; if you
canonicalize, the longest-prefix logic itself is unchanged.

---

## 6. Recommended adoption for the `solo` CLI

1. **Resolver step.** For every project-scoped subcommand, before requiring
   `--project-id`, run the resolver:
   - if an explicit id was given (flag or positional) → use it, skip the rest;
   - else read `SOLO_PROJECT_ID`;
   - else call the same project listing the CLI already uses for
     `solo projects list` and run `longestPathMatch(projects, cwd)`
     (optionally on canonicalized paths per §5);
   - else error with the actionable message from §5.
2. **Skip the list call when pinned.** When `SOLO_PROJECT_ID` is set and valid,
   Duo does *not* fetch the project list at all (it cannot change the answer).
   Cheap optimization worth keeping.
3. **Add `solo whoami`** (or a block in `solo status`) that prints the resolved
   project `id` / `name` / `path` and the **source** of the resolution
   (`flag` | `env` | `cwd` | `unresolved`). This is the single highest-value
   discoverability/debugging affordance — it makes the auto-scoping legible
   instead of magic. Duo's `duo whoami` does exactly this.
4. **Make resolution observable, not silent.** On env/cwd disagreement, show
   it in verbose/JSON output rather than failing (see §4).

A complete reference for the user-facing shape: in Duo, `duo whoami` prints
`project_id`, `project_name`, `project_path`, and `process_id`; `duo project
status` simply resolves the project the same way and forwards it to the
status call with no flags required from the user.

---

## 7. Optional: server-side variant (not recommended)

Solo *could* instead resolve this in the HTTP control plane — but only if the
caller passes its `cwd`, since the server does not otherwise know it. That
requires an API/protocol change and still depends on the client volunteering
its working directory. The client-side approach above needs none of that, is
what Duo has proven in production use, and keeps the resolution logic next to
the process that actually owns the cwd. Recommend client-side.

(One nuance worth noting: for Solo-managed agent processes, Solo *does* know
the spawn cwd, so a server-side resolution could be added later specifically
for spawned agents without a protocol change. That is additive and out of
scope for the CLI fix.)

---

## 8. Appendix: where this lives in Duo (for reference only)

You do not need these to implement the feature, but if you want to read the
originals:

| File / symbol | Role |
|---|---|
| `src/solo-client/scope.ts` — `longestPathMatch`, `resolveProjectIdAtConnect`, `parseId` | The entire portable core (reproduced in §3) |
| `src/solo-client/scope.test.ts` | The behavior spec / acceptance tests (reproduced in §5) |
| `src/solo-client.ts` — `_resolveScope()` | Connect-time wiring: env-skip optimization, disagreement logging |
| `src/solo-client.ts` — `callTool()` | Auto-injects the resolved `project_id` into downstream calls unless the caller passed one (why zero-flag commands work) |
| `src/solo-client.ts` — `listProjects()` | Fetches `{ id, name, path }[]` from Solo's `list_projects` |
| `src/types/solo.ts` — `SoloProjectSchema` | The `{ id, name, path }` shape |
| `src/cli/connect.ts` — `connectSolo()` | cwd source: `opts.cwd ?? process.cwd()` (the `--cwd` flag is a test/non-tty override) |
| `src/cli/commands/whoami.ts` | `duo whoami` — the recommended `solo whoami` analog |
| `src/cli/commands/project.ts` — `statusCommand` | `duo project status` — zero-flag project-scoped command |

A closely related but **separate** mechanism in Duo: `SOLO_PROCESS_ID` →
`bind_session_process`. That is *process identity* binding, not project scope,
and is orthogonal to everything above. Mentioned only so it is not confused
with project resolution.
