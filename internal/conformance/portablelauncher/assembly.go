//go:build linux

package portablelauncher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/registry"
)

// StageTimingFact is a common monotonic stage boundary. Sequence is checked
// against the canonical scenario; it does not select scenario semantics.
type StageTimingFact struct {
	Sequence        int
	StartedOffsetMS int64
	DurationMS      int64
}

// SetupFact contains only facts observed while preparing the isolated fixture.
type SetupFact struct {
	Isolated             bool
	CredentialPresent    bool
	CredentialProvider   string
	CredentialSourceKind string
	PiNoExtensions       bool
}

// LauncherEventFact is a normalized launcher event. Normalization remains a
// launcher integration responsibility; this common boundary accepts no
// verdict, expected value, terminal bytes, or free-form observation.
type LauncherEventFact struct {
	Kind        string
	InputSource string
	TaskDigest  string
	Arguments   []string
	Skill       SkillPin
}

// PreflightFact is the already-produced Step 05 diagnostic summary. The
// conformance suite records it but does not duplicate doctor checks.
type PreflightFact struct {
	Status            string
	RequiredChecks    int
	AllRequiredPassed bool
}

// BindFact links one inspected Duo envelope to independently projected
// authority relationships for the named canonical case.
type BindFact struct {
	Case               string
	AuthorityInstances []string
	ActiveCorrelations []string
}

// RestartFact records authority-process continuity and the conversation
// baseline used after reopening the same store.
type RestartFact struct {
	Outcome           string
	DistinctProcesses bool
	SameStore         bool
	Attempts          int
	BaselineRecordID  string
}

// IdempotencyFact records independently counted effects around the two
// idempotency actions.
type IdempotencyFact struct {
	SameCommand       bool
	Attempts          int
	NewTurns          int
	OriginalCommandID string
}

// CommandDeltaFact is an authority/controller before/after count for a
// canonical idempotency case.
type CommandDeltaFact struct {
	Case          string
	CommandsDelta int
	AttemptsDelta int
	QueueDelta    int
	TurnsDelta    int
}

// CommandIntegrityFact records whether a case's command projection remained
// unchanged across the associated non-happy observation.
type CommandIntegrityFact struct {
	Case      string
	Unchanged bool
}

// ExitedFact is the exact lifecycle observation made after controller close.
type ExitedFact struct {
	Condition        string
	SameInstance     bool
	ProcessSucceeded bool
	Attempts         int
}

// CleanupFact records independently inspected resources and export state.
type CleanupFact struct {
	PIDsRemaining    int
	SocketsRemaining int
	PanesRemaining   int
	RootRemoved      bool
	ExportComplete   bool
}

// AssemblyInput contains validated raw facts available at the common suite
// boundary. AssembleCommonStages is the only production constructor of
// Observation values.
type AssemblyInput struct {
	Run                   RunIdentity
	Pins                  Pins
	Record                RunRecord
	Timings               []StageTimingFact
	Commands              []RecordedCommand
	LauncherEvents        []LauncherEventFact
	Setup                 SetupFact
	Preflight             PreflightFact
	Binds                 []BindFact
	Restart               RestartFact
	Idempotency           IdempotencyFact
	CommandDeltas         []CommandDeltaFact
	CommandIntegrity      []CommandIntegrityFact
	Exited                ExitedFact
	Timeout               TimeoutEvidence
	TimeoutProbeSupported bool
	Cleanup               CleanupFact
}

// AssembleCommonStages derives canonical stage captures from typed raw facts.
// It returns complete captures even on rejection so tests and callers can
// inspect first-stage attribution; a non-nil error means the captures must not
// be published as a successful materialization.
func AssembleCommonStages(input AssemblyInput) ([]CommonStageCapture, error) {
	scenario := CanonicalScenario()
	stages := make([]CommonStageCapture, len(scenario.Steps))
	var firstErr error
	firstFailureIndex := -1
	firstFailureRejected := false
	setFailure := func(index int, code, message string, reject bool) {
		if reject && firstErr == nil {
			firstErr = fmt.Errorf("%s: %s", code, message)
		}
		if firstFailureIndex >= 0 && index > firstFailureIndex {
			return
		}
		if firstFailureIndex == index && firstFailureRejected {
			return
		}
		if firstFailureIndex >= 0 && index < firstFailureIndex {
			stages[firstFailureIndex].Failure = nil
		}
		firstFailureIndex = index
		firstFailureRejected = reject
		stages[index].Failure = &StageError{
			Code: code, Message: message, Effect: "unknown_effect",
			Retry: Retry{Safe: false, Action: "inspect_originating_failure"},
		}
	}
	reject := func(index int, code, message string) { setFailure(index, code, message, true) }
	failRun := func(index int, code, message string) { setFailure(index, code, message, false) }

	var priorEnd int64
	for i, step := range scenario.Steps {
		stages[i].Sequence = step.Sequence
		if i >= len(input.Timings) {
			stages[i].StartedOffsetMS = priorEnd
			reject(i, "assembly.timing_missing", "canonical stage timing is missing")
			continue
		}
		timing := input.Timings[i]
		if timing.Sequence != step.Sequence {
			stages[i].StartedOffsetMS = priorEnd
			reject(i, "assembly.timing_reordered", fmt.Sprintf("timing sequence %d does not match canonical sequence %d", timing.Sequence, step.Sequence))
			continue
		}
		stages[i].StartedOffsetMS = timing.StartedOffsetMS
		stages[i].DurationMS = timing.DurationMS
		if timing.StartedOffsetMS < priorEnd || timing.DurationMS < 0 {
			stages[i].StartedOffsetMS, stages[i].DurationMS = priorEnd, 0
			reject(i, "assembly.timing_invalid", "stage timing is negative or non-monotonic")
		}
		priorEnd = stages[i].StartedOffsetMS + stages[i].DurationMS
	}
	if len(input.Timings) > len(stages) {
		reject(len(stages)-1, "assembly.timing_extra", "timing exists outside the canonical stage sequence")
	}

	for i, step := range scenario.Steps {
		deadline := step.DeadlineMS
		if step.Case == "timeout" && step.Stage == "observe" {
			deadline = CanonicalOracle().TimeoutMaximumMS
		}
		stages[i].Observations = append(stages[i].Observations, Observation{
			Sequence: step.Sequence, AssertionID: "stage.completed_within_deadline", Source: "controller",
			Evidence: map[string]any{
				"kind": "controller.checkpoint", "monotonic_ms": float64(stages[i].StartedOffsetMS),
				"started_ms": float64(stages[i].StartedOffsetMS), "finished_ms": float64(stages[i].StartedOffsetMS + stages[i].DurationMS),
				"deadline_ms": float64(deadline),
			},
		})
		if stages[i].DurationMS > deadline {
			failRun(i, "stage.deadline_exceeded", fmt.Sprintf("%s/%s exceeded its %dms deadline", step.Case, step.Stage, deadline))
		}
	}

	commandsByStage, commandErr := assembleCommandSequence(scenario, input.Timings, input.Commands)
	if commandErr != nil {
		if hasRecordedRunFailure(input.Record) {
			code := "run.incomplete"
			message := "the recorded run ended before the next canonical command completed"
			if input.Record.CompleteRunTimedOut {
				code = "run.deadline_exceeded"
				message = recordedRunFailureMessage(input.Record)
			}
			failRun(commandErr.index, code, message)
		} else {
			reject(commandErr.index, commandErr.code, commandErr.message)
		}
	} else if hasRecordedRunFailure(input.Record) {
		failRun(lastReachedStage(commandsByStage), recordedRunFailureCode(input.Record), recordedRunFailureMessage(input.Record))
	}

	if index, err := validateLauncherEvents(input.LauncherEvents); err != nil {
		reject(index, "assembly.launcher_event_rejected", err.Error())
	}
	if err := rejectRawLauncherInputEvidence(input.Record.Capture.Events); err != nil {
		reject(collectorStageIndexForAssembly("run", "discovery"), "assembly.launcher_event_rejected", err.Error())
	}

	// Derive semantic observations only until the first bad source. Cleanup is
	// always derived independently below, even when an earlier stage failed.
	for i, step := range scenario.Steps {
		if step.Stage == "cleanup" || (step.Case == "blocked" && blockedPrerequisiteUnavailable()) {
			continue
		}
		if firstErr != nil || (firstFailureIndex >= 0 && i >= firstFailureIndex) {
			continue
		}
		observation, err := assembleSemanticObservation(input, step, commandsByStage[i])
		if err != nil {
			reject(i, "assembly.source_invalid", err.Error())
			continue
		}
		stages[i].Observations = append(stages[i].Observations, observation)
	}

	cleanupIndex := len(stages) - 1
	cleanupStep := scenario.Steps[cleanupIndex]
	stages[cleanupIndex].Observations = append(stages[cleanupIndex].Observations, controllerObservation(cleanupStep, map[string]any{
		"pids_remaining": float64(input.Cleanup.PIDsRemaining), "sockets_remaining": float64(input.Cleanup.SocketsRemaining),
		"panes_remaining": float64(input.Cleanup.PanesRemaining), "root_removed": input.Cleanup.RootRemoved,
		"export_complete": input.Cleanup.ExportComplete,
	}, nil))
	return stages, firstErr
}

type commandAssemblyError struct {
	index   int
	code    string
	message string
}

func assembleCommandSequence(scenario Scenario, timings []StageTimingFact, commands []RecordedCommand) (map[int][]RecordedCommand, *commandAssemblyError) {
	byStage := make(map[int][]RecordedCommand)
	if len(timings) < len(scenario.Steps) {
		return byStage, &commandAssemblyError{len(timings), "assembly.timing_missing", "command attribution requires every canonical stage timing"}
	}
	var priorEndNS int64
	for position, command := range commands {
		if command.StartOffsetNS < priorEndNS || command.EndOffsetNS < command.StartOffsetNS {
			return byStage, &commandAssemblyError{stageIndexForCommandOffset(timings, command.StartOffsetNS), "assembly.command_order", fmt.Sprintf("command %d overlaps or regresses", position+1)}
		}
		priorEndNS = command.EndOffsetNS
		stageIndex := stageIndexForCommandInterval(timings, command.StartOffsetNS, command.EndOffsetNS)
		if stageIndex < 0 || stageIndex >= len(scenario.Steps) {
			return byStage, &commandAssemblyError{stageIndexForCommandOffset(timings, command.StartOffsetNS), "assembly.command_extra", fmt.Sprintf("command %d is outside every canonical stage boundary", position+1)}
		}
		step := scenario.Steps[stageIndex]
		if step.Action.Executor != "duo" || (step.Case == "blocked" && blockedPrerequisiteUnavailable()) {
			return byStage, &commandAssemblyError{stageIndex, "assembly.command_extra", fmt.Sprintf("command %d occurred during non-Duo stage %s/%s", position+1, step.Case, step.Stage)}
		}
		byStage[stageIndex] = append(byStage[stageIndex], command)
	}

	for i, step := range scenario.Steps {
		if step.Action.Executor != "duo" || (step.Case == "blocked" && blockedPrerequisiteUnavailable()) {
			continue
		}
		stageCommands := byStage[i]
		primary := 0
		auxiliary := make(map[string]int, len(step.Action.AuxiliaryOperations))
		for position, command := range stageCommands {
			operation, ok := recordedCommandOperation(command.Arguments)
			if !ok {
				return byStage, &commandAssemblyError{i, "assembly.command_unknown", fmt.Sprintf("command %d in %s/%s has no registered operation", position+1, step.Case, step.Stage)}
			}
			switch {
			case operation == step.Action.Operation:
				if err := validateCanonicalArguments(step.Action, command.Arguments); err != nil {
					return byStage, &commandAssemblyError{i, "assembly.command_arguments", err.Error()}
				}
				primary++
			case containsString(step.Action.AuxiliaryOperations, operation):
				if err := validateAuxiliaryArguments(step.Action, operation, command.Arguments); err != nil {
					return byStage, &commandAssemblyError{i, "assembly.command_arguments", err.Error()}
				}
				auxiliary[operation]++
			default:
				return byStage, &commandAssemblyError{i, "assembly.command_reordered", fmt.Sprintf("%s is not allowed during %s/%s", operation, step.Case, step.Stage)}
			}
			if err := validateRecordedCommandStatus(step, operation, command); err != nil {
				return byStage, &commandAssemblyError{i, "assembly.command_status", err.Error()}
			}
		}
		if primary < step.Action.MinimumInvocations {
			return byStage, &commandAssemblyError{i, "assembly.command_missing_or_reordered", fmt.Sprintf("%s/%s requires at least %d %s invocation(s)", step.Case, step.Stage, step.Action.MinimumInvocations, step.Action.Operation)}
		}
		if step.Action.MaximumInvocations > 0 && primary > step.Action.MaximumInvocations {
			return byStage, &commandAssemblyError{i, "assembly.command_extra", fmt.Sprintf("%s has more than %d canonical invocations", step.Action.Operation, step.Action.MaximumInvocations)}
		}
		for _, operation := range step.Action.AuxiliaryOperations {
			if auxiliary[operation] < step.Action.MinimumInvocations {
				return byStage, &commandAssemblyError{i, "assembly.command_missing_or_reordered", fmt.Sprintf("%s/%s requires %s alongside %s", step.Case, step.Stage, operation, step.Action.Operation)}
			}
		}
		if len(step.Action.AuxiliaryOperations) == 1 {
			if err := validatePollingPairs(step, stageCommands, step.Action.AuxiliaryOperations[0]); err != nil {
				return byStage, &commandAssemblyError{i, "assembly.command_reordered", err.Error()}
			}
		}
	}
	return byStage, nil
}

func stageIndexForCommandInterval(timings []StageTimingFact, startNS, endNS int64) int {
	matched := -1
	for i, timing := range timings {
		stageStart := timing.StartedOffsetMS * int64(time.Millisecond)
		stageEnd := (timing.StartedOffsetMS + timing.DurationMS) * int64(time.Millisecond)
		if startNS < stageStart || endNS > stageEnd {
			continue
		}
		if matched >= 0 {
			return -1
		}
		matched = i
	}
	return matched
}

func stageIndexForCommandOffset(timings []StageTimingFact, offsetNS int64) int {
	for i, timing := range timings {
		if offsetNS < (timing.StartedOffsetMS+timing.DurationMS)*int64(time.Millisecond) {
			return i
		}
	}
	if len(timings) == 0 {
		return 0
	}
	return len(timings) - 1
}

func validatePollingPairs(step StepSpec, commands []RecordedCommand, auxiliary string) error {
	if len(commands)%2 != 0 {
		return fmt.Errorf("%s/%s polling must contain complete auxiliary/primary pairs", step.Case, step.Stage)
	}
	for i, command := range commands {
		operation, _ := recordedCommandOperation(command.Arguments)
		want := auxiliary
		if i%2 == 1 {
			want = step.Action.Operation
		}
		if operation != want {
			return fmt.Errorf("%s/%s polling command %d is %s, want %s", step.Case, step.Stage, i+1, operation, want)
		}
	}
	return nil
}

func validateRecordedCommandStatus(step StepSpec, operation string, command RecordedCommand) error {
	expectedFailure := step.Case == "same_key_different_text" && step.Stage == "send" && operation == step.Action.Operation
	if command.Signal != 0 || command.SignalName != "" {
		return fmt.Errorf("%s/%s command was terminated by a signal", step.Case, step.Stage)
	}
	if expectedFailure && command.ExitCode == 0 {
		return fmt.Errorf("%s/%s expected a nonzero schema-valid conflict command", step.Case, step.Stage)
	}
	if !expectedFailure && command.ExitCode != 0 {
		return fmt.Errorf("%s/%s command exited nonzero", step.Case, step.Stage)
	}
	return nil
}

func hasRecordedRunFailure(record RunRecord) bool {
	return record.RunnerError != nil || record.LauncherError != nil || record.CheckpointSourceError != nil || record.CompleteRunTimedOut
}

func recordedRunFailureMessage(record RunRecord) string {
	switch {
	case record.CompleteRunTimedOut:
		return "the complete outer run deadline expired at the last common checkpoint"
	case record.LauncherError != nil:
		return "the outer launcher failed after the last completed canonical command"
	case record.CheckpointSourceError != nil:
		return "the trusted checkpoint source failed after the last completed canonical command"
	default:
		return "the common runner failed after the last completed canonical command"
	}
}

func recordedRunFailureCode(record RunRecord) string {
	if record.CompleteRunTimedOut {
		return "run.deadline_exceeded"
	}
	return "run.failed"
}

func lastReachedStage(commandsByStage map[int][]RecordedCommand) int {
	last := collectorStageIndexForAssembly("run", "discovery")
	for index, commands := range commandsByStage {
		if len(commands) != 0 && index > last {
			last = index
		}
	}
	return last
}

func validateAuxiliaryArguments(primary ActionSpec, operation string, arguments []string) error {
	if operation != registeredOperationName("session", "show") || len(primary.Arguments) < 3 {
		return fmt.Errorf("%s is not a canonical auxiliary operation for %s", operation, primary.Operation)
	}
	action := ActionSpec{
		Operation: operation,
		Arguments: []string{"session", "show", primary.Arguments[2], "--output", "json"},
	}
	return validateCanonicalArguments(action, arguments)
}

func recordedCommandOperation(arguments []string) (string, bool) {
	bestLength := 0
	operation := ""
	for _, descriptor := range registry.All() {
		if len(descriptor.CLI) > len(arguments) || len(descriptor.CLI) <= bestLength {
			continue
		}
		match := true
		for i := range descriptor.CLI {
			match = match && arguments[i] == descriptor.CLI[i]
		}
		if match {
			bestLength, operation = len(descriptor.CLI), descriptor.Name
		}
	}
	return operation, operation != ""
}

func validateCanonicalArguments(action ActionSpec, arguments []string) error {
	if len(arguments) != len(action.Arguments) {
		return fmt.Errorf("%s argv length %d does not match canonical length %d", action.Operation, len(arguments), len(action.Arguments))
	}
	for i, want := range action.Arguments {
		got := arguments[i]
		switch want {
		case "$CANONICAL_PROMPT":
			want = string(PromptBytes)
		case "$CONFLICT_TEXT":
			want = string(ConflictText)
		case "$PRIMARY_KEY":
			want = CanonicalScenario().Fixture.IdempotencyKey
		case "$EXPIRES_AT":
			if parsed, err := time.Parse(time.RFC3339, got); err != nil || parsed.IsZero() {
				return fmt.Errorf("%s argv[%d] is not a finite RFC3339 expiry", action.Operation, i)
			}
			continue
		case "$WORKSPACE":
			if got != "$WORKSPACE" && !strings.HasSuffix(got, "/workspace") {
				return fmt.Errorf("%s argv[%d] is not the fixture workspace", action.Operation, i)
			}
			continue
		case "herdr:$RUN/herdr/herdr.sock":
			if got != want && !strings.HasSuffix(got, "/herdr/herdr.sock") {
				return fmt.Errorf("%s argv[%d] is not the fixture Herdr socket", action.Operation, i)
			}
			continue
		default:
			if strings.HasPrefix(want, "$") {
				if got == "" {
					return fmt.Errorf("%s argv[%d] has an empty canonical binding", action.Operation, i)
				}
				continue
			}
		}
		if got != want {
			return fmt.Errorf("%s argv[%d] = %q, want canonical %q", action.Operation, i, got, want)
		}
	}
	return nil
}

func validateLauncherEvents(events []LauncherEventFact) (int, error) {
	discoveryIndex := collectorStageIndexForAssembly("run", "discovery")
	discoveryCount := 0
	for _, event := range events {
		input := strings.ToLower(event.InputSource)
		kind := strings.ToLower(event.Kind)
		if input == "terminal" || input == "terminal_paste" || input == "paste" || input == "tool_input" || strings.Contains(kind, "terminal") || strings.Contains(kind, "paste") || kind == "tool.input" {
			return discoveryIndex, fmt.Errorf("launcher terminal/paste/tool-input event is forbidden")
		}
		if event.Kind == "skill.discovery" {
			discoveryCount++
		}
	}
	if discoveryCount != 1 {
		return discoveryIndex, fmt.Errorf("launcher emitted %d skill discovery events, want exactly one", discoveryCount)
	}
	return discoveryIndex, nil
}

func rejectRawLauncherInputEvidence(events []byte) error {
	lower := strings.ToLower(string(events))
	for _, marker := range []string{
		"terminal_snapshot", "terminal-paste", "terminal_paste", "send_keys", "pane.send_text", "pane.send_keys",
		`"kind":"tool.input"`, `"kind": "tool.input"`, `"input_source":"terminal"`, `"input_source": "terminal"`,
		`"input_source":"paste"`, `"input_source": "paste"`, `"input_source":"tool_input"`, `"input_source": "tool_input"`,
	} {
		if strings.Contains(lower, marker) {
			return fmt.Errorf("raw launcher stream contains forbidden terminal/paste/tool-input evidence")
		}
	}
	return nil
}

func assembleSemanticObservation(input AssemblyInput, step StepSpec, commands []RecordedCommand) (Observation, error) {
	id := step.Assertions[1].ID
	switch id {
	case "setup.fixture_isolated_and_pinned":
		pinsJSON, err := json.Marshal(input.Pins)
		if err != nil {
			return Observation{}, err
		}
		measurements := map[string]any{
			"fixture_root": input.Run.FixtureRoot, "shared_state": !input.Setup.Isolated,
			"plugins_enabled": input.Run.PluginsEnabled, "mcp_enabled": input.Run.MCPEnabled,
			"terminal_input_used": input.Run.TerminalInputUsed,
		}
		extra := map[string]any{
			"pins_digest": Digest(pinsJSON), "credentials_present": input.Setup.CredentialPresent,
			"credential_provider": input.Setup.CredentialProvider, "credential_source_kind": input.Setup.CredentialSourceKind,
			"pi_no_extensions": input.Setup.PiNoExtensions,
		}
		return controllerObservation(step, measurements, extra), nil
	case "discovery.skill_identity":
		for _, event := range input.LauncherEvents {
			if event.Kind != "skill.discovery" {
				continue
			}
			return Observation{Sequence: step.Sequence, AssertionID: id, Source: "launcher_event", Evidence: map[string]any{
				"kind": event.Kind, "task_digest": event.TaskDigest, "arguments": stringsToAny(event.Arguments),
				"skill": map[string]any{"name": event.Skill.Name, "format_version": event.Skill.FormatVersion, "content_digest": event.Skill.ContentDigest},
			}}, nil
		}
		return Observation{}, fmt.Errorf("skill discovery event is missing")
	case "preflight.ready":
		return controllerObservation(step, map[string]any{
			"status": input.Preflight.Status, "required_checks": float64(input.Preflight.RequiredChecks), "all_required_pass": input.Preflight.AllRequiredPassed,
		}, nil), nil
	case "restart.authority_continuity":
		return controllerObservation(step, map[string]any{
			"outcome": input.Restart.Outcome, "distinct_processes": input.Restart.DistinctProcesses,
			"same_store": input.Restart.SameStore, "attempts": float64(input.Restart.Attempts),
		}, nil), nil
	case "send.idempotent_replay":
		return controllerObservation(step, map[string]any{
			"same_command": input.Idempotency.SameCommand, "attempts": float64(input.Idempotency.Attempts), "new_turns": float64(input.Idempotency.NewTurns),
		}, nil), nil
	case "command.idempotent_no_duplication", "command.idempotency_no_drift":
		delta, ok := commandDelta(input.CommandDeltas, step.Case)
		if !ok {
			return Observation{}, fmt.Errorf("authority command delta for %s is missing", step.Case)
		}
		measurements := map[string]any{"commands_delta": float64(delta.CommandsDelta), "attempts_delta": float64(delta.AttemptsDelta), "turns_delta": float64(delta.TurnsDelta)}
		if id == "command.idempotency_no_drift" {
			measurements["queue_delta"] = float64(delta.QueueDelta)
		}
		return controllerObservation(step, measurements, nil), nil
	case "observe.exited":
		targetVerified := verifiedControls(input.Record, RunnerCaseExited, ControlSuspendExactProcess, ControlCloseExactPane)
		if !targetVerified {
			return Observation{}, fmt.Errorf("exited assertion lacks the ordered verified controller checkpoints")
		}
		measurements := map[string]any{
			"condition": input.Exited.Condition, "same_instance": input.Exited.SameInstance,
			"process_success": input.Exited.ProcessSucceeded, "attempts": float64(input.Exited.Attempts),
		}
		return controllerObservation(step, measurements, map[string]any{"target_verified": targetVerified}), nil
	case "observe.timeout":
		targetVerified := verifiedControls(input.Record, RunnerCaseTimeout, ControlSuspendExactProcess)
		if !targetVerified {
			return Observation{}, fmt.Errorf("timeout assertion lacks the verified suspension checkpoint")
		}
		measurements := map[string]any{
			"elapsed_min_ms": float64(CanonicalOracle().TimeoutMinimumMS), "elapsed_max_ms": float64(CanonicalOracle().TimeoutMaximumMS),
			"elapsed_ms": float64(input.Timeout.ElapsedMS), "process_success": input.Timeout.ProcessSucceeded,
			"outer_timed_out": input.Timeout.OuterTimedOut, "stopped_verified": targetVerified,
			"delivered": input.Timeout.Delivered, "attempts": float64(input.Timeout.Attempts), "assistant_blocks": float64(input.Timeout.AssistantBlocks),
		}
		return controllerObservation(step, measurements, map[string]any{"target_verified": targetVerified, "timeout_probe_supported": input.TimeoutProbeSupported}), nil
	}

	command, err := finalPrimaryCommand(step.Action, commands)
	if err != nil {
		return Observation{}, err
	}
	envelope, err := decodeCommandEnvelope(command, step.Action.Operation)
	if err != nil {
		return Observation{}, err
	}
	wrapper := map[string]any{"envelope": envelope}
	switch id {
	case "launch.resolution":
		wrapper["argv_has_prompt"] = containsString(command.Arguments, "--prompt")
	case "bind.runtime_identity":
		bind, ok := bindFact(input.Binds, step.Case)
		if !ok {
			return Observation{}, fmt.Errorf("authority bind fact for %s is missing", step.Case)
		}
		wrapper["authority_instance_ids"] = stringsToAny(bind.AuthorityInstances)
		wrapper["active_correlation_runtime_ids"] = stringsToAny(bind.ActiveCorrelations)
	case "observe.restart_text":
		wrapper["baseline_record_id"] = input.Restart.BaselineRecordID
	case "send.idempotency_conflict":
		wrapper["original_command_id"] = input.Idempotency.OriginalCommandID
	case "command.delivery_integrity":
		fact, ok := integrityFact(input.CommandIntegrity, step.Case)
		if !ok {
			return Observation{}, fmt.Errorf("command integrity fact for %s is missing", step.Case)
		}
		wrapper["unchanged"] = fact.Unchanged
	}
	return Observation{Sequence: step.Sequence, AssertionID: id, Source: "duo_envelope", Evidence: wrapper}, nil
}

func verifiedControls(record RunRecord, caseName RunnerCase, actions ...ControlAction) bool {
	position := 0
	for _, control := range record.Controls {
		if control.Case != caseName {
			continue
		}
		if position >= len(actions) || control.Action != actions[position] || control.Error != nil || control.Requested != control.Result.Target || !control.Result.StoppedVerified {
			return false
		}
		position++
	}
	return position == len(actions)
}

func controllerObservation(step StepSpec, measurements, extra map[string]any) Observation {
	evidence := map[string]any{"kind": "controller.checkpoint", "monotonic_ms": float64(0), "measurements": measurements}
	for key, value := range extra {
		evidence[key] = value
	}
	return Observation{Sequence: step.Sequence, AssertionID: step.Assertions[1].ID, Source: "controller", Evidence: evidence}
}

func finalPrimaryCommand(action ActionSpec, commands []RecordedCommand) (RecordedCommand, error) {
	for i := len(commands) - 1; i >= 0; i-- {
		operation, _ := recordedCommandOperation(commands[i].Arguments)
		if operation == action.Operation {
			return commands[i], nil
		}
	}
	return RecordedCommand{}, fmt.Errorf("no %s command was selected for canonical evidence", action.Operation)
}

func decodeCommandEnvelope(command RecordedCommand, operation string) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(command.Stdout))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("%s stdout is not a JSON envelope: %w", operation, err)
	}
	if err := decoder.Decode(&struct{}{}); err == nil {
		return nil, fmt.Errorf("%s stdout contains multiple JSON values", operation)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s stdout has invalid trailing data: %w", operation, err)
	}
	canonical, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var envelope map[string]any
	if err := json.Unmarshal(canonical, &envelope); err != nil {
		return nil, err
	}
	if envelope["operation"] != operation {
		return nil, fmt.Errorf("command envelope operation %q does not match canonical %q", envelope["operation"], operation)
	}
	if err := ValidateExternalEnvelope(envelope); err != nil {
		return nil, fmt.Errorf("command envelope failed duo.external/v1 validation: %w", err)
	}
	return envelope, nil
}

func bindFact(facts []BindFact, caseName string) (BindFact, bool) {
	var out BindFact
	count := 0
	for _, fact := range facts {
		if fact.Case == caseName {
			out, count = fact, count+1
		}
	}
	return out, count == 1
}

func commandDelta(facts []CommandDeltaFact, caseName string) (CommandDeltaFact, bool) {
	var out CommandDeltaFact
	count := 0
	for _, fact := range facts {
		if fact.Case == caseName {
			out, count = fact, count+1
		}
	}
	return out, count == 1
}

func integrityFact(facts []CommandIntegrityFact, caseName string) (CommandIntegrityFact, bool) {
	var out CommandIntegrityFact
	count := 0
	for _, fact := range facts {
		if fact.Case == caseName {
			out, count = fact, count+1
		}
	}
	return out, count == 1
}

func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i := range values {
		out[i] = values[i]
	}
	return out
}

func collectorStageIndexForAssembly(caseName, stageName string) int {
	for i, step := range CanonicalScenario().Steps {
		if step.Case == caseName && step.Stage == stageName {
			return i
		}
	}
	return 0
}
