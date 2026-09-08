# duo

Duo vNext: the authority repo for the Duo terminal-session tool — attach to
and observe agent terminal sessions instead of owning the agent. The
product-normative planning set lives in `docs/vnext/`, authored here.
Runtime research lives in the private `procrastivity/duo-lab`; the
pre-2026-09 record is the read-only `simensen/terminal-multiplexers`
archive (cite it by tag).

## Now (2026-09-08)

Overwrite this block in place when the bar moves. Do not append here.

**Current bar:** repo consolidation Stage B landing (handoff 27; wip
matter `repo-consolidation` in the archive checkout). Contracts and the
planning set are authored here; the interim delegation-loop skill lives
at `skills/duo-delegation-loop/`.

**Next work:** `duo-devin-mint-claim-release` (fresh-mint `prompt send`
hang), then `duo-devin-exec-posture` — Devin becomes the reliable second
driver. Then the Amp delivery-first sequence (`duo-amp-exclusive-writer`
→ `duo-amp-lag-aware-observe`, `duo-amp-pin-doctor`). Live work:
`wip status` / `wip next` in this checkout.

**Parked (not the bar):** archive freeze (Stage C); notes/52 Herdr
`launch_pending` issue, unsubmitted; Amp live-feed recovery waits on its
smoke trigger (batch in duo-lab).

**Not this:** full Stage 2 (roadmap §4), Stage 3 remainder, `--prompt`,
MCP, Chat View, `duo wait`, tmux + Codex, Cursor as a launch runtime.

## Build

Requires Go (or the Nix dev shell: `nix develop`, auto-entered via direnv).

```
make build          # bin/duo, version stamped from git describe
./bin/duo --version
make check          # golangci-lint + go test
```

## Contracts

The external contract set — JSON Schemas under `contracts/schemas/` and the
`duo-external-v1` fixtures under `contracts/fixtures/` — is authored in
`contracts/`. Run `make contracts-manifest` after editing a schema or
fixture:

```
make contracts-manifest
```

`contracts/MANIFEST` records a sha256 per file; `contracts/embed.go` embeds
the tree into the binary, and `go test ./contracts/...` verifies the
embedded set matches MANIFEST exactly.
