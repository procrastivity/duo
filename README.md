# duo

Duo vNext: the authority repo for the Duo terminal-session tool — attach to
and observe agent terminal sessions instead of owning the agent. Planning
lives in `~/Code/terminal-multiplexers` (the vNext planning set and its
research notes); this repo is where the accepted design becomes code.

## Build

Requires Go (or the Nix dev shell: `nix develop`, auto-entered via direnv).

```
make build          # bin/duo, version stamped from git describe
./bin/duo --version
make check          # golangci-lint + go test
```

## Contracts

The external contract set — JSON Schemas under `contracts/schemas/` and the
`duo-external-v1` fixtures under `contracts/fixtures/` — is synced from the
planning repo, not authored here:

```
make sync-contracts   # copies from ~/Code/terminal-multiplexers (override: DUO_CONTRACTS_SRC)
```

`contracts/SOURCE` records the planning repo's git SHA and a sha256 per
copied file; `contracts/embed.go` embeds the tree into the binary, and
`go test ./contracts/...` verifies the embedded set matches SOURCE exactly.
