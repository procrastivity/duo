package host

import "context"

// AgentSessionKind is the host-reported agent-session identity kind
// (notes/07-herdr): an opaque session id, or a path the host named.
const (
	AgentSessionKindID   = "id"
	AgentSessionKindPath = "path"
)

// AgentSessionIdentity is host-reported agent-session identity
// (notes/07-herdr AgentSessionInfo): {source, agent, kind: "id"|"path", value}.
// Kind "path" is still an agent-session identity whose value is the path;
// it is not a directory to scan for the newest transcript.
type AgentSessionIdentity struct {
	Source string
	Agent  string
	Kind   string
	Value  string
}

// AgentBindState is identity plus D3 readiness for one pane: the session
// the host named, whether the pane is still launch_pending, and
// interactive_ready when the record carries it.
type AgentBindState struct {
	Session          *AgentSessionIdentity
	LaunchPending    bool
	InteractiveReady bool
}

// AgentIdentitySource reports host-side agent-session identity for a pane
// Duo already launched. A missing row is (zero, false, nil) and is not
// "the process is gone".
type AgentIdentitySource interface {
	AgentOnPane(context.Context, string) (AgentBindState, bool, error)
}
