# Step 09 — OpenCode discovery and local-authority preflight

- **Evidence window:** 2026-09-18 08:04:57Z–08:09:32Z
- **Checkout:** `$CHECKOUT`, branch `feat/portable-launcher-conformance`, commit `75ad27a58491a00d4243564f86dc213c4688aa35`
- **Outcome:** **PASS — the exact accepted OpenCode launcher passed discovery and both preflight cases**
- **Step 09 seal condition:** **met**
- **Not claimed:** behavioral model adherence, the complete OpenCode launcher suite, or overall launcher support. Step 10 owns the behavioral suite, and the installed Herdr does not match that suite's host pin.

## Exact observed pins

| Component | Observed identity |
|---|---|
| OpenCode | `1.18.31`; 185,030,784-byte ELF x86-64 executable; SHA-256 `f9dab32248695e9ebd56b16a1921798fd85112cf5a69c7dfd0cabc1e17be4a11` |
| Duo | `75ad27a-dirty`; full commit `75ad27a58491a00d4243564f86dc213c4688aa35`; built `2026-09-18T08:04:57Z`; 14,369,268 bytes; SHA-256 `f596857ba690ac1722d8c7118875aa2cfa4b5a76434406ead278bd350b83f6b5` |
| Duo manifest | semantic digest `sha256:35fe1c26e9b619759c9c7efc3b50074af8e61fb7d0ee595ad9e98278b9e02a84` |
| Skill | `duo.skill/duo-delegation-loop/v1@sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182`; 6,284 bytes; installation ID `760f86d8e7c1d2dcb31be8ebdf4b7f3d` |
| Config | `duo.config/v3`; source SHA-256 `1b4cb5fafef7a324beb46b8d8529ee76d4cc1218c41d5da24251b0872b293840`; effective digest `sha256:92db1583823b8d8ce7384449896cea6c641b3b4c7df5d50ce56d43a0092826ae`; preset `builder` |
| Authority | `$RUN/xdg/data/duo/duo.db`; absent, healthy, locally `initializable`, schema version `0`, no writer |
| Herdr | `0.9.0`; protocol `herdr-socket-api/22`; 39,728,936 bytes; SHA-256 `5ef212a3f142f902b1a4eb3f7a76827ded9f313804c580c48297c060ac1fcac0`; schema-export SHA-256 `226d4ecbd128d2e6bc84e4c8ddcec21ba9c7e51a0aafffcf087111ead3f1fa9a` |
| Host | Linux `6.8.0-136-generic`, `x86_64` |

The OpenCode version and executable digest exactly match the accepted Step 01 and product-suite pin. No latest or substitute executable was used. The checkout was already dirty because of out-of-scope user residue; the Duo build records that honestly in its version while its full commit, build time, byte count, digest, and semantic manifest digest disambiguate the executable.

## Isolated projection and ownership

The run used a mode-0700 disposable root with separate OpenCode home, XDG config/data/state/cache, Duo config/data/runtime, workspace, binaries, and Herdr state. The current checkout built Duo directly into `$RUN/bin/duo`. The installer command was:

```text
$RUN/bin/duo install portable-launchers --workspace $RUN/workspace --output json
```

It exited 0 with `projection.install`, `state: current`, and `changed: true`. The installed `SKILL.md` was a regular copied file and byte-matched the product-owned source at `$CHECKOUT/skills/duo-delegation-loop/SKILL.md`. Its ownership record was `duo.projection-stamp/v1`, target `portable_launchers`, format `duo.skill/duo-delegation-loop/v1`, semantic manifest digest `sha256:35fe1c26…02a84`, and exact tested range `amp=0.0.1789675234-g2899fe;opencode=1.18.31;codex=0.154.0`. The stamp SHA-256 was `2a1c43f5a231905844f53d0114bb449513d31d13c1d4de81866c9e5770f9a628`.

No real user skill tree, launcher config, account, or credential store was read or written. The disposable roots were removed after evidence validation.

## OpenCode recognition and deterministic loader invocation

From the isolated workspace, under a clean allowlisted environment:

```text
opencode debug skill --pure
opencode debug config --pure
```

Both commands exited 0. `debug skill` returned exactly one `duo-delegation-loop` record at `$RUN/workspace/.agents/skills/duo-delegation-loop/SKILL.md`. It parsed the canonical frontmatter description (SHA-256 `47c044a8…9cb85e`) and returned the complete body. The loaded-body digest and the independently extracted canonical-body digest were identical: `sha256:f957e32d73807d68fab29682eff0abc5bc98da5fb5a244c841c48eb6867c6421`. The body included the launch sequence and current portable-launcher installation guidance. Directly hashing the reported installed file tied it to the full canonical skill digest above.

At this Step 09 boundary, **“invokes” means OpenCode's deterministic `debug skill` facility invoked its project skill discovery, frontmatter parser, and body loader and returned the installed canonical skill**. It does not mean a model consumed or followed the instructions. No model turn was run, no OpenCode session was created, and no behavioral adherence is claimed; that belongs to Step 10. This is the strongest invocation proof compatible with the explicit prohibition on creating threads.

`--pure` disabled external plugins. The resolved isolated config had `plugin: []` and no MCP configuration. Aggregate database checks found zero sessions, accounts, and credentials. No terminal input was used.

## Positive reachable local-authority preflight

A disposable Herdr server answered at the explicitly selected isolated Unix socket. The exact command was:

```text
$RUN/bin/duo doctor --workspace $RUN/workspace \
  --config $RUN/xdg/config/duo/duo.config.yaml \
  --host herdr:$RUN/xdg/config/herdr/sessions/duo-step09/herdr.sock \
  --output json
```

Doctor exited 0 and reported `duo.launcher-preflight/v1`, `status: ready`, `authority_scope: local`, and `host_source: explicit-flag`. All seven required checks passed in contract order: executable, effective config, authority store, workspace, host selection, host reachability, and skill projection. The projected skill was `current` with the same identity and installation ID OpenCode resolved.

The eighth informational check was `warning / host.compatibility_unverified`: the reachable host is Herdr 0.9.0/protocol 22, not the later full-suite pin Herdr 0.8.2/protocol 20/schema `c48f1f54…b150`. Contract §6 makes compatibility informational for this preflight, so this does not weaken the Step 09 readiness result. It does prevent a complete-suite claim.

Positive diagnosis was read-only: the workspace payload manifest stayed `sha256:eb06b3f…8da1`, the Duo authority tree stayed at zero entries, and no authority database was created.

## Negative unreachable-host refusal

The same Duo binary, config, workspace, and installed skill were checked against an absent explicitly selected socket:

```text
$RUN/bin/duo doctor --workspace $RUN/workspace \
  --config $RUN/xdg/config/duo/duo.config.yaml \
  --host herdr:$RUN/xdg/runtime/no-server/herdr.sock \
  --output json
```

Doctor correctly exited 0 because it produced a complete diagnostic report, but preflight was `not_ready` with `authority_scope: unavailable`. The stage-specific required failure was:

- check/stage: `host_reachability`
- status/code: `fail / host.unreachable`
- summary: `selected Herdr host did not answer the bounded ping`
- action: start or select the Herdr server at the scrubbed `$RUN/xdg/runtime/no-server/herdr.sock` path and make it reachable

`host_compatibility` was `not_checked / prerequisite.not_reached`; the other six required checks retained their independent results, including a current skill projection.

No `session launch` command was invoked. The workspace payload manifest remained byte-for-byte identical, the authority tree remained at zero entries, no store was created, and the isolated reachable Herdr server retained zero workspaces, tabs, panes, and agents before and after the probes. Thus the negative case proves refusal at host reachability rather than treating executable presence as readiness, and it proves no Duo authority/workspace/host launch mutation.

## Assumptions and remaining boundary

1. A missing local authority store is `initializable` and passes, as the contract explicitly specifies. Both preflights proved doctor did not create it.
2. Reachability is established by the explicitly selected local Unix socket, not ambient vendor markers. The host process and every selected path were under `$RUN`.
3. OpenCode invocation is loader invocation only. Model consumption, launch/send/observe behavior, and instruction adherence are deliberately not inferred.
4. Herdr 0.9.0 can prove local reachability for preflight, but cannot substitute for the complete suite's exact Herdr 0.8.2 pin.
5. The existing out-of-scope dirty checkout state affects the human-readable Duo version suffix, not the captured full source commit or executable identity.

## Verification and scope result

- `validate.sh` asserts a Step 09 pass only when the launcher is exactly OpenCode `1.18.31` with accepted executable SHA-256 `f9dab322…e4a11`; it cannot bless an ineligible version.
- The validator checks canonical loader identity, plugin/MCP/session boundaries, all positive required checks, the negative stage-specific failure, and no-write/no-launch assertions.
- `git diff --check -- evidence/portable-launcher-conformance/step-09` passed.

**This evidence meets Step 09's seal condition.** Step 10 remains open and must use the full suite's exact host/runtime fixture before any OpenCode launcher-support claim.

Cross-repo impact: **unblocks work in `duo-lab`** by confirming the accepted OpenCode pin (`1.18.31`, executable SHA-256 `f9dab32248695e9ebd56b16a1921798fd85112cf5a69c7dfd0cabc1e17be4a11`) on 2026-09-18 for deterministic installed-skill discovery/loader invocation and local preflight. It does not invalidate the 2026-09-17 duo-lab assessment at `caf02f7eb325b1c935d0cf793bc53fbb3a3f1a65`. Proposed handoff: “Capture OpenCode 1.18.31 full-suite runtime evidence,” body: consume Duo Step 09's exact launcher/skill/preflight pins, re-verify the live launcher and provision exact Pi 0.83.0 plus Herdr 0.8.2/protocol 20/schema `c48f…b150`; seal when the complete launcher-neutral scenario passes; origin: `duo`, portable-launcher-conformance Step 09, observed 2026-09-18. Author it in `duo-lab` only after user approval.
