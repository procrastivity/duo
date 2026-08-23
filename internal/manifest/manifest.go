// Package manifest builds duo's machine-readable declaration of itself:
// tool identity, every registered verb regardless of kind, and the
// shipped-asset list with checksums. It reads the chassis's single
// verb-registration point (the built root command) and the chassis's
// asset-resolution chain; it adds no second registry of either.
package manifest

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/surface"
)

// SchemaVersion is the manifest JSON shape's own version — a separate
// integer from Tool.Version. It bumps only when this shape changes, never
// for an ordinary verb or asset addition.
const SchemaVersion = 1

// Tool identifies the binary that produced the manifest. Its three fields
// reuse the exact version/commit/date vars main sets via -ldflags — the
// same names, the same package path — so `duo version` and a future
// `duo manifest --json`'s tool object are never two sources for one fact.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Arg describes one flag a verb declares on itself (its LocalFlags — the
// global --json/-v pair is the chassis's, not a per-verb arg, and is
// excluded). Positional arguments are not introspectable generically from a
// Cobra command and are not described here.
type Arg struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

// Verb describes one registered command, regardless of its surface kind —
// the manifest itself never filters; that happens only on any
// harness-projection side.
type Verb struct {
	Name         string          `json:"name"`
	Kind         surface.Kind    `json:"kind"`
	Args         []Arg           `json:"args"`
	Description  string          `json:"description"`
	OutputSchema json.RawMessage `json:"outputSchema,omitempty"`
}

// Asset describes one file under the shipped assets/ tree.
type Asset struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// Manifest is the flat object a future `duo manifest --json` emits.
type Manifest struct {
	Tool          Tool    `json:"tool"`
	SchemaVersion int     `json:"schemaVersion"`
	Verbs         []Verb  `json:"verbs"`
	Assets        []Asset `json:"assets"`
}

// Build walks root's registered command tree (the chassis's single
// registration point) and the shipped assets/ tree, and returns the
// manifest they describe. root must already carry every verb it will ever
// carry for this process — callers pass the fully-constructed root command,
// not one still being assembled.
func Build(root *cobra.Command, build buildinfo.Info) (Manifest, error) {
	verbs, err := walkVerbs(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: walking verb registry: %w", err)
	}

	assets, err := walkAssets()
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: walking shipped assets: %w", err)
	}

	return Manifest{
		Tool: Tool{
			Name:    root.Name(),
			Version: build.Version,
			Commit:  build.Commit,
			Date:    build.Date,
		},
		SchemaVersion: SchemaVersion,
		Verbs:         verbs,
		Assets:        assets,
	}, nil
}
