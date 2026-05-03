# Handoff: make `process_id` and `project_id` usable end-to-end

## Context

Duo currently detects `SOLO_PROCESS_ID` and `SOLO_PROJECT_ID` from the environment and stores both in `config.solo` (`src/config.ts:31-37`, `src/env.ts`). It also lets `duo.config.yaml` override either, with the YAML value winning (`src/config.ts:61-69`).

Of those two, only `projectId` is currently *used* downstream:

- `resolveProjectId` (`src/tools/spawn-agent.ts:65-72`) prefers caller input, falls back to `config.solo.projectId`.
- `SoloClient.spawnProcess` forwards `project_id` to Solo (`src/solo-client.ts:76-95`).

`config.solo.processId` is read into config and ignored from there on. No `src/` call site outside `config.ts` references it. The intake note that established this behavior (`notes/proposals/archive/solo-orchestrator-companion-intake.md:302-303`) explicitly resolved `SOLO_PROCESS_ID` as "informational only for MVP — purpose is self-identification, not behavioral."

## Why this is now load-bearing

Hands-on `mcp2cli` testing against the Solo MCP revealed two facts that change the picture:

1. **Many Solo tools require `process_id` (in addition to `project_id`)** — not just tools that *create* processes. Tools that proxy state on behalf of a caller (todos, scratchpads, KV under process scope, locks) need to know which process is "speaking." Solo is forgiving when `SOLO_PROJECT_ID` is unset, but in those cases it more or less requires `SOLO_PROCESS_ID` / a bound session in order to act.
2. **`list_processes` cannot do a cross-project query** (confirmed empirically). It requires `project_id` *or* `process_id` to scope itself. There is no Solo equivalent of `list_projects`-but-for-processes. (Worth flagging upstream to the Solo author as a gap.)

So as Duo grows beyond `spawn_agent` toward proxying Solo state on behalf of the caller, every such tool will hit the same wall the user just hit at the CLI: `failed to deserialize parameters: invalid type ... expected i64`, or a missing-required-arg rejection.

## The config-file fields are misfeatures

Independent of the threading work, `solo.processId` and `solo.projectId` should be **removed from `soloConfigSchema`**:

- **`solo.projectId`:** Solo project IDs are integers assigned per Solo installation (e.g. `id: 6` for `duo` on the user's machine). The user typically has many Solo projects at once (5+ here, including some no-longer-active), and IDs differ per machine. A YAML field for it duplicates information Solo itself already knows authoritatively (project-path mapping). Any time the cwd and the configured ID disagree, the config wins and you get silent cross-project writes — actively harmful. The legitimate sources are env (`SOLO_PROJECT_ID`) and pwd→project lookup. Neither benefits from a YAML override.
- **`solo.processId`:** Even more clearly wrong. Process IDs are per-launch ephemeral. A YAML field for them invites stale config.

Drop both fields. Keep env-var detection. Add a pwd-derivation fallback for `projectId`. Resolve both at connect time, store on the client, never look at them again.

## Plan

Resolve both scopes **once, at `SoloClient` connect time**, and rely on Solo's `bind_session_process` to inject `process_id` into every subsequent `tools/call`. Tool handlers stop carrying scope plumbing; `SoloClient` owns it.

### Connect-time resolution sequence

After the existing `initialize` + `notifications/initialized` handshake in `SoloClient.connect()`:

```
projectId = env.SOLO_PROJECT_ID
if !projectId:
  projects = await listProjects()
  projectId = longestMatch(projects, p => cwd.startsWith(p.path))?.id

processId = env.SOLO_PROCESS_ID
if processId: await bindSessionProcess(processId)

store projectId / processId on the client; expose getters
log resolved scope at info; log misses at info too (not error)
```

### Edge cases

- pwd matches **no** Solo project → leave `projectId` unset, log at info, let downstream Solo calls fail clearly with a structured error. Don't crash the server.
- pwd matches **multiple** projects (nested paths) → pick the **longest** path match.
- env **and** pwd both resolve, to **different** projects → **env wins**. Explicit beats inferred. Log both so the discrepancy is visible.
- `bind_session_process` fails → log and continue. Same posture as today's "informational only" — the binding is a best-effort upgrade, not a hard requirement.
- Solo session-state premise: `bind_session_process` must persist across multiple `tools/call` requests on the *same* MCP session (which Duo holds open via `SoloClient`). This needs an empirical sanity check before trusting it. If it fails, fall back to per-call `process_id` threading on each `SoloClient` method that needs it (strictly more work, always correct).

## Concrete steps

In dependency order:

1. **Verify `bind_session_process` premise.** Throwaway test: open a Solo session, call `bind_session_process`, then call a process-scoped tool *without* an explicit `process_id`. If accepted → proceed. If rejected → drop binding, plumb `process_id` per-method instead.

2. **Trim `soloConfigSchema`** (`src/config.ts:14-25`). Remove `processId` and `projectId` fields. Update `parseConfig` to stop merging YAML overrides for them. Keep `detectSoloEnv`. Update `src/config.test.ts`, `src/server.test.ts`, `src/__fixtures__/spawn-results.ts` fixtures.

3. **Add `SoloClient.listProjects()`.** New method wrapping Solo's `list_projects`. Returns `{ id, name, path }[]` (matches the shape we saw via `mcp2cli`). Add a Zod schema for it next to existing `SoloAgentToolsSchema`.

4. **Move scope resolution into the client.**
   - Suggested file: `src/solo-client/scope.ts` (or inline in `solo-client.ts` if small enough).
   - Functions: `resolveProjectIdAtConnect(env, cwd, projects)`, `bindProcessIfPresent(client, env)`.
   - `SoloClient` constructor takes `cwd` (default `process.cwd()`) and `env` (default `process.env`) — keep them injectable for tests.
   - `SoloClient.connect()` runs the sequence above. Stash results on private fields. Expose `get projectId()` / `get processId()` getters.

5. **Delete `resolveProjectId` from `src/tools/spawn-agent.ts`.** Tool handlers stop resolving scope. They take `project_id` from caller input *only*; if absent, omit it from the Solo call and let `SoloClient` inject the connection-resolved one (or for `spawn_process`, where `project_id` is an explicit Solo arg, read it from `client.projectId`).

6. **Update `SoloClient.spawnProcess`.** Today it accepts `project_id` in args. Change: if caller provides one, forward it (override). Otherwise inject `client.projectId`. Same null-handling as today (omit if undefined).

7. **Future process-scoped tool wrappers** (when added — *not* in this change): rely on the binding for `process_id`. Add an explicit `process_id` arg to the wrapper *only* if a real use case for "act on behalf of a different process" emerges. Speculative plumbing rots.

8. **Tests.**
   - `SoloClient.connect()` resolves projectId from env when present.
   - `SoloClient.connect()` falls back to `list_projects` + cwd longest-match when env unset.
   - Nested-path projects → longest match wins.
   - No match → projectId stays undefined, no throw.
   - Env and pwd disagree → env wins, warning logged.
   - `bind_session_process` called when `SOLO_PROCESS_ID` set; not called when unset.
   - `bind_session_process` failure logged but doesn't reject `connect()`.
   - `spawnProcess` uses `client.projectId` when caller omits it.
   - `spawnProcess` honors caller-supplied `project_id` over the bound one.
   - Removed config fields → `parseConfig` rejects YAML that sets `solo.processId` / `solo.projectId` (strict schema).

9. **Documentation.**
   - `README.md:88-100` — drop the `solo.processId` / `solo.projectId` config bullets. Document the env-only + pwd-derive resolution. Note that `SOLO_PROJECT_ID` is a hard override and pwd is the fallback.
   - `docs/solo-orchestrator-companion-prd.md` — update REQ-011 from "best-effort default" to "session-bound at connect."
   - Don't touch the archived intake note. The "informational only" resolution is correct *as of when it was written*; this handoff supersedes it.

## Out of scope

- Adding new process-scoped Duo tools. This change makes them *possible* without per-tool scope plumbing; it doesn't add any.
- A `bind_session_process` Duo tool / passthrough so callers can rebind mid-session. Probably YAGNI — the orchestrator companion's identity shouldn't shift mid-flight.
- Working around `list_processes`'s lack of cross-project mode. If Duo ever needs cross-project process listing, the only workaround is `list_projects` → loop `list_processes --project-id N`. Ugly but mechanical. Better: file an upstream issue with Solo.

## Files most likely to change

- `src/config.ts` — drop `processId`/`projectId` fields from schema; keep `detectSoloEnv`.
- `src/solo-client.ts` — add `listProjects`, scope resolution in `connect()`, getters; constructor takes `cwd`/`env`.
- `src/solo-client/scope.ts` (new, optional) — pure helpers for resolution + longest-match logic, easy to unit-test.
- `src/server.ts:48` — pass `cwd` and `env` to `SoloClient` if not defaulted.
- `src/tools/spawn-agent.ts` — remove `resolveProjectId`; read scope from the client.
- `src/__fixtures__/spawn-results.ts` — fixture cleanup; add new ones for connect-time resolution paths.
- `src/config.test.ts`, `src/server.test.ts`, `src/solo-client.test.ts`, `src/tools/spawn-agent.test.ts` — test updates.
- `README.md`, `docs/solo-orchestrator-companion-prd.md` — doc touch-ups.

Estimated effort: ~2 hours of work, plus the empirical `bind_session_process` check up front. Risk: low. Net code change is likely a small reduction once the dead `processId` config plumbing is removed.
