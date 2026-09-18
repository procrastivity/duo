# Portable launcher skill installation and authority preflight

> Status: **Stage 1 contract for the Amp, OpenCode, and Codex outer-launcher
> conformance Matter.** This document specifies Stage 2; it does not claim
> that the installer or readiness report already exists.

This contract specializes the general projection rules in
[`duo-vnext-installation-contract.md`](./duo-vnext-installation-contract.md)
for one shared filesystem skill. Amp, OpenCode, and Codex are outer drivers of
the existing Duo CLI. This contract adds no runtime adapter, plugin, hook, MCP
surface, remote authority, or terminal-input fallback.

## 1. Observed baseline versus the contract

The distinction is important because several useful seams exist, but the
portable projection does not.

### 1.1 Existing behavior in this checkout

- [`../../skills/duo-delegation-loop/SKILL.md`](../../skills/duo-delegation-loop/SKILL.md)
  is the normative authored skill. The implemented projection is 6,284 bytes
  with SHA-256
  `6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182`.
- The 2026-09-17 live probe in
  [`../../evidence/portable-launcher-conformance/step-01/report.md`](../../evidence/portable-launcher-conformance/step-01/report.md)
  proved project discovery at
  `.agents/skills/duo-delegation-loop/SKILL.md` for Amp
  `0.0.1789675234-g2899fe`, OpenCode `1.18.31`, and Codex CLI `0.154.0`.
  Both a copied directory and a directory symlink worked on the probed Linux
  host. The repository-relative symlink target
  `../../skills/duo-delegation-loop` was also live-confirmed. The checkout did
  not itself contain `.agents/`.
- `internal/asset` resolves shipped or embedded assets;
  `internal/manifest` inventories asset digests and declares the currently
  empty `harness_targets` array. `internal/manifest.Stamp` and
  `internal/manifest.Drift` are older, smaller primitives using
  `.duo-manifest-stamp.json`; they do **not** implement the normative
  `duo.projection-stamp/v1` ownership record.
- `contracts/schemas/duo-projection-stamp-v1.schema.json` and the installation
  contract define `.duo-generated.json`. `internal/runtime/devin` has a
  target-local implementation of that pattern. It is useful precedent, not
  the owner of this launcher-neutral skill.
- `internal/doctor` owns authority-store diagnostics. `internal/cli/doctor.go`
  composes those diagnostics with effective-path config inspection, selected
  workspace and host deduction, and other runtime-specific sections. It does
  not inspect this skill and it does not ping the deduced Herdr server.
- Contrary to the rule established below, current `duo doctor` can write: it
  calls `internal/doctor.SweepHarnessDirs`, `store.Open` may migrate a present
  database, and the writer probe transiently acquires a lease. Stage 2 must
  remove those effects from the diagnostic path rather than describing the
  current command as already read-only.
- There is no `duo install` command and no portable launcher target in the
  operation registry or manifest.

### 1.2 Contract introduced by this document

The installed product projects one stamped **copy** of the canonical bytes
into a selected workspace. A source checkout may separately use the proven
relative symlink as a development convenience, but that symlink is not a
product installation and conveys no Duo ownership.

The product target is named `portable_launchers` in JSON and
`portable-launchers` on the CLI. Its only payload is the filesystem skill:

```text
<workspace>/.agents/skills/duo-delegation-loop/
├── SKILL.md
└── .duo-generated.json
```

There is no per-launcher copy. The project target is normative; the shared
user root `~/.agents/skills` remains observed fallback behavior, not an
installation target in this Matter.

## 2. Canonical artifact and public identity

The public identity uses **both** an authored format version and a digest:

| Field | Value and rule |
|---|---|
| `name` | `duo-delegation-loop` |
| `format_version` | `duo.skill/duo-delegation-loop/v1` |
| `content_digest` | `sha256:` followed by the lowercase SHA-256 of the exact installed `SKILL.md` bytes |
| `media_type` | `text/markdown` |

The baseline identity is therefore
`duo.skill/duo-delegation-loop/v1@sha256:6f8bc16b656f564a949c99f12d4cf0bcd04ec2c9b49a8b74fb65b2e355b5d182`.
That digest is a baseline pin, not a promise that Stage 2 will leave the
manual-install prose unchanged. If Stage 2 changes any byte, the manifest,
stamp, doctor, and evidence must report the new digest.

`format_version` changes only for an incompatible change to the skill's
consumer contract or installed shape. Editorial or command-guidance changes
within the same shape retain `v1` and necessarily change `content_digest`.
The digest is over bytes as packaged, with no line-ending normalization and
no trailing-newline insertion.

The authored source remains
`skills/duo-delegation-loop/SKILL.md`. Product packaging must expose those
same bytes as the logical shipped asset
`skills/duo-delegation-loop/SKILL.md` through `internal/asset.ReadDefault`
(not the override-first `Resolve` path); a separately edited copy under
`assets/` is forbidden. The installed product, including the embedded fallback
used by a bare binary, must therefore work without a source checkout. How the
build embeds that source is a packaging detail, but the byte-equality test
between authored source, resolved shipped asset, and rendered `SKILL.md` is
mandatory.

Rendering is the identity transform: no path, version, launcher, prompt, or
configuration value is substituted into `SKILL.md`. The projection stamp is
the generated marker. No comment is prepended to `SKILL.md`, because skill
YAML frontmatter must remain at byte zero.

`duo doctor --output json` and every conformance capture report
`name`, `format_version`, and prefixed `content_digest`. Captures additionally
record the Duo version/commit, manifest digest, launcher executable identity
and version, host/Herdr pin, and observation time. A bare digest or a format
version alone is not a complete skill identity.

## 3. Ownership boundaries

Stage 2 extends existing seams rather than creating a launcher subsystem.

| Concern | Owner |
|---|---|
| Authored canonical bytes | `skills/duo-delegation-loop/SKILL.md` |
| Installed/embedded byte resolution | `internal/asset`; user overrides are not allowed for this normative skill |
| Identity rendering, normative stamp parsing, path validation, drift classification, and non-destructive placement | `internal/manifest`, replacing or widening its bootstrap stamp helpers rather than adding a parallel projection package |
| Static asset digest and the single shared target declaration | `internal/manifest.Build` and `duo.manifest/v1` |
| `duo install portable-launchers` command wiring | `internal/cli`; it delegates all rendering and mutation policy to `internal/manifest` |
| Read-only projection and authority diagnosis | `internal/doctor`, composed and rendered by `internal/cli/doctor.go` |
| Herdr selection and non-mutating reachability probe | existing `internal/launch/materialize` deduction plus `internal/host/herdr.Factory.Probe` |
| Executable registration | `internal/cli.NewRootCommand`; `cmd/duo/main.go` remains only the entry point |

`internal/runtime/devin` stays the owner of the Devin launch-time projection.
Its private stamp structs and status constants must not become the portable
skill API, and the portable installer must not import a runtime package.

The projection root is the skill directory, not `.agents` or
`.agents/skills`. The stamp owns only `SKILL.md`; unrelated siblings and
unlisted files are never removed. A first successful install mints a
non-empty opaque `installation_id`. Repair preserves that ID. `generated_at`
changes only after a successful placement.

The stamp is `duo.projection-stamp/v1` with this specialization:

```json
{
  "schema": "duo.projection-stamp/v1",
  "product_version": "<duo version>",
  "manifest_digest": "sha256:<64 lowercase hex>",
  "projection_format": "duo.skill/duo-delegation-loop/v1",
  "target": {
    "harness": "portable_launchers",
    "tested_version_range": "amp=0.0.1789675234-g2899fe;opencode=1.18.31;codex=0.154.0",
    "launchers": [
      {"name": "amp", "tested_versions": ["0.0.1789675234-g2899fe"]},
      {"name": "opencode", "tested_versions": ["1.18.31"]},
      {"name": "codex", "tested_versions": ["0.154.0"]}
    ]
  },
  "components": ["filesystem_skill"],
  "installation_id": "<opaque stable ID>",
  "generated_at": "<RFC3339 UTC>",
  "source_assets": [
    {
      "path": "skills/duo-delegation-loop/SKILL.md",
      "digest": "sha256:<canonical content digest>"
    }
  ],
  "files": [
    {"path": "SKILL.md", "digest": "sha256:<installed content digest>"}
  ]
}
```

The `launchers` array is ordered `amp`, `opencode`, `codex`; `tested_versions`
contains exact live pins, not inferred ranges. Stage 2 widens the existing
stamp schema's `target.harness` enum to include `portable_launchers`, schemas
the launcher rows, and adds the required closed `components` array. For this
target it contains only `filesystem_skill`. Slash-separated stamp paths are
relative to the projection root and must be clean, non-empty, non-absolute,
and contain no `..`. An implementation must reject duplicate paths and any
stamp that tries to claim the stamp itself or a path outside the root.

## 4. States, precedence, and non-destructive transitions

The closed projection state enum is:

```text
current | missing | stale | modified | incompatible | unowned_conflict
```

Inspection uses `lstat` and never follows a destination symlink when deciding
ownership. `SKILL.md` must be a regular file. The deterministic precedence
below prevents a mixed tree from receiving an optimistic state.

1. **`incompatible`** — a stamp is present but malformed, has an unknown
   schema or newer/unknown projection format, names another target, contains
   unsafe/duplicate paths, or the target has an unsupported filesystem shape.
   This includes a stamped symlink where the product requires a copied regular
   file. No write is allowed. This specializes the general installation
   contract for a shared target with no singular harness executable: outer
   launcher-version compatibility is evaluated separately against the
   manifest pins and does not change ownership or byte drift.
2. **`modified`** — a valid recognized stamp claims an existing regular file,
   but its bytes do not match the digest in that stamp. No write is allowed.
3. **`unowned_conflict`** — an expected destination exists but no valid stamp
   claims it. A directory symlink, including the proven repository-relative
   development symlink, is unowned for product-install purposes. No write is
   allowed. The stable conflict code is
   `projection.user_file_conflict`.
4. **`missing`** — no expected destination blocks placement, or a valid stamp
   owns an expected path that is absent. An absent target tree and an empty
   pre-existing target directory are both `missing`.
5. **`stale`** — all existing files still match a valid recognized prior
   stamp, but that stamp's Duo manifest, product version, source-asset digest,
   file set, or known older projection format differs from what the current
   binary would produce.
6. **`current`** — the valid stamp, current manifest and format, canonical
   source digest, complete expected file set, and installed file bytes all
   agree.

An outer launcher version outside the manifest's exact tested pins is an
unsupported conformance claim, but it does not mutate otherwise-current
files. Captures must reject such a run as launcher-incompatible. Doctor may
report that version when it is supplied or detected, but it must not guess
which outer launcher invoked it.

### 4.1 Install and repair

The public write surface is:

```text
duo install portable-launchers [--workspace PATH] [--repair] [--output text|json]
```

`--workspace` resolves exactly as the launch command does: the flag, else the
current directory, made absolute and cleaned. Automated tests set all roots to
temporary directories; they never write a real home skill tree.

- Plain install writes only a fresh `missing` projection for which neither
  expected destination exists. `current` is an idempotent no-op. If a stamp or
  partial owned projection already exists, plain install makes no change and
  tells the operator to inspect or use `--repair`.
- `--repair` is an idempotent no-op for `current`. It may create a fresh
  `missing` projection, restore a missing file that a valid stamp owns, or
  replace `stale` files only after every existing file claimed by the prior
  stamp still matches that stamp. It may add a newly expected file only when
  its destination is absent.
- Neither form writes in `modified`, `incompatible`, or `unowned_conflict`.
  There is no force flag. `modified` raises `projection.modified`;
  `unowned_conflict` raises `projection.user_file_conflict`; both retain
  `effect: no_effect`. Incompatible input raises the new stable
  `projection.incompatible` code (class `unsupported`, effect `no_effect`) and
  tells the operator to upgrade Duo or move the unknown projection for manual
  review. A partial owned projection passed without `--repair` raises
  `invalid.precondition` with no write.
- Repair may remove an obsolete path only when the prior valid stamp claims
  it and its current digest still matches. It never removes an unlisted file
  or any parent directory it did not create and find empty.
- Rendering occurs in a private sibling staging directory. Payload files are
  fsynced and placed by same-filesystem rename; the stamp is the final commit
  marker. A crash before the stamp commit can only yield a safe non-current
  state on the next inspection, never permission to overwrite user bytes.

The installer validates the selected workspace and every ancestor before
writing. It refuses symlink traversal through the managed destination and
does not resolve a target outside the selected workspace.

Successful creation, repair, and the `current` no-op exit `0`; the three
non-destructive conflicts and `invalid.precondition` exit `1`; malformed CLI
usage exits `2`; an unexpected staging, fsync, or placement failure exits `4`.
JSON success reports `target`, `state`, `changed` (boolean), `root`,
`installation_id`, `format_version`, and `content_digest`. A failure uses the
existing structured error envelope and writes no success result.

### 4.2 Development projection versus installed projection

For repository development on the probed Linux host, this remains valid:

```text
.agents/skills/duo-delegation-loop -> ../../skills/duo-delegation-loop
```

It keeps edits live and is suitable for deterministic discovery tests. It is
not portable product state: it depends on source-tree layout, cannot carry a
stamp inside the symlink without writing into the authored source, and has no
safe uninstall/repair ownership. Product installation therefore always uses
the stamped copied form. The installer reports a development link at the
product destination as `unowned_conflict` and asks the operator to keep it,
move it, or remove it explicitly; it never converts the link in place.

## 5. Manifest contract

`duo manifest --output json` declares exactly one shared target in
`harness_targets`. Stage 2 extends `internal/manifest.HarnessTarget` to these
stable fields:

```json
{
  "name": "portable_launchers",
  "status": "unverified",
  "scope": "workspace",
  "discovery_root": ".agents/skills",
  "projection_root": ".agents/skills/duo-delegation-loop",
  "stamp_file": ".duo-generated.json",
  "projection_format": "duo.skill/duo-delegation-loop/v1",
  "components": ["filesystem_skill"],
  "artifact": {
    "name": "duo-delegation-loop",
    "media_type": "text/markdown",
    "source_asset": "skills/duo-delegation-loop/SKILL.md",
    "output_path": "SKILL.md",
    "content_digest": "sha256:<64 lowercase hex>"
  },
  "launchers": [
    {"name": "amp", "tested_versions": ["0.0.1789675234-g2899fe"]},
    {"name": "opencode", "tested_versions": ["1.18.31"]},
    {"name": "codex", "tested_versions": ["0.154.0"]}
  ]
}
```

The array orders shown above are stable. `status` is the closed enum
`supported | unverified`; `scope` is `workspace`; `components` contains only
`filesystem_skill` in this Matter. The canonical asset also appears in the
manifest's existing `assets` array. The target's prefixed
`artifact.content_digest` is the public artifact identity; the legacy
`assets[].sha256` field may retain its existing unprefixed 64-hex encoding for
wire compatibility.

Stage 2 emits `unverified`: discovery is pinned and the filesystem component
exists, but the full launch/send/observe suite has not passed. Stage 6 may
change it to `supported` only after all three pinned launchers pass the common
suite and the release matrix is sealed. Manifest status therefore cannot be
mistaken for an end-to-end support claim.

The manifest declares tested launcher pins, not installed launcher presence
or live authority readiness. No hook, plugin, MCP component, terminal paste,
or launcher-specific operation appears in this target.

## 6. Reachable local-authority preflight

The common skill adds a phase zero before `session launch`: invoke

```text
duo doctor --workspace <the launch workspace> [--config PATH] [--host KIND[:INSTANCE]] --output json
```

using the same workspace, config, and host override intended for launch. The
new optional `--config` and `--host` flags on doctor must share the launch
command's parsers and precedence; they are not a second selection algorithm.
If the caller omits them, doctor uses the same defaults as launch.

Doctor evaluates independent checks where possible and marks a dependent
check `not_checked` when its prerequisite failed. It does not stop at the
first finding. A launcher proceeds to `duo session launch` only when
`launcher_preflight.status` is `ready`.

### 6.1 Required checks

| Ordered check ID | What must be proved | Failure action |
|---|---|---|
| `duo_executable` | The running executable resolves to a local path and reports non-empty version, commit, and build-date identity. `dev` is allowed when the commit is reported. | Run the intended installed Duo binary and record its identity. |
| `effective_config` | The exact config path launch would use loads and validates as `duo.config/v3`; report its deterministic effective digest and available preset names. | Install/fix v3 config or run the documented v2 migration; never edit it during doctor. |
| `authority_store` | The XDG-selected store path is locally addressable. A missing store is `initializable` and passes because the first real write owns creation. A present store must be readable, schema-compatible, replayable, and not held by another unexpired writer lease. | Fix path/schema/corruption, or wait for/stop the named writer through its normal lifecycle. |
| `workspace` | The selected path is absolute, exists, is a directory, and is the same root used for launch and skill lookup. | Pass the actual project root with `--workspace`; do not silently substitute another checkout. |
| `host_selection` | Existing M1 materialization deduces exactly one enabled host using explicit flag > workspace correlation > cwd correlation > ambient environment > policy default. | Supply `--host`, repair/rebind the workspace correlation, or correct host policy. |
| `host_reachability` | The selected host is reachable through a bounded, non-mutating adapter probe. For Herdr this is the existing ping path at the selected Unix socket. | Start/select the named Herdr server or make its socket reachable in this execution environment. |
| `skill_projection` | The selected workspace's copied projection is `current` for the manifest identity in section 2. | Run the installer/repair when allowed; move modified or unowned content before retrying. |

Host compatibility evidence is reported separately from reachability. A
reachable Herdr with a pin/schema mismatch is `warning` information in this
preflight because the existing adapter admits `unverified`; a conformance
capture still cannot claim the pinned supported matrix unless Herdr version,
protocol, and schema digest match the accepted evidence. Provider standing
facts, scrub-gate survivors, active/recovering counts, launcher executable
pins, and unrelated runtime projections are informational unless another
contract makes them a launch refusal.

### 6.2 Stable doctor JSON

`doctor.run` remains the public operation. Its JSON report grows by one
top-level sibling; existing fields are not renamed:

```json
{
  "launcher_preflight": {
    "schema": "duo.launcher-preflight/v1",
    "status": "ready",
    "authority_scope": "local",
    "checks": [
      {
        "id": "duo_executable",
        "stage": "executable",
        "required": true,
        "status": "pass",
        "code": "ok",
        "summary": "running Duo executable has a reportable build identity",
        "action": ""
      }
    ],
    "duo": {
      "executable_path": "/absolute/path/to/duo",
      "version": "<string>",
      "commit": "<string>",
      "build_date": "<string>"
    },
    "config": {
      "path": "/absolute/path/to/duo.config.yaml",
      "schema": "duo.config/v3",
      "effective_digest": "sha256:<64 lowercase hex>",
      "valid": true,
      "presets": ["builder"]
    },
    "authority": {
      "path": "/absolute/path/to/duo.db",
      "state": "ready",
      "present": true,
      "healthy": true,
      "schema_version": 1,
      "writer_active": false
    },
    "workspace": {
      "requested_path": "<caller value or empty string>",
      "selected_path": "/absolute/clean/path",
      "source": "flag",
      "exists": true,
      "directory": true
    },
    "host": {
      "selected": true,
      "kind": "herdr",
      "instance_id": "herdr:<instance>",
      "instance_label": "/path/to/herdr.sock",
      "host_source": "workspace-correlation",
      "reachable": true,
      "detected_version": "0.8.2",
      "protocol_identity": "herdr-socket-api/20",
      "compatibility": "supported"
    },
    "skill_projection": {
      "target": "portable_launchers",
      "name": "duo-delegation-loop",
      "format_version": "duo.skill/duo-delegation-loop/v1",
      "content_digest": "sha256:<64 lowercase hex>",
      "root": "/workspace/.agents/skills/duo-delegation-loop",
      "file": "/workspace/.agents/skills/duo-delegation-loop/SKILL.md",
      "stamp": "/workspace/.agents/skills/duo-delegation-loop/.duo-generated.json",
      "state": "current",
      "installation_id": "<opaque ID or empty string>"
    }
  }
}
```

All displayed fields are always present, using empty strings, `false`, zero,
or empty arrays when evidence is unavailable; consumers must not infer state
from omission. `checks` always uses the required-check order in section 6.1.

Closed enums are:

- overall `status`: `ready | not_ready`;
- `authority_scope`: `local | unavailable`;
- check `status`: `pass | fail | warning | not_checked`;
- check `id`: the seven IDs in section 6.1 followed by exactly one
  informational check, `host_compatibility`;
- `stage`: `executable | config | authority | workspace | host_selection |
  host_reachability | skill_projection`;
- authority `state`: `ready | initializable | writer_active | incompatible |
  unhealthy | unavailable`;
- workspace `source`: `flag | cwd`;
- host `compatibility`: `supported | unverified | incompatible | unavailable |
  unknown`;
- skill projection `state`: the six values in section 4.

`checks` therefore contains exactly eight rows. `host_compatibility` is last,
has `stage: host_reachability` and `required: false`, and is `pass` only for
the pinned supported evidence, `warning` for a reachable unverified or
incompatible version, and `not_checked` when no host answered. Adding another
check ID requires a preflight schema-version change rather than silently
growing this closed list.

`code` is `ok` on pass. Failures use stable dotted findings, including
`config.missing`, `config.invalid`, `authority.writer_active`,
`authority.unavailable`, `workspace.invalid`, `launch.host_unresolved`,
`host.unreachable`, `projection.missing`, `projection.stale`,
`projection.modified`, `projection.incompatible`, and
`projection.user_file_conflict`. `summary` and `action` are safe human text,
not parser inputs. An action names the failing stage, observed local resource,
and one concrete next step; it never includes credentials or raw environment
values.

The effective config digest is SHA-256 over the deterministic JSON encoding
of the validated configuration value launch consumes, with a `sha256:`
prefix. Preset names are sorted. This is an observation of effective intent,
not a copy of config secrets.

### 6.3 Human output

Human mode contains a `launcher preflight: ready` or
`launcher preflight: not ready` heading, followed by the seven checks in the
same order. Each line begins `[pass]`, `[fail]`, `[warning]`, or
`[not checked]`, then the check ID and summary. Every non-pass required check
has an indented `action:` line. The projection line prints the state and full
artifact identity. JSON fields and enums are the stable interface; sentence
wording may improve without a schema change.

### 6.4 Exit and refusal semantics

`duo doctor` preserves its diagnostic command semantics: exit `0` means a
complete report was produced, not that every check passed. Usage remains `2`,
and an internal failure that prevents a trustworthy report remains `4`.
Callers gate on `launcher_preflight.status`, not process exit alone.

When status is `not_ready`, the common skill refuses before launch and reports
the failed stage(s) and actions. It does not invoke `session launch`, create an
authority store, install/repair the skill, switch workspace, select a remote
authority, enable a plugin or MCP route, or paste into a terminal.

“Local” is established by reachable resources, not by vendor-environment
heuristics. Duo does not attempt to recognize every orb/container product by
environment variable. An orb or remote execution context without the selected
workspace, effective config/store paths, current copied projection, and
reachable selected host reports `authority_scope: unavailable` and
`not_ready`. An isolated environment in which those exact resources are
deliberately mounted and the local Herdr socket answers is operationally local
and may pass. There is no network fallback to an authority on another host.

### 6.5 No-write diagnosis

Doctor and every helper it calls for this preflight have zero persistent
mutation. In particular they must not:

- create or migrate the authority database;
- acquire even a transient authority-writer lease;
- enroll or bind a workspace;
- reap harness directories;
- create a target directory, stamp, or skill file;
- repair config or projection drift; or
- change Herdr or launcher state.

Stage 2 must use a genuinely read-only store/schema/lease inspection path and
replace the current doctor-time harness sweep with reporting only. A bounded
Herdr ping and schema/version query are non-mutating protocol reads and are
allowed. Install and cleanup remain explicit write operations.

## 7. Compatibility and migration rules

1. **Current manual installs are unowned.** An unstamped copied `SKILL.md` or
   source-tree symlink is `unowned_conflict`, even if its bytes match the
   canonical digest. Duo never silently adopts it. The operator moves/removes
   it and then runs `duo install portable-launchers`.
2. **Bootstrap stamps do not transfer ownership.** A
   `.duo-manifest-stamp.json` from `internal/manifest.Stamp` is not a
   `duo.projection-stamp/v1` record and cannot authorize overwrite. At the
   portable target it is unrelated or conflicting user content.
3. **Known older output migrates by repair.** A valid, recognized older
   projection stamp whose claimed files still match is `stale` and may be
   regenerated with `--repair`. The installer preserves `installation_id` and
   writes the new stamp last.
4. **Unknown/newer formats never downgrade.** An old Duo binary seeing an
   unknown projection format reports `incompatible` and writes nothing.
5. **Product upgrades are digest-driven.** A changed product/manifest alone
   makes an intact owned projection `stale`; a changed canonical skill digest
   also makes it `stale`. Repair never needs a launcher-specific migration.
6. **Launcher support is evidence-pinned.** The three exact versions in this
   document are the initial supported matrix. A newer launcher is not assumed
   compatible merely because it still finds `.agents/skills`; re-run live
   discovery and the launcher-neutral suite, then append the exact version to
   `tested_versions` without changing the skill format unless its consumed
   contract changed.
7. **The manual-install prose must converge.** When Stage 2 lands the
   installer, the normative skill and operator guidance must stop claiming
   that no renderer exists. Hand-managed Claude Code/Cursor instructions are
   not silently converted and remain outside this three-launcher target.
8. **No Windows support is inferred.** Copy installation avoids dependence on
   symlink support, but the accepted discovery evidence and relative-link
   confirmation are Linux-only. A Windows support claim needs its own live
   evidence; it does not change the safe copy/stamp rules.

## 8. Stage 2 acceptance consequences

Implementation is conformant only if isolated-root tests cover all six
projection states, state precedence, fresh install, idempotent current
install, allowed missing/stale repair, refusal without byte changes for
modified/incompatible/unowned conflicts, unsafe stamp paths, symlink
destinations, and crash-safe stamp-last placement. Manifest tests pin the
single target, its initial `unverified` status, and artifact identity. Doctor
tests pin every JSON field and enum, success and failure human sections, a
negative Herdr reachability case, and snapshots proving diagnosis creates,
modifies, and removes no file.

The live evidence for each launcher must cite the manifest target identity,
doctor-reported skill identity, exact launcher binary/version, Duo build,
Herdr compatibility evidence, selected workspace, and a `ready` preflight.
Discovery alone is not authority readiness, and authority readiness alone is
not end-to-end launcher conformance.

## 9. Conservative assumptions and open evidence

- Exact launcher versions are deliberately pins rather than semantic ranges;
  there is not enough live evidence to claim wider ranges.
- The current skill digest is retained as a baseline, while allowing Stage 2
  to update obsolete hand-install wording. The post-change digest becomes the
  identity everywhere in one commit.
- Missing authority state is treated as initializable because Duo currently
  has no separate initialization verb. This does not promise that a later
  write cannot fail; it says diagnosis observed no existing corrupt or locked
  authority.
- Duplicate skill-name precedence, launcher hot reload, Windows discovery,
  and user-global installation remain unproved and outside this contract.
- No cross-repo assumption is invalidated. This contract consumes and narrows
  the 2026-09-17 duo-lab assessment: it turns the confirmed shared discovery
  path into a product-owned copied projection and leaves runtime research in
  `duo-lab`.
