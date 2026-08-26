package launch_test

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
	"github.com/procrastivity/duo/internal/registry"
)

// This file holds the fixture-driven conformance tests for duo.config/v3's
// launch payloads.
//
// The fixtures are step 03's re-authored launch documents
// (`fixtures/duo-external-v1/`, commit 32e01fe), read from the embedded
// contracts.FS. Before Step 16 synced the contract set (workplan Risk 6),
// this package read testdata/terminal-multiplexers/fixtures/ instead — a
// hand-copied snapshot taken because the embedded set was still the
// pre-v3 one; that copy is gone now that contracts.FS carries the real
// thing.
//
// The comparison is a *shape* comparison, not whole-document equality, and
// that is forced rather than chosen. The fixtures carry hand-authored
// opaque identifiers — `hostinst_herdr_new`, `fact_corr_ws_abc_solo`,
// `lrr_fixture_8` — that no real materialization can produce: M1 folds a
// Herdr session name into `herdr:<session>`, and a fact ID comes from the
// kernel. The fixtures also disagree with each other about optional keys
// (`session-launch.json` carries `outranked_evidence: []` where
// `session-launch-model-family-avoid.json` omits it), so no single emission
// rule satisfies all of them byte for byte. What *is* normative in them —
// the code, the class, the effect, the retry action, the message, the
// per-candidate elimination reasons, the per-reason tallies, the deduced
// host's rung, the pointer set, the evidence references — is exactly what
// shapeOfFailure and shapeOfResult extract, and every one of those is
// compared. The rest is checked by keysPresent (every key a fixture
// declares, this build emits) and by the schema validation in
// schema_test.go.

// envelope is the duo.external/v1 operation envelope the projection layer
// wraps a result or an error in. It lives in the test, not in the launch
// package: internal/launch owns the launch semantics, and the envelope
// belongs to whoever projects an operation onto a wire (Step 21's CLI).
type envelope struct {
	Schema    string         `json:"schema"`
	RequestID string         `json:"request_id"`
	Operation string         `json:"operation"`
	Result    *launch.Report `json:"result"`
	Warnings  []any          `json:"warnings"`
	Features  []any          `json:"features"`
}

// errorEnvelope is the failure envelope. It is a separate type because a
// duo.external/v1 error envelope carries no warnings or features arrays at
// all, and an empty array is a different document from an absent key.
type errorEnvelope struct {
	Schema    string `json:"schema"`
	RequestID string `json:"request_id"`
	Operation string `json:"operation"`
	Error     any    `json:"error"`
}

// fixture decodes one step-03 fixture out of the embedded contract set.
func fixture(t *testing.T, name string) map[string]any {
	t.Helper()
	path := "fixtures/duo-external-v1/" + name
	data, err := contracts.FS.ReadFile(path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return out
}

// --- the scenarios the fixtures describe ----------------------------------

// herdrAmbientYAML is the declaration behind session-launch.json,
// session-launch-model-family-avoid.json, and
// session-launch-distinct-model-family.json: three variants over three
// runtimes, and the three presets those fixtures resolve.
const herdrAmbientYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
  kinds:
    herdr:
      launch_target: tab
agent_runtimes:
  codex_default: {kind: codex, executable: codex}
  claude_default: {kind: claude, executable: claude}
  pi_default: {kind: pi, executable: pi}
launch_variants:
  codex_gpt56: {agent_runtime: codex_default, model_line: gpt-5.6-codex, model_family: gpt}
  claude_sonnet: {agent_runtime: claude_default, model_line: sonnet-5, model_family: claude}
  pi_gpt56: {agent_runtime: pi_default, model_line: gpt-5.6, model_family: gpt}
presets:
  review:
    selection: ordered
    leaves:
      reviewer:
        candidates: [{variant: codex_gpt56}, {variant: claude_sonnet}]
  verify:
    selection: ordered
    leaves:
      verifier:
        candidates: [{variant: claude_sonnet}]
  builder_and_verifier:
    selection: ordered
    leaves:
      builder:
        candidates: [{variant: claude_sonnet}]
      verifier:
        candidates: [{variant: pi_gpt56}]
    relations:
      - {kind: distinct_model_family, leaves: [builder, verifier]}
`

// exhaustedFixtureYAML is session-launch-exhausted.json's declaration: one
// leaf with exactly one candidate, which is what makes the fixture's single
// candidate row and its count-of-one tally true of it.
const exhaustedFixtureYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
agent_runtimes:
  codex_default: {kind: codex, executable: codex}
launch_variants:
  codex_gpt56: {agent_runtime: codex_default, model_line: gpt-5.6-codex, model_family: gpt}
presets:
  review:
    selection: ordered
    leaves:
      reviewer:
        candidates: [{variant: codex_gpt56}]
`

// providerDisabledYAML is session-launch-provider-disabled.json's
// declaration: one leaf whose every candidate carries the same provider
// tag, over three different agent runtimes. The point of three runtimes is
// that provider state is not a runtime property — switching `codex` off
// takes out the Pi and OpenCode variants that run on it too.
const providerDisabledYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [solo]
agent_runtimes:
  codex_default: {kind: codex, executable: codex}
  pi_default: {kind: pi, executable: pi}
  opencode_default: {kind: opencode, executable: opencode}
launch_variants:
  codex_gpt56: {agent_runtime: codex_default, model_line: gpt-5.6-codex, model_family: gpt, provider: codex}
  pi_gpt56_codex: {agent_runtime: pi_default, model_line: gpt-5.6-codex, model_family: gpt, provider: codex}
  opencode_gpt56: {agent_runtime: opencode_default, model_line: gpt-5.6-codex, model_family: gpt, provider: codex}
presets:
  codex_any:
    selection: ordered
    leaves:
      main:
        candidates: [{variant: codex_gpt56}, {variant: pi_gpt56_codex}, {variant: opencode_gpt56}]
`

// mixedExhaustedYAML is session-launch-mixed-exhausted.json's declaration:
// one leaf whose two candidates fall to two different kinds of cause, one
// static and one the caller's.
const mixedExhaustedYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [solo]
agent_runtimes:
  claude_default: {kind: claude, executable: claude}
  pi_default: {kind: pi, executable: pi}
launch_variants:
  claude_sonnet: {agent_runtime: claude_default, model_line: sonnet-5, model_family: claude}
  pi_gpt56_codex: {agent_runtime: pi_default, model_line: gpt-5.6-codex, model_family: gpt, provider: codex}
presets:
  build_and_verify:
    selection: ordered
    leaves:
      verifier:
        candidates: [{variant: claude_sonnet}, {variant: pi_gpt56_codex}]
`

// explicitHostYAML is session-launch-explicit-host.json's declaration: a
// document that switches the tmux kind off, resolved against a `--host tmux`
// the operator typed anyway.
const explicitHostYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
  kinds:
    tmux: {enabled: false}
agent_runtimes:
  claude_default: {kind: claude, executable: claude}
  pi_default: {kind: pi, executable: pi}
launch_variants:
  claude_sonnet: {agent_runtime: claude_default, model_line: sonnet-5, model_family: claude}
  pi_gpt56: {agent_runtime: pi_default, model_line: gpt-5.6, model_family: gpt}
presets:
  review:
    selection: ordered
    leaves:
      main:
        candidates: [{variant: claude_sonnet}, {variant: pi_gpt56}]
`

const (
	fixtureWorkspacePath = "/work/example"
	herdrSocket          = "/run/user/1000/herdr/ws_new.sock"
	soloSocket           = "/run/user/1000/solo/ws_abc.sock"
)

// herdrAmbient materializes the deduction session-launch.json describes:
// the workspace is enrolled but unbound, so the correlation rung is
// consulted and yields nothing, and the pane's own Herdr variables win.
func herdrAmbient(t *testing.T, doc config.DocumentV3) materialize.Result {
	t.Helper()
	return fixtureMaterialization(t, materialize.Options{
		WorkspaceFlag: fixtureWorkspacePath,
		Policy:        doc.SessionHosts,
		Correlations:  fixedCorrelations{root: fixtureWorkspacePath, workspace: "ws_new"},
		LookupEnv: ambient(map[string]string{
			"HERDR_SOCKET_PATH": herdrSocket,
			"HERDR_SESSION":     "ws_new",
		}),
	})
}

// soloCorrelated materializes the deduction the two solo fixtures describe:
// a persisted workspace↔host correlation to a Solo instance. withAmbient
// puts a Herdr pane around it, which the correlation must then outrank.
func soloCorrelated(t *testing.T, doc config.DocumentV3, withAmbient bool) materialize.Result {
	t.Helper()
	env := map[string]string{}
	if withAmbient {
		env["HERDR_SOCKET_PATH"] = "/run/user/1000/herdr/ambient.sock"
	}
	return fixtureMaterialization(t, materialize.Options{
		WorkspaceFlag: fixtureWorkspacePath,
		Policy:        doc.SessionHosts,
		Correlations: fixedCorrelations{
			root:      fixtureWorkspacePath,
			workspace: "ws_abc",
			binding: &domain.HostBinding{
				Workspace:  "ws_abc",
				Kind:       "solo",
				Instance:   soloSocket,
				InstanceID: "hostinst_solo_ws_abc",
				Source:     domain.HostSourceWorkspaceCorrelation,
			},
			factID: "fact_corr_ws_abc_solo",
		},
		Providers: disabledProviders("codex"),
		LookupEnv: ambient(env),
	})
}

// --- successful results ---------------------------------------------------

// TestOrderedResolutionMatchesLaunchFixture resolves the `review` preset
// with no constraints and holds the ordinary result to
// session-launch.json's shape: one open leaf named reviewer, resolved to
// the first-preference candidate, carrying the composition the join minted
// and the host the deduction produced.
func TestOrderedResolutionMatchesLaunchFixture(t *testing.T) {
	doc := parseDoc(t, herdrAmbientYAML)
	r := newResolverOver(t, doc, herdrAmbient(t, doc), func(o *launch.Options) {
		o.NewID = fixedIDs("lrr_fixture_1")
		o.HostVersions = map[string]string{"herdr": "0.6.0"}
	})

	got := resultEnvelope(t, r, doc, launch.Request{Preset: "review"}, "req_fixture_launch")
	assertResultShape(t, got, fixture(t, "session-launch.json"))
}

// TestModelFamilyAvoidMatchesFixture is
// session-launch-model-family-avoid.json: an avoid on the third axis that
// the selected candidate does not match, so it relents nothing and the
// result's relented_avoids is empty. The determined leaf is the point —
// model_family is a real axis, and avoiding a family the leaf never
// declared changes nothing.
func TestModelFamilyAvoidMatchesFixture(t *testing.T) {
	doc := parseDoc(t, herdrAmbientYAML)
	r := newResolverOver(t, doc, herdrAmbient(t, doc), func(o *launch.Options) {
		o.NewID = fixedIDs("lrr_fixture_8")
		o.HostVersions = map[string]string{"herdr": "0.6.0"}
	})

	got := resultEnvelope(t, r, doc, launch.Request{
		Preset: "verify",
		Avoid:  []launch.Constraint{{Axis: launch.AxisModelFamily, Value: "gpt"}},
	}, "req_fixture_launch_model_family_avoid")
	assertResultShape(t, got, fixture(t, "session-launch-model-family-avoid.json"))
}

// TestDistinctModelFamilyMatchesFixture is
// session-launch-distinct-model-family.json: two determined leaves whose
// families already differ, with the relation declared and satisfied. The
// result names the relation, which is what tells a caller the pairing was
// enforced rather than coincidental.
func TestDistinctModelFamilyMatchesFixture(t *testing.T) {
	doc := parseDoc(t, herdrAmbientYAML)
	r := newResolverOver(t, doc, herdrAmbient(t, doc), func(o *launch.Options) {
		o.NewID = fixedIDs("lrr_fixture_7")
		o.HostVersions = map[string]string{"herdr": "0.6.0"}
	})

	got := resultEnvelope(t, r, doc, launch.Request{Preset: "builder_and_verifier"}, "req_fixture_launch_distinct_model_family")
	assertResultShape(t, got, fixture(t, "session-launch-distinct-model-family.json"))
}

// --- failures -------------------------------------------------------------

// TestExhaustionMatchesLaunchExhaustedFixture requires a model line no
// candidate declares and holds the failure to
// session-launch-exhausted.json: the invalid class, the
// launch.constraints_exhausted code, the safe retry advice, the no_effect
// effect, every candidate with its stable reason, the per-reason tally, the
// deduced host, and the explicit zero surviving assignments.
//
// It is also the causal split's negative case for the pointer set: nothing
// about the host is implicated, so the failure offers no way out that
// involves changing it.
func TestExhaustionMatchesLaunchExhaustedFixture(t *testing.T) {
	doc := parseDoc(t, exhaustedFixtureYAML)
	r := newResolverOver(t, doc, herdrAmbient(t, doc), func(o *launch.Options) {
		o.HostVersions = map[string]string{"herdr": "0.6.0"}
	})

	got := errEnvelope(t, r, launch.Request{
		Preset:  "review",
		Require: []launch.Constraint{{Axis: launch.AxisModelLine, Value: "claude-opus-4"}},
	}, "req_fixture_launch_exhausted")
	assertFailureShape(t, got, fixture(t, "session-launch-exhausted.json"))
}

// TestProviderDisabledMatchesFixture is the causal split's unavailable
// side, and the reason the split exists at all: a standing-disabled
// provider took out every candidate on the leaf, no caller constraint was
// involved, so the row is launch.no_eligible_candidate — class unavailable,
// an installation-and-state fact the same request cannot repair — and never
// launch.constraints_exhausted.
//
// It also pins the pointer set: `duo provider enable` is offered because a
// provider fact did the eliminating, and `duo workspace host rebind`
// because the host came from a correlation that outranked the pane the
// caller is standing in.
func TestProviderDisabledMatchesFixture(t *testing.T) {
	doc := parseDoc(t, providerDisabledYAML)
	r := newResolverOver(t, doc, soloCorrelated(t, doc, true), func(o *launch.Options) {
		o.HostVersions = map[string]string{"solo": "0.1.0"}
	})

	got := errEnvelope(t, r, launch.Request{Preset: "codex_any"}, "req_fixture_launch_provider_disabled")
	assertFailureShape(t, got, fixture(t, "session-launch-provider-disabled.json"))
}

// TestMixedExhaustionMatchesFixture is the causal split's mixed case: a
// standing-disabled provider eliminated one candidate and the caller's
// require eliminated the other. Because a require contributed, the row is
// launch.constraints_exhausted — the caller has something to change — and
// the tallies carry both reasons, so the disabled provider is not hidden by
// reporting the row the caller can act on.
func TestMixedExhaustionMatchesFixture(t *testing.T) {
	doc := parseDoc(t, mixedExhaustedYAML)
	r := newResolverOver(t, doc, soloCorrelated(t, doc, false), func(o *launch.Options) {
		o.HostVersions = map[string]string{"solo": "0.1.0"}
	})

	got := errEnvelope(t, r, launch.Request{
		Preset:  "build_and_verify",
		Require: []launch.Constraint{{Axis: launch.AxisModelFamily, Value: "gpt"}},
	}, "req_fixture_launch_mixed_exhausted")
	assertFailureShape(t, got, fixture(t, "session-launch-mixed-exhausted.json"))
}

// TestExplicitHostMatchesFixture is session-launch-explicit-host.json: an
// operator named a session-host kind the document switches off, and got the
// host they asked for rather than the one policy would have chosen.
//
// It is the one case hostKindDisabled can fire in — the policy-default rung
// already skips disabled kinds — and the message names the flag, because a
// person who typed `--host tmux` needs to be told that is what failed. The
// ambient Herdr pane the flag outranked is recorded beside it, which is
// what makes the override visible rather than silent.
func TestExplicitHostMatchesFixture(t *testing.T) {
	doc := parseDoc(t, explicitHostYAML)
	mat := fixtureMaterialization(t, materialize.Options{
		WorkspaceFlag: fixtureWorkspacePath,
		HostFlag:      "tmux:/run/tmux-1000/explicit",
		Policy:        doc.SessionHosts,
		Correlations:  fixedCorrelations{root: fixtureWorkspacePath, workspace: "ws_new"},
		LookupEnv: ambient(map[string]string{
			"HERDR_SOCKET_PATH": "/run/user/1000/herdr/ambient.sock",
		}),
	})
	if got := mat.Host().Source; string(got) != string(domain.HostSourceExplicitFlag) {
		t.Fatalf("host_source = %q, want %q", got, domain.HostSourceExplicitFlag)
	}
	r := newResolverOver(t, doc, mat, func(o *launch.Options) {
		o.HostVersions = map[string]string{"tmux": "3.5a"}
	})

	got := errEnvelope(t, r, launch.Request{Preset: "review"}, "req_fixture_launch_explicit_host")
	assertFailureShape(t, got, fixture(t, "session-launch-explicit-host.json"))
}

// TestHostUnresolvedEnvelopeMatchesFixture is session-launch-host-unresolved.json.
//
// The failure is materialization's, not the resolver's: no rung produced a
// host, so there was never anything to resolve against and NewResolver is
// never reached. The assertion is that the envelope a launch surface would
// render for it is the one the fixture fixes — the code, the unavailable
// class, the deduction trail with every rung's verdict, and the two
// pointers that are ways out of an unresolved host (enabling a provider is
// not one).
func TestHostUnresolvedEnvelopeMatchesFixture(t *testing.T) {
	_, err := materialize.Materialize(context.Background(), materialize.Options{
		WorkspaceFlag:   fixtureWorkspacePath,
		RequestedPreset: "review",
		Policy:          config.SessionHostPolicy{},
		Correlations:    fixedCorrelations{root: fixtureWorkspacePath, workspace: "ws_none"},
		LookupEnv:       ambient(nil),
		Now:             fixedClock(),
	})
	if err == nil {
		t.Fatal("materialization with no rung yielding succeeded, want launch.host_unresolved")
	}
	merr, ok := err.(*materialize.Error)
	if !ok {
		t.Fatalf("materialization returned %T, want *materialize.Error", err)
	}

	got := canonical(t, errorEnvelope{
		Schema:    "duo.external/v1",
		RequestID: "req_fixture_launch_host_unresolved",
		Operation: "session.launch",
		Error:     merr,
	})
	want := fixture(t, "session-launch-host-unresolved.json")

	gotErr, wantErr := errorObject(t, got), errorObject(t, want)
	for _, field := range []string{"class", "code", "effect", "message"} {
		if gotErr[field] != wantErr[field] {
			t.Errorf("error.%s = %v, want %v", field, gotErr[field], wantErr[field])
		}
	}
	if got, want := retryAction(gotErr), retryAction(wantErr); got != want {
		t.Errorf("retry.action = %q, want %q", got, want)
	}
	if got, want := trailShape(gotErr), trailShape(wantErr); !reflect.DeepEqual(got, want) {
		t.Errorf("deduction trail = %v, want %v", got, want)
	}
	if got, want := pointerKeys(gotErr), pointerKeys(wantErr); !reflect.DeepEqual(got, want) {
		t.Errorf("pointer set = %v, want %v", got, want)
	}
	keysPresent(t, "error.details", detailsOf(gotErr), detailsOf(wantErr))
}

// --- registry -------------------------------------------------------------

// TestErrorCodesAreRegistered holds every code this package raises to
// internal/registry's stable set, and the operation-specific ones to the
// session.launch descriptor. The registry is where a code's closed class
// lives; a code this package raised but nobody registered would be a code
// no projection could classify.
//
// config.variant_unresolved is duo-config-v3 step 13's add, and
// config.composition_unresolved stays in the table beside it: it is
// deprecated, not withdrawn, and clients must keep accepting it while
// duo.config/v2 documents remain loadable.
func TestErrorCodesAreRegistered(t *testing.T) {
	stable := registry.StableErrorCodes()
	classes := map[string]string{
		launch.CodePresetNotFound:        "not_found",
		launch.CodeInvalidRequest:        "invalid",
		launch.CodeVariantUnresolved:     "unavailable",
		launch.CodeCompositionUnresolved: "unavailable",
		launch.CodeConstraintsExhausted:  "invalid",
		launch.CodeNoEligibleCandidate:   "unavailable",
		launch.CodeInternalFailure:       "internal",
	}
	for code, want := range classes {
		got, ok := stable[code]
		if !ok {
			t.Errorf("code %q is not in the registered stable set", code)
			continue
		}
		if string(got) != want {
			t.Errorf("code %q has registered class %q, want %q", code, got, want)
		}
	}

	d, ok := registry.Lookup("session.launch")
	if !ok {
		t.Fatal("session.launch is not registered")
	}
	for _, code := range []string{
		launch.CodePresetNotFound,
		launch.CodeConstraintsExhausted,
		launch.CodeNoEligibleCandidate,
		launch.CodeVariantUnresolved,
		launch.CodeCompositionUnresolved,
	} {
		found := false
		for _, registered := range d.ErrorCodes {
			if registered == code {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("session.launch does not list operation-specific code %q", code)
		}
	}
}

// --- envelope builders ----------------------------------------------------

func resultEnvelope(t *testing.T, r *launch.Resolver, doc config.DocumentV3, req launch.Request, requestID string) map[string]any {
	t.Helper()
	res := resolveOK(t, r, req)
	// Placement is a launcher input (I-3). The resolver never stamps
	// target / target_source; the fixture comparison needs the pair the
	// launcher would emit, so apply it here the same way Launcher does.
	if err := launch.ApplyPlacement(res, "", doc.SessionHosts); err != nil {
		t.Fatalf("applying launch placement: %v", err)
	}
	report := res.Report()
	// The session ID is minted by the commit that creates the session in
	// the same transaction as the record; the resolver never invents one.
	report.SessionID = "ses_fixture"
	return canonical(t, envelope{
		Schema:    "duo.external/v1",
		RequestID: requestID,
		Operation: "session.launch",
		Result:    &report,
		Warnings:  []any{},
		Features:  []any{},
	})
}

func errEnvelope(t *testing.T, r *launch.Resolver, req launch.Request, requestID string) map[string]any {
	t.Helper()
	return canonical(t, errorEnvelope{
		Schema:    "duo.external/v1",
		RequestID: requestID,
		Operation: "session.launch",
		Error:     resolveErr(t, r, req),
	})
}

// --- shapes ---------------------------------------------------------------

// resultShape is the contract-relevant structure of a successful
// session.launch envelope: everything a client could branch on, with the
// minted identifiers left out.
type resultShape struct {
	Selection  string
	Leaves     []string
	HostKind   string
	HostSource string
	Outranked  []string
	Relations  []string
}

func shapeOfResult(t *testing.T, doc map[string]any) resultShape {
	t.Helper()
	result, ok := doc["result"].(map[string]any)
	if !ok {
		t.Fatalf("envelope carries no result object: %v", doc)
	}
	out := resultShape{Selection: str(result["selection"])}
	for _, raw := range slice(result["leaves"]) {
		leaf := obj(raw)
		comp := obj(leaf["composition"])
		out.Leaves = append(out.Leaves, fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s|%d",
			str(leaf["name"]), str(leaf["agent_runtime"]), str(leaf["model_line"]),
			str(leaf["model_family"]), str(comp["name"]), str(leaf["declared_kind"]),
			str(leaf["outcome"]), len(slice(leaf["relented_avoids"]))))
	}
	host := obj(result["host"])
	out.HostKind, out.HostSource = str(host["kind"]), str(host["host_source"])
	out.Outranked = outrankedShape(host)
	for _, raw := range slice(result["relations"]) {
		rel := obj(raw)
		out.Relations = append(out.Relations, fmt.Sprintf("%s|%v", str(rel["kind"]), rel["leaves"]))
	}
	return out
}

func assertResultShape(t *testing.T, got, want map[string]any) {
	t.Helper()
	if g, w := shapeOfResult(t, got), shapeOfResult(t, want); !reflect.DeepEqual(g, w) {
		t.Errorf("session.launch result shape\n got: %+v\nwant: %+v", g, w)
	}
	keysPresent(t, "result", obj(got["result"]), obj(want["result"]))
	for i, raw := range slice(obj(want["result"])["leaves"]) {
		gotLeaves := slice(obj(got["result"])["leaves"])
		if i >= len(gotLeaves) {
			break
		}
		keysPresent(t, fmt.Sprintf("result.leaves[%d]", i), obj(gotLeaves[i]), obj(raw))
	}
}

// failureShape is the contract-relevant structure of a session.launch
// failure envelope.
//
// Tallies are aggregated by (leaf, reason) before comparison, and their
// `detail` is left out. Step 03's fixtures are inconsistent on both points
// — session-launch-explicit-host.json writes one aggregated row of count 2
// while session-launch-provider-disabled.json writes three rows of count 1,
// and their details are variant names in one and a phrase in the other.
// What both spellings agree on, and what a client can act on, is how many
// candidates each leaf lost to each reason. This build always emits the
// aggregated row.
type failureShape struct {
	Class               string
	Code                string
	Effect              string
	Message             string
	RetrySafe           bool
	RetryAction         string
	Candidates          []string
	Tallies             []string
	HostKind            string
	HostSource          string
	Outranked           []string
	Pointers            []string
	Evidence            []string
	CompleteAssignments float64
}

func shapeOfFailure(t *testing.T, doc map[string]any) failureShape {
	t.Helper()
	e := errorObject(t, doc)
	details := detailsOf(e)
	out := failureShape{
		Class:       str(e["class"]),
		Code:        str(e["code"]),
		Effect:      str(e["effect"]),
		Message:     str(e["message"]),
		RetryAction: retryAction(e),
		Pointers:    pointerKeys(e),
	}
	if retry, ok := e["retry"].(map[string]any); ok {
		out.RetrySafe, _ = retry["safe"].(bool)
	}
	for _, raw := range slice(details["candidates"]) {
		c := obj(raw)
		out.Candidates = append(out.Candidates, fmt.Sprintf("%s|%s|%s|%s|%s|%s|%s",
			str(c["leaf"]), str(c["declaration_locator"]), str(c["variant"]),
			str(c["agent_runtime"]), str(c["model_line"]), str(c["model_family"]),
			str(c["elimination_reason"])))
	}
	totals := map[string]float64{}
	for _, raw := range slice(details["elimination_tallies"]) {
		tally := obj(raw)
		key := str(tally["leaf"]) + "|" + str(tally["reason"])
		count, _ := tally["count"].(float64)
		totals[key] += count
	}
	for key, count := range totals {
		out.Tallies = append(out.Tallies, fmt.Sprintf("%s|%v", key, count))
	}
	sort.Strings(out.Tallies)

	host := obj(details["host"])
	out.HostKind, out.HostSource = str(host["kind"]), str(host["host_source"])
	out.Outranked = outrankedShape(host)
	out.Evidence = sortedKeys(obj(details["evidence_bundle"]))
	out.CompleteAssignments, _ = details["complete_assignments_survived"].(float64)
	return out
}

func assertFailureShape(t *testing.T, got, want map[string]any) {
	t.Helper()
	if g, w := shapeOfFailure(t, got), shapeOfFailure(t, want); !reflect.DeepEqual(g, w) {
		t.Errorf("session.launch failure shape\n got: %+v\nwant: %+v", g, w)
	}
	keysPresent(t, "error.details", detailsOf(errorObject(t, got)), detailsOf(errorObject(t, want)))
}

// keysPresent asserts that every key the fixture declares at this level is
// one this build emits. It is the guard the shape comparison cannot be: a
// shape names the fields it knows about, and a payload that silently
// dropped a field nobody thought to shape would pass it.
//
// The converse is deliberately not asserted. This build emits keys step
// 03's fixtures do not — `survivors` and `consulted_record_digests`, the
// handoff-18 payload carried forward — and `launch_failure_details` is
// `additionalProperties: true`, so those are compatible carries rather than
// divergences.
func keysPresent(t *testing.T, label string, got, want map[string]any) {
	t.Helper()
	for key := range want {
		if _, ok := got[key]; !ok {
			t.Errorf("%s is missing key %q, which the fixture declares", label, key)
		}
	}
}

// --- fixture accessors ----------------------------------------------------

func errorObject(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()
	e, ok := doc["error"].(map[string]any)
	if !ok {
		t.Fatalf("envelope carries no error object: %v", doc)
	}
	return e
}

func detailsOf(e map[string]any) map[string]any { return obj(e["details"]) }

func retryAction(e map[string]any) string { return str(obj(e["retry"])["action"]) }

func pointerKeys(e map[string]any) []string {
	return sortedKeys(obj(detailsOf(e)["pointers"]))
}

func trailShape(e map[string]any) []string {
	var out []string
	for _, raw := range slice(detailsOf(e)["deduction_trail"]) {
		rung := obj(raw)
		out = append(out, fmt.Sprintf("%s|%v|%v",
			str(rung["source"]), rung["consulted"] == true, rung["yielded_host"] == true))
	}
	return out
}

func outrankedShape(host map[string]any) []string {
	var out []string
	for _, raw := range slice(host["outranked_evidence"]) {
		e := obj(raw)
		out = append(out, fmt.Sprintf("%s|%s|%d",
			str(e["source"]), str(e["kind"]), len(slice(e["captures"]))))
	}
	return out
}

func obj(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func slice(v any) []any {
	s, _ := v.([]any)
	return s
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func sortedKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
