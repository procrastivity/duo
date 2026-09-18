package portablelauncher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

// Canonical request, prompt, conflict, and outer-task payloads.
var (
	RequestBytes = []byte("fixture_id=portable-launcher-v1\nexpected_reply=DUO_PORTABLE_LAUNCHER_OK_V1\n")
	PromptBytes  = []byte("Read fixture/request.txt and reply with exactly the value of expected_reply, with no surrounding text.")
	ConflictText = []byte("This text must conflict and must never be delivered.")

	// CanonicalTaskBytes are submitted byte-for-byte to every outer launcher.
	// Expectations remain in the signed scenario and oracle, not in this task.
	CanonicalTaskBytes = []byte("Load the shared duo-delegation-loop skill and execute the canonical scenario manifest at $DUO_CONFORMANCE_SCENARIO_PATH in order.\n")
)

// Scenario is the launcher-neutral ordered conformance contract.
type Scenario struct {
	Schema        string                      `json:"schema"`
	Name          string                      `json:"name"`
	Revision      int                         `json:"revision"`
	TaskDigest    string                      `json:"task_digest"`
	Fixture       ScenarioFixture             `json:"fixture"`
	Prerequisites []Prerequisite              `json:"prerequisites"`
	Launchers     map[string]AcceptedLauncher `json:"launchers"`
	DeadlinesMS   map[string]int64            `json:"deadlines_ms"`
	Steps         []StepSpec                  `json:"steps"`
}

// ScenarioFixture describes the deterministic workspace and runtime inputs.
type ScenarioFixture struct {
	Workspace      string `json:"workspace"`
	ConfigPath     string `json:"config_path"`
	Host           string `json:"host"`
	Preset         string `json:"preset"`
	Runtime        string `json:"runtime"`
	Provider       string `json:"provider"`
	ModelLine      string `json:"model_line"`
	RequestPath    string `json:"request_path"`
	Prompt         string `json:"prompt"`
	ExpectedReply  string `json:"expected_reply"`
	ConflictText   string `json:"conflict_text"`
	RequestDigest  string `json:"request_digest"`
	ReplyDigest    string `json:"reply_digest"`
	PromptDigest   string `json:"prompt_digest"`
	ConflictDigest string `json:"conflict_digest"`
	IdempotencyKey string `json:"idempotency_key"`
}

// Prerequisite identifies one required capability and when it must be checked.
type Prerequisite struct {
	Name     string `json:"name"`
	Status   string `json:"status"`
	PreSetup bool   `json:"pre_setup"`
}

// StepSpec defines one ordered stage and its trusted expectations.
type StepSpec struct {
	Sequence   int                 `json:"sequence"`
	Stage      string              `json:"stage"`
	Case       string              `json:"case"`
	Outcome    string              `json:"expected_outcome"`
	DeadlineMS int64               `json:"deadline_ms"`
	Assertions []ExpectedAssertion `json:"assertions"`
}

// ExpectedAssertion is an assertion ID and its canonical expected value.
type ExpectedAssertion struct {
	ID       string         `json:"id"`
	Expected map[string]any `json:"expected"`
}

var caseMatrix = []struct {
	Case   string
	Stages []string
}{
	{Case: "run", Stages: []string{"setup", "discovery", "preflight"}},
	{Case: "happy", Stages: []string{"launch", "bind", "send", "observe", "command_inspection"}},
	{Case: "restart", Stages: []string{"restart", "bind", "observe", "command_inspection"}},
	{Case: "same_key_same_text", Stages: []string{"send", "command_inspection"}},
	{Case: "same_key_different_text", Stages: []string{"send", "command_inspection"}},
	{Case: "blocked", Stages: []string{"launch", "bind", "send", "observe", "command_inspection"}},
	{Case: "exited", Stages: []string{"launch", "bind", "send", "observe", "command_inspection"}},
	{Case: "timeout", Stages: []string{"launch", "bind", "send", "observe", "command_inspection"}},
	{Case: "run", Stages: []string{"cleanup"}},
}

var deadlineByStage = map[string]int64{
	"setup": 30_000, "discovery": 30_000, "preflight": 15_000,
	"launch": 30_000, "bind": 12_000, "send": 30_000,
	"observe": 20_000, "command_inspection": 15_000, "restart": 15_000,
	"cleanup": 30_000,
}

// CanonicalScenario returns the complete portable-launcher scenario.
func CanonicalScenario() Scenario {
	s := Scenario{
		Schema:     ScenarioSchema,
		Name:       ScenarioName,
		Revision:   1,
		TaskDigest: Digest(CanonicalTaskBytes),
		Fixture: ScenarioFixture{
			Workspace: "$WORKSPACE", ConfigPath: "$RUN/xdg/config/duo/duo.config.yaml",
			Host: "herdr:$RUN/herdr/herdr.sock", Preset: "builder", Runtime: "pi",
			Provider: "openai-codex", ModelLine: "gpt-5.6-luna",
			RequestPath: "$WORKSPACE/fixture/request.txt", Prompt: string(PromptBytes),
			ExpectedReply: "DUO_PORTABLE_LAUNCHER_OK_V1", ConflictText: string(ConflictText),
			RequestDigest: RequestDigest, ReplyDigest: ReplyDigest,
			PromptDigest: PromptDigest, ConflictDigest: ConflictDigest,
			IdempotencyKey: "portable-launcher-v1-primary",
		},
		Prerequisites: []Prerequisite{
			{Name: "pinned_launcher_binary", Status: "required", PreSetup: true},
			{Name: "pinned_duo_build", Status: "required", PreSetup: true},
			{Name: "pinned_pi_0_83_0", Status: "required", PreSetup: true},
			{Name: "pinned_herdr_0_8_2_protocol_20", Status: "required", PreSetup: true},
			{Name: "isolated_inner_provider_credentials", Status: "required", PreSetup: true},
			{Name: "timeout_induction_socket_probe", Status: "required", PreSetup: false},
			{Name: "supported_admitted_then_blocked_producer", Status: "unavailable", PreSetup: false},
		},
		Launchers:   cloneLauncherPins(),
		DeadlinesMS: cloneDeadlines(),
	}
	sequence := 0
	for _, group := range caseMatrix {
		for _, stage := range group.Stages {
			sequence++
			outcome := "success"
			if stage == "observe" {
				switch group.Case {
				case "blocked", "exited", "timeout":
					outcome = group.Case
				}
			}
			s.Steps = append(s.Steps, StepSpec{
				Sequence: sequence, Stage: stage, Case: group.Case, Outcome: outcome,
				DeadlineMS: deadlineByStage[stage], Assertions: expectedAssertions(stage, group.Case),
			})
		}
	}
	return s
}

func cloneLauncherPins() map[string]AcceptedLauncher {
	out := make(map[string]AcceptedLauncher, len(acceptedLaunchers))
	for name, pin := range acceptedLaunchers {
		out[name] = pin
	}
	return out
}

func cloneDeadlines() map[string]int64 {
	out := make(map[string]int64, len(deadlineByStage)+1)
	for k, v := range deadlineByStage {
		out[k] = v
	}
	out["complete_run"] = int64((10 * time.Minute) / time.Millisecond)
	return out
}

func expectedAssertions(stage, caseName string) []ExpectedAssertion {
	timing := ExpectedAssertion{ID: "stage.completed_within_deadline", Expected: map[string]any{"within_deadline": true}}
	semantic := ExpectedAssertion{ID: stageAssertionID(stage, caseName), Expected: assertionExpectation(stage, caseName)}
	return []ExpectedAssertion{timing, semantic}
}

func stageAssertionID(stage, caseName string) string {
	switch {
	case stage == "setup":
		return "setup.fixture_isolated_and_pinned"
	case stage == "discovery":
		return "discovery.skill_identity"
	case stage == "preflight":
		return "preflight.ready"
	case stage == "launch":
		return "launch.resolution"
	case stage == "bind":
		return "bind.runtime_identity"
	case stage == "restart":
		return "restart.authority_continuity"
	case stage == "cleanup":
		return "cleanup.complete"
	case stage == "observe" && caseName == "happy":
		return "observe.assistant_text"
	case stage == "observe" && caseName == "restart":
		return "observe.restart_text"
	case stage == "observe":
		return "observe." + caseName
	case stage == "send" && caseName == "same_key_same_text":
		return "send.idempotent_replay"
	case stage == "send" && caseName == "same_key_different_text":
		return "send.idempotency_conflict"
	case stage == "send":
		return "send.delivery"
	case stage == "command_inspection" && caseName == "same_key_same_text":
		return "command.idempotent_no_duplication"
	case stage == "command_inspection" && caseName == "same_key_different_text":
		return "command.idempotency_no_drift"
	default:
		return "command.delivery_integrity"
	}
}

func assertionExpectation(stage, caseName string) map[string]any {
	switch stageAssertionID(stage, caseName) {
	case "setup.fixture_isolated_and_pinned":
		return map[string]any{"fixture_root": "$RUN", "shared_state": false, "plugins_enabled": false, "mcp_enabled": false, "terminal_input_used": false}
	case "discovery.skill_identity":
		return map[string]any{"name": SkillName, "format_version": SkillFormat, "content_digest": SkillContentDigest}
	case "preflight.ready":
		return map[string]any{"status": "ready", "required_checks": float64(7), "all_required_pass": true}
	case "launch.resolution":
		return map[string]any{"runtime": "pi", "host": "herdr", "selected_leaves": float64(1), "prelaunch_prompt": false}
	case "bind.runtime_identity":
		return map[string]any{"instances": float64(1), "correlations": float64(1), "attachment_claimed": true}
	case "send.delivery":
		return map[string]any{
			"state": "delivered", "queue_policy": "queue_until_safe", "attempts": float64(1),
			"realization": "native", "recorded_result": "delivered", "activity_observed": false,
			"acknowledged": false, "retry_safe": false, "retry_action": "observe_existing_command",
		}
	case "observe.assistant_text":
		return map[string]any{"assistant_blocks": float64(1), "text_digest": ReplyDigest, "source_complete": true}
	case "restart.authority_continuity":
		return map[string]any{"outcome": "same_live", "distinct_processes": true, "same_store": true, "attempts": float64(1)}
	case "observe.restart_text":
		return map[string]any{"text_digest": ReplyDigest, "same_record": true}
	case "send.idempotent_replay":
		return map[string]any{"same_command": true, "attempts": float64(1), "new_turns": float64(0)}
	case "command.idempotent_no_duplication":
		return map[string]any{"commands_delta": float64(0), "attempts_delta": float64(0), "turns_delta": float64(0)}
	case "send.idempotency_conflict":
		return map[string]any{
			"code": "command.idempotency_conflict", "effect": "no_effect", "retry_safe": false,
			"retry_action": "use_new_idempotency_key", "idempotency_key": "portable-launcher-v1-primary",
			"target_original": true, "existing_digest": PromptDigest, "request_digest": ConflictDigest,
		}
	case "command.idempotency_no_drift":
		return map[string]any{"commands_delta": float64(0), "attempts_delta": float64(0), "queue_delta": float64(0), "turns_delta": float64(0)}
	case "observe.blocked":
		return map[string]any{"condition": "blocked", "nonterminal": true, "attempts": float64(1), "assistant_blocks": float64(0)}
	case "observe.exited":
		return map[string]any{"condition": "exited", "same_instance": true, "process_success": true, "attempts": float64(1)}
	case "observe.timeout":
		return map[string]any{
			"elapsed_min_ms": float64(20_000), "elapsed_max_ms": float64(22_500), "elapsed_ms": float64(20_000),
			"process_success": true, "outer_timed_out": false, "stopped_verified": true,
			"delivered": true, "attempts": float64(1), "assistant_blocks": float64(0),
		}
	case "cleanup.complete":
		return map[string]any{"pids_remaining": float64(0), "sockets_remaining": float64(0), "panes_remaining": float64(0), "root_removed": true, "export_complete": true}
	default:
		return map[string]any{
			"state": "delivered", "queue_policy": "queue_until_safe", "attempts": float64(1),
			"realization": "native", "recorded_result": "delivered", "retry_safe": false,
			"retry_action": "observe_existing_command", "unchanged": true,
		}
	}
}

// ScenarioJSON encodes a scenario using the canonical fixture formatting.
func ScenarioJSON(s Scenario) ([]byte, error) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal scenario: %w", err)
	}
	return append(b, '\n'), nil
}

// Digest returns a sha256-prefixed lowercase digest of data.
func Digest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
