# Step 07 — Amp discovery and local-authority preflight

- **Evidence window:** 2026-09-18 07:45:21Z–07:51:13Z
- **Checkout:** `$CHECKOUT`, branch `feat/portable-launcher-conformance`, commit `e38ff77744ad453912ba4dd3a9bc7372c95e1130`
- **Outcome:** **BLOCKED — the accepted Amp executable was unavailable**
- **Not claimed:** the complete Amp launcher suite or launcher support. The live Amp and Herdr versions do not match the repository's later full-suite pins.

## Live pins

| Component | Observed identity |
|---|---|
| Amp | `0.0.1789715829-g82b7fb`, released `2026-09-18T07:17:09.000Z`; 100,787,680-byte resolved executable; SHA-256 `36b6da094db6e984ede80f65027674bda3fd1c126828b1fb4c254c003b6cd568` |
| Duo | `e38ff77-dirty`; full commit `e38ff77744ad453912ba4dd3a9bc7372c95e1130`; built `2026-09-18T07:45:21Z`; SHA-256 `871361f7d066e67b72cd6a95ba07c2d73ed21c6db14cd614795a7f6569028cf8` |
| Skill | `duo.skill/duo-delegation-loop/v1@sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182`; 6,284 bytes; installation ID `99c41f9c084e349f940813980c9dcd22` |
| Config | `duo.config/v3`; effective digest `sha256:bb5b986318036d0c25d6ed9d5f586ffde5a0973ab2f3a38daf98fe29ab4ae674`; preset `builder` |
| Authority | `$RUN/xdg/data/duo/duo.db`; absent, healthy, locally `initializable`, no writer |
| Herdr | `0.9.0`; protocol `herdr-socket-api/22`; 39,728,936-byte executable; SHA-256 `5ef212a3f142f902b1a4eb3f7a76827ded9f313804c580c48297c060ac1fcac0`; schema-export SHA-256 `226d4ecbd128d2e6bc84e4c8ddcec21ba9c7e51a0aafffcf087111ead3f1fa9a` |
| Host | Linux `6.8.0-136-generic`, `x86_64` |

The live Amp pin changed since Step 01 (`0.0.1789675234-g2899fe`, digest `f351217d…`). It also differs from the manifest's current exact tested version. This run therefore re-probed the live binary instead of inheriting Step 01's result. The new pin is discovery evidence only; the manifest and full-suite accepted pin remain unchanged. Because the workplan requires the pinned launcher and the suite admits exact identities, this capture does not meet Step 07's seal condition.

The checkout was already dirty before this task because of out-of-scope user work. The Duo build records that honestly as `e38ff77-dirty`; its full commit, build time, manifest digest, byte size, and executable digest disambiguate the tested binary. No product source was changed.

## Isolated setup and projection

The run used one mode-0700 disposable root with separate home, XDG config/data/state/cache/runtime, workspace, binaries, and Herdr session. The canonical probe did not use account credentials. A known-invalid local sentinel prevented account-global plugin retrieval without exposing or copying a credential. The local skill commands still completed with exit 0.

The checkout-built Duo command was:

```text
$RUN/bin/duo install portable-launchers --workspace $RUN/workspace --output json
```

It returned `projection.install`, `state: current`, `changed: true`, and the identity above. The installed `SKILL.md` was a regular copied file with the exact canonical digest. Its Duo ownership stamp had manifest digest `sha256:f508953d954b9d1ad2f37f18753a1d77a3b71ef847c280572bfb6d196d0f528f`.

No real user skill tree was written. The disposable roots were removed after validation.

## Amp recognition and loader invocation

From the isolated workspace, with the isolated settings and roots:

```text
amp skill list --json --settings-file $RUN/noauth/config/amp/settings.json
amp skill info duo-delegation-loop --json --settings-file $RUN/noauth/config/amp/settings.json
```

Both commands exited 0. `skill list` returned exactly one `duo-delegation-loop` entry with `source: workspace-agents` and base directory `file://$RUN/workspace/.agents/skills/duo-delegation-loop`; `skill info` resolved the same path. The returned descriptions were byte-identical (SHA-256 `47c044a8e999289d69dd7912f007af0d7f11b6e10a8f5ffe28e730c7139cb85e`). Direct hashing tied that path to the installed skill digest.

This is a direct invocation of Amp's local skill loader and metadata parser. It is deliberately not a model-turn behavioral invocation: `amp -x` would create a thread, which this task prohibited and Step 08 owns. No thread or terminal-input path was used.

The canonical Amp log showed zero plugin-start events. Account-global plugin lookup was deliberately denied, project/system plugin roots were absent, and both MCP initialization records had empty `ready` and `disabled` sets. The skill result had no errors. Thus neither a plugin nor MCP supplied the recognition evidence.

## Positive local-authority preflight

A disposable Herdr 0.9.0 server answered at the explicit isolated Unix socket. The exact command was:

```text
$RUN/bin/duo doctor --workspace $RUN/workspace \
  --config $RUN/xdg/config/duo/duo.config.yaml \
  --host herdr:$RUN/xdg/config/herdr/sessions/duo-step07/herdr.sock \
  --output json
```

Doctor exited 0 and reported `duo.launcher-preflight/v1`, `status: ready`, and `authority_scope: local`. All seven required checks passed in contract order: executable, config, authority, workspace, host selection, host reachability, and skill projection. The selected host answered the bounded ping, and the projected skill was `current` with the same installation ID and digest Amp resolved.

The eighth informational check was separately `warning / host.compatibility_unverified`: installed Herdr is 0.9.0/protocol 22, not the full-suite pin 0.8.2/protocol 20/schema `c48f1f54…`. Under contract §6, that warning does not make otherwise reachable local authority not-ready. This evidence does **not** claim the later suite pin.

## Negative reachability and refusal before launch

The same binary, config, workspace, and installed skill were checked against an absent socket:

```text
$RUN/bin/duo doctor --workspace $RUN/workspace \
  --config $RUN/xdg/config/duo/duo.config.yaml \
  --host herdr:$RUN/xdg/runtime/no-server/herdr.sock \
  --output json
```

Doctor still exited 0, as required for a complete diagnostic report, but preflight was `not_ready` with `authority_scope: unavailable`. The exact required failure was:

- stage/check: `host_reachability`
- status/code: `fail / host.unreachable`
- summary: `selected Herdr host did not answer the bounded ping`
- action: `Host reachability stage: start or select the Herdr server at $RUN/xdg/runtime/no-server/herdr.sock and make its socket reachable.`

`host_compatibility` was `not_checked / prerequisite.not_reached`. The Duo executable, valid config, initializable authority, workspace, host selection, and current projection still passed, proving readiness did not collapse to binary presence.

No launch command was invoked. The Duo authority tree had zero entries before and after, so doctor did not create a store or session record. The workspace payload manifest was identical before and after (`sha256:f900a9968fff9683031b6e87016860c75f80508657f4c30ddab8a5561bcf61ea`).

## Conservative assumptions and remaining boundary

1. “Invokes the skill” is interpreted at this Step 07 boundary as Amp's deterministic local loader invocation (`skill info`), not an agent/model turn. That is the strongest proof compatible with the explicit no-thread rule; behavioral scenario execution remains Step 08.
2. A missing authority store is accepted as `initializable`, exactly as contract §6.1 specifies. Doctor remained read-only and did not create it.
3. Herdr 0.9.0 can honestly prove reachability/readiness under the informational compatibility rule, but cannot prove the later exact Herdr suite pin.
4. Live Amp discovery is proved for the newly observed exact executable. Because it differs from the manifest's tested pin, no broader launcher-support or full-suite compatibility claim is made.
5. The canonical Amp probe intentionally had no account-global state. Its expected unauthenticated global-plugin warning is not a skill error and does not weaken the direct workspace source/path evidence.

## Blocker and resume condition

The accepted `0.0.1789675234-g2899fe` executable with SHA-256 `f351217d…` was no longer present under the local Amp installation or in the searched local caches. The installed self-updating binary cannot substitute for it: the suite explicitly rejects unpinned latest versions even when their observed behavior succeeds.

Resume this step by provisioning that exact accepted executable and rerunning discovery plus both preflight cases. Alternatively, revise the accepted Amp pin through a deliberate contract change and rerun every Stage 1 observation and Stage 2 artifact whose identity embeds the old version. This evidence alone is insufficient to authorize that wider change.

## Verification

- `jq` parsed and asserted the raw installer and both raw doctor captures before scrubbing.
- `validate.sh` parses `probe-summary.json`, checks cross-field identities and positive/negative verdicts, confirms plugin/MCP/thread boundaries, and rejects raw run paths or common credential material.
- `git diff --check -- evidence/portable-launcher-conformance/step-07` passed.

Cross-repo impact: **the live Amp observation invalidates the freshness assumption for the accepted Amp pin in `duo-lab`'s 2026-09-17 assessment at `caf02f7eb325b1c935d0cf793bc53fbb3a3f1a65`**. It does not contradict that assessment's historical discovery result or alter the Herdr 0.8.2/protocol 20 runtime-fixture requirement. Offer a finding on the affected `duo-lab` matter before authoring it there.
