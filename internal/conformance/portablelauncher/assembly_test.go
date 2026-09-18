//go:build linux

package portablelauncher

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestTrustedAssemblyMaterializesAuthoritativeStructuralBlockedBundle(t *testing.T) {
	input := assemblyTestInput(t)
	stages, err := AssembleCommonStages(input)
	if err != nil {
		t.Fatalf("AssembleCommonStages: %v", err)
	}
	result, capture := CollectResult(CollectorInput{Run: input.Run, Pins: input.Pins, Record: input.Record, Stages: stages})
	if result.Summary.Verdict != "fail" || result.Summary.FirstFailedCase == nil || *result.Summary.FirstFailedCase != "blocked" {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if err := validateCollectedCapture(result, capture); err != nil {
		t.Fatalf("trusted assembled structural bundle did not validate: %v", err)
	}
	blocked := result.Stages[collectorStageIndex("blocked", "launch")]
	if blocked.Error == nil || blocked.Error.Code != "prerequisite.blocked_induction_unavailable" {
		t.Fatalf("blocked stage = %#v", blocked)
	}
}

func TestCanonicalScenarioOwnsEveryOrderedActionWithoutResultAuthorship(t *testing.T) {
	scenario := CanonicalScenario()
	for i, step := range scenario.Steps {
		if step.Sequence != i+1 || step.Action.Executor == "" || step.Action.Operation == "" || step.Action.Instruction == "" || step.Action.MinimumInvocations < 1 {
			t.Fatalf("step %d has incomplete canonical action: %#v", i, step)
		}
		if step.Action.Executor == "duo" && len(step.Action.Arguments) == 0 {
			t.Fatalf("step %d Duo action has no argv template", i)
		}
		lower := strings.ToLower(step.Action.Instruction + " " + step.Action.Operation)
		if strings.Contains(lower, "author result") || strings.Contains(lower, "write result") || strings.Contains(lower, "verdict") {
			t.Fatalf("step %d delegates result authorship: %#v", i, step.Action)
		}
	}
	blocked := scenario.Steps[collectorStageIndex("blocked", "launch")]
	if blocked.Action.Executor != "common_controller" || blocked.Action.Operation != "prerequisite.require_admitted_then_blocked" {
		t.Fatalf("blocked action = %#v", blocked.Action)
	}
}

func TestTrustedAssemblyRejectsCommandAndStageSequenceDrift(t *testing.T) {
	base := assemblyTestInput(t)
	tests := []struct {
		name      string
		mutate    func(*AssemblyInput)
		wantCode  string
		wantStage string
		wantCase  string
	}{
		{name: "missing command", mutate: func(in *AssemblyInput) { in.Commands = in.Commands[1:] }, wantCode: "assembly.command_missing_or_reordered", wantStage: "preflight", wantCase: "run"},
		{name: "reordered command", mutate: func(in *AssemblyInput) {
			in.Commands[0].Arguments, in.Commands[1].Arguments = in.Commands[1].Arguments, in.Commands[0].Arguments
		}, wantCode: "assembly.command_reordered", wantStage: "preflight", wantCase: "run"},
		{name: "extra command", mutate: func(in *AssemblyInput) {
			command := in.Commands[len(in.Commands)-1]
			cleanup := in.Timings[len(in.Timings)-1]
			command.StartOffsetNS = cleanup.StartedOffsetMS*int64(time.Millisecond) + int64(100*time.Microsecond)
			command.EndOffsetNS = command.StartOffsetNS + int64(100*time.Microsecond)
			in.Commands = append(in.Commands, command)
		}, wantCode: "assembly.command_extra", wantStage: "cleanup", wantCase: "run"},
		{name: "missing timing", mutate: func(in *AssemblyInput) { in.Timings = in.Timings[:len(in.Timings)-1] }, wantCode: "assembly.timing_missing", wantStage: "cleanup", wantCase: "run"},
		{name: "reordered timing", mutate: func(in *AssemblyInput) { in.Timings[3], in.Timings[4] = in.Timings[4], in.Timings[3] }, wantCode: "assembly.timing_reordered", wantStage: "launch", wantCase: "happy"},
		{name: "extra timing", mutate: func(in *AssemblyInput) { in.Timings = append(in.Timings, StageTimingFact{Sequence: 33}) }, wantCode: "assembly.timing_extra", wantStage: "cleanup", wantCase: "run"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := cloneAssemblyInput(t, base)
			test.mutate(&input)
			stages, err := AssembleCommonStages(input)
			if err == nil || !strings.Contains(err.Error(), test.wantCode) {
				t.Fatalf("error = %v, want %s", err, test.wantCode)
			}
			index := collectorStageIndex(test.wantCase, test.wantStage)
			if stages[index].Failure == nil || stages[index].Failure.Code != test.wantCode {
				t.Fatalf("attributed stage %s/%s = %#v", test.wantCase, test.wantStage, stages[index])
			}
		})
	}
}

func TestTrustedAssemblyRejectsCommandStatusAndPollingDrift(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*AssemblyInput)
		wantCode  string
		wantStage string
		wantCase  string
	}{
		{name: "successful stage exits nonzero", mutate: func(in *AssemblyInput) { in.Commands[0].ExitCode = 1 }, wantCode: "assembly.command_status", wantStage: "preflight", wantCase: "run"},
		{name: "conflict stage exits zero", mutate: func(in *AssemblyInput) {
			for i := range in.Commands {
				if operation, _ := recordedCommandOperation(in.Commands[i].Arguments); operation == registeredOperationName("prompt", "send") && containsString(in.Commands[i].Arguments, string(ConflictText)) {
					in.Commands[i].ExitCode = 0
					return
				}
			}
		}, wantCode: "assembly.command_status", wantStage: "send", wantCase: "same_key_different_text"},
		{name: "poll misses auxiliary", mutate: func(in *AssemblyInput) {
			observe := collectorStageIndex("happy", "observe")
			indexes := commandIndexesForStage(*in, observe)
			in.Commands = append(in.Commands[:indexes[0]], in.Commands[indexes[0]+1:]...)
		}, wantCode: "assembly.command_missing_or_reordered", wantStage: "observe", wantCase: "happy"},
		{name: "poll pair reordered", mutate: func(in *AssemblyInput) {
			observe := collectorStageIndex("happy", "observe")
			indexes := commandIndexesForStage(*in, observe)
			first, second := indexes[0], indexes[1]
			in.Commands[first].Arguments, in.Commands[second].Arguments = in.Commands[second].Arguments, in.Commands[first].Arguments
		}, wantCode: "assembly.command_reordered", wantStage: "observe", wantCase: "happy"},
		{name: "command crosses stage boundary", mutate: func(in *AssemblyInput) {
			preflight := collectorStageIndex("run", "preflight")
			in.Commands[0].EndOffsetNS = (in.Timings[preflight].StartedOffsetMS+in.Timings[preflight].DurationMS)*int64(time.Millisecond) + 1
		}, wantCode: "assembly.command_extra", wantStage: "preflight", wantCase: "run"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := assemblyTestInput(t)
			test.mutate(&input)
			stages, err := AssembleCommonStages(input)
			if err == nil || !strings.Contains(err.Error(), test.wantCode) {
				t.Fatalf("error = %v, want %s", err, test.wantCode)
			}
			index := collectorStageIndex(test.wantCase, test.wantStage)
			if stages[index].Failure == nil || stages[index].Failure.Code != test.wantCode {
				t.Fatalf("attributed stage %s/%s = %#v", test.wantCase, test.wantStage, stages[index])
			}
		})
	}
}

func TestTrustedAssemblyRejectsLauncherInputChannels(t *testing.T) {
	for _, source := range []string{"terminal", "terminal_paste", "paste", "tool_input"} {
		t.Run(source, func(t *testing.T) {
			input := assemblyTestInput(t)
			input.LauncherEvents = append(input.LauncherEvents, LauncherEventFact{Kind: "tool.input", InputSource: source})
			stages, err := AssembleCommonStages(input)
			if err == nil || !strings.Contains(err.Error(), "terminal/paste/tool-input") {
				t.Fatalf("error = %v", err)
			}
			index := collectorStageIndex("run", "discovery")
			if stages[index].Failure == nil || stages[index].Failure.Code != "assembly.launcher_event_rejected" {
				t.Fatalf("discovery attribution = %#v", stages[index])
			}
		})
	}
	t.Run("raw launcher stream", func(t *testing.T) {
		input := assemblyTestInput(t)
		input.Record.Capture.Events = []byte(`{"kind":"tool.input","input_source":"terminal"}`)
		stages, err := AssembleCommonStages(input)
		if err == nil || !strings.Contains(err.Error(), "terminal/paste/tool-input") {
			t.Fatalf("error = %v", err)
		}
		index := collectorStageIndex("run", "discovery")
		if stages[index].Failure == nil || stages[index].Failure.Code != "assembly.launcher_event_rejected" {
			t.Fatalf("discovery attribution = %#v", stages[index])
		}
	})
}

func TestTrustedAssemblyAttributesEveryStageDeadline(t *testing.T) {
	for i, step := range CanonicalScenario().Steps {
		t.Run(fmt.Sprintf("%02d-%s-%s", step.Sequence, step.Case, step.Stage), func(t *testing.T) {
			input := assemblyTestInput(t)
			limit := step.DeadlineMS
			if step.Case == "timeout" && step.Stage == "observe" {
				limit = CanonicalOracle().TimeoutMaximumMS
			}
			oldEnd := input.Timings[i].StartedOffsetMS + input.Timings[i].DurationMS
			delta := limit + 1 - input.Timings[i].DurationMS
			input.Timings[i].DurationMS = limit + 1
			for j := i + 1; j < len(input.Timings); j++ {
				input.Timings[j].StartedOffsetMS += delta
			}
			for j := range input.Commands {
				if input.Commands[j].StartOffsetNS >= oldEnd*int64(time.Millisecond) {
					input.Commands[j].StartOffsetNS += delta * int64(time.Millisecond)
					input.Commands[j].EndOffsetNS += delta * int64(time.Millisecond)
				}
			}
			stages, err := AssembleCommonStages(input)
			if err != nil {
				t.Fatalf("deadline failure should materialize, got rejection: %v", err)
			}
			if stages[i].Failure == nil || stages[i].Failure.Code != "stage.deadline_exceeded" {
				t.Fatalf("stage %d attribution = %#v", i, stages[i])
			}
		})
	}
}

func TestTrustedAssemblyMaterializesCompleteRunDeadline(t *testing.T) {
	input := assemblyTestInput(t)
	preflight := collectorStageIndex("run", "preflight")
	oldDuration := input.Timings[preflight].DurationMS
	input.Timings[preflight].DurationMS = CanonicalScenario().DeadlinesMS["complete_run"] + 1
	delta := input.Timings[preflight].DurationMS - oldDuration
	for i := preflight + 1; i < len(input.Timings); i++ {
		input.Timings[i].StartedOffsetMS += delta
	}
	input.Commands = nil
	input.Record.CompleteRunTimedOut = true
	input.Record.RunnerError = fmt.Errorf("complete-run deadline exceeded")
	stages, err := AssembleCommonStages(input)
	if err != nil {
		t.Fatalf("complete-run deadline should materialize, got rejection: %v", err)
	}
	if stages[preflight].Failure == nil || stages[preflight].Failure.Code != "run.deadline_exceeded" {
		t.Fatalf("preflight deadline attribution = %#v", stages[preflight])
	}
	result, capture := CollectResult(CollectorInput{Run: input.Run, Pins: input.Pins, Record: input.Record, Stages: stages})
	if err := validateCollectedCapture(result, capture); err != nil {
		t.Fatalf("materialized complete-run deadline bundle did not validate: %v", err)
	}
}

func TestDynamicDuoCommitPinValidation(t *testing.T) {
	valid := fixturePins().Duo
	for _, commit := range []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"0123456789abcdef0123456789abcdef01234567",
	} {
		pin := valid
		pin.Commit = commit
		if !validDuoPin(pin) {
			t.Fatalf("valid dynamic commit %q rejected", commit)
		}
	}
	for _, commit := range []string{"", "too-short", strings.Repeat("A", 40), strings.Repeat("g", 40), strings.Repeat("0", 39), strings.Repeat("0", 40)} {
		pin := valid
		pin.Commit = commit
		if validDuoPin(pin) {
			t.Fatalf("invalid exact commit %q accepted", commit)
		}
	}
}

func assemblyTestInput(t *testing.T) AssemblyInput {
	t.Helper()
	pins := fixturePins()
	input := AssemblyInput{
		Run:   RunIdentity{RunID: "assembly-test", ObservedAt: "2026-09-18T00:00:00Z", HostOS: "linux", HostArch: "x86_64", FixtureRoot: "$RUN"},
		Pins:  pins,
		Setup: SetupFact{Isolated: true, CredentialPresent: true, CredentialProvider: "openai-codex", CredentialSourceKind: "operator_copy", PiNoExtensions: true},
		LauncherEvents: []LauncherEventFact{{
			Kind: "skill.discovery", InputSource: "task_argument", TaskDigest: Digest(CanonicalTaskBytes), Arguments: []string{"fixture-launcher", TaskArgument}, Skill: pins.Skill,
		}},
		Preflight:   PreflightFact{Status: "ready", RequiredChecks: 7, AllRequiredPassed: true},
		Restart:     RestartFact{Outcome: "same_live", DistinctProcesses: true, SameStore: true, Attempts: 1, BaselineRecordID: "record_fixture"},
		Idempotency: IdempotencyFact{SameCommand: true, Attempts: 1, NewTurns: 0, OriginalCommandID: "cmd_fixture"},
		CommandDeltas: []CommandDeltaFact{
			{Case: "same_key_same_text"}, {Case: "same_key_different_text"},
		},
		Exited:                ExitedFact{Condition: "exited", SameInstance: true, ProcessSucceeded: true, Attempts: 1},
		Timeout:               TimeoutEvidence{ElapsedMS: 20_000, ProcessSucceeded: true, StoppedVerified: true, Delivered: true, Attempts: 1},
		TimeoutProbeSupported: true,
		Cleanup:               CleanupFact{RootRemoved: true, ExportComplete: true},
	}
	input.Record.Controls = []RecordedControl{
		{Action: ControlSuspendExactProcess, Case: RunnerCaseExited, Requested: exitedIdentity, Result: ControlCheckpoint{Target: exitedIdentity, StoppedVerified: true, MonotonicMS: 1}},
		{Action: ControlCloseExactPane, Case: RunnerCaseExited, Requested: exitedIdentity, Result: ControlCheckpoint{Target: exitedIdentity, StoppedVerified: true, MonotonicMS: 2}},
		{Action: ControlSuspendExactProcess, Case: RunnerCaseTimeout, Requested: timeoutIdentity, Result: ControlCheckpoint{Target: timeoutIdentity, StoppedVerified: true, MonotonicMS: 3}},
	}
	for _, caseName := range []string{"happy", "restart", "exited", "timeout"} {
		input.Binds = append(input.Binds, BindFact{Case: caseName, AuthorityInstances: []string{"run_fixture"}, ActiveCorrelations: []string{"run_fixture"}})
		input.CommandIntegrity = append(input.CommandIntegrity, CommandIntegrityFact{Case: caseName, Unchanged: true})
	}

	var offset int64
	for _, step := range CanonicalScenario().Steps {
		duration := int64(1)
		if step.Case == "timeout" && step.Stage == "observe" {
			duration = 20_000
		}
		input.Timings = append(input.Timings, StageTimingFact{Sequence: step.Sequence, StartedOffsetMS: offset, DurationMS: duration})
		stageStart := offset * int64(time.Millisecond)
		offset += duration
		if step.Action.Executor != "duo" || (step.Case == "blocked" && blockedPrerequisiteUnavailable()) {
			continue
		}
		arguments := resolveAssemblyArguments(step.Action.Arguments)
		stdout := []byte("{}\n")
		id := step.Assertions[1].ID
		if evidenceSource(id) == "duo_envelope" {
			actual := step.Assertions[1].Expected
			wrapper := duoEvidenceDocument(t, id, step.Sequence, actual)
			envelope := wrapper["envelope"]
			var err error
			stdout, err = json.Marshal(envelope)
			if err != nil {
				t.Fatal(err)
			}
			stdout = append(stdout, '\n')
		}
		commandOffset := stageStart + int64(100*time.Microsecond)
		if len(step.Action.AuxiliaryOperations) != 0 {
			auxiliary := []string{"session", "show", arguments[2], "--output", "json"}
			input.Commands = append(input.Commands, RecordedCommand{
				Arguments: auxiliary, Stdout: []byte("{}\n"), StartOffsetNS: commandOffset, EndOffsetNS: commandOffset + int64(100*time.Microsecond),
			})
			commandOffset += int64(200 * time.Microsecond)
		}
		exitCode := 0
		if step.Case == "same_key_different_text" && step.Stage == "send" {
			exitCode = 1
		}
		input.Commands = append(input.Commands, RecordedCommand{
			Arguments: arguments, Stdout: stdout, ExitCode: exitCode,
			StartOffsetNS: commandOffset, EndOffsetNS: commandOffset + int64(100*time.Microsecond),
		})
	}
	return input
}

func commandIndexesForStage(input AssemblyInput, stage int) []int {
	timing := input.Timings[stage]
	start := timing.StartedOffsetMS * int64(time.Millisecond)
	end := (timing.StartedOffsetMS + timing.DurationMS) * int64(time.Millisecond)
	var indexes []int
	for i, command := range input.Commands {
		if command.StartOffsetNS >= start && command.EndOffsetNS <= end {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func resolveAssemblyArguments(arguments []string) []string {
	out := append([]string(nil), arguments...)
	for i, value := range out {
		switch value {
		case "$WORKSPACE":
			out[i] = "$WORKSPACE"
		case "herdr:$RUN/herdr/herdr.sock":
			out[i] = value
		case "$CANONICAL_PROMPT":
			out[i] = string(PromptBytes)
		case "$CONFLICT_TEXT":
			out[i] = string(ConflictText)
		case "$PRIMARY_KEY":
			out[i] = CanonicalScenario().Fixture.IdempotencyKey
		case "$EXPIRES_AT":
			out[i] = "2026-09-18T00:10:00Z"
		default:
			if strings.HasPrefix(value, "$") {
				out[i] = "fixture-binding"
			}
		}
	}
	return out
}

func cloneAssemblyInput(t *testing.T, input AssemblyInput) AssemblyInput {
	t.Helper()
	b, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	var out AssemblyInput
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(input, out) {
		t.Fatal("assembly fixture clone changed value")
	}
	return out
}
