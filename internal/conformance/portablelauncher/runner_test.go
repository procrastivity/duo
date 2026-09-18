package portablelauncher

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	exitedIdentity  = ProcessIdentity{PID: 101, StartTime: "birth-exited", TerminalID: "terminal-exited", AttachmentID: "attachment-exited"}
	timeoutIdentity = ProcessIdentity{PID: 202, StartTime: "birth-timeout", TerminalID: "terminal-timeout", AttachmentID: "attachment-timeout"}
)

type fakeFaultController struct {
	mu        sync.Mutex
	actions   []ControlAction
	responses []ControlCheckpoint
	errors    []error
}

func (f *fakeFaultController) SuspendExactProcess(_ context.Context, target ProcessIdentity) (ControlCheckpoint, error) {
	return f.control(ControlSuspendExactProcess, target)
}

func (f *fakeFaultController) CloseExactPane(_ context.Context, target ProcessIdentity) (ControlCheckpoint, error) {
	return f.control(ControlCloseExactPane, target)
}

func (f *fakeFaultController) control(action ControlAction, target ProcessIdentity) (ControlCheckpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	index := len(f.actions)
	f.actions = append(f.actions, action)
	if index < len(f.responses) {
		var err error
		if index < len(f.errors) {
			err = f.errors[index]
		}
		return f.responses[index], err
	}
	monotonic := []int64{15, 25, 35}
	return ControlCheckpoint{Target: target, StoppedVerified: true, MonotonicMS: monotonic[index]}, nil
}

func (f *fakeFaultController) recordedActions() []ControlAction {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ControlAction(nil), f.actions...)
}

func TestRunCommonOrdersControlsAndRunsLauncherAsynchronously(t *testing.T) {
	controller := &fakeFaultController{}
	checkpoints := happyRunnerCheckpoints()
	input, cleanup := runnerTestInput(t, controller, checkpoints)
	record := runCommon(context.Background(), input, runnerConfig{completeTimeout: time.Second})
	cleanup.assertOnce(t)

	if record.RunnerError != nil || record.LauncherError != nil || record.CheckpointSourceError != nil || record.CleanupError != nil {
		t.Fatalf("unexpected errors: runner=%v launcher=%v source=%v cleanup=%v", record.RunnerError, record.LauncherError, record.CheckpointSourceError, record.CleanupError)
	}
	wantActions := []ControlAction{ControlSuspendExactProcess, ControlCloseExactPane, ControlSuspendExactProcess}
	if got := controller.recordedActions(); !reflect.DeepEqual(got, wantActions) {
		t.Fatalf("controller actions = %v, want %v", got, wantActions)
	}
	if len(record.Checkpoints) != 4 || len(record.Controls) != 3 {
		t.Fatalf("recorded checkpoints/controls = %d/%d, want 4/3", len(record.Checkpoints), len(record.Controls))
	}
	if string(record.Capture.Events) != "raw event\n" {
		t.Fatalf("launcher capture was changed: %+v", record.Capture)
	}
	for i, control := range record.Controls {
		if control.Error != nil || control.Requested != control.Result.Target {
			t.Fatalf("control %d did not retain an exact successful result: %+v", i, control)
		}
	}
}

func TestRunCommonFailsClosedOnCheckpointAndControllerDrift(t *testing.T) {
	changedIdentity := exitedIdentity
	changedIdentity.StartTime = "different-birth"
	tests := []struct {
		name        string
		checkpoints []RunnerCheckpoint
		controller  *fakeFaultController
		wantError   string
		wantActions []ControlAction
	}{
		{
			name: "delivery before suspension",
			checkpoints: []RunnerCheckpoint{
				{Kind: CheckpointCommandDelivered, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 10},
			},
			controller: &fakeFaultController{}, wantError: "wrong checkpoint case/order",
		},
		{
			name: "delivery monotonic time before suspension",
			checkpoints: []RunnerCheckpoint{
				{Kind: CheckpointAttachmentBound, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 10},
				{Kind: CheckpointCommandDelivered, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 11},
			},
			controller: &fakeFaultController{}, wantError: "delivery preceded verified suspension",
			wantActions: []ControlAction{ControlSuspendExactProcess},
		},
		{
			name: "identity changes",
			checkpoints: []RunnerCheckpoint{
				{Kind: CheckpointAttachmentBound, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 10},
				{Kind: CheckpointCommandDelivered, Case: RunnerCaseExited, Target: changedIdentity, MonotonicMS: 20},
			},
			controller: &fakeFaultController{}, wantError: "process identity changed",
			wantActions: []ControlAction{ControlSuspendExactProcess},
		},
		{
			name: "duplicate checkpoint and control",
			checkpoints: []RunnerCheckpoint{
				{Kind: CheckpointAttachmentBound, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 10},
				{Kind: CheckpointAttachmentBound, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 20},
			},
			controller: &fakeFaultController{}, wantError: "duplicate checkpoint/control",
			wantActions: []ControlAction{ControlSuspendExactProcess},
		},
		{
			name: "wrong case order",
			checkpoints: []RunnerCheckpoint{
				{Kind: CheckpointAttachmentBound, Case: RunnerCaseTimeout, Target: timeoutIdentity, MonotonicMS: 10},
			},
			controller: &fakeFaultController{}, wantError: "wrong checkpoint case/order",
		},
		{
			name: "blocked remains unsupported",
			checkpoints: []RunnerCheckpoint{
				{Kind: CheckpointAttachmentBound, Case: RunnerCaseBlocked, Target: exitedIdentity, MonotonicMS: 10},
			},
			controller: &fakeFaultController{}, wantError: "prerequisite.blocked_induction_unavailable",
		},
		{
			name: "controller does not verify suspension",
			checkpoints: []RunnerCheckpoint{
				{Kind: CheckpointAttachmentBound, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 10},
			},
			controller: &fakeFaultController{responses: []ControlCheckpoint{{Target: exitedIdentity, MonotonicMS: 15}}},
			wantError:  "suspension was not verified", wantActions: []ControlAction{ControlSuspendExactProcess},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input, cleanup := runnerTestInput(t, tc.controller, tc.checkpoints)
			record := runCommon(context.Background(), input, runnerConfig{completeTimeout: time.Second})
			cleanup.assertOnce(t)
			if record.RunnerError == nil || !strings.Contains(record.RunnerError.Error(), tc.wantError) {
				t.Fatalf("runner error = %v, want %q", record.RunnerError, tc.wantError)
			}
			if got := tc.controller.recordedActions(); !reflect.DeepEqual(got, tc.wantActions) {
				t.Fatalf("controller actions = %v, want %v", got, tc.wantActions)
			}
			if len(record.Checkpoints) == 0 || record.Checkpoints[len(record.Checkpoints)-1].Error == nil {
				t.Fatal("rejected raw checkpoint and error were not retained")
			}
		})
	}
}

func TestRunCommonPreservesFailedLauncherCapture(t *testing.T) {
	controller := &fakeFaultController{}
	input, cleanup := runnerTestInput(t, controller, nil)
	wantError := errors.New("launcher exited 7")
	input.Launch = func(context.Context) (DriverCapture, error) {
		return DriverCapture{
			Events: []byte("partial event\n"), Stderr: []byte("raw stderr\n"),
		}, wantError
	}
	record := runCommon(context.Background(), input, runnerConfig{completeTimeout: time.Second})
	cleanup.assertOnce(t)

	if !errors.Is(record.LauncherError, wantError) || record.RunnerError == nil || !strings.Contains(record.RunnerError.Error(), "launcher failed") {
		t.Fatalf("launcher failure was not retained: launcher=%v runner=%v", record.LauncherError, record.RunnerError)
	}
	if string(record.Capture.Events) != "partial event\n" || string(record.Capture.Stderr) != "raw stderr\n" {
		t.Fatalf("failed launcher capture was repaired or lost: %+v", record.Capture)
	}
}

func TestRunCommonCompleteTimeoutStillCleansExactlyOnce(t *testing.T) {
	controller := &fakeFaultController{}
	input, cleanup := runnerTestInput(t, controller, nil)
	input.Launch = func(ctx context.Context) (DriverCapture, error) {
		<-ctx.Done()
		return DriverCapture{Events: []byte("before timeout\n")}, ctx.Err()
	}
	input.Checkpoints = func(ctx context.Context, _ chan<- RunnerCheckpoint) error {
		<-ctx.Done()
		return ctx.Err()
	}
	record := runCommon(context.Background(), input, runnerConfig{completeTimeout: 10 * time.Millisecond})
	cleanup.assertOnce(t)

	if !record.CompleteRunTimedOut || record.RunnerError == nil || !strings.Contains(record.RunnerError.Error(), "complete-run deadline exceeded") {
		t.Fatalf("timeout record = timedOut:%v runner:%v", record.CompleteRunTimedOut, record.RunnerError)
	}
	if string(record.Capture.Events) != "before timeout\n" || !errors.Is(record.LauncherError, context.DeadlineExceeded) {
		t.Fatalf("timeout capture/error not preserved: capture=%+v error=%v", record.Capture, record.LauncherError)
	}
}

func TestRunCommonRecordsControllerFailureAndCleanupFailure(t *testing.T) {
	controllerError := errors.New("signal refused")
	controller := &fakeFaultController{
		responses: []ControlCheckpoint{{Target: exitedIdentity, MonotonicMS: 15}},
		errors:    []error{controllerError},
	}
	input, cleanup := runnerTestInput(t, controller, []RunnerCheckpoint{
		{Kind: CheckpointAttachmentBound, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 10},
	})
	cleanup.inspectError = errors.New("pane remains")
	record := runCommon(context.Background(), input, runnerConfig{completeTimeout: time.Second})
	cleanup.assertOnce(t)

	if record.RunnerError == nil || !errors.Is(record.Controls[0].Error, controllerError) {
		t.Fatalf("controller failure not retained: runner=%v controls=%+v", record.RunnerError, record.Controls)
	}
	if record.CleanupError == nil || !strings.Contains(record.CleanupError.Error(), "pane remains") {
		t.Fatalf("cleanup failure was hidden: %v", record.CleanupError)
	}
}

func happyRunnerCheckpoints() []RunnerCheckpoint {
	return []RunnerCheckpoint{
		{Kind: CheckpointAttachmentBound, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 10},
		{Kind: CheckpointCommandDelivered, Case: RunnerCaseExited, Target: exitedIdentity, MonotonicMS: 20},
		{Kind: CheckpointAttachmentBound, Case: RunnerCaseTimeout, Target: timeoutIdentity, MonotonicMS: 30},
		{Kind: CheckpointCommandDelivered, Case: RunnerCaseTimeout, Target: timeoutIdentity, MonotonicMS: 40},
	}
}

type cleanupSpy struct {
	mu           sync.Mutex
	inspectCalls int
	exportCalls  int
	deadlineSeen bool
	inspectError error
}

func (s *cleanupSpy) inspect(ctx context.Context, _ *Fixture) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inspectCalls++
	deadline, ok := ctx.Deadline()
	s.deadlineSeen = ok && time.Until(deadline) > 29*time.Second && time.Until(deadline) <= cleanupTimeout
	return s.inspectError
}

func (s *cleanupSpy) export(context.Context, *Fixture) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.exportCalls++
	return nil
}

func (s *cleanupSpy) assertOnce(t *testing.T) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inspectCalls != 1 || s.exportCalls != 1 || !s.deadlineSeen {
		t.Fatalf("cleanup calls inspect/export/deadline = %d/%d/%v, want 1/1/true", s.inspectCalls, s.exportCalls, s.deadlineSeen)
	}
}

func runnerTestInput(t *testing.T, controller FaultController, checkpoints []RunnerCheckpoint) (RunnerInput, *cleanupSpy) {
	t.Helper()
	root, err := os.MkdirTemp(t.TempDir(), "duo-portable-launcher-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "capture"), 0o700); err != nil {
		t.Fatal(err)
	}

	started := make(chan struct{})
	sourceFinished := make(chan struct{})
	cleanup := &cleanupSpy{}
	input := RunnerInput{
		Fixture: &Fixture{Root: root, Capture: filepath.Join(root, "capture")},
		Launch: func(ctx context.Context) (DriverCapture, error) {
			close(started)
			select {
			case <-sourceFinished:
				return DriverCapture{Events: []byte("raw event\n")}, nil
			case <-ctx.Done():
				return DriverCapture{}, ctx.Err()
			}
		},
		Checkpoints: func(ctx context.Context, output chan<- RunnerCheckpoint) error {
			defer close(sourceFinished)
			select {
			case <-started:
			case <-ctx.Done():
				return ctx.Err()
			}
			for _, checkpoint := range checkpoints {
				select {
				case output <- checkpoint:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		},
		Controller: controller, InspectResources: cleanup.inspect, ExportEvidence: cleanup.export,
	}
	return input, cleanup
}
