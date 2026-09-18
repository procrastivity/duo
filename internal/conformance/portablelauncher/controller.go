package portablelauncher

import (
	"context"
	"fmt"
	"sync"
)

// ExactControllerOperation performs one controller action against the full
// process-and-attachment identity. Implementations must fail rather than act
// on a PID, terminal, or attachment match alone.
type ExactControllerOperation func(context.Context, ProcessIdentity) error

// ExactFaultControllerConfig supplies every operation needed for verified
// fault induction. Keeping all five operations mandatory leaves no concrete
// controller path that can fall back to PID-only signaling or an unverified
// pane close.
type ExactFaultControllerConfig struct {
	VerifyLiveTarget ExactControllerOperation
	StopProcess      ExactControllerOperation
	VerifyStopped    ExactControllerOperation
	ClosePane        ExactControllerOperation
	VerifyPaneAbsent ExactControllerOperation
	Monotonic        MonotonicClock
}

// ExactFaultController applies launcher-neutral exact process and pane
// controls. It serializes complete control sequences so verification and the
// action it guards cannot be interleaved by another caller.
type ExactFaultController struct {
	config ExactFaultControllerConfig

	mu               sync.Mutex
	suspended        map[ProcessIdentity]struct{}
	lastCheckpointMS int64
}

var _ FaultController = (*ExactFaultController)(nil)

// NewExactFaultController validates and constructs an exact fault controller.
func NewExactFaultController(config ExactFaultControllerConfig) (*ExactFaultController, error) {
	switch {
	case config.VerifyLiveTarget == nil:
		return nil, fmt.Errorf("exact fault controller requires live-target verification")
	case config.StopProcess == nil:
		return nil, fmt.Errorf("exact fault controller requires an exact stop operation")
	case config.VerifyStopped == nil:
		return nil, fmt.Errorf("exact fault controller requires stopped-state verification")
	case config.ClosePane == nil:
		return nil, fmt.Errorf("exact fault controller requires an exact pane-close operation")
	case config.VerifyPaneAbsent == nil:
		return nil, fmt.Errorf("exact fault controller requires pane-absence verification")
	case config.Monotonic == nil:
		return nil, fmt.Errorf("exact fault controller requires a monotonic clock shared with the checkpoint observer")
	}
	return &ExactFaultController{
		config:    config,
		suspended: make(map[ProcessIdentity]struct{}),
	}, nil
}

// SuspendExactProcess verifies the full live identity, stops that identity,
// and independently verifies its stopped state. A failed sequence is never
// recorded as an eligible suspension.
func (c *ExactFaultController) SuspendExactProcess(ctx context.Context, target ProcessIdentity) (ControlCheckpoint, error) {
	if !validProcessIdentity(target) {
		return ControlCheckpoint{}, fmt.Errorf("exact process suspension requires a complete process identity")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.config.VerifyLiveTarget(ctx, target); err != nil {
		return ControlCheckpoint{}, fmt.Errorf("verify exact live target: %w", err)
	}
	if err := c.config.StopProcess(ctx, target); err != nil {
		return ControlCheckpoint{}, fmt.Errorf("stop exact process: %w", err)
	}
	if err := c.config.VerifyStopped(ctx, target); err != nil {
		return ControlCheckpoint{}, fmt.Errorf("verify exact process stopped: %w", err)
	}

	checkpoint := c.checkpoint(target)
	c.suspended[target] = struct{}{}
	return checkpoint, nil
}

// CloseExactPane closes only an exact identity successfully suspended by this
// controller. Failures retain the suspension record so callers can retry or
// conservatively clean up; only an independently verified absence consumes it.
func (c *ExactFaultController) CloseExactPane(ctx context.Context, target ProcessIdentity) (ControlCheckpoint, error) {
	if !validProcessIdentity(target) {
		return ControlCheckpoint{}, fmt.Errorf("exact pane close requires a complete process identity")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.suspended[target]; !ok {
		return ControlCheckpoint{}, fmt.Errorf("exact pane close requires a prior verified suspension for the requested identity")
	}
	if err := c.config.VerifyStopped(ctx, target); err != nil {
		return ControlCheckpoint{}, fmt.Errorf("reverify exact process stopped: %w", err)
	}
	if err := c.config.ClosePane(ctx, target); err != nil {
		return ControlCheckpoint{}, fmt.Errorf("close exact pane: %w", err)
	}
	if err := c.config.VerifyPaneAbsent(ctx, target); err != nil {
		return ControlCheckpoint{}, fmt.Errorf("verify exact pane absent: %w", err)
	}

	checkpoint := c.checkpoint(target)
	delete(c.suspended, target)
	return checkpoint, nil
}

func (c *ExactFaultController) checkpoint(target ProcessIdentity) ControlCheckpoint {
	milliseconds := c.config.Monotonic().Milliseconds()
	if milliseconds < 0 {
		milliseconds = 0
	}
	if milliseconds < c.lastCheckpointMS {
		milliseconds = c.lastCheckpointMS
	}
	c.lastCheckpointMS = milliseconds
	return ControlCheckpoint{Target: target, StoppedVerified: true, MonotonicMS: milliseconds}
}
