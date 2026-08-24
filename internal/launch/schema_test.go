package launch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// This file validates the payloads internal/launch emits against the
// amended duo-external-v1 schema, read from the embedded contracts.FS.
// Before Step 16 synced the contract set (workplan Risk 6), the embedded
// schema was still the pre-v3 one (only step 10's three hand-patched enum
// lists), so this file read a testdata copy taken at
// ~/Code/terminal-multiplexers commit 767f413 (step 02's compatible adds,
// branch A) instead; that copy is gone now that contracts.FS carries the
// synced schema.
//
// The validator below is a deliberate subset. It covers exactly the JSON
// Schema keywords the launch $defs use, and refuses any keyword it does not
// know rather than ignoring it — a validator that silently skipped a
// constraint would report a payload as valid on the strength of not having
// checked it. Whole-envelope validation, which needs the root document's
// oneOf/allOf/if-then machinery, is done out of band with check-jsonschema
// and recorded in the step's finding.

// externalV1Schema loads the amended schema from the embedded contract set.
func externalV1Schema(t *testing.T) map[string]any {
	t.Helper()
	data, err := contracts.FS.ReadFile("schemas/duo-external-v1.schema.json")
	if err != nil {
		t.Fatalf("reading embedded schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decoding embedded schema: %v", err)
	}
	return doc
}

// TestLaunchPayloadsValidateAgainstTheAmendedSchema runs every payload this
// step grows through the schema def that governs it.
func TestLaunchPayloadsValidateAgainstTheAmendedSchema(t *testing.T) {
	schema := externalV1Schema(t)

	t.Run("ordinary result", func(t *testing.T) {
		doc := parseDoc(t, herdrAmbientYAML)
		r := newResolverOver(t, doc, herdrAmbient(t, doc), func(o *launch.Options) {
			o.HostVersions = map[string]string{"herdr": "0.6.0"}
		})
		for _, preset := range []string{"review", "verify", "builder_and_verifier"} {
			env := resultEnvelope(t, r, launch.Request{Preset: preset}, "req_schema_"+preset)
			validate(t, schema, "launch_resolution_report", env["result"])
		}
	})

	t.Run("constraints exhausted", func(t *testing.T) {
		doc := parseDoc(t, exhaustedFixtureYAML)
		r := newResolverOver(t, doc, herdrAmbient(t, doc), func(o *launch.Options) {
			o.HostVersions = map[string]string{"herdr": "0.6.0"}
		})
		env := errEnvelope(t, r, launch.Request{
			Preset:  "review",
			Require: []launch.Constraint{{Axis: launch.AxisModelLine, Value: "claude-opus-4"}},
		}, "req_schema_exhausted")
		validateFailure(t, schema, env)
	})

	t.Run("no eligible candidate", func(t *testing.T) {
		doc := parseDoc(t, providerDisabledYAML)
		r := newResolverOver(t, doc, soloCorrelated(t, doc, true), func(o *launch.Options) {
			o.HostVersions = map[string]string{"solo": "0.1.0"}
		})
		validateFailure(t, schema, errEnvelope(t, r, launch.Request{Preset: "codex_any"}, "req_schema_provider"))
	})

	t.Run("mixed exhaustion", func(t *testing.T) {
		doc := parseDoc(t, mixedExhaustedYAML)
		r := newResolverOver(t, doc, soloCorrelated(t, doc, false), func(o *launch.Options) {
			o.HostVersions = map[string]string{"solo": "0.1.0"}
		})
		validateFailure(t, schema, errEnvelope(t, r, launch.Request{
			Preset:  "build_and_verify",
			Require: []launch.Constraint{{Axis: launch.AxisModelFamily, Value: "gpt"}},
		}, "req_schema_mixed"))
	})

	// config.ParseV3 already refuses an undeclared variant reference, so the
	// declaration defect the resolver still owns is the complexity ceiling:
	// a leaf over MaxCandidatesPerLeaf. It raises the same code.
	t.Run("variant unresolved", func(t *testing.T) {
		doc := parseDoc(t, herdrAmbientYAML)
		r := newResolverOver(t, doc, herdrAmbient(t, doc), func(o *launch.Options) {
			o.Limits = launch.Limits{MaxLeaves: 4, MaxCandidatesPerLeaf: 1, MaxAssignments: 8}
		})
		env := errEnvelope(t, r, launch.Request{Preset: "review"}, "req_schema_variant")
		e := errorObject(t, env)
		if got := str(e["code"]); got != launch.CodeVariantUnresolved {
			t.Fatalf("code = %q, want %q", got, launch.CodeVariantUnresolved)
		}
		if got := str(e["class"]); got != "unavailable" {
			t.Errorf("class = %q, want unavailable", got)
		}
		if got := str(e["effect"]); got != "no_effect" {
			t.Errorf("effect = %q, want no_effect", got)
		}
		validate(t, schema, "error", env["error"])
		validate(t, schema, "config_declaration_details", detailsOf(e))
	})

	t.Run("host unresolved", func(t *testing.T) {
		_, err := materialize.Materialize(context.Background(), materialize.Options{
			WorkspaceFlag:   fixtureWorkspacePath,
			RequestedPreset: "review",
			Policy:          config.SessionHostPolicy{},
			Correlations:    fixedCorrelations{root: fixtureWorkspacePath, workspace: "ws_none"},
			LookupEnv:       ambient(nil),
			Now:             fixedClock(),
		})
		merr, ok := err.(*materialize.Error)
		if !ok {
			t.Fatalf("materialization returned %T, want *materialize.Error", err)
		}
		env := canonical(t, errorEnvelope{
			Schema:    "duo.external/v1",
			RequestID: "req_schema_host_unresolved",
			Operation: "session.launch",
			Error:     merr,
		})
		validate(t, schema, "error", env["error"])
		validate(t, schema, "launch_host_unresolved_details", detailsOf(errorObject(t, env)))
	})
}

// TestHostSourceVocabularyMatchesTheSchema pins the closed `host_source`
// enum step 02 added to the domain vocabulary M1 emits. It is the
// duo-config-v3 counterpart of vocab_test.go's three enum pins, which
// cannot cover host_source: the embedded contract schema has no such def
// yet.
func TestHostSourceVocabularyMatchesTheSchema(t *testing.T) {
	want := schemaEnum(t, externalV1Schema(t), "host_source")
	got := []string{
		string(domain.HostSourceExplicitFlag),
		string(domain.HostSourceWorkspaceCorrelation),
		string(domain.HostSourceAmbientEnv),
		string(domain.HostSourcePolicyDefault),
	}
	assertSameSet(t, "host_source", got, want)
}

// validateFailure validates a whole failure envelope's error object and its
// launch failure details.
func validateFailure(t *testing.T, schema map[string]any, env map[string]any) {
	t.Helper()
	e := errorObject(t, env)
	validate(t, schema, "error", env["error"])
	validate(t, schema, "launch_failure_details", detailsOf(e))
}

// --- the subset validator -------------------------------------------------

// validate checks value against $defs.<def> and fails t with every
// violation it found.
func validate(t *testing.T, schema map[string]any, def string, value any) {
	t.Helper()
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("schema carries no $defs object")
	}
	sub, ok := defs[def].(map[string]any)
	if !ok {
		t.Fatalf("schema $defs carries no %q", def)
	}
	v := &validator{t: t, defs: defs}
	v.check("$defs."+def, sub, value)
}

type validator struct {
	t    *testing.T
	defs map[string]any
}

// knownKeywords is the subset this validator implements. Anything else in a
// schema it is asked to apply is a hard failure, not a skip: the point of
// validating in-repo is to be told when the contract grows a constraint
// this test cannot see.
var knownKeywords = map[string]bool{
	"$ref": true, "type": true, "properties": true, "required": true,
	"additionalProperties": true, "items": true, "enum": true, "const": true,
	"minLength": true, "minimum": true, "minItems": true, "pattern": true,
	// Annotations, carried by the contract for readers rather than
	// validators.
	"description": true, "title": true, "format": true, "deprecated": true,
	"examples": true, "default": true,
}

func (v *validator) check(path string, schema map[string]any, value any) {
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		sub, ok := v.defs[name].(map[string]any)
		if !ok {
			v.t.Fatalf("%s: schema $defs carries no %q", path, name)
		}
		v.check(path, sub, value)
		return
	}
	for keyword := range schema {
		if !knownKeywords[keyword] {
			v.t.Fatalf("%s: schema keyword %q is outside this validator's subset; extend it rather than trusting the result",
				path, keyword)
		}
	}

	if want, ok := schema["const"]; ok && !equalJSON(value, want) {
		v.t.Errorf("%s = %v, want const %v", path, value, want)
	}
	if raw, ok := schema["enum"].([]any); ok {
		found := false
		for _, want := range raw {
			if equalJSON(value, want) {
				found = true
			}
		}
		if !found {
			v.t.Errorf("%s = %v, which is outside the enum %v", path, value, raw)
		}
	}

	switch want, _ := schema["type"].(string); want {
	case "object":
		v.checkObject(path, schema, value)
	case "array":
		v.checkArray(path, schema, value)
	case "string":
		s, ok := value.(string)
		if !ok {
			v.t.Errorf("%s is a %T, want a string", path, value)
			return
		}
		if limit, ok := schema["minLength"].(float64); ok && float64(len(s)) < limit {
			v.t.Errorf("%s = %q, shorter than minLength %v", path, s, limit)
		}
		if pattern, ok := schema["pattern"].(string); ok {
			re, err := regexp.Compile(pattern)
			if err != nil {
				v.t.Fatalf("%s: schema pattern %q does not compile: %v", path, pattern, err)
			}
			if !re.MatchString(s) {
				v.t.Errorf("%s = %q, which does not match %q", path, s, pattern)
			}
		}
	case "integer", "number":
		n, ok := value.(float64)
		if !ok {
			v.t.Errorf("%s is a %T, want a number", path, value)
			return
		}
		if limit, ok := schema["minimum"].(float64); ok && n < limit {
			v.t.Errorf("%s = %v, below minimum %v", path, n, limit)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			v.t.Errorf("%s is a %T, want a boolean", path, value)
		}
	case "":
		// A def with no type (host_source, opaque_id via $ref) constrains
		// through enum, const, or its properties only.
		if _, hasProps := schema["properties"]; hasProps {
			v.checkObject(path, schema, value)
		}
	default:
		v.t.Fatalf("%s: schema type %q is outside this validator's subset", path, want)
	}
}

func (v *validator) checkObject(path string, schema map[string]any, value any) {
	m, ok := value.(map[string]any)
	if !ok {
		v.t.Errorf("%s is a %T, want an object", path, value)
		return
	}
	props, _ := schema["properties"].(map[string]any)
	for _, raw := range sliceOf(schema["required"]) {
		name, _ := raw.(string)
		if _, ok := m[name]; !ok {
			v.t.Errorf("%s is missing required property %q", path, name)
		}
	}
	if extra, ok := schema["additionalProperties"].(bool); ok && !extra {
		var unknown []string
		for name := range m {
			if _, ok := props[name]; !ok {
				unknown = append(unknown, name)
			}
		}
		sort.Strings(unknown)
		for _, name := range unknown {
			v.t.Errorf("%s carries property %q, which the schema forbids (additionalProperties: false)", path, name)
		}
	}
	for name, raw := range props {
		child, ok := m[name]
		if !ok {
			continue
		}
		sub, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		v.check(path+"."+name, sub, child)
	}
}

func (v *validator) checkArray(path string, schema map[string]any, value any) {
	items, ok := value.([]any)
	if !ok {
		v.t.Errorf("%s is a %T, want an array", path, value)
		return
	}
	if limit, ok := schema["minItems"].(float64); ok && float64(len(items)) < limit {
		v.t.Errorf("%s has %d items, below minItems %v", path, len(items), limit)
	}
	sub, ok := schema["items"].(map[string]any)
	if !ok {
		return
	}
	for i, item := range items {
		v.check(fmt.Sprintf("%s[%d]", path, i), sub, item)
	}
}

func sliceOf(v any) []any {
	s, _ := v.([]any)
	return s
}

func equalJSON(a, b any) bool { return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b) }
