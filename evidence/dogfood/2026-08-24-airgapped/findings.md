# Findings — 2026-08-24

## Multi-leaf launch fails: agent name has no leaf segment
- ran: `duo session launch build_and_verify` (from ~/Code/duo, bound workspace)
- got: `duo: session launch: launch: leaf verifier: starting launch: herdr agent.start: agent_name_taken: agent name duo-lrr_df18a3d9f2ab608865d88c52 is already used; candidates: terminal_id=term_659ced67899835 pane_id=w2:p4 …` exit 4. The builder leaf's agent had already taken the name `duo-lrr_df18a…` — no `<leaf>` segment — so the verifier leaf computed the identical name and refused.
- expected: agents named `duo-builder-lrr_…` / `duo-verifier-lrr_…`, both panes up.
- status: known, confirmed (sharp edge 11) — binary `53ddced` predated the multi-leaf naming fix. Single-leaf launches also showed the leafless name (`duo-lrr_5a73a…` for orchestrator), which is the tell.
- **RESOLVED same day:** user pulled the latest `go` commits and rebuilt (`duo version 4ced46d`, built 2026-08-24T18:21:02Z). Retest: `duo session launch build_and_verify` exit 0, ses_156959…; both panes spawned with distinct leaf-segment names `duo-builder-lrr_1a7030b5…` (pi, w3:p2) and `duo-verifier-lrr_1a7030b5…` (claude, w3:p3) — matches the fixed-build transcript.

## No cleanup on spawn failure: stray pane + committed session
- ran: (same failed build_and_verify launch)
- got: pane `w2:p5` (terminal `term_659ced678b1e26`, cwd ~/Code/duo) left behind with no agent — the verifier pane was created, then agent.start failed, and nothing tore it down. Session `ses_06553833e0edf8e71eba97e0c9a49aa1` committed to the store with the builder-leaf pi agent running in `w2:p4`; no `session remove` exists.
- expected: (nothing else — this is the documented design)
- status: known, confirmed (sharp edge 10). Stray closed with `herdr pane close w2:p5` after user sign-off (see day log).

## `recovering` view on every session after creation
- ran: `duo session list`
- got: all three sessions VIEW=`recovering` on the command after their creation.
- expected: same — documented one-shot-CLI behavior.
- status: known, confirmed (sharp edge 3 / concepts.md).

## Policy default refuses with two live instances (correct, but worth knowing)
- ran: `duo session launch orchestrator --dry-run` (no --host, bare shell, unbound workspace)
- got: `Host deduction produced no candidate host for this launch.` exit 1, with the ways-out rail; doctor's trail shows discovery finding both `.setup` and `duo` sockets.
- expected: same — discovery never guesses between several instances.
- status: known, confirmed (concepts.md deduction ladder). Day-1 friction for anyone with a `.setup` session lying around: every pre-bind launch from a bare shell needs `--host`.

## Detach on a launched session works on 4ced46d (was refused on 53ddced)
- ran: `duo session detach ses_156959299a047c7a3d8693f7a1ddf3fe --reason "retest: attachment recorded at launch"` (a *launched* multi-leaf session, binary 4ced46d)
- got: `attachment detached`, exit 0.
- expected: same — commit 8c8deac ("record the host attachment from the launch's own spawn") + 4ced46d's evidence note say launched sessions now carry an attachment.
- status: fix confirmed. Supersedes the day-log #13 refusal (`has no host attachment` on 53ddced).

## OPEN: reattach on a launched session refuses the documented fingerprints
- ran: `duo session reattach ses_156959299a047c7a3d8693f7a1ddf3fe --integration-instance herdr:duo --epoch-kind herdr.terminal_id --epoch-value <terminal-id> --epoch-scope pane --container <pane-id>` — tried BOTH leaves' fingerprints exactly as `herdr pane list` reported them (builder: term_659cf111588327 / w3:p2, then verifier: term_659cf1124fe578 / w3:p3), the flow procedure 03 prescribes for the drill.
- got: both attempts: `duo: session reattach: domain: enrollment conflict: observed fingerprint is not the claim this session holds; a new execution needs a new runtime instance`, exit 1.
- expected: one of the two panes to match the claim recorded at launch, since the panes/terminals were the launch's own spawns and neither pane was recreated (verifier's claude was idle+ready continuously; builder's pi showed interactive_ready=false at one point, so an agent restart changing process identity is a possible explanation for that leaf — but not for the verifier).
- open questions for the orchestrator: what fingerprint does the launch-recorded claim actually hold (which leaf's pane? the spawn's process-birth set?), and can the operator reproduce it from `herdr pane list` alone? If the claim needs the process-birth set, the drill instructions and the reference docs need to say so.
- consequence: ses_156959… was left detached (observation disabled); its panes were closed at cleanup, so nothing is lost. On this build the detach→reattach recovery drill still only round-trips on an *enrolled* session.
- status: new (binary 4ced46d).

### Resolution (traced 2026-08-25 on the connected machine, branch `go` at a91a0ff)
- root cause: **the operator's reattach fingerprint lacks the process-birth tuple, and the claim key includes it.** `recordLaunchAttachments` → `liveRuntimeFingerprint` (`internal/cli/hostbind.go`) records each leaf's claim from the spawn's own evidence: `herdr:<session>` + epoch `herdr.terminal_id`/pane/`<terminal_id>` + container `<pane_id>` + `pid=<agent pid> started=<ms UTC>`. `Authority.Reattach` (`internal/domain/lifecycle.go`) hashes the supplied fingerprint with `Fingerprint.ClaimRef()` (`internal/domain/fingerprint.go`) and does an exact lookup in the active-claim index. `ClaimRef()` appends PID + start time to the digest whenever the process birth is present. A reattach without `--process-pid` / `--process-started-at` therefore digests to a different key, is not found, and is reported as `enrollment conflict`. Not a stale or wrong claim: the terminal_id and pane_id typed were correct.
- why the enrolled session round-tripped: `session enroll` was run without process flags, so its claim is degraded (no process birth), and a process-less reattach digests to the same key.
- why both leaves failed: same missing tuple on both attempts. Claims are held per leaf and reattach only checks `held.Session == id`, so either leaf would have matched with the full tuple. Last-leaf-wins on `Session.Attachment` was not the cause.
- what would have worked: the flags the project's own test uses (`TestDetachAndReattachSucceedOnALaunchedSession`, `internal/cli/session_launch_attach_test.go`): the five documented flags **plus** `--process-pid <pid>` and `--process-started-at <start>`, start time in `2006-01-02T15:04:05.000Z` (millisecond UTC, `materialize.CaptureTimeLayout`).
- answer to "can the operator reproduce it from `herdr pane list` alone": **no.** `pane list` gives pane_id and terminal_id only; `session show` prints no attachment fields; `workspace host show` prints a process birth only for the first-bind correlation (the orchestrator pane, not this session's). Nothing an operator can read yields the millisecond start-time string.
- was it already known / fixed: **known, not fixed.** `4ced46d`'s own evidence `16-detach-reattach-launched.txt` reproduces the identical conflict and its message calls it "the show-fingerprint follow-on"; `docs/cli/decisions.md` (from 8c8deac) lists it as a standing limit ("Reattach's fingerprint is not readable from any verb… surfacing them on `session show` is the obvious next step"). None of the five commits after 4ced46d (launch placement, cwd-correlation, close-on-exit) touch `session_detach.go`, `session_show.go`, `fingerprint.go`, or `lifecycle.go`.
- doc gap: `procedure/03-daily-use.md` ("fingerprints from `herdr pane list`") is complete only for a process-less enrolled session. For a launched session the drill needs the process tuple, which today no verb exposes.
- proposed fix (taken up by the user on the connected machine): make `duo session show <id>` print the current attachment's fingerprint, including process birth, ideally as a ready-to-paste `duo session reattach …` line, mirroring the rebind pointer `launch` already prints. This closes the finding without changing claim semantics. A looser reattach match (epoch + container, no process) is a separate domain decision, because the process birth is what proves execution continuity across a restart.
- status: **root-caused; fix pending (session show).**

## OPEN: launched sessions never leave `active` / `starting`; the store has no exit path and no prune verb
- ran: `duo session list` on 2026-08-25 (binary `a91a0ff`), the morning after all agents had been exited by hand and every duo-spawned pane closed.
- got: 25 rows, every one `LIFECYCLE=active VIEW=recovering`. `session show` on each: runtime instance state `starting` for all 24 launched sessions, `live` for the one enrolled session. Both Herdr servers checked live: the `duo` server has one plain shell pane and no agents; `xesapps-demo` has only the user's own interactive Claude Code, which is not in the store. Nothing duo launched is running.
- expected: sessions whose agent exited to read `inactive` (or at least an instance state of `exited`), and a way to prune the ledger.
- where the 25 came from (audit_log + `~/.bash_history` agree; one row per invocation, by design): 5 from the dogfood package run in `~/Code/duo` (13:05–13:22 CDT, transcript `18b0716a`); 9 typed by hand in `~/Code/duo` 13:29–15:48 CDT (launch-placement / cwd-correlation work); 11 typed by hand in `~/Code/xesapps-demo` 17:44–17:47 CDT (six `builder`, five `builder --close-on-exit`; the first one cold-bound that workspace). No launch created more than one session; the multi-leaf launches created one session with two attachments.
- root cause (traced in the repo): **no code outside tests ever calls `Authority.Exit`, `MarkLive`, `LaunchFailed`, or `ResolveRecovery`.** The store holds zero `instance.state` facts. Duo never learns an agent went live and never learns it exited. `--close-on-exit` closes the Herdr pane but never reports to duo. The `recovering` view is separate and documented (sharp edge 3: `Authority.Open` marks every non-terminal instance recovering on every load, `internal/domain/authority.go:131`). `docs/cli/decisions.md:120–140` already names this gap and assigns "`ResolveRecovery`'s real caller" to Step 22.
- both halves exist, neither is wired: the Herdr adapter has `ValidateAttachment` (`internal/host/herdr/host.go:508`: pane exists → same terminal_id → same process birth) and the domain has `ResolveRecovery` with §4.4's outcomes (`internal/domain/recovery.go`). No composition-root code joins them.
- no prune verb, and none would work alone: `session.remove` is in the registry (`internal/registry/table.go:239`) but has no cobra command. `Authority.Remove` needs `archived`; `Archive` needs `inactive` and no live instance; `inactive` is reached only through `exitInstance`. So the exit path is the prerequisite for any cleanup.
- consequence: `session list` is a ledger of every launch ever made, not a view of what runs, and it grows without bound. 25 live-runtime claims are held on panes that no longer exist (harmless for now: each claim includes process birth, so a reused pane cannot collide). Any future feature that reads `active` as "running" will be wrong.
- minor oddity, not chased: five `--close-on-exit` launches, three harness dirs under `~/.local/share/duo/harness/`.
- status: new. Handoff: `records/handoff-session-exit-reconciliation.md`.

### Shell history for the hand-typed launches (from `~/.bash_history`, epoch stamps → CDT)
Several shells appended at exit, so line order is not time order; sorted by time here. Not every typed
launch committed a session: `-h`, `-v`, `buildera` typos, and the 13:28:54 orchestrator have no audit row.
The audit_log in `duo-db-export.sql` is the authority on what committed.

```
line  time (CDT)         command
 1043 08-24 10:46:13  git clone git@github.com:procrastivity/duo.git
 1045 08-24 10:46:16  cd duo
 1053 08-24 11:13:21  vi ~/.config/herdr/config.toml 
 1057 08-24 11:13:45  herdr update
 1059 08-24 11:13:57  herdr server reload-config
 1063 08-24 11:16:08  vi ~/.config/herdr/config.toml 
 1071 08-24 11:22:09  systemctl restart --user herdr-session@*
  931 08-24 13:28:06  duo workspace host show
  933 08-24 13:28:54  duo session launch orchestrator
  955 08-24 13:29:20  duo session launch orchestrator
  935 08-24 13:30:10  cd Code/duo
  937 08-24 13:30:11  duo session launch orchestrator
  941 08-24 13:38:26  duo session launch builder --avoid model_family=claude
 1213 08-24 14:16:55  duo session
 1215 08-24 14:16:58  duo session launch
 1217 08-24 14:17:01  duo session launch -h
 1219 08-24 14:17:34  duo session launch -v orchestrator
 1083 08-24 14:18:08  duo doctor
  973 08-24 14:18:46  duo doctor
  975 08-24 14:20:11  duo doctor --output=json
 1085 08-24 15:27:53  duo doctor
 1087 08-24 15:28:00  ls /home/bsimensen/.config/herdr/sessions/.setup/
 1089 08-24 15:28:06  jq < /home/bsimensen/.config/herdr/sessions/.setup/session.json 
 1247 08-24 15:45:23  dev duo
 1227 08-24 15:45:52  duo version
 1197 08-24 15:46:05  duo doctor
 1229 08-24 15:46:53  duo session launch builder
 1231 08-24 15:47:02  duo session launch builder
 1233 08-24 15:47:06  duo session launch builder
 1235 08-24 15:47:09  duo session launch buildera
 1237 08-24 15:47:11  duo session launch buildera
 1239 08-24 15:47:13  duo session launch builder
 1241 08-24 15:48:24  duo session launch builder -h
 1243 08-24 15:48:55  duo session launch builder
 1187 08-24 15:56:50  vi ~/.config/duo/duo.config.yaml 
 1519 08-24 17:44:46  duo session launch builder
 1521 08-24 17:45:04  duo session launch builder
 1523 08-24 17:45:07  duo session launch builder
 1525 08-24 17:45:10  duo session launch builder
 1527 08-24 17:45:12  duo session launch builder
 1529 08-24 17:46:04  duo session launch builder
 1531 08-24 17:46:40  duo session launch builder --close-on-exit
 1533 08-24 17:46:43  duo session launch builder --close-on-exit
 1535 08-24 17:47:17  duo session launch builder --close-on-exit
 1537 08-24 17:47:21  duo session launch builder --close-on-exit
 1539 08-24 17:47:24  duo session launch builder --close-on-exit
 1511 08-24 17:48:23  duo 
```

### Store export
The full authority store is exported beside this file as `duo-db-export.sql` (complete SQLite dump: 25 sessions,
270 facts, 52 audit rows). Provenance, schema version, build lineage, sensitivity note (credential fingerprints
only, no secrets), and restore steps are in `duo-db-export.md`.
