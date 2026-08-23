// Package buildinfo carries the version/commit/date triple set by
// cmd/duo/main.go's -ldflags at build time down to the verbs that report it.
package buildinfo

// Info is the version metadata triple: the same three main-package var names
// are targeted by both the Makefile build/cross-compile targets and the Nix
// buildGoModule package, so a locally-built binary and a Nix-built one carry
// identical labels.
type Info struct {
	Version string
	Commit  string
	Date    string
}
