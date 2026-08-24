package launch_test

import (
	"context"
	"reflect"
	"sort"
	"testing"

	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// --- the join -------------------------------------------------------------

// TestTheJoinMintsTheComposition is duo-config-v3's central structural
// change (notes/42 §4 step 2): nothing authors a composition any more. Each
// candidate is one launch variant joined to the single host M1 deduced, and
// the join is what names the result.
//
// The name format is `<variant>@<host_kind>`, which is the form step 03
// fixed across every re-authored launch fixture. The instance is not in the
// name — exactly one host is deduced per resolution, so the kind already
// separates every join inside one record — and it travels on the tuple
// instead.
func TestTheJoinMintsTheComposition(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{Preset: "review"})

	tuple := res.Assignments()[0].Tuple
	if got, want := tuple.Composition, "review_codex_gpt56@"+testHostKind; got != want {
		t.Errorf("minted composition = %q, want %q", got, want)
	}
	if got, want := tuple.LaunchVariant, "launch_variants.review_codex_gpt56"; got != want {
		t.Errorf("declaration locator = %q, want %q", got, want)
	}
	if tuple.HostKind != testHostKind || tuple.HostInstance != testHostInstance {
		t.Errorf("joined host = %s:%s, want %s:%s",
			tuple.HostKind, tuple.HostInstance, testHostKind, testHostInstance)
	}
	if tuple.IntegrationInstanceID != testHostInstanceID {
		t.Errorf("integration instance = %q, want the deduced host's own ID %q",
			tuple.IntegrationInstanceID, testHostInstanceID)
	}
	if tuple.ModelFamily != "gpt" {
		t.Errorf("model family = %q, want gpt", tuple.ModelFamily)
	}
	if tuple.Provider != "openai" {
		t.Errorf("provider = %q, want openai", tuple.Provider)
	}
}

// TestEveryCandidateJoinsTheOneDeducedHost is the property the join
// replaces the v2 dereference chain with: a candidate can no longer
// disagree with another candidate about where it runs, because there is one
// host for the whole resolution and no candidate carries its own.
func TestEveryCandidateJoinsTheOneDeducedHost(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{Preset: "adversarial_pair"})

	for _, leaf := range res.Record.Leaves {
		for _, c := range leaf.Candidates {
			if c.Tuple.HostKind != testHostKind || c.Tuple.HostInstance != testHostInstance {
				t.Errorf("%s joined %s:%s, want the one deduced host",
					c.Tuple.Composition, c.Tuple.HostKind, c.Tuple.HostInstance)
			}
		}
	}
}

// TestTheCommandComesFromTheRuntimePlusTheVariantsAppends pins v3's move of
// the command shape: `executable` and the base `arguments` belong to the
// agent runtime, and a variant contributes only what it appends.
func TestTheCommandComesFromTheRuntimePlusTheVariantsAppends(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "claude"}},
	})
	tuple := res.Assignments()[0].Tuple
	if tuple.Executable != "claude" {
		t.Errorf("executable = %q, want the runtime's declared executable", tuple.Executable)
	}
	if !reflect.DeepEqual(tuple.Arguments, []string{"--continue"}) {
		t.Errorf("arguments = %v, want the runtime's base arguments", tuple.Arguments)
	}
}

// --- the re-key -----------------------------------------------------------

// keyRecordingSupport records every SupportKey the resolver consults.
type keyRecordingSupport struct {
	keys *[]launch.SupportKey
}

func (s keyRecordingSupport) Supported(t launch.Tuple) launch.Verdict {
	*s.keys = append(*s.keys, t.SupportKey())
	return launch.Verdict{OK: true, RecordDigest: "sha256:conformance-fixture"}
}

// TestSupportIsKeyedOnHostKindVersionAndRuntime is the thread-5 re-key.
//
// Under v2 the evidence key rode on the tuple's *config* session-host
// declaration name, so two `session_hosts` entries pointing at one socket
// were two evidence keys for one real host, and renaming a config entry
// invalidated a conformance lookup that nothing about the installation had
// changed. Config names are gone in v3, and the key is now (host kind, host
// version, agent-runtime kind).
//
// The two socket-shaped joins here are the successor of exactly that v2
// pair: two names for one host. They resolve to one key, and the two
// candidates that differ only in model line, model family, and provider
// resolve to one key too.
func TestSupportIsKeyedOnHostKindVersionAndRuntime(t *testing.T) {
	doc := parseDoc(t, scenarioYAML)

	// Two materializations of the same kind and version, deduced two
	// different ways and naming two different instances — the v2 pair of
	// config names for one socket, with the config names gone.
	viaPolicy := materialized(t, doc)
	viaFlag := materialized(t, doc, func(o *materialize.Options) {
		o.HostFlag = testHostKind + ":/run/tmux-1000/other"
	})

	var keys []launch.SupportKey
	for _, mat := range []materialize.Result{viaPolicy, viaFlag} {
		r := newResolverOver(t, doc, mat, func(o *launch.Options) {
			o.Support = keyRecordingSupport{keys: &keys}
		})
		resolveOK(t, r, launch.Request{Preset: "review"})
	}

	// Three declared candidates x two resolutions = six consultations, and
	// exactly three distinct keys: one per agent-runtime kind. Neither the
	// instance, nor the variant name, nor the model line, family, or
	// provider is in the key.
	if len(keys) != 6 {
		t.Fatalf("Support consulted %d times, want 6", len(keys))
	}
	seen := map[launch.SupportKey]bool{}
	for _, k := range keys {
		if k.HostKind != testHostKind || k.HostVersion != testHostVersion {
			t.Errorf("key %v does not carry the deduced kind and the pinned version", k)
		}
		seen[k] = true
	}
	if len(seen) != 3 {
		t.Errorf("distinct evidence keys = %d (%v), want 3, one per agent-runtime kind", len(seen), seen)
	}

	// And the same key from both sockets, stated directly.
	byRuntime := map[string][]launch.SupportKey{}
	for _, k := range keys {
		byRuntime[k.AgentRuntime] = append(byRuntime[k.AgentRuntime], k)
	}
	for runtime, group := range byRuntime {
		for _, k := range group {
			if k != group[0] {
				t.Errorf("runtime %s produced two evidence keys, %v and %v", runtime, group[0], k)
			}
		}
	}
}

// TestHostVersionIsThePinnedBuildConstant records the thread-5 position in
// a test: the version in the key is whatever the caller supplied from the
// installed adapter's descriptor, matched exactly. A kind this build
// carries no adapter for has no version, and the Support oracle is the
// thing that refuses it — the resolver never invents one, and never probes
// for one.
func TestHostVersionIsThePinnedBuildConstant(t *testing.T) {
	doc := parseDoc(t, scenarioYAML)
	var keys []launch.SupportKey
	r := newResolverOver(t, doc, materialized(t, doc), func(o *launch.Options) {
		o.HostVersions = map[string]string{"some-other-kind": "9.9"}
		o.Support = keyRecordingSupport{keys: &keys}
	})
	resolveOK(t, r, launch.Request{Preset: "review"})

	if len(keys) == 0 {
		t.Fatal("Support was never consulted")
	}
	if keys[0].HostVersion != "" {
		t.Errorf("host version = %q, want empty: this build declares no adapter for %q",
			keys[0].HostVersion, testHostKind)
	}
}

// --- provider_disabled ----------------------------------------------------

// TestProviderDisabledEliminatesTaggedVariantsOnly is step 3.2: the
// elimination is against M2's snapshot, and an *untagged* variant is never
// affected, because provider state is keyed by name and a variant that
// names no provider names nothing a `duo provider disable` could have
// switched off.
func TestProviderDisabledEliminatesTaggedVariantsOnly(t *testing.T) {
	doc := parseDoc(t, scenarioYAML)
	mat := materialized(t, doc, func(o *materialize.Options) {
		o.Providers = disabledProviders("openai")
	})

	res := resolveOK(t, newResolverOver(t, doc, mat), launch.Request{Preset: "review"})
	// The codex variant is tagged `openai` and goes; the untagged opencode
	// variant is next in preference order and wins.
	wantAssignment(t, res, "reviewer=opencode/gpt-5.6")

	byLocator := map[string]launch.RecordCandidate{}
	for _, c := range res.Record.Leaves[0].Candidates {
		byLocator[c.Tuple.LaunchVariant] = c
	}
	codex := byLocator["launch_variants.review_codex_gpt56"]
	if codex.EliminationReason != launch.ReasonProviderDisabled {
		t.Errorf("tagged candidate reason = %q, want %q", codex.EliminationReason, launch.ReasonProviderDisabled)
	}
	if codex.ProviderFactID != "f_disabled_openai" {
		t.Errorf("provider fact id = %q, want the fact M2 snapshotted", codex.ProviderFactID)
	}
	if got := byLocator["launch_variants.review_opencode_gpt56"].EliminationReason; got != "" {
		t.Errorf("untagged candidate was eliminated for %q; no provider fact can reach it", got)
	}
	if got := byLocator["launch_variants.review_claude_opus4"].EliminationReason; got != "" {
		t.Errorf("candidate on an enabled provider was eliminated for %q", got)
	}
}

// TestProviderDisabledIsHardAndNeverRelents is I-2's boundary, exercised.
// The relent re-run undoes avoid_matched and nothing else, so an avoid that
// empties the pool cannot resurrect a candidate a provider fact removed:
// the resolution exhausts instead.
func TestProviderDisabledIsHardAndNeverRelents(t *testing.T) {
	doc := parseDoc(t, scenarioYAML)
	mat := materialized(t, doc, func(o *materialize.Options) {
		o.Providers = disabledProviders("openai")
	})
	r := newResolverOver(t, doc, mat)

	// determined_review declares only the openai-tagged codex variant, so
	// step 3 empties the leaf before any constraint runs. That is an
	// installation fact, not narrowing.
	err := resolveErr(t, r, launch.Request{Preset: "determined_review"})
	wantCode(t, err, launch.CodeNoEligibleCandidate)

	// And with an avoid present — the one verb that relents — the answer
	// does not change, and nothing is restored.
	r = newResolverOver(t, doc, mat)
	err = resolveErr(t, r, launch.Request{
		Preset: "determined_review",
		Avoid:  []launch.Constraint{{Axis: launch.AxisModelLine, Value: "gpt-5.6", Source: "flag"}},
	})
	wantCode(t, err, launch.CodeNoEligibleCandidate)
	details := canonical(t, err)["details"].(map[string]any)
	for _, c := range details["candidates"].([]any) {
		row := c.(map[string]any)
		if row["elimination_reason"] != launch.ReasonProviderDisabled {
			t.Errorf("candidate %v, want reason %q", row, launch.ReasonProviderDisabled)
		}
	}
}

// TestProviderDisabledOutranksAConstraintFailure keeps the two failures
// from being confused, the same way the conformance drop already is: step 3
// runs strictly before any launch constraint, so a disabled provider is
// reported as an installation fact even when a require would also have
// emptied the pool.
func TestProviderDisabledOutranksAConstraintFailure(t *testing.T) {
	doc := parseDoc(t, scenarioYAML)
	mat := materialized(t, doc, func(o *materialize.Options) {
		o.Providers = disabledProviders("openai", "anthropic")
	})
	// Every provider-tagged variant is out; the untagged opencode variant
	// survives step 3 and is then removed by a require it cannot satisfy.
	// The leaf is empty either way, and step 3's verdict is the one that
	// stands, because it was reached first.
	r := newResolverOver(t, doc, mat)
	res := resolveOK(t, r, launch.Request{Preset: "review"})
	wantAssignment(t, res, "reviewer=opencode/gpt-5.6")

	r = newResolverOver(t, doc, mat)
	err := resolveErr(t, r, launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "codex"}},
	})
	wantCode(t, err, launch.CodeConstraintsExhausted)
	reasons := map[string]string{}
	for _, c := range canonical(t, err)["details"].(map[string]any)["candidates"].([]any) {
		row := c.(map[string]any)
		reasons[row["declaration_locator"].(string)], _ = row["elimination_reason"].(string)
	}
	if got := reasons["launch_variants.review_codex_gpt56"]; got != launch.ReasonProviderDisabled {
		t.Errorf("codex reason = %q, want %q: step 3 ran before the require",
			got, launch.ReasonProviderDisabled)
	}
	if got := reasons["launch_variants.review_opencode_gpt56"]; got != launch.ReasonRequireUnmatched {
		t.Errorf("opencode reason = %q, want %q", got, launch.ReasonRequireUnmatched)
	}
}

// --- the third axis -------------------------------------------------------

// TestRequireOnModelFamilyNarrows is v3's third require/avoid axis
// (notes/42 §4): exact equality on the variant's declared model_family
// label, the same comparison the other two axes make.
func TestRequireOnModelFamilyNarrows(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisModelFamily, Value: "claude"}},
	})
	wantAssignment(t, res, "reviewer=claude/claude-opus-4")

	var eliminated int
	for _, c := range res.Record.Leaves[0].Candidates {
		if c.EliminationReason == launch.ReasonRequireUnmatched {
			eliminated++
		}
	}
	if eliminated != 2 {
		t.Errorf("candidates eliminated by the model_family require = %d, want the two gpt variants", eliminated)
	}
}

// TestAvoidOnModelFamilyRemovesAWholeFamily is
// contracts/fixtures/duo-external-v1/session-launch-model-family-avoid.json's
// case: one avoid on the family label removes every variant in it at once,
// which is the reason the axis exists — a caller that wants "not gpt" would
// otherwise have to enumerate every gpt model line.
func TestAvoidOnModelFamilyRemovesAWholeFamily(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{
		Preset: "review",
		Avoid:  []launch.Constraint{{Axis: launch.AxisModelFamily, Value: "gpt"}},
	})
	wantAssignment(t, res, "reviewer=claude/claude-opus-4")
	if res.Record.AvoidRelented {
		t.Error("record marks a relent, but a complete assignment survived the avoid")
	}

	var avoided []string
	for _, c := range res.Record.Leaves[0].Candidates {
		if c.EliminationReason == launch.ReasonAvoidMatched {
			avoided = append(avoided, c.Tuple.LaunchVariant)
		}
	}
	sort.Strings(avoided)
	want := []string{"launch_variants.review_codex_gpt56", "launch_variants.review_opencode_gpt56"}
	if !reflect.DeepEqual(avoided, want) {
		t.Errorf("avoided = %v, want the whole gpt family %v", avoided, want)
	}
}

// TestThreeAxesAreConjunctiveOnRequire is §6.6's conjunction rule extended
// to three axes: different-axis requirements all have to hold at once.
func TestThreeAxesAreConjunctiveOnRequire(t *testing.T) {
	r := newResolver(t, scenarioYAML)
	res := resolveOK(t, r, launch.Request{
		Preset: "review",
		Require: []launch.Constraint{
			{Axis: launch.AxisAgentRuntime, Value: "opencode"},
			{Axis: launch.AxisModelLine, Value: "gpt-5.6"},
			{Axis: launch.AxisModelFamily, Value: "gpt"},
		},
	})
	wantAssignment(t, res, "reviewer=opencode/gpt-5.6")

	// Change one axis to a value the same candidate does not carry and the
	// conjunction fails, even though the other two still match.
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{
		Preset: "review",
		Require: []launch.Constraint{
			{Axis: launch.AxisAgentRuntime, Value: "opencode"},
			{Axis: launch.AxisModelFamily, Value: "claude"},
		},
	})
	wantCode(t, err, launch.CodeConstraintsExhausted)
}

// TestDistinctModelFamilyRejectsTuples is step 02's new cross-leaf relation
// kind, resolving: it compares exact declared model_family labels pairwise
// across the leaves it names, exactly as distinct_model_line compares model
// lines.
func TestDistinctModelFamilyRejectsTuples(t *testing.T) {
	res := resolveOK(t, newResolver(t, scenarioYAML), launch.Request{Preset: "distinct_family_pair"})

	// Leaf order then array order: (codex, codex) and (codex, opencode)
	// are both gpt on both leaves and are rejected; (codex, claude) is the
	// first assignment with distinct families.
	wantAssignment(t, res, "first=codex/gpt-5.6", "second=claude/claude-opus-4")
	for _, rejection := range res.Record.RelationRejections {
		if rejection.Kind != "distinct_model_family" {
			t.Errorf("rejection kind = %q, want distinct_model_family", rejection.Kind)
		}
	}
	// 3x3 assignments; the four gpt/gpt pairs and the one claude/claude
	// pair violate the relation.
	if got := len(res.Record.RelationRejections); got != 5 {
		t.Errorf("relation rejections = %d, want 5", got)
	}
	if got := res.Record.EligibleAssignments; got != 4 {
		t.Errorf("eligible assignments = %d, want the 4 with distinct families", got)
	}
}

// TestDistinctModelFamilyCanExhaust proves the new relation is a real
// constraint on the whole plan and not decoration: narrow both leaves to
// one family and no complete assignment exists, even though each leaf on
// its own still has candidates.
func TestDistinctModelFamilyCanExhaust(t *testing.T) {
	err := resolveErr(t, newResolver(t, scenarioYAML), launch.Request{
		Preset:  "distinct_family_pair",
		Require: []launch.Constraint{{Axis: launch.AxisModelFamily, Value: "gpt"}},
	})
	wantCode(t, err, launch.CodeConstraintsExhausted)
}

// TestResolverRefusesAMaterializationWithNoHost is I-3's other half: the
// resolver is *told* where the host is and never looks. A materialization
// that produced none is refused at construction rather than resolved
// against an empty join — the CLI raises launch.host_unresolved before it
// ever gets here, and this refusal is what stops a hand-built caller from
// skipping that.
func TestResolverRefusesAMaterializationWithNoHost(t *testing.T) {
	doc := parseDoc(t, scenarioYAML)
	// M1 itself refuses to return a hostless Result — it raises the
	// materialization failure instead — so the only way to hand the
	// resolver one is to build it by hand, which is precisely the caller
	// this refusal exists for.
	var empty materialize.Result
	if empty.Host().Present() {
		t.Fatal("the zero materialization carries a host")
	}
	if _, err := launch.NewResolver(doc, empty, defaultOptions()); err == nil {
		t.Fatal("NewResolver accepted a materialization with no deduced host")
	}
}

// TestMaterializationRefusesToProduceNoHost is the other side of the same
// boundary, pinned here because it is what makes the refusal above
// unreachable in practice: with no flag, no correlation, no ambient
// environment, and nothing discoverable, M1 fails loudly rather than
// handing the resolver an empty join.
func TestMaterializationRefusesToProduceNoHost(t *testing.T) {
	doc := parseDoc(t, scenarioYAML)
	_, err := materialize.Materialize(context.Background(), materialize.Options{
		WorkspaceFlag: "/work/example",
		Policy:        doc.SessionHosts,
		LookupEnv:     func(string) (string, bool) { return "", false },
		Now:           fixedClock(),
	})
	if err == nil {
		t.Fatal("materialization with nothing to deduce from succeeded")
	}
}
