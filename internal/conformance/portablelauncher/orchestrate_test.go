//go:build linux

package portablelauncher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/manifest"
	"github.com/procrastivity/duo/internal/surface"
)

func TestPrepareFixtureMaterializesCanonicalScenarioAndDynamicDuoBuild(t *testing.T) {
	base, err := os.MkdirTemp(os.TempDir(), "duo-portable-setup-test-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(base) })
	artifacts := make(map[string]Artifact)
	writeArtifact := func(key, name, version string, data []byte) {
		path := filepath.Join(base, key)
		if err := os.WriteFile(path, data, 0o700); err != nil {
			t.Fatal(err)
		}
		artifacts[key] = Artifact{Name: name, Path: path, Version: version, Digest: Digest(data)}
	}
	writeArtifact("launcher", "amp", "fixture-launcher", []byte("launcher\n"))
	writeArtifact("duo", "duo", "fixture-duo", []byte("duo\n"))
	writeArtifact("host", "herdr", "0.8.2", []byte("herdr\n"))
	writeArtifact("runtime", "pi", "0.83.0", []byte("pi\n"))
	duoPin := DuoPin{Version: "fixture-duo", Commit: "0123456789abcdef0123456789abcdef01234567", BuildDate: "2026-09-18T00:00:00Z", ExecutableSHA256: artifacts["duo"].Digest}
	duoArtifact := artifacts["duo"]
	duoArtifact.Commit, duoArtifact.BuildDate = duoPin.Commit, duoPin.BuildDate
	artifacts["duo"] = duoArtifact

	root := &cobra.Command{Use: "duo"}
	verb := &cobra.Command{Use: "fixture", RunE: func(*cobra.Command, []string) error { return nil }}
	surface.Annotate(verb, surface.Plumbing)
	root.AddCommand(verb)
	productManifest, err := manifest.Build(root, buildinfo.Info{Version: duoPin.Version, Commit: duoPin.Commit, Date: duoPin.BuildDate})
	if err != nil {
		t.Fatal(err)
	}
	config := []byte("schema: duo.config/v3\n")
	setupInput := SetupInput{
		BaseDir: base, Artifacts: artifacts, HostProtocol: "herdr-socket-api/20", HostSchemaDigest: HostSchemaDigest,
		ProductManifest: productManifest, ConfigBytes: config, EffectiveConfigDigest: Digest(config),
		Credential: Credential{Present: true, SourceKind: "operator_copy", Bytes: []byte("fixture credential\n")}, Duo: duoPin,
	}
	mismatch := setupInput
	mismatch.ProductManifest.Tool.Commit = strings.Repeat("b", 40)
	if err := CheckSetupPrerequisites(mismatch); err == nil || !strings.Contains(err.Error(), "product manifest build identity") {
		t.Fatalf("product manifest with a different Duo pin was accepted: %v", err)
	}
	fixture, err := PrepareFixture(setupInput)
	if err != nil {
		t.Fatalf("PrepareFixture: %v", err)
	}
	if fixture.InstallationID == "" {
		t.Fatal("materialized fixture has no projected skill identity")
	}
	wantScenario, _ := ScenarioJSON(CanonicalScenario())
	gotScenario, err := os.ReadFile(filepath.Join(fixture.Workspace, ".duo-conformance", "scenario.json"))
	if err != nil || string(gotScenario) != string(wantScenario) {
		t.Fatalf("materialized scenario mismatch: err=%v", err)
	}
	pinBytes, err := os.ReadFile(filepath.Join(fixture.Root, filepath.FromSlash(launcherPinRelativePath)))
	if err != nil {
		t.Fatal(err)
	}
	var runPin LauncherPin
	if err := decodeStrict(pinBytes, &runPin); err != nil {
		t.Fatal(err)
	}
	wantPin := LauncherPin{Name: artifacts["launcher"].Name, Version: artifacts["launcher"].Version, ExecutableSHA256: artifacts["launcher"].Digest}
	if runPin != wantPin {
		t.Fatalf("run launcher pin = %#v, want %#v", runPin, wantPin)
	}
	if err := fixture.Cleanup(context.Background(), func(context.Context, *Fixture) error { return nil }, func(context.Context, *Fixture) error { return nil }); err != nil {
		t.Fatalf("cleanup materialized fixture: %v", err)
	}
}

func TestOrchestrateRunsOfflinePipelineAndPublishesStructuralBlockedBundle(t *testing.T) {
	input, seams, root, order := orchestrationTestFixture(t)
	result, err := orchestrate(context.Background(), input, seams)
	if err != nil {
		t.Fatalf("orchestrate: %v", err)
	}
	if result.Summary.Verdict != "fail" || result.Summary.FirstFailedCase == nil || *result.Summary.FirstFailedCase != "blocked" {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if _, err := os.Stat(filepath.Join(input.Destination, "result.json")); err != nil {
		t.Fatalf("bundle was not published: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture root remains after orchestration: %v", err)
	}
	want := []string{"run", "inspect", "load", "export", "assemble", "validate", "write"}
	if strings.Join(*order, ",") != strings.Join(want, ",") {
		t.Fatalf("pipeline order = %v, want %v", *order, want)
	}
	last := result.Stages[len(result.Stages)-1]
	if last.Stage != "cleanup" || last.Verdict != "pass" {
		t.Fatalf("independent final cleanup = %#v", last)
	}
}

func TestOrchestrateCleansAfterEveryMajorFailureBoundary(t *testing.T) {
	tests := []struct {
		name string
		edit func(*OrchestrationSeams)
	}{
		{name: "load", edit: func(s *OrchestrationSeams) {
			s.Load = func(string, int64, RecordedExecutable) ([]RecordedCommand, error) {
				return nil, errors.New("load failed")
			}
		}},
		{name: "assemble", edit: func(s *OrchestrationSeams) {
			base := s.Assemble
			s.Assemble = func(input AssemblyInput) ([]CommonStageCapture, error) {
				stages, _ := base(input)
				return stages, errors.New("assembly failed")
			}
		}},
		{name: "validate", edit: func(s *OrchestrationSeams) {
			s.Validate = func(Result, *Capture) error { return errors.New("validation failed") }
		}},
		{name: "write", edit: func(s *OrchestrationSeams) {
			s.Write = func(string, Result, *Capture) error { return errors.New("write failed") }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, seams, root, _ := orchestrationTestFixture(t)
			test.edit(&seams)
			if _, err := orchestrate(context.Background(), input, seams); err == nil {
				t.Fatal("injected pipeline failure was hidden")
			}
			if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("fixture root remains after %s failure: %v", test.name, err)
			}
		})
	}
}

func TestOrchestrateCleansAndMaterializesCommonRunFailure(t *testing.T) {
	input, seams, root, _ := orchestrationTestFixture(t)
	seams.Run = func(context.Context, RunnerInput) RunRecord {
		record := baseRunRecordForOrchestration(input.Facts)
		record.RunnerError = errors.New("injected common run failure")
		return record // orchestration must perform the missed cleanup itself.
	}
	result, err := orchestrate(context.Background(), input, seams)
	if err != nil {
		t.Fatalf("authoritative failed run was not materialized: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture root remains after common run failure: %v", err)
	}
	if result.Summary.FirstFailedCase == nil || *result.Summary.FirstFailedCase != "blocked" {
		t.Fatalf("structural blocker was not preserved as the first failure: %#v", result.Summary)
	}
	lastReached := result.Stages[collectorStageIndex("timeout", "command_inspection")]
	if lastReached.Error == nil || lastReached.Error.Code != "run.failed" {
		t.Fatalf("run failure attribution = %#v", lastReached)
	}
}

func TestOrchestrateBoundsCleanupAfterCallerCancellation(t *testing.T) {
	input, seams, _, _ := orchestrationTestFixture(t)
	input.Pins.Skill.InstallationID = "different-installation"
	deadlineSeen := false
	input.Runner.InspectResources = func(ctx context.Context, _ *Fixture) error {
		deadline, ok := ctx.Deadline()
		deadlineSeen = ok && ctx.Err() == nil && time.Until(deadline) > 0 && time.Until(deadline) <= cleanupTimeout
		return nil
	}
	input.Runner.ExportEvidence = func(context.Context, *Fixture) error { return nil }
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := orchestrate(ctx, input, seams); err == nil || !strings.Contains(err.Error(), "installed skill identity") {
		t.Fatalf("pin mismatch = %v", err)
	}
	if !deadlineSeen {
		t.Fatal("cleanup did not receive a fresh bounded context after caller cancellation")
	}
}

func TestOrchestrateRejectsSetupResultLauncherDisagreement(t *testing.T) {
	input, seams, _, _ := orchestrationTestFixture(t)
	input.Pins.Launcher.Version = "different-fresh-version"
	if _, err := orchestrate(context.Background(), input, seams); err == nil || !strings.Contains(err.Error(), "setup launcher identity") {
		t.Fatalf("launcher disagreement error = %v", err)
	}
}

func TestOrchestrateRejectsDriverResultLauncherDisagreement(t *testing.T) {
	input, seams, root, _ := orchestrationTestFixture(t)
	writeDriverLauncherPin(t, root, LauncherPin{Name: "amp", Version: "different-driver-version", ExecutableSHA256: input.Pins.Launcher.ExecutableSHA256})
	if _, err := orchestrate(context.Background(), input, seams); err == nil || !strings.Contains(err.Error(), "driver launcher identity") {
		t.Fatalf("driver/result disagreement error = %v", err)
	}
}

func baseRunRecordForOrchestration(facts AssemblyInput) RunRecord {
	return RunRecord{Controls: append([]RecordedControl(nil), facts.Record.Controls...)}
}

func orchestrationTestFixture(t *testing.T) (OrchestrationInput, OrchestrationSeams, string, *[]string) {
	t.Helper()
	root, err := os.MkdirTemp(t.TempDir(), "duo-portable-launcher-")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bin", "capture", "workspace", "workspace/.duo-conformance"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	duoBytes := []byte("fixture duo executable\n")
	if err := os.WriteFile(filepath.Join(root, "bin", "duo"), duoBytes, 0o700); err != nil {
		t.Fatal(err)
	}
	pins := fixturePins()
	writeDriverLauncherPin(t, root, pins.Launcher)
	facts := assemblyTestInput(t)
	facts.Pins = pins
	setup := SetupInput{
		Artifacts: map[string]Artifact{
			"launcher": {Name: pins.Launcher.Name, Version: pins.Launcher.Version, Digest: pins.Launcher.ExecutableSHA256},
			"duo":      {Name: "duo", Version: pins.Duo.Version, Commit: pins.Duo.Commit, BuildDate: pins.Duo.BuildDate, Digest: pins.Duo.ExecutableSHA256},
			"host":     {Name: "herdr", Version: pins.Host.Version, Digest: pins.Host.ExecutableSHA256},
			"runtime":  {Name: "pi", Version: pins.Runtime.Version, Digest: pins.Runtime.ExecutableSHA256},
		},
		HostProtocol: pins.Host.Protocol, HostSchemaDigest: pins.Host.SchemaDigest,
		EffectiveConfigDigest: pins.Config.EffectiveDigest,
		Credential:            Credential{Present: true, SourceKind: "operator_copy", Bytes: []byte("fixture credential")},
		Duo:                   pins.Duo,
	}
	fixture := &Fixture{
		Root: root, Workspace: filepath.Join(root, "workspace"), Capture: filepath.Join(root, "capture"),
		Result: filepath.Join(root, "result"), InstallationID: pins.Skill.InstallationID,
	}
	order := &[]string{}
	runner := RunnerInput{
		InspectResources: func(context.Context, *Fixture) error { *order = append(*order, "inspect"); return nil },
		ExportEvidence:   func(context.Context, *Fixture) error { *order = append(*order, "export"); return nil },
	}
	input := OrchestrationInput{
		Setup: setup, Runner: runner, Run: facts.Run, Pins: pins, Facts: facts,
		RunOriginBootTimeNS: 1, Destination: filepath.Join(t.TempDir(), "bundle"),
	}
	seams := OrchestrationSeams{
		Prepare: func(SetupInput) (*Fixture, error) { return fixture, nil },
		Run: func(ctx context.Context, in RunnerInput) RunRecord {
			*order = append(*order, "run")
			record := facts.Record
			record.CleanupError = in.Fixture.Cleanup(ctx, in.InspectResources, in.ExportEvidence)
			return record
		},
		Load: func(string, int64, RecordedExecutable) ([]RecordedCommand, error) {
			*order = append(*order, "load")
			return facts.Commands, nil
		},
		Assemble: func(input AssemblyInput) ([]CommonStageCapture, error) {
			*order = append(*order, "assemble")
			return AssembleCommonStages(input)
		},
		Collect: CollectResult,
		Validate: func(result Result, capture *Capture) error {
			*order = append(*order, "validate")
			return validateCollectedCapture(result, capture)
		},
		Write: func(destination string, result Result, capture *Capture) error {
			*order = append(*order, "write")
			return WriteBundle(destination, result, capture)
		},
	}
	return input, seams, root, order
}
