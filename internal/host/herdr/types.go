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

type agentStartParams struct {
	Name   string   `json:"name"`
	Kind   string   `json:"kind"`
	PaneID string   `json:"pane_id"`
	Args   []string `json:"args,omitempty"`
}

type paneTargetParams struct {
	PaneID string `json:"pane_id"`
}
