package config

import (
	"errors"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/duoerr"
)

// TestParseV2_Fixture proves the synced contracts/fixtures/duo-external-v1/
// config.json — a real duo.config/v2 document — resolves cleanly and its
// composition's required fields come through untouched.
func TestParseV2_Fixture(t *testing.T) {
	data, err := contracts.FS.ReadFile("fixtures/duo-external-v1/config.json")
	if err != nil {
		t.Fatalf("reading embedded fixture: %v", err)
	}

	doc, err := ParseV2(data)
	if err != nil {
		t.Fatalf("ParseV2(fixture): %v", err)
	}

	if doc.Schema != SchemaV2 {
		t.Errorf("Schema = %q, want %q", doc.Schema, SchemaV2)
	}

	review, ok := doc.Compositions["review"]
	if !ok {
		t.Fatal(`fixture composition "review" did not resolve`)
	}
	if review.LaunchVariant != "codex_in_tmux" {
		t.Errorf("review.LaunchVariant = %q, want %q", review.LaunchVariant, "codex_in_tmux")
	}
	if review.ModelLine != "gpt-5.6" {
		t.Errorf("review.ModelLine = %q, want %q", review.ModelLine, "gpt-5.6")
	}

	preset, ok := doc.Presets["review"]
	if !ok {
		t.Fatal(`fixture preset "review" did not resolve`)
	}
	if preset.Selection != "ordered" {
		t.Errorf("preset.Selection = %q, want %q", preset.Selection, "ordered")
	}
	leaf, ok := preset.Leaves["main"]
	if !ok || len(leaf.Candidates) != 1 || leaf.Candidates[0].Composition != "review" {
		t.Errorf("preset.Leaves[main] = %+v, want one candidate referencing %q", leaf, "review")
	}
}

// TestParseV2_Mutations is the table-driven acceptance the Step 10 spec
// names: a document missing launch_variant, one missing model_line, and one
// carrying the duo.config/v1 marker each fail with a distinct, stable
// error code. Two further rows (unrecognized schema, unknown top-level
// field) exercise the rest of the resolver's own vocabulary.
func TestParseV2_Mutations(t *testing.T) {
	const validDoc = `
schema: duo.config/v2
compositions:
  review:
    launch_variant: codex_in_tmux
    model_line: gpt-5.6
`

	tests := []struct {
		name     string
		doc      string
		wantCode string // "" means ParseV2 must succeed
	}{
		{
			name:     "valid document resolves",
			doc:      validDoc,
			wantCode: "",
		},
		{
			name: "missing launch_variant",
			doc: `
schema: duo.config/v2
compositions:
  review:
    model_line: gpt-5.6
`,
			wantCode: ErrCodeCompositionLaunchVariantRequired,
		},
		{
			name: "missing model_line",
			doc: `
schema: duo.config/v2
compositions:
  review:
    launch_variant: codex_in_tmux
`,
			wantCode: ErrCodeCompositionModelLineRequired,
		},
		{
			name: "v1 schema marker",
			doc: `
schema: duo.config/v1
compositions:
  review:
    launch_variant: codex_in_tmux
    model_line: gpt-5.6
`,
			wantCode: ErrCodeSchemaV1Unsupported,
		},
		{
			name:     "missing schema field",
			doc:      "compositions: {}\n",
			wantCode: ErrCodeSchemaMissing,
		},
		{
			name: "unrecognized schema value",
			doc: `
schema: duo.config/v99
`,
			wantCode: ErrCodeSchemaUnrecognized,
		},
		{
			name: "unknown top-level field",
			doc: `
schema: duo.config/v2
not_a_real_field: true
`,
			wantCode: ErrCodeDecodeFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseV2([]byte(tt.doc))

			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("ParseV2: unexpected error: %v", err)
				}
				if doc.Schema != SchemaV2 {
					t.Errorf("Schema = %q, want %q", doc.Schema, SchemaV2)
				}
				return
			}

			if err == nil {
				t.Fatalf("ParseV2: got no error, want code %q", tt.wantCode)
			}
			var derr *duoerr.Error
			if !errors.As(err, &derr) {
				t.Fatalf("ParseV2 returned a non-duoerr error: %v", err)
			}
			if derr.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q (message: %s)", derr.Code, tt.wantCode, derr.Message)
			}
		})
	}
}

// TestParseV2_MissingLaunchVariantWinsOverMissingModelLine pins that a
// composition missing both required fields reports launch_variant first —
// the resolver checks it before model_line, deterministically, regardless
// of Go's map iteration order.
func TestParseV2_MissingLaunchVariantWinsOverMissingModelLine(t *testing.T) {
	const doc = `
schema: duo.config/v2
compositions:
  review: {}
`
	_, err := ParseV2([]byte(doc))
	var derr *duoerr.Error
	if !errors.As(err, &derr) {
		t.Fatalf("ParseV2 returned a non-duoerr error: %v", err)
	}
	if derr.Code != ErrCodeCompositionLaunchVariantRequired {
		t.Errorf("error code = %q, want %q", derr.Code, ErrCodeCompositionLaunchVariantRequired)
	}
}
