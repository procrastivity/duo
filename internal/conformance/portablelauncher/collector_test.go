package portablelauncher

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCollectorBuildsCanonicalCompleteOrderAndCleanupLast(t *testing.T) {
	scenario := CanonicalScenario()
	result, _ := CollectResult(collectorTestInput(t))

	if len(result.Stages) != len(scenario.Steps) {
		t.Fatalf("stage count = %d, want %d", len(result.Stages), len(scenario.Steps))
	}
	cleanupCount := 0
	for i, step := range scenario.Steps {
		got := result.Stages[i]
		if got.Sequence != step.Sequence || got.Stage != step.Stage || got.Case != step.Case {
			t.Fatalf("stage %d = %d/%s/%s, want %d/%s/%s", i, got.Sequence, got.Case, got.Stage, step.Sequence, step.Case, step.Stage)
		}
		if got.Stage == "cleanup" {
			cleanupCount++
		}
	}
	last := result.Stages[len(result.Stages)-1]
	if cleanupCount != 1 || last.Stage != "cleanup" || last.Case != "run" {
		t.Fatalf("cleanup count/last = %d, %s/%s", cleanupCount, last.Case, last.Stage)
	}
}

func TestCollectorPropagatesFirstFailureWithoutSemanticSuccesses(t *testing.T) {
	input := collectorTestInput(t)
	failureIndex := 4 // happy/bind, independently selected from the manifest order.
	input.Stages[failureIndex].Failure = &StageError{
		Code: "fixture.bind_failed", Message: "bind checkpoint failed", Effect: "no_effect",
		Retry: Retry{Safe: false, Action: "inspect_bind"},
	}

	result, captured := CollectResult(input)
	origin := result.Stages[failureIndex]
	if origin.Verdict != "fail" || origin.Error == nil || origin.Error.Code != "fixture.bind_failed" {
		t.Fatalf("origin = %#v", origin)
	}
	if len(origin.Evidence) != 2 {
		t.Fatalf("origin evidence = %#v, want captured observations plus failure checkpoint", origin.Evidence)
	}
	assertCollectorObservation(t, captured, origin.Evidence[0], origin.Sequence, CanonicalScenario().Steps[failureIndex].Assertions[0].ID)
	for i := failureIndex + 1; i < len(result.Stages); i++ {
		stage := result.Stages[i]
		if stage.Stage == "cleanup" {
			continue
		}
		if stage.Case == "blocked" && stage.Stage == "launch" {
			if stage.Error == nil || stage.Error.Code != "prerequisite.blocked_induction_unavailable" {
				t.Fatalf("canonical blocked failure was not preserved: %#v", stage)
			}
			continue
		}
		if stage.Verdict != "fail" || stage.Outcome != "error" || stage.Error == nil || stage.Error.Code != "prerequisite.not_reached" {
			t.Fatalf("stage %d did not fail closed: %#v", i, stage)
		}
		if len(stage.Assertions) != 1 || stage.Assertions[0].ID != "prerequisite.not_reached" || stage.Assertions[0].Matched {
			t.Fatalf("stage %d fabricated or retained semantic assertions: %#v", i, stage.Assertions)
		}
		actual := stage.Assertions[0].Actual
		wantOrigin := origin
		if stage.Case == "blocked" {
			blockedOrigin := result.Stages[collectorStageIndex("blocked", "launch")]
			wantOrigin = blockedOrigin
		}
		if actual["origin_sequence"] != float64(wantOrigin.Sequence) || actual["origin_stage"] != wantOrigin.Stage || actual["origin_case"] != wantOrigin.Case {
			t.Fatalf("stage %d origin = %#v", i, actual)
		}
		assertCollectorEvidence(t, captured, stage, "prerequisite.not_reached", actual)
	}
	last := result.Stages[len(result.Stages)-1]
	if last.Stage != "cleanup" || last.Verdict != "pass" || last.Error != nil {
		t.Fatalf("independently executed cleanup did not pass last: %#v", last)
	}
}

func TestCollectorMandatoryBlockedPrerequisitePreventsPass(t *testing.T) {
	result, captured := CollectResult(collectorTestInput(t))
	index := collectorStageIndex("blocked", "launch")
	stage := result.Stages[index]

	wantError := StageError{
		Code: "prerequisite.blocked_induction_unavailable", Message: "supported admitted-then-blocked evidence is unavailable",
		Effect: "no_effect", Retry: Retry{Safe: false, Action: "add_supported_blocked_evidence"},
	}
	if stage.Verdict != "fail" || stage.Outcome != "error" || stage.Error == nil || !reflect.DeepEqual(*stage.Error, wantError) {
		t.Fatalf("blocked stage = %#v, want stable unavailable metadata %#v", stage, wantError)
	}
	if len(stage.Assertions) != 1 || stage.Assertions[0].ID != "blocked.induction_available" || stage.Assertions[0].Actual["available"] != false {
		t.Fatalf("blocked assertion = %#v", stage.Assertions)
	}
	assertCollectorEvidence(t, captured, stage, "blocked.induction_available", map[string]any{"available": false})
	if result.Summary.Verdict != "fail" || result.Summary.FirstFailedStage == nil || *result.Summary.FirstFailedStage != "launch" || result.Summary.FirstFailedCase == nil || *result.Summary.FirstFailedCase != "blocked" {
		t.Fatalf("summary = %#v", result.Summary)
	}
	for _, caseName := range []string{"exited", "timeout"} {
		for i, step := range CanonicalScenario().Steps {
			if step.Case == caseName && result.Stages[i].Verdict != "pass" {
				t.Fatalf("case-local blocked prerequisite suppressed %s stage %d: %#v", caseName, i, result.Stages[i])
			}
		}
	}
}

func TestCollectorRawLauncherEventsCannotOverrideAuthoritativeEvidence(t *testing.T) {
	input := collectorTestInput(t)
	input.Record.Capture.Events = []byte(`{"summary":{"verdict":"pass"},"stages":[{"verdict":"pass","outcome":"success"}]}`)

	// The independently captured setup measurement disagrees with the signed
	// scenario. The expected value below comes from CanonicalScenario, not from
	// collector output or the raw launcher event bytes above.
	semantic := &input.Stages[0].Observations[1]
	semantic.Evidence["measurements"].(map[string]any)["shared_state"] = true
	wantExpected := CanonicalScenario().Steps[0].Assertions[1].Expected

	result, captured := CollectResult(input)
	stage := result.Stages[0]
	if stage.Verdict != "fail" || result.Summary.Verdict != "fail" {
		t.Fatalf("launcher raw pass overrode common evidence: stage=%#v summary=%#v", stage, result.Summary)
	}
	if len(stage.Assertions) != 2 {
		t.Fatalf("setup assertions = %#v", stage.Assertions)
	}
	got := stage.Assertions[1]
	if !reflect.DeepEqual(got.Expected, wantExpected) || got.Actual["shared_state"] != true || got.Matched {
		t.Fatalf("authoritative assertion = %#v, canonical expected = %#v", got, wantExpected)
	}
	assertCollectorEvidence(t, captured, stage, got.ID, got.Actual)

	for i := 1; i < len(result.Stages); i++ {
		if result.Stages[i].Stage == "cleanup" {
			continue
		}
		if result.Stages[i].Case == "blocked" && result.Stages[i].Stage == "launch" {
			if result.Stages[i].Error == nil || result.Stages[i].Error.Code != "prerequisite.blocked_induction_unavailable" {
				t.Fatalf("canonical blocked failure was not preserved: %#v", result.Stages[i])
			}
			continue
		}
		if result.Stages[i].Verdict != "fail" || result.Stages[i].Assertions[0].ID != "prerequisite.not_reached" {
			t.Fatalf("stage %d was turned into pass by raw launcher events: %#v", i, result.Stages[i])
		}
	}
}

func TestCollectorMissingCaptureFailsClosedWithCompleteSummary(t *testing.T) {
	input := collectorTestInput(t)
	input.Stages = input.Stages[:3]
	input.Record.Capture.Events = []byte(`{"summary":{"verdict":"pass"}}`)
	result, _ := CollectResult(input)

	want := CanonicalScenario().Steps[3]
	first := result.Stages[3]
	if first.Stage != want.Stage || first.Case != want.Case || first.Error == nil || first.Error.Code != "collector.stage_missing" {
		t.Fatalf("first missing stage = %#v, want %s/%s", first, want.Case, want.Stage)
	}
	if result.Summary.FirstFailedStage == nil || *result.Summary.FirstFailedStage != want.Stage || result.Summary.FirstFailedCase == nil || *result.Summary.FirstFailedCase != want.Case {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if len(result.Stages) != len(CanonicalScenario().Steps) {
		t.Fatalf("incomplete result has %d stages", len(result.Stages))
	}
}

func collectorTestInput(t *testing.T) CollectorInput {
	t.Helper()
	scenario := CanonicalScenario()
	pins := fixturePins()
	input := CollectorInput{
		Run:  RunIdentity{RunID: "collector-test", ObservedAt: "2026-09-18T00:00:00Z", HostOS: "linux", HostArch: "x86_64", FixtureRoot: "$RUN"},
		Pins: pins,
	}
	var offset int64
	for _, step := range scenario.Steps {
		duration := int64(1)
		if step.Case == "timeout" && step.Stage == "observe" {
			duration = 20_000
		}
		capture := CommonStageCapture{Sequence: step.Sequence, StartedOffsetMS: offset, DurationMS: duration}
		for _, expected := range step.Assertions {
			actual := collectorCloneMap(t, expected.Expected)
			evidence := map[string]any{
				"kind": "controller.checkpoint", "monotonic_ms": float64(offset), "measurements": actual,
			}
			if expected.ID == "stage.completed_within_deadline" {
				delete(evidence, "measurements")
				deadline := step.DeadlineMS
				if step.Case == "timeout" && step.Stage == "observe" {
					deadline = CanonicalOracle().TimeoutMaximumMS
				}
				evidence["started_ms"] = float64(offset)
				evidence["finished_ms"] = float64(offset + duration)
				evidence["deadline_ms"] = float64(deadline)
			}
			if expected.ID == "setup.fixture_isolated_and_pinned" {
				pinBytes, err := json.Marshal(pins)
				if err != nil {
					t.Fatal(err)
				}
				evidence["pins_digest"] = Digest(pinBytes)
				evidence["credentials_present"] = true
				evidence["credential_provider"] = "openai-codex"
				evidence["credential_source_kind"] = "operator_copy"
				evidence["pi_no_extensions"] = true
			}
			if expected.ID == "observe.exited" || expected.ID == "observe.timeout" {
				evidence["target_verified"] = true
			}
			if expected.ID == "observe.timeout" {
				evidence["timeout_probe_supported"] = true
			}
			capture.Observations = append(capture.Observations, Observation{
				Sequence: step.Sequence, AssertionID: expected.ID, Source: "controller", Evidence: evidence,
			})
		}
		input.Stages = append(input.Stages, capture)
		offset += duration
	}
	return input
}

func collectorStageIndex(caseName, stageName string) int {
	for i, step := range CanonicalScenario().Steps {
		if step.Case == caseName && step.Stage == stageName {
			return i
		}
	}
	return -1
}

func assertCollectorEvidence(t *testing.T, capture *Capture, stage StageResult, assertionID string, actual map[string]any) {
	t.Helper()
	if len(stage.Evidence) != 1 {
		t.Fatalf("evidence refs = %#v", stage.Evidence)
	}
	b, ok := capture.Blob(stage.Evidence[0])
	if !ok {
		t.Fatalf("missing captured blob %q", stage.Evidence[0])
	}
	var evidence Evidence
	if err := json.Unmarshal(b, &evidence); err != nil {
		t.Fatal(err)
	}
	for _, observation := range evidence.Observations {
		if observation.Sequence != stage.Sequence || observation.AssertionID != assertionID {
			continue
		}
		measurements, _ := observation.Evidence["measurements"].(map[string]any)
		if !reflect.DeepEqual(measurements, actual) {
			t.Fatalf("captured measurements = %#v, want %#v", measurements, actual)
		}
		return
	}
	t.Fatalf("no observation for sequence %d assertion %q", stage.Sequence, assertionID)
}

func assertCollectorObservation(t *testing.T, capture *Capture, reference string, sequence int, assertionID string) {
	t.Helper()
	b, ok := capture.Blob(reference)
	if !ok {
		t.Fatalf("missing captured blob %q", reference)
	}
	var evidence Evidence
	if err := json.Unmarshal(b, &evidence); err != nil {
		t.Fatal(err)
	}
	for _, observation := range evidence.Observations {
		if observation.Sequence == sequence && observation.AssertionID == assertionID {
			return
		}
	}
	t.Fatalf("no preserved observation for sequence %d assertion %q", sequence, assertionID)
}

func collectorCloneMap(t *testing.T, in map[string]any) map[string]any {
	t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
