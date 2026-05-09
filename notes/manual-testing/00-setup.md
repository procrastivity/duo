# 00 · Setup

> Applies to: Duo current `main`.
> Prereqs: a clone of this repo, Node ≥ 24, a working **Solo**
> install, and an MCP client to drive Duo (Claude Code recommended;
> raw JSON-RPC also covered in `01-running-duo.md`).

This doc gets you to a state where the `dist/duo.mjs` bundle exists,
a Duo config file points at your local Solo binary, Solo has at
least a couple of agent tools registered spanning two or more tiers,
and Duo is ready to be wired into a client. Subsequent docs assume
this is done.

All commands assume the working directory is the Duo repo root
unless noted.

## 1. Toolchain

```bash
node --version    # expect v24 or newer
npm --version
```

If `node --version` reports anything older than 24, install a newer
runtime (nvm, asdf, the Nix flake in this repo, etc.) before
continuing — Duo's `package.json` declares `"engines": { "node":
">=24.0.0" }` and the bundle is built with esbuild's `node24` target,
so older runtimes are unsupported and may not run the published
output.

## 2. Build the binary

```bash
npm install
npm run build
```

`npm run build` runs `node scripts/build.mjs` (esbuild bundle,
`node24` target) and writes a single ESM bundle to `dist/duo.mjs`.
The `bin` entry in `package.json` points at that file, so
`npx @procrastivity/duo` (once published) and a local
`node ./dist/duo.mjs` invocation are equivalent.

Optional sanity checks:

```bash
npm run typecheck   # tsc --noEmit
npm test            # vitest run — full unit suite
```

The runbook does not require either to pass; it does require
`dist/duo.mjs` to exist.

## 3. Solo prereqs

Duo is an MCP **client** of Solo: it spawns the configured `solo`
binary over stdio and calls Solo's tools (`list_agent_tools`,
`spawn_process`, etc.). Before running Duo, make sure:

1. **Solo is installed** somewhere on disk and runnable. Find the
   absolute path you want Duo to spawn, e.g.:
   ```
   /Applications/Solo.app/Contents/MacOS/mcp
   ```
   or `which solo` if you have a shell wrapper.
2. **Solo has agent tools registered** for at least two tiers.
   Without registered tools, `list_agent_tiers` returns every tier
   as `available: false` and `spawn_agent` cannot succeed.

   You can confirm via Solo directly (from any MCP client already
   wired to Solo, e.g. Claude Code with Solo already configured):
   ```
   mcp__solo__list_agent_tools
   ```
   Look for at least one tool whose `command` or `name` contains a
   small-tier token (`haiku`, `mini`, `fast`) and one whose token
   maps to medium (`sonnet`, `standard`) or large (`opus`,
   `flagship`, `pro`). Duo's built-in classifier rules live in
   `src/classifier.ts` if you need to confirm what tokens are
   recognized.

If your Solo install has only one or zero tools, register more
before continuing — the runbook's tier-coverage steps depend on at
least two distinct tiers being populated.

## 4. Duo config file

Resolution order (see `src/cli/config-loader.ts`):

1. `DUO_CONFIG` env var — verbatim path (highest priority).
2. `$XDG_CONFIG_HOME/duo/config.yaml` — if `XDG_CONFIG_HOME` is set.
3. `~/.config/duo/config.yaml` — unconditional XDG default fallback.

Note: cwd-relative `./duo.config.yaml` lookup has been removed.
Dropping a `duo.config.yaml` next to the repo no longer works on
its own — point `DUO_CONFIG` at it, or move it to the XDG path.

For the runbook, the simplest path is to point `DUO_CONFIG` at a
local copy of the fixture so you can edit it in-tree:

```bash
cp notes/manual-testing/fixtures/duo.config.yaml ./duo.config.yaml
export DUO_CONFIG="$(pwd)/duo.config.yaml"
```

Then edit `./duo.config.yaml` so `solo.transport.command` and
`solo.transport.args` match your local Solo binary. The fixture
ships with a placeholder you must replace; if you forget, Duo will
log a stderr error from `execa` when it tries to spawn Solo.

If you'd rather use the XDG default and skip `DUO_CONFIG` entirely:

```bash
mkdir -p ~/.config/duo
cp notes/manual-testing/fixtures/duo.config.yaml ~/.config/duo/config.yaml
$EDITOR ~/.config/duo/config.yaml
```

The full minimum looks like this:

```yaml
solo:
  transport:
    type: stdio
    command: /absolute/path/to/solo
    args: ["mcp", "serve"]   # or whatever your Solo build expects
```

> **README discrepancy**: the project README shows
> `solo.transportType: "stdio"` as a flat field. That shape does
> **not** validate against `src/config.ts` and Duo will exit at
> startup with `solo.transport.command is required`. Use the nested
> `solo.transport.{type,command,args}` shape above (and in the
> fixture).

**Project / process scope is not a YAML field.** Duo resolves both
at connect time:

- `SOLO_PROJECT_ID` (env, integer) is a hard override. If unset, Duo
  calls Solo's `list_projects` and selects the project whose `path` is
  the longest prefix of the cwd Duo was launched from.
- `SOLO_PROCESS_ID` (env, integer), if set, triggers a one-shot
  `bind_session_process` at connect; Solo then routes process-scoped
  calls to that process for the rest of the session.

For the runbook, leaving both unset and launching Duo from this repo
root (`/.../duo`) is the easiest path — pwd derivation will pick the
matching Solo project. `02-tier-tools.md` exercises both the
auto-resolved scope and the per-call `project_id` override.

## 5. duo.policy.yaml (optional, runbook step 03)

Skip for now. Step 03 walks you through dropping a policy file in
place to exercise overrides; for steps 01 and 02 the built-in
classifier rules are what you want.

If a `./duo.policy.yaml` already exists from a prior session, move
it aside before continuing so steps 01–02 see only built-in
behavior:

```bash
mv duo.policy.yaml duo.policy.yaml.bak    # only if present
```

Duo's policy-load behavior:

- `DUO_POLICY` set, file missing → startup error.
- `DUO_POLICY` unset, default `./duo.policy.yaml` missing → silent
  no-op, built-ins only.
- File present (default or explicit) → parsed and merged.

## 6. Smoke check

Before wiring Duo into a client, confirm it boots cleanly without
any client attached:

```bash
node ./dist/duo.mjs < /dev/null
```

Duo will **not exit on its own** — even with stdin closed, the
spawned Solo child process keeps Node's event loop alive. The
smoke check is therefore: wait ~2 seconds, confirm nothing prints
on stderr, then Ctrl+C. Any **config / policy / Solo-spawn** error
surfaces on stderr almost immediately; if Solo's path is wrong
you'll see an `execa` error naming the missing command. Fix the
config and rerun until the first couple of seconds are silent.

For a scriptable form, bound the run with `timeout`:

```bash
timeout 2 node ./dist/duo.mjs < /dev/null; echo "exit=$?"
```

Exit code `124` means `timeout` killed a healthy process — that's
the success case. Any other non-zero exit accompanied by stderr
output is the failure case.

Or use the matching driver:

```sh
./notes/manual-testing/scripts/00-smoke.sh
```

You're ready for [`01-running-duo.md`](./01-running-duo.md).
