package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/protocolowned/devclient"
	"github.com/procrastivity/duo/internal/surface"
)

// The opt-in CLI is a private development path, not a projection of
// session.launch or prompt.deliver. The owner may run separately, or run in
// the foreground as a direct child; neither path arbitrates its ordinary
// human writer through a public Duo operation.
func agentLocalCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{Use: "agent-local", Short: "opt-in private first-party agent path (development only)"}
	cmd.AddCommand(agentLocalCreate(streams), agentLocalConnect(streams), agentLocalTurn(streams), agentLocalRun(streams))
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

type localOwner struct {
	Client devclient.Client
	ID     string
}

type localNoEffectRefusal struct{ Class string }

func (e *localNoEffectRefusal) Error() string {
	return fmt.Sprintf("private agent refused %s without effect", e.Class)
}

func openLocalOwner(ctx context.Context, root string) (localOwner, error) {
	if !filepath.IsAbs(root) {
		return localOwner{}, duoerr.New("invalid.request", "--state-dir must be absolute")
	}
	for path, mode := range map[string]os.FileMode{root: 0o700, filepath.Join(root, "token"): 0o600} {
		info, err := os.Lstat(path)
		if err != nil || info.Mode().Perm() != mode || info.Mode()&os.ModeSymlink != 0 {
			return localOwner{}, duoerr.New("invalid.request", "first-party agent state directory/token must be private, regular, and owned by this user")
		}
		if (path == root && !info.IsDir()) || (path != root && !info.Mode().IsRegular()) {
			return localOwner{}, duoerr.New("invalid.request", "first-party agent state directory/token have the wrong type")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != uint32(os.Getuid()) {
			return localOwner{}, duoerr.New("invalid.request", "first-party agent state is not owned by this user")
		}
	}
	token, err := os.ReadFile(filepath.Join(root, "token"))
	if err != nil || len(token) != 64 || strings.ContainsAny(string(token), "\r\n") {
		return localOwner{}, duoerr.New("invalid.request", "first-party agent token is unavailable or invalid")
	}
	client := devclient.Client{Socket: filepath.Join(root, "agent.sock"), Token: string(token)}
	var description struct {
		OwnerID  string   `json:"owner_id"`
		Profiles []string `json:"profiles"`
	}
	if err := localCall(ctx, client, "protocol.describe", nil, "describe", &description); err != nil {
		return localOwner{}, err
	}
	if description.OwnerID == "" || len(description.Profiles) != 0 {
		return localOwner{}, duoerr.New("operation.temporarily_unavailable", "private agent owner or claim is unexpected")
	}
	return localOwner{Client: client, ID: description.OwnerID}, nil
}

func localCall(ctx context.Context, client devclient.Client, operation string, fields map[string]any, kind string, dst any) error {
	raw, err := client.Call(ctx, operation, fields)
	if err != nil {
		return duoerr.New("operation.temporarily_unavailable", "private agent reply unavailable; inspect the original key before any retry")
	}
	var doc struct {
		Schema string          `json:"schema"`
		Kind   string          `json:"kind"`
		Value  json.RawMessage `json:"value"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.Schema != "agent.harness/v0" {
		return duoerr.New("operation.temporarily_unavailable", "private agent returned an unexpected document")
	}
	if doc.Kind == "refusal" {
		var refused struct {
			Class  string `json:"class"`
			Effect string `json:"effect"`
		}
		if json.Unmarshal(doc.Value, &refused) != nil || refused.Effect != "no_effect" {
			return duoerr.New("operation.temporarily_unavailable", "private agent refused with unknown effect")
		}
		return &localNoEffectRefusal{Class: refused.Class}
	}
	if doc.Kind != kind || json.Unmarshal(doc.Value, dst) != nil {
		return duoerr.New("operation.temporarily_unavailable", "private agent returned an unexpected result")
	}
	return nil
}

func agentLocalCreate(streams *iostreams.Streams) *cobra.Command {
	var root, label, key, deadline, provider string
	cmd := &cobra.Command{
		Use: "create", Args: cobra.NoArgs,
		Short: "create one owner-scoped first-party session (not a Duo session)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			when, err := time.Parse("2006-01-02T15:04:05Z", deadline)
			if err != nil || when.Format("2006-01-02T15:04:05Z") != deadline {
				return duoerr.New("invalid.request", "--deadline must be exact UTC seconds; reuse it with the same key")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			owner, err := openLocalOwner(ctx, root)
			if err != nil {
				return err
			}
			payload := map[string]string{"label": label}
			if provider != "" {
				payload["provider"] = provider
			}
			write := map[string]any{
				"operation": "session.create", "target": map[string]string{"owner_id": owner.ID},
				"idempotency_key": key, "deadline": deadline,
				"payload": payload, "preconditions": map[string]any{}, "grant": "local",
			}
			var created struct {
				Operation string `json:"operation"`
				State     string `json:"state"`
				Target    struct {
					OwnerID string `json:"owner_id"`
				} `json:"target"`
				Result struct {
					SessionID string `json:"session_id"`
				} `json:"result"`
			}
			if err := localCall(ctx, owner.Client, "session.create", map[string]any{"write": write}, "command", &created); err != nil {
				return err
			}
			if created.Operation != "session.create" || created.State != "completed" ||
				created.Target.OwnerID != owner.ID || created.Result.SessionID == "" {
				return duoerr.New("operation.temporarily_unavailable", "owner did not prove a scoped session creation")
			}
			return writeLocalResult(streams, map[string]string{"owner_session_id": created.Result.SessionID})
		},
	}
	cmd.Flags().StringVar(&root, "state-dir", "", "absolute private state directory of an already-running first-party agent")
	cmd.Flags().StringVar(&label, "label", "", "short owner-session label")
	cmd.Flags().StringVar(&key, "key", "", "stable owner-scoped create key")
	cmd.Flags().StringVar(&deadline, "deadline", "", "UTC YYYY-MM-DDTHH:MM:SSZ deadline; reuse exactly on retry")
	cmd.Flags().StringVar(&provider, "provider", "", "optional registered owner route, selected for this session (no fallback)")
	_ = cmd.MarkFlagRequired("state-dir")
	_ = cmd.MarkFlagRequired("label")
	_ = cmd.MarkFlagRequired("key")
	_ = cmd.MarkFlagRequired("deadline")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func agentLocalConnect(streams *iostreams.Streams) *cobra.Command {
	var root, workspace, actor string
	cmd := &cobra.Command{
		Use: "connect <owner-session-id>", Args: cobra.ExactArgs(1),
		Short: "associate one existing private owner session with a hostless Duo session",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			owner, err := openLocalOwner(ctx, root)
			if err != nil {
				return err
			}
			target := map[string]string{"owner_id": owner.ID, "session_id": args[0]}
			var session struct {
				OwnerID       string `json:"owner_id"`
				SessionID     string `json:"session_id"`
				IncarnationID string `json:"incarnation_id"`
			}
			if err := localCall(ctx, owner.Client, "session.inspect", map[string]any{"target": target}, "session", &session); err != nil {
				return err
			}
			if session.OwnerID != owner.ID || session.SessionID != args[0] || session.IncarnationID == "" {
				return duoerr.New("operation.temporarily_unavailable", "private agent session identity does not match")
			}
			if workspace == "" {
				workspace, err = os.Getwd()
				if err != nil {
					return duoerr.New("invalid.request", "workspace directory unavailable")
				}
			}
			a, store, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			scope := "agent.harness/v0@" + owner.ID
			for _, existing := range a.Sessions() {
				for _, c := range a.Correlations(domain.TargetInstance, string(existing.Current)) {
					if c.Status == domain.CorrelationActive && c.ExternalKind == "agent.session" && c.Scope == scope && c.ExternalValue == args[0] {
						instance, ok := a.Instance(existing.Current)
						if !ok || existing.State != domain.SessionActive || instance.State != domain.InstanceLive {
							return duoerr.New("operation.temporarily_unavailable", "existing Duo association is not live; inspect it before reconnecting")
						}
						return writeLocalResult(streams, map[string]string{"duo_session_id": string(existing.ID), "owner_session_id": args[0]})
					}
				}
			}
			launched, err := a.Launch(cmd.Context(), domain.LaunchRequest{RootPath: workspace, Actor: actor, Reason: "explicit private owner session association"})
			if err != nil {
				return duoerrFromDomain(err)
			}
			if err := a.Bind(cmd.Context(), domain.BindRequest{
				Session: launched.Session, Actor: actor,
				Attestation:  domain.Attestation{Source: domain.SourceOwner, Subject: actor},
				AgentSession: domain.AgentSessionRef{IntegrationInstance: scope, SessionID: args[0]},
				Reason:       "operator connected authenticated private owner session",
			}); err != nil {
				return duoerrFromDomain(err)
			}
			if err := a.MarkLive(cmd.Context(), launched.Instance, actor, "authenticated private owner session response"); err != nil {
				return duoerrFromDomain(err)
			}
			return writeLocalResult(streams, map[string]string{"duo_session_id": string(launched.Session), "owner_session_id": args[0]})
		},
	}
	cmd.Flags().StringVar(&root, "state-dir", "", "absolute private state directory of an already-running first-party agent")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Duo workspace root (defaults to current directory)")
	cmd.Flags().StringVar(&actor, "actor", "cli", "operator making the explicit association")
	_ = cmd.MarkFlagRequired("state-dir")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func writeLocalResult(streams *iostreams.Streams, result any) error {
	encoded, err := json.Marshal(result)
	if err != nil {
		return duoerr.New("internal.agent_local_encode_failed", "private agent result could not be encoded")
	}
	_, err = fmt.Fprintln(streams.Out, string(encoded))
	return err
}

type localConversationRecord struct {
	RecordID string `json:"record_id"`
	TurnID   string `json:"turn_id"`
	Blocks   []struct {
		Type    string `json:"type"`
		Source  string `json:"source"`
		Content string `json:"content"`
	} `json:"blocks"`
}

type localConversationSnapshot struct {
	Items    []localConversationRecord `json:"items"`
	NextPage string                    `json:"next_page"`
}

func agentLocalTurn(streams *iostreams.Streams) *cobra.Command {
	var root, text, key string
	cmd := &cobra.Command{
		Use: "turn <duo-session-id>", Args: cobra.ExactArgs(1),
		Short: "submit one of two bounded private agent turns for a connected Duo session",
		RunE: func(cmd *cobra.Command, args []string) (returnErr error) {
			if text == "" || key == "" {
				return duoerr.New("invalid.request", "--text and --key are required")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 65*time.Second)
			defer cancel()
			owner, err := openLocalOwner(ctx, root)
			if err != nil {
				return err
			}
			a, store, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() {
				if store != nil {
					_ = store.Close()
				}
			}()
			s, ok := a.Session(domain.SessionID(args[0]))
			if !ok || s.Current == "" || s.State != domain.SessionActive {
				return duoerr.New("object.not_found", "Duo session is not active")
			}
			instance, ok := a.Instance(s.Current)
			if !ok || instance.State != domain.InstanceLive {
				return duoerr.New("operation.temporarily_unavailable", "Duo instance is not live")
			}
			var sessionID string
			for _, c := range a.Correlations(domain.TargetInstance, string(s.Current)) {
				if c.Status == domain.CorrelationActive && c.ExternalKind == "agent.session" && c.Scope == "agent.harness/v0@"+owner.ID {
					if sessionID != "" && sessionID != c.ExternalValue {
						return duoerr.New("operation.temporarily_unavailable", "Duo instance has ambiguous private-owner bindings")
					}
					sessionID = c.ExternalValue
				}
			}
			if sessionID == "" || len(a.Attachments(s.ID)) != 0 {
				return duoerr.New("operation.temporarily_unavailable", "Duo session has no exact hostless private-owner binding")
			}
			target := map[string]string{"owner_id": owner.ID, "session_id": sessionID}
			var inspected struct {
				OwnerID       string `json:"owner_id"`
				SessionID     string `json:"session_id"`
				IncarnationID string `json:"incarnation_id"`
				Revision      string `json:"revision"`
				Provider      string `json:"provider"`
			}
			if err := localCall(ctx, owner.Client, "session.inspect", map[string]any{"target": target}, "session", &inspected); err != nil {
				return err
			}
			if inspected.OwnerID != owner.ID || inspected.SessionID != sessionID || inspected.IncarnationID == "" {
				return duoerr.New("operation.temporarily_unavailable", "private agent session identity changed")
			}
			var prior struct {
				CommandID string `json:"command_id"`
				Operation string `json:"operation"`
				State     string `json:"state"`
				Target    struct {
					OwnerID   string `json:"owner_id"`
					SessionID string `json:"session_id"`
				} `json:"target"`
				Result struct {
					TurnID string `json:"turn_id"`
				} `json:"result"`
			}
			lookup := map[string]any{"target": map[string]string{"owner_id": owner.ID}, "key": key}
			raw, err := owner.Client.Call(ctx, "command.inspect", lookup)
			if err != nil {
				return duoerr.New("operation.temporarily_unavailable", "original-key inspection unavailable; turn not retried")
			}
			var existing struct {
				Schema string          `json:"schema"`
				Kind   string          `json:"kind"`
				Value  json.RawMessage `json:"value"`
			}
			if json.Unmarshal(raw, &existing) != nil || existing.Schema != "agent.harness/v0" {
				return duoerr.New("operation.temporarily_unavailable", "original-key inspection malformed; turn not retried")
			}
			switch existing.Kind {
			case "command":
				if json.Unmarshal(existing.Value, &prior) != nil || prior.Operation != "turn.submit" ||
					prior.Target.OwnerID != owner.ID || prior.Target.SessionID != sessionID ||
					prior.CommandID == "" {
					return duoerr.New("operation.temporarily_unavailable", "original command identity is not proved; turn not retried")
				}
				if prior.State != "completed" || prior.Result.TurnID == "" {
					actor := "agent-local@" + owner.ID
					accepted, err := a.AcceptPrompt(cmd.Context(), domain.AcceptPromptRequest{
						Session: s.ID, Instance: s.Current, Actor: actor, IdempotencyKey: key,
						CanonicalDigest: promptCanonicalDigest(text), ExpiresAt: time.Now().Add(5 * time.Minute),
						QueuePolicy: domain.QueueUntilSafe,
					})
					if err != nil {
						return duoerrFromDomain(err)
					}
					if accepted.Command.State == domain.ResponsibilityAttempting {
						attempts := accepted.Command.Attempts
						if len(attempts) == 0 || a.ReconcileAttempt(cmd.Context(), accepted.Command.ID, attempts[len(attempts)-1].ID, actor, false) != nil {
							return duoerr.New("operation.temporarily_unavailable", "Duo could not close the uncertain attempt; inspect its command")
						}
					}
					return duoerr.New("operation.temporarily_unavailable", "original command is not proved completed; turn not retried")
				}
			case "refusal":
				var refusal struct {
					Class  string `json:"class"`
					Effect string `json:"effect"`
				}
				if json.Unmarshal(existing.Value, &refusal) != nil || refusal.Class != "unavailable" || refusal.Effect != "no_effect" {
					return duoerr.New("operation.temporarily_unavailable", "original-key inspection did not prove an absent command")
				}
			default:
				return duoerr.New("operation.temporarily_unavailable", "original-key inspection returned an unexpected document")
			}
			var snap localConversationSnapshot
			if err := localCall(ctx, owner.Client, "conversation.snapshot", map[string]any{"target": target, "page_size": 8}, "snapshot", &snap); err != nil {
				return err
			}
			if snap.NextPage != "" || len(snap.Items)%2 != 0 || len(snap.Items) > 4 {
				return duoerr.New("operation.temporarily_unavailable", "owner conversation exceeds this two-turn path")
			}
			original := snap.Items
			pair := -1
			var ownerWrite map[string]any
			if prior.CommandID == "" {
				if len(original) > 2 {
					return duoerr.New("operation.temporarily_unavailable", "this path accepts at most two owner turns")
				}
				pair = len(original)
				deadline := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second).Format("2006-01-02T15:04:05Z")
				ownerWrite = map[string]any{
					"operation": "turn.submit", "target": map[string]string{
						"owner_id": owner.ID, "session_id": sessionID, "incarnation_id": inspected.IncarnationID,
					}, "idempotency_key": key, "deadline": deadline, "queue_policy": "require_ready",
					"payload":       map[string]any{"blocks": []any{map[string]string{"type": "text", "content": text}}},
					"preconditions": map[string]string{"session_revision": inspected.Revision}, "grant": "local",
				}
			} else {
				for i := 0; i < len(original); i += 2 {
					if original[i].TurnID == prior.Result.TurnID && original[i+1].TurnID == prior.Result.TurnID {
						if pair != -1 {
							return duoerr.New("operation.temporarily_unavailable", "original turn has ambiguous records")
						}
						pair = i
					}
				}
				if pair == -1 {
					return duoerr.New("operation.temporarily_unavailable", "original turn not observed for this key")
				}
			}
			actor := "agent-local@" + owner.ID
			accepted, err := a.AcceptPrompt(cmd.Context(), domain.AcceptPromptRequest{
				Session: s.ID, Instance: s.Current, Actor: actor, IdempotencyKey: key,
				CanonicalDigest: promptCanonicalDigest(text), ExpiresAt: time.Now().Add(5 * time.Minute),
				QueuePolicy: domain.QueueUntilSafe,
			})
			if err != nil {
				return duoerrFromDomain(err)
			}
			duoCommand := accepted.Command
			var attempt domain.AttemptID
			provedNoEffect := false
			defer func() {
				if attempt == "" {
					return
				}
				// An incomplete or ambiguous handoff is never counted as delivered.
				reconcileCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
				defer stop()
				if store == nil {
					var reopenErr error
					a, store, reopenErr = openWriteAuthority(reconcileCtx)
					if reopenErr != nil {
						returnErr = duoerr.New("operation.temporarily_unavailable", "Duo could not reopen its uncertain attempt; inspect its command")
						return
					}
				}
				if err := a.ReconcileAttempt(reconcileCtx, duoCommand.ID, attempt, actor, provedNoEffect); err != nil {
					returnErr = duoerr.New("operation.temporarily_unavailable", "Duo could not close the uncertain attempt; inspect its command")
				}
			}()
			switch duoCommand.State {
			case domain.ResponsibilityQueued:
				attempt, err = a.CreateAttempt(cmd.Context(), duoCommand.ID, actor, domain.PromptPathRuntime)
				if err != nil {
					return duoerrFromDomain(err)
				}
			case domain.ResponsibilityAttempting:
				if len(duoCommand.Attempts) == 0 {
					return duoerr.New("operation.temporarily_unavailable", "Duo attempt has no durable identity")
				}
				attempt = duoCommand.Attempts[len(duoCommand.Attempts)-1].ID
				if prior.CommandID == "" {
					provedNoEffect = true // authenticated original-key absence; caller may explicitly retry
					return duoerr.New("operation.temporarily_unavailable", "prior attempt had no owner command; retry the same key explicitly")
				}
			case domain.ResponsibilityDelivered:
				if prior.CommandID == "" {
					return duoerr.New("operation.temporarily_unavailable", "Duo says delivered but the owner has no original command")
				}
			default:
				return duoerr.New("operation.temporarily_unavailable", "Duo prompt responsibility is terminal without delivery")
			}
			// Do not hold Duo's writer lease during the possibly long model call.
			// A foreground supervisor can then record a direct-child wait even
			// when this attempt's owner reply has not arrived.
			if err := store.Close(); err != nil {
				return duoerr.New("operation.temporarily_unavailable", "Duo writer lease could not be released before the owner turn")
			}
			store = nil
			if ownerWrite != nil {
				if err := localCall(ctx, owner.Client, "turn.submit", map[string]any{"write": ownerWrite}, "command", &prior); err != nil {
					var refused *localNoEffectRefusal
					provedNoEffect = errors.As(err, &refused)
					return err
				}
			}
			if prior.Operation != "turn.submit" || prior.Target.OwnerID != owner.ID ||
				prior.Target.SessionID != sessionID || prior.State != "completed" || prior.CommandID == "" || prior.Result.TurnID == "" {
				return duoerr.New("operation.temporarily_unavailable", "owner has not proved full input delivery; inspect original key")
			}
			var after localConversationSnapshot
			if err := localCall(ctx, owner.Client, "conversation.snapshot", map[string]any{"target": target, "page_size": 8}, "snapshot", &after); err != nil {
				return err
			}
			expected := len(original)
			if pair == len(original) {
				expected += 2
			}
			if after.NextPage != "" || len(after.Items) != expected {
				return duoerr.New("operation.temporarily_unavailable", "owner output not observed for this bounded session")
			}
			for i, old := range original {
				if old.RecordID == "" || old.RecordID != after.Items[i].RecordID || old.TurnID != after.Items[i].TurnID {
					return duoerr.New("operation.temporarily_unavailable", "owner conversation changed during turn")
				}
			}
			input, output := after.Items[pair], after.Items[pair+1]
			worker := inspected.Provider
			if worker == "" {
				worker = "deterministic_worker"
			}
			if input.TurnID != prior.Result.TurnID || output.TurnID != prior.Result.TurnID ||
				len(input.Blocks) != 1 || len(output.Blocks) != 1 ||
				input.RecordID == "" || output.RecordID == "" || input.Blocks[0].Type != "text" ||
				input.Blocks[0].Source != "caller" || input.Blocks[0].Content != text ||
				output.Blocks[0].Type != "text" || output.Blocks[0].Source != worker || output.Blocks[0].Content == "" {
				return duoerr.New("operation.temporarily_unavailable", "owner output not observed for this turn")
			}
			if attempt != "" {
				a, store, err = openWriteAuthority(cmd.Context())
				if err != nil {
					return duoerr.New("operation.temporarily_unavailable", "Duo could not reopen the owner turn result; inspect its command")
				}
				if err := a.CommitDelivered(cmd.Context(), duoCommand.ID, attempt, actor); err != nil {
					return duoerr.New("operation.temporarily_unavailable", "Duo could not commit delivery; inspect its command")
				}
				attempt = ""
			}
			return writeLocalResult(streams, map[string]string{
				"duo_session_id": args[0], "duo_command_id": string(duoCommand.ID), "owner_command_id": prior.CommandID,
				"turn_id": prior.Result.TurnID, "output": output.Blocks[0].Content,
			})
		},
	}
	cmd.Flags().StringVar(&root, "state-dir", "", "absolute private state directory of an already-running first-party agent")
	cmd.Flags().StringVar(&text, "text", "", "one text block for the next bounded owner turn")
	cmd.Flags().StringVar(&key, "key", "", "original owner-scoped command key for inspection, never automatically retried")
	_ = cmd.MarkFlagRequired("state-dir")
	_ = cmd.MarkFlagRequired("text")
	_ = cmd.MarkFlagRequired("key")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}
