package portablelauncher

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

const cleanupTimeout = 30 * time.Second

// RunnerCheckpointKind identifies a raw fact observed by the checkpoint
// source. Checkpoints intentionally cannot carry a verdict, assertion, or
// expected value.
type RunnerCheckpointKind string

const (
	// CheckpointAttachmentBound reports an exact, durable attachment identity.
	CheckpointAttachmentBound RunnerCheckpointKind = "attachment_bound"
	// CheckpointCommandDelivered reports durable delivery for that identity.
	CheckpointCommandDelivered RunnerCheckpointKind = "command_delivered"
)

// RunnerCase identifies the state-induction case to which a raw checkpoint
// belongs.
type RunnerCase string

const (
	// RunnerCaseBlocked names the unsupported admitted-then-blocked case.
	RunnerCaseBlocked RunnerCase = "blocked"
	// RunnerCaseExited names the exact-pane lifecycle case.
	RunnerCaseExited RunnerCase = "exited"
	// RunnerCaseTimeout names the suspended-process observation case.
	RunnerCaseTimeout RunnerCase = "timeout"
)

// RunnerCheckpoint is launcher-neutral evidence emitted by an authority or
// host observer. The source reports facts only; RunCommon owns their meaning
// and every resulting controller action.
type RunnerCheckpoint struct {
	Kind        RunnerCheckpointKind
	Case        RunnerCase
	Target      ProcessIdentity
	MonotonicMS int64
}

// LauncherFunc runs one thin launcher and returns its uninterpreted capture.
// It must honor context cancellation.
type LauncherFunc func(context.Context) (DriverCapture, error)

// CheckpointSource emits raw checkpoints until the semantic run is complete.
// It must honor context cancellation and must not close the supplied channel.
type CheckpointSource func(context.Context, chan<- RunnerCheckpoint) error

// ControlAction identifies an action selected by common runner policy.
type ControlAction string

const (
	// ControlSuspendExactProcess is verified exact-process suspension.
	ControlSuspendExactProcess ControlAction = "suspend_exact_process"
	// ControlCloseExactPane is exact-pane closure after durable delivery.
	ControlCloseExactPane ControlAction = "close_exact_pane"
)

// RecordedCheckpoint retains every raw checkpoint and any reason it was
// rejected. It is input for a later collector, not a suite verdict.
type RecordedCheckpoint struct {
	Checkpoint RunnerCheckpoint
	Error      error
}

// RecordedControl retains the exact request, result, and error for one common
// runner controller action.
type RecordedControl struct {
	Action    ControlAction
	Case      RunnerCase
	Requested ProcessIdentity
	Result    ControlCheckpoint
	Error     error
}

// RunRecord is the raw outcome of launcher orchestration. It deliberately
// does not contain or imply a conformance verdict.
type RunRecord struct {
	Capture               DriverCapture
	LauncherError         error
	CheckpointSourceError error
	RunnerError           error
	Checkpoints           []RecordedCheckpoint
	Controls              []RecordedControl
	CompleteRunTimedOut   bool
	CleanupError          error
}

// RunnerInput supplies only the launcher-neutral seams needed by the common
// runner. Fixture is already prepared, so cleanup is mandatory on every
// return from RunCommon.
type RunnerInput struct {
	Fixture          *Fixture
	Launch           LauncherFunc
	Checkpoints      CheckpointSource
	Controller       FaultController
	InspectResources ResourceInspector
	ExportEvidence   EvidenceExporter
}

type controllerCleaner interface {
	Cleanup(context.Context) error
}

type runnerConfig struct {
	completeTimeout time.Duration
}

// RunCommon runs the launcher and checkpoint observer concurrently under the
// canonical complete-run deadline, applies the common fault-induction order,
// and cleans the prepared fixture exactly once. Concrete authority polling,
// host inspection, and final result assembly are intentionally outside this
// boundary.
func RunCommon(ctx context.Context, input RunnerInput) (record RunRecord) {
	return runCommon(ctx, input, runnerConfig{completeTimeout: completeRunTimeout})
}

func runCommon(ctx context.Context, input RunnerInput, config runnerConfig) (record RunRecord) {
	if input.Fixture == nil {
		record.RunnerError = fmt.Errorf("common runner requires a prepared fixture")
		return record
	}
	defer func() {
		cleanupContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), cleanupTimeout)
		defer cancel()

		var cleanupErrors []error
		if cleaner, ok := input.Controller.(controllerCleaner); ok {
			if err := cleaner.Cleanup(cleanupContext); err != nil {
				cleanupErrors = append(cleanupErrors, fmt.Errorf("fault-controller cleanup failed: %w", err))
			}
		}
		if err := input.Fixture.Cleanup(cleanupContext, input.InspectResources, input.ExportEvidence); err != nil {
			cleanupErrors = append(cleanupErrors, fmt.Errorf("fixture cleanup failed: %w", err))
		}
		record.CleanupError = errors.Join(record.CleanupError, errors.Join(cleanupErrors...))
	}()

	if input.Launch == nil {
		record.RunnerError = fmt.Errorf("common runner requires a launcher function")
		return record
	}
	if input.Checkpoints == nil {
		record.RunnerError = fmt.Errorf("common runner requires a checkpoint source")
		return record
	}
	if input.Controller == nil {
		record.RunnerError = fmt.Errorf("common runner requires a fault controller")
		return record
	}
	if config.completeTimeout <= 0 {
		record.RunnerError = fmt.Errorf("common runner requires a positive complete-run deadline")
		return record
	}

	completeContext, completeCancel := context.WithTimeout(ctx, config.completeTimeout)
	defer completeCancel()
	workContext, workCancel := context.WithCancel(completeContext)
	defer workCancel()

	type launcherResult struct {
		capture DriverCapture
		err     error
	}
	launcherDone := make(chan launcherResult, 1)
	go func() {
		capture, err := input.Launch(workContext)
		launcherDone <- launcherResult{capture: capture, err: err}
	}()

	checkpointEvents := make(chan RunnerCheckpoint)
	checkpointSourceDone := make(chan error, 1)
	go func() {
		checkpointSourceDone <- input.Checkpoints(workContext, checkpointEvents)
	}()

	state := runnerControlState{controller: input.Controller}
	launcherPending, sourcePending := true, true
	deadline := completeContext.Done()
	for launcherPending || sourcePending {
		select {
		case checkpoint, open := <-checkpointEvents:
			if !open {
				checkpointEvents = nil
				if record.RunnerError == nil {
					record.RunnerError = fmt.Errorf("checkpoint source closed the runner-owned event channel")
					workCancel()
				}
				continue
			}
			entry := RecordedCheckpoint{Checkpoint: checkpoint}
			if record.RunnerError != nil {
				entry.Error = fmt.Errorf("checkpoint received after runner failure")
			} else if !launcherPending {
				entry.Error = fmt.Errorf("checkpoint received after launcher completion")
			} else {
				entry.Error = state.accept(workContext, checkpoint, &record.Controls)
			}
			record.Checkpoints = append(record.Checkpoints, entry)
			if entry.Error != nil && record.RunnerError == nil {
				record.RunnerError = entry.Error
				workCancel()
			}
		case result := <-launcherDone:
			launcherPending = false
			record.Capture = result.capture
			record.LauncherError = result.err
			if result.err != nil && record.RunnerError == nil {
				record.RunnerError = fmt.Errorf("launcher failed: %w", result.err)
				workCancel()
			}
		case err := <-checkpointSourceDone:
			sourcePending = false
			record.CheckpointSourceError = err
			if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, io.EOF) && record.RunnerError == nil {
				record.RunnerError = fmt.Errorf("checkpoint source failed: %w", err)
				workCancel()
			}
		case <-deadline:
			deadline = nil
			record.CompleteRunTimedOut = errors.Is(completeContext.Err(), context.DeadlineExceeded)
			if record.RunnerError == nil {
				record.RunnerError = fmt.Errorf("complete-run deadline exceeded: %w", completeContext.Err())
			}
			workCancel()
		}
	}
	if errors.Is(completeContext.Err(), context.DeadlineExceeded) {
		record.CompleteRunTimedOut = true
		if record.RunnerError == nil || errors.Is(record.LauncherError, context.DeadlineExceeded) {
			record.RunnerError = fmt.Errorf("complete-run deadline exceeded: %w", completeContext.Err())
		}
	}

	if record.RunnerError == nil {
		if err := state.complete(); err != nil {
			record.RunnerError = err
		}
	}
	return record
}

type runnerControlState struct {
	controller     FaultController
	phase          int
	lastCheckpoint int64
	lastRaw        RunnerCheckpoint
	exitedTarget   ProcessIdentity
	timeoutTarget  ProcessIdentity
	lastControl    ControlCheckpoint
}

var orderedControlCheckpoints = [...]struct {
	kind     RunnerCheckpointKind
	caseName RunnerCase
}{
	{kind: CheckpointAttachmentBound, caseName: RunnerCaseExited},
	{kind: CheckpointCommandDelivered, caseName: RunnerCaseExited},
	{kind: CheckpointAttachmentBound, caseName: RunnerCaseTimeout},
	{kind: CheckpointCommandDelivered, caseName: RunnerCaseTimeout},
}

func (s *runnerControlState) accept(ctx context.Context, checkpoint RunnerCheckpoint, controls *[]RecordedControl) error {
	if checkpoint.Case == RunnerCaseBlocked {
		return fmt.Errorf("prerequisite.blocked_induction_unavailable: common runner has no supported blocked control")
	}
	if !validProcessIdentity(checkpoint.Target) {
		return fmt.Errorf("checkpoint has incomplete process identity")
	}
	if checkpoint.MonotonicMS < 0 || (s.phase > 0 && checkpoint.MonotonicMS < s.lastCheckpoint) {
		return fmt.Errorf("checkpoint monotonic order regressed")
	}
	if s.phase > 0 && checkpoint.Kind == s.lastRaw.Kind && checkpoint.Case == s.lastRaw.Case && checkpoint.Target == s.lastRaw.Target {
		return fmt.Errorf("duplicate checkpoint/control for %s/%s", checkpoint.Case, checkpoint.Kind)
	}
	if s.phase >= len(orderedControlCheckpoints) {
		return fmt.Errorf("duplicate checkpoint/control after induction sequence completed")
	}
	want := orderedControlCheckpoints[s.phase]
	if checkpoint.Kind != want.kind || checkpoint.Case != want.caseName {
		return fmt.Errorf("wrong checkpoint case/order: got %s/%s, want %s/%s", checkpoint.Case, checkpoint.Kind, want.caseName, want.kind)
	}

	switch s.phase {
	case 0:
		s.exitedTarget = checkpoint.Target
		control, err := s.suspend(ctx, RunnerCaseExited, checkpoint, controls)
		if err != nil {
			return err
		}
		s.lastControl = control
	case 1:
		if checkpoint.Target != s.exitedTarget {
			return fmt.Errorf("exited checkpoint process identity changed")
		}
		if checkpoint.MonotonicMS < s.lastControl.MonotonicMS {
			return fmt.Errorf("exited delivery preceded verified suspension")
		}
		control, err := s.close(ctx, checkpoint, controls)
		if err != nil {
			return err
		}
		s.lastControl = control
	case 2:
		if checkpoint.Target == s.exitedTarget {
			return fmt.Errorf("timeout case reused exited process identity")
		}
		s.timeoutTarget = checkpoint.Target
		control, err := s.suspend(ctx, RunnerCaseTimeout, checkpoint, controls)
		if err != nil {
			return err
		}
		s.lastControl = control
	case 3:
		if checkpoint.Target != s.timeoutTarget {
			return fmt.Errorf("timeout checkpoint process identity changed")
		}
		if checkpoint.MonotonicMS < s.lastControl.MonotonicMS {
			return fmt.Errorf("timeout delivery preceded verified suspension")
		}
	}

	s.lastCheckpoint = checkpoint.MonotonicMS
	s.lastRaw = checkpoint
	s.phase++
	return nil
}

func (s *runnerControlState) suspend(ctx context.Context, caseName RunnerCase, checkpoint RunnerCheckpoint, controls *[]RecordedControl) (ControlCheckpoint, error) {
	result, err := s.controller.SuspendExactProcess(ctx, checkpoint.Target)
	if err != nil {
		err = fmt.Errorf("%s exact process suspension failed: %w", caseName, err)
	} else if result.Target != checkpoint.Target {
		err = fmt.Errorf("%s suspension controller changed process identity", caseName)
	} else if !result.StoppedVerified {
		err = fmt.Errorf("%s exact process suspension was not verified", caseName)
	} else if result.MonotonicMS < checkpoint.MonotonicMS {
		err = fmt.Errorf("%s suspension control predates its checkpoint", caseName)
	}
	*controls = append(*controls, RecordedControl{Action: ControlSuspendExactProcess, Case: caseName, Requested: checkpoint.Target, Result: result, Error: err})
	return result, err
}

func (s *runnerControlState) close(ctx context.Context, checkpoint RunnerCheckpoint, controls *[]RecordedControl) (ControlCheckpoint, error) {
	result, err := s.controller.CloseExactPane(ctx, checkpoint.Target)
	if err != nil {
		err = fmt.Errorf("exited exact pane close failed: %w", err)
	} else if result.Target != checkpoint.Target {
		err = fmt.Errorf("exited pane-close controller changed process identity")
	} else if !result.StoppedVerified {
		err = fmt.Errorf("exited pane close did not retain verified suspension")
	} else if result.MonotonicMS < checkpoint.MonotonicMS {
		err = fmt.Errorf("exited pane close preceded durable delivery")
	}
	*controls = append(*controls, RecordedControl{Action: ControlCloseExactPane, Case: RunnerCaseExited, Requested: checkpoint.Target, Result: result, Error: err})
	return result, err
}

func (s *runnerControlState) complete() error {
	if s.phase != len(orderedControlCheckpoints) {
		return fmt.Errorf("controller checkpoint sequence ended incomplete after %d of %d checkpoints", s.phase, len(orderedControlCheckpoints))
	}
	return nil
}

func validProcessIdentity(identity ProcessIdentity) bool {
	return identity.PID > 0 && identity.StartTime != "" && identity.TerminalID != "" && identity.AttachmentID != ""
}
