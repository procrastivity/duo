package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/duoerr"
)

// TestParseV3_Fixture proves the notes/42-shaped duo.config/v3 fixture —
// contracts/fixtures/duo-external-v1/config.json, synced from
// terminal-multiplexers by Step 16 (workplan Risk 6; the testdata copy this
// test read before the sync is gone) — resolves cleanly and its host-free
// variant and preset shapes come through untouched.
func TestParseV3_Fixture(t *testing.T) {
	data, err := contracts.FS.ReadFile("fixtures/duo-external-v1/config.json")
	if err != nil {
		t.Fatalf("reading embedded fixture: %v", err)
	}

	doc, err := ParseV3(data)
	if err != nil {
		t.Fatalf("ParseV3(fixture): %v", err)
	}

	if doc.Schema != SchemaV3 {
		t.Errorf("Schema = %q, want %q", doc.Schema, SchemaV3)
	}

	sonnet, ok := doc.LaunchVariants["claude_sonnet"]
	if !ok {
		t.Fatal(`fixture launch_variant "claude_sonnet" did not resolve`)
	}
	if sonnet.AgentRuntime != "claude" {
		t.Errorf("claude_sonnet.AgentRuntime = %q, want %q", sonnet.AgentRuntime, "claude")
	}
	if sonnet.ModelLine != "sonnet-5" {
		t.Errorf("claude_sonnet.ModelLine = %q, want %q", sonnet.ModelLine, "sonnet-5")
	}
	if sonnet.ModelFamily != "claude" {
		t.Errorf("claude_sonnet.ModelFamily = %q, want %q", sonnet.ModelFamily, "claude")
	}

	claude, ok := doc.AgentRuntimes["claude"]
	if !ok {
		t.Fatal(`fixture agent_runtime "claude" did not resolve`)
	}
	if claude.Executable != "claude" {
		t.Errorf("agent_runtimes[claude].Executable = %q, want %q", claude.Executable, "claude")
	}

	preset, ok := doc.Presets["review"]
	if !ok {
		t.Fatal(`fixture preset "review" did not resolve`)
	}
	if preset.Selection != "ordered" {
		t.Errorf("preset.Selection = %q, want %q", preset.Selection, "ordered")
	}
	leaf, ok := preset.Leaves["main"]
	if !ok || len(leaf.Candidates) != 2 || leaf.Candidates[0].Variant != "claude_sonnet" {
		t.Errorf("preset.Leaves[main] = %+v, want first candidate referencing %q", leaf, "claude_sonnet")
	}

	if len(doc.SessionHosts.Prefer) != 1 || doc.SessionHosts.Prefer[0] != "herdr" {
		t.Errorf("SessionHosts.Prefer = %v, want [herdr]", doc.SessionHosts.Prefer)
	}
	herdrKind, ok := doc.SessionHosts.Kinds["herdr"]
	if !ok || herdrKind.Enabled == nil || !*herdrKind.Enabled {
		t.Errorf("SessionHosts.Kinds[herdr] = %+v, want enabled true", herdrKind)
	}
}

// TestParseV3_PreferWithoutKindsStanzaIsAccepted pins the thread-2 decision
// recorded on SessionHostPolicy.Prefer's doc comment: a prefer entry is
// never checked against kinds, so a document that prefers a host kind it
// says nothing else about still resolves — the absent stanza means
// enabled, the same as an absent "enabled" flag inside a stanza that is
// present.
func TestParseV3_PreferWithoutKindsStanzaIsAccepted(t *testing.T) {
	const doc = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr, tmux]
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
    model_line: sonnet-5
    model_family: claude
`
	got, err := ParseV3([]byte(doc))
	if err != nil {
		t.Fatalf("ParseV3: unexpected error: %v", err)
	}
	if len(got.SessionHosts.Prefer) != 2 || got.SessionHosts.Prefer[1] != "tmux" {
		t.Errorf("SessionHosts.Prefer = %v, want [herdr tmux]", got.SessionHosts.Prefer)
	}
	if _, ok := got.SessionHosts.Kinds["tmux"]; ok {
		t.Errorf("SessionHosts.Kinds[tmux] = present, want absent (no stanza was authored)")
	}
}

// TestParseV3_Mutations is the table-driven acceptance the Step 06 spec
// names: a v2 document (with the migrate pointer in its message), a v1
// document, a document with no marker, a variant missing model_family, a
// candidate naming an unknown variant, a variant naming an unknown
// runtime, a root compositions key, and an unknown key each fail with a
// distinct, stable error code. Further rows (unrecognized schema, missing
// model_line, an unknown key nested inside a launch_variant) exercise the
// rest of the resolver's vocabulary and the "closes the launch blocks too"
// half of the unknown-key rule.
func TestParseV3_Mutations(t *testing.T) {
	const validDoc = `
schema: duo.config/v3
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
    model_line: sonnet-5
    model_family: claude
presets:
  review:
    leaves:
      main:
        candidates:
          - variant: claude_sonnet
`

	tests := []struct {
		name        string
		doc         string
		wantCode    string // "" means ParseV3 must succeed
		wantMessage string // optional substring the error message must contain
	}{
		{
			name:     "valid document resolves",
			doc:      validDoc,
			wantCode: "",
		},
		{
			name: "v2 schema marker",
			doc: `
schema: duo.config/v2
compositions:
  review:
    launch_variant: codex_in_tmux
    model_line: gpt-5.6
`,
			wantCode:    ErrCodeSchemaV2Unsupported,
			wantMessage: "duo config migrate --to duo.config/v3",
		},
		{
			name: "v1 schema marker",
			doc: `
schema: duo.config/v1
`,
			wantCode: ErrCodeSchemaV1Unsupported,
		},
		{
			name:     "missing schema field",
			doc:      "agent_runtimes: {}\n",
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
			name: "variant missing model_line",
			doc: `
schema: duo.config/v3
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
    model_family: claude
`,
			wantCode: ErrCodeVariantModelLineRequired,
		},
		{
			name: "variant missing model_family",
			doc: `
schema: duo.config/v3
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
    model_line: sonnet-5
`,
			wantCode: ErrCodeVariantModelFamilyRequired,
		},
		{
			name: "variant names unknown runtime",
			doc: `
schema: duo.config/v3
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: nonexistent
    model_line: sonnet-5
    model_family: claude
`,
			wantCode: ErrCodeVariantUnknownRuntime,
		},
		{
			name: "candidate names unknown variant",
			doc: `
schema: duo.config/v3
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
    model_line: sonnet-5
    model_family: claude
presets:
  review:
    leaves:
      main:
        candidates:
          - variant: nonexistent
`,
			wantCode: ErrCodeCandidateUnknownVariant,
		},
		{
			name: "root compositions key is refused",
			doc: validDoc + `
compositions:
  review:
    launch_variant: claude_sonnet
    model_line: sonnet-5
`,
			wantCode: ErrCodeDecodeFailed,
		},
		{
			name: "unknown top-level key",
			doc: `
schema: duo.config/v3
not_a_real_field: true
`,
			wantCode: ErrCodeDecodeFailed,
		},
		{
			name: "unknown key inside a launch_variant block",
			doc: `
schema: duo.config/v3
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
    model_line: sonnet-5
    model_family: claude
    session_host: herdr
`,
			wantCode: ErrCodeDecodeFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := ParseV3([]byte(tt.doc))

			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("ParseV3: unexpected error: %v", err)
				}
				if doc.Schema != SchemaV3 {
					t.Errorf("Schema = %q, want %q", doc.Schema, SchemaV3)
				}
				return
			}

			if err == nil {
				t.Fatalf("ParseV3: got no error, want code %q", tt.wantCode)
			}
			var derr *duoerr.Error
			if !errors.As(err, &derr) {
				t.Fatalf("ParseV3 returned a non-duoerr error: %v", err)
			}
			if derr.Code != tt.wantCode {
				t.Errorf("error code = %q, want %q (message: %s)", derr.Code, tt.wantCode, derr.Message)
			}
			if tt.wantMessage != "" && !strings.Contains(derr.Message, tt.wantMessage) {
				t.Errorf("error message = %q, want it to contain %q", derr.Message, tt.wantMessage)
			}
		})
	}
}

// TestParseV3_MissingModelLineWinsOverMissingModelFamily pins that a
// variant missing both required fields reports model_line first — the
// resolver checks it before model_family, deterministically, mirroring
// v2_test.go's analogous composition-field pin.
func TestParseV3_MissingModelLineWinsOverMissingModelFamily(t *testing.T) {
	const doc = `
schema: duo.config/v3
agent_runtimes:
  claude:
    kind: claude
    executable: claude
launch_variants:
  claude_sonnet:
    agent_runtime: claude
`
	_, err := ParseV3([]byte(doc))
	var derr *duoerr.Error
	if !errors.As(err, &derr) {
		t.Fatalf("ParseV3 returned a non-duoerr error: %v", err)
	}
	if derr.Code != ErrCodeVariantModelLineRequired {
		t.Errorf("error code = %q, want %q", derr.Code, ErrCodeVariantModelLineRequired)
	}
}
