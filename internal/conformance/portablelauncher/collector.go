package portablelauncher

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// CommonStageCapture is the launcher-neutral input for one stage. Observations
// are raw, independently captured facts; they cannot carry expectations or a
// verdict. Sequence selects the corresponding canonical scenario step.
type CommonStageCapture struct {
	Sequence        int
	StartedOffsetMS int64
	DurationMS      int64
	Observations    []Observation
	Failure         *StageError
}

// CollectorInput contains common-owned run metadata and stage captures. The
// RunRecord is retained so orchestration and cleanup failures cannot be hidden.
// Raw launcher events and stderr are diagnostic capture, never result input.
type CollectorInput struct {
	Run    RunIdentity
	Pins   Pins
	Record RunRecord
	Stages []CommonStageCapture
}

// CollectResult constructs the authoritative result and its in-memory,
// content-addressed evidence. It always returns the complete canonical stage
// list. The caller may persist the returned Capture in a later bundle-writing
// step.
func CollectResult(input CollectorInput) (Result, *Capture) {
	scenario := CanonicalScenario()
	scenarioJSON, err := ScenarioJSON(scenario)
	if err != nil {
		panic(fmt.Sprintf("portable launcher collector: canonical scenario: %v", err))
	}

	result := Result{
		Schema: ResultSchema,
		Suite: SuiteIdentity{
			Name: ScenarioName, Revision: scenario.Revision,
			ManifestDigest: Digest(scenarioJSON), OracleDigest: OracleDigest(),
		},
		Run: input.Run, Pins: input.Pins,
		Stages: make([]StageResult, len(scenario.Steps)),
		Scrub:  ScrubRecord{Status: "pass", Policy: ScrubPolicy, Findings: []string{}},
	}

	// Build identities and timings before deriving observations: the oracle's
	// deadline derivation checks these authoritative stage fields.
	var priorEnd int64
	for i, step := range scenario.Steps {
		started, duration := priorEnd, int64(0)
		if i < len(input.Stages) && input.Stages[i].Sequence == step.Sequence {
			started = input.Stages[i].StartedOffsetMS
			duration = input.Stages[i].DurationMS
		}
		if started < priorEnd || duration < 0 {
			started, duration = priorEnd, 0
		}
		result.Stages[i] = StageResult{
			Sequence: step.Sequence, Stage: step.Stage, Case: step.Case,
			StartedOffsetMS: started, DurationMS: duration,
		}
		priorEnd = started + duration
	}

	capture := NewCapture()
	var firstFailure *failureOrigin
	var origin *failureOrigin
	rememberFailure := func(stage StageResult) {
		origin = newFailureOrigin(stage)
		if firstFailure == nil {
			firstFailure = origin
		}
	}
	for i, step := range scenario.Steps {
		stage := &result.Stages[i]
		if origin != nil && origin.Case == "blocked" && step.Case != "blocked" {
			origin = nil
		}
		if origin != nil && step.Stage != "cleanup" {
			stage.StartedOffsetMS = result.Stages[i-1].StartedOffsetMS + result.Stages[i-1].DurationMS
			stage.DurationMS = 0
			setNotReached(stage, *origin, capture, &result.Scrub)
			continue
		}

		if err := recordFailureAt(input.Record, i, len(scenario.Steps)); err != nil {
			setFailed(stage, collectorError("collector.run_failed", err.Error()), capture, &result.Scrub)
			rememberFailure(*stage)
			continue
		}

		if blockedPrerequisiteUnavailable() && step.Case == "blocked" && step.Stage == "launch" {
			setBlockedUnavailable(stage, capture, &result.Scrub)
			rememberFailure(*stage)
			continue
		}

		if i >= len(input.Stages) {
			setFailed(stage, collectorError("collector.stage_missing", "required common stage capture is missing"), capture, &result.Scrub)
			rememberFailure(*stage)
			continue
		}
		common := input.Stages[i]
		if common.Sequence != step.Sequence {
			message := fmt.Sprintf("common stage capture sequence %d does not match required sequence %d", common.Sequence, step.Sequence)
			setFailed(stage, collectorError("collector.stage_invalid", message), capture, &result.Scrub)
			rememberFailure(*stage)
			continue
		}

		ref, evidenceErr := addStageEvidence(capture, common.Observations)
		if evidenceErr != nil {
			result.Scrub.Status = "fail"
			result.Scrub.Findings = append(result.Scrub.Findings, evidenceErr.Error())
			setFailed(stage, collectorError("collector.evidence_rejected", "independent stage evidence failed capture policy"), capture, &result.Scrub)
			rememberFailure(*stage)
			continue
		}
		stage.Evidence = []string{ref}

		if common.Failure != nil {
			stage.Verdict, stage.Outcome = "fail", "error"
			stage.Assertions, _ = collectAssertions(result, step, common.Observations)
			stage.Error = cloneStageError(common.Failure)
			if stage.Error.Code == "" || stage.Error.Message == "" || stage.Error.Effect == "" || stage.Error.Retry.Action == "" {
				stage.Error = collectorError("collector.stage_failed", "common stage reported an incomplete failure")
			}
			setFailureAssertion(stage, capture, &result.Scrub)
			rememberFailure(*stage)
			continue
		}

		assertions, assertionErr := collectAssertions(result, step, common.Observations)
		stage.Assertions = assertions
		if assertionErr != nil {
			stage.Verdict, stage.Outcome = "fail", "error"
			stage.Error = collectorError("collector.invalid_stage_evidence", assertionErr.Error())
			if len(stage.Assertions) == 0 {
				setFailureAssertion(stage, capture, &result.Scrub)
			}
			rememberFailure(*stage)
			continue
		}
		stage.Verdict, stage.Outcome = "pass", step.Outcome
	}

	// An extra capture has no canonical identity. Fail the final canonical
	// checkpoint rather than append a launcher-defined stage.
	if len(input.Stages) > len(scenario.Steps) {
		last := &result.Stages[len(result.Stages)-1]
		setFailed(last, collectorError("collector.unexpected_stage", "common capture contains a stage outside the canonical scenario"), capture, &result.Scrub)
		rememberFailure(*last)
	}

	if firstFailure == nil {
		result.Summary = Summary{Verdict: "pass"}
	} else {
		result.Summary = Summary{
			Verdict: "fail", FirstFailedStage: stringPointerValue(firstFailure.Stage), FirstFailedCase: stringPointerValue(firstFailure.Case),
		}
	}
	return result, capture
}

type failureOrigin struct {
	Sequence int
	Stage    string
	Case     string
	Evidence []string
}

func newFailureOrigin(stage StageResult) *failureOrigin {
	return &failureOrigin{Sequence: stage.Sequence, Stage: stage.Stage, Case: stage.Case, Evidence: append([]string(nil), stage.Evidence...)}
}

func collectAssertions(result Result, step StepSpec, observations []Observation) ([]Assertion, error) {
	byID := make(map[string][]Observation, len(observations))
	for _, observation := range observations {
		if observation.Sequence != step.Sequence {
			return nil, fmt.Errorf("observation sequence %d does not match stage sequence %d", observation.Sequence, step.Sequence)
		}
		byID[observation.AssertionID] = append(byID[observation.AssertionID], observation)
	}

	assertions := make([]Assertion, 0, len(step.Assertions))
	for _, expected := range step.Assertions {
		matches := byID[expected.ID]
		if len(matches) != 1 {
			return assertions, fmt.Errorf("assertion %q has %d independent observations, want 1", expected.ID, len(matches))
		}
		observation := matches[0]
		if observation.Source == "launcher_result" || observation.Source == "launcher_final_text" || observation.Source == "terminal" {
			return assertions, fmt.Errorf("assertion %q uses untrusted source %q", expected.ID, observation.Source)
		}
		var problems Problems
		actual := deriveObservationActual(result, observation, "collector."+expected.ID, &problems)
		if err := problems.Err(); err != nil {
			return assertions, err
		}
		assertion := Assertion{ID: expected.ID, Expected: cloneValueMap(expected.Expected), Actual: cloneValueMap(actual)}
		assertion.Matched = reflect.DeepEqual(assertion.Expected, assertion.Actual)
		assertions = append(assertions, assertion)
		if !assertion.Matched {
			return assertions, fmt.Errorf("assertion %q did not match canonical expectation", expected.ID)
		}
		delete(byID, expected.ID)
	}
	if len(byID) != 0 {
		return assertions, fmt.Errorf("stage contains observations outside its canonical assertion set")
	}
	return assertions, nil
}

func addStageEvidence(capture *Capture, observations []Observation) (string, error) {
	evidence := Evidence{Schema: EvidenceSchema, ScrubStatus: "pass", Observations: cloneObservations(observations)}
	return capture.AddJSON("common_collector", evidence)
}

func setBlockedUnavailable(stage *StageResult, capture *Capture, scrub *ScrubRecord) {
	stage.Verdict, stage.Outcome = "fail", "error"
	stage.Error = &StageError{
		Code: "prerequisite.blocked_induction_unavailable", Message: "supported admitted-then-blocked evidence is unavailable",
		Effect: "no_effect", Retry: Retry{Safe: false, Action: "add_supported_blocked_evidence"},
	}
	stage.Assertions = []Assertion{{
		ID: "blocked.induction_available", Expected: map[string]any{"available": true},
		Actual: map[string]any{"available": false}, Matched: false,
	}}
	setGeneratedEvidence(stage, stage.Assertions[0], nil, capture, scrub)
}

func setFailed(stage *StageResult, stageError *StageError, capture *Capture, scrub *ScrubRecord) {
	stage.Verdict, stage.Outcome, stage.Error = "fail", "error", stageError
	setFailureAssertion(stage, capture, scrub)
}

func setFailureAssertion(stage *StageResult, capture *Capture, scrub *ScrubRecord) {
	assertion := Assertion{ID: "stage.failure", Expected: map[string]any{"success": true}, Actual: map[string]any{"success": false}, Matched: false}
	stage.Assertions = append(stage.Assertions, assertion)
	setGeneratedEvidence(stage, assertion, nil, capture, scrub)
}

func setNotReached(stage *StageResult, origin failureOrigin, capture *Capture, scrub *ScrubRecord) {
	actual := map[string]any{
		"reached": false, "origin_sequence": float64(origin.Sequence), "origin_stage": origin.Stage, "origin_case": origin.Case,
	}
	assertion := Assertion{ID: "prerequisite.not_reached", Expected: map[string]any{"reached": true}, Actual: actual, Matched: false}
	stage.Verdict, stage.Outcome = "fail", "error"
	stage.Error = &StageError{
		Code:    "prerequisite.not_reached",
		Message: fmt.Sprintf("required stage was not reached after %s/%s checkpoint failed", origin.Case, origin.Stage),
		Effect:  "no_effect", Retry: Retry{Safe: false, Action: "inspect_originating_failure"},
	}
	stage.Assertions = []Assertion{assertion}
	setGeneratedEvidence(stage, assertion, &origin, capture, scrub)
}

func setGeneratedEvidence(stage *StageResult, assertion Assertion, origin *failureOrigin, capture *Capture, scrub *ScrubRecord) {
	checkpoint := map[string]any{
		"kind": "controller.checkpoint", "monotonic_ms": float64(stage.StartedOffsetMS),
		"measurements": cloneValueMap(assertion.Actual),
	}
	if origin != nil {
		checkpoint["origin_sequence"] = float64(origin.Sequence)
		checkpoint["origin_stage"] = origin.Stage
		checkpoint["origin_case"] = origin.Case
		checkpoint["origin_evidence"] = append([]string(nil), origin.Evidence...)
	}
	ref, err := addStageEvidence(capture, []Observation{{
		Sequence: stage.Sequence, AssertionID: assertion.ID, Source: "controller", Evidence: checkpoint,
	}})
	if err != nil {
		scrub.Status = "fail"
		scrub.Findings = append(scrub.Findings, err.Error())
		if len(stage.Evidence) == 0 {
			stage.Evidence = append([]string(nil), originEvidence(origin)...)
		}
		return
	}
	stage.Evidence = append(stage.Evidence, ref)
}

func originEvidence(origin *failureOrigin) []string {
	if origin == nil {
		return nil
	}
	return origin.Evidence
}

func recordFailureAt(record RunRecord, index, stageCount int) error {
	if index == 0 {
		if record.RunnerError != nil {
			return fmt.Errorf("common runner reported a failure")
		}
		if record.LauncherError != nil {
			return fmt.Errorf("launcher process reported a failure")
		}
		if record.CheckpointSourceError != nil {
			return fmt.Errorf("checkpoint source reported a failure")
		}
	}
	if index == stageCount-1 {
		if record.CleanupError != nil {
			return fmt.Errorf("common cleanup reported a failure")
		}
		if record.CompleteRunTimedOut {
			return fmt.Errorf("complete-run deadline exceeded")
		}
	}
	return nil
}

func collectorError(code, message string) *StageError {
	return &StageError{Code: code, Message: message, Effect: "unknown_effect", Retry: Retry{Safe: false, Action: "inspect_originating_failure"}}
}

func cloneStageError(in *StageError) *StageError {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}

func cloneObservations(in []Observation) []Observation {
	out := make([]Observation, len(in))
	for i, observation := range in {
		out[i] = observation
		out[i].Evidence = cloneValueMap(observation.Evidence)
	}
	return out
}

func cloneValueMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	b, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil
	}
	return out
}

func stringPointerValue(value string) *string { return &value }
