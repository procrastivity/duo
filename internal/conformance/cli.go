package conformance

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// EncodeCLI renders canonical as the CLI's --output json wire form. Stage 0
// has no generated CLI result codec (docs/conformance/decisions.md, "the
// identity codec"): the CLI's json mode is defined here to print the
// envelope verbatim, so this is the identity encoding over Canonical. The
// seam exists so a later stage can replace it with the generated codec
// without touching a caller.
func EncodeCLI(v Canonical) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("conformance: EncodeCLI: %v", err))
	}
	return data
}

// DecodeCLI parses a CLI --output json result line back to Canonical.
func DecodeCLI(data []byte) (Canonical, error) {
	return decodeCanonicalObject(data)
}

// CaseArgSpec describes how one projection-cases.json case's CLI argument
// vector maps to its semantic.input value: which positional slots after the
// verb path fill which key, which "--flag" names alias a differently spelled
// semantic key, and which semantic keys are numbers rather than strings.
// This table is per-case (keyed by the case's stable "name"), not a general
// argument grammar — Stage 0's CLI has no flag parser to reuse yet
// (internal/cli registers only "duo version"), so the harness owns a
// deliberately small, explicit mapping instead of guessing one
// structurally. It never branches on an integration name — only on the
// case's own request shape.
type CaseArgSpec struct {
	// Positional lists the semantic keys taken from CLI positional
	// arguments, in order, after the verb path is consumed.
	Positional []string
	// FlagAlias maps a "--flag-name" (without the leading "--") to the
	// semantic key it fills, when kebab-to-snake conversion of the flag
	// name would not already produce that key.
	FlagAlias map[string]string
	// Numeric lists semantic keys whose CLI flag value is a decimal
	// integer and must decode to a JSON number, not a string.
	Numeric map[string]bool
}

// kebabToSnake converts "runtime-instance" to "runtime_instance", the
// default flag-to-key mapping used when spec.FlagAlias has no entry.
func kebabToSnake(s string) string {
	return strings.ReplaceAll(s, "-", "_")
}

// DecodeCLIRequestArgs parses a projection-cases.json CLI argument vector
// into Canonical, using verbPath (the registered CLI verb path for the
// case's operation) to find where positional/flag parsing starts and spec
// to resolve positional slots, flag aliases, and numeric coercion. The
// "--output json" flag is a projection-modality selector, not domain data,
// and is dropped.
func DecodeCLIRequestArgs(args []string, verbPath []string, spec CaseArgSpec) (Canonical, error) {
	if len(args) < len(verbPath) {
		return nil, fmt.Errorf("conformance: CLI arguments %v shorter than verb path %v", args, verbPath)
	}
	for i, seg := range verbPath {
		if args[i] != seg {
			return nil, fmt.Errorf("conformance: CLI arguments %v do not start with verb path %v", args, verbPath)
		}
	}
	rest := args[len(verbPath):]

	out := Canonical{}
	posIdx := 0
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		switch {
		case tok == "--output":
			i++ // skip its value ("json")
		case strings.HasPrefix(tok, "--"):
			name := strings.TrimPrefix(tok, "--")
			if i+1 >= len(rest) {
				return nil, fmt.Errorf("conformance: flag %q has no value in %v", tok, args)
			}
			value := rest[i+1]
			i++
			key := spec.FlagAlias[name]
			if key == "" {
				key = kebabToSnake(name)
			}
			out[key] = decodeArgValue(key, value, spec.Numeric)
		default:
			if posIdx >= len(spec.Positional) {
				return nil, fmt.Errorf("conformance: unexpected positional argument %q in %v (spec declares %d positional slots)", tok, args, len(spec.Positional))
			}
			key := spec.Positional[posIdx]
			posIdx++
			out[key] = decodeArgValue(key, tok, spec.Numeric)
		}
	}
	if posIdx != len(spec.Positional) {
		return nil, fmt.Errorf("conformance: %v filled only %d of %d declared positional slots", args, posIdx, len(spec.Positional))
	}
	return out, nil
}

// decodeArgValue coerces a raw CLI token to the JSON-typed value semantic
// keys carry: a number when spec.Numeric names the key, the string
// otherwise. Cursor and ID values that happen to look numeric (a stream
// "after" position) stay strings unless the spec says otherwise — the
// schema's own type, not the token's shape, decides.
func decodeArgValue(key, raw string, numeric map[string]bool) any {
	if numeric[key] {
		n, err := strconv.ParseFloat(raw, 64)
		if err == nil {
			return n
		}
	}
	return raw
}
