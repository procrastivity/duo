# Step 01 live launcher discovery evidence

- **Probe window:** 2026-09-17, completed 20:56:15Z
- **Host:** Linux 6.8.0-136-generic x86_64
- **Duo checkout:** `/home/dev/Code/duo` at `1437f2f69e71d164b4248ed121803e53e3e1a2ba`
- **Prior assessment pin:** duo-lab `caf02f7eb325b1c935d0cf793bc53fbb3a3f1a65`, dated 2026-09-17
- **Canonical skill tested:** `skills/duo-delegation-loop/SKILL.md`, 6,002 bytes, SHA-256 `4c84381b7b4a3454abd58a192bd158b3ae14cd7e1bcd7dba4c87d5d5ae9be30e`

## Result

The prior assessment's central claim is live-confirmed at the versions below:
**Amp, OpenCode, and Codex all discover a project skill at
`<worktree>/.agents/skills/duo-delegation-loop/SKILL.md`**, including when the
skill directory is a symlink. All three also discover the same skill from the
shared user root `~/.agents/skills/`.

The current Duo checkout itself does **not** yet contain `.agents/`; its
canonical skill exists only under `skills/duo-delegation-loop/`. Thus the
isolated probes prove that the proposed projection works, not that this
checkout already exposes the project skill.

Evidence strength:

- **Direct/deterministic:** executable identity, version output, CLI discovery
  output, Codex model-visible prompt input, copy/symlink behavior, and parent
  worktree traversal.
- **Strong behavioral invocation:** a minimal authenticated inference for each
  launcher explicitly loaded the named skill and returned its three phases and
  20-second timeout without running Duo. Amp and OpenCode emitted a `skill`
  tool event. Codex advertised the skill in model input, read `SKILL.md` through
  the projected symlink, and then followed it.
- **Not proved here:** a real `duo session launch` / `duo prompt send` cycle,
  hot reload after changing a skill in an already-running launcher, precedence
  when duplicate skill names exist, or Windows behavior.

## Installed launchers and executable identity

| Launcher | Live version output | Resolved executable identity | SHA-256 |
|---|---|---|---|
| Amp | `0.0.1789675234-g2899fe (released 2026-09-17T20:00:34.000Z, 45m ago)` | `command -v`: `/home/dev/.local/bin/amp`; symlink to `/home/dev/.amp/bin/amp`; ELF x86-64, 100,750,816 bytes | `f351217dac739614b4d9b3eacad728ce6d61647ab1f5d4a4de44d99aba6598ee` |
| OpenCode | `1.18.31` | `/home/dev/.opencode/bin/opencode`; ELF x86-64, 185,030,784 bytes | `f9dab32248695e9ebd56b16a1921798fd85112cf5a69c7dfd0cabc1e17be4a11` |
| Codex | `codex-cli 0.154.0` | `/home/dev/.local/bin/codex` → npm `bin/codex.js` (SHA-256 `61b0194f…`), which launches package `@openai/codex-linux-x64` `0.154.0-linux-x64`; native ELF at `.../vendor/x86_64-unknown-linux-musl/bin/codex`, 262,858,016 bytes | native: `3188814c35471432d4123203e0eb38e5bddc60226e3d7ddf0e59e649ea140022` |

`amp --version` and `amp version` agreed. `opencode --version` was authoritative;
`opencode version` is not a version command and attempted to open a directory
named `version`. `codex --version` was authoritative; `codex version` attempted
to start the TUI and failed noninteractively. Those latter two failures are CLI
shape observations, not version uncertainty.

## Isolated probe layout

All probe projects and homes were under
`/tmp/duo-launcher-probe-step01` and were removed after evidence was distilled.
The meaningful layout was:

```text
source/duo-delegation-loop/SKILL.md       # byte-identical canonical copy
project-copy/.agents/skills/duo-delegation-loop/      # copied directory
project-link/.agents/skills/duo-delegation-loop -> /tmp/.../source/duo-delegation-loop
project-relative/.agents/skills/duo-delegation-loop -> ../../skills/duo-delegation-loop
project-empty/                            # user-root probes
home/                                     # isolated HOME/XDG/CODEX_HOME
```

Each project was an independent initialized Git working tree. Discovery was
also rerun from `project-link/nested/a`; every launcher resolved the skill from
the worktree root, rather than requiring the process cwd to contain `.agents`.

No real user-global skill tree was changed. Authentication files needed for
the Amp and OpenCode inference/user-root probes were mechanically copied into
mode-0700/0600 temporary paths, never printed, and deleted immediately after
use. Codex used `exec --ephemeral`. OpenCode used `--pure` and isolated XDG
state; Amp used an empty isolated settings file and `--no-ide`; Codex used
`--ignore-user-config`. No plugin or MCP supplied discovery evidence.

## Discovery matrix

`yes` means the launcher itself returned the exact skill name, description,
and/or source path. `no` means the same isolated probe returned no matching
skill. Blank native roots were not tested because they are irrelevant to the
shared projection.

| Root/form | Amp | OpenCode | Codex |
|---|---:|---:|---:|
| project `.agents/skills`, copied directory | yes (`amp skill info`) | yes (`opencode debug skill`) | yes (`codex debug prompt-input`) |
| project `.agents/skills`, absolute directory symlink | yes | yes | yes |
| project `.agents/skills`, invoked from nested cwd | yes | yes | yes |
| user `~/.agents/skills`, copied directory | yes | yes | yes |
| user `~/.config/agents/skills`, copied directory | yes | no | no |
| user `~/.config/amp/skills`, copied directory | yes | — | — |
| project `.opencode/skills`, copied directory | — | yes | — |
| user `~/.config/opencode/skills`, copied directory | — | yes | — |
| user `$CODEX_HOME/skills`, copied directory | — | — | yes |

Amp's live help additionally identifies `.claude/skills` roots and custom
`amp.skills.path`; OpenCode's built-in customization skill identifies singular
and plural `.opencode/skill(s)` plus `~/.claude/skills`; those extra roots were
not needed to establish the common contract.

### Symlink versus copy

The copied and symlinked project forms exposed the same frontmatter and body.
OpenCode reported `body_has_launch=true` for both. Codex included the skill in
the model-visible skills block for both. Amp resolved both with `skill info`.
The authenticated invocation used the symlinked form for all three launchers.
Therefore a directory symlink from
`.agents/skills/duo-delegation-loop` to
`skills/duo-delegation-loop` is sufficient on this Linux host; copying also
works but can drift from the canonical source. The original probes used an
absolute target. An independent deterministic check then confirmed Amp,
OpenCode, and Codex all recognize the intended repository-relative target
`../../skills/duo-delegation-loop`.

## Specific skill recognition and invocation

The prompt for each live inference was narrowly constrained: use
`duo-delegation-loop`, execute no Duo command, and return the three phases plus
the timeout.

- **Amp:** stream JSON contained a `tool_use` named `skill` with input
  `{"name":"duo-delegation-loop"}`. Final output was
  `session launch builder → prompt send → session show/conversation list/prompt show; polling timeout=20s`.
  Amp necessarily created one remote execute thread; it was permanently
  deleted immediately after the probe.
- **OpenCode:** JSON contained completed `tool_use` `skill` with
  `name=duo-delegation-loop`, exact content, source directory under the
  symlinked projection, and `truncated=false`. Final output reproduced the
  launch/send/observe sequence and `20s` timeout.
- **Codex:** `debug prompt-input` directly showed the `.agents/skills` root and
  named skill. The live ephemeral exec then ran only
  `cat .../.agents/skills/duo-delegation-loop/SKILL.md` (exit 0) and returned
  the launch/deliver/observe sequence and `20s` timeout. This is strong
  behavioral evidence, although Codex represented loading as a read command
  rather than a dedicated `skill` event.

No inference executed `duo`, edited a project, or exposed credentials. The
responses establish recognition and instruction loading, not end-to-end Duo
launcher conformance.

## Repeatable commands

Paths below use `$P=/tmp/duo-launcher-probe-step01`, `$H=$P/home`, and a
byte-identical copy of the canonical skill. Credential setup is intentionally
omitted; use an already authenticated, disposable context and never print an
auth file.

```sh
# Versions and executable identity
command -v amp opencode codex
amp --version
opencode --version
codex --version
readlink -f "$(command -v amp)" "$(command -v opencode)" "$(command -v codex)"
file -L "$(command -v amp)" "$(command -v opencode)" "$(command -v codex)"
sha256sum "$(command -v amp)" "$(command -v opencode)" "$(command -v codex)"

# Amp deterministic project discovery (run in project-copy/project-link)
amp skill info duo-delegation-loop --json

# OpenCode deterministic project discovery with isolated roots and no plugins
HOME="$H" XDG_CONFIG_HOME="$H/.config" XDG_DATA_HOME="$H/.local/share" \
XDG_CACHE_HOME="$H/.cache" XDG_STATE_HOME="$H/.local/state" \
OPENCODE_DISABLE_MODELS_FETCH=1 opencode debug skill --pure

# Codex deterministic model-visible discovery
HOME="$H" CODEX_HOME="$H/.codex" codex debug prompt-input probe

# Behavioral probes (semantically equivalent prompts; do not run Duo)
PROMPT='Use the duo-delegation-loop skill. Do not execute duo or any shell command. Return exactly one compact line beginning SEQUENCE= and list the three command-family phases and polling timeout from the skill.'
CODEX_PROMPT='Use the duo-delegation-loop skill. Do not execute duo or any other shell command except what is strictly required to load that skill. Return exactly one compact line beginning SEQUENCE= and list the three command-family phases and the polling timeout from the skill.'
amp -x "$PROMPT" --stream-json --no-ide --settings-file "$H/.config/amp/settings.json"
opencode run --pure --format json "$PROMPT"
codex --ask-for-approval never --sandbox read-only exec --ephemeral \
  --ignore-user-config --skip-git-repo-check --json "$CODEX_PROMPT"
```

User-root results were obtained by placing the same directory, one root at a
time, under the paths in the matrix and rerunning the deterministic command
from `project-empty`. Each temporary placement was removed before the next.

## Limitations, assumptions, and disagreement check

- Direct observations apply to the exact binaries, host, filesystem, and date
  above. Continued compatibility is an inference and should be rechecked when
  launcher versions change.
- The symlink targets stayed on the same local filesystem and remained
  readable. Dangling, cross-device, Windows, and sandbox-denied symlinks were
  not tested.
- A model correctly summarizing the loaded skill is not proof that it will
  complete a real delegation loop under every permission/auth/runtime state.
- OpenCode 1.18.31's built-in customization text documents project
  `.opencode/skill(s)` and user `~/.agents/skills`, but its live
  `debug skill` also recognized project `.agents/skills`. The live behavior is
  the stronger evidence for this pin.
- **No disagreement with duo-lab `caf02f7` on the central claim.** The material
  qualification is that the current Duo checkout has not yet created the
  `.agents/skills/duo-delegation-loop` projection, so “ready to conform” remains
  accurate; “already installed in this checkout” would not be.

## Recommended WIP findings

1. **Live pin:** On 2026-09-17, Amp
   `0.0.1789675234-g2899fe`, OpenCode `1.18.31`, and Codex CLI `0.154.0`
   all directly discovered and behaviorally loaded the canonical
   `duo-delegation-loop` skill from project `.agents/skills`.
2. **Projection contract:** Both a copied directory and an absolute directory
   symlink worked for all three. A follow-up deterministic probe also confirmed
   the intended relative symlink for all three; prefer
   `.agents/skills/duo-delegation-loop -> ../../skills/duo-delegation-loop`
   to avoid copy drift.
3. **User fallback:** `~/.agents/skills/duo-delegation-loop` was recognized by
   all three. `~/.config/agents/skills` was Amp-only in this probe and must not
   be treated as the shared user root.
4. **Current gap:** `/home/dev/Code/duo` at
   `1437f2f69e71d164b4248ed121803e53e3e1a2ba` had no `.agents` directory; this
   step validates the design but does not land the projection.
5. **Boundary:** Invocation proof covered skill loading and behavioral
   adherence only. Real launch/send/observe, failure states, duplicate-name
   precedence, and reload behavior remain for launcher conformance work.
6. **Cross-repo impact:** this confirms, rather than invalidates, the pinned
   duo-lab `caf02f7` assumption. It unblocks product-side shared projection and
   conformance design; no duo-lab correction is required.
