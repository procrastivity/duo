package manifest

import (
	"strings"
	"testing"
)

// TestLoadContracts holds the manifest's contracts block to the embedded
// SOURCE manifest: a source SHA, schema rows, and the conformance
// projection-cases fixture must all be present with sha256 digests.
func TestLoadContracts(t *testing.T) {
	c, err := loadContracts()
	if err != nil {
		t.Fatalf("loadContracts: %v", err)
	}
	if len(c.SourceSHA) != 40 {
		t.Errorf("SourceSHA = %q, want a 40-hex git SHA", c.SourceSHA)
	}
	var schemaRows, projectionCases int
	for _, f := range c.Files {
		if len(f.SHA256) != 64 {
			t.Errorf("%s: sha256 %q is not 64 hex chars", f.Path, f.SHA256)
		}
		if strings.HasPrefix(f.Path, "schemas/") {
			schemaRows++
		}
		if strings.HasSuffix(f.Path, "projection-cases.json") {
			projectionCases++
		}
	}
	if schemaRows == 0 {
		t.Error("no schemas/ rows: manifest would report no schema digests")
	}
	if projectionCases != 1 {
		t.Errorf("projection-cases.json rows = %d, want 1 (the conformance digest)", projectionCases)
	}
}
