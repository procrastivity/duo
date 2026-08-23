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
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	At       string `json:"at"`
	Actor    string `json:"actor"`
	Reason   string `json:"reason,omitempty"`
	Evidence string `json:"evidence,omitempty"`

	Workspace   *wireWorkspace   `json:"workspace,omitempty"`
	Session     *wireSession     `json:"session,omitempty"`
	Instance    *wireInstance    `json:"instance,omitempty"`
	AgentActor  *wireActor       `json:"agent_actor,omitempty"`
	Attachment  *wireAttachment  `json:"attachment,omitempty"`
	Correlation *wireCorrelation `json:"correlation,omitempty"`
	Claim       *wireClaim       `json:"claim,omitempty"`

	WorkspaceID   string `json:"workspace_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	InstanceID    string `json:"instance_id,omitempty"`
	AttachmentID  string `json:"attachment_id,omitempty"`
	ActorID       string `json:"actor_id,omitempty"`
	CorrelationID string `json:"correlation_id,omitempty"`
	State         string `json:"state,omitempty"`
	Detail        string `json:"detail,omitempty"`
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
	State               string `json:"state"`
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
			EpochScope: string(v.Epoch.Scope),
			Container:  v.Container, State: string(v.State),
		}
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
		}
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
	return f
}
