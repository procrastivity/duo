package portablelauncher

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const fixtureDir = "../../../contracts/fixtures/duo-portable-launcher-conformance-v1"

const fixtureDuoCommit = "0123456789abcdef0123456789abcdef01234567"

type builtFixture struct {
	Scenario []byte
	Result   Result
	Rejected Result
	Index    EvidenceIndex
	BlobRef  string
	Blob     []byte
}

func buildContractFixture(t *testing.T) builtFixture {
	t.Helper()
	scenario := CanonicalScenario()
	scenarioJSON, err := ScenarioJSON(scenario)
	if err != nil {
		t.Fatal(err)
	}
	result := Result{
		Schema:  ResultSchema,
		Suite:   SuiteIdentity{Name: ScenarioName, Revision: scenario.Revision, ManifestDigest: Digest(scenarioJSON), OracleDigest: OracleDigest()},
		Run:     RunIdentity{RunID: "fixture-structural-blocked", ObservedAt: "2026-09-17T00:00:00Z", HostOS: "linux", HostArch: "x86_64", FixtureRoot: "$RUN"},
		Pins:    fixturePins(),
		Summary: Summary{Verdict: "fail", FirstFailedStage: stringPointer("launch"), FirstFailedCase: stringPointer("blocked")},
		Scrub:   ScrubRecord{Status: "pass", Policy: ScrubPolicy, Findings: []string{}},
	}

	var observations []Observation
	var offset int64
	blockedFailed := false
	for _, step := range scenario.Steps {
		stage := StageResult{
			Sequence: step.Sequence, Stage: step.Stage, Case: step.Case,
			Verdict: "pass", Outcome: step.Outcome, StartedOffsetMS: offset, DurationMS: 1,
		}
		if step.Case == "timeout" && step.Stage == "observe" {
			stage.DurationMS = 20_000
		}
		switch {
		case step.Case == "blocked" && step.Stage == "launch":
			blockedFailed = true
			stage.Verdict, stage.Outcome = "fail", "error"
			stage.Error = &StageError{
				Code: "prerequisite.blocked_induction_unavailable", Message: "supported admitted-then-blocked evidence is unavailable",
				Effect: "no_effect", Retry: Retry{Safe: false, Action: "add_supported_blocked_evidence"},
			}
			stage.Assertions = []Assertion{{
				ID: "blocked.induction_available", Expected: map[string]any{"available": true},
				Actual: map[string]any{"available": false}, Matched: false,
			}}
		case step.Case == "blocked" && blockedFailed:
			stage.Verdict, stage.Outcome = "fail", "error"
			stage.Error = &StageError{
				Code: "prerequisite.not_reached", Message: "blocked induction prerequisite failed",
				Effect: "no_effect", Retry: Retry{Safe: false, Action: "inspect_originating_failure"},
			}
			stage.Assertions = []Assertion{{
				ID: "prerequisite.not_reached", Expected: map[string]any{"reached": true},
				Actual: map[string]any{"reached": false}, Matched: false,
			}}
		default:
			for _, expected := range step.Assertions {
				actual := cloneMap(t, expected.Expected)
				stage.Assertions = append(stage.Assertions, Assertion{ID: expected.ID, Expected: cloneMap(t, expected.Expected), Actual: actual, Matched: true})
			}
		}
		for _, assertion := range stage.Assertions {
			observations = append(observations, Observation{
				Sequence: stage.Sequence, AssertionID: assertion.ID,
				Source:   evidenceSource(assertion.ID),
				Evidence: evidenceDocument(t, assertion.ID, stage, result.Pins, assertion.Actual),
			})
		}
		result.Stages = append(result.Stages, stage)
		offset += stage.DurationMS
	}

	evidence := Evidence{Schema: EvidenceSchema, ScrubStatus: "pass", Observations: observations}
	capture := NewCapture()
	ref, err := capture.AddJSON("common_collector", evidence)
	if err != nil {
		t.Fatal(err)
	}
	for i := range result.Stages {
		result.Stages[i].Evidence = []string{ref}
	}
	blob, _ := capture.Blob(ref)

	rejected := cloneResult(t, result)
	for i, step := range scenario.Steps {
		if rejected.Stages[i].Case != "blocked" {
			continue
		}
		rejected.Stages[i].Verdict = "pass"
		rejected.Stages[i].Outcome = step.Outcome
		rejected.Stages[i].Error = nil
		rejected.Stages[i].Assertions = nil
		for _, expected := range step.Assertions {
			rejected.Stages[i].Assertions = append(rejected.Stages[i].Assertions, Assertion{ID: expected.ID, Expected: cloneMap(t, expected.Expected), Actual: cloneMap(t, expected.Expected), Matched: true})
		}
	}
	rejected.Summary = Summary{Verdict: "pass"}
	return builtFixture{Scenario: scenarioJSON, Result: result, Rejected: rejected, Index: capture.Index(), BlobRef: ref, Blob: blob}
}

func evidenceDocument(t *testing.T, id string, stage StageResult, pins Pins, actual map[string]any) map[string]any {
	t.Helper()
	sequence := stage.Sequence
	source := evidenceSource(id)
	switch source {
	case "launcher_event":
		return map[string]any{"kind": "skill.discovery", "task_digest": Digest(CanonicalTaskBytes), "arguments": []any{"fixture-launcher", TaskArgument}, "skill": cloneMap(t, actual)}
	case "controller":
		doc := map[string]any{"kind": "controller.checkpoint", "monotonic_ms": float64(stage.StartedOffsetMS), "measurements": cloneMap(t, actual)}
		if id == "stage.completed_within_deadline" {
			delete(doc, "measurements")
			doc["started_ms"] = float64(stage.StartedOffsetMS)
			doc["finished_ms"] = float64(stage.StartedOffsetMS + stage.DurationMS)
			deadline := CanonicalScenario().Steps[sequence-1].DeadlineMS
			if stage.Case == "timeout" && stage.Stage == "observe" {
				deadline = CanonicalOracle().TimeoutMaximumMS
			}
			doc["deadline_ms"] = float64(deadline)
		}
		if id == "observe.exited" || id == "observe.timeout" {
			doc["target_verified"] = true
		}
		if id == "observe.timeout" {
			doc["timeout_probe_supported"] = true
		}
		if id == "setup.fixture_isolated_and_pinned" {
			pinBytes, err := json.Marshal(pins)
			if err != nil {
				t.Fatal(err)
			}
			doc["pins_digest"] = Digest(pinBytes)
			doc["credentials_present"] = true
			doc["credential_provider"] = "openai-codex"
			doc["credential_source_kind"] = "operator_copy"
			doc["pi_no_extensions"] = true
		}
		return doc
	default:
		return duoEvidenceDocument(t, id, sequence, actual)
	}
}

func duoEvidenceDocument(t *testing.T, id string, sequence int, actual map[string]any) map[string]any {
	t.Helper()
	envelope := map[string]any{
		"schema": "duo.external/v1", "request_id": fmt.Sprintf("req_fixture_%d", sequence),
		"operation": CanonicalOracle().DuoOperations[id], "warnings": []any{}, "features": []any{},
	}
	wrapper := map[string]any{"envelope": envelope}
	switch id {
	case "launch.resolution":
		envelope["result"] = map[string]any{
			"session_id": "ses_fixture", "launch_resolution_id": "lrr_fixture", "selection": "ordered",
			"leaves": []any{map[string]any{
				"name": "main", "agent_runtime": actual["runtime"], "model_line": "gpt-5.6-luna",
				"declared_kind": "open", "outcome": "selected", "relented_avoids": []any{},
			}},
			"host": map[string]any{"kind": actual["host"], "instance_id": "hostinst_fixture", "instance_label": "fixture", "host_source": "explicit-flag", "workspace_id": "workspace_fixture", "outranked_evidence": []any{}},
		}
		wrapper["argv_has_prompt"] = actual["prelaunch_prompt"]
	case "bind.runtime_identity":
		envelope["result"] = map[string]any{
			"session_id": "ses_fixture", "lifecycle": "active", "runtime_instance_id": "run_fixture",
			"attachments": []any{map[string]any{
				"attachment_id": "attachment_fixture", "state": "attached", "integration_instance": "herdr:fixture",
				"epoch":         map[string]any{"kind": "herdr.terminal_id", "value": "term_fixture", "scope": "pane"},
				"container":     "workspace-1:pane-1",
				"process_birth": map[string]any{"pid": float64(41210), "started_at": "2026-09-17T00:00:00Z"},
				"claim_held":    true,
			}},
		}
		wrapper["authority_instance_ids"] = []any{"run_fixture"}
		wrapper["active_correlation_runtime_ids"] = []any{"run_fixture"}
	case "send.delivery", "command.delivery_integrity":
		envelope["result"] = map[string]any{
			"command_id": "cmd_fixture", "revision": "4", "operation": "prompt.deliver",
			"target":               map[string]any{"session_id": "ses_fixture", "runtime_instance_id": "run_fixture"},
			"responsibility_state": actual["state"], "queue_policy": "queue_until_safe",
			"attempts":   []any{map[string]any{"attempt_id": "attempt_fixture", "realization": "native", "started_at": "2026-09-17T00:00:00Z", "recorded_result": "delivered"}},
			"milestones": map[string]any{"accepted_at": "2026-09-17T00:00:00Z", "delivered_at": "2026-09-17T00:00:01Z", "activity_observed": false, "acknowledged": false},
			"retry":      map[string]any{"safe": false, "action": "observe_existing_command"},
		}
		if id == "command.delivery_integrity" {
			wrapper["unchanged"] = actual["unchanged"]
		}
	case "observe.assistant_text", "observe.restart_text":
		envelope["result"] = map[string]any{
			"session_id": "ses_fixture",
			"items": []any{map[string]any{
				"record_id": "record_fixture", "session_id": "ses_fixture", "runtime_instance_id": "run_fixture",
				"author_role": "agent", "blocks": []any{map[string]any{"position": "0", "type": "text", "content": map[string]any{"text": "DUO_PORTABLE_LAUNCHER_OK_V1"}}},
				"completion": "source_record_complete", "received_at": "2026-09-17T00:00:00Z",
			}},
		}
		if id == "observe.restart_text" {
			wrapper["baseline_record_id"] = "record_fixture"
		}
	case "send.idempotency_conflict":
		envelope["error"] = map[string]any{
			"class": "conflict", "code": actual["code"], "message": "fixture idempotency conflict",
			"target": map[string]any{"kind": "prompt_command", "id": "cmd_fixture"},
			"retry":  map[string]any{"safe": false, "action": actual["retry_action"]}, "effect": actual["effect"],
			"details": map[string]any{"idempotency_key": "portable-launcher-v1-primary", "existing_digest": actual["existing_digest"], "request_digest": actual["request_digest"]},
		}
		wrapper["original_command_id"] = "cmd_fixture"
		delete(envelope, "warnings")
		delete(envelope, "features")
	default:
		t.Fatalf("no fixture Duo evidence builder for %s", id)
	}
	return wrapper
}

func fixturePins() Pins {
	return Pins{
		Launcher:       LauncherPin{Name: "amp", Version: "fixture-fresh-not-in-history", ExecutableSHA256: Digest([]byte("fixture launcher executable\n"))},
		Duo:            DuoPin{Version: "fixture", Commit: fixtureDuoCommit, BuildDate: "2026-09-17T00:00:00Z", ExecutableSHA256: Digest([]byte("fixture duo executable\n"))},
		Skill:          SkillPin{Name: SkillName, FormatVersion: SkillFormat, ContentDigest: SkillContentDigest, InstallationID: "fixture-installation"},
		Config:         ConfigPin{Schema: "duo.config/v3", EffectiveDigest: Digest([]byte("fixture effective config\n"))},
		Authority:      AuthorityPin{SchemaVersion: 1, StoreDigestBefore: Digest([]byte("fixture empty authority\n")), StoreDigestAfter: Digest([]byte("fixture populated authority\n"))},
		Host:           HostPin{Name: "herdr", Version: "0.8.2", Protocol: "herdr-socket-api/20", SchemaDigest: HostSchemaDigest, ExecutableSHA256: Digest([]byte("fixture herdr executable\n"))},
		Runtime:        RuntimePin{Name: "pi", Version: "0.83.0", Format: "pi-session-jsonl/v3", AdapterBuild: "stage1", ConformanceRecord: "pi-0.83.0-2026-08-23", ExecutableSHA256: Digest([]byte("fixture pi executable\n")), DeliveryAssetSHA256: DeliveryAssetDigest},
		Model:          ModelPin{Provider: "openai-codex", ModelLine: "gpt-5.6-luna"},
		ExternalSchema: ExternalSchemaPin{Identity: "duo.external/v1", Digest: ExternalSchemaDigest},
	}
}

func evidenceSource(id string) string {
	switch {
	case strings.HasPrefix(id, "discovery."):
		return "launcher_event"
	case strings.HasPrefix(id, "setup."), strings.HasPrefix(id, "cleanup."), strings.HasPrefix(id, "blocked."), strings.HasPrefix(id, "prerequisite."), strings.HasPrefix(id, "preflight."), strings.HasPrefix(id, "restart."), strings.HasPrefix(id, "command.idempot"), id == "send.idempotent_replay", id == "observe.timeout", id == "observe.exited", id == "stage.completed_within_deadline":
		return "controller"
	default:
		return "duo_envelope"
	}
}

func TestContractFixtures(t *testing.T) {
	built := buildContractFixture(t)
	files := map[string][]byte{
		"scenario.json":                        built.Scenario,
		"result-structural-blocked.json":       marshalFixture(t, built.Result),
		"result-rejected-fabricated-pass.json": marshalFixture(t, built.Rejected),
		"evidence-index.json":                  marshalFixture(t, built.Index),
		filepath.Join("blobs", strings.TrimPrefix(built.BlobRef, "blob:sha256:")+".json"): built.Blob,
	}
	if os.Getenv("UPDATE_PORTABLE_LAUNCHER_FIXTURES") == "1" {
		if err := os.RemoveAll(fixtureDir); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			path := filepath.Join(fixtureDir, name)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s is stale; run UPDATE_PORTABLE_LAUNCHER_FIXTURES=1 go test ./internal/conformance/portablelauncher", name)
		}
	}
	if _, err := validateFixture(built); err != nil {
		t.Fatalf("structural blocked fixture must validate: %v", err)
	}
	rejected := built
	rejected.Result = built.Rejected
	if _, err := validateFixture(rejected); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("fabricated-pass fixture must be rejected for blocked prerequisite: %v", err)
	}
}

func TestDeterministicConstantsAndScenarioMatrix(t *testing.T) {
	assertDigest(t, RequestBytes, RequestDigest)
	assertDigest(t, []byte("DUO_PORTABLE_LAUNCHER_OK_V1"), ReplyDigest)
	assertDigest(t, PromptBytes, PromptDigest)
	assertDigest(t, ConflictText, ConflictDigest)
	for path, want := range map[string]string{
		"../../../skills/duo-delegation-loop/SKILL.md":           SkillContentDigest,
		"../../runtime/pi/inject/duo-inject.ts":                  DeliveryAssetDigest,
		"../../../contracts/schemas/duo-external-v1.schema.json": ExternalSchemaDigest,
		"../../../contracts/schemas/duo-config-v3.schema.json":   ConfigSchemaDigest,
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		assertDigest(t, data, want)
	}
	s := CanonicalScenario()
	if s.Revision != ScenarioRevision || s.LauncherEligibility != LauncherEligibilityCapabilityEvidence || !reflect.DeepEqual(s.Launchers, []string{"amp", "opencode", "codex"}) {
		t.Fatalf("scenario launcher policy = revision %d policy %q launchers %v", s.Revision, s.LauncherEligibility, s.Launchers)
	}
	scenarioJSON, err := ScenarioJSON(s)
	if err != nil {
		t.Fatal(err)
	}
	if bytesContainsAny(scenarioJSON, []string{"executable_sha256", "0.0.1789675234-g2899fe", "1.18.31", "0.154.0"}) {
		t.Fatalf("canonical scenario contains an exact outer-launcher allowlist: %s", scenarioJSON)
	}
	if len(s.Steps) != 32 {
		t.Fatalf("stage/case rows = %d, want 32", len(s.Steps))
	}
	for i, step := range s.Steps {
		if step.Sequence != i+1 || len(step.Assertions) != 2 {
			t.Fatalf("step %d is not complete and ordered: %+v", i, step)
		}
	}
	for _, prereq := range s.Prerequisites {
		if prereq.Name == "supported_admitted_then_blocked_producer" && prereq.Status != "unavailable" {
			t.Fatal("blocked producer must remain an explicit unavailable prerequisite")
		}
	}
	task := strings.ToLower(string(CanonicalTaskBytes))
	if strings.Contains(task, "result") || strings.Contains(task, "verdict") || strings.Contains(task, "duo_conformance_result_path") {
		t.Fatalf("canonical task assigns final-result ownership to the launcher: %q", CanonicalTaskBytes)
	}
}

func bytesContainsAny(data []byte, values []string) bool {
	for _, value := range values {
		if strings.Contains(string(data), value) {
			return true
		}
	}
	return false
}

func TestLauncherCapabilityEvidencePolicy(t *testing.T) {
	freshDigest := Digest([]byte("fresh rolling launcher"))
	for _, name := range []string{"amp", "opencode", "codex"} {
		if !validLauncherPin(LauncherPin{Name: name, Version: "9999.fresh-not-in-history", ExecutableSHA256: freshDigest}) {
			t.Errorf("fresh %s run identity was rejected", name)
		}
	}
	for _, pin := range []LauncherPin{
		{Name: "unknown", Version: "1", ExecutableSHA256: freshDigest},
		{Name: "amp", Version: "", ExecutableSHA256: freshDigest},
		{Name: "amp", Version: " 1 ", ExecutableSHA256: freshDigest},
		{Name: "amp", Version: "1", ExecutableSHA256: ""},
		{Name: "amp", Version: "1", ExecutableSHA256: "sha256:not-a-digest"},
	} {
		if validLauncherPin(pin) {
			t.Errorf("malformed run identity was accepted: %#v", pin)
		}
	}
}

func TestCopiedArtifactMustMatchDeclaredRunDigest(t *testing.T) {
	source := filepath.Join(t.TempDir(), "launcher")
	if err := os.WriteFile(source, []byte("rolling launcher bytes"), 0o700); err != nil {
		t.Fatal(err)
	}
	destination := filepath.Join(t.TempDir(), "launcher")
	err := copyPinnedArtifact(source, destination, 0o700, Digest([]byte("different declared bytes")))
	if err == nil || !strings.Contains(err.Error(), "copied executable digest mismatch") {
		t.Fatalf("copy mismatch error = %v", err)
	}
}

func TestOfflineValidatorRejectsContractDrift(t *testing.T) {
	tests := []struct {
		name string
		want string
		edit func(*builtFixture)
	}{
		{"missing stage", "got 31 records", func(f *builtFixture) { f.Result.Stages = f.Result.Stages[:31] }},
		{"duplicate stage", "got 33 records", func(f *builtFixture) { f.Result.Stages = append(f.Result.Stages, f.Result.Stages[31]) }},
		{"reordered stage", "want run/setup", func(f *builtFixture) { f.Result.Stages[0], f.Result.Stages[1] = f.Result.Stages[1], f.Result.Stages[0] }},
		{"unknown stage", "want run/setup", func(f *builtFixture) { f.Result.Stages[0].Stage = "mystery" }},
		{"unknown case", "want run/setup", func(f *builtFixture) { f.Result.Stages[0].Case = "mystery" }},
		{"nonmonotonic timing", "timing is not monotonic", func(f *builtFixture) { f.Result.Stages[2].StartedOffsetMS = 0 }},
		{"setup offset", "offset zero", func(f *builtFixture) { f.Result.Stages[0].StartedOffsetMS = 1 }},
		{"deadline exceeded", "stage deadline exceeded", func(f *builtFixture) { f.Result.Stages[0].DurationMS = 30_001 }},
		{"cleanup omission", "got 31 records", func(f *builtFixture) { f.Result.Stages = f.Result.Stages[:31] }},
		{"cleanup failure", "failed or omitted cleanup", func(f *builtFixture) { failStage(&f.Result.Stages[31], "cleanup.failed") }},
		{"terminal paste", "terminal input", func(f *builtFixture) { f.Result.Run.TerminalInputUsed = true }},
		{"plugin enabled", "plugins, MCP", func(f *builtFixture) { f.Result.Run.PluginsEnabled = true }},
		{"MCP enabled", "plugins, MCP", func(f *builtFixture) { f.Result.Run.MCPEnabled = true }},
		{"unpinned identity", "pins.runtime", func(f *builtFixture) { f.Result.Pins.Runtime.ExecutableSHA256 = "" }},
		{"unknown launcher", "pins.launcher", func(f *builtFixture) { f.Result.Pins.Launcher.Name = "unknown" }},
		{"empty launcher version", "pins.launcher", func(f *builtFixture) { f.Result.Pins.Launcher.Version = "" }},
		{"malformed launcher digest", "pins.launcher", func(f *builtFixture) { f.Result.Pins.Launcher.ExecutableSHA256 = "sha256:nope" }},
		{"wrong current runtime", "Pi 0.83.0", func(f *builtFixture) { f.Result.Pins.Runtime.Version = "0.84.4" }},
		{"wrong current host", "Herdr 0.8.2", func(f *builtFixture) { f.Result.Pins.Host.Version = "0.9.0" }},
		{"missing assertions", "passing stage has 0 assertions", func(f *builtFixture) { f.Result.Stages[0].Assertions = nil }},
		{"fabricated pass", "overall pass forbidden", func(f *builtFixture) {
			for i := range f.Result.Stages {
				f.Result.Stages[i].Verdict = "pass"
				f.Result.Stages[i].Outcome = CanonicalScenario().Steps[i].Outcome
				f.Result.Stages[i].Error = nil
			}
			f.Result.Summary = Summary{Verdict: "pass"}
		}},
		{"idempotency duplication", "expectation is not the trusted scenario", func(f *builtFixture) {
			mutateAssertion(f, "command.idempotent_no_duplication", "attempts_delta", float64(1))
		}},
		{"conflict drift", "expectation is not the trusted scenario", func(f *builtFixture) { mutateAssertion(f, "send.idempotency_conflict", "effect", "possible_effect") }},
		{"blocked pass", "blocked_producer is unavailable", func(f *builtFixture) {
			i := stageIndex(f.Result, "blocked", "launch")
			f.Result.Stages[i].Verdict = "pass"
			f.Result.Stages[i].Outcome = "success"
			f.Result.Stages[i].Error = nil
		}},
		{"secret-shaped content", "result scrub failed", func(f *builtFixture) { f.Result.Run.RunID = "sk-0123456789abcdef" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := buildContractFixture(t)
			tc.edit(&f)
			_, err := validateFixture(f)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestOfflineValidatorRejectsEvidenceFailures(t *testing.T) {
	t.Run("terminal evidence", func(t *testing.T) {
		findings := Scrub([]byte(`{"kind":"terminal_snapshot","terminal_paste":true}`))
		if len(findings) == 0 {
			t.Fatal("terminal-paste evidence passed scrub")
		}
	})
	t.Run("public authorization object is not a secret", func(t *testing.T) {
		if findings := Scrub([]byte(`{"authorization":{"allowed":true,"policy_version":"12"}}`)); len(findings) != 0 {
			t.Fatalf("public authorization metadata failed scrub: %v", findings)
		}
		if findings := Scrub([]byte(`{"authorization":"Bearer abcdefghijklmnop"}`)); len(findings) == 0 {
			t.Fatal("authorization header secret passed scrub")
		}
	})
	t.Run("missing blob", func(t *testing.T) {
		f := buildContractFixture(t)
		_, err := Validate(ValidationInput{ResultJSON: marshalFixture(t, f.Result), IndexJSON: marshalFixture(t, f.Index), ReadBlob: func(string) ([]byte, error) { return nil, errors.New("gone") }})
		if err == nil || !strings.Contains(err.Error(), "missing blob") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("mismatched blob", func(t *testing.T) {
		f := buildContractFixture(t)
		f.Blob = append(f.Blob, ' ')
		if _, err := validateFixture(f); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("malformed Duo envelope", func(t *testing.T) {
		f := buildContractFixture(t)
		mutateEvidence(t, &f, func(e *Evidence) {
			for i := range e.Observations {
				if e.Observations[i].Source == "duo_envelope" {
					envelope := e.Observations[i].Evidence["envelope"].(map[string]any)
					envelope["schema"] = "vendor.result/v1"
					return
				}
			}
		})
		if _, err := validateFixture(f); err == nil || !strings.Contains(err.Error(), "malformed duo.external/v1") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("wrong Duo operation", func(t *testing.T) {
		f := buildContractFixture(t)
		mutateEvidence(t, &f, func(e *Evidence) {
			for i := range e.Observations {
				if e.Observations[i].Source == "duo_envelope" {
					envelope := e.Observations[i].Evidence["envelope"].(map[string]any)
					envelope["operation"] = "fixture.observe"
					return
				}
			}
		})
		if _, err := validateFixture(f); err == nil || !strings.Contains(err.Error(), "operation does not match") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("oracle actual shortcut", func(t *testing.T) {
		f := buildContractFixture(t)
		mutateEvidence(t, &f, func(e *Evidence) {
			e.Observations[0].Evidence["oracle_actual"] = map[string]any{"within_deadline": true}
		})
		if _, err := validateFixture(f); err == nil || !strings.Contains(err.Error(), "forbidden oracle_actual") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("raw evidence disagrees with assertion", func(t *testing.T) {
		f := buildContractFixture(t)
		mutateEvidence(t, &f, func(e *Evidence) {
			for i := range e.Observations {
				if e.Observations[i].AssertionID == "send.delivery" {
					envelope := e.Observations[i].Evidence["envelope"].(map[string]any)
					result := envelope["result"].(map[string]any)
					result["attempts"] = []any{}
					return
				}
			}
		})
		if _, err := validateFixture(f); err == nil || !strings.Contains(err.Error(), "actual does not match independent evidence") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("pin evidence mismatch", func(t *testing.T) {
		f := buildContractFixture(t)
		f.Result.Pins.Duo.ExecutableSHA256 = "sha256:" + strings.Repeat("2", 64)
		if _, err := validateFixture(f); err == nil || !strings.Contains(err.Error(), "pin evidence") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("timeout probe unavailable", func(t *testing.T) {
		f := buildContractFixture(t)
		mutateEvidence(t, &f, func(e *Evidence) {
			for i := range e.Observations {
				if e.Observations[i].AssertionID == "observe.timeout" {
					e.Observations[i].Evidence["timeout_probe_supported"] = false
				}
			}
		})
		if _, err := validateFixture(f); err == nil || !strings.Contains(err.Error(), "prerequisite.timeout_induction_unavailable") {
			t.Fatalf("error = %v", err)
		}
	})
	t.Run("scrubbed blob", func(t *testing.T) {
		f := buildContractFixture(t)
		f.Blob = []byte(`{"authorization":"Bearer abcdefghijklmnop"}`)
		f.Index.Blobs[0].Bytes = int64(len(f.Blob))
		f.Index.Blobs[0].SHA256 = Digest(f.Blob)
		f.Index.Blobs[0].Reference = "blob:" + Digest(f.Blob)
		for i := range f.Result.Stages {
			f.Result.Stages[i].Evidence = []string{f.Index.Blobs[0].Reference}
		}
		f.BlobRef = f.Index.Blobs[0].Reference
		if _, err := validateFixture(f); err == nil || !strings.Contains(err.Error(), "scrub failed") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestEveryStageDeadlineIsEnforced(t *testing.T) {
	for i, step := range CanonicalScenario().Steps {
		t.Run(fmt.Sprintf("%02d-%s-%s", step.Sequence, step.Case, step.Stage), func(t *testing.T) {
			f := buildContractFixture(t)
			limit := step.DeadlineMS
			if step.Case == "timeout" && step.Stage == "observe" {
				limit = CanonicalOracle().TimeoutMaximumMS
			}
			f.Result.Stages[i].DurationMS = limit + 1
			_, err := validateFixture(f)
			want := fmt.Sprintf("stages[%d]: stage deadline exceeded", i)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %v, want %q", err, want)
			}
		})
	}
}

func TestTimeoutBoundaryRejectsProcessFailureAndEdges(t *testing.T) {
	valid := TimeoutEvidence{ElapsedMS: 20_000, ProcessSucceeded: true, StoppedVerified: true, Delivered: true, Attempts: 1}
	if err := ValidateIntentionalTimeout(valid); err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(*TimeoutEvidence){
		"below":              func(e *TimeoutEvidence) { e.ElapsedMS = 19_999 },
		"above":              func(e *TimeoutEvidence) { e.ElapsedMS = 22_501 },
		"process failure":    func(e *TimeoutEvidence) { e.ProcessSucceeded = false },
		"outer timeout":      func(e *TimeoutEvidence) { e.OuterTimedOut = true },
		"not stopped":        func(e *TimeoutEvidence) { e.StoppedVerified = false },
		"duplicate delivery": func(e *TimeoutEvidence) { e.Attempts = 2 },
	} {
		t.Run(name, func(t *testing.T) {
			e := valid
			edit(&e)
			if err := ValidateIntentionalTimeout(e); err == nil {
				t.Fatal("invalid timeout evidence accepted")
			}
		})
	}
}

func TestFixtureFailsClosedAndCleanupSurfacesFailures(t *testing.T) {
	base := t.TempDir()
	artifact := filepath.Join(base, "artifact")
	if err := os.WriteFile(artifact, []byte("not a pinned binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	in := SetupInput{
		BaseDir: base,
		Artifacts: map[string]Artifact{
			"launcher": {Name: "amp", Path: artifact, Version: "0.0.1789675234-g2899fe", Digest: Digest([]byte("not a pinned binary"))},
			"duo":      {Name: "duo", Path: artifact, Version: "fixture", Commit: fixtureDuoCommit, BuildDate: "2026-09-17T00:00:00Z", Digest: Digest([]byte("not a pinned binary"))},
			"host":     {Name: "herdr", Path: artifact, Version: "0.9.0", Digest: Digest([]byte("not a pinned binary"))},
			"runtime":  {Name: "pi", Path: artifact, Version: "0.84.4", Digest: Digest([]byte("not a pinned binary"))},
		},
		HostProtocol: "herdr-socket-api/22", HostSchemaDigest: "sha256:" + strings.Repeat("2", 64),
		ConfigBytes: []byte("schema: duo.config/v3\n"), EffectiveConfigDigest: "sha256:" + strings.Repeat("3", 64),
		Duo: DuoPin{Version: "fixture", Commit: fixtureDuoCommit, BuildDate: "2026-09-17T00:00:00Z", ExecutableSHA256: Digest([]byte("not a pinned binary"))},
	}
	before := directoryNames(t, base)
	if _, err := PrepareFixture(in); err == nil || !strings.Contains(err.Error(), "Pi 0.83.0") || !strings.Contains(err.Error(), "Herdr 0.8.2") {
		t.Fatalf("wrong current binaries were not rejected explicitly: %v", err)
	}
	after := directoryNames(t, base)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed setup touched run state: before=%v after=%v", before, after)
	}
	shared := in
	shared.BaseDir = filepath.Clean(os.TempDir())
	if err := CheckSetupPrerequisites(shared); err == nil || !strings.Contains(err.Error(), "private child") {
		t.Fatalf("shared root accepted: %v", err)
	}
	linkedBase := filepath.Join(filepath.Dir(base), "linked-base")
	if err := os.Symlink(base, linkedBase); err != nil {
		t.Fatal(err)
	}
	shared.BaseDir = linkedBase
	if err := CheckSetupPrerequisites(shared); err == nil || !strings.Contains(err.Error(), "non-symlink directory") {
		t.Fatalf("symlinked setup root accepted: %v", err)
	}
	realParent := t.TempDir()
	child := filepath.Join(realParent, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	linkedParent := filepath.Join(filepath.Dir(realParent), "linked-parent-"+filepath.Base(realParent))
	if err := os.Symlink(realParent, linkedParent); err != nil {
		t.Fatal(err)
	}
	shared.BaseDir = filepath.Join(linkedParent, "child")
	if err := CheckSetupPrerequisites(shared); err == nil || !strings.Contains(err.Error(), "symlink component") {
		t.Fatalf("setup root beneath a symlink accepted: %v", err)
	}

	root, err := os.MkdirTemp(t.TempDir(), "duo-portable-launcher-")
	if err != nil {
		t.Fatal(err)
	}
	f := &Fixture{Root: root}
	if _, err := f.Path("../escape"); err == nil {
		t.Fatal("fixture path escape accepted")
	}
	err = f.Cleanup(context.Background(), func(context.Context, *Fixture) error { return errors.New("pane remains") }, nil)
	if err == nil || !strings.Contains(err.Error(), "pane remains") || !strings.Contains(err.Error(), "export omitted") {
		t.Fatalf("cleanup failure/omission hidden: %v", err)
	}
	if _, statErr := os.Stat(root); !os.IsNotExist(statErr) {
		t.Fatalf("best-effort exact-root cleanup did not run: %v", statErr)
	}
}

func TestPersistDriverCapturePreservesRawStreams(t *testing.T) {
	root, err := os.MkdirTemp(t.TempDir(), "duo-portable-launcher-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "capture"), 0o700); err != nil {
		t.Fatal(err)
	}
	capture := DriverCapture{Events: []byte("event\n"), Stderr: []byte("stderr\n")}
	if err := PersistDriverCapture(root, capture); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string][]byte{
		"launcher-events.jsonl": capture.Events,
		"launcher-stderr.log":   capture.Stderr,
	} {
		got, err := os.ReadFile(filepath.Join(root, "capture", name))
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, "capture"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("persisted capture files = %v, want only raw events and stderr", entries)
	}
	if err := PersistDriverCapture(root, capture); err == nil {
		t.Fatal("capture persistence overwrote an existing raw stream")
	}
}

func prepareDriverFixture(t *testing.T, launcher string, script []byte) (string, DriverSpec) {
	t.Helper()
	root, err := os.MkdirTemp(t.TempDir(), "duo-portable-launcher-")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bin", "workspace", "capture", "workspace/.duo-conformance"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(root, "bin", launcher)
	if err := os.WriteFile(executable, script, 0o700); err != nil {
		t.Fatal(err)
	}
	writeDriverLauncherPin(t, root, LauncherPin{Name: launcher, Version: "fixture-fresh-not-in-history", ExecutableSHA256: Digest(script)})
	scenario, err := ScenarioJSON(CanonicalScenario())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", ".duo-conformance", "scenario.json"), scenario, 0o600); err != nil {
		t.Fatal(err)
	}
	return root, DriverSpec{Name: launcher, Executable: executable, Arguments: []string{TaskArgument}}
}

func writeDriverLauncherPin(t *testing.T, root string, pin LauncherPin) {
	t.Helper()
	b, err := json.Marshal(pin)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(launcherPinRelativePath)), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDriverRequiresRunLocalLauncherIdentity(t *testing.T) {
	script := []byte("#!/bin/sh\nexit 0\n")
	for _, name := range []string{"amp", "opencode", "codex"} {
		t.Run("fresh_"+name, func(t *testing.T) {
			root, spec := prepareDriverFixture(t, name, script)
			if _, err := RunDriver(context.Background(), spec, RequestFromRunRoot(root, "123")); err != nil {
				t.Fatalf("fresh recognized launcher rejected: %v", err)
			}
		})
	}

	tests := []struct {
		name string
		want string
		edit func(string, *DriverSpec)
	}{
		{name: "unknown launcher", want: "unsupported launcher", edit: func(_ string, spec *DriverSpec) { spec.Name = "unknown" }},
		{name: "malformed identity", want: "identity is malformed", edit: func(root string, _ *DriverSpec) {
			writeDriverLauncherPin(t, root, LauncherPin{Name: "amp", ExecutableSHA256: Digest(script)})
		}},
		{name: "setup driver disagreement", want: "disagrees with setup", edit: func(root string, _ *DriverSpec) {
			writeDriverLauncherPin(t, root, LauncherPin{Name: "codex", Version: "fresh", ExecutableSHA256: Digest(script)})
		}},
		{name: "copied binary digest mismatch", want: "pinned digest", edit: func(_ string, spec *DriverSpec) {
			if err := os.WriteFile(spec.Executable, []byte("#!/bin/sh\nexit 9\n"), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, spec := prepareDriverFixture(t, "amp", script)
			test.edit(root, &spec)
			if _, err := RunDriver(context.Background(), spec, RequestFromRunRoot(root, "123")); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestRunOriginValidationRejectsBeforeLauncher(t *testing.T) {
	script := []byte("#!/bin/sh\nprintf launched > launched\n")
	root, spec := prepareDriverFixture(t, "amp", script)
	t.Setenv(CommandRunOriginEnv, "987654321")
	for _, test := range []struct {
		name   string
		origin string
	}{
		{name: "missing", origin: ""},
		{name: "zero", origin: "0"},
		{name: "nondecimal", origin: "12x"},
		{name: "overflow", origin: "9223372036854775808"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := RunDriver(context.Background(), spec, RequestFromRunRoot(root, test.origin))
			if err == nil || !strings.Contains(err.Error(), "positive decimal int64") {
				t.Fatalf("RunDriver origin %q error = %v, want positive decimal int64 rejection", test.origin, err)
			}
			if _, err := os.Stat(filepath.Join(root, "workspace", "launched")); !os.IsNotExist(err) {
				t.Fatalf("launcher ran for rejected origin %q: %v", test.origin, err)
			}
		})
	}
}

func TestRecorderEnvironmentUsesExactRunConfiguration(t *testing.T) {
	const origin = "123456789"
	root := filepath.Join(t.TempDir(), "duo-portable-launcher-environment")
	environment := make(map[string]string)
	for _, entry := range isolatedEnvironment(RequestFromRunRoot(root, origin)) {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			t.Fatalf("malformed environment entry %q", entry)
		}
		environment[key] = value
	}
	if got, want := environment[CommandCaptureDirectoryEnv], filepath.Join(root, "capture"); got != want {
		t.Errorf("%s = %q, want %q", CommandCaptureDirectoryEnv, got, want)
	}
	if got := environment[CommandRunOriginEnv]; got != origin {
		t.Errorf("%s = %q, want %q", CommandRunOriginEnv, got, origin)
	}
	if _, ok := environment["DUO_CONFORMANCE_RESULT_PATH"]; ok {
		t.Fatal("isolated launcher environment contains result path")
	}
}

func TestDriverEnvironmentCannotOverrideRecorderConfiguration(t *testing.T) {
	script := []byte("#!/bin/sh\nprintf launched > launched\n")
	root, spec := prepareDriverFixture(t, "amp", script)
	for _, key := range []string{CommandCaptureDirectoryEnv, CommandRunOriginEnv} {
		t.Run(key, func(t *testing.T) {
			request := RequestFromRunRoot(root, "123456789")
			request.Environment = map[string]string{key: "override"}
			_, err := RunDriver(context.Background(), spec, request)
			if err == nil || !strings.Contains(err.Error(), "environment key") {
				t.Fatalf("RunDriver override of %s error = %v, want forbidden environment key", key, err)
			}
			if _, err := os.Stat(filepath.Join(root, "workspace", "launched")); !os.IsNotExist(err) {
				t.Fatalf("launcher ran for forbidden %s override: %v", key, err)
			}
		})
	}
}

func TestRunDriverSucceedsWithoutResultFileOrResultPathEnvironment(t *testing.T) {
	root, err := os.MkdirTemp(t.TempDir(), "duo-portable-launcher-")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bin", "workspace", "capture", "workspace/.duo-conformance"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(root, "bin", "amp")
	script := []byte("#!/bin/sh\nif [ \"${DUO_CONFORMANCE_RESULT_PATH+x}\" = x ]; then\n  printf 'unexpected result path\\n' >&2\n  exit 9\nfi\n[ \"$#\" -eq 1 ] || exit 8\nprintf '%s\\n%s\\n%s' \"$DUO_CONFORMANCE_COMMAND_CAPTURE_DIR\" \"$DUO_CONFORMANCE_RUN_ORIGIN_BOOTTIME_NS\" \"$1\"\n")
	if err := os.WriteFile(executable, script, 0o700); err != nil {
		t.Fatal(err)
	}
	const launcher = "amp"
	writeDriverLauncherPin(t, root, LauncherPin{Name: launcher, Version: "9999.rolling-fresh", ExecutableSHA256: Digest(script)})
	scenario, err := ScenarioJSON(CanonicalScenario())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", ".duo-conformance", "scenario.json"), scenario, 0o600); err != nil {
		t.Fatal(err)
	}

	const origin = "123456789"
	request := RequestFromRunRoot(root, origin)
	for _, entry := range isolatedEnvironment(request) {
		if strings.HasPrefix(entry, "DUO_CONFORMANCE_RESULT_PATH=") {
			t.Fatalf("isolated launcher environment contains result path: %q", entry)
		}
	}
	capture, err := RunDriver(context.Background(), DriverSpec{
		Name: launcher, Executable: executable, Arguments: []string{TaskArgument},
	}, request)
	if err != nil {
		t.Fatalf("RunDriver without a result file failed: %v", err)
	}
	wantEvents := []byte(filepath.Join(root, "capture") + "\n" + origin + "\n" + string(CanonicalTaskBytes))
	if !reflect.DeepEqual(capture.Events, wantEvents) || len(capture.Stderr) != 0 {
		t.Fatalf("successful launcher streams = events %q stderr %q", capture.Events, capture.Stderr)
	}
}

func TestRunDriverPreservesStreamsFromFailedLauncher(t *testing.T) {
	root, err := os.MkdirTemp(t.TempDir(), "duo-portable-launcher-")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bin", "workspace", "capture", "workspace/.duo-conformance"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	executable := filepath.Join(root, "bin", "codex")
	script := []byte("#!/bin/sh\nprintf 'event\\n'\nprintf 'stderr\\n' >&2\nexit 7\n")
	if err := os.WriteFile(executable, script, 0o700); err != nil {
		t.Fatal(err)
	}
	const launcher = "codex"
	writeDriverLauncherPin(t, root, LauncherPin{Name: launcher, Version: "9999.rolling-fresh", ExecutableSHA256: Digest(script)})
	scenario, err := ScenarioJSON(CanonicalScenario())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", ".duo-conformance", "scenario.json"), scenario, 0o600); err != nil {
		t.Fatal(err)
	}

	capture, err := RunDriver(context.Background(), DriverSpec{
		Name: launcher, Executable: executable, Arguments: []string{TaskArgument},
	}, RequestFromRunRoot(root, "123456789"))
	if err == nil || !strings.Contains(err.Error(), "launcher process failed") {
		t.Fatalf("RunDriver error = %v, want launcher failure", err)
	}
	if string(capture.Events) != "event\n" || string(capture.Stderr) != "stderr\n" {
		t.Fatalf("failed launcher streams not preserved: events=%q stderr=%q", capture.Events, capture.Stderr)
	}
}

func TestNeutralityAndThinDriverGuards(t *testing.T) {
	for _, name := range []string{"scenario.go", "oracle.go", "validate.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateSemanticSource(data); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
	for _, name := range []string{"amp", "opencode", "codex"} {
		data, err := os.ReadFile(filepath.Join("../../../contrib/portable-launcher-conformance", name, "main.go"))
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateDriverSource(data); err != nil {
			t.Errorf("%s driver: %v", name, err)
		}
		if strings.Count(string(data), "TaskArgument") != 1 {
			t.Errorf("%s driver does not submit exactly one common task marker", name)
		}
	}
	if ValidateSemanticSource([]byte(`switch launcher { case "x": }`)) == nil {
		t.Fatal("launcher-specific semantic switch accepted")
	}
	if ValidateDriverSource([]byte(`duo session launch; retry; DUO_PORTABLE_LAUNCHER_OK_V1`)) == nil {
		t.Fatal("semantic contamination in driver accepted")
	}
}

func TestPublicSchemaParsesAndIsClosed(t *testing.T) {
	data, err := os.ReadFile("../../../contracts/schemas/duo-portable-launcher-conformance-result-v1.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	var schema map[string]any
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatal(err)
	}
	if schema["additionalProperties"] != false {
		t.Fatal("result schema is not closed")
	}
	stages := schema["properties"].(map[string]any)["stages"].(map[string]any)
	if stages["minItems"] != float64(32) || stages["maxItems"] != float64(32) {
		t.Fatal("result schema does not close the 32-stage matrix")
	}
	defs, ok := schema["$defs"].(map[string]any)
	if !ok {
		t.Fatal("result schema has no definitions")
	}
	for _, name := range []string{"pins", "stage", "assertion", "error", "summary", "scrub"} {
		def, ok := defs[name].(map[string]any)
		if !ok || def["additionalProperties"] != false {
			t.Errorf("schema definition %s is absent or open", name)
		}
	}
	assertion := defs["assertion"].(map[string]any)
	properties := assertion["properties"].(map[string]any)
	idSchema := properties["id"].(map[string]any)
	var gotIDs []string
	for _, value := range idSchema["enum"].([]any) {
		gotIDs = append(gotIDs, value.(string))
	}
	wantIDs := append([]string(nil), CanonicalOracle().AssertionIDs...)
	sort.Strings(gotIDs)
	sort.Strings(wantIDs)
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Errorf("schema assertion enum = %v, oracle = %v", gotIDs, wantIDs)
	}
	suite := defs["suite"].(map[string]any)
	revisions := suite["properties"].(map[string]any)["revision"].(map[string]any)["enum"].([]any)
	if !reflect.DeepEqual(revisions, []any{float64(1), float64(2)}) {
		t.Fatalf("result schema revisions = %v, want archived 1 and current 2", revisions)
	}
	for _, name := range []string{"result-structural-blocked.json", "result-rejected-fabricated-pass.json"} {
		data, err := os.ReadFile(filepath.Join(fixtureDir, name))
		if err != nil {
			t.Fatal(err)
		}
		var document any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		var problems Problems
		validateSchemaNode(schema, schema, document, "$", &problems)
		if err := problems.Err(); err != nil {
			t.Errorf("%s does not satisfy the public schema: %v", name, err)
		}
	}
	archived := buildContractFixture(t)
	archived.Result.Suite.Revision = 1
	var archivedDocument any
	if err := json.Unmarshal(marshalFixture(t, archived.Result), &archivedDocument); err != nil {
		t.Fatal(err)
	}
	var archivedProblems Problems
	validateSchemaNode(schema, schema, archivedDocument, "$", &archivedProblems)
	if err := archivedProblems.Err(); err != nil {
		t.Fatalf("archived revision-1 result is not schema-readable: %v", err)
	}
	if _, err := validateFixture(archived); err == nil || !strings.Contains(err.Error(), "wrong name or revision") {
		t.Fatalf("current semantic validator accepted archived revision: %v", err)
	}
}

func validateFixture(f builtFixture) (*Result, error) {
	return Validate(ValidationInput{
		ResultJSON: marshalJSON(f.Result), IndexJSON: marshalJSON(f.Index),
		ReadBlob: func(ref string) ([]byte, error) {
			if ref != f.BlobRef {
				return nil, fmt.Errorf("unknown reference %s", ref)
			}
			return append([]byte(nil), f.Blob...), nil
		},
	})
}

func mutateAssertion(f *builtFixture, id, key string, value any) {
	for i := range f.Result.Stages {
		for j := range f.Result.Stages[i].Assertions {
			if f.Result.Stages[i].Assertions[j].ID != id {
				continue
			}
			f.Result.Stages[i].Assertions[j].Expected[key] = value
			f.Result.Stages[i].Assertions[j].Actual[key] = value
			f.Result.Stages[i].Assertions[j].Matched = true
		}
	}
}

func mutateEvidence(t *testing.T, f *builtFixture, mutate func(*Evidence)) {
	t.Helper()
	var evidence Evidence
	if err := json.Unmarshal(f.Blob, &evidence); err != nil {
		t.Fatal(err)
	}
	mutate(&evidence)
	capture := NewCapture()
	ref, err := capture.AddJSON("common_collector", evidence)
	if err != nil {
		t.Fatal(err)
	}
	f.BlobRef = ref
	f.Blob, _ = capture.Blob(ref)
	f.Index = capture.Index()
	for i := range f.Result.Stages {
		f.Result.Stages[i].Evidence = []string{ref}
	}
}

func failStage(stage *StageResult, code string) {
	stage.Verdict, stage.Outcome = "fail", "error"
	stage.Error = &StageError{Code: code, Message: "fixture failure", Effect: "no_effect", Retry: Retry{Action: "inspect_failure"}}
	stage.Assertions = []Assertion{{ID: "stage.failure", Expected: map[string]any{"success": true}, Actual: map[string]any{"success": false}, Matched: false}}
}

func stageIndex(r Result, caseName, stageName string) int {
	for i, stage := range r.Stages {
		if stage.Case == caseName && stage.Stage == stageName {
			return i
		}
	}
	return -1
}

func cloneMap(t *testing.T, in map[string]any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(in)
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func cloneResult(t *testing.T, in Result) Result {
	t.Helper()
	b, _ := json.Marshal(in)
	var out Result
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func marshalFixture(t *testing.T, value any) []byte {
	t.Helper()
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return append(b, '\n')
}

func marshalJSON(value any) []byte       { b, _ := json.Marshal(value); return b }
func stringPointer(value string) *string { return &value }

func assertDigest(t *testing.T, data []byte, want string) {
	t.Helper()
	if got := Digest(data); got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
}

func directoryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	sort.Strings(out)
	return out
}
