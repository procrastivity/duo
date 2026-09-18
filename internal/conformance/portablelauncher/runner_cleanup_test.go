package portablelauncher

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type cleanupTestController struct {
	*fakeFaultController

	mu             sync.Mutex
	cleanupCalls   int
	cleanupContext context.Context
	cleanupErr     error
	onCleanup      func(context.Context)
}

func (c *cleanupTestController) Cleanup(ctx context.Context) error {
	c.mu.Lock()
	c.cleanupCalls++
	c.cleanupContext = ctx
	onCleanup := c.onCleanup
	err := c.cleanupErr
	c.mu.Unlock()

	if onCleanup != nil {
		onCleanup(ctx)
	}
	return err
}

func (c *cleanupTestController) cleanupState() (int, context.Context) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cleanupCalls, c.cleanupContext
}

type cleanupEventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *cleanupEventLog) append(event string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, event)
}

func (l *cleanupEventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

func TestRunCommonCleansControllerBeforeFixtureAndRetainsBothErrors(t *testing.T) {
	controllerErr := errors.New("controller cleanup refused")
	fixtureErr := errors.New("fixture resource remains")
	events := &cleanupEventLog{}
	rootPresentDuringControllerCleanup := false
	controller := &cleanupTestController{
		fakeFaultController: &fakeFaultController{},
		cleanupErr:          controllerErr,
	}
	input, _ := runnerTestInput(t, controller, happyRunnerCheckpoints())
	root := input.Fixture.Root
	controller.onCleanup = func(context.Context) {
		events.append("controller")
		_, err := os.Stat(root)
		rootPresentDuringControllerCleanup = err == nil
	}
	input.InspectResources = func(context.Context, *Fixture) error {
		events.append("inspect")
		return fixtureErr
	}
	input.ExportEvidence = func(context.Context, *Fixture) error {
		events.append("export")
		return nil
	}

	record := runCommon(context.Background(), input, runnerConfig{completeTimeout: time.Second})

	if record.RunnerError != nil {
		t.Fatalf("runner error = %v, want nil", record.RunnerError)
	}
	if got, want := events.snapshot(), []string{"controller", "inspect", "export"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cleanup order = %v, want %v", got, want)
	}
	if calls, _ := controller.cleanupState(); calls != 1 {
		t.Fatalf("controller cleanup calls = %d, want 1", calls)
	}
	if !rootPresentDuringControllerCleanup {
		t.Fatal("fixture root was absent before controller cleanup")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("fixture root still exists after cleanup: %v", err)
	}
	if record.CleanupError == nil {
		t.Fatal("cleanup error = nil, want controller and fixture failures")
	}
	if !errors.Is(record.CleanupError, controllerErr) {
		t.Fatalf("cleanup error does not retain controller failure: %v", record.CleanupError)
	}
	var fixtureProblems *Problems
	if !errors.As(record.CleanupError, &fixtureProblems) || len(fixtureProblems.Items) != 1 || fixtureProblems.Items[0] != "cleanup resource inspection failed: fixture resource remains" {
		t.Fatalf("cleanup error does not retain fixture failure: %v", record.CleanupError)
	}
	for _, want := range []string{
		"fault-controller cleanup failed: controller cleanup refused",
		"fixture cleanup failed: cleanup resource inspection failed: fixture resource remains",
	} {
		if !strings.Contains(record.CleanupError.Error(), want) {
			t.Fatalf("cleanup error = %v, want it to contain %q", record.CleanupError, want)
		}
	}
}

func TestRunCommonCleanupContextOutlivesCanceledRunContext(t *testing.T) {
	controller := &cleanupTestController{fakeFaultController: &fakeFaultController{}}
	input, _ := runnerTestInput(t, controller, nil)
	type contextObservation struct {
		ctx         context.Context
		err         error
		deadline    time.Time
		hasDeadline bool
	}
	var cleanupContexts []contextObservation
	observe := func(ctx context.Context) {
		deadline, hasDeadline := ctx.Deadline()
		cleanupContexts = append(cleanupContexts, contextObservation{
			ctx: ctx, err: ctx.Err(), deadline: deadline, hasDeadline: hasDeadline,
		})
	}
	controller.onCleanup = func(ctx context.Context) {
		observe(ctx)
	}
	input.InspectResources = func(ctx context.Context, _ *Fixture) error {
		observe(ctx)
		return nil
	}
	input.ExportEvidence = func(ctx context.Context, _ *Fixture) error {
		observe(ctx)
		return nil
	}
	runContext, cancel := context.WithCancel(context.Background())
	cancel()

	record := runCommon(runContext, input, runnerConfig{completeTimeout: time.Second})

	if record.CleanupError != nil {
		t.Fatalf("cleanup error = %v, want nil", record.CleanupError)
	}
	if len(cleanupContexts) != 3 {
		t.Fatalf("cleanup context observations = %d, want 3", len(cleanupContexts))
	}
	for i, observation := range cleanupContexts {
		if observation.err != nil {
			t.Fatalf("cleanup context %d inherited run cancellation: %v", i, observation.err)
		}
		if !observation.hasDeadline || time.Until(observation.deadline) <= 0 || time.Until(observation.deadline) > cleanupTimeout {
			t.Fatalf("cleanup context %d deadline = %v/%v, want active cleanup deadline", i, observation.deadline, observation.hasDeadline)
		}
		if observation.ctx != cleanupContexts[0].ctx {
			t.Fatalf("cleanup context %d differs from controller cleanup context", i)
		}
	}
	if calls, gotContext := controller.cleanupState(); calls != 1 || gotContext != cleanupContexts[0].ctx {
		t.Fatalf("controller cleanup state = calls:%d context:%v, want one call with shared context", calls, gotContext)
	}
}

func TestRunCommonNilFixtureReturnsRunnerErrorWithoutCleanup(t *testing.T) {
	record := RunCommon(context.Background(), RunnerInput{})

	if record.RunnerError == nil || record.RunnerError.Error() != "common runner requires a prepared fixture" {
		t.Fatalf("runner error = %v, want missing prepared fixture", record.RunnerError)
	}
	if record.CleanupError != nil {
		t.Fatalf("cleanup error = %v, want nil for an unprepared fixture", record.CleanupError)
	}
}

func TestRunCommonControllerWithoutCleanupPreservesFixtureCleanup(t *testing.T) {
	controller := &fakeFaultController{}
	input, cleanup := runnerTestInput(t, controller, happyRunnerCheckpoints())

	record := runCommon(context.Background(), input, runnerConfig{completeTimeout: time.Second})

	cleanup.assertOnce(t)
	if record.RunnerError != nil || record.CleanupError != nil {
		t.Fatalf("errors = runner:%v cleanup:%v, want nil", record.RunnerError, record.CleanupError)
	}
}
