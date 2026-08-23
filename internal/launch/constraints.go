package launch

import (
	"fmt"
	"sort"
	"strings"
)

// Axis is a launch-constraint axis. §6.6 fixes exactly two, and
// duo-external-v1's launch_constraint_axis enumerates the same pair: a
// constraint can name the public agent runtime or the declared model line
// and nothing else. There is no session-host, launch-variant, composition,
// provider, vendor, or capability axis, deliberately (§9.2).
type Axis string

// The two constraint axes.
const (
	AxisAgentRuntime Axis = "agent_runtime"
	AxisModelLine    Axis = "model_line"
)

// Valid reports whether a is one of the two declared axes.
func (a Axis) Valid() bool { return a == AxisAgentRuntime || a == AxisModelLine }

// Predicate is one exact-equality axis/value pair, in the shape
// duo-external-v1's launch_constraint_predicate fixes: axis and value, and
// no third field. It is the safe wire form — provenance never rides on it.
type Predicate struct {
	Axis  Axis   `json:"axis"`
	Value string `json:"value"`
}

// String renders a predicate the way the exhaustion message names it.
func (p Predicate) String() string { return string(p.Axis) + "=" + p.Value }

// matches reports whether t satisfies this predicate's exact equality.
// §6.6: both verbs are exact-equality over the *declared* labels. No
// normalization, case folding, prefix rule, or family lineage is applied —
// §6.4 graft 1 puts the model-line label on the determined composition
// precisely so that comparison is a string comparison of authored intent.
func (p Predicate) matches(t Tuple) bool {
	switch p.Axis {
	case AxisAgentRuntime:
		return t.AgentRuntime == p.Value
	case AxisModelLine:
		return t.ModelLine == p.Value
	default:
		return false
	}
}

// Constraint is one requested predicate together with its provenance: the
// layer or flag that asked for it. §6.6 requires repeated equal constraints
// to "normalize while preserving provenance", so the source travels into
// the durable record even though it never reaches the safe wire detail.
type Constraint struct {
	Axis  Axis
	Value string
	// Source names where the constraint came from — "flag", "policy:site",
	// "preset-default". Free-form: this package records it and never
	// interprets it.
	Source string
}

// NormalizedConstraint is one deduplicated constraint plus every source
// that asked for it. It is a record-only shape (§6.9: normalized
// constraints and provenance belong to the launch-resolution record).
type NormalizedConstraint struct {
	Axis    Axis     `json:"axis"`
	Value   string   `json:"value"`
	Sources []string `json:"sources,omitempty"`
}

// predicate projects the safe wire pair out of a normalized constraint.
func (c NormalizedConstraint) predicate() Predicate {
	return Predicate{Axis: c.Axis, Value: c.Value}
}

// Constraints is the normalized effective request: at most one required
// value per axis, and any number of avoids, each carrying every source that
// asked for it, in first-request order.
type Constraints struct {
	Require []NormalizedConstraint `json:"require"`
	Avoid   []NormalizedConstraint `json:"avoid"`
}

// wire projects the constraint set into the safe error-detail shape the
// session-launch-exhausted fixture fixes: axis/value pairs only, in
// normalized order, with provenance left behind in the record.
func (s Constraints) wire() wireConstraints {
	out := wireConstraints{
		Require: make([]Predicate, 0, len(s.Require)),
		Avoid:   make([]Predicate, 0, len(s.Avoid)),
	}
	for _, c := range s.Require {
		out.Require = append(out.Require, c.predicate())
	}
	for _, c := range s.Avoid {
		out.Avoid = append(out.Avoid, c.predicate())
	}
	return out
}

// wireConstraints is the "constraints" object inside a launch error's safe
// details.
type wireConstraints struct {
	Require []Predicate `json:"require"`
	Avoid   []Predicate `json:"avoid"`
}

// normalize turns the requested require/avoid lists into one effective
// constraint set, applying §6.6's normalization rules:
//
//   - an unknown axis or an empty value is malformed flag grammar
//     (invalid.request);
//   - repeated equal constraints collapse to one entry that keeps every
//     provenance string, in first-request order; and
//   - two *different* required values on one axis are contradictory
//     (invalid.request), because "there is at most one required value per
//     axis in one effective request". Two different avoided values on one
//     axis are ordinary: avoid is a set of exclusions, not a selection.
//
// A require and an avoid that name the same value are not a contradiction
// here. Avoid is soft: the require narrows the pool, the avoid empties it,
// and §6.7 step 8 relents — reporting the matched avoid on the selected
// assignment. Refusing that combination up front would turn a relenting
// predicate into a hard one.
func normalize(require, avoid []Constraint) (Constraints, *Error) {
	req, err := normalizeList(require, "require")
	if err != nil {
		return Constraints{}, err
	}
	byAxis := map[Axis]string{}
	for _, c := range req {
		if prev, seen := byAxis[c.Axis]; seen && prev != c.Value {
			return Constraints{}, invalidRequest(fmt.Sprintf(
				"Contradictory requirements on axis %s: %q and %q; one effective request admits at most one required value per axis.",
				c.Axis, prev, c.Value))
		}
		byAxis[c.Axis] = c.Value
	}
	avd, err := normalizeList(avoid, "avoid")
	if err != nil {
		return Constraints{}, err
	}
	return Constraints{Require: req, Avoid: avd}, nil
}

// normalizeList deduplicates one verb's constraints by (axis, value),
// preserving first-request order and merging provenance.
func normalizeList(in []Constraint, verb string) ([]NormalizedConstraint, *Error) {
	out := make([]NormalizedConstraint, 0, len(in))
	index := map[Predicate]int{}
	for _, c := range in {
		if !c.Axis.Valid() {
			return nil, invalidRequest(fmt.Sprintf(
				"A %s names axis %q; the admissible axes are %s and %s.",
				verb, c.Axis, AxisAgentRuntime, AxisModelLine))
		}
		if c.Value == "" {
			return nil, invalidRequest(fmt.Sprintf(
				"A %s on axis %s carries an empty value.", verb, c.Axis))
		}
		key := Predicate{Axis: c.Axis, Value: c.Value}
		at, seen := index[key]
		if !seen {
			index[key] = len(out)
			out = append(out, NormalizedConstraint{Axis: c.Axis, Value: c.Value})
			at = len(out) - 1
		}
		if c.Source != "" && !containsString(out[at].Sources, c.Source) {
			out[at].Sources = append(out[at].Sources, c.Source)
			sort.Strings(out[at].Sources)
		}
	}
	return out, nil
}

// joinPredicates renders a normalized list for a failure message.
func joinPredicates(cs []NormalizedConstraint) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.predicate().String())
	}
	return strings.Join(parts, ", ")
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
