# Step 07 attempt 02 — rolling Amp discovery and local-authority preflight

- **Evidence window:** 2026-09-18 10:20:59Z–10:28:17Z
- **Checkout:** `$CHECKOUT`, branch `feat/portable-launcher-rolling-evidence`, commit `61bb889f1d70e4a1654a53e0828be52638081a02`
- **Outcome:** **PASS — the exact copied current Amp passed the Step 07 capability assertions**
- **Step 07 seal condition:** **met under `launcher_eligibility: capability_evidence`**
- **Historical record:** the blocked files directly under `step-07/` remain unchanged as attempt 1.
- **Not claimed:** model adherence, the complete Amp launcher suite, or launcher support.

## Immutable run pins

At run start, the resolved Amp source was observed, copied into a fresh mode-0700 disposable root, and the copy was changed to mode 0700. Source stat identity and SHA-256 were checked before and after the copy; they agreed with the post-placement copy digest on the first attempt. The bounded procedure would discard a changed copy and retry once from a fresh observation, then fail on a second change. Every Amp probe below ran `$RUN/bin/amp`, never the PATH entry.

| Component | Exact observed identity |
|---|---|
| Amp copy | `0.0.1789724374-g0d2ed0`; released `2026-09-18T09:39:34.000Z`; 100,795,872 bytes; SHA-256 `7c45e90d54d8a46adf8069077341e85471c6bff3e2a881afc00f0ab35b188f91` |
| Duo | `61bb889-dirty`; full commit `61bb889f1d70e4a1654a53e0828be52638081a02`; built `2026-09-18T10:22:15Z`; 18,474,494 bytes; SHA-256 `ecdbb54dd8f38e44ea6cac0abeb855be97ecde6f9cd2a0ea9f5846cf356a6bd9` |
| Duo manifest | `duo.manifest/v1`; semantic digest `sha256:fdf2dfe00b8638976bf319fd88e4b8f03db29e680ea9cc723e06d36fe354d172` |
| Current scenario | `portable-launcher-delegation/v1`, revision `2`; SHA-256 `b08b8a4f715137e60d098e725442f925f28b8b66970e3188428927ab447820d4` |
| Skill | `duo.skill/duo-delegation-loop/v1@sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182`; 6,284 bytes; installation ID `801f13028ed977a2764e01695764ea29` |
| Config | `duo.config/v3`; source SHA-256 `1b4cb5fafef7a324beb46b8d8529ee76d4cc1218c41d5da24251b0872b293840`; effective digest `sha256:92db1583823b8d8ce7384449896cea6c641b3b4c7df5d50ce56d43a0092826ae` |
| Herdr | `0.9.0`; protocol `herdr-socket-api/22`; executable SHA-256 `5ef212a3f142f902b1a4eb3f7a76827ded9f313804c580c48297c060ac1fcac0`; schema SHA-256 `226d4ecbd128d2e6bc84e4c8ddcec21ba9c7e51a0aafffcf087111ead3f1fa9a` |

The checkout was already dirty due to out-of-scope residue, so the build version records `-dirty`. The exact full source commit, build date, size, executable digest, and semantic manifest digest bind the tested Duo bytes without inspecting or changing that residue.

## Current policy and installed projection

The checkout manifest and ownership stamp both declared `launcher_eligibility: capability_evidence`. The current scenario was revision 2 and named `amp` as a recognized launcher without an exact version allowlist. Amp `0.0.1789724374-g0d2ed0` is absent from the historical `tested_versions` row (`0.0.1789675234-g2899fe`), as expected; history is not an eligibility gate under commit `61bb889`.

Checkout-built Duo ran:

```text
$RUN/bin/duo install portable-launchers --workspace $RUN/workspace --output json
```

It exited 0 with `duo.external/v1`, `projection.install`, `state: current`, and `changed: true`. The owned `SKILL.md` was a regular mode-0600 copy whose 6,284 bytes exactly matched the canonical checkout file. `.duo-generated.json` had schema `duo.projection-stamp/v1`, target `portable_launchers`, the current manifest digest, policy `capability_evidence`, the same skill digest, and SHA-256 `700c2cc3b3f5bcb61651b015e34c0c57794778a6a0a2753b00e018cb1294d8e4`.

## Copied-Amp deterministic discovery

Only Amp's non-agent skill diagnostics were used:

```text
$RUN/bin/amp skill list --json --settings-file $RUN/noauth/config/amp/settings.json
$RUN/bin/amp skill info duo-delegation-loop --json --settings-file $RUN/noauth/config/amp/settings.json
```

Both exited 0. `skill list` contained exactly one project skill and exactly one matching `duo-delegation-loop` record, with `source: workspace-agents` and base directory `file://$RUN/workspace/.agents/skills/duo-delegation-loop`; its errors array was empty. `skill info` returned the same name, description, and locator. The two parsed description digests were identical: `sha256:47c044a8e999289d69dd7912f007af0d7f11b6e10a8f5ffe28e730c7139cb85e`. Independent hashing bound that reported locator to the canonical full-file digest.

Here **“invokes” means deterministic project-skill discovery plus local loader/frontmatter metadata parsing**. It does not mean a model consumed the body or followed its instructions. No model turn, Amp thread, terminal input, plugin, MCP server, account credential, or account-global skill supplied the result. Update checks and non-project skill roots were disabled; MCP configuration was empty and all MCP launch permissions were rejected. A known-invalid non-secret auth sentinel made account-global retrieval unavailable without using a credential. No project or isolated system plugin root existed. Amp's only isolated persisted launcher state was under `$RUN` and was removed at cleanup.

## Positive reachable local-authority preflight

A copied Herdr 0.9.0 executable served an explicitly selected Unix socket wholly under `$RUN`. The probe was:

```text
$RUN/bin/duo doctor --workspace $RUN/workspace \
  --config $RUN/xdg/config/duo/duo.config.yaml \
  --host herdr:$RUN/xdg/runtime/herdr-step07/herdr.sock \
  --output json
```

Doctor exited 0 with `duo.launcher-preflight/v1`, `status: ready`, `authority_scope: local`, and `host_source: explicit-flag`. All seven required checks passed in contract order. The projection check reported `current`, the exact skill identity, and the same installation ID.

The eighth informational check was `warning / host.compatibility_unverified`. Herdr `0.9.0`/protocol `22` is not the complete suite's Herdr `0.8.2`/protocol `20`/schema `c48f1f54…b150`. That mismatch is informational for Step 07 preflight and is not represented here as the full-suite fixture.

## Negative unreachable-host refusal and no mutation

The same Duo binary, config, workspace, and projection were checked against an absent explicit socket:

```text
$RUN/bin/duo doctor --workspace $RUN/workspace \
  --config $RUN/xdg/config/duo/duo.config.yaml \
  --host herdr:$RUN/xdg/runtime/no-server/herdr.sock \
  --output json
```

Doctor exited 0 after producing the complete diagnostic, while preflight was `not_ready` with `authority_scope: unavailable`. The required failure was `host_reachability`, `fail / host.unreachable`; compatibility was `not_checked / prerequisite.not_reached`.

No launch command was invoked. Across positive and negative probes, the workspace payload manifest remained `sha256:ce0fe7a7827f9a5313b57e95961a31c451a754f07f53dc52b32aeb84effc56b8`, the Duo authority tree stayed at zero entries, no database was created, and the isolated Herdr snapshot stayed at zero workspaces, tabs, panes, and agents. Thus the negative result is refusal at host reachability without Duo authority, workspace, or host mutation.

## Cleanup, assumptions, and scope

Herdr was stopped through its isolated local control socket; its process exited and the socket disappeared. The disposable run root was then deleted. No raw run path, vendor state, transcript, environment dump, or secret is retained in this bundle.

Assumptions and remaining boundary:

1. A missing local authority store is `initializable`, as the doctor contract specifies; snapshots prove doctor did not create it.
2. Loader invocation is metadata-level capability evidence, not behavioral model adherence. Full launch/send/observe behavior remains outside Step 07.
3. Herdr 0.9.0 can prove explicit local reachability for this preflight but cannot substitute for the full suite's exact host fixture.
4. The policy makes absence from historical `tested_versions` expected, not a blocker. The exact copied version and digest remain immutable provenance for this run only.

This attempt passes Step 07. It does not claim the full Amp suite or Amp launcher support.

## Cross-repo impact

**Classification: invalidates an assumption in `duo-lab`; it does not unblock the full runtime suite.** On `2026-09-18`, exact Amp `0.0.1789724374-g0d2ed0` (copied executable SHA-256 `7c45e90d54d8a46adf8069077341e85471c6bff3e2a881afc00f0ab35b188f91`) passed deterministic installed-skill discovery and reachable/unreachable preflight under the current capability-evidence policy. This updates the planning-only Amp observation by invalidating its stale assumption that absence from historical `tested_versions` blocks Step 07. It does not establish the full runtime fixture or launcher support, so it does not unblock full-suite execution. A finding with these exact pins/date should be offered on the affected `duo-lab` matter; none was authored here.

## Verification

- `attempt-02/validate.sh`
- `sh -n attempt-02/validate.sh`
- `jq -e . attempt-02/probe-summary.json`
- `git diff --check -- evidence/portable-launcher-conformance/step-07/attempt-02`
