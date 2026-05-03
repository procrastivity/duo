# Manual testing — Duo

Hand-driven runbook for exercising Duo against a real Solo install.
The automated test suite (`npm test`) is the primary regression net;
this directory complements it — what to run end-to-end when bringing
Duo up on a new machine, verifying a release candidate, or feeling
the surface after touching tier resolution / policy / spawn paths.

Duo's surface is small (one stdio MCP server, three tools, an
optional policy file). The runbook reflects that — six short docs,
no fixtures-as-large-trees, no embedding service to provision.
Compared to a project like Hypomnema, the heavy prereq here is
**Solo**, not Duo itself.

## Reading order

1. [`00-setup.md`](./00-setup.md) — clone, `npm install`, `npm run
   build`, confirm Solo is installed and has agent tools registered
   spanning at least two tiers, copy the fixture config(s) into
   place.
2. [`01-running-duo.md`](./01-running-duo.md) — run Duo as an MCP
   stdio server, drive it from Claude Code (recommended) or raw
   JSON-RPC, verify the handshake and `tools/list` returns the three
   Duo tools.
3. [`02-tier-tools.md`](./02-tier-tools.md) — exercise
   `list_agent_tiers`, `resolve_agent_tool`, and `spawn_agent`.
   Confirm a spawned process appears in `mcp__solo__list_processes`
   and is reachable.
4. [`03-policy-overrides.md`](./03-policy-overrides.md) — drop in a
   `duo.policy.yaml`, exercise `extend` / `replace` token modes and
   `selection.preference`, observe the resolution diagnostics
   change.
5. [`04-logging.md`](./04-logging.md) — read Duo's structured stderr
   logs (`resolution.success`, `resolution.failure`,
   `spawn.success`, `spawn.failure`); confirm stdout stays clean
   (MCP protocol only).

## Driver scripts

POSIX `/bin/sh` drivers under [`scripts/`](./scripts/) — one per
documented exercise. Each section in the docs below pairs the
"Ask Claude" / "Call X" prose with a corresponding
`./notes/manual-testing/scripts/NN-thing.sh` invocation. Run them
directly when you want to reproduce a check fast or wire one into
CI. See [`scripts/README.md`](./scripts/README.md) for the index
and the tunables (`DUO_TIMEOUT`, `DUO_SLEEP`, etc.).

## Fixtures

Tiny by design — Duo doesn't ship test data, it operates against
whatever Solo has registered.

- [`fixtures/duo.config.yaml`](./fixtures/duo.config.yaml) — minimal
  config that points Duo at a locally-installed `solo` binary over
  stdio. Adjust the `command`/`args` to match your install path.
- [`fixtures/duo.policy.yaml`](./fixtures/duo.policy.yaml) —
  reference policy file used by `03-policy-overrides.md`. Comment
  blocks out by default; uncomment one section at a time.
- [`fixtures/README.md`](./fixtures/README.md) — what each fixture
  config exercises and why.

## Surface covered

| Area | Covered | Notes |
|---|---|---|
| Build (`npm install` + `npm run build`) | ✅ | `00` |
| Stdio MCP handshake (`initialize`, `tools/list`) | ✅ | `01` |
| `list_agent_tiers` | ✅ | `02` |
| `resolve_agent_tool` (per tier) | ✅ | `02` |
| `spawn_agent` end-to-end → Solo process | ✅ | `02` |
| Policy: `command_tokens` extend / replace | ✅ | `03` |
| Policy: `selection.preference` | ✅ | `03` |
| Policy: missing-but-explicit `DUO_POLICY` errors at startup | ✅ | `03` |
| Stderr structured logs | ✅ | `04` |
| Stdout MCP-only invariant | ✅ | `04` |

## Version-skew warning

The shipped `README.md` example currently shows
`solo.transportType: "stdio"` as the config shape. The validated
schema (`src/config.ts`) requires the longer
`solo.transport.{type,command,args}` form — that's what the runbook
uses. If you copy from the README and Duo exits at startup with a
`solo.transport.command` error, switch to the runbook's fixture
shape.
