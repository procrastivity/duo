package domain

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// hostcorrelation.go is the workspace↔host-instance correlation: the durable
// record of which session-host instance a workspace's new spawns go to, plus
// the two verbs that write it.
//
// It is the host sibling of the workspace *path* rebind (decision-01 §3.3,
// docs/domain/decisions.md "No path rebind flow"): the same shape — one
// current correlation, changed only by an explicit, audited rebind — applied
// to the host instance instead of the root path. `duo.config/v3` stops
// authoring the session host in configuration, so the socket path stops
// being intent and becomes state, and this is where that state lives
// (notes/42 §8, notes/43 item 13, 2026-08-24 handoff 22).
//
// The deferral docs/domain/decisions.md recorded for the path sibling — "the
// verb that decides *when* a rebind is warranted needs filesystem identity
// evidence, which is I/O, and belongs with the application layer" — is
// resolved the same way here and for the same reason: the kernel records the
// binding it is handed, and the application layer decides what to hand it.
//
// What this file deliberately does not do: it never deduces a host (the
// fixed M1 ladder — explicit flag > correlation > cwd correlation >
// ambient env > policy default — belongs to the materializer), never reads
// the environment, never
// consults a launch, and never checks that an instance is reachable. A dead
// socket fails at spawn, not here (notes/42 §5).

// HostSource is the closed provenance vocabulary for a host correlation:
// which rung of the fixed deduction ranking established the binding. The
// values are duo.external/v1's `host_source` enum verbatim, so a fact, a
// launch record, and a wire envelope all spell provenance the same way.
type HostSource string

// The five host-deduction rungs, in rank order.
const (
	// HostSourceExplicitFlag is rung 0: an operator named the host. It is
	// also the provenance of every audited rebind, which by construction
	// cannot happen without an explicit target.
	HostSourceExplicitFlag HostSource = "explicit-flag"
	// HostSourceWorkspaceCorrelation is rung 1: a persisted binding — one
	// of these records. Intent, which is why it outranks the environment.
	HostSourceWorkspaceCorrelation HostSource = "workspace-correlation"
	// HostSourceCwdCorrelation is rung 2: a discovered session claims the
	// workspace path as its directory identity. Observed identity — weaker
	// than a recorded intent, stronger than the pane you happen to be
	// standing in, which is why it sits between correlation and ambient.
	HostSourceCwdCorrelation HostSource = "cwd-correlation"
	// HostSourceAmbientEnv is rung 3: an ambient host variable captured at
	// materialization. Accident, not intent (notes/19 §0's precedence
	// footgun is the reason this rung sits below correlation).
	HostSourceAmbientEnv HostSource = "ambient-env"
	// HostSourcePolicyDefault is rung 4: the first enabled kind in the
	// installed host policy, with the instance from host discovery.
	HostSourcePolicyDefault HostSource = "policy-default"
)

// HostSources lists the closed vocabulary in rank order.
var HostSources = []HostSource{
	HostSourceExplicitFlag,
	HostSourceWorkspaceCorrelation,
	HostSourceCwdCorrelation,
	HostSourceAmbientEnv,
	HostSourcePolicyDefault,
}

// Valid reports whether s is one of the five sealed rungs.
func (s HostSource) Valid() bool {
	for _, known := range HostSources {
		if s == known {
			return true
		}
	}
	return false
}

// HostFingerprint is the evidence that identifies one host instance and the
// container the correlation was proven from: notes/19 §5's set — session
// name, pane_id, terminal_id, and process_info.
//
// The field set mirrors what internal/host/herdr's evidenceFor builds for a
// pane, one for one: TerminalID is the epoch-equivalent (evidence.go's
// HostContainerID), PaneID is addressing only, and Process is the birth
// tuple procfsBirth resolves from pane.process_info. Herdr exposes no
// server-scoped epoch at 0.8.2, so SessionName — the value that selects a
// socket — is the only server-scoped identity there is.
//
// Nothing in this package builds one from a host.Evidence: adapters sit
// below the kernel and do not import it, so that bridge belongs to the
// composition layer that owns both.
type HostFingerprint struct {
	// SessionName is the host session the instance belongs to (Herdr's
	// session name, which is what selects a socket).
	SessionName string
	// PaneID is the host container coordinate. It is restored across a
	// host-server restart, so it addresses but never distinguishes an
	// incarnation.
	PaneID string
	// TerminalID is the epoch-equivalent at pane scope: it changes on
	// every new terminal, so a TerminalID change on a stable PaneID proves
	// a new incarnation (notes/19 §5).
	TerminalID string
	// Process is the process-birth tuple from the host's process report,
	// when one was available.
	Process ProcessBirth
}

// Present reports whether the fingerprint carries any identifying evidence
// at all. notes/42 §11 makes fingerprints a hard condition of both writes,
// so an empty one is refused rather than recorded as a binding nobody can
// later check.
func (f HostFingerprint) Present() bool {
	return f.SessionName != "" || f.PaneID != "" || f.TerminalID != "" || f.Process.Present()
}

// HostBinding is one workspace↔host-instance correlation.
type HostBinding struct {
	// Workspace is the correlated workspace.
	Workspace WorkspaceID
	// Kind is the session-host kind ("herdr"), never an instance.
	Kind string
	// Instance is the instance locator: the value that addresses this one
	// host instance. For Herdr that is the API socket path.
	Instance string
	// InstanceID is the Duo integration-instance ID for the same host,
	// when the caller has one (for Herdr, the session name folded into an
	// ID, because Herdr has no server identity of its own). Optional: the
	// locator is what addresses the host, and this is what a fingerprint
	// and a conformance-evidence key are scoped by.
	InstanceID string
	// Source is the rung that established this binding.
	Source HostSource
	// Fingerprint is the notes/19 §5 evidence set for the instance.
	Fingerprint HostFingerprint
}

// Locator renders the binding in the `<kind>:<instance>` form the rebind
// verb's target flag accepts, so what an operator reads back is what they
// would type.
func (b HostBinding) Locator() string {
	return b.Kind + ":" + b.Instance
}

// validate reports whether the binding may be recorded.
func (b HostBinding) validate() error {
	var missing []string
	if b.Kind == "" {
		missing = append(missing, "host kind")
	}
	if b.Instance == "" {
		missing = append(missing, "instance locator")
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: missing %s", ErrHostTargetRequired, strings.Join(missing, ", "))
	}
	if !b.Source.Valid() {
		return fmt.Errorf("%w: %q", ErrHostSourceUnknown, b.Source)
	}
	if !b.Fingerprint.Present() {
		return ErrHostFingerprintRequired
	}
	return nil
}

// HostCorrelation is the read-model entry for one workspace: its current
// binding, the binding it replaced when it came from a rebind, and the
// durable fact that recorded it.
//
// The fact ID is part of the model rather than a lookup, because the
// materializer that reads this at the `workspace-correlation` rung has to
// put it in the evidence bundle: the launch record cites the exact fact its
// host deduction rested on.
type HostCorrelation struct {
	// Binding is the current correlation.
	Binding HostBinding
	// Previous is the binding this one replaced, present only when the
	// current binding came from a rebind.
	Previous *HostBinding
	// FactID is the fact that recorded the current binding.
	FactID FactID
	// FactKind is workspace.host_bound or workspace.host_rebound.
	FactKind FactKind
	// At, Actor, Reason, and Evidence are that fact's provenance.
	At       string
	Actor    string
	Reason   string
	Evidence string
}

// clone returns a copy whose Previous pointer is not shared with the index.
func (c *HostCorrelation) clone() HostCorrelation {
	out := *c
	if c.Previous != nil {
		prev := *c.Previous
		out.Previous = &prev
	}
	return out
}

// The refusals the correlation verbs raise.
var (
	// ErrHostTargetRequired reports a binding with no host kind or no
	// instance locator. Both halves address exactly one instance; either
	// alone addresses a set.
	ErrHostTargetRequired = errors.New("domain: a host correlation requires a host kind and an instance locator")
	// ErrHostSourceUnknown reports a provenance outside the sealed
	// host_source vocabulary.
	ErrHostSourceUnknown = errors.New("domain: host correlation provenance is outside the host_source vocabulary")
	// ErrHostFingerprintRequired reports a binding offered with no
	// identifying evidence. notes/42 §11: a binding is recorded with
	// fingerprints or not at all.
	ErrHostFingerprintRequired = errors.New("domain: a host correlation requires fingerprint evidence (session name, pane id, terminal id, or process birth)")
	// ErrHostAlreadyBound reports a first bind against a workspace that
	// already has a correlation. Replacing one is the audited rebind's
	// job, never a second bind's.
	ErrHostAlreadyBound = errors.New("domain: workspace already has a host correlation; changing it requires the audited rebind")
	// ErrHostNotBound reports a rebind against a workspace with no
	// correlation. A rebind records old and new; with no old there is
	// nothing to rebind, and the first bind is a different write.
	ErrHostNotBound = errors.New("domain: workspace has no host correlation to rebind")
	// ErrHostEvidenceRequired reports a rebind that named no evidence.
	// notes/42 §11 requires a rebind to name its evidence, which is also
	// what keeps it from ever running implicitly.
	ErrHostEvidenceRequired = errors.New("domain: an audited host rebind must name its evidence")
)

// HostCorrelation returns the current host correlation for one workspace.
//
// This is the read model the materializer consults at the
// `workspace-correlation` rung, and the read verb prints. It is a replay of
// the two fact kinds and nothing else: the latest fact for a workspace wins,
// which is what makes a rebind take effect and a restart agree with a
// running kernel.
func (a *Authority) HostCorrelation(id WorkspaceID) (HostCorrelation, bool) {
	c, ok := a.hostBindings[id]
	if !ok {
		return HostCorrelation{}, false
	}
	return c.clone(), true
}

// HostCorrelations lists every workspace's current host correlation, in
// workspace-ID order.
func (a *Authority) HostCorrelations() []HostCorrelation {
	out := make([]HostCorrelation, 0, len(a.hostBindings))
	for _, c := range a.hostBindings {
		out = append(out, c.clone())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Binding.Workspace < out[j].Binding.Workspace
	})
	return out
}

// WorkspaceForRoot returns the workspace whose current root path is root.
//
// The root index is the same one enrollment resolves through (§3.3: one
// normalized current root maps to one active workspace), exposed read-only
// so a verb can address a workspace by the directory the operator is
// standing in without re-deriving the mapping.
func (a *Authority) WorkspaceForRoot(root string) (Workspace, bool) {
	clean, err := normalizeRoot(root)
	if err != nil {
		return Workspace{}, false
	}
	id, ok := a.rootIndex[clean]
	if !ok {
		return Workspace{}, false
	}
	w, ok := a.workspaces[id]
	if !ok {
		return Workspace{}, false
	}
	return *w, true
}

// BindHostRequest is BindWorkspaceHost's input.
type BindHostRequest struct {
	// Workspace is the workspace to correlate. It must already exist:
	// nothing here enrolls one, because a bind follows a spawn that
	// already resolved its workspace.
	Workspace WorkspaceID
	// Kind, Instance, and InstanceID name the host instance.
	Kind       string
	Instance   string
	InstanceID string
	// Source is the rung that produced this instance.
	Source HostSource
	// Fingerprint is the notes/19 §5 evidence set.
	Fingerprint HostFingerprint
	// Actor, Reason, and Evidence are the fact's attribution.
	Actor    string
	Reason   string
	Evidence string
}

// RebindHostRequest is RebindWorkspaceHost's input. It carries no Source:
// an audited rebind always requires an explicit target, so its provenance is
// always HostSourceExplicitFlag and is not a caller's to choose.
type RebindHostRequest struct {
	// Workspace is the workspace whose correlation changes.
	Workspace WorkspaceID
	// Kind, Instance, and InstanceID name the new host instance.
	Kind       string
	Instance   string
	InstanceID string
	// Fingerprint is the new instance's evidence set.
	Fingerprint HostFingerprint
	// Actor and Reason are the fact's attribution. Evidence is required:
	// a rebind names what it rests on.
	Actor    string
	Reason   string
	Evidence string
}

// BindWorkspaceHost records the first workspace↔host-instance correlation as
// one workspace.host_bound fact.
//
// It is the domain half of the cold-start bind: the application layer calls
// it after a spawn succeeded in a workspace that had no correlation, and the
// hybrid confirmation rule (notes/43 item 13 — an `ambient-env` bind asks,
// an `explicit-flag` or `policy-default` bind writes silently with loud
// output) is that caller's, not this verb's. The kernel's job is that the
// write is durable, attributable, and refused when it would replace an
// existing correlation.
func (a *Authority) BindWorkspaceHost(ctx context.Context, req BindHostRequest) (HostCorrelation, error) {
	if _, ok := a.workspaces[req.Workspace]; !ok {
		return HostCorrelation{}, fmt.Errorf("%w: workspace %s", ErrUnknownObject, req.Workspace)
	}
	if held, ok := a.hostBindings[req.Workspace]; ok {
		return HostCorrelation{}, fmt.Errorf("%w: workspace %s is bound to %s (fact %s)",
			ErrHostAlreadyBound, req.Workspace, held.Binding.Locator(), held.FactID)
	}
	binding := HostBinding{
		Workspace:   req.Workspace,
		Kind:        req.Kind,
		Instance:    req.Instance,
		InstanceID:  req.InstanceID,
		Source:      req.Source,
		Fingerprint: req.Fingerprint,
	}
	if err := binding.validate(); err != nil {
		return HostCorrelation{}, err
	}

	reason := req.Reason
	if reason == "" {
		reason = "first host correlation for this workspace"
	}
	b := a.change(actorOr(req.Actor))
	b.fact(FactWorkspaceHostBound, Fact{
		WorkspaceID: req.Workspace,
		HostBinding: &binding,
		Reason:      reason,
		Evidence:    req.Evidence,
		Detail:      binding.Locator(),
	})
	b.auditEntry(AuditEntry{
		Target: string(req.Workspace),
		Reason: reason,
		Detail: "host bound to " + binding.Locator() + " (" + string(binding.Source) + ")",
	})
	if err := a.commitChange(ctx, b); err != nil {
		return HostCorrelation{}, err
	}
	current, _ := a.HostCorrelation(req.Workspace)
	return current, nil
}

// RebindWorkspaceHost changes an existing correlation as one
// workspace.host_rebound fact carrying both instances with their
// fingerprints.
//
// It never runs implicitly: the caller supplies an explicit target and names
// its evidence, and neither has a default (notes/42 §11). Rebinding to the
// same locator is allowed on purpose — a host-server restart keeps the
// socket and changes the terminal_id, and re-attesting the binding with the
// new fingerprint is exactly what the operator means then.
func (a *Authority) RebindWorkspaceHost(ctx context.Context, req RebindHostRequest) (HostCorrelation, error) {
	if _, ok := a.workspaces[req.Workspace]; !ok {
		return HostCorrelation{}, fmt.Errorf("%w: workspace %s", ErrUnknownObject, req.Workspace)
	}
	held, ok := a.hostBindings[req.Workspace]
	if !ok {
		return HostCorrelation{}, fmt.Errorf("%w: workspace %s", ErrHostNotBound, req.Workspace)
	}
	if strings.TrimSpace(req.Evidence) == "" {
		return HostCorrelation{}, ErrHostEvidenceRequired
	}
	next := HostBinding{
		Workspace:   req.Workspace,
		Kind:        req.Kind,
		Instance:    req.Instance,
		InstanceID:  req.InstanceID,
		Source:      HostSourceExplicitFlag,
		Fingerprint: req.Fingerprint,
	}
	if err := next.validate(); err != nil {
		return HostCorrelation{}, err
	}
	previous := held.Binding

	reason := req.Reason
	if reason == "" {
		reason = "audited host rebind"
	}
	b := a.change(actorOr(req.Actor))
	b.fact(FactWorkspaceHostRebound, Fact{
		WorkspaceID:         req.Workspace,
		HostBinding:         &next,
		PreviousHostBinding: &previous,
		Reason:              reason,
		Evidence:            req.Evidence,
		Detail:              previous.Locator() + " -> " + next.Locator(),
	})
	b.auditEntry(AuditEntry{
		Target: string(req.Workspace),
		Reason: reason,
		Detail: "host rebound from " + previous.Locator() + " to " + next.Locator(),
	})
	if err := a.commitChange(ctx, b); err != nil {
		return HostCorrelation{}, err
	}
	current, _ := a.HostCorrelation(req.Workspace)
	return current, nil
}

// applyHostCorrelation folds one correlation fact into the read model. Both
// kinds replace the current entry: the latest fact for a workspace wins, so
// replay and a running kernel land on the same binding.
func (a *Authority) applyHostCorrelation(f Fact) {
	if f.HostBinding == nil {
		return
	}
	binding := *f.HostBinding
	if binding.Workspace == "" {
		binding.Workspace = f.WorkspaceID
	}
	entry := &HostCorrelation{
		Binding:  binding,
		FactID:   f.ID,
		FactKind: f.Kind,
		At:       f.At,
		Actor:    f.Actor,
		Reason:   f.Reason,
		Evidence: f.Evidence,
	}
	if f.PreviousHostBinding != nil {
		prev := *f.PreviousHostBinding
		entry.Previous = &prev
	}
	a.hostBindings[binding.Workspace] = entry
}

// commitChange builds and commits one assembled change through the identity
// boundary. A host correlation is a correlation, which §4.2 places on the
// enrollment boundary alongside every other identity write.
func (a *Authority) commitChange(ctx context.Context, b *changeBuilder) error {
	c, err := b.build()
	if err != nil {
		return err
	}
	return a.commit(ctx, a.repo.CommitIdentity, c)
}

// actorOr defaults an unattributed write to the authority itself, matching
// Launch's own default.
func actorOr(actor string) string {
	if actor == "" {
		return "authority"
	}
	return actor
}
