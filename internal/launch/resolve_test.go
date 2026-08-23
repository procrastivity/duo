package launch_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/launch"
)

// assignment renders a resolution as "leaf=agent_runtime/model_line" pairs,
// which is the whole of what an assignment means for these assertions.
func assignment(res *launch.Resolution) []string {
	out := []string{}
	for _, a := range res.Assignments() {
		out = append(out, a.Leaf+"="+a.Tuple.AgentRuntime+"/"+a.Tuple.ModelLine)
	}
	return out
}

func wantAssignment(t *testing.T, res *launch.Resolution, want ...string) {
	t.Helper()
	if got := assignment(res); !reflect.DeepEqual(got, want) {
		t.Errorf("assignment = %v, want %v", got, want)
	}
}

func wantCode(t *testing.T, err *launch.Error, code string) {
	t.Helper()
	if err.Code != code {
		t.Errorf("code = %q (%s), want %q", err.Code, err.Message, code)
	}
}

// TestOrderedSelectionTakesTheFirstCandidate is S1: an open leaf with no
// constraints resolves to its first-preference candidate, and array order
// is what "first" means.
func TestOrderedSelectionTakesTheFirstCandidate(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{Preset: "review"})
	wantAssignment(t, res, "reviewer=codex/gpt-5.6")

	report := res.Report()
	if report.Selection != launch.SelectionOrdered {
		t.Errorf("selection = %q, want ordered", report.Selection)
	}
	if report.Leaves[0].DeclaredKind != "open" {
		t.Errorf("declared_kind = %q, want open", report.Leaves[0].DeclaredKind)
	}
}

// TestDeclaredKindDescribesTheDeclarationNotTheOutcome is §6.5 rule 7: a
// require that narrows an open leaf's surviving pool to exactly one
// candidate does not turn the leaf determined. Only a one-candidate
// declaration is determined.
func TestDeclaredKindDescribesTheDeclarationNotTheOutcome(t *testing.T) {
	r := newResolver(t, scenarioYAML)

	narrowed := resolveOK(t, r, launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisModelLine, Value: "claude-opus-4"}},
	})
	wantAssignment(t, narrowed, "reviewer=claude/claude-opus-4")
	if kind := narrowed.Report().Leaves[0].DeclaredKind; kind != "open" {
		t.Errorf("narrowed-to-one open leaf reports declared_kind %q, want open", kind)
	}

	determined := resolveOK(t, r, launch.Request{Preset: "determined_review"})
	if kind := determined.Report().Leaves[0].DeclaredKind; kind != "determined" {
		t.Errorf("one-candidate leaf reports declared_kind %q, want determined", kind)
	}
}

// TestRequireNarrowsThePool is S5: a launch-time require narrows the named
// pool by exact equality on the public agent-runtime label.
func TestRequireNarrowsThePool(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "opencode"}},
	})
	wantAssignment(t, res, "reviewer=opencode/gpt-5.6")

	// Every candidate the require removed carries the stable reason.
	var eliminated int
	for _, c := range res.Record.Leaves[0].Candidates {
		if c.EliminationReason == launch.ReasonRequireUnmatched {
			eliminated++
		}
	}
	if eliminated != 2 {
		t.Errorf("candidates eliminated by require = %d, want 2", eliminated)
	}
}

// TestRequireNeverRelents is S7 and §6.6's non-relenting half: a require
// that matches nothing is exhaustion, never a fallback to a wider pool and
// never a search of another preset.
func TestRequireNeverRelents(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "pi"}},
	})
	wantCode(t, err, launch.CodeConstraintsExhausted)
	if err.Class != "invalid" {
		t.Errorf("class = %q, want invalid (a caller-correctable narrowing failure)", err.Class)
	}
	if err.Effect != "no_effect" {
		t.Errorf("effect = %q, want no_effect", err.Effect)
	}
}

// TestAvoidWithARemainderDoesNotRelent is S3: a soft avoid that still
// leaves a complete assignment simply narrows, and the result relents on
// nothing.
func TestAvoidWithARemainderDoesNotRelent(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset: "review",
		Avoid:  []launch.Constraint{{Axis: launch.AxisModelLine, Value: "gpt-5.6"}},
	})
	wantAssignment(t, res, "reviewer=claude/claude-opus-4")

	if res.Record.AvoidRelented {
		t.Error("record marks an avoid relent, but a complete assignment survived the avoid")
	}
	if got := res.Report().Leaves[0].RelentedAvoids; len(got) != 0 {
		t.Errorf("relented_avoids = %v, want empty", got)
	}
}

// TestAvoidRelentsWhenItWouldPreventAnyAssignment is S4's driver and §6.7
// step 8: when an avoid is what left no complete assignment, the pre-avoid
// pool is restored and the result reports the avoid its chosen candidate
// actually matched.
func TestAvoidRelentsWhenItWouldPreventAnyAssignment(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset: "determined_review",
		Avoid: []launch.Constraint{
			{Axis: launch.AxisModelLine, Value: "gpt-5.6", Source: "flag"},
		},
	})
	wantAssignment(t, res, "reviewer=codex/gpt-5.6")

	if !res.Record.AvoidRelented {
		t.Error("record does not mark the avoid relent")
	}
	if got, want := res.Record.RestoredCandidates, []string{"compositions.review_codex_gpt56"}; !reflect.DeepEqual(got, want) {
		t.Errorf("restored candidates = %v, want %v", got, want)
	}
	want := []launch.Predicate{{Axis: launch.AxisModelLine, Value: "gpt-5.6"}}
	if got := res.Report().Leaves[0].RelentedAvoids; !reflect.DeepEqual(got, want) {
		t.Errorf("relented_avoids = %v, want %v", got, want)
	}
}

// TestRelentReportsOnlyTheAvoidsTheChosenCandidateMatched is §6.6's
// reporting rule: relenting restores the whole pre-avoid pool, but the
// result names only the predicates the selected assignment actually
// matches. Here the model-line avoid is relented onto and the
// agent-runtime avoid is not, even though both were restored.
func TestRelentReportsOnlyTheAvoidsTheChosenCandidateMatched(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset: "review",
		Avoid: []launch.Constraint{
			{Axis: launch.AxisModelLine, Value: "gpt-5.6"},
			{Axis: launch.AxisModelLine, Value: "claude-opus-4"},
			{Axis: launch.AxisAgentRuntime, Value: "opencode"},
		},
	})
	// Every candidate was avoided, so the relent restores all three and
	// ordered selection takes the first.
	wantAssignment(t, res, "reviewer=codex/gpt-5.6")

	want := []launch.Predicate{{Axis: launch.AxisModelLine, Value: "gpt-5.6"}}
	if got := res.Report().Leaves[0].RelentedAvoids; !reflect.DeepEqual(got, want) {
		t.Errorf("relented_avoids = %v, want just the avoid the chosen candidate matches (%v)", got, want)
	}
	if len(res.Record.RestoredCandidates) != 3 {
		t.Errorf("restored candidates = %v, want all three", res.Record.RestoredCandidates)
	}
}

// TestRelentRestoresOnlyWhatAnAvoidRemoved is the precedence rule §6.6
// fixes: "relenting a request avoid therefore does not expand the
// configured candidate ceiling or undo a system requirement". A require
// that empties the pool exhausts even when an avoid is also present,
// because the relent re-runs over the post-require pools.
func TestRelentRestoresOnlyWhatAnAvoidRemoved(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "pi"}},
		Avoid:   []launch.Constraint{{Axis: launch.AxisModelLine, Value: "gpt-5.6"}},
	})
	wantCode(t, err, launch.CodeConstraintsExhausted)

	details := canonical(t, err)["details"].(map[string]any)
	survivors := details["survivors"].(map[string]any)
	if got := survivors["after_avoid_restoration"].([]any); len(got) != 0 {
		t.Errorf("post-relent survivors = %v, want none: the require, not the avoid, emptied the pool", got)
	}
	for _, c := range details["candidates"].([]any) {
		if reason := c.(map[string]any)["elimination_reason"]; reason != launch.ReasonRequireUnmatched {
			t.Errorf("candidate %v eliminated for %v, want %q", c, reason, launch.ReasonRequireUnmatched)
		}
	}
}

// TestAnAvoidedRequireSurvivorIsRelentedOnto is the mirror case: when the
// require leaves exactly one candidate and the avoid removes it, the relent
// restores that candidate and the result says so.
func TestAnAvoidedRequireSurvivorIsRelentedOnto(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "codex"}},
		Avoid:   []launch.Constraint{{Axis: launch.AxisModelLine, Value: "gpt-5.6"}},
	})
	wantAssignment(t, res, "reviewer=codex/gpt-5.6")
	want := []launch.Predicate{{Axis: launch.AxisModelLine, Value: "gpt-5.6"}}
	if got := res.Report().Leaves[0].RelentedAvoids; !reflect.DeepEqual(got, want) {
		t.Errorf("relented_avoids = %v, want %v", got, want)
	}
}

// TestDistinctModelLineRejectsTuples is S9: the cross-leaf relation
// constrains the complete assignment, and the first tuple that satisfies it
// wins.
func TestDistinctModelLineRejectsTuples(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{Preset: "adversarial_pair"})

	// Leaf order then array order: (codex, codex) and (codex, opencode)
	// both put gpt-5.6 on both leaves and are rejected; (codex, claude) is
	// the first tuple with distinct model lines.
	wantAssignment(t, res, "first=codex/gpt-5.6", "second=claude/claude-opus-4")

	// The enumeration is exhaustive, so the record holds every rejected
	// tuple, not just the ones before the winner: 3x3 candidates, four
	// gpt-5.6 pairs and one claude-opus-4 pair.
	if len(res.Record.RelationRejections) != 5 {
		t.Errorf("relation rejections = %d, want 5", len(res.Record.RelationRejections))
	}
	if got := res.Record.EligibleAssignments; got != 4 {
		t.Errorf("eligible assignments = %d, want the 4 tuples with distinct model lines", got)
	}
	for _, r := range res.Record.RelationRejections {
		if r.Kind != "distinct_model_line" {
			t.Errorf("rejection kind = %q", r.Kind)
		}
	}
}

// TestDistinctModelLineCanExhaust proves the relation is a real constraint
// on the whole plan: narrow both leaves to one model line and no complete
// assignment exists, even though every leaf on its own has candidates.
func TestDistinctModelLineCanExhaust(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "adversarial_pair",
		Require: []launch.Constraint{{Axis: launch.AxisModelLine, Value: "gpt-5.6"}},
	})
	wantCode(t, err, launch.CodeConstraintsExhausted)
}

// TestMultiLeafResolutionIsAtomic is §6.7's "every leaf and relation
// resolves before any process spawns. Any resolution failure launches
// nothing": one leaf that cannot be assigned fails the whole plan, and the
// leaf that could have been assigned is not returned in a partial result.
func TestMultiLeafResolutionIsAtomic(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "mixed_pair",
		Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "codex"}},
	})
	wantCode(t, err, launch.CodeConstraintsExhausted)

	details := canonical(t, err)["details"].(map[string]any)
	// alpha's codex candidate survived the require and beta's did not;
	// the failure is still whole-plan, and the surviving candidate is
	// reported as a survivor rather than as a launched leaf.
	survivors := details["survivors"].(map[string]any)
	after := survivors["after_require"].([]any)
	if len(after) != 1 || after[0] != "compositions.review_codex_gpt56" {
		t.Errorf("after_require survivors = %v, want just alpha's candidate", after)
	}
	if got := details["complete_assignments_survived"]; got != float64(0) {
		t.Errorf("complete_assignments_survived = %v, want 0", got)
	}
}

// TestRandomSelectionIsDeterministicUnderAnInjectedSource is §7.2's
// qualified determinism: the same static inputs plus the same recorded draw
// evidence produce the same selected assignment. The draw records the set
// size and index that explain the choice.
func TestRandomSelectionIsDeterministicUnderAnInjectedSource(t *testing.T) {
	resolveWithSeed := func(seed uint64) *launch.Resolution {
		r := newResolver(t, scenarioYAML, func(o *launch.Options) {
			o.Random = launch.NewSeededSource(seed)
		})
		return resolveOK(t, r, launch.Request{Preset: "review_random"})
	}

	first := resolveWithSeed(7)
	second := resolveWithSeed(7)
	if !reflect.DeepEqual(assignment(first), assignment(second)) {
		t.Errorf("the same seed drew %v then %v", assignment(first), assignment(second))
	}

	draw := first.Record.Draw
	if draw == nil {
		t.Fatal("a random selection recorded no draw evidence")
	}
	if draw.SetSize != 3 {
		t.Errorf("draw set size = %d, want the 3 eligible assignments", draw.SetSize)
	}
	if draw.Index < 0 || draw.Index >= 3 {
		t.Errorf("draw index = %d, outside the eligible set", draw.Index)
	}
	if first.Report().Selection != launch.SelectionRandom {
		t.Errorf("selection = %q, want random", first.Report().Selection)
	}

	// Randomness changes only which member of the eligible set is chosen;
	// the set itself is the same one ordered mode enumerates.
	if first.Record.EligibleAssignments != 3 {
		t.Errorf("eligible assignments = %d, want 3", first.Record.EligibleAssignments)
	}
}

// TestRandomSelectionRespectsConstraints proves randomness never widens a
// pool: with two candidates avoided, the only draw available is the
// remaining one, on every seed.
func TestRandomSelectionRespectsConstraints(t *testing.T) {
	for seed := uint64(0); seed < 8; seed++ {
		r := newResolver(t, scenarioYAML, func(o *launch.Options) {
			o.Random = launch.NewSeededSource(seed)
		})
		res := resolveOK(t, r, launch.Request{
			Preset:  "review_random",
			Require: []launch.Constraint{{Axis: launch.AxisModelLine, Value: "claude-opus-4"}},
		})
		wantAssignment(t, res, "reviewer=claude/claude-opus-4")
	}
}

// TestRandomPresetNeedsAnInjectedSource pins the no-hidden-entropy rule: a
// resolver with no random source refuses a random preset instead of
// reaching for a package-level generator.
func TestRandomPresetNeedsAnInjectedSource(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{Preset: "review_random"})
	wantCode(t, err, launch.CodeInvalidRequest)
}

// TestPresetNotFound is §6.8's first row.
func TestPresetNotFound(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{Preset: "nope"})
	wantCode(t, err, launch.CodePresetNotFound)
	if err.Class != "not_found" {
		t.Errorf("class = %q, want not_found", err.Class)
	}
	details := canonical(t, err)["details"].(map[string]any)
	if details["requested_preset"] != "nope" {
		t.Errorf("details = %v, want the requested name", details)
	}
}

// TestContradictoryRequiresAreInvalid is §6.6's "at most one required value
// per axis in one effective request".
func TestContradictoryRequiresAreInvalid(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{
		Preset: "review",
		Require: []launch.Constraint{
			{Axis: launch.AxisAgentRuntime, Value: "codex"},
			{Axis: launch.AxisAgentRuntime, Value: "opencode"},
		},
	})
	wantCode(t, err, launch.CodeInvalidRequest)
}

// TestRepeatedConstraintsNormalizeAndKeepProvenance is §6.6's normalization
// rule: the same constraint asked for twice is one constraint that
// remembers both askers.
func TestRepeatedConstraintsNormalizeAndKeepProvenance(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset: "review",
		Require: []launch.Constraint{
			{Axis: launch.AxisAgentRuntime, Value: "codex", Source: "flag"},
			{Axis: launch.AxisAgentRuntime, Value: "codex", Source: "policy:site"},
		},
	})
	require := res.Record.Constraints.Require
	if len(require) != 1 {
		t.Fatalf("normalized require = %v, want one entry", require)
	}
	if got, want := require[0].Sources, []string{"flag", "policy:site"}; !reflect.DeepEqual(got, want) {
		t.Errorf("provenance = %v, want %v", got, want)
	}
}

// TestUnknownAxisIsInvalid is §6.8's malformed-grammar row.
func TestUnknownAxisIsInvalid(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: "session_host", Value: "local_tmux"}},
	})
	wantCode(t, err, launch.CodeInvalidRequest)
}

// TestUnresolvedCompositionReference is §6.5 rule 3: a preset that
// references a composition nothing declares is declaration ambiguity, not
// narrowing.
func TestUnresolvedCompositionReference(t *testing.T) {
	const yaml = `
schema: duo.config/v2
session_hosts:
  local_tmux: {kind: tmux}
agent_runtimes:
  codex_default: {kind: codex}
launch_variants:
  codex_in_tmux:
    agent_runtime: codex_default
    session_host: local_tmux
    executable: codex
compositions:
  declared: {launch_variant: codex_in_tmux, model_line: gpt-5.6}
presets:
  review:
    leaves:
      reviewer:
        candidates:
          - composition: missing
`
	err := resolveErr(t, newResolver(t, yaml), launch.Request{Preset: "review"})
	wantCode(t, err, launch.CodeCompositionUnresolved)
	if !strings.Contains(err.Message, "compositions.missing") {
		t.Errorf("message = %q, want the declaration locator", err.Message)
	}
}

// TestAgentRuntimeKindIsNeverInferred pins §6.4 graft 7's separation of
// declaration locators from identity: the public agent-runtime value comes
// from the runtime declaration's kind, and a declaration without one is
// unresolved rather than silently named after its config key.
func TestAgentRuntimeKindIsNeverInferred(t *testing.T) {
	const yaml = `
schema: duo.config/v2
session_hosts:
  local_tmux: {kind: tmux}
agent_runtimes:
  codex_default: {}
launch_variants:
  codex_in_tmux:
    agent_runtime: codex_default
    session_host: local_tmux
    executable: codex
compositions:
  review: {launch_variant: codex_in_tmux, model_line: gpt-5.6}
presets:
  review:
    leaves:
      reviewer:
        candidates:
          - composition: review
`
	err := resolveErr(t, newResolver(t, yaml), launch.Request{Preset: "review"})
	wantCode(t, err, launch.CodeCompositionUnresolved)
}

// TestDisabledSessionHostLeavesNoEligibleCandidate is §6.8's installation
// row: an enabled-host declaration, not a constraint, emptied the pool, so
// the failure is unavailable/launch.no_eligible_candidate and never
// constraints_exhausted.
func TestDisabledSessionHostLeavesNoEligibleCandidate(t *testing.T) {
	yaml := strings.Replace(scenarioYAML,
		"  local_tmux:\n    kind: tmux\n    server: default\n",
		"  local_tmux:\n    kind: tmux\n    server: default\n    enabled: false\n", 1)

	err := resolveErr(t, newResolver(t, yaml), launch.Request{Preset: "review"})
	wantCode(t, err, launch.CodeNoEligibleCandidate)
	if err.Class != "unavailable" {
		t.Errorf("class = %q, want unavailable", err.Class)
	}
	details := canonical(t, err)["details"].(map[string]any)
	for _, c := range details["candidates"].([]any) {
		row := c.(map[string]any)
		if row["elimination_reason"] != launch.ReasonSessionHostDisabled {
			t.Errorf("candidate %v, want reason %q", row, launch.ReasonSessionHostDisabled)
		}
	}
}

// TestMissingConformanceEvidenceEliminatesACandidate is §6.7 step 3 and
// §7.1's installed-evidence rung: a tuple with no accepted conformance
// record is dropped before any constraint applies, with the consulted
// digest recorded.
func TestMissingConformanceEvidenceEliminatesACandidate(t *testing.T) {
	r := newResolver(t, scenarioYAML, func(o *launch.Options) {
		o.Support = launch.SupportFunc(func(tuple launch.Tuple) launch.Verdict {
			if tuple.AgentRuntime == "codex" {
				return launch.Verdict{RecordDigest: "sha256:conformance-fixture"}
			}
			return launch.Verdict{OK: true, RecordDigest: "sha256:conformance-fixture"}
		})
	})

	res := resolveOK(t, r, launch.Request{Preset: "review"})
	wantAssignment(t, res, "reviewer=opencode/gpt-5.6")

	if got, want := res.Record.ConsultedRecordDigests, []string{"sha256:conformance-fixture"}; !reflect.DeepEqual(got, want) {
		t.Errorf("consulted digests = %v, want %v", got, want)
	}
	if reason := res.Record.Leaves[0].Candidates[0].EliminationReason; reason != launch.ReasonNoConformanceEvidence {
		t.Errorf("elimination reason = %q, want %q", reason, launch.ReasonNoConformanceEvidence)
	}
}

// TestNoEligibleCandidateOutranksConstraints proves the two installation
// and narrowing failures cannot be confused: with every candidate
// unsupported *and* a require that would also have emptied the pool, the
// reported failure is the installation one, because §6.7 puts the evidence
// drop before the constraints.
func TestNoEligibleCandidateOutranksConstraints(t *testing.T) {
	r := newResolver(t, scenarioYAML, func(o *launch.Options) {
		o.Support = launch.SupportFunc(func(launch.Tuple) launch.Verdict {
			return launch.Verdict{RecordDigest: "sha256:conformance-fixture"}
		})
	})
	err := resolveErr(t, r, launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "pi"}},
	})
	wantCode(t, err, launch.CodeNoEligibleCandidate)
}

// TestComplexityLimitIsADeclarationError is §6.7's bounded-search rule: an
// oversized declaration is refused, never truncated, because truncating
// would silently change which candidate wins.
func TestComplexityLimitIsADeclarationError(t *testing.T) {
	r := newResolver(t, scenarioYAML, func(o *launch.Options) {
		o.Limits = launch.Limits{MaxLeaves: 16, MaxCandidatesPerLeaf: 2, MaxAssignments: 4096}
	})
	err := resolveErr(t, r, launch.Request{Preset: "review"})
	wantCode(t, err, launch.CodeCompositionUnresolved)

	r = newResolver(t, scenarioYAML, func(o *launch.Options) {
		o.Limits = launch.Limits{MaxLeaves: 16, MaxCandidatesPerLeaf: 64, MaxAssignments: 4}
	})
	err = resolveErr(t, r, launch.Request{Preset: "adversarial_pair"})
	wantCode(t, err, launch.CodeCompositionUnresolved)
}

// TestResolutionIsRepeatable is §7.2's baseline determinism contract in
// ordered mode: the same document, constraints, and installed evidence
// produce the same selected assignment and the same record content, every
// time.
func TestResolutionIsRepeatable(t *testing.T) {
	req := launch.Request{
		Preset: "adversarial_pair",
		Avoid:  []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "opencode"}},
	}
	var previous map[string]any
	for i := 0; i < 3; i++ {
		r := newResolver(t, scenarioYAML, func(o *launch.Options) {
			o.NewID = fixedIDs("lrr_repeat")
			o.Now = fixedClock()
		})
		got := canonical(t, resolveOK(t, r, req).Record)
		if previous != nil && !reflect.DeepEqual(got, previous) {
			t.Fatalf("resolution %d differs from the previous one\n got: %#v\nwant: %#v", i, got, previous)
		}
		previous = got
	}
}

// TestRecordCarriesTheWholeExplanation pins §7.3's split: the record holds
// the candidate table, the digests, and the pools, and the ordinary result
// holds none of them.
func TestRecordCarriesTheWholeExplanation(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset: "review",
		Avoid:  []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "codex"}},
	})

	leaf := res.Record.Leaves[0]
	if len(leaf.Candidates) != 3 {
		t.Errorf("record leaf carries %d candidates, want all 3 declared", len(leaf.Candidates))
	}
	if got, want := leaf.Survivors.BeforeConstraints, []string{
		"compositions.review_codex_gpt56",
		"compositions.review_opencode_gpt56",
		"compositions.review_claude_opus4",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("pre-constraint pool = %v, want %v", got, want)
	}
	if got := leaf.Survivors.AfterAvoid; len(got) != 2 {
		t.Errorf("post-avoid pool = %v, want the two candidates the avoid left", got)
	}
	if res.Record.ConfigurationDigest == "" {
		t.Error("record carries no configuration digest")
	}

	report := canonical(t, res.Report())
	for _, absent := range []string{"candidates", "survivors", "consulted_record_digests", "draw", "constraints"} {
		if _, present := report[absent]; present {
			t.Errorf("ordinary result exposes %q, which belongs to the record", absent)
		}
	}
}
