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
	"github.com/procrastivity/duo/internal/registry"
	"github.com/procrastivity/duo/internal/surface"
)

// SchemaVersion is the manifest JSON shape's own version — a separate
// integer from Tool.Version. It bumps only when this shape changes, never
// for an ordinary verb or asset addition.
const SchemaVersion = 1

// Tool identifies the binary that produced the manifest. Its three fields
// reuse the exact version/commit/date vars main sets via -ldflags — the
// same names, the same package path — so `duo version` and `duo manifest
// --output json`'s tool object are never two sources for one fact. It is a
// chassis-internal extra, richer than the contract's own "product" object
// (see Product) — duo.manifest/v1's additionalProperties: true root
// permits both to coexist.
type Tool struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

// Product is the duo.manifest/v1 contract's required "product" object: just
// the two fields the schema names (name, version) — commit and build date
// stay on Tool, which is additive beyond the contract.
type Product struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Adapter describes one registered session-host or agent-runtime adapter —
// the duo.manifest/v1 contract's "adapters" array. No adapter registers
// itself yet: internal/host and internal/runtime, which own that
// registration, are later steps' work. Build always emits an empty slice;
// this shape exists now so that later registration is additive to the
// manifest wire format, never a breaking rename.
type Adapter struct {
	Name              string `json:"name"`
	Kind              string `json:"kind"` // "session_host" | "agent_runtime"
	Version           string `json:"version,omitempty"`
	ConformanceDigest string `json:"conformance_digest,omitempty"`
}

// HarnessTarget describes one generated-integration harness target — the
// duo.manifest/v1 contract's "harness_targets" array. No harness renderer
// exists yet, so Build always emits an empty slice; the shape exists ready
// for that later work (duo-vnext-installation-contract.md §2).
type HarnessTarget struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Arg describes one flag a verb declares on itself (its LocalFlags — the
// global --output/-v pair is the chassis's, not a per-verb arg, and is
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

// WireSchema is the duo.manifest/v1 contract's required "schema" marker —
// contracts/schemas/duo-manifest-v1.schema.json's properties.schema.const.
const WireSchema = "duo.manifest/v1"

// Manifest is the object `duo manifest` emits. Its first ten fields are the
// duo.manifest/v1 contract's required root properties, in the contract's
// own order; Tool, SchemaVersion, and Verbs are chassis-internal extras the
// contract's additionalProperties: true root permits. Operations comes
// straight from internal/registry — the binary's one operation table — the
// same way Verbs comes straight from the built root command: the manifest
// holds no second copy of either registration.
type Manifest struct {
	Schema               string                       `json:"schema"`
	Product              Product                      `json:"product"`
	ManifestDigest       string                       `json:"manifest_digest"`
	PublicSchemas        []string                     `json:"public_schemas"`
	ConfigurationSchemas []string                     `json:"configuration_schemas"`
	ProjectionFormats    []string                     `json:"projection_formats"`
	Operations           []registry.ManifestOperation `json:"operations"`
	Adapters             []Adapter                    `json:"adapters"`
	Assets               []Asset                      `json:"assets"`
	HarnessTargets       []HarnessTarget              `json:"harness_targets"`

	Tool          Tool      `json:"tool"`
	SchemaVersion int       `json:"schemaVersion"`
	Verbs         []Verb    `json:"verbs"`
	Contracts     Contracts `json:"contracts"`
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
	if assets == nil {
		assets = []Asset{}
	}

	contractDigests, err := loadContracts()
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}

	m := Manifest{
		Schema:               WireSchema,
		Product:              Product{Name: "duo", Version: build.Version},
		PublicSchemas:        cloneStrings(publicSchemas),
		ConfigurationSchemas: cloneStrings(configurationSchemas),
		ProjectionFormats:    cloneStrings(projectionFormats),
		Operations:           registry.ManifestOperations(),
		Adapters:             []Adapter{},
		Assets:               assets,
		HarnessTargets:       []HarnessTarget{},

		Tool: Tool{
			Name:    root.Name(),
			Version: build.Version,
			Commit:  build.Commit,
			Date:    build.Date,
		},
		SchemaVersion: SchemaVersion,
		Verbs:         verbs,
		Contracts:     contractDigests,
	}

	digest, err := manifestDigest(m)
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: computing manifest_digest: %w", err)
	}
	m.ManifestDigest = digest

	return m, nil
}

func cloneStrings(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	return out
}
