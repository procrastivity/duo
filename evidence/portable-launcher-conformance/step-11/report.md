# Step 11 — Codex discovery and local-authority preflight

- **Evidence window:** 2026-09-18 08:28:56Z–08:33:22Z
- **Checkout:** `$CHECKOUT`, branch `feat/portable-launcher-conformance`, commit `40a2d24652c2543b4cde3bc1ebbd164c7b8952c9`
- **Outcome:** **PASS — the exact accepted Codex native launcher passed deterministic discovery and both preflight cases**
- **Step 11 seal condition:** **met**
- **Not claimed:** behavioral model adherence, the complete Codex launcher suite, or overall launcher support.

## Exact observed pins

| Component | Observed identity |
|---|---|
| Codex wrapper | CLI `0.154.0`; 8,790-byte JavaScript entrypoint; SHA-256 `61b0194f3bb6534439c8d26a3ed57d0805f84b884588b761795323eeb92fcf70` |
| Codex accepted native | package `@openai/codex` `0.154.0-linux-x64`; 262,858,016-byte ELF x86-64; SHA-256 `3188814c35471432d4123203e0eb38e5bddc60226e3d7ddf0e59e649ea140022` |
| Duo | `40a2d24-dirty`; full commit `40a2d24652c2543b4cde3bc1ebbd164c7b8952c9`; built `2026-09-18T08:28:56Z`; 14,369,268 bytes; SHA-256 `546be61fc15d0744d9d0758e6e2629cf154dfbdc2df1eebf72aa5884787de2c7` |
| Duo manifest | semantic digest `sha256:5d247c2033264777e20566c0bbc53842ebed5078f4135ca6a899ecfea8bf7404` |
| Skill | `duo.skill/duo-delegation-loop/v1@sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182`; 6,284 bytes; installation ID `7e04a954a123f6fc2e1129d5f8e9e752` |
| Config | `duo.config/v3`; source SHA-256 `1b4cb5fafef7a324beb46b8d8529ee76d4cc1218c41d5da24251b0872b293840`; effective digest `sha256:92db1583823b8d8ce7384449896cea6c641b3b4c7df5d50ce56d43a0092826ae` |
| Authority | `$RUN/xdg/data/duo/duo.db`; absent, healthy, locally `initializable`, schema version `0`, no writer |
| Herdr | `0.9.0`; protocol `herdr-socket-api/22`; SHA-256 `5ef212a3f142f902b1a4eb3f7a76827ded9f313804c580c48297c060ac1fcac0`; schema SHA-256 `226d4ecbd128d2e6bc84e4c8ddcec21ba9c7e51a0aafffcf087111ead3f1fa9a` |
| Host | Linux `6.8.0-136-generic`, `x86_64` |

The accepted Codex pin is the native executable, not the npm JavaScript entrypoint. The deterministic diagnostic was run directly with `$CODEX_NATIVE`; its version, native package version, byte count, and digest exactly match the accepted pin. The wrapper identity is retained separately so its smaller JavaScript digest cannot accidentally satisfy validation.

## Isolated projection and deterministic invocation boundary

The run used a mode-0700 disposable root with clean `HOME`, `CODEX_HOME`, XDG roots, workspace, Duo binary, and Herdr state. Checkout-built Duo installed a regular copied projection:

```text
$RUN/bin/duo install portable-launchers --workspace $RUN/workspace --output json
```

Installation returned `projection.install`, `state: current`, and `changed: true`. Installed `SKILL.md` byte-matched the current product source. Its ownership stamp was `duo.projection-stamp/v1`, target `portable_launchers`, with the current manifest and canonical skill digests and tested range `amp=0.0.1789675234-g2899fe;opencode=1.18.31;codex=0.154.0`.

From the isolated workspace, the accepted native executable ran only Codex's deterministic diagnostic:

```text
$CODEX_NATIVE debug prompt-input 'Use the duo-delegation-loop skill.'
```

It exited 0 and injected exactly one project entry named `duo-delegation-loop` from `$RUN/workspace/.agents/skills/duo-delegation-loop/SKILL.md` into model-visible prompt input. The parsed description digest was `sha256:47c044a8e999289d69dd7912f007af0d7f11b6e10a8f5ffe28e730c7139cb85e`; direct file hashing tied the locator to the canonical full-file digest above.

At this boundary, **“invokes” means the diagnostic invoked Codex's project skill discovery and frontmatter parser, then injected the skill name, canonical description, and installed locator into the model-visible input list**. It does not mean a model read or followed the body. No model turn, Codex thread/session, or behavioral adherence claim occurred. This is the strongest invocation proof allowed by the explicit no-thread constraint.

`codex plugin list --json` reported no installed plugins and `codex mcp list --json` reported no configured servers under the isolated roots. The diagnostic bootstrapped Codex's built-in system skills only inside disposable `CODEX_HOME`; those are not plugins and were deleted with the run root. No credentials, terminal input, user-global skill tree, or raw vendor state entered the evidence.

## Reachable local-authority preflight

A disposable real Herdr server answered at the explicitly selected isolated Unix socket:

```text
$RUN/bin/duo doctor --workspace $RUN/workspace \
  --config $RUN/xdg/config/duo/duo.config.yaml \
  --host herdr:$RUN/xdg/config/herdr/sessions/duo-step11/herdr.sock \
  --output json
```

Doctor exited 0 with `duo.launcher-preflight/v1`, `status: ready`, `authority_scope: local`, and `host_source: explicit-flag`. All seven required checks passed in contract order. The projected skill was `current` with the same identity and installation ID Codex exposed.

The informational compatibility check was `warning / host.compatibility_unverified`. Installed Herdr `0.9.0`/protocol `22`/schema `226d…1fa9a` does not match the later complete-suite pin `0.8.2`/protocol `20`/schema `c48f…b150`. Contract §6 makes this informational for preflight, so Step 11 can pass, but this run cannot claim the complete launcher suite.

## Unreachable local-authority refusal and no mutation

The same binary, config, workspace, and projection were checked against an absent explicitly selected socket:

```text
$RUN/bin/duo doctor --workspace $RUN/workspace \
  --config $RUN/xdg/config/duo/duo.config.yaml \
  --host herdr:$RUN/xdg/runtime/no-server/herdr.sock \
  --output json
```

Doctor correctly exited 0 after producing a complete report, while preflight was `not_ready` with `authority_scope: unavailable`. The stage-specific failure was `host_reachability`, `fail / host.unreachable`; `host_compatibility` was `not_checked / prerequisite.not_reached`.

No launch command was invoked. Across both probes, the Duo authority tree remained at zero entries, no authority database was created, and the workspace payload manifest remained `sha256:bbf0034693c602bd9ecebe65a6f21caf0e8c1556cac49c9cbb46628cb385efc2`. The fixture Herdr snapshot stayed at zero workspaces, tabs, panes, and agents. The server was stopped through local control, its process and socket disappeared, and the disposable root was removed.

## Assumptions, unresolved boundary, and verification

1. A missing local authority store is `initializable`, as the contract specifies; the no-write snapshots prove doctor did not create it.
2. Locality is the explicitly selected reachable Unix socket and isolated local paths, not ambient vendor markers.
3. The dirty version suffix reflects pre-existing out-of-scope checkout residue. The full source commit, build date, executable digest, and manifest digest disambiguate the tested Duo binary; the residue was not inspected or incorporated.
4. Deterministic loader invocation is not behavioral model adherence. Full launch/send/observe behavior belongs to Step 12.
5. Step 11 has no remaining blocker. Step 12 still requires the exact Pi `0.83.0` and Herdr `0.8.2` fixture and remains subject to the suite's documented blocked-condition prerequisite.

Verification passed:

- `evidence/portable-launcher-conformance/step-11/validate.sh`
- `git diff --check -- evidence/portable-launcher-conformance/step-11`

Cross-repo impact: **unblocks work in `duo-lab`** by confirming on 2026-09-18 that Codex CLI `0.154.0`, native package `0.154.0-linux-x64`, native SHA-256 `3188814c35471432d4123203e0eb38e5bddc60226e3d7ddf0e59e649ea140022` discovers the installed canonical skill and passes reachable/unreachable local preflight. It does not invalidate the 2026-09-17 assessment at `caf02f7eb325b1c935d0cf793bc53fbb3a3f1a65`. Proposed handoff (not authored without approval): title “Capture Codex 0.154.0 full-suite runtime evidence”; body: consume Duo Step 11's exact launcher/skill/preflight pins, re-verify the live pin, and provision exact Pi `0.83.0` plus Herdr `0.8.2`/protocol `20`/schema `c48f…b150`; seal when the complete launcher-neutral scenario passes or records its named prerequisite honestly; origin: `duo`, portable-launcher-conformance Step 11, commit `40a2d24652c2543b4cde3bc1ebbd164c7b8952c9`, observed 2026-09-18.
