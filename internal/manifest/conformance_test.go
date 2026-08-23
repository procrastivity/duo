package manifest

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/surface"
)

// TestManifestMatchesContractSchema checks Build's output against the
// embedded duo-manifest-v1.schema.json root shape, following
// internal/registry/conformance_test.go's pattern (TestManifestOperations-
// MatchContractSchema): read the schema's own required-key list and closed
// enums/consts/patterns directly, rather than restating them as a second
// copy in Go. The binary carries no JSON Schema validator; that test's
// coverage is nested operations items, and this one is the root object.
func TestManifestMatchesContractSchema(t *testing.T) {
	data, err := contracts.FS.ReadFile("schemas/duo-manifest-v1.schema.json")
	if err != nil {
		t.Fatalf("reading duo-manifest-v1.schema.json: %v", err)
	}
	var schema struct {
		Required   []string `json:"required"`
		Properties map[string]struct {
			Const      string `json:"const"`
			Pattern    string `json:"pattern"`
			Properties map[string]struct {
				Const string `json:"const"`
			} `json:"properties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parsing duo-manifest-v1.schema.json: %v", err)
	}
	if len(schema.Required) == 0 {
		t.Fatal("the manifest schema lists no required root keys; the test is reading the wrong node")
	}

	root := &cobra.Command{Use: "duo"}
	verb := &cobra.Command{Use: "noop", Short: "test verb", RunE: func(*cobra.Command, []string) error { return nil }}
	surface.Annotate(verb, surface.Plumbing)
	root.AddCommand(verb)

	m, err := Build(root, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	encoded, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshalling manifest: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(encoded, &fields); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}

	for _, key := range schema.Required {
		v, ok := fields[key]
		if !ok {
			t.Errorf("manifest omits required key %q", key)
			continue
		}
		// The four array-typed required keys must round-trip as JSON
		// arrays, never null, even when empty (adapters, harness_targets
		// today).
		switch key {
		case "public_schemas", "configuration_schemas", "projection_formats", "operations", "adapters", "assets", "harness_targets":
			if _, ok := v.([]any); !ok {
				t.Errorf("manifest key %q = %T, want a JSON array", key, v)
			}
		}
	}

	if c := schema.Properties["schema"].Const; c != "" && m.Schema != c {
		t.Errorf("Schema = %q, want the schema's const %q", m.Schema, c)
	}
	if c := schema.Properties["product"].Properties["name"].Const; c != "" && m.Product.Name != c {
		t.Errorf("Product.Name = %q, want the schema's const %q", m.Product.Name, c)
	}
	if m.Product.Version == "" {
		t.Error("Product.Version is empty")
	}
	if p := schema.Properties["manifest_digest"].Pattern; p != "" {
		re := regexp.MustCompile(p)
		if !re.MatchString(m.ManifestDigest) {
			t.Errorf("ManifestDigest = %q, does not match schema pattern %q", m.ManifestDigest, p)
		}
	}
}

// TestSchemaFamilyListsResolve proves every family string in publicSchemas,
// configurationSchemas, and projectionFormats — and WireSchema itself — is
// backed by an actual embedded schema file, so a typo in the authored lists
// fails here rather than shipping silently in `duo manifest` output.
func TestSchemaFamilyListsResolve(t *testing.T) {
	check := func(family string) {
		t.Helper()
		file, ok := schemaFamilyFiles[family]
		if !ok {
			t.Errorf("family %q has no entry in schemaFamilyFiles", family)
			return
		}
		if _, err := contracts.FS.ReadFile(file); err != nil {
			t.Errorf("family %q: reading %s: %v", family, file, err)
		}
	}

	check(WireSchema)
	for _, f := range publicSchemas {
		check(f)
	}
	for _, f := range configurationSchemas {
		check(f)
	}
	for _, f := range projectionFormats {
		check(f)
	}
}

// TestManifestDigestIsDeterministic pins that two Build calls against an
// identical command tree and build metadata produce the same digest — the
// property manifestDigest's doc comment claims.
func TestManifestDigestIsDeterministic(t *testing.T) {
	build := func() Manifest {
		root := &cobra.Command{Use: "duo"}
		verb := &cobra.Command{Use: "noop", Short: "test verb", RunE: func(*cobra.Command, []string) error { return nil }}
		surface.Annotate(verb, surface.Plumbing)
		root.AddCommand(verb)
		m, err := Build(root, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		return m
	}

	a, b := build(), build()
	if a.ManifestDigest == "" {
		t.Fatal("ManifestDigest is empty")
	}
	if a.ManifestDigest != b.ManifestDigest {
		t.Errorf("digest not deterministic: %q != %q", a.ManifestDigest, b.ManifestDigest)
	}
}
