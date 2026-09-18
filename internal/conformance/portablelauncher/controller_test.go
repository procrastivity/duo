package portablelauncher

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	controllerExitedIdentity = ProcessIdentity{
		PID: 701, StartTime: "birth-alpha", TerminalID: "terminal-zeta", AttachmentID: "attachment-beta",
	}
	controllerTimeoutIdentity = ProcessIdentity{
		PID: 907, StartTime: "birth-gamma", TerminalID: "terminal-alpha", AttachmentID: "attachment-omega",
	}
)

const (
	operationVerifyLive   = "verify-live"
	operationStop         = "stop"
	operationVerifyStop   = "verify-stopped"
	operationClosePane    = "close-pane"
	operationVerifyAbsent = "verify-absent"
)

type controllerOperationCall struct {
	name   string
	target ProcessIdentity
}

type controllerOperationSpy struct {
	mu       sync.Mutex
	calls    []controllerOperationCall
	failures map[string][]error
}

func (s *controllerOperationSpy) operation(name string) ExactControllerOperation {
	return func(_ context.Context, target ProcessIdentity) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.calls = append(s.calls, controllerOperationCall{name: name, target: target})
		if len(s.failures[name]) == 0 {
			return nil
		}
		err := s.failures[name][0]
		s.failures[name] = s.failures[name][1:]
		return err
	}
}

func (s *controllerOperationSpy) config(clock MonotonicClock) ExactFaultControllerConfig {
	return ExactFaultControllerConfig{
		VerifyLiveTarget: s.operation(operationVerifyLive),
		StopProcess:      s.operation(operationStop),
		VerifyStopped:    s.operation(operationVerifyStop),
		ClosePane:        s.operation(operationClosePane),
		VerifyPaneAbsent: s.operation(operationVerifyAbsent),
		Monotonic:        clock,
	}
}

func (s *controllerOperationSpy) recorded() []controllerOperationCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]controllerOperationCall(nil), s.calls...)
}

func (s *controllerOperationSpy) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = nil
}

func TestExactFaultControllerRunnerSequenceUsesExactOrderedOperations(t *testing.T) {
	spy := &controllerOperationSpy{}
	clock := &offsetClock{offsets: []time.Duration{11 * time.Millisecond, 22 * time.Millisecond, 33 * time.Millisecond}}
	controller := newExactControllerForTest(t, spy, clock.Now)

	exitedSuspend, err := controller.SuspendExactProcess(context.Background(), controllerExitedIdentity)
	if err != nil {
		t.Fatalf("SuspendExactProcess(exited): %v", err)
	}
	exitedClose, err := controller.CloseExactPane(context.Background(), controllerExitedIdentity)
	if err != nil {
		t.Fatalf("CloseExactPane(exited): %v", err)
	}
	timeoutSuspend, err := controller.SuspendExactProcess(context.Background(), controllerTimeoutIdentity)
	if err != nil {
		t.Fatalf("SuspendExactProcess(timeout): %v", err)
	}

	assertControlCheckpoint(t, exitedSuspend, controllerExitedIdentity, 11)
	assertControlCheckpoint(t, exitedClose, controllerExitedIdentity, 22)
	assertControlCheckpoint(t, timeoutSuspend, controllerTimeoutIdentity, 33)
	want := []controllerOperationCall{
		{name: operationVerifyLive, target: controllerExitedIdentity},
		{name: operationStop, target: controllerExitedIdentity},
		{name: operationVerifyStop, target: controllerExitedIdentity},
		{name: operationVerifyStop, target: controllerExitedIdentity},
		{name: operationClosePane, target: controllerExitedIdentity},
		{name: operationVerifyAbsent, target: controllerExitedIdentity},
		{name: operationVerifyLive, target: controllerTimeoutIdentity},
		{name: operationStop, target: controllerTimeoutIdentity},
		{name: operationVerifyStop, target: controllerTimeoutIdentity},
	}
	if got := spy.recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("operation calls = %+v, want %+v", got, want)
	}
}

func TestExactFaultControllerSuspendFailsClosedAtEveryBoundary(t *testing.T) {
	wantFailure := errors.New("injected suspension failure")
	tests := []struct {
		name      string
		operation string
		wantCalls []string
	}{
		{name: "live target verification", operation: operationVerifyLive, wantCalls: []string{operationVerifyLive}},
		{name: "exact stop", operation: operationStop, wantCalls: []string{operationVerifyLive, operationStop}},
		{name: "stopped state verification", operation: operationVerifyStop, wantCalls: []string{operationVerifyLive, operationStop, operationVerifyStop}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spy := &controllerOperationSpy{failures: map[string][]error{test.operation: {wantFailure}}}
			controller := newExactControllerForTest(t, spy, func() time.Duration { return time.Millisecond })

			if _, err := controller.SuspendExactProcess(context.Background(), controllerExitedIdentity); !errors.Is(err, wantFailure) {
				t.Fatalf("SuspendExactProcess error = %v, want injected failure", err)
			}
			assertOperationNames(t, spy.recorded(), test.wantCalls)

			beforeClose := len(spy.recorded())
			if _, err := controller.CloseExactPane(context.Background(), controllerExitedIdentity); err == nil || !strings.Contains(err.Error(), "prior verified suspension") {
				t.Fatalf("CloseExactPane after failed suspension error = %v", err)
			}
			if got := len(spy.recorded()); got != beforeClose {
				t.Fatalf("close after failed suspension invoked %d operations, want none", got-beforeClose)
			}
		})
	}
}

func TestExactFaultControllerCloseFailsClosedAndRemainsRetryable(t *testing.T) {
	wantFailure := errors.New("injected close failure")
	tests := []struct {
		name      string
		operation string
		wantCalls []string
	}{
		{name: "stopped state reverification", operation: operationVerifyStop, wantCalls: []string{operationVerifyStop}},
		{name: "exact pane close", operation: operationClosePane, wantCalls: []string{operationVerifyStop, operationClosePane}},
		{name: "pane absence verification", operation: operationVerifyAbsent, wantCalls: []string{operationVerifyStop, operationClosePane, operationVerifyAbsent}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spy := &controllerOperationSpy{}
			controller := newExactControllerForTest(t, spy, func() time.Duration { return 9 * time.Millisecond })
			if _, err := controller.SuspendExactProcess(context.Background(), controllerExitedIdentity); err != nil {
				t.Fatalf("SuspendExactProcess: %v", err)
			}
			spy.reset()
			spy.failures = map[string][]error{test.operation: {wantFailure}}

			if _, err := controller.CloseExactPane(context.Background(), controllerExitedIdentity); !errors.Is(err, wantFailure) {
				t.Fatalf("CloseExactPane error = %v, want injected failure", err)
			}
			assertOperationNames(t, spy.recorded(), test.wantCalls)

			spy.reset()
			checkpoint, err := controller.CloseExactPane(context.Background(), controllerExitedIdentity)
			if err != nil {
				t.Fatalf("CloseExactPane retry: %v", err)
			}
			assertControlCheckpoint(t, checkpoint, controllerExitedIdentity, 9)
			assertOperationNames(t, spy.recorded(), []string{operationVerifyStop, operationClosePane, operationVerifyAbsent})
		})
	}
}

func TestExactFaultControllerRejectsIncompleteIdentitiesWithoutOperations(t *testing.T) {
	tests := []struct {
		name   string
		target ProcessIdentity
	}{
		{name: "zero value", target: ProcessIdentity{}},
		{name: "nonpositive PID", target: ProcessIdentity{PID: -1, StartTime: "birth", TerminalID: "terminal", AttachmentID: "attachment"}},
		{name: "missing start time", target: ProcessIdentity{PID: 1, TerminalID: "terminal", AttachmentID: "attachment"}},
		{name: "missing terminal", target: ProcessIdentity{PID: 1, StartTime: "birth", AttachmentID: "attachment"}},
		{name: "missing attachment", target: ProcessIdentity{PID: 1, StartTime: "birth", TerminalID: "terminal"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spy := &controllerOperationSpy{}
			controller := newExactControllerForTest(t, spy, func() time.Duration { return 0 })
			if _, err := controller.SuspendExactProcess(context.Background(), test.target); err == nil || !strings.Contains(err.Error(), "complete process identity") {
				t.Fatalf("SuspendExactProcess error = %v", err)
			}
			if _, err := controller.CloseExactPane(context.Background(), test.target); err == nil || !strings.Contains(err.Error(), "complete process identity") {
				t.Fatalf("CloseExactPane error = %v", err)
			}
			if calls := spy.recorded(); len(calls) != 0 {
				t.Fatalf("incomplete identity operations = %+v, want none", calls)
			}
		})
	}
}

func TestExactFaultControllerRejectsCloseBeforeSuspension(t *testing.T) {
	spy := &controllerOperationSpy{}
	controller := newExactControllerForTest(t, spy, func() time.Duration { return 0 })
	if _, err := controller.CloseExactPane(context.Background(), controllerExitedIdentity); err == nil || !strings.Contains(err.Error(), "prior verified suspension") {
		t.Fatalf("CloseExactPane error = %v", err)
	}
	if calls := spy.recorded(); len(calls) != 0 {
		t.Fatalf("close-before-suspend operations = %+v, want none", calls)
	}
}

func TestExactFaultControllerDoesNotReuseSuspensionAcrossIdentities(t *testing.T) {
	spy := &controllerOperationSpy{}
	controller := newExactControllerForTest(t, spy, func() time.Duration { return 0 })
	if _, err := controller.SuspendExactProcess(context.Background(), controllerExitedIdentity); err != nil {
		t.Fatalf("SuspendExactProcess: %v", err)
	}
	spy.reset()

	mismatches := []ProcessIdentity{
		{PID: controllerExitedIdentity.PID, StartTime: controllerTimeoutIdentity.StartTime, TerminalID: controllerExitedIdentity.TerminalID, AttachmentID: controllerExitedIdentity.AttachmentID},
		{PID: controllerExitedIdentity.PID, StartTime: controllerExitedIdentity.StartTime, TerminalID: controllerTimeoutIdentity.TerminalID, AttachmentID: controllerExitedIdentity.AttachmentID},
		{PID: controllerExitedIdentity.PID, StartTime: controllerExitedIdentity.StartTime, TerminalID: controllerExitedIdentity.TerminalID, AttachmentID: controllerTimeoutIdentity.AttachmentID},
		controllerTimeoutIdentity,
	}
	for _, target := range mismatches {
		if _, err := controller.CloseExactPane(context.Background(), target); err == nil || !strings.Contains(err.Error(), "prior verified suspension") {
			t.Fatalf("CloseExactPane(%+v) error = %v", target, err)
		}
	}
	if calls := spy.recorded(); len(calls) != 0 {
		t.Fatalf("mismatched identity operations = %+v, want none", calls)
	}

	if _, err := controller.CloseExactPane(context.Background(), controllerExitedIdentity); err != nil {
		t.Fatalf("CloseExactPane(original): %v", err)
	}
}

func TestExactFaultControllerCheckpointMillisecondsNeverRegress(t *testing.T) {
	spy := &controllerOperationSpy{}
	clock := &offsetClock{offsets: []time.Duration{
		19*time.Millisecond + 900*time.Microsecond,
		7 * time.Millisecond,
		-3 * time.Millisecond,
	}}
	controller := newExactControllerForTest(t, spy, clock.Now)

	first, err := controller.SuspendExactProcess(context.Background(), controllerExitedIdentity)
	if err != nil {
		t.Fatalf("first suspension: %v", err)
	}
	second, err := controller.CloseExactPane(context.Background(), controllerExitedIdentity)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	third, err := controller.SuspendExactProcess(context.Background(), controllerTimeoutIdentity)
	if err != nil {
		t.Fatalf("second suspension: %v", err)
	}
	if got, want := []int64{first.MonotonicMS, second.MonotonicMS, third.MonotonicMS}, []int64{19, 19, 19}; !reflect.DeepEqual(got, want) {
		t.Fatalf("checkpoint milliseconds = %v, want %v", got, want)
	}
}

func TestNewExactFaultControllerRequiresCompleteConfig(t *testing.T) {
	noop := func(context.Context, ProcessIdentity) error { return nil }
	complete := ExactFaultControllerConfig{
		VerifyLiveTarget: noop,
		StopProcess:      noop,
		VerifyStopped:    noop,
		ClosePane:        noop,
		VerifyPaneAbsent: noop,
		Monotonic:        func() time.Duration { return 0 },
	}
	tests := []struct {
		name      string
		mutate    func(*ExactFaultControllerConfig)
		wantError string
	}{
		{name: "live target verifier", mutate: func(c *ExactFaultControllerConfig) { c.VerifyLiveTarget = nil }},
		{name: "exact stopper", mutate: func(c *ExactFaultControllerConfig) { c.StopProcess = nil }},
		{name: "stopped verifier", mutate: func(c *ExactFaultControllerConfig) { c.VerifyStopped = nil }},
		{name: "pane closer", mutate: func(c *ExactFaultControllerConfig) { c.ClosePane = nil }},
		{name: "pane absence verifier", mutate: func(c *ExactFaultControllerConfig) { c.VerifyPaneAbsent = nil }},
		{
			name:      "monotonic clock",
			mutate:    func(c *ExactFaultControllerConfig) { c.Monotonic = nil },
			wantError: "requires a monotonic clock shared with the checkpoint observer",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := complete
			test.mutate(&config)
			_, err := NewExactFaultController(config)
			if err == nil {
				t.Fatal("NewExactFaultController accepted an incomplete config")
			}
			if test.wantError != "" && !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("NewExactFaultController error = %q, want it to contain %q", err, test.wantError)
			}
		})
	}
}

func newExactControllerForTest(t *testing.T, spy *controllerOperationSpy, clock MonotonicClock) *ExactFaultController {
	t.Helper()
	controller, err := NewExactFaultController(spy.config(clock))
	if err != nil {
		t.Fatalf("NewExactFaultController: %v", err)
	}
	return controller
}

func assertControlCheckpoint(t *testing.T, got ControlCheckpoint, target ProcessIdentity, milliseconds int64) {
	t.Helper()
	want := ControlCheckpoint{Target: target, StoppedVerified: true, MonotonicMS: milliseconds}
	if got != want {
		t.Fatalf("checkpoint = %+v, want %+v", got, want)
	}
}

func assertOperationNames(t *testing.T, calls []controllerOperationCall, want []string) {
	t.Helper()
	got := make([]string, len(calls))
	for index, call := range calls {
		got[index] = call.name
		if call.target != controllerExitedIdentity {
			t.Fatalf("operation %d target = %+v, want %+v", index, call.target, controllerExitedIdentity)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("operation names = %v, want %v", got, want)
	}
}
