// Package contracts embeds the external contract set — the JSON Schemas and
// duo-external-v1 fixtures — authored in this repo, so the binary always
// carries the exact contract snapshot it was built against. MANIFEST
// records a sha256 per file; embed_test.go holds the embedded tree to it.
package contracts

import "embed"

// FS holds MANIFEST plus the schemas/ and fixtures/ trees.
//
//go:embed MANIFEST schemas fixtures
var FS embed.FS
