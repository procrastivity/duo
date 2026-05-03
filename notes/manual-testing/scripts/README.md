# Driver scripts

POSIX `/bin/sh` drivers, one per documented exercise in the
runbook. Each script:

- Sources [`lib.sh`](./lib.sh) for the MCP handshake helpers and
  the `duo_drive` runner.
- `cd`s into the repo root before invoking `node ./dist/index.js`,
  so your local `./duo.config.yaml` is what gets loaded.
- Holds stdin open via `sleep` so async responses flush before
  EOF, then bounds the run with `timeout` (Duo does not
  self-exit). Exit code `124` = healthy completion.

## Prerequisites

Run from a shell where `npm run build` has produced
`dist/index.js`. Each script will check and bail out with a clear
message otherwise.

## Tunables

Override anything in `lib.sh` by exporting before the call:

```sh
DUO_TIMEOUT=20 DUO_SLEEP=10 ./02-spawn-agent.sh large my-helper
```

Knobs (defaults in `lib.sh`):

| Variable | Purpose |
|---|---|
| `DUO_TIMEOUT` | seconds before `timeout(1)` kills the run (10) |
| `DUO_SLEEP` | seconds to keep stdin open after last request (5) |
| `DUO_NODE` | node binary (`node`) |
| `DUO_DIST` | path to `dist/index.js` |
| `DUO_REPO_ROOT` | repo root (auto-derived) |
| `DUO_PROTOCOL` | MCP protocol version (`2024-11-05`) |
| `DUO_CLIENT_NAME` | initialize clientInfo.name (`runbook`) |

The `04-log-walkthrough.sh` script also honors `DUO_OUT` /
`DUO_ERR` for the capture file paths.

## Index

| Script | Exercise | Doc reference |
|---|---|---|
| `00-smoke.sh` | boot Duo with no requests; verify silence on stderr | 00-setup.md §6 |
| `01-tools-list.sh` | handshake + tools/list (no Solo round-trip) | 01-running-duo.md Option B |
| `02-list-agent-tiers.sh` | `list_agent_tiers` | 02-tier-tools.md §1; 03-policy-overrides.md §2 |
| `02-resolve-agent-tool.sh [tier]` | `resolve_agent_tool` (default `medium`) | 02-tier-tools.md §2; 03-policy-overrides.md §1, §3 |
| `02-resolve-unsupported.sh` | `resolve_agent_tool tier=purple` failure case | 02-tier-tools.md §2 |
| `02-spawn-agent.sh [tier] [name] [project_id]` | `spawn_agent` | 02-tier-tools.md §3 |
| `02-spawn-unsupported.sh` | `spawn_agent tier=purple` failure case | 02-tier-tools.md §3 |
| `04-log-walkthrough.sh [tier]` | multi-call sequence with split stdout/stderr capture | 04-logging.md §1 |

Step 03 (policy overrides) does not introduce new scripts — its
exercises uncomment a block in `duo.policy.yaml`, restart Duo, and
re-run an existing 02-* script to observe the behavior change.

## Caveat — Solo state matters

These scripts exercise Duo's surface, but every meaningful
response depends on what your local Solo install has registered.
If `02-list-agent-tiers.sh` reports `available: false` for every
tier, register agent tools in Solo before continuing — see
`00-setup.md` §3.

For the same reason, `02-spawn-agent.sh medium` may produce
different selected tools across runs (or even fail with "no tools
available" if your medium tier is empty). That's expected. Pass an
explicit tier that you've verified is populated:

```sh
./02-spawn-agent.sh small
```
