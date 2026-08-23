package conformance

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// Canonical is the domain value every projection must decode to the same
// shape: a fixture (or a projection's request/result payload) parsed as
// plain JSON — objects as maps, arrays as slices, numbers as float64. Using
// encoding/json's default decode target on both sides of every comparison
// is what makes DeepEqual a valid equality check: every projection's
// decoder produces this exact Go shape, never a typed struct that could
// paper over a field a hand-written codec dropped.
type Canonical = map[string]any

// decodeCanonicalObject parses data as a JSON object into Canonical. Every
// fixture in the synced set is a top-level object; a fixture that were not
// would be a contract error, not a projection defect, so this fails hard
// rather than returning a partial value.
func decodeCanonicalObject(data []byte) (Canonical, error) {
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, fmt.Errorf("conformance: decoding JSON: %w", err)
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("conformance: decoded value is a %T, not a JSON object", v)
	}
	return obj, nil
}

// equalCanonical reports whether two canonical values are the same domain
// value. Both sides are always the output of decodeCanonicalObject (or a
// projection decoder built the same way), so DeepEqual is exact: no
// normalization step could hide a real divergence.
func equalCanonical(a, b Canonical) bool {
	return reflect.DeepEqual(a, b)
}

// cloneJSON returns a deep copy of v by round-tripping it through JSON. Used
// to build fixture variants (the neutrality gate's name-substituted
// envelopes) without aliasing nested maps between variants.
func cloneJSON[T any](v T) T {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("conformance: cloneJSON: marshal: %v", err))
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		panic(fmt.Sprintf("conformance: cloneJSON: unmarshal: %v", err))
	}
	return out
}
