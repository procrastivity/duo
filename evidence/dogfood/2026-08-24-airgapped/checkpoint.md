# Checkpoint verdict — 2026-08-24

Mode: live (duo Go build `53ddced`, built 2026-08-24T17:24:27Z,
installed as `duo`; Herdr session `duo` running from a clean shell —
server env verified marker-free via /proc/<pid>/environ; claude
2.1.241, pi 0.83.0)

| leg | verdict | evidence |
|---|---|---|
| config authored + loads (doctor: duo.config/v3) | **pass** | `duo doctor`: `config: ~/.config/duo/duo.config.yaml` / `schema: duo.config/v3`, exit 0 (day-log #1, #18) |
| every preset dry-runs | **pass** | 4/4 presets exit 0 with `--host` (day-log #3); build_and_verify resolves builder=pi/qwen-3.6 + verifier=claude/sonnet-5 — relation-forced, by design (decisions.md) |
| claude leg launched | **pass** | orchestrator → ses_5717e0b0…, claude/fable-5 pane w2:p2, agent idle + interactive_ready (day-log #7) |
| pi leg launched | **pass** | builder `--require agent_runtime=pi` → ses_bd47d03e…, pi/qwen-3.6 (`--provider x-es --model anthropic/Wyvern`) pane w2:p3, host_source=workspace-correlation (day-log #9) |
| multi-leaf preset, distinct agent names | **pass (on retest)** | On `53ddced`: exit 4 `agent_name_taken: duo-lrr_df18a…` (sharp edge 11). User rebuilt to `4ced46d` same day; retest exit 0, both panes up, names `duo-builder-lrr_1a7030b5…` / `duo-verifier-lrr_1a7030b5…` (day-log #19–20) |
| host correlation bound deliberately + shown | **pass** | explicit `--host` with user pre-approval; loud `host bound:` + fact_43f3491c…; `workspace host show` verbatim in day-log; post-bind launch deduced workspace-correlation |
| detach/reattach drill | **pass** | on the enrolled session ses_285f7397… (detach→reattach across two invocations, day-log #16–17). On a *launched* session this build refuses `has no host attachment` (day-log #13) — documented, recorded |
| enroll | **pass** | externally started claude (haiku) via `herdr agent start` in w2:p5, enrolled as ses_285f7397…; reporter credential shown once, not recorded (day-log #14–15) |
| full-day verb coverage | **pass** | all 9 checklist verbs exercised (day-log coverage list) |

Skipped legs and why:
- `check-jsonschema` structural validation — tool not installed;
  optional, dry-run is the authoritative check.
- `duo config migrate` — no v2 document on this machine; nothing to
  migrate.

Overall: **onboarded, live, 9/9 pass after the same-day rebuild to
`4ced46d`.** On the original `53ddced` binary the multi-leaf leg failed
with the sharp-edge-11 naming bug (leafless `duo-lrr_…` names →
`agent_name_taken` on the second leaf, plus sharp-edge-10 debris: a
stray pane, closed with user sign-off, and committed sessions that
remain in the store — no `session remove` exists). The rebuild fixed
it, and also fixed detach on *launched* sessions (commit 8c8deac).
One **new OPEN finding** on `4ced46d`: reattach on a launched session
refuses the `herdr pane list` fingerprints of both of its own panes
(`enrollment conflict: observed fingerprint is not the claim this
session holds`) — the recovery drill still only round-trips on an
*enrolled* session; details and open questions in findings.md.
Workspace `~/Code/duo` is durably bound to
`herdr:~/.config/herdr/sessions/duo/herdr.sock`; daily launches from
the project root need no flags.

Addendum 2026-08-25: the OPEN reattach finding is root-caused (see
findings.md → Resolution). The launch-recorded claim includes the
agent's process birth (pid + ms start time) and reattach needs the same
tuple via `--process-pid` / `--process-started-at`; no verb exposes it
yet. Known limit since 8c8deac, no fix in the five commits after
4ced46d. Fix in progress: surface the fingerprint on `session show`.

Addendum 2026-08-25 (2): a second OPEN finding — every launched session
stays `active`/`starting` forever because nothing ever records exit
(no caller of `Exit`/`ResolveRecovery`), and no prune verb exists.
Details in findings.md; handoff in
handoff-session-exit-reconciliation.md.
