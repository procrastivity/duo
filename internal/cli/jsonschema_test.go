package cli

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/procrastivity/duo/contracts"
)

// This file is a small, purpose-built JSON Schema checker over exactly the
// constructs contracts/schemas/duo-external-v1.schema.json uses: $ref
// against $defs, allOf with if/then, oneOf, type, enum, const, properties,
// additionalProperties, items, required, minItems, and minLength. It is not
// a general-purpose validator — the repo carries no JSON Schema library
// dependency (go.mod, flake.nix's pinned vendorHash), and adding one for
// this step's session --output json conformance test would risk breaking
// `nix develop --command make check`'s hermetic build. See the step-21 wip
// findings.

// loadSchema reads and decodes one embedded JSON Schema document.
func loadSchema(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := contracts.FS.ReadFile(path)
	if err != nil {
		t.Fatalf("reading embedded schema %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parsing embedded schema %s: %v", path, err)
	}
	return doc
}

// assertValidExternalV1 decodes raw as JSON and checks it against the
// embedded duo.external/v1 schema, failing t with every violation found.
func assertValidExternalV1(t *testing.T, raw []byte) {
	t.Helper()
	schema := loadSchema(t, "schemas/duo-external-v1.schema.json")
	var instance any
	if err := json.Unmarshal(raw, &instance); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, raw)
	}
	if errs := validateNode(schema, schema, instance); len(errs) > 0 {
		t.Fatalf("duo.external/v1 schema violations:\n  %s\ninstance:\n%s",
			strings.Join(errs, "\n  "), raw)
	}
}

// resolveRef follows a "$ref": "#/$defs/name" against root's $defs. A
// schema with no $ref passes through unchanged.
func resolveRef(root, schema map[string]any) map[string]any {
	ref, ok := schema["$ref"].(string)
	if !ok {
		return schema
	}
	name := strings.TrimPrefix(ref, "#/$defs/")
	defs, _ := root["$defs"].(map[string]any)
	target, _ := defs[name].(map[string]any)
	return target
}

// validateNode checks v against schema (resolved against root's $defs),
// returning every violation found. Keywords are independent constraints, as
// in JSON Schema: every one present on schema is checked, and their
// violations accumulate rather than short-circuit, so one failing call
// reports everything wrong at once.
func validateNode(root, schema map[string]any, v any) []string {
	if schema == nil {
		return nil
	}
	schema = resolveRef(root, schema)
	if schema == nil {
		return []string{"$ref target not found"}
	}
	var errs []string

	if oneOf, ok := schema["oneOf"].([]any); ok {
		matched := false
		for _, s := range oneOf {
			if sm, ok := s.(map[string]any); ok && len(validateNode(root, sm, v)) == 0 {
				matched = true
				break
			}
		}
		if !matched {
			errs = append(errs, "value matches none of oneOf")
		}
	}

	if allOf, ok := schema["allOf"].([]any); ok {
		for _, s := range allOf {
			sm, ok := s.(map[string]any)
			if !ok {
				continue
			}
			if ifSchema, ok := sm["if"].(map[string]any); ok {
				if len(validateNode(root, ifSchema, v)) != 0 {
					continue // "if" did not match: "then" does not apply.
				}
				if thenSchema, ok := sm["then"].(map[string]any); ok {
					errs = append(errs, validateNode(root, thenSchema, v)...)
				}
				continue
			}
			errs = append(errs, validateNode(root, sm, v)...)
		}
	}

	if constVal, ok := schema["const"]; ok {
		if !reflect.DeepEqual(constVal, v) {
			errs = append(errs, fmt.Sprintf("expected const %v, got %v", constVal, v))
		}
	}
	if enumVals, ok := schema["enum"].([]any); ok {
		found := false
		for _, e := range enumVals {
			if reflect.DeepEqual(e, v) {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Sprintf("value %v is not one of enum %v", v, enumVals))
		}
	}
	if want, ok := schema["type"].(string); ok && !matchesType(want, v) {
		errs = append(errs, fmt.Sprintf("expected type %s, got %T (%v)", want, v, v))
	}

	switch vv := v.(type) {
	case map[string]any:
		if required, ok := schema["required"].([]any); ok {
			for _, r := range required {
				key, _ := r.(string)
				if _, present := vv[key]; !present {
					errs = append(errs, fmt.Sprintf("missing required field %q", key))
				}
			}
		}
		props, _ := schema["properties"].(map[string]any)
		addl, hasAddl := schema["additionalProperties"]
		for key, val := range vv {
			if propSchema, ok := props[key]; ok {
				psm, _ := propSchema.(map[string]any)
				errs = append(errs, validateNode(root, psm, val)...)
				continue
			}
			if hasAddl {
				if allowed, ok := addl.(bool); ok && !allowed {
					errs = append(errs, fmt.Sprintf("unexpected additional property %q", key))
				}
			}
		}
	case []any:
		if minItems, ok := schema["minItems"].(float64); ok && float64(len(vv)) < minItems {
			errs = append(errs, fmt.Sprintf("array has %d items, want at least %v", len(vv), minItems))
		}
		if items, ok := schema["items"].(map[string]any); ok {
			for _, item := range vv {
				errs = append(errs, validateNode(root, items, item)...)
			}
		}
	case string:
		if minLen, ok := schema["minLength"].(float64); ok && float64(len(vv)) < minLen {
			errs = append(errs, fmt.Sprintf("string %q is shorter than minLength %v", vv, minLen))
		}
	}
	return errs
}

// matchesType checks v against one JSON Schema primitive type name. Every
// JSON number decodes as float64 via encoding/json into `any`, so
// "integer" additionally requires no fractional part.
func matchesType(want string, v any) bool {
	switch want {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == math.Trunc(f)
	case "null":
		return v == nil
	default:
		return true
	}
}
