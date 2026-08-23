package launch

import (
	"fmt"
	"sort"

	"github.com/procrastivity/duo/internal/config"
)

// Tuple is one materialized launch candidate: §6.7 step 2's declared tuple
// `(composition locator, agent runtime, model line, launch variant, session
// host)`, plus the two fields a host launcher needs to prepare a spawn from
// it.
//
// Every string here comes from one resolved declaration. AgentRuntime is
// the *public* runtime kind (`codex`, `claude`, `pi`) rather than the
// config-local declaration key, because §7.3 makes the ordinary result name
// "the chosen candidate by its public agent-runtime and declared model-line
// values" and §6.4 graft 7 forbids config names from standing in as
// identity. The declaration keys travel alongside as locators, which is
// what records and error details cite.
type Tuple struct {
	// Composition is the candidate's declaration locator, e.g.
	// "compositions.review_codex_gpt56". It identifies the declaration,
	// never a Duo object.
	Composition string `json:"composition"`
	// AgentRuntime is the public agent-runtime kind, the value an
	// agent_runtime constraint compares against.
	AgentRuntime string `json:"agent_runtime"`
	// ModelLine is the composition's authored model-line label, the value a
	// model_line constraint compares against and the label a caller chains
	// on.
	ModelLine string `json:"model_line"`
	// LaunchVariant, AgentRuntimeDecl, and SessionHost are the remaining
	// declaration locators the tuple was materialized from.
	LaunchVariant    string `json:"launch_variant"`
	AgentRuntimeDecl string `json:"agent_runtime_declaration"`
	SessionHost      string `json:"session_host"`
	// IntegrationInstanceID is the session-host declaration's name: one
	// declared session host is one integration instance, which is the
	// identifier host adapters scope their evidence by.
	IntegrationInstanceID string `json:"integration_instance_id"`
	// Executable and Arguments are the launch variant's declared command.
	// They are declaration data, not resolved paths: nothing here consults
	// PATH, and no file is stat-ed (§7.1 forbids executable-path state).
	Executable string   `json:"executable"`
	Arguments  []string `json:"arguments,omitempty"`
}

// Limits are the finite maxima §6.7 requires validation to enforce.
// Exceeding one is a declaration error (config.composition_unresolved); the
// resolver never truncates its search, because candidate and leaf order are
// semantic inputs and a truncated search would silently change the lexical
// winner.
type Limits struct {
	MaxLeaves            int
	MaxCandidatesPerLeaf int
	MaxAssignments       int
}

// DefaultLimits are the shipped maxima. They are generous next to any
// declaration a person would write by hand and small enough that the
// assignment product stays a bounded in-memory enumeration.
func DefaultLimits() Limits {
	return Limits{MaxLeaves: 16, MaxCandidatesPerLeaf: 64, MaxAssignments: 4096}
}

// candidate is one declared candidate as it moves through the pipeline.
// Its stage fields are the record's raw material: §6.4 graft 3 wants
// pre-constraint, post-require, post-avoid, restored-on-relent, and final
// outcomes with stable reason codes, and every one of those is a fact about
// an individual candidate.
type candidate struct {
	tuple Tuple
	leaf  string
	// order is the candidate's index in its leaf's declared array. Array
	// order is preference order (§6.5 rule 4), so it is semantic.
	order int

	// eliminated is the stable reason this candidate left the pool, or ""
	// while it is still in it. A candidate is eliminated at most once: the
	// first stage that removes it owns the reason.
	eliminated string
	// restored records that an avoid elimination was undone by the relent
	// re-run (§6.7 step 8).
	restored bool
	// selected records that this candidate is in the chosen assignment.
	selected bool
	// supportDigest is the conformance record Support cited for this
	// tuple, whatever its verdict was.
	supportDigest string
}

// outcome is the candidate's final record outcome.
func (c *candidate) outcome() string {
	switch {
	case c.selected:
		return OutcomeSelected
	case c.eliminated != "":
		return OutcomeEliminated
	default:
		return OutcomeEligible
	}
}

// locator is the candidate's declaration locator.
func (c *candidate) locator() string { return c.tuple.Composition }

// leafPlan is one materialized leaf: its declared candidates in array
// order, plus the pools each pipeline stage leaves behind.
type leafPlan struct {
	name     string
	locator  string
	declared []*candidate

	// beforeConstraints is the pool after materialization and the
	// installed-evidence drop, and before any launch constraint. It is the
	// pool launch.no_eligible_candidate is judged against.
	beforeConstraints []*candidate
	afterRequire      []*candidate
	afterAvoid        []*candidate
}

// declaredKind is §6.5 rule 7's distinction, and it is about the
// *declaration*, not the outcome: a one-candidate leaf is determined, and a
// longer declared list stays open even when require narrows its surviving
// pool to exactly one.
func (l *leafPlan) declaredKind() string {
	if len(l.declared) == 1 {
		return "determined"
	}
	return "open"
}

// relation is one validated cross-leaf relation.
type relation struct {
	kind   string
	leaves []string
	// indexes are the positions of leaves in the plan's leaf order,
	// resolved once so relation checking inside the enumeration is an index
	// lookup rather than a name lookup.
	indexes []int
}

// relationDistinctModelLine is the only cross-leaf relation this Matter
// defines (§6.5 rule 5). It compares exact declared labels.
const relationDistinctModelLine = "distinct_model_line"

// plan is one fully materialized and validated preset.
type plan struct {
	preset    string
	locator   string
	selection string
	leaves    []*leafPlan
	relations []relation
}

// The two selection modes. Ordered is the contractual default; random is an
// explicit per-preset opt-in (§6.5 rule 4).
const (
	SelectionOrdered = "ordered"
	SelectionRandom  = "random"
)

// materialize validates one requested preset end to end and materializes
// every leaf's declared candidates as launch tuples — §6.7 steps 1 and 2.
//
// It is the only place a declaration defect can be found, and it finds all
// of them before any constraint is applied, which is what keeps
// config.composition_unresolved (declaration ambiguity) from ever being
// confused with launch.constraints_exhausted (narrowing).
//
// Leaf order is lexical by leaf name. That is a deviation from §6.7's "leaf
// declaration order", forced by internal/config: its resolved Preset.Leaves
// is a map[string]PresetLeaf, so YAML declaration order is already gone by
// the time a document reaches this package. Lexical order is deterministic,
// replayable, and stable across re-reads of the same document, which is
// what §7.2's determinism contract actually requires of it; recovering true
// declaration order needs an ordered leaf list in internal/config. See
// docs/launch/decisions.md.
func materialize(doc config.DocumentV2, presetName string, lim Limits) (*plan, *Error) {
	declared, ok := doc.Presets[presetName]
	if !ok {
		return nil, presetNotFound(presetName)
	}
	presetLocator := "presets." + presetName

	selection := declared.Selection
	switch selection {
	case SelectionOrdered, SelectionRandom:
	case "":
		// internal/config applies the contractual default already; an empty
		// value here means a caller built the document by hand.
		selection = SelectionOrdered
	default:
		return nil, unresolved(presetLocator, fmt.Sprintf(
			"selection %q is neither %q nor %q", selection, SelectionOrdered, SelectionRandom))
	}

	if len(declared.Leaves) == 0 {
		return nil, unresolved(presetLocator, "declares no leaves; a preset needs at least one")
	}
	if len(declared.Leaves) > lim.MaxLeaves {
		return nil, unresolved(presetLocator, fmt.Sprintf(
			"declares %d leaves, over the limit of %d", len(declared.Leaves), lim.MaxLeaves))
	}

	names := make([]string, 0, len(declared.Leaves))
	for name := range declared.Leaves {
		names = append(names, name)
	}
	sort.Strings(names)

	p := &plan{preset: presetName, locator: presetLocator, selection: selection}
	product := 1
	for _, name := range names {
		leaf, err := materializeLeaf(doc, presetLocator, name, declared.Leaves[name], lim)
		if err != nil {
			return nil, err
		}
		p.leaves = append(p.leaves, leaf)

		product *= len(leaf.declared)
		if product > lim.MaxAssignments {
			return nil, unresolved(presetLocator, fmt.Sprintf(
				"declares more than %d complete assignments; the resolver refuses an oversized declaration rather than truncating its search",
				lim.MaxAssignments))
		}
	}

	relations, err := materializeRelations(p, declared.Relations)
	if err != nil {
		return nil, err
	}
	p.relations = relations
	return p, nil
}

// materializeLeaf validates one leaf and materializes its candidates.
func materializeLeaf(doc config.DocumentV2, presetLocator, name string, declared config.PresetLeaf, lim Limits) (*leafPlan, *Error) {
	locator := presetLocator + ".leaves." + name
	if len(declared.Candidates) == 0 {
		return nil, unresolved(locator, "declares no candidates; a leaf needs at least one composition reference")
	}
	if len(declared.Candidates) > lim.MaxCandidatesPerLeaf {
		return nil, unresolved(locator, fmt.Sprintf(
			"declares %d candidates, over the limit of %d", len(declared.Candidates), lim.MaxCandidatesPerLeaf))
	}

	leaf := &leafPlan{name: name, locator: locator}
	seen := map[string]bool{}
	for i, ref := range declared.Candidates {
		if seen[ref.Composition] {
			return nil, unresolved(locator, fmt.Sprintf(
				"references composition %q twice; a leaf's references must be distinct", ref.Composition))
		}
		seen[ref.Composition] = true

		tuple, err := materializeTuple(doc, ref.Composition)
		if err != nil {
			return nil, err
		}
		leaf.declared = append(leaf.declared, &candidate{tuple: tuple, leaf: name, order: i})
	}
	return leaf, nil
}

// materializeTuple resolves one composition reference into a launch tuple,
// following the declaration chain composition -> launch variant -> agent
// runtime + session host. Every missing or malformed link on that chain is
// config.composition_unresolved: §6.5 rule 3, and the code's own registered
// meaning.
func materializeTuple(doc config.DocumentV2, name string) (Tuple, *Error) {
	locator := "compositions." + name
	comp, ok := doc.Compositions[name]
	if !ok {
		return Tuple{}, unresolved(locator, "is referenced by the preset but not declared")
	}
	// internal/config already refuses a composition with no launch_variant
	// or model_line, so reaching this with either empty means a
	// hand-built document.
	if comp.LaunchVariant == "" || comp.ModelLine == "" {
		return Tuple{}, unresolved(locator, "declares no launch_variant or no model_line")
	}

	variantLocator := "launch_variants." + comp.LaunchVariant
	variant, ok := doc.LaunchVariants[comp.LaunchVariant]
	if !ok {
		return Tuple{}, unresolved(variantLocator, fmt.Sprintf(
			"is referenced by %s but not declared", locator))
	}

	runtimeName, ok := stringField(variant, "agent_runtime")
	if !ok {
		return Tuple{}, unresolved(variantLocator, "declares no agent_runtime")
	}
	hostName, ok := stringField(variant, "session_host")
	if !ok {
		return Tuple{}, unresolved(variantLocator, "declares no session_host")
	}
	executable, ok := stringField(variant, "executable")
	if !ok {
		return Tuple{}, unresolved(variantLocator, "declares no executable")
	}
	arguments, argErr := stringSliceField(variant, "arguments")
	if argErr != "" {
		return Tuple{}, unresolved(variantLocator, argErr)
	}

	runtimeLocator := "agent_runtimes." + runtimeName
	runtimeDecl, ok := doc.AgentRuntimes[runtimeName]
	if !ok {
		return Tuple{}, unresolved(runtimeLocator, fmt.Sprintf(
			"is referenced by %s but not declared", variantLocator))
	}
	kind, ok := stringField(runtimeDecl, "kind")
	if !ok {
		return Tuple{}, unresolved(runtimeLocator,
			"declares no kind; the public agent-runtime value a constraint compares against is never inferred from the declaration name")
	}

	hostLocator := "session_hosts." + hostName
	if _, ok := doc.SessionHosts[hostName]; !ok {
		return Tuple{}, unresolved(hostLocator, fmt.Sprintf(
			"is referenced by %s but not declared", variantLocator))
	}

	return Tuple{
		Composition:           locator,
		AgentRuntime:          kind,
		ModelLine:             comp.ModelLine,
		LaunchVariant:         variantLocator,
		AgentRuntimeDecl:      runtimeLocator,
		SessionHost:           hostLocator,
		IntegrationInstanceID: hostName,
		Executable:            executable,
		Arguments:             arguments,
	}, nil
}

// materializeRelations validates every declared cross-leaf relation and
// resolves its leaf names to plan indexes.
func materializeRelations(p *plan, declared []config.PresetRelation) ([]relation, *Error) {
	index := map[string]int{}
	for i, leaf := range p.leaves {
		index[leaf.name] = i
	}

	out := make([]relation, 0, len(declared))
	for _, r := range declared {
		if r.Kind != relationDistinctModelLine {
			return nil, unresolved(p.locator, fmt.Sprintf(
				"declares relation kind %q; %q is the only defined cross-leaf relation",
				r.Kind, relationDistinctModelLine))
		}
		if len(r.Leaves) < 2 {
			return nil, unresolved(p.locator, fmt.Sprintf(
				"relation %s names %d leaves; a relation names at least two", r.Kind, len(r.Leaves)))
		}
		seen := map[string]bool{}
		indexes := make([]int, 0, len(r.Leaves))
		for _, name := range r.Leaves {
			if seen[name] {
				return nil, unresolved(p.locator, fmt.Sprintf(
					"relation %s names leaf %q twice", r.Kind, name))
			}
			seen[name] = true
			at, ok := index[name]
			if !ok {
				return nil, unresolved(p.locator, fmt.Sprintf(
					"relation %s names leaf %q, which the preset does not declare", r.Kind, name))
			}
			indexes = append(indexes, at)
		}
		out = append(out, relation{kind: r.Kind, leaves: append([]string(nil), r.Leaves...), indexes: indexes})
	}
	return out, nil
}

// stringField reads a required string field off a declaration map. A
// present-but-empty or wrongly-typed value reads as absent, so the caller's
// "declares no X" refusal covers all three — the same simplification
// internal/config records for compositions.
func stringField(m map[string]any, key string) (string, bool) {
	v, ok := m[key]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	if !ok || s == "" {
		return "", false
	}
	return s, true
}

// stringSliceField reads an optional list-of-strings field. It returns a
// non-empty reason string when the field is present but not a list of
// strings; an absent field is not an error (a launch variant with no
// arguments is ordinary).
func stringSliceField(m map[string]any, key string) ([]string, string) {
	v, ok := m[key]
	if !ok || v == nil {
		return nil, ""
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Sprintf("declares %s as %T; a list of strings is required", key, v)
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Sprintf("declares %s[%d] as %T; every argument must be a string", key, i, item)
		}
		out = append(out, s)
	}
	return out, ""
}
