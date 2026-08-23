// Package contracts embeds the external contract set — the JSON Schemas and
// duo-external-v1 fixtures synced from the planning repo by
// contrib/sync-contracts (`make sync-contracts`) — so the binary always
// carries the exact contract snapshot it was built against. SOURCE records
// the provenance: the planning repo's git SHA and a sha256 per file;
// embed_test.go holds the embedded tree to it.
package contracts

import "embed"

// FS holds SOURCE plus the synced schemas/ and fixtures/ trees.
//
//go:embed SOURCE schemas fixtures
var FS embed.FS
