package herdr

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/procrastivity/duo/internal/host"
)

// Fixture shaped like a live 0.8.2 agent.list row (notes/19 §1): identity
// and readiness on the agent record, not on the pane.
const agentBindFixture = `{
  "name": "duo-claude",
  "pane_id": "w1:p3",
  "agent_session": {
    "source": "herdr:claude",
    "agent": "claude",
    "kind": "id",
    "value": "c77205ab-21b0-49d9-a7f2-3fc865e49e8f"
  },
  "launch_pending": false,
  "interactive_ready": true
}`

func TestAgentInfoFixtureYieldsSessionIdentity(t *testing.T) {
	var got agentInfo
	if err := json.Unmarshal([]byte(agentBindFixture), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.PaneID != "w1:p3" || got.Name != "duo-claude" {
		t.Fatalf("agent addressing = %+v", got)
	}
	if got.AgentSession == nil {
		t.Fatal("agent_session missing from decoded fixture")
	}
	if got.AgentSession.Kind != "id" || got.AgentSession.Value != "c77205ab-21b0-49d9-a7f2-3fc865e49e8f" {
		t.Fatalf("agent_session = %+v, want kind=id and the fixture session id", got.AgentSession)
	}
	if got.AgentSession.Source != "herdr:claude" || got.AgentSession.Agent != "claude" {
		t.Fatalf("agent_session source/agent = %+v", got.AgentSession)
	}
	if got.LaunchPending {
		t.Fatal("launch_pending true; fixture is past pending")
	}
	if !got.InteractiveReady {
		t.Fatal("interactive_ready false; fixture carries ready")
	}
}

func TestAgentInfoFixturePathKind(t *testing.T) {
	const pathFixture = `{
  "name": "duo-pi",
  "pane_id": "w1:p4",
  "agent_session": {
    "source": "herdr:pi",
    "agent": "pi",
    "kind": "path",
    "value": "/tmp/pi-sessions/abc/session.jsonl"
  },
  "launch_pending": false,
  "interactive_ready": true
}`
	var got agentInfo
	if err := json.Unmarshal([]byte(pathFixture), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.AgentSession == nil || got.AgentSession.Kind != "path" {
		t.Fatalf("want path-shaped identity, got %+v", got.AgentSession)
	}
	if got.AgentSession.Value != "/tmp/pi-sessions/abc/session.jsonl" {
		t.Fatalf("path value = %q", got.AgentSession.Value)
	}
}

func TestAgentOnPaneRoundTripsBindFields(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	f.addAgent("duo-claude", pane.paneID)
	f.setAgentBindState(pane.paneID, &agentSessionInfo{
		Source: "herdr:claude",
		Agent:  "claude",
		Kind:   "id",
		Value:  "sess-round-trip",
	}, false, true)
	h := testHost(t, f)

	state, found, err := h.AgentOnPane(context.Background(), pane.paneID)
	if err != nil {
		t.Fatalf("AgentOnPane: %v", err)
	}
	if !found {
		t.Fatal("AgentOnPane: agent row missing")
	}
	if state.Session == nil || state.Session.Value != "sess-round-trip" || state.Session.Kind != "id" {
		t.Fatalf("Session = %+v, want id sess-round-trip", state.Session)
	}
	if state.LaunchPending {
		t.Fatal("LaunchPending true after setAgentBindState cleared it")
	}
	if !state.InteractiveReady {
		t.Fatal("InteractiveReady false")
	}
	if f.callCount("agent.list") != 1 {
		t.Fatalf("agent.list calls = %d, want 1", f.callCount("agent.list"))
	}
}

func TestAgentOnPaneMissingRowIsNotProcessGone(t *testing.T) {
	f := newFakeHerdr(t)
	pane := f.addPane("w1")
	h := testHost(t, f)

	state, found, err := h.AgentOnPane(context.Background(), pane.paneID)
	if err != nil {
		t.Fatalf("AgentOnPane: %v", err)
	}
	if found {
		t.Fatalf("found = true with state %+v; missing registry row must not invent an agent", state)
	}
	if state.Session != nil {
		t.Fatalf("Session = %+v on a missing row", state.Session)
	}
}

// Start must not consult agent.list just because bind decode exists:
// settledBirth is process handover, and the registry is not an inventory.
func TestStartDoesNotCallAgentList(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	f.setAgentStartPID(7777)
	h := testHost(t, f)
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	if _, err := h.Start(ctx, prepared); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n := f.callCount("agent.list"); n != 0 {
		t.Errorf("agent.list called %d times on Start; bind decode must not pull Start onto the registry", n)
	}
}
