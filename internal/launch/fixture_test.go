package launch_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/registry"
)

// envelope is the duo.external/v1 operation envelope the projection layer
// wraps a result or an error in. It lives in the test, not in the launch
// package: internal/launch owns the launch semantics, and the envelope
// belongs to whoever projects an operation onto a wire (Step 21's CLI). The
// test builds it so that fixture conformance can be an equality check over
// the whole document rather than a field-by-field inspection of a fragment.
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
	Schema    string        `json:"schema"`
	RequestID string        `json:"request_id"`
	Operation string        `json:"operation"`
	Error     *launch.Error `json:"error"`
}

// fixture decodes one embedded contract fixture.
func fixture(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := contracts.FS.ReadFile(path)
	if err != nil {
		t.Fatalf("reading embedded fixture %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decoding %s: %v", path, err)
	}
	return out
}

// TestOrderedResolutionMatchesLaunchFixture resolves §6.5's declared
// `review` preset with no constraints and asserts the ordinary result is,
// byte for byte after a JSON round trip,
// contracts/fixtures/duo-external-v1/session-launch.json.
//
// That fixture is the normative shape of a successful session.launch: one
// open leaf named reviewer, resolved to the first-preference candidate
// (codex on gpt-5.6), with an empty relented-avoids list because no avoid
// was requested.
func TestOrderedResolutionMatchesLaunchFixture(t *testing.T) {
	r := newResolver(t, scenarioYAML, func(o *launch.Options) {
		o.NewID = fixedIDs("lrr_fixture_1")
	})

	res := resolveOK(t, r, launch.Request{Preset: "review"})
	report := res.Report()
	// The session ID is minted by the commit that creates the session in
	// the same transaction as the record; the resolver never invents one.
	report.SessionID = "ses_fixture_1"

	got := canonical(t, envelope{
		Schema:    "duo.external/v1",
		RequestID: "req_fixture_launch",
		Operation: "session.launch",
		Result:    &report,
		Warnings:  []any{},
		Features:  []any{},
	})
	want := fixture(t, "fixtures/duo-external-v1/session-launch.json")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("session.launch result envelope does not match the fixture\n got: %#v\nwant: %#v", got, want)
	}
}

// TestExhaustionMatchesLaunchExhaustedFixture requires a model line no
// candidate declares and asserts the failure is, byte for byte after a JSON
// round trip, contracts/fixtures/duo-external-v1/session-launch-exhausted.json.
//
// The fixture pins every part of §6.8's exhaustion row at once: the invalid
// class, the launch.constraints_exhausted code, the safe retry advice, the
// no_effect effect, the normalized constraints, every candidate with its
// stable elimination reason, the four survivor pools, and the explicit zero
// surviving assignments.
func TestExhaustionMatchesLaunchExhaustedFixture(t *testing.T) {
	r := newResolver(t, exhaustionYAML)

	err := resolveErr(t, r, launch.Request{
		Preset: "review",
		Require: []launch.Constraint{
			{Axis: launch.AxisModelLine, Value: "claude-opus-4", Source: "flag"},
		},
	})

	got := canonical(t, errorEnvelope{
		Schema:    "duo.external/v1",
		RequestID: "req_fixture_launch_exhausted",
		Operation: "session.launch",
		Error:     err,
	})
	want := fixture(t, "fixtures/duo-external-v1/session-launch-exhausted.json")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("session.launch error envelope does not match the fixture\n got: %#v\nwant: %#v", got, want)
	}
}

// TestErrorCodesAreRegistered holds every code this package raises to
// internal/registry's stable set, and the operation-specific ones to the
// session.launch descriptor. The registry is where a code's closed class
// lives; a code this package raised but nobody registered would be a code
// no projection could classify.
func TestErrorCodesAreRegistered(t *testing.T) {
	stable := registry.StableErrorCodes()
	classes := map[string]string{
		launch.CodePresetNotFound:        "not_found",
		launch.CodeInvalidRequest:        "invalid",
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
