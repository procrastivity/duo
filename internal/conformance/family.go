// Package conformance is the Stage 0 fixture round-trip harness
// (duo-vnext-go-architecture.md §8, §10): it decodes the synced contract
// fixtures through minimal CLI, MCP, and presentation encoders and proves
// the three projections reach an equal canonical (domain) value.
//
// Stage 0 has no live socket, no real CLI process, and no generated
// projection codec yet. The encoders in this package are therefore the
// minimal canonical-form codec §8 promises will eventually be generated —
// built here, consumed only by this package's tests, and never wired into
// internal/cli. docs/conformance/decisions.md records what that scope
// forced and what a later stage must replace.
package conformance

import (
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strings"
)

// schemaFilePattern parses a synced schema filename into its family base
// and version. The base is greedy up to the last "-vN" segment, which
// correctly separates a hyphenated base ("projection-conformance") from
// the version suffix.
var schemaFilePattern = regexp.MustCompile(`^duo-(.+)-v(\d+)\.schema\.json$`)

// SchemaFile is one synced JSON Schema file, decomposed into the family and
// version its own filename encodes.
type SchemaFile struct {
	// Path is the file's path inside contracts.FS, e.g.
	// "schemas/duo-config-v1.schema.json".
	Path string
	// Family is the schema family without a version, e.g. "duo.config".
	// This is the grouping unit the step's five-families requirement
	// counts: duo-config-v1 and duo-config-v2 share family "duo.config".
	Family string
	// Version is the bare version segment, e.g. "1".
	Version string
	// FamilyVersion is the full "family/vN" string as it appears in a
	// fixture's top-level "schema" field, e.g. "duo.config/v1".
	FamilyVersion string
}

// SchemaFiles enumerates every synced schema file under "schemas" in fsys
// (contracts.FS in production), decomposed by filename. It is the harness's
// derivation point for "enumerate the families from contracts/schemas": no
// family list is hand-maintained here.
func SchemaFiles(fsys fs.FS) ([]SchemaFile, error) {
	entries, err := fs.ReadDir(fsys, "schemas")
	if err != nil {
		return nil, fmt.Errorf("conformance: reading schemas directory: %w", err)
	}

	var out []SchemaFile
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := schemaFilePattern.FindStringSubmatch(e.Name())
		if m == nil {
			return nil, fmt.Errorf("conformance: schema file %q does not match the duo-<family>-v<N>.schema.json pattern", e.Name())
		}
		base, version := m[1], m[2]
		out = append(out, SchemaFile{
			Path:          "schemas/" + e.Name(),
			Family:        "duo." + base,
			Version:       version,
			FamilyVersion: "duo." + base + "/v" + version,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// Families returns the distinct schema families in fsys, sorted. Two schema
// files at different versions of the same base (duo-config-v1,
// duo-config-v2) collapse to one family entry.
func Families(fsys fs.FS) ([]string, error) {
	files, err := SchemaFiles(fsys)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		if seen[f.Family] {
			continue
		}
		seen[f.Family] = true
		out = append(out, f.Family)
	}
	sort.Strings(out)
	return out, nil
}

// FamilyOf splits a fixture's top-level "schema" value ("duo.config/v2")
// into its family ("duo.config") and version ("v2").
func FamilyOf(schemaValue string) (family, version string) {
	family, version, ok := strings.Cut(schemaValue, "/")
	if !ok {
		return schemaValue, ""
	}
	return family, version
}
