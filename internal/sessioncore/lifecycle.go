// Package sessioncore is the small neutral glue Step 11's hostless-session
// test needs: enough of the runtime-instance lifecycle
// (duo-vnext-decision-01-identity-lifecycle.md §5.1) to let a session
// advance from agent-runtime evidence alone, with no session-host adapter
// attached at all.
//
// This is deliberately not the domain kernel's eventual session record.
// decision-01 §5.1 names five durable session states (active with an
// attached or detached host, inactive, archived, removed) plus the
// runtime-instance states (starting, live, stop_requested, exited); this
// package implements only the latter, because the former requires host
// attachment, workspace, and correlation concepts this step does not own
// and the domain kernel has not yet decided. See docs/adapters/decisions.md.
//
// sessioncore imports both internal/host and internal/runtime freely —
// unlike those two packages, it is not itself a "host" or "runtime"
// adapter, so it is not subject to §5.4's mutual-exclusion rule. In
// practice its one test only imports internal/runtime, to keep the
// hostless proof visibly hostless.
package sessioncore

import (
	"errors"
	"fmt"
)

// RuntimeInstanceState is the runtime-instance lifecycle from decision-01
// §5.1: Starting -> Live -> (StopRequested) -> Exited. Exited is the one
// terminal state; the rest are nonterminal.
type RuntimeInstanceState string

// The four runtime-instance states decision-01 §5.1 allows.
const (
	RuntimeInstanceStarting      RuntimeInstanceState = "starting"
	RuntimeInstanceLive          RuntimeInstanceState = "live"
	RuntimeInstanceStopRequested RuntimeInstanceState = "stop_requested"
	RuntimeInstanceExited        RuntimeInstanceState = "exited"
)

// Terminal reports whether s is Exited, decision-01 §5.1's one terminal
// runtime-instance state.
func (s RuntimeInstanceState) Terminal() bool {
	return s == RuntimeInstanceExited
}

// legalNext maps each runtime-instance state to the states decision-01
// §5.1's diagram allows next:
//
//	Starting -> Live | Exited
//	Live -> StopRequested | Exited
//	StopRequested -> Exited
//	Exited -> (nothing; terminal)
var legalNext = map[RuntimeInstanceState][]RuntimeInstanceState{
	RuntimeInstanceStarting:      {RuntimeInstanceLive, RuntimeInstanceExited},
	RuntimeInstanceLive:          {RuntimeInstanceStopRequested, RuntimeInstanceExited},
	RuntimeInstanceStopRequested: {RuntimeInstanceExited},
	RuntimeInstanceExited:        {},
}

// ErrIllegalTransition is returned by HostlessSession.Advance for a
// transition decision-01 §5.1 does not allow.
var ErrIllegalTransition = errors.New("sessioncore: illegal runtime-instance transition")

// HostlessSession is a session tracked purely by its runtime instance's
// lifecycle state, with no host-attachment field of any kind. It exists to
// prove the architecture's invariant that "a future Duo session can have
// no session-host adapter and no terminal" (architecture §1) holds at the
// type level, not only in prose: nothing in this type can express a host
// attachment, so a caller cannot accidentally smuggle one in.
type HostlessSession struct {
	SessionID         string
	RuntimeInstanceID string
	State             RuntimeInstanceState
}

// NewHostlessSession starts a session with its runtime instance in the
// Starting state, matching the launch verb: "Create a Duo session and
// starting runtime instance" (decision-01 §5.2).
func NewHostlessSession(sessionID, runtimeInstanceID string) *HostlessSession {
	return &HostlessSession{
		SessionID:         sessionID,
		RuntimeInstanceID: runtimeInstanceID,
		State:             RuntimeInstanceStarting,
	}
}

// Advance moves the session's runtime-instance state to next, refusing any
// transition decision-01 §5.1's diagram does not allow — including any
// transition attempted once the state is already Exited.
func (h *HostlessSession) Advance(next RuntimeInstanceState) error {
	for _, allowed := range legalNext[h.State] {
		if allowed == next {
			h.State = next
			return nil
		}
	}
	return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, h.State, next)
}
