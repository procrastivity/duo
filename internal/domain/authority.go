package domain

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// timestampLayout matches the store's: fixed width, UTC, millisecond, so
// values sort lexically wherever they are compared.
const timestampLayout = "2006-01-02T15:04:05.000Z"

// Authority is the local Duo authority's identity and lifecycle kernel: the
// issuer of every primary Duo ID and the holder of the active-claim index.
//
// It keeps the whole model in memory and treats the durable fact log as the
// record of truth: every verb builds a Change, commits it through the
// repository, and only then applies the same facts to the index, so the
// index can never describe something the store did not commit. One Authority
// is one writer, matching the store's single writer lease.
type Authority struct {
	repo Repository
	now  func() time.Time

	// incarnation is the authority incarnation this kernel run stamps its
	// facts with. §4.4: a restart "assigns a new authority incarnation ID".
	// The kernel does not mint it — the durable writer does, and the kernel
	// reads it back through the repository seam, so one authority run and
	// one writer lease can never disagree about which run is writing.
	incarnation string

	workspaces   map[WorkspaceID]*Workspace
	rootIndex    map[string]WorkspaceID
	sessions     map[SessionID]*Session
	instances    map[InstanceID]*RuntimeInstance
	actors       map[ActorID]*AgentActor
	attachments  map[AttachmentID]*HostAttachment
	correlations map[CorrelationID]*Correlation

	// claims is the active-claim index: the live entries only.
	claims map[ClaimRef]*Claim
	// nextGen is the generation a claim key must be re-taken at once its
	// previous holder released it. Held claims are absent from it.
	nextGen map[ClaimRef]int
	// instanceClaims lets exit release everything one instance holds.
	instanceClaims map[InstanceID][]ClaimRef
	// actorBinding is the one-live-binding-per-actor index (§3.4).
	actorBinding map[ActorID]SessionID
	// recovering is a startup view, never durable (§5.1).
	recovering map[InstanceID]bool
	// launchResolutions holds every committed launch-resolution record by
	// ID, and sessionLaunch indexes the record that explains one session.
	// Both are a replay of durable facts; nothing mutates a record once it
	// is here.
	launchResolutions map[LaunchResolutionID]*recordedLaunchResolution
	sessionLaunch     map[SessionID]LaunchResolutionID
	// parked holds the unresolved instance reports each session collected
	// while its attachment continuity was unverified. It is a replay of
	// durable facts like everything else here; nothing in it has been
	// applied to identity state.
	parked map[SessionID][]*ParkedReport

	// hostBindings is the workspace↔host-instance correlation read model:
	// the current binding per workspace, replayed from the
	// workspace.host_bound / workspace.host_rebound facts. See
	// hostcorrelation.go.
	hostBindings map[WorkspaceID]*HostCorrelation

	// providers is the "standing provider facts" read model (step 08): the
	// latest provider.disabled or provider.enabled fact per name, keyed by
	// name rather than by a minted Duo ID. A name absent from this map has
	// no standing fact — see ProviderStanding and StandingProviderFacts.
	providers map[string]ProviderStanding
}

// Option configures an Authority.
type Option func(*Authority)

// WithClock replaces the wall clock the kernel stamps facts with.
func WithClock(now func() time.Time) Option {
	return func(a *Authority) { a.now = now }
}

// Open builds the kernel from repo's durable fact log.
//
// This is decision-01 §4.4's reload: durable Duo IDs, active claims,
// lifecycle facts, and correlation history come back, and every
// previously-live runtime instance enters the recovering view until an
// integration proves what happened to it. Nothing here infers exit, and
// nothing reactivates automatically.
func Open(ctx context.Context, repo Repository, opts ...Option) (*Authority, error) {
	a := &Authority{
		repo:           repo,
		now:            time.Now,
		workspaces:     map[WorkspaceID]*Workspace{},
		rootIndex:      map[string]WorkspaceID{},
		sessions:       map[SessionID]*Session{},
		instances:      map[InstanceID]*RuntimeInstance{},
		actors:         map[ActorID]*AgentActor{},
		attachments:    map[AttachmentID]*HostAttachment{},
		correlations:   map[CorrelationID]*Correlation{},
		claims:         map[ClaimRef]*Claim{},
		nextGen:        map[ClaimRef]int{},
		instanceClaims: map[InstanceID][]ClaimRef{},
		actorBinding:   map[ActorID]SessionID{},
		recovering:     map[InstanceID]bool{},
		parked:         map[SessionID][]*ParkedReport{},
		hostBindings:   map[WorkspaceID]*HostCorrelation{},
		providers:      map[string]ProviderStanding{},

		launchResolutions: map[LaunchResolutionID]*recordedLaunchResolution{},
		sessionLaunch:     map[SessionID]LaunchResolutionID{},
	}
	for _, opt := range opts {
		opt(a)
	}
	if r, ok := repo.(IncarnationReporter); ok {
		a.incarnation = r.Incarnation()
	}

	facts, err := repo.Load(ctx)
	if err != nil {
		return nil, err
	}
	for _, f := range facts {
		a.apply(f)
	}
	for id, inst := range a.instances {
		if !inst.State.Terminal() {
			a.recovering[id] = true
		}
	}
	return a, nil
}

// stamp returns the current domain time in the canonical layout.
func (a *Authority) stamp() string { return a.now().UTC().Format(timestampLayout) }

// Incarnation reports the authority incarnation this kernel run records its
// facts under, or "" when the repository reports none.
func (a *Authority) Incarnation() string { return a.incarnation }

// commit runs one boundary and, only on success, folds its facts into the
// index. The order is the whole point: a refused or crashed transaction
// leaves the kernel exactly as it was.
func (a *Authority) commit(ctx context.Context, boundary func(context.Context, Change) error, c Change) error {
	if err := boundary(ctx, c); err != nil {
		return err
	}
	for _, f := range c.Facts {
		a.apply(f)
	}
	return nil
}

// apply folds one fact into the in-memory model. It is the only mutator, and
// it is shared by replay and by post-commit application, so a reloaded
// authority and a running one cannot drift. It stays one switch over the
// fact enumeration on purpose: splitting it by object would hide which facts
// exist, and every one of them has to be handled somewhere.
func (a *Authority) apply(f Fact) {
	switch f.Kind {
	case FactWorkspaceCreated:
		if f.Workspace != nil {
			w := *f.Workspace
			a.workspaces[w.ID] = &w
			a.rootIndex[w.RootPath] = w.ID
		}
	case FactWorkspaceRebound:
		if w, ok := a.workspaces[f.WorkspaceID]; ok {
			delete(a.rootIndex, w.RootPath)
			w.RootPath = f.Detail
			a.rootIndex[w.RootPath] = w.ID
		}
	case FactSessionCreated:
		if f.Session != nil {
			s := *f.Session
			a.sessions[s.ID] = &s
			if s.BoundActor != "" {
				a.actorBinding[s.BoundActor] = s.ID
			}
		}
	case FactSessionState:
		if s, ok := a.sessions[f.SessionID]; ok {
			s.State = SessionState(f.State)
		}
	case FactSessionOwner:
		if s, ok := a.sessions[f.SessionID]; ok {
			s.Owner = f.Detail
		}
	case FactSessionQuarantined:
		if s, ok := a.sessions[f.SessionID]; ok {
			s.Quarantined = f.State == "true"
		}
	case FactAttachmentCreated:
		if f.Attachment != nil {
			at := *f.Attachment
			a.attachments[at.ID] = &at
			if s, ok := a.sessions[at.Session]; ok {
				s.Attachment = at.ID
			}
		}
	case FactAttachmentState:
		if at, ok := a.attachments[f.AttachmentID]; ok {
			at.State = AttachmentState(f.State)
		}
	case FactAttachmentContinuity:
		if at, ok := a.attachments[f.AttachmentID]; ok {
			at.Continuity = ContinuityState(f.State)
			at.ContinuityInstance = f.InstanceID
		}
	case FactLaunchResolved:
		// The record is evidence, not state: it is retained exactly as it
		// was committed and applied to nothing else.
		if f.LaunchResolution != nil {
			a.recordLaunchResolution(*f.LaunchResolution, f.SessionID, f.InstanceID)
		}
	case FactReportParked:
		// A parked report is applied to nothing but the parked list. That
		// is the whole point of the fact: durable, attributable, inert.
		if f.Parked != nil {
			p := *f.Parked
			a.parked[p.Session] = append(a.parked[p.Session], &p)
		}
	case FactProviderDisabled:
		// Provider state (step 08): the latest fact per name wins on
		// replay, and the fact ID it wins with is retained (step 11 snaps
		// provider state by fact ID).
		if f.Provider != nil {
			a.providers[f.Provider.Name] = ProviderStanding{Enabled: false, FactID: f.ID}
		}
	case FactProviderEnabled:
		if f.Provider != nil {
			a.providers[f.Provider.Name] = ProviderStanding{Enabled: true, FactID: f.ID}
		}
	case FactInstanceStarted:
		if f.Instance != nil {
			inst := *f.Instance
			a.instances[inst.ID] = &inst
		}
	case FactInstanceCurrent:
		if s, ok := a.sessions[f.SessionID]; ok {
			s.Current = f.InstanceID
		}
	case FactInstanceState:
		if inst, ok := a.instances[f.InstanceID]; ok {
			inst.State = InstanceState(f.State)
			if inst.State.Terminal() {
				inst.ExitedAt = f.At
				delete(a.recovering, inst.ID)
			}
		}
	case FactRecoveryDecision:
		// An unreachable host resolves nothing: §4.4 rule 4 keeps the
		// instance in the recovering view with its claim reserved rather
		// than inferring exit.
		if f.State != string(RecoveryUnreachable) {
			delete(a.recovering, f.InstanceID)
		}
	case FactActorCreated:
		if f.AgentActor != nil {
			ac := *f.AgentActor
			a.actors[ac.ID] = &ac
		}
	case FactActorBound:
		if s, ok := a.sessions[f.SessionID]; ok {
			s.BoundActor = f.ActorID
		}
		a.actorBinding[f.ActorID] = f.SessionID
	case FactActorUnbound:
		if s, ok := a.sessions[f.SessionID]; ok {
			s.BoundActor = ""
		}
		delete(a.actorBinding, f.ActorID)
	case FactCorrelationClaimed:
		if f.Correlation != nil {
			c := *f.Correlation
			a.correlations[c.ID] = &c
		}
	case FactCorrelationStatus:
		if c, ok := a.correlations[f.CorrelationID]; ok {
			c.Status = CorrelationStatus(f.State)
			if c.Status == CorrelationActive {
				c.LastVerifiedAt = f.At
			}
		}
	case FactClaimHeld:
		if f.Claim != nil {
			c := *f.Claim
			a.claims[c.Ref] = &c
			delete(a.nextGen, c.Ref)
			a.instanceClaims[c.Instance] = append(a.instanceClaims[c.Instance], c.Ref)
		}
	case FactClaimReleased:
		if f.Claim != nil {
			ref := f.Claim.Ref
			if held, ok := a.claims[ref]; ok {
				a.nextGen[ref] = held.Generation + 1
				delete(a.claims, ref)
			}
		}

	// Workspace↔host correlation (duo.config/v3, 2026-08-24 handoff 22).
	// The fold lives in hostcorrelation.go beside the model it builds.
	case FactWorkspaceHostBound, FactWorkspaceHostRebound:
		a.applyHostCorrelation(f)
	}
}

// nextGeneration reports the generation a fresh hold of ref must use.
func (a *Authority) nextGeneration(ref ClaimRef) int { return a.nextGen[ref] }

// --- read surface -----------------------------------------------------
//
// Every accessor returns a copy. The index is the kernel's, and a caller
// that could mutate a returned record could move state the fact log never
// recorded.

// Workspace returns one workspace.
func (a *Authority) Workspace(id WorkspaceID) (Workspace, bool) {
	w, ok := a.workspaces[id]
	if !ok {
		return Workspace{}, false
	}
	return *w, true
}

// Session returns one Duo session.
func (a *Authority) Session(id SessionID) (Session, bool) {
	s, ok := a.sessions[id]
	if !ok {
		return Session{}, false
	}
	return *s, true
}

// Instance returns one runtime instance.
func (a *Authority) Instance(id InstanceID) (RuntimeInstance, bool) {
	inst, ok := a.instances[id]
	if !ok {
		return RuntimeInstance{}, false
	}
	return *inst, true
}

// Attachment returns one host attachment.
func (a *Authority) Attachment(id AttachmentID) (HostAttachment, bool) {
	at, ok := a.attachments[id]
	if !ok {
		return HostAttachment{}, false
	}
	return *at, true
}

// Actor returns one agent actor.
func (a *Authority) Actor(id ActorID) (AgentActor, bool) {
	ac, ok := a.actors[id]
	if !ok {
		return AgentActor{}, false
	}
	return *ac, true
}

// ActiveClaim returns the current holder of one active-claim entry.
func (a *Authority) ActiveClaim(ref ClaimRef) (Claim, bool) {
	c, ok := a.claims[ref]
	if !ok {
		return Claim{}, false
	}
	return *c, true
}

// ActiveClaims reports how many entries the active-claim index holds.
func (a *Authority) ActiveClaims() int { return len(a.claims) }

// Sessions lists every session, in ID order.
func (a *Authority) Sessions() []Session {
	out := make([]Session, 0, len(a.sessions))
	for _, s := range a.sessions {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Correlations lists the correlation records that target one Duo object, in
// ID order. Sequential records for one external value are all returned: §4.1
// keeps a retired record as history rather than replacing it.
func (a *Authority) Correlations(kind CorrelationTarget, id string) []Correlation {
	var out []Correlation
	for _, c := range a.correlations {
		if c.TargetKind == kind && c.TargetID == id {
			out = append(out, *c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Recovering lists the runtime instances still awaiting a recovery decision,
// in ID order.
func (a *Authority) Recovering() []InstanceID {
	out := make([]InstanceID, 0, len(a.recovering))
	for id := range a.recovering {
		out = append(out, id)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// StandingProviderFacts returns the "standing provider facts" read model
// (step 08): every provider name that has at least one recorded
// provider.disabled or provider.enabled fact, mapped to its latest standing
// state. A name with no standing fact is absent from the result — a reader
// that also needs config-declared names (a provider a launch_variant names
// but no fact has ever toggled) merges those in itself and applies the
// default-enabled rule, since the kernel holds no config to consult. The
// returned map is a copy.
func (a *Authority) StandingProviderFacts() map[string]ProviderStanding {
	out := make(map[string]ProviderStanding, len(a.providers))
	for name, st := range a.providers {
		out[name] = st
	}
	return out
}

// View returns the state a presentation shows for one session, deriving
// attached, detached, and recovering rather than storing them.
func (a *Authority) View(id SessionID) (SessionView, bool) {
	s, ok := a.sessions[id]
	if !ok {
		return "", false
	}
	switch s.State {
	case SessionArchived:
		return ViewArchived, true
	case SessionRemoved:
		return ViewRemoved, true
	case SessionInactive:
		return ViewInactive, true
	case SessionActive:
		if s.Current != "" && a.recovering[s.Current] {
			return ViewRecovering, true
		}
		if at, ok := a.attachments[s.Attachment]; ok && at.State == Attached {
			return ViewAttached, true
		}
		return ViewDetached, true
	}
	return "", false
}

// --- change construction ----------------------------------------------

// changeBuilder assembles one atomic Change. It carries the first error it
// hits so a verb can build a whole change and check once, instead of
// checking after every minted ID.
type changeBuilder struct {
	a      *Authority
	at     string
	actor  string
	facts  []Fact
	claims []ClaimToken
	audit  AuditEntry
	err    error
}

// change starts a Change attributed to actor.
func (a *Authority) change(actor string) *changeBuilder {
	return &changeBuilder{a: a, at: a.stamp(), actor: actor}
}

// fact appends one fact, stamping the fields every fact shares.
func (b *changeBuilder) fact(kind FactKind, f Fact) *changeBuilder {
	if b.err != nil {
		return b
	}
	id, err := mintID(factPrefix)
	if err != nil {
		b.err = err
		return b
	}
	f.ID = FactID(id)
	f.Kind = kind
	f.At = b.at
	if f.Actor == "" {
		f.Actor = b.actor
	}
	if f.Incarnation == "" {
		f.Incarnation = b.a.incarnation
	}
	b.facts = append(b.facts, f)
	return b
}

// hold seizes one active-claim entry: the durable token and the fact that
// records it are added together, so replay and the durable index agree.
func (b *changeBuilder) hold(ref ClaimRef, session SessionID, instance InstanceID, degraded bool, reason string) *changeBuilder {
	if b.err != nil {
		return b
	}
	claim := Claim{
		Ref:        ref,
		Generation: b.a.nextGeneration(ref),
		Session:    session,
		Instance:   instance,
		Degraded:   degraded,
		HeldAt:     b.at,
	}
	b.claims = append(b.claims, ClaimToken{
		Ref:        claim.Ref,
		Generation: claim.Generation,
		Session:    claim.Session,
		Instance:   claim.Instance,
	})
	return b.fact(FactClaimHeld, Fact{
		Claim:      &claim,
		SessionID:  session,
		InstanceID: instance,
		Reason:     reason,
	})
}

// release gives up one active-claim entry. §4.2: "An exited instance
// releases the active claim but keeps its historical correlations."
func (b *changeBuilder) release(ref ClaimRef, reason string) *changeBuilder {
	held, ok := b.a.claims[ref]
	if !ok {
		return b
	}
	claim := *held
	return b.fact(FactClaimReleased, Fact{
		Claim:      &claim,
		SessionID:  claim.Session,
		InstanceID: claim.Instance,
		Reason:     reason,
	})
}

// auditEntry sets the change's audit row.
func (b *changeBuilder) auditEntry(a AuditEntry) *changeBuilder {
	if a.Actor == "" {
		a.Actor = b.actor
	}
	b.audit = a
	return b
}

// build returns the assembled Change or the first error hit while building.
func (b *changeBuilder) build() (Change, error) {
	if b.err != nil {
		return Change{}, b.err
	}
	return Change{Facts: b.facts, Claims: b.claims, Audit: b.audit}, nil
}

// mint is a small helper for the verbs, which mint several IDs in a row.
func (b *changeBuilder) mint(prefix string) string {
	if b.err != nil {
		return ""
	}
	id, err := mintID(prefix)
	if err != nil {
		b.err = err
		return ""
	}
	return id
}

// requireSession returns a session or a typed refusal.
func (a *Authority) requireSession(id SessionID) (*Session, error) {
	s, ok := a.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: session %s", ErrUnknownObject, id)
	}
	return s, nil
}

// requireInstance returns a runtime instance or a typed refusal.
func (a *Authority) requireInstance(id InstanceID) (*RuntimeInstance, error) {
	inst, ok := a.instances[id]
	if !ok {
		return nil, fmt.Errorf("%w: runtime instance %s", ErrUnknownObject, id)
	}
	return inst, nil
}

// --- provider state (step 08) ------------------------------------------
//
// A provider is config data — a launch_variant names it — not a durable Duo
// object, so unlike every verb above these two take a bare name rather than
// a minted Duo ID, and never refuse an unrecognized one: the kernel holds no
// config and cannot know whether any launch_variant names the provider. The
// CLI layer decides what to say about that ("no variant names this
// provider"); the kernel only ever records the standing decision.

// DisableProvider records a standing decision that launch resolution must
// not choose the named provider, superseding any earlier provider.disabled
// or provider.enabled fact for the same name. It returns the ID of the fact
// it wrote.
func (a *Authority) DisableProvider(ctx context.Context, name, actor, reason string) (FactID, error) {
	return a.setProviderStanding(ctx, FactProviderDisabled, name, actor, reason)
}

// EnableProvider records a standing decision that the named provider is
// eligible, superseding any earlier provider.disabled or provider.enabled
// fact for the same name. It returns the ID of the fact it wrote — a
// provider with no standing fact at all is already enabled by default (see
// StandingProviderFacts), but this still writes durable evidence of the
// explicit decision.
func (a *Authority) EnableProvider(ctx context.Context, name, actor, reason string) (FactID, error) {
	return a.setProviderStanding(ctx, FactProviderEnabled, name, actor, reason)
}

// setProviderStanding builds and commits one provider.disabled or
// provider.enabled fact through the identity boundary (CommitIdentity):
// like most administrative state changes that touch no session,
// runtime-instance, or launch-resolution boundary specifically, provider
// standing rides the general identity/lifecycle commit path (see
// Repository.CommitIdentity's doc comment).
func (a *Authority) setProviderStanding(ctx context.Context, kind FactKind, name, actor, reason string) (FactID, error) {
	if name == "" {
		return "", ErrProviderNameRequired
	}
	b := a.change(actor)
	b.fact(kind, Fact{
		Provider: &ProviderFact{Name: name},
		Reason:   reason,
	}).auditEntry(AuditEntry{Target: name, Reason: reason})
	change, err := b.build()
	if err != nil {
		return "", err
	}
	if err := a.commit(ctx, a.repo.CommitIdentity, change); err != nil {
		return "", err
	}
	return change.Facts[len(change.Facts)-1].ID, nil
}
