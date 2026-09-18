package portablelauncher

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/procrastivity/duo/contracts"
)

var externalSchemaOnce = sync.OnceValues(func() (map[string]any, error) {
	data, err := contracts.FS.ReadFile("schemas/duo-external-v1.schema.json")
	if err != nil {
		return nil, err
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, err
	}
	return schema, nil
})

// ValidateExternalEnvelope validates normalized captured Duo evidence against
// the pinned public schema using the standard-library JSON value model.
func ValidateExternalEnvelope(envelope map[string]any) error {
	schema, err := externalSchemaOnce()
	if err != nil {
		return fmt.Errorf("load duo.external/v1 schema: %w", err)
	}
	var problems Problems
	validateSchemaNode(schema, schema, envelope, "$", &problems)
	return problems.Err()
}

func validateSchemaNode(root, schema map[string]any, value any, path string, problems *Problems) {
	if ref, ok := schema["$ref"].(string); ok {
		name := strings.TrimPrefix(ref, "#/$defs/")
		defs, _ := root["$defs"].(map[string]any)
		target, ok := defs[name].(map[string]any)
		if !ok {
			problems.Add(path + ": unresolved schema reference " + ref)
			return
		}
		validateSchemaNode(root, target, value, path, problems)
		return
	}
	if alternatives, ok := schema["oneOf"].([]any); ok {
		matches := 0
		for _, raw := range alternatives {
			candidate, _ := raw.(map[string]any)
			var candidateProblems Problems
			validateSchemaNode(root, candidate, value, path, &candidateProblems)
			if len(candidateProblems.Items) == 0 {
				matches++
			}
		}
		if matches != 1 {
			problems.Add(fmt.Sprintf("%s: matches %d oneOf alternatives, want exactly one", path, matches))
		}
	}
	if clauses, ok := schema["allOf"].([]any); ok {
		for _, raw := range clauses {
			clause, _ := raw.(map[string]any)
			if condition, ok := clause["if"].(map[string]any); ok {
				var conditionProblems Problems
				validateSchemaNode(root, condition, value, path, &conditionProblems)
				if len(conditionProblems.Items) == 0 {
					if thenSchema, ok := clause["then"].(map[string]any); ok {
						validateSchemaNode(root, thenSchema, value, path, problems)
					}
				} else if elseSchema, ok := clause["else"].(map[string]any); ok {
					validateSchemaNode(root, elseSchema, value, path, problems)
				}
				continue
			}
			validateSchemaNode(root, clause, value, path, problems)
		}
	}
	if constValue, ok := schema["const"]; ok && !reflect.DeepEqual(constValue, value) {
		problems.Add(fmt.Sprintf("%s: value does not match const", path))
	}
	if values, ok := schema["enum"].([]any); ok {
		matched := false
		for _, candidate := range values {
			matched = matched || reflect.DeepEqual(candidate, value)
		}
		if !matched {
			problems.Add(fmt.Sprintf("%s: value is outside enum", path))
		}
	}
	if rawType, ok := schema["type"]; ok && !schemaTypeMatches(rawType, value) {
		problems.Add(fmt.Sprintf("%s: type mismatch", path))
		return
	}

	switch typed := value.(type) {
	case map[string]any:
		if minimum, ok := schema["minProperties"].(float64); ok && len(typed) < int(minimum) {
			problems.Add(fmt.Sprintf("%s: too few properties", path))
		}
		if required, ok := schema["required"].([]any); ok {
			for _, raw := range required {
				key, _ := raw.(string)
				if _, present := typed[key]; !present {
					problems.Add(fmt.Sprintf("%s: missing required property %s", path, key))
				}
			}
		}
		properties, _ := schema["properties"].(map[string]any)
		additional, hasAdditional := schema["additionalProperties"]
		for key, child := range typed {
			if raw, ok := properties[key]; ok {
				childSchema, _ := raw.(map[string]any)
				validateSchemaNode(root, childSchema, child, path+"."+key, problems)
				continue
			}
			if allowed, ok := additional.(bool); hasAdditional && ok && !allowed {
				problems.Add(fmt.Sprintf("%s: additional property %s", path, key))
			} else if childSchema, ok := additional.(map[string]any); hasAdditional && ok {
				validateSchemaNode(root, childSchema, child, path+"."+key, problems)
			}
		}
	case []any:
		if minimum, ok := schema["minItems"].(float64); ok && len(typed) < int(minimum) {
			problems.Add(fmt.Sprintf("%s: too few items", path))
		}
		if unique, _ := schema["uniqueItems"].(bool); unique {
			seen := map[string]bool{}
			for _, item := range typed {
				encoded, _ := json.Marshal(item)
				if seen[string(encoded)] {
					problems.Add(fmt.Sprintf("%s: duplicate array item", path))
				}
				seen[string(encoded)] = true
			}
		}
		if itemSchema, ok := schema["items"].(map[string]any); ok {
			for i, item := range typed {
				validateSchemaNode(root, itemSchema, item, fmt.Sprintf("%s[%d]", path, i), problems)
			}
		}
	case string:
		if minimum, ok := schema["minLength"].(float64); ok && len(typed) < int(minimum) {
			problems.Add(fmt.Sprintf("%s: string is too short", path))
		}
		if pattern, ok := schema["pattern"].(string); ok {
			compiled, err := regexp.Compile(pattern)
			if err != nil || !compiled.MatchString(typed) {
				problems.Add(fmt.Sprintf("%s: string does not match pattern", path))
			}
		}
		if format, _ := schema["format"].(string); format == "date-time" {
			if _, err := time.Parse(time.RFC3339Nano, typed); err != nil {
				problems.Add(fmt.Sprintf("%s: invalid date-time", path))
			}
		}
	case float64:
		if minimum, ok := schema["minimum"].(float64); ok && typed < minimum {
			problems.Add(fmt.Sprintf("%s: number is below minimum", path))
		}
	}
}

func schemaTypeMatches(rawType, value any) bool {
	if alternatives, ok := rawType.([]any); ok {
		for _, alternative := range alternatives {
			if schemaTypeMatches(alternative, value) {
				return true
			}
		}
		return false
	}
	typeName, _ := rawType.(string)
	switch typeName {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		_, ok := value.(float64)
		return ok
	case "integer":
		number, ok := value.(float64)
		return ok && number == math.Trunc(number)
	case "null":
		return value == nil
	default:
		return true
	}
}
