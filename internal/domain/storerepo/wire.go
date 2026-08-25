package storerepo

import "github.com/procrastivity/duo/internal/domain"

// The wire types are the durable shape of the domain's facts. They are
// written out explicitly, rather than by tagging the domain structs, for two
// reasons: the kernel is not allowed to own JSON, and a durable log's field
// names should change only when someone edits this file, never as a side
// effect of renaming a Go field.

type wireToken struct {
	Kind       string `json:"kind"`
	Key        string `json:"key"`
	Generation int    `json:"generation"`
	Session    string `json:"session"`
	Instance   string `json:"instance"`
}

type wireFact struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	At          string `json:"at"`
	Actor       string `json:"actor"`
	Incarnation string `json:"incarnation,omitempty"`
	Reason      string `json:"reason,omitempty"`
	Evidence    string `json:"evidence,omitempty"`

	Workspace   *wireWorkspace   `json:"workspace,omitempty"`
	Session     *wireSession     `json:"session,omitempty"`
	Instance    *wireInstance    `json:"instance,omitempty"`
	AgentActor  *wireActor       `json:"agent_actor,omitempty"`
	Attachment  *wireAttachment  `json:"attachment,omitempty"`
	Correlation *wireCorrelation `json:"correlation,omitempty"`
	Claim       *wireClaim       `json:"claim,omitempty"`
	Parked      *wireParked      `json:"parked,omitempty"`
	Provider    *wireProvider    `json:"provider,omitempty"`

	LaunchResolution *wireLaunchResolution `json:"launch_resolution,omitempty"`

	WorkspaceID   string `json:"workspace_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	InstanceID    string `json:"instance_id,omitempty"`
	AttachmentID  string `json:"attachment_id,omitempty"`
	ActorID       string `json:"actor_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	State         string `json:"state,omitempty"`
	Detail        string `json:"detail,omitempty"`

	// Workspace↔host correlation payloads (duo.config/v3, 2026-08-24
	// handoff 22). PreviousHostBinding appears only on a
	// workspace.host_rebound fact, which records old and new instance
	// together.
	HostBinding         *wireHostBinding `json:"host_binding,omitempty"`
	PreviousHostBinding *wireHostBinding `json:"previous_host_binding,omitempty"`
}

// wireHostBinding is one workspace↔host-instance correlation on the wire.
// The fingerprint fields are notes/19 §5's set, spelled the way the host
// surfaces spell them (pane_id, terminal_id) rather than the way the Go
// struct does, because this is the durable format.
type wireHostBinding struct {
	Workspace  string `json:"workspace"`
	Kind       string `json:"kind"`
	Instance   string `json:"instance"`
	InstanceID string `json:"instance_id,omitempty"`
	HostSource string `json:"host_source"`

	SessionName       string `json:"session_name,omitempty"`
	PaneID            string `json:"pane_id,omitempty"`
	TerminalID        string `json:"terminal_id,omitempty"`
	ProcessHost       string `json:"process_host,omitempty"`
	ProcessPID        int    `json:"process_pid,omitempty"`
	ProcessStartedAt  string `json:"process_started_at,omitempty"`
	ProcessExecutable string `json:"process_executable,omitempty"`
}

func hostBindingToWire(b *domain.HostBinding) *wireHostBinding {
	if b == nil {
		return nil
	}
	return &wireHostBinding{
		Workspace:         string(b.Workspace),
		Kind:              b.Kind,
		Instance:          b.Instance,
		InstanceID:        b.InstanceID,
		HostSource:        string(b.Source),
		SessionName:       b.Fingerprint.SessionName,
		PaneID:            b.Fingerprint.PaneID,
		TerminalID:        b.Fingerprint.TerminalID,
		ProcessHost:       b.Fingerprint.Process.Host,
		ProcessPID:        b.Fingerprint.Process.PID,
		ProcessStartedAt:  b.Fingerprint.Process.StartedAt,
		ProcessExecutable: b.Fingerprint.Process.Executable,
	}
}

func hostBindingFromWire(w *wireHostBinding) *domain.HostBinding {
	if w == nil {
		return nil
	}
	return &domain.HostBinding{
		Workspace:  domain.WorkspaceID(w.Workspace),
		Kind:       w.Kind,
		Instance:   w.Instance,
		InstanceID: w.InstanceID,
		Source:     domain.HostSource(w.HostSource),
		Fingerprint: domain.HostFingerprint{
			SessionName: w.SessionName,
			PaneID:      w.PaneID,
			TerminalID:  w.TerminalID,
			Process: domain.ProcessBirth{
				Host:       w.ProcessHost,
				PID:        w.ProcessPID,
				StartedAt:  w.ProcessStartedAt,
				Executable: w.ProcessExecutable,
			},
		},
	}
}

type wireWorkspace struct {
	ID        string `json:"id"`
	RootPath  string `json:"root_path"`
	CreatedAt string `json:"created_at"`
}

type wireSession struct {
	ID          string `json:"id"`
	Workspace   string `json:"workspace"`
	State       string `json:"state"`
	Owner       string `json:"owner"`
	Creator     string `json:"creator"`
	Current     string `json:"current,omitempty"`
	Attachment  string `json:"attachment,omitempty"`
	BoundActor  string `json:"bound_actor,omitempty"`
	Quarantined bool   `json:"quarantined,omitempty"`
	CreatedAt   string `json:"created_at"`
}

type wireInstance struct {
	ID                    string `json:"id"`
	Session               string `json:"session"`
	State                 string `json:"state"`
	CredentialFingerprint string `json:"credential_fingerprint,omitempty"`
	StartedAt             string `json:"started_at"`
	ExitedAt              string `json:"exited_at,omitempty"`
}

type wireActor struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

type wireAttachment struct {
	ID                  string `json:"id"`
	Session             string `json:"session"`
	IntegrationInstance string `json:"integration_instance"`
	EpochKind           string `json:"epoch_kind"`
	EpochValue          string `json:"epoch_value"`
	EpochScope          string `json:"epoch_scope"`
	Container           string `json:"container"`
	ProcessHost         string `json:"process_host,omitempty"`
	ProcessPID          int    `json:"process_pid,omitempty"`
	ProcessStartedAt    string `json:"process_started_at,omitempty"`
	ProcessExecutable   string `json:"process_executable,omitempty"`
	State               string `json:"state"`
	Continuity          string `json:"continuity,omitempty"`
	ContinuityInstance  string `json:"continuity_instance,omitempty"`
}

// wireParked is one report held as unresolved evidence. The agent-runtime
// identity keeps its two halves separate because the retroactive-binding rule
// (decision-02, G-03) matches on the scoped identity, not on a rendering of
// it.
type wireParked struct {
	ID                       string `json:"id"`
	Session                  string `json:"session"`
	Instance                 string `json:"instance,omitempty"`
	Source                   string `json:"source,omitempty"`
	AgentIntegrationInstance string `json:"agent_integration_instance,omitempty"`
	AgentSessionID           string `json:"agent_session_id,omitempty"`
	Transcript               string `json:"transcript,omitempty"`
	Evidence                 string `json:"evidence,omitempty"`
	Reason                   string `json:"reason,omitempty"`
	ReceivedAt               string `json:"received_at"`
}

// wireLaunchResolution is one launch-resolution record on the wire.
//
// Body is a []byte, so encoding/json base64-encodes it: the record's own
// bytes are carried as data rather than spliced into this document as JSON.
// That is what makes the durable body byte-identical to the one the resolver
// produced. Nesting it as raw JSON would re-encode it — key order, number
// formatting, escaping — and the kernel is not allowed to interpret the
// record at all, let alone rewrite it.
type wireLaunchResolution struct {
	ID   string `json:"id"`
	Body []byte `json:"body,omitempty"`
}

type wireCorrelation struct {
	ID              string `json:"id"`
	TargetKind      string `json:"target_kind"`
	TargetID        string `json:"target_id"`
	ExternalKind    string `json:"external_kind"`
	ExternalValue   string `json:"external_value"`
	Scope           string `json:"scope,omitempty"`
	Source          string `json:"source,omitempty"`
	Evidence        string `json:"evidence,omitempty"`
	FirstVerifiedAt string `json:"first_verified_at,omitempty"`
	LastVerifiedAt  string `json:"last_verified_at,omitempty"`
	ValidUntil      string `json:"valid_until,omitempty"`
	Status          string `json:"status"`
}

// wireProvider is one provider.disabled or provider.enabled fact's payload
// on the wire (step 08). Note is carried even though nothing populates it
// yet, so thread 4 (a recorded disable reason / until-schedule) is a value
// change here, not a wire-shape change.
type wireProvider struct {
	Name string `json:"name"`
	Note string `json:"note,omitempty"`
}

type wireClaim struct {
	Kind       string `json:"kind"`
	Key        string `json:"key"`
	Generation int    `json:"generation"`
	Session    string `json:"session"`
	Instance   string `json:"instance"`
	Degraded   bool   `json:"degraded,omitempty"`
	HeldAt     string `json:"held_at,omitempty"`
}

func toWire(f domain.Fact) wireFact {
	w := wireFact{
		ID:            string(f.ID),
		Kind:          string(f.Kind),
		At:            f.At,
		Actor:         f.Actor,
		Incarnation:   f.Incarnation,
		Reason:        f.Reason,
		Evidence:      f.Evidence,
		WorkspaceID:   string(f.WorkspaceID),
		SessionID:     string(f.SessionID),
		InstanceID:    string(f.InstanceID),
		AttachmentID:  string(f.AttachmentID),
		ActorID:       string(f.ActorID),
		CorrelationID: string(f.CorrelationID),
		State:         f.State,
		Detail:        f.Detail,
	}
	if v := f.Workspace; v != nil {
		w.Workspace = &wireWorkspace{ID: string(v.ID), RootPath: v.RootPath, CreatedAt: v.CreatedAt}
	}
	if v := f.Session; v != nil {
		w.Session = &wireSession{
			ID: string(v.ID), Workspace: string(v.Workspace), State: string(v.State),
			Owner: v.Owner, Creator: v.Creator, Current: string(v.Current),
			Attachment: string(v.Attachment), BoundActor: string(v.BoundActor),
			Quarantined: v.Quarantined, CreatedAt: v.CreatedAt,
		}
	}
	if v := f.Instance; v != nil {
		w.Instance = &wireInstance{
			ID: string(v.ID), Session: string(v.Session), State: string(v.State),
			CredentialFingerprint: v.CredentialFingerprint,
			StartedAt:             v.StartedAt, ExitedAt: v.ExitedAt,
		}
	}
	if v := f.AgentActor; v != nil {
		w.AgentActor = &wireActor{ID: string(v.ID), Name: v.Name, CreatedAt: v.CreatedAt}
	}
	if v := f.Attachment; v != nil {
		w.Attachment = &wireAttachment{
			ID: string(v.ID), Session: string(v.Session),
			IntegrationInstance: v.IntegrationInstance,
			EpochKind:           v.Epoch.Kind, EpochValue: v.Epoch.Value,
			EpochScope:         string(v.Epoch.Scope),
			Container:          v.Container,
			ProcessHost:        v.Process.Host,
			ProcessPID:         v.Process.PID,
			ProcessStartedAt:   v.Process.StartedAt,
			ProcessExecutable:  v.Process.Executable,
			State:              string(v.State),
			Continuity:         string(v.Continuity),
			ContinuityInstance: string(v.ContinuityInstance),
		}
	}
	if v := f.Parked; v != nil {
		w.Parked = &wireParked{
			ID: string(v.ID), Session: string(v.Session), Instance: string(v.Instance),
			Source:                   string(v.Source),
			AgentIntegrationInstance: v.AgentSession.IntegrationInstance,
			AgentSessionID:           v.AgentSession.SessionID,
			Transcript:               v.Transcript, Evidence: v.Evidence,
			Reason: v.Reason, ReceivedAt: v.ReceivedAt,
		}
	}
	if v := f.LaunchResolution; v != nil {
		w.LaunchResolution = &wireLaunchResolution{ID: string(v.ID), Body: v.Body}
	}
	if v := f.Provider; v != nil {
		w.Provider = &wireProvider{Name: v.Name, Note: v.Note}
	}
	if v := f.Correlation; v != nil {
		w.Correlation = &wireCorrelation{
			ID: string(v.ID), TargetKind: string(v.TargetKind), TargetID: v.TargetID,
			ExternalKind: v.ExternalKind, ExternalValue: v.ExternalValue,
			Scope: v.Scope, Source: v.Source, Evidence: v.Evidence,
			FirstVerifiedAt: v.FirstVerifiedAt, LastVerifiedAt: v.LastVerifiedAt,
			ValidUntil: v.ValidUntil, Status: string(v.Status),
		}
	}
	w.HostBinding = hostBindingToWire(f.HostBinding)
	w.PreviousHostBinding = hostBindingToWire(f.PreviousHostBinding)
	if v := f.Claim; v != nil {
		w.Claim = &wireClaim{
			Kind: string(v.Ref.Kind), Key: string(v.Ref.Key), Generation: v.Generation,
			Session: string(v.Session), Instance: string(v.Instance),
			Degraded: v.Degraded, HeldAt: v.HeldAt,
		}
	}
	return w
}

func fromWire(w wireFact) domain.Fact {
	f := domain.Fact{
		ID:            domain.FactID(w.ID),
		Kind:          domain.FactKind(w.Kind),
		At:            w.At,
		Actor:         w.Actor,
		Incarnation:   w.Incarnation,
		Reason:        w.Reason,
		Evidence:      w.Evidence,
		WorkspaceID:   domain.WorkspaceID(w.WorkspaceID),
		SessionID:     domain.SessionID(w.SessionID),
		InstanceID:    domain.InstanceID(w.InstanceID),
		AttachmentID:  domain.AttachmentID(w.AttachmentID),
		ActorID:       domain.ActorID(w.ActorID),
		CorrelationID: domain.CorrelationID(w.CorrelationID),
		State:         w.State,
		Detail:        w.Detail,
	}
	if v := w.Workspace; v != nil {
		f.Workspace = &domain.Workspace{
			ID: domain.WorkspaceID(v.ID), RootPath: v.RootPath, CreatedAt: v.CreatedAt,
		}
	}
	if v := w.Session; v != nil {
		f.Session = &domain.Session{
			ID: domain.SessionID(v.ID), Workspace: domain.WorkspaceID(v.Workspace),
			State: domain.SessionState(v.State), Owner: v.Owner, Creator: v.Creator,
			Current: domain.InstanceID(v.Current), Attachment: domain.AttachmentID(v.Attachment),
			BoundActor: domain.ActorID(v.BoundActor), Quarantined: v.Quarantined,
			CreatedAt: v.CreatedAt,
		}
	}
	if v := w.Instance; v != nil {
		f.Instance = &domain.RuntimeInstance{
			ID: domain.InstanceID(v.ID), Session: domain.SessionID(v.Session),
			State: domain.InstanceState(v.State), CredentialFingerprint: v.CredentialFingerprint,
			StartedAt: v.StartedAt, ExitedAt: v.ExitedAt,
		}
	}
	if v := w.AgentActor; v != nil {
		f.AgentActor = &domain.AgentActor{
			ID: domain.ActorID(v.ID), Name: v.Name, CreatedAt: v.CreatedAt,
		}
	}
	if v := w.Attachment; v != nil {
		f.Attachment = &domain.HostAttachment{
			ID: domain.AttachmentID(v.ID), Session: domain.SessionID(v.Session),
			IntegrationInstance: v.IntegrationInstance,
			Epoch: domain.HostEpoch{
				Kind: v.EpochKind, Value: v.EpochValue,
				Scope: domain.EpochScope(v.EpochScope),
			},
			Container: v.Container, State: domain.AttachmentState(v.State),
			Process: domain.ProcessBirth{
				Host:       v.ProcessHost,
				PID:        v.ProcessPID,
				StartedAt:  v.ProcessStartedAt,
				Executable: v.ProcessExecutable,
			},
			Continuity:         domain.ContinuityState(v.Continuity),
			ContinuityInstance: domain.InstanceID(v.ContinuityInstance),
		}
	}
	if v := w.Parked; v != nil {
		f.Parked = &domain.ParkedReport{
			ID: domain.ParkedReportID(v.ID), Session: domain.SessionID(v.Session),
			Instance: domain.InstanceID(v.Instance),
			Source:   domain.BindingSource(v.Source),
			AgentSession: domain.AgentSessionRef{
				IntegrationInstance: v.AgentIntegrationInstance,
				SessionID:           v.AgentSessionID,
			},
			Transcript: v.Transcript, Evidence: v.Evidence,
			Reason: v.Reason, ReceivedAt: v.ReceivedAt,
		}
	}
	if v := w.LaunchResolution; v != nil {
		f.LaunchResolution = &domain.LaunchResolution{
			ID: domain.LaunchResolutionID(v.ID), Body: v.Body,
		}
	}
	if v := w.Provider; v != nil {
		f.Provider = &domain.ProviderFact{Name: v.Name, Note: v.Note}
	}
	if v := w.Correlation; v != nil {
		f.Correlation = &domain.Correlation{
			ID: domain.CorrelationID(v.ID), TargetKind: domain.CorrelationTarget(v.TargetKind),
			TargetID: v.TargetID, ExternalKind: v.ExternalKind, ExternalValue: v.ExternalValue,
			Scope: v.Scope, Source: v.Source, Evidence: v.Evidence,
			FirstVerifiedAt: v.FirstVerifiedAt, LastVerifiedAt: v.LastVerifiedAt,
			ValidUntil: v.ValidUntil, Status: domain.CorrelationStatus(v.Status),
		}
	}
	if v := w.Claim; v != nil {
		f.Claim = &domain.Claim{
			Ref: domain.ClaimRef{
				Kind: domain.ClaimKind(v.Kind),
				Key:  domain.ClaimKey(v.Key),
			},
			Generation: v.Generation,
			Session:    domain.SessionID(v.Session),
			Instance:   domain.InstanceID(v.Instance),
			Degraded:   v.Degraded,
			HeldAt:     v.HeldAt,
		}
	}
	f.HostBinding = hostBindingFromWire(w.HostBinding)
	f.PreviousHostBinding = hostBindingFromWire(w.PreviousHostBinding)
	return f
}
