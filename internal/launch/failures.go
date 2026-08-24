package launch

import (
	"fmt"
	"strings"

	"github.com/procrastivity/duo/internal/launch/materialize"
)

// --- the causal split -----------------------------------------------------

// The two `host_source` values this package branches on. Everything else it
// only ever passes through.
//
// They are literals rather than domain.HostSourceExplicitFlag and
// domain.HostSourceWorkspaceCorrelation because internal/launch does not
// import the kernel (duo-vnext-go-architecture.md §3: the resolver is a
// pure decision component, and internal/launch/materialize is the package
// that holds the domain edge). They are pinned end to end by the
// explicit-host and correlation tests, which materialize a real deduction
// and assert the emitted `host_source` against the domain vocabulary.
const (
	hostSourceExplicitFlag         = "explicit-flag"
	hostSourceWorkspaceCorrelation = "workspace-correlation"
)

// causalSplit decides which of the two exhaustion rows a resolution that
// produced no complete assignment reports under. It is duo-config-v3 step
// 13's whole subject, and the rule is ratified in
// duo-vnext-access-errors-audit.md §4.1 ("duo.config/v3 launch codes",
// 2026-08-24 handoff 22):
//
//	A static elimination with reason provider_disabled is an
//	installed-eligibility loss, so on its own it reports
//	launch.no_eligible_candidate. A case where a require, an avoid, or a
//	relation also contributed reports launch.constraints_exhausted and
//	carries both tallies.
//
// So the row is decided by *cause*, never by which stage happened to empty
// the last pool: when no caller narrowing contributed, the row is
// launch.no_eligible_candidate (class unavailable — an installation fact
// the same request cannot repair); when any require, avoid, or cross-leaf
// relation contributed, the row is launch.constraints_exhausted (class
// invalid — the caller can correct it).
//
// The generalization from the ratified text is deliberate and conservative:
// the audit names provider_disabled because it is the reason step 10 added,
// but the same argument holds for session_host_disabled and
// no_conformance_evidence, which are equally installed-policy and
// installed-evidence facts. All three are "static" here.
//
// The split runs at exactly two places, and they cannot disagree:
//
//   - Step 3 emptying a leaf is decided before any constraint has been
//     applied, so no caller narrowing can have contributed and the row is
//     always launch.no_eligible_candidate. Resolve returns there directly.
//   - This function decides the post-enumeration failure, where a static
//     reason and a caller reason may both be present. That is the mixed
//     case, and constraints_exhausted wins it — the caller has something to
//     change, and the tallies carry the static reasons too, so nothing is
//     hidden by reporting the row the caller can act on.
//
// Today the second call site always finds a contribution: a resolution
// whose leaves all survived step 3 and whose request narrowed nothing
// enumerates at least one complete assignment. The unavailable branch is
// therefore unreachable through Resolve as it stands, and it is written
// anyway because the split is a rule about causes, not a description of the
// current control flow: a later stage that can empty a pool without a
// caller constraint must report the honest row rather than inherit
// constraints_exhausted from where it happened to fail.
func causalSplit(p *plan, rejections []RelationRejection) string {
	if callerNarrowingContributed(p, rejections) {
		return CodeConstraintsExhausted
	}
	return CodeNoEligibleCandidate
}

// callerNarrowingContributed reports whether a require, an avoid, or a
// cross-leaf relation took part in the failure.
//
// A restored candidate counts. The relent re-run undoes an avoid
// elimination, so by the time the failure is built the avoid has left no
// trace on `eliminated` — but it did narrow the pool that was enumerated
// first, and it is the reason the relent happened at all. Ignoring it would
// let a resolution the caller's avoid demonstrably touched report as an
// installation fact.
func callerNarrowingContributed(p *plan, rejections []RelationRejection) bool {
	if len(rejections) > 0 {
		return true
	}
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			if c.restored {
				return true
			}
			switch c.eliminated {
			case ReasonRequireUnmatched, ReasonAvoidMatched:
				return true
			}
		}
	}
	return false
}

// staticReasons are the elimination reasons that are *not* caller
// narrowing: installed policy, standing state, and installed conformance
// evidence. They are what a mixed case carries "both tallies" of, and what
// leaves the unavailable row when they are the only thing present.
var staticReasons = []string{
	ReasonSessionHostDisabled,
	ReasonProviderDisabled,
	ReasonNoConformanceEvidence,
}

// reasonOrder is the order tallies are emitted in: the static reasons
// first, in step 3's own check order, then the two narrowing reasons in
// pipeline order. Emission order is not semantic, but it is deterministic,
// which is what makes a failure payload comparable across two runs.
var reasonOrder = []string{
	ReasonSessionHostDisabled,
	ReasonProviderDisabled,
	ReasonNoConformanceEvidence,
	ReasonRequireUnmatched,
	ReasonAvoidMatched,
}

// --- failure builders -----------------------------------------------------

// exhaustionFailure builds the failure for a resolution that enumerated no
// complete assignment, choosing its row with causalSplit.
//
// relented says whether §6.7 step 8's re-run happened, which is what
// decides *which* pool an empty leaf is judged against: the post-require
// pool after a relent, the post-avoid pool otherwise. The leaf it finds
// there is the one the message names, and it can legitimately be nil — a
// relation can reject every complete assignment while every leaf's pool is
// still populated.
func (r *Resolver) exhaustionFailure(
	req Request,
	p *plan,
	constraints Constraints,
	digests []string,
	rejections []RelationRejection,
	relented bool,
) *Error {
	pool := func(l *leafPlan) []*candidate { return l.afterAvoid }
	if relented {
		pool = func(l *leafPlan) []*candidate { return l.afterRequire }
	}
	empty := firstEmptyLeaf(p, pool)

	if causalSplit(p, rejections) == CodeNoEligibleCandidate {
		// No caller narrowing took part: this is an installed-eligibility
		// loss wearing an exhaustion's clothes, and it reports as one.
		if empty == nil {
			empty = p.leaves[0]
		}
		return r.noEligibleCandidate(req, p, digests, empty)
	}

	message, action := r.exhaustionMessage(p, constraints, rejections, empty)
	return newError(CodeConstraintsExhausted, message,
		Retry{Safe: true, Action: action},
		r.failureDetails(req, p, constraints, digests, ""))
}

// noEligibleCandidate builds §6.8's launch.no_eligible_candidate failure:
// installed policy, standing state, or installed conformance evidence — not
// the caller's constraints — left a leaf with nothing to assign.
//
// It is class unavailable and its safe detail is candidate availability
// outcomes, the per-reason tallies, the deduced host, the evidence-bundle
// references, and the pointer set — never the adapter internals or the
// paths behind them, which stay with diagnostics.read.
func (r *Resolver) noEligibleCandidate(req Request, p *plan, digests []string, empty *leafPlan) *Error {
	message, action := r.eligibilityMessage(empty)
	return newError(CodeNoEligibleCandidate, message,
		Retry{Safe: true, Action: action},
		r.failureDetails(req, p, Constraints{}, digests, empty.name))
}

// eligibilityMessage names what actually did the eliminating on the empty
// leaf, and pairs it with the retry action that is the way out of exactly
// that.
//
// It reports a categorical cause only when the whole leaf shares it. A leaf
// whose candidates died of different things has no single sentence that is
// true of it, so it falls back to the general one and lets the tallies and
// the candidate rows carry the detail.
func (r *Resolver) eligibilityMessage(empty *leafPlan) (string, string) {
	switch {
	case allEliminatedFor(empty, ReasonProviderDisabled):
		return fmt.Sprintf("All candidates for leaf %s carry the disabled %s provider.",
				empty.name, joinNames(providerNames(empty))),
			"enable_provider_or_change_host"
	case allEliminatedFor(empty, ReasonSessionHostDisabled):
		if string(r.host.Source) == hostSourceExplicitFlag {
			return fmt.Sprintf("--host %s names a disabled session-host kind; every join was eliminated.", r.host.Kind),
				"change_host_or_enable_kind"
		}
		return fmt.Sprintf("Session-host kind %q is disabled; every join was eliminated.", r.host.Kind),
			"change_host_or_enable_kind"
	default:
		return fmt.Sprintf("No candidate declared for leaf %q is supported by installed evidence.", empty.name),
			"install_or_enable_a_supported_candidate"
	}
}

// exhaustionMessage names what did the eliminating, and appends the static
// causes when the case is mixed.
//
// A require is named first because it is the non-relenting verb: when one is
// present it is always part of the reason, and a caller reading the message
// needs the predicate it must change. The leaf is named only in a mixed
// case, where the sentence has two causes to keep apart; in a single-cause
// exhaustion the candidate rows already say which leaf, and naming it would
// be noise.
func (r *Resolver) exhaustionMessage(
	p *plan,
	constraints Constraints,
	rejections []RelationRejection,
	empty *leafPlan,
) (string, string) {
	clauses := staticClauses(p)
	mixed := len(clauses) > 0

	var base string
	switch {
	case len(constraints.Require) > 0:
		base = fmt.Sprintf("Require %s", joinPredicates(constraints.Require))
		if mixed && empty != nil {
			base += fmt.Sprintf(" on leaf %s", empty.name)
		}
		base += " left no complete assignment"
	case len(rejections) > 0:
		base = fmt.Sprintf("Relation %s left no complete assignment", rejections[0].Kind)
	default:
		base = "No complete assignment survived the requested constraints"
	}
	if mixed {
		base += "; " + strings.Join(clauses, "; ")
	}

	// The retry action stays "change_constraints_or_preset" even in a mixed
	// case, which
	// contracts/fixtures/duo-external-v1/session-launch-mixed-exhausted.json
	// fixes. It is the right advice: the row is constraints_exhausted
	// precisely because the caller's narrowing contributed, so changing it
	// is a way out, and the installed side the message and the tallies
	// name has its own pointer in the payload rather than a second verb in
	// the retry advice.
	return base + ".", "change_constraints_or_preset"
}

// staticClauses renders the sentence fragments for the static eliminations
// that stand alongside the caller's narrowing, in reason order. An empty
// result means the exhaustion has exactly one kind of cause.
func staticClauses(p *plan) []string {
	var out []string
	for _, reason := range staticReasons {
		count := 0
		for _, leaf := range p.leaves {
			for _, c := range leaf.declared {
				if c.eliminated == reason {
					count++
				}
			}
		}
		if count == 0 {
			continue
		}
		switch reason {
		case ReasonProviderDisabled:
			names := planProviderNames(p)
			verb := "provider is"
			if len(names) > 1 {
				verb = "providers are"
			}
			out = append(out, fmt.Sprintf("the %s %s also disabled", joinNames(names), verb))
		case ReasonSessionHostDisabled:
			out = append(out, fmt.Sprintf("the %s session-host kind is also disabled", p.hostKind()))
		case ReasonNoConformanceEvidence:
			noun, verb := "candidate", "lacks"
			if count > 1 {
				noun, verb = "candidates", "lack"
			}
			out = append(out, fmt.Sprintf("%d other %s also %s installed conformance evidence", count, noun, verb))
		}
	}
	return out
}

// allEliminatedFor reports whether every declared candidate on the leaf was
// eliminated for exactly this reason. It is what lets a message make a
// categorical claim about a leaf without ever overstating one.
func allEliminatedFor(leaf *leafPlan, reason string) bool {
	if len(leaf.declared) == 0 {
		return false
	}
	for _, c := range leaf.declared {
		if c.eliminated != reason {
			return false
		}
	}
	return true
}

// providerNames lists the distinct providers that eliminated candidates on
// one leaf, in declaration order.
func providerNames(leaf *leafPlan) []string { return distinctProviders(leaf.declared) }

// planProviderNames lists the distinct providers that eliminated candidates
// anywhere in the plan, in leaf then declaration order.
func planProviderNames(p *plan) []string {
	var all []*candidate
	for _, leaf := range p.leaves {
		all = append(all, leaf.declared...)
	}
	return distinctProviders(all)
}

func distinctProviders(cs []*candidate) []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range cs {
		if c.eliminated != ReasonProviderDisabled || c.tuple.Provider == "" || seen[c.tuple.Provider] {
			continue
		}
		seen[c.tuple.Provider] = true
		out = append(out, c.tuple.Provider)
	}
	return out
}

// joinNames renders a list of names as prose: "a", "a and b", "a, b and c".
func joinNames(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	case 2:
		return names[0] + " and " + names[1]
	default:
		return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
	}
}

// --- the failure payload --------------------------------------------------

// failureDetails is the safe detail duo-external-v1's
// `launch_failure_details` fixes, carried by both
// launch.constraints_exhausted and launch.no_eligible_candidate.
//
// One struct serves both rows on purpose. The 2026-08-24 amendment grows
// "the details" of the two codes together — per-reason tallies, the deduced
// host with its host_source, the evidence-bundle references, and the
// pointer set — and the causal split means one resolution state can report
// under either row. Two payload shapes would make a reader's parse depend
// on the very decision the payload exists to explain.
//
// `survivors` is the handoff-18 payload carried forward unchanged. Step
// 03's re-authored fixtures omit it, but the audit's handoff-18 clause
// requiring the four survivor pools was not amended, and
// `launch_failure_details` is `additionalProperties: true`, so keeping it
// is a compatible carry-forward rather than a contradiction.
//
// Provenance on the constraints stays deliberately absent. §6.8's prose
// asks for "constraints with provenance", but
// `launch_constraint_predicate` is `additionalProperties: false` over
// axis/value. The fixture wins, and the provenance lives where §6.9 puts
// everything else that explains a resolution: the launch-resolution record.
type failureDetails struct {
	RequestedPreset string `json:"requested_preset"`
	// Leaf is the leaf that had nothing to assign, on the rows that have
	// one. An exhaustion a cross-leaf relation caused has none.
	Leaf                        string                  `json:"leaf,omitempty"`
	Constraints                 wireConstraints         `json:"constraints"`
	Candidates                  []wireCandidate         `json:"candidates"`
	EliminationTallies          []EliminationTally      `json:"elimination_tallies"`
	Relations                   []WireRelation          `json:"relations,omitempty"`
	Host                        WireHost                `json:"host"`
	EvidenceBundle              *materialize.WireBundle `json:"evidence_bundle,omitempty"`
	Pointers                    *materialize.PointerSet `json:"pointers,omitempty"`
	Survivors                   Survivors               `json:"survivors"`
	ConsultedRecordDigests      []string                `json:"consulted_record_digests"`
	CompleteAssignmentsSurvived int                     `json:"complete_assignments_survived"`
}

// failureDetails assembles the payload both rows carry.
//
// complete_assignments_survived is always zero here: every failure this
// builder serves is one where the enumeration produced nothing. Recording
// the zero explicitly is §6.8's required detail, and it distinguishes "the
// enumeration ran and found none" from "the enumeration never ran".
func (r *Resolver) failureDetails(
	req Request,
	p *plan,
	constraints Constraints,
	digests []string,
	leaf string,
) failureDetails {
	if digests == nil {
		digests = []string{}
	}
	return failureDetails{
		RequestedPreset:    req.Preset,
		Leaf:               leaf,
		Constraints:        constraints.wire(),
		Candidates:         wireCandidates(p),
		EliminationTallies: eliminationTallies(p),
		Relations:          wireRelations(p),
		Host:               r.wireHost(),
		EvidenceBundle:     r.failureEvidence(),
		Pointers:           r.pointers(p),
		Survivors: Survivors{
			BeforeConstraints:     poolLocators(p, func(l *leafPlan) []*candidate { return l.beforeConstraints }),
			AfterRequire:          poolLocators(p, func(l *leafPlan) []*candidate { return l.afterRequire }),
			AfterAvoid:            poolLocators(p, func(l *leafPlan) []*candidate { return l.afterAvoid }),
			AfterAvoidRestoration: restorationSurvivors(p),
		},
		ConsultedRecordDigests:      digests,
		CompleteAssignmentsSurvived: 0,
	}
}

// failureEvidence renders the evidence-bundle *references* a failure cites:
// the workspace↔host correlation fact and the standing provider facts, by
// ID.
//
// Ambient captures are deliberately not here, even though
// `launch_evidence_bundle` has room for them. A failure's bundle answers
// "which durable facts did this rest on, so a later reader can replay it";
// an ambient capture is not a durable fact and has no ID to replay. Where a
// capture explains something — it was read, and then a higher rung beat it
// — it appears on the deduced host's outranked evidence, which is where
// step 03's fixtures put it. The full bundle, captures included, is on the
// record.
func (r *Resolver) failureEvidence() *materialize.WireBundle {
	out := &materialize.WireBundle{}
	if id, ok := r.bundle.CorrelationFactID(); ok {
		out.CorrelationFactID = string(id)
	}
	for _, id := range r.bundle.ProviderFactIDs() {
		out.ProviderFactIDs = append(out.ProviderFactIDs, string(id))
	}
	if out.CorrelationFactID == "" && out.ProviderFactIDs == nil {
		// An empty object would claim evidence that does not exist.
		return nil
	}
	return out
}

// pointers names the ways out of this failure, and only the ones that
// actually apply. The set is closed (`launch_pointer_set`), and an
// inapplicable pointer is worse than a missing one: it sends an operator to
// a verb that cannot help.
//
//   - `duo provider enable` appears when a standing-disabled provider
//     eliminated something. Nothing else makes it relevant.
//   - `duo workspace host rebind` appears when the deduced host came from
//     the workspace correlation, which is the only case rebinding changes.
//     It is workplan Risk 2's mitigation: a stale correlation that
//     outranked the pane the operator is standing in is visible here, next
//     to the verb that repairs it.
//   - `--host` appears whenever the host itself is plausibly implicated: a
//     disabled kind eliminated candidates, the host came from a
//     correlation, or some evidence was captured and outranked. A failure
//     with nothing to do with the host does not offer to change it.
func (r *Resolver) pointers(p *plan) *materialize.PointerSet {
	out := &materialize.PointerSet{}

	if names := planProviderNames(p); len(names) > 0 {
		// One verb call per provider; the first is the one to run, and the
		// tallies name the rest.
		out.ProviderEnable = "duo provider enable " + names[0]
	}
	correlated := string(r.host.Source) == hostSourceWorkspaceCorrelation
	if correlated {
		out.WorkspaceHostRebind = "duo workspace host rebind"
	}
	hostDisabled := false
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			if c.eliminated == ReasonSessionHostDisabled {
				hostDisabled = true
			}
		}
	}
	if hostDisabled || correlated || len(r.mat.OutrankedEvidence()) > 0 {
		out.OverrideFlag = "--host"
	}

	if *out == (materialize.PointerSet{}) {
		return nil
	}
	return out
}

// wireHost renders the deduced host duo-external-v1's `launch_deduced_host`
// asks for: the instance, the rung that produced it, the workspace it was
// deduced for, and every piece of evidence a higher rung beat.
func (r *Resolver) wireHost() WireHost {
	out := WireHost{
		Kind:              r.host.Kind,
		InstanceID:        r.host.InstanceID,
		InstanceLabel:     r.host.Instance,
		HostSource:        string(r.host.Source),
		WorkspaceID:       string(r.mat.WorkspaceID()),
		OutrankedEvidence: []WireOutranked{},
	}
	for _, e := range r.mat.OutrankedEvidence() {
		row := WireOutranked{
			Source:        string(e.Source),
			Kind:          e.Kind,
			InstanceLabel: e.Instance,
			FactID:        string(e.FactID),
			Detail:        e.Detail,
		}
		for _, c := range e.Captures {
			row.Captures = append(row.Captures, materialize.WireCapture(c))
		}
		out.OutrankedEvidence = append(out.OutrankedEvidence, row)
	}
	return out
}

// eliminationTallies counts eliminations per leaf and per reason.
//
// It is a *tally*, not one row per candidate: one row per (leaf, reason)
// with the count, which is what makes "three of the four candidates on this
// leaf died of a disabled provider" readable without walking the candidate
// array. The candidate rows are still there for anyone who needs which
// ones.
//
// Static reasons are included whenever they stand, which is what the
// ratified mixed case means by "carries both tallies": a
// launch.constraints_exhausted row still tallies the provider_disabled
// eliminations that happened before the require ran.
//
// A candidate an avoid eliminated and the relent then restored is not
// tallied. The relent undid that elimination, so tallying it would report a
// narrowing that no longer stands; that the relent happened is on the
// record, as AvoidRelented and RestoredCandidates.
func eliminationTallies(p *plan) []EliminationTally {
	out := []EliminationTally{}
	for _, leaf := range p.leaves {
		counts := map[string]int{}
		for _, c := range leaf.declared {
			if c.eliminated != "" {
				counts[c.eliminated]++
			}
		}
		for _, reason := range reasonOrder {
			if counts[reason] == 0 {
				continue
			}
			out = append(out, EliminationTally{
				Leaf:   leaf.name,
				Reason: reason,
				Count:  counts[reason],
				Detail: tallyDetail(leaf, reason),
			})
		}
	}
	return out
}

// tallyDetail is the one short phrase that says *which* installed fact a
// static tally is about. The narrowing reasons get none: the constraints
// object already carries the predicates, and repeating them here would be
// two spellings of one fact.
func tallyDetail(leaf *leafPlan, reason string) string {
	switch reason {
	case ReasonSessionHostDisabled:
		return leaf.declared[0].tuple.HostKind + " disabled"
	case ReasonProviderDisabled:
		names := providerNames(leaf)
		if len(names) == 0 {
			return ""
		}
		noun := "provider disabled"
		if len(names) > 1 {
			noun = "providers disabled"
		}
		return joinNames(names) + " " + noun
	default:
		return ""
	}
}

// wireCandidates projects every declared candidate, in leaf order then
// array order, with the stable reason for any that were eliminated. §6.8
// requires "every safe candidate" on an exhaustion, so this lists the whole
// declaration and not just the survivors.
func wireCandidates(p *plan) []wireCandidate {
	out := []wireCandidate{}
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			out = append(out, wireCandidate{
				Leaf:               leaf.name,
				DeclarationLocator: c.locator(),
				Variant:            c.tuple.Variant,
				Composition:        c.tuple.MintedComposition(),
				AgentRuntime:       c.tuple.AgentRuntime,
				ModelLine:          c.tuple.ModelLine,
				ModelFamily:        c.tuple.ModelFamily,
				EliminationReason:  c.eliminated,
			})
		}
	}
	return out
}

// wireRelations projects the preset's declared cross-leaf relations, or nil
// when it declares none.
func wireRelations(p *plan) []WireRelation {
	if len(p.relations) == 0 {
		return nil
	}
	out := make([]WireRelation, 0, len(p.relations))
	for _, rel := range p.relations {
		out = append(out, WireRelation{Kind: rel.kind, Leaves: append([]string(nil), rel.leaves...)})
	}
	return out
}

// hostKind returns the host kind every candidate in the plan is joined to.
// There is exactly one deduced host per resolution, so any candidate
// answers for all of them; a plan with no candidates answers with "".
func (p *plan) hostKind() string {
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			return c.tuple.HostKind
		}
	}
	return ""
}

// poolLocators flattens one pipeline stage's pools across every leaf, in
// leaf order then candidate order, deduplicating locators that more than
// one leaf declares.
func poolLocators(p *plan, pool func(*leafPlan) []*candidate) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, leaf := range p.leaves {
		for _, c := range pool(leaf) {
			if seen[c.locator()] {
				continue
			}
			seen[c.locator()] = true
			out = append(out, c.locator())
		}
	}
	return out
}

// restorationSurvivors is the post-relent pool, which is the post-require
// pool when a relent happened and empty when none did. An empty list is the
// honest answer for a resolution no avoid ever narrowed: nothing was
// restored because nothing had been taken away.
func restorationSurvivors(p *plan) []string {
	restored := false
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			if c.restored {
				restored = true
			}
		}
	}
	if !restored {
		return []string{}
	}
	return poolLocators(p, func(l *leafPlan) []*candidate { return l.afterRequire })
}

// --- wire types -----------------------------------------------------------

// EliminationTally is duo-external-v1's `launch_elimination_tally`: one
// leaf's count for one stable reason.
type EliminationTally struct {
	Leaf   string `json:"leaf,omitempty"`
	Reason string `json:"reason"`
	Count  int    `json:"count"`
	Detail string `json:"detail,omitempty"`
}

// wireCandidate is one candidate's safe row: which leaf declared it, its
// declaration locator and variant name, the composition the join minted,
// its three public axis values, and — when it did not survive — the stable
// reason it was eliminated.
type wireCandidate struct {
	Leaf               string            `json:"leaf"`
	DeclarationLocator string            `json:"declaration_locator"`
	Variant            string            `json:"variant"`
	Composition        MintedComposition `json:"composition"`
	AgentRuntime       string            `json:"agent_runtime"`
	ModelLine          string            `json:"model_line"`
	ModelFamily        string            `json:"model_family"`
	EliminationReason  string            `json:"elimination_reason,omitempty"`
}

// WireRelation is one declared cross-leaf relation in its duo-external-v1
// spelling (`launch_relation`).
type WireRelation struct {
	Kind   string   `json:"kind"`
	Leaves []string `json:"leaves"`
}

// WireHost is the deduced host in its duo-external-v1 spelling
// (`launch_deduced_host`). It is the same object on a failure, on the
// ordinary result, and on the record: one host was deduced for the launch,
// and every surface that mentions it mentions the same one.
//
// OutrankedEvidence is always present, the empty array included. "Nothing
// was outranked" is a positive claim about a deduction, and an absent key
// would leave it unsaid.
type WireHost struct {
	Kind              string          `json:"kind"`
	InstanceID        string          `json:"instance_id,omitempty"`
	InstanceLabel     string          `json:"instance_label,omitempty"`
	HostSource        string          `json:"host_source"`
	WorkspaceID       string          `json:"workspace_id,omitempty"`
	OutrankedEvidence []WireOutranked `json:"outranked_evidence"`
}

// WireOutranked is one captured-and-beaten piece of host evidence
// (`launch_outranked_evidence`).
type WireOutranked struct {
	Source        string                    `json:"source"`
	Kind          string                    `json:"kind,omitempty"`
	InstanceLabel string                    `json:"instance_label,omitempty"`
	FactID        string                    `json:"fact_id,omitempty"`
	Captures      []materialize.WireCapture `json:"captures,omitempty"`
	Detail        string                    `json:"detail,omitempty"`
}

// Survivors is §6.4 graft 3's explicit pool report: the declaration
// locators that survived each stage of the pipeline.
//
// In an error's safe details the lists are flat across leaves — leaf order,
// then candidate order, with a locator two leaves share listed once —
// because the fixture fixes them as arrays of locators; which leaf declared
// a locator is recoverable from the candidate rows, which name the leaf. On
// a record leaf they are that one leaf's pools.
type Survivors struct {
	BeforeConstraints     []string `json:"before_constraints"`
	AfterRequire          []string `json:"after_require"`
	AfterAvoid            []string `json:"after_avoid"`
	AfterAvoidRestoration []string `json:"after_avoid_restoration"`
}
