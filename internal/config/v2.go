package config

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/procrastivity/duo/internal/duoerr"
)

// SchemaV2 is the duo.config/v2 marker literal the strict resolver accepts.
const SchemaV2 = "duo.config/v2"

// SchemaV1 is the duo.config/v1 marker literal. The strict v2 resolver never
// parses a v1 document's body — it only recognizes the marker well enough to
// name it precisely in a refusal (duo-vnext-installation-contract.md §1:
// "Documents with schema: duo.config/v1 remain valid under that schema", a
// different schema, resolved elsewhere).
const SchemaV1 = "duo.config/v1"

// Stable, named error codes the strict v2 resolver raises. Every one is
// namespaced "config." to mark a document-shape failure caught before any
// registry-owned operation runs — distinct from the operation-level codes
// internal/registry's stable set owns. See docs/config/decisions.md for the
// rationale behind each and why they are plain (unprefixed) codes, which
// internal/exitcode maps to the default user-failure exit.
const (
	// ErrCodeSchemaMissing reports a document with no "schema" field at all.
	ErrCodeSchemaMissing = "config.schema_missing"
	// ErrCodeSchemaV1Unsupported reports a document explicitly marked
	// duo.config/v1, presented to the strict v2 resolver. This is the "never
	// silently reinterpreted" case named in the Step 10 spec: the document
	// may well be a perfectly valid v1 document, but this resolver only
	// understands v2 and refuses rather than guessing.
	ErrCodeSchemaV1Unsupported = "config.schema_v1_unsupported"
	// ErrCodeSchemaUnrecognized reports a "schema" field present but neither
	// duo.config/v1 nor duo.config/v2 — an unknown or malformed marker.
	ErrCodeSchemaUnrecognized = "config.schema_unrecognized"
	// ErrCodeDecodeFailed reports a document that fails to parse as strict
	// YAML/JSON, or that carries a field the duo.config/v2 shape does not
	// recognize (unknown-field strictness, enforced by the YAML decoder's
	// KnownFields mode).
	ErrCodeDecodeFailed = "config.decode_failed"
	// ErrCodeCompositionLaunchVariantRequired reports a composition entry
	// with no launch_variant. duo.config/v2 requires one on every
	// composition; the resolver never infers it.
	ErrCodeCompositionLaunchVariantRequired = "config.composition_launch_variant_required"
	// ErrCodeCompositionModelLineRequired reports a composition entry with
	// no model_line. duo.config/v2 requires one on every composition; the
	// resolver never infers it.
	ErrCodeCompositionModelLineRequired = "config.composition_model_line_required"
)

// DocumentV2 is the strictly-resolved duo.config/v2 root. Every field
// mirrors contracts/schemas/duo-config-v2.schema.json's root properties.
// ParseV2 never infers, defaults, or otherwise fills a missing required
// field — a document that omits one fails resolution instead of silently
// gaining an inferred value.
type DocumentV2 struct {
	Schema              string
	Authority           map[string]any
	Workspaces          map[string]map[string]any
	SessionHosts        map[string]map[string]any
	AgentRuntimes       map[string]map[string]any
	LaunchVariants      map[string]map[string]any
	Compositions        map[string]Composition
	Presets             map[string]Preset
	Control             map[string]any
	Collaboration       map[string]any
	PresentationClients map[string]map[string]any
	Assets              map[string]any
	Projections         map[string]map[string]any
}

// Composition is one duo.config/v2 composition: a named, fully-determined
// selection of one session host, agent runtime, launch variant, and
// declared model line (duo-vnext-installation-contract.md §1.1).
// LaunchVariant and ModelLine are the schema's own required pair; Extra
// carries every other field the document attached (the schema allows
// additional properties on a composition).
type Composition struct {
	LaunchVariant string
	ModelLine     string
	Extra         map[string]any
}

// Preset is one duo.config/v2 named launch intent.
type Preset struct {
	// Selection is "ordered" or "random". A document that omits it resolves
	// to "ordered" per duo-vnext-installation-contract.md §1.1's named
	// contractual default — distinct from the composition fields above,
	// which are never defaulted.
	Selection string
	Leaves    map[string]PresetLeaf
	Relations []PresetRelation
}

// PresetLeaf is one named leaf of a preset: an ordered list of composition
// candidates.
type PresetLeaf struct {
	Candidates []PresetCandidate
}

// PresetCandidate references one composition by name.
type PresetCandidate struct {
	Composition string
}

// PresetRelation is one cross-leaf relation (only "distinct_model_line" is
// defined in the schema today).
type PresetRelation struct {
	Kind   string
	Leaves []string
}

// rawDocument is the strict decode target for a duo.config/v2 root: its
// field set mirrors the schema's root properties exactly, so decoding with
// the YAML decoder's KnownFields(true) mode rejects any unknown top-level
// key (the schema's additionalProperties: false). Compositions decodes
// loosely (map[string]any per entry) because the schema allows a
// composition additional properties beyond launch_variant/model_line;
// resolveCompositions applies the required-field check by hand afterward.
type rawDocument struct {
	Schema              string                    `yaml:"schema"`
	Authority           map[string]any            `yaml:"authority"`
	Workspaces          map[string]map[string]any `yaml:"workspaces"`
	SessionHosts        map[string]map[string]any `yaml:"session_hosts"`
	AgentRuntimes       map[string]map[string]any `yaml:"agent_runtimes"`
	LaunchVariants      map[string]map[string]any `yaml:"launch_variants"`
	Compositions        map[string]map[string]any `yaml:"compositions"`
	Presets             map[string]rawPreset      `yaml:"presets"`
	Control             map[string]any            `yaml:"control"`
	Collaboration       map[string]any            `yaml:"collaboration"`
	PresentationClients map[string]map[string]any `yaml:"presentation_clients"`
	Assets              map[string]any            `yaml:"assets"`
	Projections         map[string]map[string]any `yaml:"projections"`
}

// rawPreset, rawPresetLeaf, rawPresetCandidate, and rawPresetRelation all
// have additionalProperties: false in the schema, so — unlike compositions
// — strict struct decoding under KnownFields(true) is the right strictness
// mechanism for them: an unrecognized key inside a preset fails the decode.
type rawPreset struct {
	Selection string                   `yaml:"selection"`
	Leaves    map[string]rawPresetLeaf `yaml:"leaves"`
	Relations []rawPresetRelation      `yaml:"relations"`
}

type rawPresetLeaf struct {
	Candidates []rawPresetCandidate `yaml:"candidates"`
}

type rawPresetCandidate struct {
	Composition string `yaml:"composition"`
}

type rawPresetRelation struct {
	Kind   string   `yaml:"kind"`
	Leaves []string `yaml:"leaves"`
}

// LoadV2 reads path and strictly resolves it as a duo.config/v2 document.
func LoadV2(path string) (DocumentV2, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DocumentV2{}, duoerr.New(ErrCodeDecodeFailed,
			fmt.Sprintf("config: reading %s: %v", path, err))
	}
	return ParseV2(data)
}

// ParseV2 strictly parses and validates data as a duo.config/v2 document. It
// never infers a missing required field, and a document explicitly marked
// duo.config/v1 is never silently reinterpreted as v2 — it fails with
// ErrCodeSchemaV1Unsupported instead. data may be YAML or JSON; JSON is a
// syntactic subset of YAML 1.2, so no separate JSON path is needed.
func ParseV2(data []byte) (DocumentV2, error) {
	marker, err := peekSchema(data)
	if err != nil {
		return DocumentV2{}, err
	}
	switch marker {
	case SchemaV2:
		// Fall through to the full strict decode below.
	case "":
		return DocumentV2{}, duoerr.New(ErrCodeSchemaMissing,
			`config: document declares no "schema" field; duo.config/v2 requires schema: duo.config/v2`)
	case SchemaV1:
		return DocumentV2{}, duoerr.New(ErrCodeSchemaV1Unsupported,
			`config: document declares schema: duo.config/v1; the strict v2 resolver never reinterprets a v1 document as v2 — migrate it first (duo config migrate --to duo.config/v2)`)
	default:
		return DocumentV2{}, duoerr.New(ErrCodeSchemaUnrecognized,
			fmt.Sprintf("config: document declares schema: %q, which duo.config/v2 does not recognize", marker))
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw rawDocument
	if err := dec.Decode(&raw); err != nil {
		return DocumentV2{}, duoerr.New(ErrCodeDecodeFailed,
			fmt.Sprintf("config: decoding duo.config/v2 document: %v", err))
	}

	compositions, err := resolveCompositions(raw.Compositions)
	if err != nil {
		return DocumentV2{}, err
	}

	return DocumentV2{
		Schema:              raw.Schema,
		Authority:           raw.Authority,
		Workspaces:          raw.Workspaces,
		SessionHosts:        raw.SessionHosts,
		AgentRuntimes:       raw.AgentRuntimes,
		LaunchVariants:      raw.LaunchVariants,
		Compositions:        compositions,
		Presets:             resolvePresets(raw.Presets),
		Control:             raw.Control,
		Collaboration:       raw.Collaboration,
		PresentationClients: raw.PresentationClients,
		Assets:              raw.Assets,
		Projections:         raw.Projections,
	}, nil
}

// peekSchema loosely decodes just data's top-level "schema" field, without
// the full-document strictness ParseV2 applies afterward — so an unrelated
// unknown field in a v1 (or garbage) document never masks the more useful
// "this is a v1/unrecognized document" diagnosis behind a generic decode
// error.
func peekSchema(data []byte) (string, error) {
	var probe struct {
		Schema any `yaml:"schema"`
	}
	if err := yaml.Unmarshal(data, &probe); err != nil {
		return "", duoerr.New(ErrCodeDecodeFailed,
			fmt.Sprintf("config: parsing document to read its schema marker: %v", err))
	}
	if probe.Schema == nil {
		return "", nil
	}
	s, ok := probe.Schema.(string)
	if !ok {
		return "", duoerr.New(ErrCodeSchemaUnrecognized,
			fmt.Sprintf("config: \"schema\" field is not a string (got %T)", probe.Schema))
	}
	return s, nil
}

// resolveCompositions validates every composition's required launch_variant
// and model_line by hand (the schema permits a composition additional
// properties, so it cannot be strict-struct-decoded like a preset). Names
// are checked in sorted order so which composition's error surfaces first
// is deterministic when more than one entry is invalid.
func resolveCompositions(raw map[string]map[string]any) (map[string]Composition, error) {
	if raw == nil {
		return nil, nil
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make(map[string]Composition, len(raw))
	for _, name := range names {
		fields := raw[name]

		lv, ok := stringField(fields, "launch_variant")
		if !ok || lv == "" {
			return nil, duoerr.New(ErrCodeCompositionLaunchVariantRequired,
				fmt.Sprintf("config: composition %q has no launch_variant — duo.config/v2 requires one, and never infers it", name))
		}
		ml, ok := stringField(fields, "model_line")
		if !ok || ml == "" {
			return nil, duoerr.New(ErrCodeCompositionModelLineRequired,
				fmt.Sprintf("config: composition %q has no model_line — duo.config/v2 requires one, and never infers it", name))
		}

		extra := make(map[string]any, len(fields))
		for k, v := range fields {
			if k == "launch_variant" || k == "model_line" {
				continue
			}
			extra[k] = v
		}
		out[name] = Composition{LaunchVariant: lv, ModelLine: ml, Extra: extra}
	}
	return out, nil
}

// stringField reads key from m as a string, reporting whether it was
// present and string-typed. A present-but-wrong-typed value is treated the
// same as absent — deliberate simplification, recorded in
// docs/config/decisions.md.
func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

// resolvePresets converts the strictly-decoded raw preset shape into the
// resolved Preset shape, applying the one named contractual default
// (selection: ordered).
func resolvePresets(raw map[string]rawPreset) map[string]Preset {
	if raw == nil {
		return nil
	}

	out := make(map[string]Preset, len(raw))
	for name, p := range raw {
		selection := p.Selection
		if selection == "" {
			selection = "ordered"
		}

		leaves := make(map[string]PresetLeaf, len(p.Leaves))
		for leafName, leaf := range p.Leaves {
			candidates := make([]PresetCandidate, 0, len(leaf.Candidates))
			for _, c := range leaf.Candidates {
				candidates = append(candidates, PresetCandidate(c))
			}
			leaves[leafName] = PresetLeaf{Candidates: candidates}
		}

		relations := make([]PresetRelation, 0, len(p.Relations))
		for _, r := range p.Relations {
			relations = append(relations, PresetRelation{
				Kind:   r.Kind,
				Leaves: append([]string(nil), r.Leaves...),
			})
		}

		out[name] = Preset{Selection: selection, Leaves: leaves, Relations: relations}
	}
	return out
}
