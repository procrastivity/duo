package launch

import (
	"errors"
	"fmt"
	"time"

	"github.com/procrastivity/duo/internal/config"
)

// timestampLayout matches the store's and the domain's: fixed width, UTC,
// millisecond, so values sort lexically wherever they are compared.
const timestampLayout = "2006-01-02T15:04:05.000Z"

// Options configure a Resolver. Everything that could make two resolutions
// of the same request differ — installed evidence, entropy, the clock, ID
// minting — arrives here, so a test can pin all four and §7.2's determinism
// contract becomes something a test can actually assert.
type Options struct {
	// Support is the installed-evidence oracle. It is required: see
	// AllSupported for why there is no permissive default.
	Support Support
	// Random is required only to resolve a preset whose selection is
	// random. A resolver without one refuses such a preset rather than
	// reaching for a package-level generator.
	Random RandomSource
	// Limits are the complexity maxima. A zero field takes its default.
	Limits Limits
	// Now stamps the record. Defaults to time.Now.
	Now func() time.Time
	// NewID mints the record's Duo ID. Defaults to a 128-bit random
	// "lrr_..." value.
	NewID func() (string, error)
}

// Resolver resolves one requested preset against one configuration
// document.
//
// It is a pure decision component: constructing one reads nothing and
// resolving with one performs no I/O, no probe, and no clock read beyond
// the injected Now. That is §7.1's accepted rung — configuration plus
// installed evidence — made structural rather than promised.
type Resolver struct {
	doc    config.DocumentV2
	digest string
	opts   Options
}

// NewResolver builds a resolver over one already-validated duo.config/v2
// document.
func NewResolver(doc config.DocumentV2, opts Options) (*Resolver, error) {
	if opts.Support == nil {
		return nil, errors.New(
			"launch: NewResolver needs a Support oracle; pass launch.AllSupported{RecordDigest: ...} to declare that installed evidence supports every declaration")
	}
	defaults := DefaultLimits()
	if opts.Limits.MaxLeaves <= 0 {
		opts.Limits.MaxLeaves = defaults.MaxLeaves
	}
	if opts.Limits.MaxCandidatesPerLeaf <= 0 {
		opts.Limits.MaxCandidatesPerLeaf = defaults.MaxCandidatesPerLeaf
	}
	if opts.Limits.MaxAssignments <= 0 {
		opts.Limits.MaxAssignments = defaults.MaxAssignments
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NewID == nil {
		opts.NewID = mintRecordID
	}

	digest, err := configurationDigest(doc)
	if err != nil {
		return nil, err
	}
	return &Resolver{doc: doc, digest: digest, opts: opts}, nil
}

// Request is one launch-resolution request: the preset to resolve and the
// constraints to resolve it under.
//
// Everything here is an explicit request argument. Nothing about the host,
// the runtime, or the machine is discovered on the way in — §7.1's static
// input rule is a property of this type as much as of the algorithm.
type Request struct {
	// Preset is the requested preset name. Presets supersede direct
	// composition naming as the open session.launch request surface
	// (§6.10); there is no "launch this composition" request shape here.
	Preset string
	// Require and Avoid are the requested constraints, in request order.
	// Both are normalized (deduplicated, provenance preserved) before
	// anything is applied.
	Require []Constraint
	Avoid   []Constraint
	// RequestID and Caller are recorded on the launch-resolution record.
	RequestID string
	Caller    string
}

// Resolve runs the whole pipeline for one request and returns the complete
// resolution, or a typed *Error.
//
// It takes no context because it does no I/O and cannot block: there is
// nothing to cancel. A failure means nothing was chosen, nothing was
// recorded, and — since the record commit is downstream of this call —
// nothing can have been launched.
//
// The pipeline is §6.7 in order:
//
//	1-2. validate and materialize the whole preset;
//	  3. drop candidates installed evidence or a disabled host declaration
//	     does not support;
//	  4. apply every non-relenting require;
//	  5. apply every avoid to form the strictly narrowed pools;
//	  6. enumerate complete assignments in leaf order then array order,
//	     rejecting tuples that violate a cross-leaf relation;
//	  7. select the first (ordered) or drawn (random) complete assignment;
//	  8. if none survived and an avoid contributed to that, re-run the
//	     enumeration over the post-require, pre-avoid pools;
//	  9. if none survives that either, return exhaustion.
//
// Step 10 — durably recording the resolution before any spawn — is
// Launcher's, because it is the only step that needs anything outside this
// package.
func (r *Resolver) Resolve(req Request) (*Resolution, error) {
	res, err := r.resolve(req)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// resolve is Resolve with the concrete error type, so the pipeline can
// return *Error internally without the typed-nil hazard at the boundary.
func (r *Resolver) resolve(req Request) (*Resolution, *Error) {
	constraints, err := normalize(req.Require, req.Avoid)
	if err != nil {
		return nil, err
	}

	p, err := materialize(r.doc, req.Preset, r.opts.Limits)
	if err != nil {
		return nil, err
	}
	if p.selection == SelectionRandom && r.opts.Random == nil {
		return nil, invalidRequest(fmt.Sprintf(
			"Preset %q selects randomly and this resolver has no random source.", req.Preset))
	}

	digests := r.applyInstalledEvidence(p)
	if empty := firstEmptyLeaf(p, func(l *leafPlan) []*candidate { return l.beforeConstraints }); empty != nil {
		return nil, r.noEligibleCandidate(req, p, digests, empty)
	}

	applyRequire(p, constraints.Require)
	applyAvoid(p, constraints.Avoid)

	// §6.7 steps 6-7: the strict pass, over the post-avoid pools.
	assignments, rejections := enumerate(p, func(l *leafPlan) []*candidate { return l.afterAvoid })
	relented := false
	if len(assignments) == 0 && avoidContributed(p) {
		// §6.7 step 8: an avoid, not a require, is what emptied the
		// result. Restore the pre-avoid pools whole and enumerate again.
		relented = true
		relentAvoids(p)
		assignments, rejections = enumerate(p, func(l *leafPlan) []*candidate { return l.afterRequire })
	}
	if len(assignments) == 0 {
		return nil, r.exhausted(req, p, constraints, rejections)
	}

	selected, draw, err := r.selectAssignment(p, assignments)
	if err != nil {
		return nil, err
	}
	for _, c := range selected {
		c.selected = true
	}

	record, err := r.buildRecord(req, p, constraints, digests, selected, rejections, relented, len(assignments), draw)
	if err != nil {
		return nil, err
	}
	return &Resolution{Record: *record}, nil
}

// applyInstalledEvidence is §6.7 step 3: drop every candidate that
// installed evidence or an enabled-host declaration does not support,
// recording the reason on each. It returns the consulted record digests, in
// first-consulted order.
//
// This is the one stage that can raise launch.no_eligible_candidate — an
// installation fact, class unavailable — and it runs strictly before any
// launch constraint so that an installation gap can never be misreported as
// a caller's over-narrowing.
func (r *Resolver) applyInstalledEvidence(p *plan) []string {
	var digests []string
	seen := map[string]bool{}
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			if r.hostDisabled(c.tuple) {
				c.eliminated = ReasonSessionHostDisabled
				continue
			}
			verdict := r.opts.Support.Supported(c.tuple)
			if verdict.RecordDigest != "" && !seen[verdict.RecordDigest] {
				seen[verdict.RecordDigest] = true
				digests = append(digests, verdict.RecordDigest)
			}
			c.supportDigest = verdict.RecordDigest
			if !verdict.OK {
				c.eliminated = verdict.Reason
				if c.eliminated == "" {
					c.eliminated = ReasonNoConformanceEvidence
				}
				continue
			}
			leaf.beforeConstraints = append(leaf.beforeConstraints, c)
		}
	}
	return digests
}

// hostDisabled reports whether the tuple's session-host declaration is
// marked disabled.
//
// This is installed policy, not a live assertion that the host can be
// reached (§7.1). `enabled: false` disables; anything else — including the
// field's absence — leaves the host enabled, because a declaration that
// says nothing about being enabled is not a declaration that it is off.
func (r *Resolver) hostDisabled(t Tuple) bool {
	decl, ok := r.doc.SessionHosts[t.IntegrationInstanceID]
	if !ok {
		return false
	}
	enabled, ok := decl["enabled"].(bool)
	return ok && !enabled
}

// applyRequire is §6.7 step 4: non-relenting exact-equality narrowing.
// Different-axis requirements are conjunctive, so a candidate must match
// every one of them.
func applyRequire(p *plan, require []NormalizedConstraint) {
	for _, leaf := range p.leaves {
		for _, c := range leaf.beforeConstraints {
			if matchesAll(c.tuple, require) {
				leaf.afterRequire = append(leaf.afterRequire, c)
				continue
			}
			c.eliminated = ReasonRequireUnmatched
		}
	}
}

// applyAvoid is §6.7 step 5: soft exact-equality narrowing. A candidate
// matching any avoid leaves the strict pool; whether that elimination
// stands depends on whether a complete assignment survives it.
func applyAvoid(p *plan, avoid []NormalizedConstraint) {
	for _, leaf := range p.leaves {
		for _, c := range leaf.afterRequire {
			if matchesAny(c.tuple, avoid) {
				c.eliminated = ReasonAvoidMatched
				continue
			}
			leaf.afterAvoid = append(leaf.afterAvoid, c)
		}
	}
}

// avoidContributed reports whether any avoid actually removed a candidate.
// When none did, the strict and relented passes are the same enumeration
// over the same pools, and re-running would only relabel a failure that
// require or a relation caused.
func avoidContributed(p *plan) bool {
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			if c.eliminated == ReasonAvoidMatched {
				return true
			}
		}
	}
	return false
}

// relentAvoids restores every avoid-eliminated candidate. §6.6 restores
// "the pre-avoid pool", not a minimal subset: relenting is whole-pool, so
// the retry cannot depend on which avoid a search happened to drop first.
func relentAvoids(p *plan) {
	for _, leaf := range p.leaves {
		for _, c := range leaf.declared {
			if c.eliminated == ReasonAvoidMatched {
				c.eliminated = ""
				c.restored = true
			}
		}
	}
}

// matchesAll reports whether t satisfies every predicate (conjunctive
// require).
func matchesAll(t Tuple, cs []NormalizedConstraint) bool {
	for _, c := range cs {
		if !c.predicate().matches(t) {
			return false
		}
	}
	return true
}

// matchesAny reports whether t matches at least one predicate.
func matchesAny(t Tuple, cs []NormalizedConstraint) bool {
	for _, c := range cs {
		if c.predicate().matches(t) {
			return true
		}
	}
	return false
}

// matchedAvoids returns the avoid predicates t matches, in normalized
// order.
func matchedAvoids(t Tuple, avoid []NormalizedConstraint) []Predicate {
	out := []Predicate{}
	for _, c := range avoid {
		if c.predicate().matches(t) {
			out = append(out, c.predicate())
		}
	}
	return out
}

// firstEmptyLeaf returns the first leaf whose pool is empty, or nil.
// "First" is in leaf order, so which leaf a failure names is deterministic.
func firstEmptyLeaf(p *plan, pool func(*leafPlan) []*candidate) *leafPlan {
	for _, leaf := range p.leaves {
		if len(pool(leaf)) == 0 {
			return leaf
		}
	}
	return nil
}

// enumerate produces every complete assignment over the given pools, in
// leaf order then candidate array order, and the relation rejections it
// found on the way.
//
// §6.7 step 6 and §7.2 together make this ordering semantic: the enumeration
// is lexicographic over the leaves' preference ranks, so the first surviving
// assignment is the one an ordered selection must choose, and the eligible
// set a random selection draws from is the same ordered set on every replay.
// Multi-leaf resolution is whole-plan here: a leaf never sees another leaf's
// pool, but a relation can reject the tuple the two of them form, and the
// only thing that can be selected is a complete assignment.
func enumerate(p *plan, pool func(*leafPlan) []*candidate) ([][]*candidate, []RelationRejection) {
	var (
		out        [][]*candidate
		rejections []RelationRejection
		current    = make([]*candidate, len(p.leaves))
	)
	var walk func(depth int)
	walk = func(depth int) {
		if depth == len(p.leaves) {
			if rejection, ok := violates(p, current); ok {
				rejections = append(rejections, rejection)
				return
			}
			out = append(out, append([]*candidate(nil), current...))
			return
		}
		for _, c := range pool(p.leaves[depth]) {
			current[depth] = c
			walk(depth + 1)
		}
	}
	walk(0)
	return out, rejections
}

// violates checks one complete assignment against every declared relation.
// distinct_model_line compares exact declared model-line labels, pairwise,
// across the leaves it names (§6.5 rule 5). distinct_model_family
// (duo-config-v3 step 10, by reference to step 02's duo-external-v1
// launch_relation_kind add) compares exact declared model-family labels the
// same way.
func violates(p *plan, assignment []*candidate) (RelationRejection, bool) {
	for _, rel := range p.relations {
		field := "model line"
		value := func(t Tuple) string { return t.ModelLine }
		if rel.kind == relationDistinctModelFamily {
			field = "model family"
			value = func(t Tuple) string { return t.ModelFamily }
		}
		seen := map[string]bool{}
		for _, at := range rel.indexes {
			v := value(assignment[at].tuple)
			if seen[v] {
				locators := make([]string, 0, len(assignment))
				for _, c := range assignment {
					locators = append(locators, c.locator())
				}
				return RelationRejection{
					Kind:       rel.kind,
					Leaves:     rel.leaves,
					Assignment: locators,
					Reason:     fmt.Sprintf("%s %q appears on more than one of the related leaves", field, v),
				}, true
			}
			seen[v] = true
		}
	}
	return RelationRejection{}, false
}

// selectAssignment is §6.7 step 7: the first complete assignment in ordered
// mode, or one drawn from the whole eligible set in explicit random mode.
//
// Random mode chooses among complete valid assignments, never independently
// per leaf, and it changes nothing about filtering, relenting, relation
// checks, or the candidate set (§6.7). All it does is pick a different
// member of the same set, and record enough evidence to explain which.
func (r *Resolver) selectAssignment(p *plan, assignments [][]*candidate) ([]*candidate, *Draw, *Error) {
	if p.selection != SelectionRandom {
		return assignments[0], nil, nil
	}
	draw, err := r.opts.Random.Draw(len(assignments))
	if err != nil {
		// §6.8: entropy failing before the selection is recorded is an
		// internal failure with a diagnostics reference, not a narrowing
		// error the caller could correct.
		return nil, nil, internalFailure("random draw failed during launch resolution")
	}
	if draw.Index < 0 || draw.Index >= len(assignments) {
		return nil, nil, internalFailure(fmt.Sprintf(
			"random source drew index %d over an eligible set of %d", draw.Index, len(assignments)))
	}
	draw.SetSize = len(assignments)
	return assignments[draw.Index], &draw, nil
}

// buildRecord assembles the launch-resolution record from the finished
// pipeline state.
func (r *Resolver) buildRecord(
	req Request,
	p *plan,
	constraints Constraints,
	digests []string,
	selected []*candidate,
	rejections []RelationRejection,
	relented bool,
	eligible int,
	draw *Draw,
) (*Record, *Error) {
	id, err := r.opts.NewID()
	if err != nil {
		return nil, internalFailure("minting the launch-resolution record id failed")
	}

	rec := &Record{
		ID:                     id,
		RequestID:              req.RequestID,
		Caller:                 req.Caller,
		ResolvedAt:             r.opts.Now().UTC().Format(timestampLayout),
		RequestedPreset:        req.Preset,
		PresetLocator:          p.locator,
		ConfigurationDigest:    r.digest,
		ConsultedRecordDigests: digests,
		Selection:              p.selection,
		Constraints:            constraints,
		RelationRejections:     rejections,
		AvoidRelented:          relented,
		EligibleAssignments:    eligible,
		Draw:                   draw,
	}
	if rec.ConsultedRecordDigests == nil {
		rec.ConsultedRecordDigests = []string{}
	}
	if rec.RelationRejections == nil {
		rec.RelationRejections = []RelationRejection{}
	}
	rec.RestoredCandidates = []string{}

	for _, leaf := range p.leaves {
		rl := RecordLeaf{
			Name:         leaf.name,
			Locator:      leaf.locator,
			DeclaredKind: leaf.declaredKind(),
			Survivors: Survivors{
				BeforeConstraints:     locators(leaf.beforeConstraints),
				AfterRequire:          locators(leaf.afterRequire),
				AfterAvoid:            locators(leaf.afterAvoid),
				AfterAvoidRestoration: []string{},
			},
		}
		if relented {
			rl.Survivors.AfterAvoidRestoration = locators(leaf.afterRequire)
		}
		for _, c := range leaf.declared {
			if c.restored {
				rec.RestoredCandidates = append(rec.RestoredCandidates, c.locator())
			}
			rl.Candidates = append(rl.Candidates, RecordCandidate{
				Tuple:               c.tuple,
				Order:               c.order,
				Outcome:             c.outcome(),
				EliminationReason:   c.eliminated,
				Restored:            c.restored,
				SupportRecordDigest: c.supportDigest,
			})
		}
		rec.Leaves = append(rec.Leaves, rl)
	}

	for i, c := range selected {
		rec.Assignment = append(rec.Assignment, Assignment{
			Leaf:           p.leaves[i].name,
			Tuple:          c.tuple,
			RelentedAvoids: matchedAvoids(c.tuple, constraints.Avoid),
		})
	}
	return rec, nil
}

// locators projects a pool to its declaration locators.
func locators(pool []*candidate) []string {
	out := make([]string, 0, len(pool))
	for _, c := range pool {
		out = append(out, c.locator())
	}
	return out
}
