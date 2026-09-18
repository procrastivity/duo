package portablelauncher

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"sort"

	"github.com/procrastivity/duo/internal/registry"
)

// OracleSpec identifies the trusted derivation rules and assertion inventory
// used by the offline validator.
type OracleSpec struct {
	Revision                 int               `json:"revision"`
	Derivation               string            `json:"derivation"`
	AssertionIDs             []string          `json:"assertion_ids"`
	DuoOperations            map[string]string `json:"duo_operations"`
	BlockedPrerequisite      string            `json:"blocked_prerequisite"`
	TimeoutMinimumMS         int64             `json:"timeout_minimum_ms"`
	TimeoutMaximumMS         int64             `json:"timeout_maximum_ms"`
	CompleteRunMaximumMS     int64             `json:"complete_run_maximum_ms"`
	IndependentEvidenceOnly  bool              `json:"independent_evidence_only"`
	LauncherVerdictUntrusted bool              `json:"launcher_verdict_untrusted"`
}

// CanonicalOracle returns the trusted oracle definition for the suite.
func CanonicalOracle() OracleSpec {
	ids := map[string]bool{
		"stage.completed_within_deadline": true,
		"stage.failure":                   true,
		"prerequisite.not_reached":        true,
		"blocked.induction_available":     true,
	}
	for _, step := range CanonicalScenario().Steps {
		for _, assertion := range step.Assertions {
			ids[assertion.ID] = true
		}
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	return OracleSpec{
		Revision: 1, Derivation: "portable-launcher-evidence-derivation/v1", AssertionIDs: ordered,
		DuoOperations:       portableOperationNames(),
		BlockedPrerequisite: "supported_admitted_then_blocked_producer",
		TimeoutMinimumMS:    20_000, TimeoutMaximumMS: 22_500,
		CompleteRunMaximumMS:    600_000,
		IndependentEvidenceOnly: true, LauncherVerdictUntrusted: true,
	}
}

func portableOperationNames() map[string]string {
	assertionCLI := map[string][]string{
		"bind.runtime_identity":      {"session", "show"},
		"command.delivery_integrity": {"prompt", "show"},
		"launch.resolution":          {"session", "launch"},
		"observe.assistant_text":     {"conversation", "list"},
		"observe.restart_text":       {"conversation", "list"},
		"send.delivery":              {"prompt", "show"},
		"send.idempotency_conflict":  {"prompt", "send"},
	}
	operations := make(map[string]string, len(assertionCLI))
	for assertionID, cli := range assertionCLI {
		for _, descriptor := range registry.All() {
			if slices.Equal(descriptor.CLI, cli) {
				operations[assertionID] = descriptor.Name
				break
			}
		}
		if operations[assertionID] == "" {
			panic(fmt.Sprintf("portable launcher oracle: no registry operation for CLI path %v", cli))
		}
	}
	return operations
}

// OracleDigest returns the digest of the canonical oracle bytes.
func OracleDigest() string {
	b, err := json.Marshal(CanonicalOracle())
	if err != nil {
		panic(fmt.Sprintf("portable launcher oracle: %v", err))
	}
	return Digest(b)
}

type capturedObservation struct {
	Observation
	Reference string
}

func validateAssertions(result Result, scenario Scenario, observations map[observationKey]capturedObservation, problems *Problems) {
	allowed := map[string]bool{}
	for _, id := range CanonicalOracle().AssertionIDs {
		allowed[id] = true
	}

	limit := len(result.Stages)
	if len(scenario.Steps) < limit {
		limit = len(scenario.Steps)
	}
	for i := 0; i < limit; i++ {
		stage := result.Stages[i]
		step := scenario.Steps[i]
		seen := map[string]bool{}
		for _, assertion := range stage.Assertions {
			path := fmt.Sprintf("stages[%d].assertions[%s]", i, assertion.ID)
			if !allowed[assertion.ID] {
				problems.Add(path + ": unknown oracle assertion")
			}
			if seen[assertion.ID] {
				problems.Add(path + ": duplicate oracle assertion")
			}
			seen[assertion.ID] = true
			obs, ok := observations[observationKey{Sequence: stage.Sequence, AssertionID: assertion.ID}]
			if !ok {
				problems.Add(path + ": no independent evidence observation")
				continue
			}
			if !containsString(stage.Evidence, obs.Reference) {
				problems.Add(path + ": observation is not in evidence referenced by this stage")
			}
			if obs.Source == "launcher_result" || obs.Source == "launcher_final_text" || obs.Source == "terminal" {
				problems.Add(path + ": untrusted source cannot satisfy an oracle assertion")
			}
			actual := deriveObservationActual(result, obs.Observation, path, problems)
			if !reflect.DeepEqual(assertion.Actual, actual) {
				problems.Add(path + ": actual does not match independent evidence")
			}
			wantMatched := reflect.DeepEqual(assertion.Expected, assertion.Actual)
			if assertion.Matched != wantMatched {
				problems.Add(path + ": matched is inconsistent with expected and actual")
			}
		}

		switch stage.Verdict {
		case "pass":
			if len(stage.Assertions) != len(step.Assertions) {
				problems.Add(fmt.Sprintf("stages[%d]: passing stage has %d assertions, want %d", i, len(stage.Assertions), len(step.Assertions)))
			}
			for _, expected := range step.Assertions {
				var found *Assertion
				for j := range stage.Assertions {
					if stage.Assertions[j].ID == expected.ID {
						found = &stage.Assertions[j]
						break
					}
				}
				if found == nil {
					problems.Add(fmt.Sprintf("stages[%d]: missing required assertion %q", i, expected.ID))
					continue
				}
				if !reflect.DeepEqual(found.Expected, expected.Expected) {
					problems.Add(fmt.Sprintf("stages[%d].assertions[%s]: expectation is not the trusted scenario value", i, expected.ID))
				}
				if !found.Matched {
					problems.Add(fmt.Sprintf("stages[%d].assertions[%s]: passing stage has unmatched assertion", i, expected.ID))
				}
			}
		case "fail":
			if len(stage.Assertions) == 0 {
				problems.Add(fmt.Sprintf("stages[%d]: failed stage has no independently checkable assertion", i))
			}
			allMatched := true
			for _, assertion := range stage.Assertions {
				allMatched = allMatched && assertion.Matched
			}
			if allMatched {
				problems.Add(fmt.Sprintf("stages[%d]: failed stage has no failed assertion", i))
			}
		}
	}
}

func deriveObservationActual(result Result, obs Observation, path string, problems *Problems) map[string]any {
	if _, present := obs.Evidence["oracle_actual"]; present {
		problems.Add(path + ": evidence contains forbidden oracle_actual shortcut")
	}
	switch obs.Source {
	case "duo_envelope":
		envelope, ok := obs.Evidence["envelope"].(map[string]any)
		if !ok || envelope["schema"] != "duo.external/v1" || stringValue(envelope["request_id"]) == "" || !operationPattern.MatchString(stringValue(envelope["operation"])) {
			problems.Add(path + ": malformed duo.external/v1 evidence envelope")
			return nil
		}
		if err := ValidateExternalEnvelope(envelope); err != nil {
			problems.Add(path + ": duo.external/v1 schema violation: " + err.Error())
		}
		wantOperation, known := CanonicalOracle().DuoOperations[obs.AssertionID]
		if !known || envelope["operation"] != wantOperation {
			problems.Add(path + ": Duo operation does not match oracle assertion")
		}
		_, hasResult := envelope["result"]
		_, hasError := envelope["error"]
		if hasResult == hasError {
			problems.Add(path + ": Duo evidence must carry exactly one of result or error")
		}
		return deriveDuoActual(obs.AssertionID, envelope, obs.Evidence, path, problems)
	case "controller":
		monotonic, hasMonotonic := obs.Evidence["monotonic_ms"]
		if obs.Evidence["kind"] != "controller.checkpoint" || !hasMonotonic || !isNumber(monotonic) || int64Number(monotonic) < 0 {
			problems.Add(path + ": malformed controller checkpoint")
		}
		if (obs.AssertionID == "observe.exited" || obs.AssertionID == "observe.timeout") && obs.Evidence["target_verified"] != true {
			problems.Add(path + ": exact process/attachment target was not verified")
		}
		if obs.AssertionID == "observe.timeout" && obs.Evidence["timeout_probe_supported"] != true {
			problems.Add(path + ": prerequisite.timeout_induction_unavailable")
		}
		if obs.AssertionID == "stage.completed_within_deadline" {
			started, startOK := numberValue(obs.Evidence["started_ms"])
			finished, finishOK := numberValue(obs.Evidence["finished_ms"])
			deadline, deadlineOK := numberValue(obs.Evidence["deadline_ms"])
			if !startOK || !finishOK || !deadlineOK || finished < started || deadline < 0 {
				problems.Add(path + ": malformed monotonic deadline checkpoint")
				return nil
			}
			if obs.Sequence < 1 || obs.Sequence > len(result.Stages) || obs.Sequence > len(CanonicalScenario().Steps) {
				problems.Add(path + ": deadline checkpoint sequence is outside the scenario")
				return nil
			}
			stage := result.Stages[obs.Sequence-1]
			step := CanonicalScenario().Steps[obs.Sequence-1]
			wantDeadline := float64(step.DeadlineMS)
			if step.Case == "timeout" && step.Stage == "observe" {
				wantDeadline = float64(CanonicalOracle().TimeoutMaximumMS)
			}
			if started != float64(stage.StartedOffsetMS) || finished-started != float64(stage.DurationMS) || deadline != wantDeadline {
				problems.Add(path + ": deadline checkpoint does not match the stage timing contract")
			}
			return map[string]any{"within_deadline": finished-started <= deadline}
		}
		measurements, ok := obs.Evidence["measurements"].(map[string]any)
		if !ok {
			problems.Add(path + ": controller checkpoint has no measurements")
			return nil
		}
		if _, forbidden := measurements["oracle_actual"]; forbidden {
			problems.Add(path + ": controller measurements contain forbidden oracle_actual shortcut")
		}
		if obs.AssertionID == "setup.fixture_isolated_and_pinned" {
			pinsJSON, err := json.Marshal(result.Pins)
			if err != nil || obs.Evidence["pins_digest"] != Digest(pinsJSON) {
				problems.Add(path + ": controller pin evidence does not match result pins")
			}
			if obs.Evidence["credentials_present"] != true || obs.Evidence["credential_provider"] != "openai-codex" || !oneOf(stringValue(obs.Evidence["credential_source_kind"]), "ci_secret", "operator_copy", "environment") {
				problems.Add(path + ": isolated credential metadata is missing or invalid")
			}
			if obs.Evidence["pi_no_extensions"] != true {
				problems.Add(path + ": Pi extension boundary was not proved")
			}
		}
		return measurements
	case "launcher_event":
		arguments, argumentsOK := obs.Evidence["arguments"].([]any)
		if obs.Evidence["kind"] != "skill.discovery" || obs.Evidence["task_digest"] != Digest(CanonicalTaskBytes) || !argumentsOK || len(arguments) == 0 {
			problems.Add(path + ": malformed launcher discovery event")
		}
		skill, ok := obs.Evidence["skill"].(map[string]any)
		if !ok {
			problems.Add(path + ": launcher discovery event has no skill identity")
			return nil
		}
		return skill
	default:
		problems.Add(path + ": unknown evidence source")
		return nil
	}
}

func deriveDuoActual(assertionID string, envelope, wrapper map[string]any, path string, problems *Problems) map[string]any {
	result, _ := envelope["result"].(map[string]any)
	switch assertionID {
	case "launch.resolution":
		argvHasPrompt, argvRecorded := wrapper["argv_has_prompt"].(bool)
		if !argvRecorded {
			problems.Add(path + ": launch evidence does not record prompt argv presence")
		}
		leaves, _ := result["leaves"].([]any)
		selected := 0
		runtime := ""
		for _, raw := range leaves {
			leaf, _ := raw.(map[string]any)
			if leaf["outcome"] == "selected" {
				selected++
				runtime = stringValue(leaf["agent_runtime"])
			}
		}
		host, _ := result["host"].(map[string]any)
		return map[string]any{"runtime": runtime, "host": stringValue(host["kind"]), "selected_leaves": float64(selected), "prelaunch_prompt": argvHasPrompt}
	case "bind.runtime_identity":
		instances, instancesOK := wrapper["authority_instance_ids"].([]any)
		correlations, correlationsOK := wrapper["active_correlation_runtime_ids"].([]any)
		attachments, _ := result["attachments"].([]any)
		runtimeInstanceID := stringValue(result["runtime_instance_id"])
		claimed := len(attachments) == 1
		if claimed {
			attachment, _ := attachments[0].(map[string]any)
			birth, _ := attachment["process_birth"].(map[string]any)
			claimed = attachment["state"] == "attached" && attachment["claim_held"] == true && int64Number(birth["pid"]) > 0 && stringValue(birth["started_at"]) != ""
		}
		if !instancesOK || !correlationsOK {
			problems.Add(path + ": bind evidence lacks authority relationships")
		}
		if runtimeInstanceID == "" || len(instances) != 1 || len(correlations) != 1 || stringValue(instances[0]) != runtimeInstanceID || stringValue(correlations[0]) != runtimeInstanceID {
			problems.Add(path + ": bind authority relationships do not name the inspected runtime")
		}
		return map[string]any{"instances": float64(len(instances)), "correlations": float64(len(correlations)), "attachment_claimed": claimed}
	case "send.delivery":
		attempts, _ := result["attempts"].([]any)
		realization, recordedResult := "", ""
		if len(attempts) == 1 {
			attempt, _ := attempts[0].(map[string]any)
			realization = stringValue(attempt["realization"])
			recordedResult = stringValue(attempt["recorded_result"])
		}
		retry, _ := result["retry"].(map[string]any)
		milestones, _ := result["milestones"].(map[string]any)
		return map[string]any{
			"state": result["responsibility_state"], "queue_policy": result["queue_policy"],
			"attempts": float64(len(attempts)), "realization": realization, "recorded_result": recordedResult,
			"activity_observed": milestones["activity_observed"], "acknowledged": milestones["acknowledged"],
			"retry_safe": retry["safe"], "retry_action": retry["action"],
		}
	case "observe.assistant_text", "observe.restart_text":
		items, _ := result["items"].([]any)
		blocks := 0
		complete := true
		text := ""
		recordID := ""
		for _, rawItem := range items {
			item, _ := rawItem.(map[string]any)
			if item["author_role"] != "agent" {
				continue
			}
			for _, rawBlock := range sliceValue(item["blocks"]) {
				block, _ := rawBlock.(map[string]any)
				if block["type"] != "text" {
					continue
				}
				content, _ := block["content"].(map[string]any)
				blocks++
				text += stringValue(content["text"])
			}
			complete = complete && item["completion"] == "source_record_complete"
			recordID = stringValue(item["record_id"])
		}
		if assertionID == "observe.restart_text" {
			return map[string]any{"text_digest": Digest([]byte(text)), "same_record": recordID != "" && recordID == stringValue(wrapper["baseline_record_id"])}
		}
		return map[string]any{"assistant_blocks": float64(blocks), "text_digest": Digest([]byte(text)), "source_complete": complete}
	case "send.idempotency_conflict":
		errorObject, _ := envelope["error"].(map[string]any)
		retry, _ := errorObject["retry"].(map[string]any)
		details, _ := errorObject["details"].(map[string]any)
		target, _ := errorObject["target"].(map[string]any)
		return map[string]any{
			"code": errorObject["code"], "effect": errorObject["effect"], "retry_safe": retry["safe"],
			"retry_action": retry["action"], "idempotency_key": details["idempotency_key"],
			"target_original": stringValue(target["id"]) != "" && target["id"] == wrapper["original_command_id"],
			"existing_digest": details["existing_digest"], "request_digest": details["request_digest"],
		}
	case "command.delivery_integrity":
		attempts, _ := result["attempts"].([]any)
		realization, recordedResult := "", ""
		if len(attempts) == 1 {
			attempt, _ := attempts[0].(map[string]any)
			realization = stringValue(attempt["realization"])
			recordedResult = stringValue(attempt["recorded_result"])
		}
		retry, _ := result["retry"].(map[string]any)
		return map[string]any{
			"state": result["responsibility_state"], "queue_policy": result["queue_policy"],
			"attempts": float64(len(attempts)), "realization": realization, "recorded_result": recordedResult,
			"retry_safe": retry["safe"], "retry_action": retry["action"], "unchanged": wrapper["unchanged"] == true,
		}
	default:
		problems.Add(path + ": assertion has no Duo evidence derivation")
		return nil
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func numberValue(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}

func sliceValue(value any) []any {
	values, _ := value.([]any)
	return values
}

// TimeoutEvidence records the independent measurements used to validate the
// suite's intentional observation timeout.
type TimeoutEvidence struct {
	ElapsedMS        int64
	ProcessSucceeded bool
	StoppedVerified  bool
	Delivered        bool
	Attempts         int
	AssistantBlocks  int
	OuterTimedOut    bool
}

// ValidateIntentionalTimeout is the controller/oracle boundary for the
// intentional observation timeout. It performs no live signal or host probe.
func ValidateIntentionalTimeout(e TimeoutEvidence) error {
	var p Problems
	if e.ElapsedMS < 20_000 || e.ElapsedMS > 22_500 {
		p.Add(fmt.Sprintf("timeout observation elapsed_ms=%d outside 20000..22500", e.ElapsedMS))
	}
	if !e.ProcessSucceeded || e.OuterTimedOut {
		p.Add("timeout outcome came from launcher/process failure")
	}
	if !e.StoppedVerified {
		p.Add("timeout process suspension was not independently verified")
	}
	if !e.Delivered || e.Attempts != 1 {
		p.Add("timeout command was not delivered exactly once")
	}
	if e.AssistantBlocks != 0 {
		p.Add("timeout observation contains an assistant response")
	}
	return p.Err()
}
