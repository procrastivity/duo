package herdr

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/host"
)

// Host is the Herdr session-host adapter. One Host serves one Herdr
// server, addressed by its API socket.
type Host struct {
	cfg    Config
	client *client
	probe  adapter.Probe
}

var (
	_ host.HostDiscovery           = (*Host)(nil)
	_ host.HostLauncher            = (*Host)(nil)
	_ host.HostAttachmentValidator = (*Host)(nil)
	_ host.HostLifecycleSource     = (*Host)(nil)
)

// Config is everything a Host needs to reach and interpret one Herdr
// server.
type Config struct {
	// IntegrationInstanceID identifies this Herdr server as a Duo
	// integration instance. It is the only place the Herdr session name
	// enters evidence, because Herdr itself exposes no server identity;
	// see InstanceIDForSession.
	IntegrationInstanceID string
	// SocketPath is the Herdr API socket (herdr.sock, not
	// herdr-client.sock, which is the UI transport and carries no API).
	SocketPath string
	// Binary is the herdr executable the §5.1 probe runs to export a
	// schema for digest comparison. Empty means "herdr" on PATH; 0.8.2
	// also publishes HERDR_BIN_PATH into every pane, which is a usable
	// source for a Duo running inside Herdr.
	Binary string
	// CallTimeout bounds one request/response round trip. It does not
	// bound an open event subscription, which must block indefinitely.
	CallTimeout time.Duration
	// SnapshotInterval is how often an open lifecycle observation
	// re-diffs session.snapshot even when no event arrives. This is the
	// snapshot-diff duty made concrete: events can be missed entirely
	// (no cursor, no replay token) and pane.created backfill is partial,
	// so the poll is the floor on how stale an observation can be.
	SnapshotInterval time.Duration
	// StartRetryLimit and StartRetryDelay bound the agent_pane_busy retry
	// in Start. That refusal is proven no-effect at 0.8.2 (the text never
	// reaches the pane), so retrying it cannot double-deliver.
	StartRetryLimit int
	StartRetryDelay time.Duration
	// LaunchSettleTimeout and LaunchSettlePoll bound how long Start waits
	// for the pane's foreground process to change after agent.start.
	// agent.start is asynchronous at 0.8.2 — it returns launch_pending
	// true, before the agent process exists — so evidence taken at that
	// instant names the pane's shell, not what was launched.
	LaunchSettleTimeout time.Duration
	LaunchSettlePoll    time.Duration
	// AgentNamePrefix prefixes the Herdr agent name Start registers.
	AgentNamePrefix string
	// KindByCommand maps a resolved launch command to a Herdr agent kind
	// when the base name of the command is not the kind. Herdr's kinds are
	// a fixed enum served by its manifests, not free-form commands.
	KindByCommand map[string]string
	// ResolveProcessBirth supplies birth evidence for a pane's process.
	// Defaults to procfs.
	ResolveProcessBirth ProcessBirthResolver
	// Now is the clock, injectable for tests.
	Now func() time.Time
}

// Defaults for Config, exported so callers can see what they are opting
// out of rather than rediscovering it.
const (
	DefaultCallTimeout         = 10 * time.Second
	DefaultSnapshotInterval    = 2 * time.Second
	DefaultStartRetryLimit     = 10
	DefaultStartRetryDelay     = time.Second
	DefaultLaunchSettleTimeout = 10 * time.Second
	DefaultLaunchSettlePoll    = 250 * time.Millisecond
	DefaultAgentNamePrefix     = "duo"
)

func (c Config) withDefaults() Config {
	if c.CallTimeout <= 0 {
		c.CallTimeout = DefaultCallTimeout
	}
	if c.SnapshotInterval <= 0 {
		c.SnapshotInterval = DefaultSnapshotInterval
	}
	if c.StartRetryLimit <= 0 {
		c.StartRetryLimit = DefaultStartRetryLimit
	}
	if c.StartRetryDelay <= 0 {
		c.StartRetryDelay = DefaultStartRetryDelay
	}
	if c.LaunchSettleTimeout <= 0 {
		c.LaunchSettleTimeout = DefaultLaunchSettleTimeout
	}
	if c.LaunchSettlePoll <= 0 {
		c.LaunchSettlePoll = DefaultLaunchSettlePoll
	}
	if c.AgentNamePrefix == "" {
		c.AgentNamePrefix = DefaultAgentNamePrefix
	}
	if c.ResolveProcessBirth == nil {
		c.ResolveProcessBirth = procfsBirth
	}
	if c.Now == nil {
		c.Now = func() time.Time { return time.Now().UTC() }
	}
	return c
}

func (c Config) validate() error {
	if c.IntegrationInstanceID == "" {
		return errors.New("herdr: Config.IntegrationInstanceID is required")
	}
	if c.SocketPath == "" {
		return errors.New("herdr: Config.SocketPath is required")
	}
	return nil
}

// InstanceIDForSession is the conventional integration-instance ID for a
// Herdr session name. Herdr has no server identity of its own, so the
// session name — which is what selects a socket — is the identity Duo
// records.
func InstanceIDForSession(session string) string {
	return "herdr:" + session
}

// New builds a Host from a validated config.
func New(cfg Config) (*Host, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	cfg = cfg.withDefaults()
	return &Host{cfg: cfg, client: newClient(cfg.SocketPath, cfg.CallTimeout)}, nil
}

// Probe returns the §5.1 probe result this Host was built from. It is the
// empty probe for a Host built directly with New rather than through
// Factory.
func (h *Host) Probe() adapter.Probe { return h.probe }

// Discover implements host.HostDiscovery.
//
// Candidates come from session.snapshot, never from Herdr's agent
// registry. At 0.8.2 an agent registration drops durably when the agent
// loses the pane foreground (ctrl-Z, a foreground child, a stopped
// process) and never returns, while the process keeps running: the
// registry is a view, not an inventory. Which candidates are agent
// runtimes is a §5.3 correlation question, not a host question.
func (h *Host) Discover(ctx context.Context, req host.DiscoveryRequest) ([]host.HostCandidate, error) {
	if err := h.requireInstance(req.IntegrationInstanceID); err != nil {
		return nil, err
	}
	snap, err := h.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	now := h.cfg.Now()
	var out []host.HostCandidate
	for _, pane := range snap.Panes {
		if req.WorkspaceHint != "" && pane.WorkspaceID != req.WorkspaceHint {
			continue
		}
		birth, err := h.processBirth(ctx, pane.PaneID)
		if err != nil {
			// A pane that vanished between the snapshot and its process
			// lookup is not a discovery failure; it is a pane that is no
			// longer there.
			if ErrorCode(err) == CodePaneNotFound {
				continue
			}
			return nil, err
		}
		out = append(out, host.HostCandidate{
			Evidence:   evidenceFor(h.cfg.IntegrationInstanceID, pane, birth),
			DetectedAt: now,
		})
	}
	return out, nil
}

// preparedLaunch is the adapter-private payload of a PreparedHostLaunch.
//
// It is where the environment-scrub seam lives: Env is the environment
// Start sets on the pane, applied to the pane's shell at creation time and
// therefore before any agent process starts. Step 20 decides what belongs
// in it.
type preparedLaunch struct {
	AgentName       string
	Kind            string
	Args            []string
	Cwd             string
	Env             map[string]string
	CreateWorkspace bool
	SplitTargetPane string
	Label           string
}

// PrepareLaunch implements host.HostLauncher.
//
// It mutates nothing on the Herdr server. Pane creation is not stageable —
// a Herdr pane exists or it does not — so all of it belongs to Start, and
// a launch that fails validation leaves no orphan pane behind.
func (h *Host) PrepareLaunch(ctx context.Context, req host.HostLaunchRequest) (host.PreparedHostLaunch, error) {
	tuple := req.ResolvedLaunchTuple
	if err := h.requireInstance(tuple.IntegrationInstanceID); err != nil {
		return host.PreparedHostLaunch{}, err
	}
	if tuple.LaunchResolutionID == "" {
		return host.PreparedHostLaunch{}, errors.New("herdr: launch tuple has no launch-resolution ID")
	}
	kind, err := h.kindFor(tuple.Command)
	if err != nil {
		return host.PreparedHostLaunch{}, err
	}

	snap, err := h.snapshot(ctx)
	if err != nil {
		return host.PreparedHostLaunch{}, err
	}
	prepared := preparedLaunch{
		AgentName: agentName(h.cfg.AgentNamePrefix, tuple.LaunchResolutionID),
		Kind:      kind,
		Args:      append([]string(nil), tuple.Args...),
		Cwd:       tuple.WorkspacePath,
		Env:       copyEnv(tuple.Env),
		Label:     agentName(h.cfg.AgentNamePrefix, tuple.LaunchResolutionID),
	}
	target := splitTarget(snap)
	if target == "" {
		prepared.CreateWorkspace = true
	} else {
		prepared.SplitTargetPane = target
	}

	return host.PreparedHostLaunch{
		IntegrationInstanceID: h.cfg.IntegrationInstanceID,
		LaunchResolutionID:    tuple.LaunchResolutionID,
		Opaque:                prepared,
	}, nil
}

// Start implements host.HostLauncher: create the pane, register the agent,
// then read back the evidence that proves what is now live.
func (h *Host) Start(ctx context.Context, launch host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	if err := h.requireInstance(launch.IntegrationInstanceID); err != nil {
		return host.HostLaunchEvidence{}, err
	}
	prepared, ok := launch.Opaque.(preparedLaunch)
	if !ok {
		return host.HostLaunchEvidence{}, fmt.Errorf(
			"herdr: prepared launch %s was not prepared by this adapter", launch.LaunchResolutionID)
	}

	pane, err := h.createPane(ctx, prepared)
	if err != nil {
		return host.HostLaunchEvidence{}, err
	}
	// The pane's foreground process before the agent starts: its shell.
	// Everything after this compares against that baseline.
	baseline, err := h.processBirth(ctx, pane.PaneID)
	if err != nil {
		return host.HostLaunchEvidence{}, err
	}
	if err := h.startAgent(ctx, prepared, pane.PaneID); err != nil {
		return host.HostLaunchEvidence{}, err
	}

	birth := h.settledBirth(ctx, pane.PaneID, baseline)

	// Read the pane back rather than trusting the creation result: process
	// birth is the half of the evidence Herdr's creation result never
	// carries, and the pane record is the only source of terminal_id.
	live, found, err := h.pane(ctx, pane.PaneID)
	if err != nil {
		return host.HostLaunchEvidence{}, err
	}
	if !found {
		return host.HostLaunchEvidence{}, fmt.Errorf("herdr: pane %s vanished during launch", pane.PaneID)
	}
	return host.HostLaunchEvidence{
		Evidence:  evidenceFor(h.cfg.IntegrationInstanceID, live, birth),
		StartedAt: h.cfg.Now(),
	}, nil
}

// settledBirth waits, bounded, for the pane's foreground process to differ
// from the pre-start baseline.
//
// agent.start is asynchronous at 0.8.2: it returns immediately with a
// launch_pending agent record, before the agent process exists. Evidence
// taken at that moment names the pane's shell, so a caller storing it as
// its attachment would watch its own attachment fail validation seconds
// later, when the shell handed the terminal to the agent (observed live).
//
// This is not an interactive-readiness wait — that is agent.wait and
// interactive_ready, which belong to a surface Stage 1 does not implement.
// It only waits for the observable handover. If it does not happen in
// time, the baseline is returned unchanged: weaker evidence, not an error,
// because the launch did happen and the caller re-fingerprints at
// correlation time anyway.
func (h *Host) settledBirth(ctx context.Context, paneID string, baseline host.ProcessBirthEvidence) host.ProcessBirthEvidence {
	deadline := h.cfg.Now().Add(h.cfg.LaunchSettleTimeout)
	for {
		birth, err := h.processBirth(ctx, paneID)
		if err != nil {
			return baseline
		}
		if birth.PID != baseline.PID {
			return birth
		}
		if !h.cfg.Now().Before(deadline) {
			return baseline
		}
		select {
		case <-ctx.Done():
			return baseline
		case <-time.After(h.cfg.LaunchSettlePoll):
		}
	}
}

func (h *Host) createPane(ctx context.Context, prepared preparedLaunch) (paneInfo, error) {
	if prepared.CreateWorkspace {
		var res workspaceCreatedResult
		params := workspaceCreateParams{Cwd: prepared.Cwd, Label: prepared.Label, Env: prepared.Env}
		if err := h.client.call(ctx, "workspace.create", params, &res); err != nil {
			return paneInfo{}, err
		}
		return res.RootPane, nil
	}
	var res paneInfoResult
	params := paneSplitParams{
		Direction:    "right",
		TargetPaneID: prepared.SplitTargetPane,
		Cwd:          prepared.Cwd,
		Env:          prepared.Env,
	}
	if err := h.client.call(ctx, "pane.split", params, &res); err != nil {
		return paneInfo{}, err
	}
	return res.Pane, nil
}

// startAgent registers the agent in a freshly created pane, retrying only
// agent_pane_busy.
//
// agent_pane_busy means the pane's shell has not reached its interactive
// prompt yet. It is a pre-delivery refusal: nothing was typed into the
// pane, verified at 0.8.2, so a retry cannot double-deliver. Every other
// typed refusal is returned unchanged — notably nothing here retries on an
// unknown effect, which is the rule the prompt path will need too.
func (h *Host) startAgent(ctx context.Context, prepared preparedLaunch, paneID string) error {
	params := agentStartParams{
		Name:   prepared.AgentName,
		Kind:   prepared.Kind,
		PaneID: paneID,
		Args:   prepared.Args,
	}
	var lastErr error
	for attempt := 0; attempt < h.cfg.StartRetryLimit; attempt++ {
		err := h.client.call(ctx, "agent.start", params, nil)
		if err == nil {
			return nil
		}
		if ErrorCode(err) != CodeAgentPaneBusy {
			return err
		}
		lastErr = err
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(h.cfg.StartRetryDelay):
		}
	}
	return fmt.Errorf("herdr: agent %s still busy after %d attempts: %w",
		prepared.AgentName, h.cfg.StartRetryLimit, lastErr)
}

// ValidateAttachment implements host.HostAttachmentValidator.
//
// Three checks, in order of strength:
//
//  1. The pane must still exist. pane_not_found is an absence, not an
//     error.
//  2. The pane's terminal_id must still match the claim. A restored pane
//     keeps its pane_id across a server restart but always gets a new
//     terminal_id, so this is what distinguishes an incarnation and stops
//     a stale claim from re-enrolling onto a different terminal.
//  3. The process birth must match. Herdr reports no start time, so this
//     needs procfs (or another ProcessBirthResolver); when birth cannot be
//     proven the verdict is "not the same process", never "same".
func (h *Host) ValidateAttachment(ctx context.Context, claim host.HostAttachmentClaim) (host.HostContinuityEvidence, error) {
	if err := h.requireInstance(claim.Attachment.IntegrationInstanceID); err != nil {
		return host.HostContinuityEvidence{}, err
	}
	live, found, err := h.pane(ctx, claim.Attachment.PaneID)
	if err != nil {
		return host.HostContinuityEvidence{}, err
	}
	if !found {
		return host.HostContinuityEvidence{SameProcess: false}, nil
	}
	birth, err := h.processBirth(ctx, live.PaneID)
	if err != nil {
		if ErrorCode(err) == CodePaneNotFound {
			return host.HostContinuityEvidence{SameProcess: false}, nil
		}
		return host.HostContinuityEvidence{}, err
	}
	evidence := evidenceFor(h.cfg.IntegrationInstanceID, live, birth)
	if claim.Attachment.HostContainerID != "" && claim.Attachment.HostContainerID != live.TerminalID {
		// A different terminal now backs this pane_id: same coordinates,
		// new incarnation. The live evidence still goes back so the caller
		// can enroll the new runtime instance instead of guessing at it.
		return host.HostContinuityEvidence{Evidence: evidence, SameProcess: false}, nil
	}
	return host.HostContinuityEvidence{
		Evidence:    evidence,
		SameProcess: sameBirth(birth, claim.LastKnownProcessBirth),
	}, nil
}

// ObserveHostLifecycle implements host.HostLifecycleSource.
//
// The stream ends when the caller calls Close or when ctx is cancelled.
func (h *Host) ObserveHostLifecycle(ctx context.Context, req host.HostObservationRequest) (host.HostObservationStream, error) {
	if err := h.requireInstance(req.Attachment.IntegrationInstanceID); err != nil {
		return nil, err
	}
	if req.Attachment.PaneID == "" {
		return nil, errors.New("herdr: observation request has no pane ID")
	}
	streamCtx, cancel := context.WithCancel(ctx)
	s := &observationStream{
		host:       h,
		attachment: req.Attachment,
		events:     make(chan host.LifecycleEvent, observationBuffer),
		wake:       make(chan struct{}, 1),
		done:       make(chan struct{}),
		cancel:     cancel,
	}
	s.start(streamCtx)
	return s, nil
}

// Request helpers.

func (h *Host) requireInstance(id string) error {
	if id != "" && id != h.cfg.IntegrationInstanceID {
		return fmt.Errorf("herdr %s: request for integration instance %s",
			h.cfg.IntegrationInstanceID, id)
	}
	return nil
}

func (h *Host) ping(ctx context.Context) (pongResult, error) {
	var res pongResult
	if err := h.client.call(ctx, "ping", nil, &res); err != nil {
		return pongResult{}, err
	}
	return res, nil
}

func (h *Host) snapshot(ctx context.Context) (sessionSnapshot, error) {
	var res sessionSnapshotResult
	if err := h.client.call(ctx, "session.snapshot", nil, &res); err != nil {
		return sessionSnapshot{}, err
	}
	return res.Snapshot, nil
}

// pane returns one pane, reporting absence as (_, false, nil).
func (h *Host) pane(ctx context.Context, paneID string) (paneInfo, bool, error) {
	var res paneInfoResult
	err := h.client.call(ctx, "pane.get", paneTargetParams{PaneID: paneID}, &res)
	if err != nil {
		if ErrorCode(err) == CodePaneNotFound {
			return paneInfo{}, false, nil
		}
		return paneInfo{}, false, err
	}
	return res.Pane, true, nil
}

func (h *Host) processBirth(ctx context.Context, paneID string) (host.ProcessBirthEvidence, error) {
	var res paneProcessInfoResult
	if err := h.client.call(ctx, "pane.process_info", paneTargetParams{PaneID: paneID}, &res); err != nil {
		return host.ProcessBirthEvidence{}, err
	}
	return h.cfg.ResolveProcessBirth(ctx, res.ProcessInfo), nil
}

func (h *Host) kindFor(command string) (string, error) {
	if kind, ok := h.cfg.KindByCommand[command]; ok && kind != "" {
		return kind, nil
	}
	kind := filepath.Base(strings.TrimSpace(command))
	if kind == "" || kind == "." || kind == string(filepath.Separator) {
		return "", fmt.Errorf("herdr: launch tuple command %q names no agent kind", command)
	}
	return kind, nil
}

// splitTarget picks the pane a new pane splits from, or "" when the
// session holds nothing to split and a workspace must be created instead.
func splitTarget(snap sessionSnapshot) string {
	if len(snap.Workspaces) == 0 || len(snap.Panes) == 0 {
		return ""
	}
	if snap.FocusedPaneID != "" {
		return snap.FocusedPaneID
	}
	return snap.Panes[0].PaneID
}

// agentName builds a Herdr agent name from a launch-resolution ID. Herdr
// agent names are a flat per-server namespace, so the resolution ID (which
// is unique per launch) is what keeps two launches apart.
func agentName(prefix, launchResolutionID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, launchResolutionID)
	return prefix + "-" + safe
}

func copyEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = v
	}
	return out
}
