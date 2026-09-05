package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/surface"
)

// conversationListResult is conversation.list's result. Shape follows
// fixtures/duo-external-v1/conversation-page.json minus barrier (streams
// are out of this milestone).
type conversationListResult struct {
	SessionID string                 `json:"session_id"`
	Items     []conversationListItem `json:"items"`
	NextPage  *string                `json:"next_page"`
}

// conversationListItem is one wire conversation record projected from a
// runtime.ConversationTurn.
type conversationListItem struct {
	RecordID          string              `json:"record_id"`
	SessionID         string              `json:"session_id"`
	RuntimeInstanceID string              `json:"runtime_instance_id,omitempty"`
	AuthorRole        string              `json:"author_role"`
	Origin            *conversationOrigin `json:"origin,omitempty"`
	Blocks            []conversationBlock `json:"blocks"`
	Completion        string              `json:"completion,omitempty"`
	ReceivedAt        string              `json:"received_at,omitempty"`
}

type conversationOrigin struct {
	Kind string `json:"kind"`
}

type conversationBlock struct {
	Position string                   `json:"position"`
	Type     string                   `json:"type"`
	Content  conversationBlockContent `json:"content"`
}

type conversationBlockContent struct {
	Text       string          `json:"text,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	Name       string          `json:"name,omitempty"`
	Arguments  json.RawMessage `json:"arguments,omitempty"`
	Status     string          `json:"status,omitempty"`
}

// conversationCommand builds the `duo conversation` parent verb.
func conversationCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "conversation",
		Short: "read a duo session's semantic conversation transcript",
	}
	cmd.AddCommand(conversationListCommand(streams))
	return cmd
}

// conversationListCommand constructs `duo conversation list`: the registry
// operation whose CLI path is conversation/list. Streams stay out: no
// subscribe verb is wired.
func conversationListCommand(streams *iostreams.Streams) *cobra.Command {
	var (
		after string
		limit int
	)
	cmd := &cobra.Command{
		Use:   "list <session-id>",
		Short: "list semantic conversation turns for one duo session",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output

			a, closer, err := openReadAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()

			id := domain.SessionID(args[0])
			s, ok := a.Session(id)
			if !ok {
				return duoerr.New("object.not_found", fmt.Sprintf("No session named %q is known.", args[0]))
			}

			result, err := conversationListFor(cmd.Context(), a, s, after, limit)
			if err != nil {
				return err
			}

			if mode == "json" {
				b, err := json.Marshal(newEnvelope(registeredOpByCLI("conversation", "list"), result))
				if err != nil {
					return duoerr.New("internal.conversation_list_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			return renderConversationListText(streams, result)
		},
	}
	cmd.Flags().StringVar(&after, "after", "", "opaque cursor from a prior conversation.list next_page")
	cmd.Flags().IntVar(&limit, "limit", 0, "maximum number of turns to return (adapter default when zero)")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func conversationListFor(ctx context.Context, a *domain.Authority, s domain.Session, after string, limit int) (conversationListResult, error) {
	bindings, ok := agentBindingsFor(a, s)
	if !ok {
		return conversationListResult{}, duoerr.New("object.not_found",
			fmt.Sprintf("No bound agent transcript is known for session %q.", s.ID))
	}
	rt, err := openAgentRuntime(bindings.IntegrationInstance)
	if err != nil {
		return conversationListResult{}, duoerr.New("operation.temporarily_unavailable",
			fmt.Sprintf("No agent-runtime adapter is available for %q.", bindings.IntegrationInstance))
	}
	provider, ok := rt.(runtime.ConversationProvider)
	if !ok {
		return conversationListResult{}, duoerr.New("operation.temporarily_unavailable",
			fmt.Sprintf("Agent runtime %q does not support conversation.list.", bindings.IntegrationInstance))
	}

	batch, err := provider.ReadConversation(ctx, conversationReadRequest(bindings, after, limit))
	if err != nil {
		return conversationListResult{}, duoerr.New("internal.conversation_list_failed", err.Error())
	}

	runtimeInstanceID := string(s.Current)
	items := make([]conversationListItem, 0, len(batch.Turns))
	for _, turn := range batch.Turns {
		items = append(items, conversationItemFromTurn(string(s.ID), runtimeInstanceID, turn, batch.Complete))
	}

	result := conversationListResult{
		SessionID: string(s.ID),
		Items:     items,
	}
	if batch.NextCursor != "" {
		next := batch.NextCursor
		result.NextPage = &next
	}
	return result, nil
}

func conversationItemFromTurn(sessionID, runtimeInstanceID string, turn runtime.ConversationTurn, batchComplete bool) conversationListItem {
	blockType := turn.Kind
	if blockType == "" {
		blockType = "text"
	}
	content := conversationBlockContent{Text: turn.Text}
	if blockType == "tool_call" {
		content = conversationBlockContent{
			ToolCallID: turn.ToolCallID,
			Name:       turn.ToolName,
			Arguments:  append(json.RawMessage(nil), turn.Arguments...),
		}
	} else if blockType == "tool_result" {
		content.ToolCallID = turn.ToolCallID
		content.Status = turn.Status
	}
	item := conversationListItem{
		RecordID:          turn.ID,
		SessionID:         sessionID,
		RuntimeInstanceID: runtimeInstanceID,
		AuthorRole:        turn.Role,
		Blocks: []conversationBlock{{
			Position: "0",
			Type:     blockType,
			Content:  content,
		}},
	}
	if batchComplete {
		item.Completion = "source_record_complete"
	}
	if !turn.At.IsZero() {
		item.ReceivedAt = turn.At.UTC().Format(time.RFC3339Nano)
	}
	if turn.OriginKind != "" {
		item.Origin = &conversationOrigin{Kind: turn.OriginKind}
	}
	return item
}

func renderConversationListText(streams *iostreams.Streams, r conversationListResult) error {
	if len(r.Items) == 0 {
		_, err := fmt.Fprintf(streams.Out, "no conversation turns for %s\n", r.SessionID)
		return err
	}
	for i, item := range r.Items {
		text := ""
		role := item.AuthorRole
		if len(item.Blocks) > 0 {
			block := item.Blocks[0]
			switch block.Type {
			case "tool_call":
				role = "tool_call"
				text = fmt.Sprintf("%s\t%s\t%s", block.Content.ToolCallID, block.Content.Name, string(block.Content.Arguments))
			case "tool_result":
				role = "tool_result"
				text = fmt.Sprintf("%s\t%s\t%s", block.Content.ToolCallID, block.Content.Status, block.Content.Text)
			default:
				text = block.Content.Text
			}
		}
		if _, err := fmt.Fprintf(streams.Out, "%s\t%s\t%s\n", item.RecordID, role, text); err != nil {
			return err
		}
		if i == len(r.Items)-1 && r.NextPage != nil {
			if _, err := fmt.Fprintf(streams.Out, "next_page: %s\n", *r.NextPage); err != nil {
				return err
			}
		}
	}
	return nil
}
