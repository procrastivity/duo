# Day log — 2026-08-24

Binary: `duo version` = `duo version 53ddced (commit 53ddced, built 2026-08-24T17:24:27Z)`. Config: `~/.config/duo/duo.config.yaml`.

All launches run from the project root `/home/bsimensen/Code/duo`
(sharp edge 7). Host: Herdr session `duo`,
socket `~/.config/herdr/sessions/duo/herdr.sock`. The Herdr server's
environment was verified clean of agent-harness markers before any
launch (scrub gate passed on every spawn).

| # | command | outcome | notes |
|---|---------|---------|-------|
| 1 | `duo doctor` (pre-bind) | exit 0 | store not yet initialized; config `schema: duo.config/v3`; policy-default rung refused: discovery found 2 instances (`.setup`, `duo`) — never guesses |
| 2 | `duo session launch orchestrator --dry-run` (no --host) | exit 1 | `Host deduction produced no candidate host` + ways-out rail; expected from a bare shell with 2 instances |
| 3 | dry-run × 4 presets with `--host herdr:…/duo/herdr.sock` | all exit 0 | orchestrator→fable-5 (determined); builder→sonnet-5 (open); reviewer→opus-5 (determined); build_and_verify→builder=pi/qwen-3.6 + verifier=claude/sonnet-5 (relation-forced, by design) |
| 4 | `builder --dry-run --avoid model_family=claude` | exit 0 | selection flipped to pi/qwen-3.6 ✓ |
| 5 | `builder --dry-run --require agent_runtime=pi` | exit 0 | narrowed to pi/qwen-3.6 ✓ |
| 6 | `reviewer --dry-run --require agent_runtime=pi` | exit 1 | `Require agent_runtime=pi left no complete assignment.` — correct exhaustion |
| 7 | `duo session launch orchestrator --host herdr:…/duo/herdr.sock` | exit 0 | **claude leg + deliberate first bind** (user pre-approved the target). `host bound: ws_8e951a… -> herdr:…` loud, fact_43f3491c…; ses_5717e0b0…, lrr_5a73a077…; pane w2:p2, agent `duo-lrr_5a73a…` (no leaf segment — see findings) |
| 8 | `duo workspace host show` | exit 0 | output below |
| 9 | `duo session launch builder --require agent_runtime=pi` | exit 0 | **pi leg**, no --host: `host_source=workspace-correlation` ✓; ses_bd47d03e…, pane w2:p3, pi/qwen-3.6 spawned with `--provider x-es --model anthropic/Wyvern` |
| 10 | `duo session launch build_and_verify` | **exit 4** | `agent_name_taken: duo-lrr_df18a…` on the verifier leaf — sharp edge 11, build predates the multi-leaf naming fix. Builder-leaf pi agent left running (w2:p4), stray agent-less pane w2:p5, session ses_06553833… committed (sharp edge 10) |
| 11 | `duo session list` | exit 0 | 3 sessions, all VIEW=`recovering` (sharp edge 3, documented) |
| 12 | `duo session show ses_5717e0b0…` | exit 0 | lifecycle active, instance state `starting`, owner cli, quarantined false |
| 13 | `duo session detach ses_5717e0b0…` (launched session) | exit 1 | `session … has no host attachment` — the documented refusal on builds that don't record attachment at launch; drill moved to an enrolled session |
| 14 | `herdr agent start drill-external --kind claude --pane w2:p5 -- --model haiku` | exit 0 | repurposed the stray pane as an externally started agent for the enroll leg |
| 15 | `duo session enroll --root-path ~/Code/duo --integration-instance herdr:duo --epoch-kind herdr.terminal_id --epoch-value term_659ced678b1e26 --epoch-scope pane --container w2:p5` | exit 0 | ses_285f7397…; reporter credential shown once and deliberately not recorded |
| 16 | `duo session detach ses_285f7397… --reason "restart-recovery drill"` | exit 0 | `attachment detached` |
| 17 | `duo session reattach ses_285f7397… --integration-instance herdr:duo --epoch-kind herdr.terminal_id --epoch-value term_659ced678b1e26 --epoch-scope pane --container w2:p5` | exit 0 | `attachment attached` — restart-recovery drill complete across two invocations |
| 18 | `duo doctor` (post-bind) | exit 0 | store healthy, writer none active; binding shown with fingerprint (pane w2:p2, term_659ced3feecdf3); M1 would deduce `workspace-correlation` |

## Retest after rebuild (user pulled latest `go` commits; `duo version 4ced46d`, built 2026-08-24T18:21:02Z)

| # | command | outcome | notes |
|---|---------|---------|-------|
| 19 | `duo session launch build_and_verify` | exit 0 | **sharp-edge-11 fix confirmed**: ses_156959…, lrr_1a7030b5…; builder=pi/qwen-3.6 (w3:p2), verifier=claude/sonnet-5 (w3:p3); host via workspace-correlation |
| 20 | `herdr agent list` | ok | distinct leaf-segment names: `duo-builder-lrr_1a7030b5…`, `duo-verifier-lrr_1a7030b5…` — matches the fixed-build transcript |
| 21 | `duo session detach ses_156959…` (launched session) | exit 0 | `attachment detached` — the 53ddced refusal (#13) is fixed by 8c8deac |
| 22 | `duo session reattach ses_156959… …` (builder pane fingerprints from `herdr pane list`) | exit 1 | `enrollment conflict: observed fingerprint is not the claim this session holds; a new execution needs a new runtime instance` |
| 23 | same with verifier pane fingerprints | exit 1 | same conflict — **new OPEN finding** (see findings.md); ses_156959… left detached, its panes closed at cleanup |
| 24 | `herdr pane close w3:p2` / `w3:p3` | ok | retest panes closed |

## workspace host show (step-2 evidence, verbatim)

```
workspace root:  /home/bsimensen/Code/duo
workspace:       ws_8e951a55bccb72170e35ab4eaacee434
host:            herdr:/home/bsimensen/.config/herdr/sessions/duo/herdr.sock
host instance:   -
host source:     explicit-flag
session name:    -
pane id:         w2:p2
terminal id:     term_659ced3feecdf3
process birth:   pid=35966 started=2026-08-24T18:05:14.150Z
bound by:        workspace.host_bound (fact_43f3491c070fe6fa98b44d0f94adbc7b)
bound at:        2026-08-24T18:05:16.103Z
actor:           cli
reason:          cold-start host correlation from launch deduction
evidence:        host deduced by explicit-flag and proven by a successful spawn (launch-resolution record lrr_5a73a0779abf763ef5932963f44c9ce0)
```

## Verb coverage
- [x] launch (claude leg) — #7
- [x] launch (pi leg) — #9
- [x] list — #11
- [x] show — #12
- [x] enroll — #15
- [x] detach — #16 (on the enrolled session; #13 records the launched-session refusal)
- [x] reattach (recovery drill) — #17
- [x] workspace host show — #8
- [x] doctor — #1, #18
