package launch

import (
	"fmt"
	"sort"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// Tuple is one materialized launch candidate.
//
// Under `duo.config/v2` a tuple was a dereference of one authored
// `compositions.<name>` declaration, and the session host was one of the
// names on that chain. `duo.config/v3` removes the composition object and
// late-binds the host (notes/42 §2 and §4 step 2, notes/43 item 13,
// 2026-08-24 handoff 22), so a tuple is now a **join**: one declared launch
// variant × the one host M1 deduced for this launch. The join is where a
// composition is *minted*; nothing authors one.
//
// Every declaration-side string here comes from one resolved variant.
// AgentRuntime is the *public* runtime kind (`codex`, `claude`, `pi`)
// rather than the config-local declaration key, because §7.3 makes the
// ordinary result name "the chosen candidate by its public agent-runtime
// and declared model-line values" and §6.4 graft 7 forbids config names
// from standing in as identity. The declaration keys travel alongside as
// locators, which is what records and error details cite.
type Tuple struct {
	// Composition is the minted composition name: `<variant>@<host_kind>`
	// (contracts/fixtures/duo-external-v1/session-launch.json's
	// `minted_composition.name`). It is not a declaration locator and
	// never appears in configuration — MintComposition is the only thing
	// that produces one. LaunchVariant is the locator a declaration error
	// cites.
	Composition string `json:"composition"`
	// AgentRuntime is the public agent-runtime kind, the value an
	// agent_runtime constraint compares against.
	AgentRuntime string `json:"agent_runtime"`
	// ModelLine is the variant's authored model-line label, the value a
	// model_line constraint compares against and the label a caller chains
	// on.
	ModelLine string `json:"model_line"`
	// ModelFamily is the variant's authored model-family label, v3's
	// required third label (notes/42 §4). It is the value a model_family
	// constraint and a distinct_model_family relation compare against.
	ModelFamily string `json:"model_family"`
	// Provider is the variant's optional provider tag. An empty Provider
	// is an *untagged* variant, which no provider fact can ever eliminate
	// — provider state is keyed by name, and a variant that names no
	// provider names nothing a `duo provider disable` could have turned
	// off. It is deliberately not a constraint axis (constraints.go).
	Provider string `json:"provider,omitempty"`
	// LaunchVariant and AgentRuntimeDecl are the declaration locators the
	// tuple was joined from. LaunchVariant is *the* candidate locator:
	// candidate.locator, the survivor pools, and the failure details all
	// cite it.
	LaunchVariant    string `json:"launch_variant"`
	AgentRuntimeDecl string `json:"agent_runtime_declaration"`
	// HostKind and HostInstance are the deduced host: its kind and its
	// instance locator (for Herdr, the API socket path). They come from
	// the materialization bundle, never from configuration — v3's
	// session_hosts block is policy only and carries no instance.
	HostKind     string `json:"host_kind"`
	HostInstance string `json:"host_instance"`
	// HostVersion is the pinned external version the *installed adapter*
	// for HostKind declares — a build constant read off the adapter
	// descriptor, never a probe (I-3, I-4). It is the third component of
	// SupportKey; see that type for the thread-5 position it encodes.
	HostVersion string `json:"host_version"`
	// IntegrationInstanceID is the Duo integration-instance ID the host
	// adapter scopes its evidence by. Under v2 this was the *config*
	// session-host declaration name; under v3 it is the deduced host's own
	// instance ID, falling back to its `<kind>:<instance>` locator when
	// the rung that produced the host carried no ID (an explicit
	// `--host <kind>:<instance>` is the ordinary case). Nothing derives it
	// from a config key any more.
	IntegrationInstanceID string `json:"integration_instance_id"`
	// Executable and Arguments are the resolved command: the agent
	// runtime's declared executable and base arguments, with the variant's
	// append_arguments appended (v3 moves the command shape onto the
	// runtime and leaves the variant only its additions). They are
	// declaration data, not resolved paths: nothing here consults PATH,
	// and no file is stat-ed (§7.1 forbids executable-path state).
	Executable string   `json:"executable"`
	Arguments  []string `json:"arguments,omitempty"`
}

// MintComposition renders the composition name for one variant joined to
// one host kind.
//
// The format is `<variant>@<host_kind>` — the form step 03 fixed across
// every re-authored launch fixture (`codex_gpt56@herdr`). It deliberately
// does *not* carry the instance locator: exactly one host is deduced per
// resolution, so the kind already disambiguates every join inside one
// record, and a socket path in a name would make the name change whenever
// the same workspace rebound to a different socket. The instance travels
// as Tuple.HostInstance and as the minted composition's `host_instance_id`.
func MintComposition(variant, hostKind string) string { return variant + "@" + hostKind }

// SupportKey is the key installed conformance evidence is looked up by:
// (host kind, host version, agent-runtime kind).
//
// This is the **re-key** v3 forces. Under v2, Support was consulted per
// tuple and the tuple's host identity was a *config declaration name*, so
// two `session_hosts` entries pointing at one socket were two evidence keys
// for one real host, and renaming a config entry invalidated a conformance
// lookup that nothing about the installation had changed. Config names are
// gone; what conformance evidence is actually about is which host software
// at which version this build talks to, and which agent runtime it starts.
//
// Thread-5 position (workplan Risk 4, notes/43 record 7): HostVersion is
// the pinned external-version string on the host adapter's own descriptor —
// a build constant (`herdr.PinnedVersion` reached through
// `Descriptor().SupportedExternalVersions`), never a probe and never a
// detected version — and it is matched **exactly**, as a string. A change
// to the version source or to the match semantics (ranges, rules,
// lineage) belongs to the conformance-record contract, not here.
type SupportKey struct {
	HostKind     string `json:"host_kind"`
	HostVersion  string `json:"host_version"`
	AgentRuntime string `json:"agent_runtime"`
}

// String renders the key in a stable, greppable form.
func (k SupportKey) String() string {
	return k.HostKind + "@" + k.HostVersion + "/" + k.AgentRuntime
}

// SupportKey projects the conformance-evidence key out of a tuple. Two
// tuples that differ only in variant name, model line, model family, or
// provider share one key, which is the whole point of the re-key.
func (t Tuple) SupportKey() SupportKey {
	return SupportKey{HostKind: t.HostKind, HostVersion: t.HostVersion, AgentRuntime: t.AgentRuntime}
}

// Limits are the finite maxima §6.7 requires validation to enforce.
// Exceeding one is a declaration error; the resolver never truncates its
// search, because candidate and leaf order are semantic inputs and a
// truncated search would silently change the lexical winner.
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
	// providerFactID is the standing provider fact consulted at step 3.2,
	// carried so the record and the exhaustion row can cite the exact fact
	// that disabled the provider (step 13).
	providerFactID string
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

// locator is the candidate's declaration locator: its launch-variant
// locator. The minted composition is on the tuple, and it is not a locator
// — nothing in configuration is named by it.
func (c *candidate) locator() string { return c.tuple.LaunchVariant }

// leafPlan is one materialized leaf: its declared candidates in array
// order, plus the pools each pipeline stage leaves behind.
type leafPlan struct {
	name     string
	locator  string
	declared []*candidate

	// beforeConstraints is the pool after materialization and the step-3
	// eliminations, and before any launch constraint. It is the pool
	// launch.no_eligible_candidate is judged against.
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

// relationDistinctModelLine was, until duo-config-v3 step 10, the only
// cross-leaf relation this Matter defined (§6.5 rule 5). It compares exact
// declared model-line labels.
//
// relationDistinctModelFamily is step 10's addition, by reference to step
// 02's duo-external-v1 launch_relation_kind add (2026-08-24 handoff 22): it
// compares exact declared model-family labels the same way. Both relations
// live in the same closed kind vocabulary, and materializeRelations refuses
// any third name.
const (
	relationDistinctModelLine   = "distinct_model_line"
	relationDistinctModelFamily = "distinct_model_family"
)

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

// materializePlan validates one requested preset end to end and mints a
// launch tuple for every declared candidate — §6.7 steps 1 and 2, in v3's
// join form.
//
// It is the only place a declaration defect can be found, and it finds all
// of them before any constraint is applied, which is what keeps declaration
// ambiguity from ever being confused with launch.constraints_exhausted
// (narrowing).
//
// Leaf order is lexical by leaf name. That is a deviation from §6.7's "leaf
// declaration order", forced by internal/config: its resolved
// PresetV3.Leaves is a map[string]PresetLeafV3, so YAML declaration order
// is already gone by the time a document reaches this package. Lexical
// order is deterministic, replayable, and stable across re-reads of the
// same document, which is what §7.2's determinism contract actually
// requires of it; recovering true declaration order needs an ordered leaf
// list in internal/config. See docs/launch/decisions.md.
func (r *Resolver) materializePlan(presetName string, lim Limits) (*plan, *Error) {
	declared, ok := r.doc.Presets[presetName]
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
		leaf, err := r.materializeLeaf(presetLocator, name, declared.Leaves[name], lim)
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

// materializeLeaf validates one leaf and mints its candidates.
func (r *Resolver) materializeLeaf(presetLocator, name string, declared config.PresetLeafV3, lim Limits) (*leafPlan, *Error) {
	locator := presetLocator + ".leaves." + name
	if len(declared.Candidates) == 0 {
		return nil, unresolved(locator, "declares no candidates; a leaf needs at least one launch-variant reference")
	}
	if len(declared.Candidates) > lim.MaxCandidatesPerLeaf {
		return nil, unresolved(locator, fmt.Sprintf(
			"declares %d candidates, over the limit of %d", len(declared.Candidates), lim.MaxCandidatesPerLeaf))
	}

	leaf := &leafPlan{name: name, locator: locator}
	seen := map[string]bool{}
	for i, ref := range declared.Candidates {
		if seen[ref.Variant] {
			return nil, unresolved(locator, fmt.Sprintf(
				"references launch variant %q twice; a leaf's references must be distinct", ref.Variant))
		}
		seen[ref.Variant] = true

		tuple, err := r.mintTuple(ref.Variant)
		if err != nil {
			return nil, err
		}
		leaf.declared = append(leaf.declared, &candidate{tuple: tuple, leaf: name, order: i})
	}
	return leaf, nil
}

// mintTuple joins one launch variant to the deduced host and mints the
// composition.
//
// The declaration half follows the v3 chain launch variant -> agent
// runtime; every missing or malformed link on it is declaration ambiguity
// (§6.5 rule 3). The host half is not a chain at all: there is exactly one
// deduced host for the whole resolution, so every candidate in every leaf
// joins the same one. That is the structural change v3 makes — a candidate
// can no longer disagree with another candidate about where it runs.
func (r *Resolver) mintTuple(name string) (Tuple, *Error) {
	variantLocator := "launch_variants." + name
	variant, ok := r.doc.LaunchVariants[name]
	if !ok {
		return Tuple{}, unresolved(variantLocator, "is referenced by the preset but not declared")
	}
	// internal/config already refuses a variant with no model_line or
	// model_family, so reaching this with either empty means a hand-built
	// document.
	if variant.ModelLine == "" || variant.ModelFamily == "" {
		return Tuple{}, unresolved(variantLocator, "declares no model_line or no model_family")
	}

	runtimeLocator := "agent_runtimes." + variant.AgentRuntime
	runtimeDecl, ok := r.doc.AgentRuntimes[variant.AgentRuntime]
	if !ok {
		return Tuple{}, unresolved(runtimeLocator, fmt.Sprintf(
			"is referenced by %s but not declared", variantLocator))
	}
	if runtimeDecl.Kind == "" {
		return Tuple{}, unresolved(runtimeLocator,
			"declares no kind; the public agent-runtime value a constraint compares against is never inferred from the declaration name")
	}
	if runtimeDecl.Executable == "" {
		return Tuple{}, unresolved(runtimeLocator, "declares no executable")
	}

	// v3 moves the command shape onto the runtime; the variant contributes
	// only what it appends. A nil result keeps the tuple's omitempty
	// honest for a runtime with no arguments and a variant that appends
	// none.
	var args []string
	if len(runtimeDecl.Arguments)+len(variant.AppendArguments) > 0 {
		args = make([]string, 0, len(runtimeDecl.Arguments)+len(variant.AppendArguments))
		args = append(args, runtimeDecl.Arguments...)
		args = append(args, variant.AppendArguments...)
	}

	return Tuple{
		Composition:           MintComposition(name, r.host.Kind),
		AgentRuntime:          runtimeDecl.Kind,
		ModelLine:             variant.ModelLine,
		ModelFamily:           variant.ModelFamily,
		Provider:              variant.Provider,
		LaunchVariant:         variantLocator,
		AgentRuntimeDecl:      runtimeLocator,
		HostKind:              r.host.Kind,
		HostInstance:          r.host.Instance,
		HostVersion:           r.hostVersion,
		IntegrationInstanceID: r.integrationInstanceID,
		Executable:            runtimeDecl.Executable,
		Arguments:             args,
	}, nil
}

// integrationInstanceIDFor is the one rule that turns a deduced host into
// the ID host adapters scope evidence by. M1 carries an InstanceID only
// when the rung that won knew one (a correlation fact, or a discoverer that
// reports one); an explicit `--host <kind>:<instance>` knows only the
// locator. Falling back to the `<kind>:<instance>` locator keeps the ID
// stable and addressable for exactly that case, and it is still not a
// config name — the locator is state.
func integrationInstanceIDFor(h materialize.DeducedHost) string {
	if h.InstanceID != "" {
		return h.InstanceID
	}
	return h.Locator()
}

// materializeRelations validates every declared cross-leaf relation and
// resolves its leaf names to plan indexes.
func materializeRelations(p *plan, declared []config.PresetRelationV3) ([]relation, *Error) {
	index := map[string]int{}
	for i, leaf := range p.leaves {
		index[leaf.name] = i
	}

	out := make([]relation, 0, len(declared))
	for _, rel := range declared {
		if rel.Kind != relationDistinctModelLine && rel.Kind != relationDistinctModelFamily {
			return nil, unresolved(p.locator, fmt.Sprintf(
				"declares relation kind %q; %q and %q are the only defined cross-leaf relations",
				rel.Kind, relationDistinctModelLine, relationDistinctModelFamily))
		}
		if len(rel.Leaves) < 2 {
			return nil, unresolved(p.locator, fmt.Sprintf(
				"relation %s names %d leaves; a relation names at least two", rel.Kind, len(rel.Leaves)))
		}
		seen := map[string]bool{}
		indexes := make([]int, 0, len(rel.Leaves))
		for _, name := range rel.Leaves {
			if seen[name] {
				return nil, unresolved(p.locator, fmt.Sprintf(
					"relation %s names leaf %q twice", rel.Kind, name))
			}
			seen[name] = true
			at, ok := index[name]
			if !ok {
				return nil, unresolved(p.locator, fmt.Sprintf(
					"relation %s names leaf %q, which the preset does not declare", rel.Kind, name))
			}
			indexes = append(indexes, at)
		}
		out = append(out, relation{kind: rel.Kind, leaves: append([]string(nil), rel.Leaves...), indexes: indexes})
	}
	return out, nil
}
