package herdr

// The decoded subset of Herdr's 0.8.2 result shapes. Only fields this
// adapter actually uses are declared, which is a deliberate guard as much
// as a convenience: `revision` is absent from paneInfo because at 0.8.2 it
// no longer tracks screen content, and a field nobody decodes is a field
// nobody can key on (TestNoRevisionDependence).

// paneInfo is Herdr's pane record.
//
// The two identity fields are not interchangeable. PaneID is addressing:
// the server restores it across a restart, so it collides across
// incarnations. TerminalID names the terminal that currently backs the
// pane and changes on every new one, which makes it the epoch-equivalent
// this host has (there is no server-scoped epoch at 0.8.2).
type paneInfo struct {
	PaneID      string `json:"pane_id"`
	TerminalID  string `json:"terminal_id"`
	WorkspaceID string `json:"workspace_id"`
}

// processInfo is pane.process_info's result payload. Herdr reports PIDs
// and argv but no process start time, so a birth fingerprint needs a
// second source (see evidence.go).
type processInfo struct {
	ShellPID            int            `json:"shell_pid"`
	ForegroundProcesses []processEntry `json:"foreground_processes"`
}

type processEntry struct {
	PID int `json:"pid"`
}

// sessionSnapshot is session.snapshot's payload. This is the inventory:
// every pane the server currently holds, including panes restored from
// session persistence that no event will ever replay.
//
// Agents are deliberately not decoded. A pane whose agent registration
// dropped — the spontaneous durable deregistration on foreground loss —
// still runs its process, so the agent list is not an inventory of what
// exists and must never be used as one.
type sessionSnapshot struct {
	Workspaces    []workspace `json:"workspaces"`
	Panes         []paneInfo  `json:"panes"`
	FocusedPaneID string      `json:"focused_pane_id"`
}

type workspace struct {
	WorkspaceID string `json:"workspace_id"`
}

// Result envelopes, keyed by their "type" discriminator.

type pongResult struct {
	Version  string `json:"version"`
	Protocol int    `json:"protocol"`
}

type sessionSnapshotResult struct {
	Snapshot sessionSnapshot `json:"snapshot"`
}

type paneInfoResult struct {
	Pane paneInfo `json:"pane"`
}

type paneProcessInfoResult struct {
	ProcessInfo processInfo `json:"process_info"`
}

type workspaceCreatedResult struct {
	RootPane paneInfo `json:"root_pane"`
}

// tabInfo is the decoded subset of Herdr's tab record: only the ID, which
// is all the teardown path needs to close a tab this adapter created.
type tabInfo struct {
	TabID string `json:"tab_id"`
}

// tabCreatedResult is tab.create's result ("type": "tab_created" in the
// pinned schema): the new tab plus the root pane Herdr opens in it.
type tabCreatedResult struct {
	Tab      tabInfo  `json:"tab"`
	RootPane paneInfo `json:"root_pane"`
}

// workspaceCreateParams and paneSplitParams both carry Env, which is the
// environment-scrub seam: Herdr panes inherit the *server's* environment,
// so whatever Duo needs set in the pane has to travel here.
type workspaceCreateParams struct {
	Cwd   string            `json:"cwd,omitempty"`
	Label string            `json:"label,omitempty"`
	Env   map[string]string `json:"env,omitempty"`
}

type paneSplitParams struct {
	Direction    string            `json:"direction"`
	TargetPaneID string            `json:"target_pane_id,omitempty"`
	Cwd          string            `json:"cwd,omitempty"`
	Env          map[string]string `json:"env,omitempty"`
}

// tabCreateParams carries Env for the same environment-scrub reason the
// other creation params do. focus is deliberately not sent: Herdr defaults
// it to false, and a Duo launch never steals the user's focus.
type tabCreateParams struct {
	Cwd         string            `json:"cwd,omitempty"`
	Label       string            `json:"label,omitempty"`
	Env         map[string]string `json:"env,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
}

type tabTargetParams struct {
	TabID string `json:"tab_id"`
}

type agentStartParams struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	PaneID string   `json:"pane_id"`
	Args   []string `json:"args,omitempty"`
}

type paneTargetParams struct {
	PaneID string `json:"pane_id"`
}

// agentPromptParams is agent.prompt's wire shape: {target, text, wait}.
// wait is omitted on the delivery call — Herdr wait is condition
// evidence, not acknowledgment (notes/19 §3), and this adapter reports
// admission, not a quiet-gate.
type agentPromptParams struct {
	Target string `json:"target"`
	Text   string `json:"text"`
}

// agentSessionInfo is Herdr's AgentSessionInfo (notes/07-herdr): the
// host-reported agent-session identity the post-launch bind pass needs.
// kind is "id" or "path".
type agentSessionInfo struct {
	Source string `json:"source"`
	Agent  string `json:"agent"`
	Kind   string `json:"kind"`
	Value  string `json:"value"`
}

// agentInfo is the decoded subset of a Herdr agent record.
//
// At live 0.8.2 (notes/19 §1) agent_session, interactive_ready, and the
// schema's launch_pending live on the agent record — not on the pane
// record, which carries no agent_session. This adapter therefore decodes
// identity and D3 readiness here, never by inventing a pane field the
// live shape lacks. revision is deliberately absent: at 0.8.2 it is not
// a change detector (TestNoRevisionDependence).
type agentInfo struct {
	Name             string            `json:"name"`
	PaneID           string            `json:"pane_id"`
	AgentSession     *agentSessionInfo `json:"agent_session"`
	LaunchPending    bool              `json:"launch_pending"`
	InteractiveReady bool              `json:"interactive_ready"`
}

type agentListResult struct {
	Agents []agentInfo `json:"agents"`
}
