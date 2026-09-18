//go:build linux

package portablelauncher

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// OrchestrationInput is the reusable launcher-neutral live-suite entry point.
// Facts contains normalized raw facts and timings only; its Run, Pins, Record,
// Commands, and Cleanup.RootRemoved fields are overwritten by orchestration.
type OrchestrationInput struct {
	Setup               SetupInput
	Runner              RunnerInput
	Run                 RunIdentity
	Pins                Pins
	Facts               AssemblyInput
	RunOriginBootTimeNS int64
	Destination         string
}

// OrchestrationSeams make the complete pipeline deterministic in offline
// tests. Zero-valued fields use the production implementations.
type OrchestrationSeams struct {
	Prepare  func(SetupInput) (*Fixture, error)
	Run      func(context.Context, RunnerInput) RunRecord
	Load     func(string, int64, RecordedExecutable) ([]RecordedCommand, error)
	Assemble func(AssemblyInput) ([]CommonStageCapture, error)
	Collect  func(CollectorInput) (Result, *Capture)
	Validate func(Result, *Capture) error
	Write    func(string, Result, *Capture) error
}

// Orchestrate prepares one fixture, runs the common controller, loads the
// command journals before fixture removal, assembles trusted stages, collects
// and offline-validates the result, then atomically writes the bundle. Fixture
// cleanup is always completed before assembly and remains an independent final
// canonical stage.
func Orchestrate(ctx context.Context, input OrchestrationInput) (Result, error) {
	return orchestrate(ctx, input, OrchestrationSeams{})
}

func orchestrate(ctx context.Context, input OrchestrationInput, seams OrchestrationSeams) (Result, error) {
	seams = defaultOrchestrationSeams(seams)
	if input.Destination == "" {
		return Result{}, fmt.Errorf("portable launcher orchestration requires a bundle destination")
	}
	if input.RunOriginBootTimeNS <= 0 {
		return Result{}, fmt.Errorf("portable launcher orchestration requires a positive command run origin")
	}
	if input.Setup.Duo != input.Pins.Duo {
		return Result{}, fmt.Errorf("portable launcher orchestration: setup Duo identity does not match result pins")
	}

	fixture, err := seams.Prepare(input.Setup)
	if err != nil {
		if fixture != nil {
			_ = cleanupFixture(ctx, fixture, input.Runner.InspectResources, input.Runner.ExportEvidence)
		}
		return Result{}, fmt.Errorf("portable launcher orchestration: prepare fixture: %w", err)
	}
	if fixture == nil {
		return Result{}, fmt.Errorf("portable launcher orchestration: prepare fixture returned nil")
	}

	if input.Pins.Skill.InstallationID == "" {
		input.Pins.Skill.InstallationID = fixture.InstallationID
	} else if input.Pins.Skill.InstallationID != fixture.InstallationID {
		cleanupErr := cleanupFixture(ctx, fixture, input.Runner.InspectResources, input.Runner.ExportEvidence)
		return Result{}, errors.Join(fmt.Errorf("portable launcher orchestration: installed skill identity does not match result pins"), cleanupErr)
	}
	if err := setupMatchesResultPins(input.Setup, input.Pins); err != nil {
		cleanupErr := cleanupFixture(ctx, fixture, input.Runner.InspectResources, input.Runner.ExportEvidence)
		return Result{}, errors.Join(err, cleanupErr)
	}
	driverPin, err := loadRunLauncherPin(fixture.Root)
	if err != nil || driverPin != input.Pins.Launcher {
		cleanupErr := cleanupFixture(ctx, fixture, input.Runner.InspectResources, input.Runner.ExportEvidence)
		return Result{}, errors.Join(fmt.Errorf("portable launcher orchestration: driver launcher identity does not match result pins"), err, cleanupErr)
	}

	identity, err := recordedExecutableForPath(filepath.Join(fixture.Root, "bin", "duo"))
	if err != nil {
		cleanupErr := cleanupFixture(ctx, fixture, input.Runner.InspectResources, input.Runner.ExportEvidence)
		return Result{}, errors.Join(fmt.Errorf("portable launcher orchestration: identify copied Duo executable: %w", err), cleanupErr)
	}

	var commands []RecordedCommand
	var loadErr error
	loaded := false
	callerExport := input.Runner.ExportEvidence
	input.Runner.ExportEvidence = func(exportCtx context.Context, f *Fixture) error {
		if !loaded {
			commands, loadErr = seams.Load(f.Capture, input.RunOriginBootTimeNS, identity)
			loaded = true
		}
		var exportErr error
		if callerExport == nil {
			exportErr = fmt.Errorf("cleanup evidence export omitted")
		} else {
			exportErr = callerExport(exportCtx, f)
		}
		return errors.Join(loadErr, exportErr)
	}
	input.Runner.Fixture = fixture
	record := seams.Run(ctx, input.Runner)
	if !fixture.cleaned {
		cleanupErr := cleanupFixture(ctx, fixture, input.Runner.InspectResources, input.Runner.ExportEvidence)
		record.CleanupError = errors.Join(record.CleanupError, cleanupErr)
	}

	facts := input.Facts
	facts.Run, facts.Pins, facts.Record, facts.Commands = input.Run, input.Pins, record, commands
	facts.Setup.Isolated = true
	facts.Setup.CredentialPresent = input.Setup.Credential.Present
	facts.Setup.CredentialProvider = CanonicalScenario().Fixture.Provider
	facts.Setup.CredentialSourceKind = input.Setup.Credential.SourceKind
	facts.Cleanup.RootRemoved = fixture.cleaned
	stages, assemblyErr := seams.Assemble(facts)
	result, capture := seams.Collect(CollectorInput{Run: input.Run, Pins: input.Pins, Record: record, Stages: stages})
	if loadErr != nil || assemblyErr != nil {
		return result, errors.Join(fmt.Errorf("portable launcher orchestration: trusted assembly rejected raw facts"), loadErr, assemblyErr)
	}
	if err := seams.Validate(result, capture); err != nil {
		return result, fmt.Errorf("portable launcher orchestration: offline validation: %w", err)
	}
	if err := seams.Write(input.Destination, result, capture); err != nil {
		return result, fmt.Errorf("portable launcher orchestration: write bundle: %w", err)
	}
	return result, nil
}

func cleanupFixture(ctx context.Context, fixture *Fixture, inspect ResourceInspector, export EvidenceExporter) error {
	cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
	defer cancel()
	return fixture.Cleanup(cleanupContext, inspect, export)
}

func defaultOrchestrationSeams(seams OrchestrationSeams) OrchestrationSeams {
	if seams.Prepare == nil {
		seams.Prepare = PrepareFixture
	}
	if seams.Run == nil {
		seams.Run = RunCommon
	}
	if seams.Load == nil {
		seams.Load = LoadRecordedCommands
	}
	if seams.Assemble == nil {
		seams.Assemble = AssembleCommonStages
	}
	if seams.Collect == nil {
		seams.Collect = CollectResult
	}
	if seams.Validate == nil {
		seams.Validate = validateCollectedCapture
	}
	if seams.Write == nil {
		seams.Write = WriteBundle
	}
	return seams
}

func validateCollectedCapture(result Result, capture *Capture) error {
	if capture == nil {
		return fmt.Errorf("capture is nil")
	}
	resultJSON, err := marshalBundleJSON(result)
	if err != nil {
		return err
	}
	indexJSON, err := marshalBundleJSON(capture.Index())
	if err != nil {
		return err
	}
	_, err = Validate(ValidationInput{ResultJSON: resultJSON, IndexJSON: indexJSON, ReadBlob: func(reference string) ([]byte, error) {
		blob, ok := capture.Blob(reference)
		if !ok {
			return nil, fmt.Errorf("blob %s not found", reference)
		}
		return blob, nil
	}})
	return err
}

func setupMatchesResultPins(setup SetupInput, pins Pins) error {
	launcher := setup.Artifacts["launcher"]
	duo := setup.Artifacts["duo"]
	host := setup.Artifacts["host"]
	runtime := setup.Artifacts["runtime"]
	if launcher.Name != pins.Launcher.Name || launcher.Version != pins.Launcher.Version || launcher.Digest != pins.Launcher.ExecutableSHA256 {
		return fmt.Errorf("portable launcher orchestration: setup launcher identity does not match result pins")
	}
	if duo.Version != pins.Duo.Version || duo.Commit != pins.Duo.Commit || duo.BuildDate != pins.Duo.BuildDate || duo.Digest != pins.Duo.ExecutableSHA256 {
		return fmt.Errorf("portable launcher orchestration: setup Duo identity does not match result pins")
	}
	if host.Version != pins.Host.Version || host.Digest != pins.Host.ExecutableSHA256 || setup.HostProtocol != pins.Host.Protocol || setup.HostSchemaDigest != pins.Host.SchemaDigest {
		return fmt.Errorf("portable launcher orchestration: setup host identity does not match result pins")
	}
	if runtime.Version != pins.Runtime.Version || runtime.Digest != pins.Runtime.ExecutableSHA256 {
		return fmt.Errorf("portable launcher orchestration: setup runtime identity does not match result pins")
	}
	if setup.EffectiveConfigDigest != pins.Config.EffectiveDigest {
		return fmt.Errorf("portable launcher orchestration: setup config identity does not match result pins")
	}
	return nil
}
