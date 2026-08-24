package launch_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/launch"
)

// TestVocabulariesMatchExternalV1Schema pins the closed vocabularies this
// package owns in Go (the constraint axes, the cross-leaf relation kinds,
// and the elimination reasons) to the embedded duo-external-v1 schema's
// $defs enums. duo-config-v3 step 10 is what first grows both sides
// together — model_family as a third axis, distinct_model_family as a
// second relation kind, and provider_disabled as a fifth elimination
// reason — by reference to step 02's normative schema amendment
// (2026-08-24 handoff 22). Before step 10 the schema carried no
// launch_relation_kind or launch_elimination_reason def at all; this test
// is new precisely because step 10 is the first point where both sides
// exist to compare.
//
// This test compares against contracts/schemas/duo-external-v1.schema.json
// as embedded in this binary — step 10 patched only these three enum lists
// by hand; `make sync-contracts` (step 16) has since replaced the whole
// file with the normative one.
func TestVocabulariesMatchExternalV1Schema(t *testing.T) {
	schema := loadExternalV1Schema(t)

	t.Run("launch_constraint_axis", func(t *testing.T) {
		want := schemaEnum(t, schema, "launch_constraint_axis")
		got := []string{
			string(launch.AxisAgentRuntime),
			string(launch.AxisModelLine),
			string(launch.AxisModelFamily),
		}
		assertSameSet(t, "launch_constraint_axis", got, want)
	})

	t.Run("launch_relation_kind", func(t *testing.T) {
		want := schemaEnum(t, schema, "launch_relation_kind")
		// relationDistinctModelLine and relationDistinctModelFamily are
		// unexported in package launch; their values are pinned here as
		// literals, which is what a resolve_test.go table in package
		// launch_test (fixture_test.go's approach) already does for the
		// declared_kind and outcome closed sets.
		got := []string{"distinct_model_line", "distinct_model_family"}
		assertSameSet(t, "launch_relation_kind", got, want)
	})

	t.Run("launch_elimination_reason", func(t *testing.T) {
		want := schemaEnum(t, schema, "launch_elimination_reason")
		got := []string{
			launch.ReasonSessionHostDisabled,
			launch.ReasonNoConformanceEvidence,
			launch.ReasonRequireUnmatched,
			launch.ReasonAvoidMatched,
			launch.ReasonProviderDisabled,
		}
		assertSameSet(t, "launch_elimination_reason", got, want)
	})
}

// loadExternalV1Schema reads and decodes the embedded duo-external-v1
// schema document.
func loadExternalV1Schema(t *testing.T) map[string]any {
	t.Helper()
	data, err := contracts.FS.ReadFile("schemas/duo-external-v1.schema.json")
	if err != nil {
		t.Fatalf("reading embedded schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing embedded schema: %v", err)
	}
	return doc
}

// schemaEnum reads $defs.<name>.enum as a []string, failing t if the def or
// its enum is missing or not all strings.
func schemaEnum(t *testing.T, schema map[string]any, name string) []string {
	t.Helper()
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatalf("schema carries no $defs object")
	}
	def, ok := defs[name].(map[string]any)
	if !ok {
		t.Fatalf("schema $defs carries no %q", name)
	}
	rawEnum, ok := def["enum"].([]any)
	if !ok {
		t.Fatalf("$defs.%s carries no enum array", name)
	}
	out := make([]string, 0, len(rawEnum))
	for _, v := range rawEnum {
		s, ok := v.(string)
		if !ok {
			t.Fatalf("$defs.%s.enum carries a non-string value %v", name, v)
		}
		out = append(out, s)
	}
	return out
}

// assertSameSet fails t unless got and want carry exactly the same values,
// order and duplication aside — a Go-side vocabulary and a schema enum are
// two independently authored lists, and only their set needs to agree.
func assertSameSet(t *testing.T, label string, got, want []string) {
	t.Helper()
	gotSorted := append([]string(nil), got...)
	wantSorted := append([]string(nil), want...)
	slices.Sort(gotSorted)
	slices.Sort(wantSorted)
	if !slices.Equal(gotSorted, wantSorted) {
		t.Errorf("%s: Go vocabulary %v does not match schema enum %v", label, got, want)
	}
}
