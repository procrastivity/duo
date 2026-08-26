package herdr

import (
	"context"
	"fmt"

	"github.com/procrastivity/duo/internal/host"
)

// AgentSessionRef is the host-reported agent-session identity the
// post-launch bind pass consumes (notes/07-herdr AgentSessionInfo).
type AgentSessionRef = host.AgentSessionIdentity

// AgentBindState is the subset of a Herdr 0.8.2 agent record the bind
// pass needs for one pane: session identity plus D3 readiness flags
// (past launch_pending; interactive_ready when the record carries it).
type AgentBindState = host.AgentBindState

var _ host.AgentIdentitySource = (*Host)(nil)

// AgentOnPane looks up the agent record for a pane Duo already launched.
//
// This is a name/identity lookup for that pane, not an inventory of what
// exists: a missing row is (zero, false, nil) and must not be read as
// "the process is gone" (doc.go; spontaneous durable deregistration on
// foreground loss). Start must not call this — settledBirth stays a
// process-handover wait.
func (h *Host) AgentOnPane(ctx context.Context, paneID string) (host.AgentBindState, bool, error) {
	if paneID == "" {
		return host.AgentBindState{}, false, fmt.Errorf("herdr: agent lookup has no pane ID")
	}
	var listed agentListResult
	if err := h.client.call(ctx, "agent.list", nil, &listed); err != nil {
		return host.AgentBindState{}, false, err
	}
	for _, agent := range listed.Agents {
		if agent.PaneID != paneID {
			continue
		}
		state := host.AgentBindState{
			LaunchPending:    agent.LaunchPending,
			InteractiveReady: agent.InteractiveReady,
		}
		if agent.AgentSession != nil {
			ref := host.AgentSessionIdentity{
				Source: agent.AgentSession.Source,
				Agent:  agent.AgentSession.Agent,
				Kind:   agent.AgentSession.Kind,
				Value:  agent.AgentSession.Value,
			}
			state.Session = &ref
		}
		return state, true, nil
	}
	return host.AgentBindState{}, false, nil
}
