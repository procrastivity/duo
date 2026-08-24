package conformance

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/procrastivity/duo/internal/registry"
)

// FixtureCase is one embedded fixture file, classified for the per-fixture
// round trip: either applicable (Reason == "", Operation names the
// registered descriptor the harness will project it through) or explicitly
// skipped (Reason explains why, per the step's "no silent skips"
// requirement).
type FixtureCase struct {
	Path      string
	Data      Canonical
	Operation string
	Reason    string
}

// Skipped reports whether the case is excluded from the three-projection
// round trip.
func (c FixtureCase) Skipped() bool { return c.Reason != "" }

// fixtureFamilyReasons gives the skip reason for every duo-external-v1
// fixture whose top-level "schema" family is not "duo.external" — none of
// those fixtures are a public operation's request/result envelope, so none
// has a CLI/MCP/presentation projection to compare.
var fixtureFamilyReasons = map[string]string{
	"duo.config": "duo.config/v3 fixture is local configuration input, not a wire operation envelope",
	"duo.manifest": "duo.manifest/v1 fixture is manifest.show's CLI-only result body " +
		"(manifest.show is registered local_admin-shaped: no MCP tool or presentation route)",
	"duo.projection-stamp": "duo.projection-stamp/v1 fixture is a harness installation stamp, not an operation envelope",
	"duo.projection-conformance": "duo.projection-conformance/v1 fixture (projection-cases.json) is consumed by " +
		"TestProjectionCasesValueEquality, not this per-fixture round trip",
}

// LoadFixtureCases walks every embedded duo-external-v1 fixture and
// classifies it against the registry table.
func LoadFixtureCases(fsys fs.FS) ([]FixtureCase, error) {
	var out []FixtureCase
	err := fs.WalkDir(fsys, "fixtures/duo-external-v1", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return readErr
		}
		obj, parseErr := decodeCanonicalObject(data)
		if parseErr != nil {
			return fmt.Errorf("conformance: %s: %w", path, parseErr)
		}
		out = append(out, classify(path, obj))
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if len(out) == 0 {
		return nil, fmt.Errorf("conformance: no fixtures found under fixtures/duo-external-v1")
	}
	return out, nil
}

// classify assigns a fixture either an applicable operation or a skip
// reason. It is the single place fixture shape is turned into a
// round-trip decision; the reasoning is data-driven off the registry
// table and the fixture's own "schema"/"operation"/"stream" fields — never
// off an integration name.
func classify(path string, obj Canonical) FixtureCase {
	c := FixtureCase{Path: path, Data: obj}

	schema, _ := obj["schema"].(string)
	family, _ := FamilyOf(schema)
	if family != "duo.external" {
		reason, known := fixtureFamilyReasons[family]
		if !known {
			reason = fmt.Sprintf("schema family %q is not decoded through operation projections by this harness", family)
		}
		c.Reason = reason
		return c
	}

	if op, ok := obj["operation"].(string); ok && op != "" {
		return classifyOperation(c, op)
	}
	if stream, ok := obj["stream"].(string); ok && stream != "" {
		return classifyStream(c, stream)
	}

	c.Reason = "fixture carries neither an \"operation\" nor a \"stream\" field; not a decodable envelope shape"
	return c
}

func classifyOperation(c FixtureCase, op string) FixtureCase {
	if reason, deferred := registry.DeferredOperations()[op]; deferred {
		c.Reason = fmt.Sprintf("operation %q is deferred: %s", op, reason)
		return c
	}
	d, ok := registry.Lookup(op)
	if !ok {
		c.Reason = fmt.Sprintf("operation %q is neither registered nor deferred (internal/registry conformance tests should already fail this)", op)
		return c
	}
	if d.MCPTool == "" || d.Route == nil {
		c.Reason = fmt.Sprintf("operation %q is registered %s: no MCP tool or presentation route to compare (CLI-only projection)", op, d.Projectability)
		return c
	}
	c.Operation = op
	return c
}

func classifyStream(c FixtureCase, stream string) FixtureCase {
	for _, d := range registry.All() {
		if d.StreamFamily != stream {
			continue
		}
		if d.MCPTool == "" || d.Route == nil {
			c.Reason = fmt.Sprintf("stream family %q's subscribe operation %q is registered %s: no MCP tool or presentation route", stream, d.Name, d.Projectability)
			return c
		}
		c.Operation = d.Name
		return c
	}
	c.Reason = fmt.Sprintf("stream family %q has no registered subscribe operation (docs/registry/decisions.md, stream families section)", stream)
	return c
}
