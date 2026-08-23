package domain

import (
	"testing"

	"github.com/procrastivity/duo/internal/sessioncore"
)

// TestInstanceTransitionsMatchSessioncore guards a deliberate duplication.
//
// internal/sessioncore already encodes decision-01 §5.1's runtime-instance
// diagram, for Step 11's hostless-session proof. The kernel does not import
// it: sessioncore's contract lets it import internal/host and
// internal/runtime freely, and the domain kernel may not depend on adapter
// packages. Duplication is the price, and this test is what keeps the two
// tables from drifting apart while both exist.
func TestInstanceTransitionsMatchSessioncore(t *testing.T) {
	states := []InstanceState{
		InstanceStarting, InstanceLive, InstanceStopRequested, InstanceExited,
	}
	for _, from := range states {
		for _, to := range states {
			session := &sessioncore.HostlessSession{
				State: sessioncore.RuntimeInstanceState(from),
			}
			want := session.Advance(sessioncore.RuntimeInstanceState(to)) == nil
			if got := canAdvance(from, to); got != want {
				t.Errorf("%s -> %s: domain allows %v, sessioncore allows %v",
					from, to, got, want)
			}
		}
	}
}
