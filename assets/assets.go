// Package assets embeds the shipped default asset tree as the last-resort
// fallback the chassis asset-resolution chain reaches for when neither a
// user override ($XDG_CONFIG_HOME/duo/) nor an installed share tree
// (<prefix>/share/duo/assets/) has the requested file — e.g. a bare
// `go run` with nothing installed. Assets are inputs, never state: this
// package only ever reads the tree baked in at build time.
package assets

import "embed"

// FS holds every top-level shipped asset file. New assets need no code
// change here: "*" matches any non-hidden file added to this directory.
//
//go:embed *
var FS embed.FS
