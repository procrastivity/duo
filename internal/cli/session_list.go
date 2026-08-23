package cli

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// sessionListItem is one row of session.list's result, projecting
// domain.Session plus its derived view (which also carries the
// authority-restart "recovering" state — decision-01 §5.1 makes Recovering
// a derived view, never a durable field) onto the safe wire shape.
type sessionListItem struct {
	SessionID         string `json:"session_id"`
	WorkspaceID       string `json:"workspace_id"`
	Lifecycle         string `json:"lifecycle"`
	View              string `json:"view"`
	RuntimeInstanceID string `json:"runtime_instance_id,omitempty"`
	Quarantined       bool   `json:"quarantined,omitempty"`
}

type sessionListResult struct {
	Items []sessionListItem `json:"items"`
}

// sessionListCommand constructs `duo session list`: internal/registry's
// "session.list" operation, CLI path {"session", "list"}.
func sessionListCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list every duo session this authority knows about",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}

			a, closer, err := openReadAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()

			result := sessionListResult{Items: []sessionListItem{}}
			for _, s := range a.Sessions() {
				result.Items = append(result.Items, listItemFor(a, s))
			}

			if mode == "json" {
				b, err := json.Marshal(newEnvelope("session.list", result))
				if err != nil {
					return duoerr.New("internal.session_list_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			return renderSessionListText(streams, result)
		},
	}
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// listItemFor projects one session plus its derived view into a
// sessionListItem.
func listItemFor(a *domain.Authority, s domain.Session) sessionListItem {
	view, _ := a.View(s.ID)
	item := sessionListItem{
		SessionID:   string(s.ID),
		WorkspaceID: string(s.Workspace),
		Lifecycle:   lifecycle(s.State),
		View:        string(view),
		Quarantined: s.Quarantined,
	}
	if s.Current != "" {
		item.RuntimeInstanceID = string(s.Current)
	}
	return item
}

// renderSessionListText writes session.list's human-mode table.
func renderSessionListText(streams *iostreams.Streams, result sessionListResult) error {
	if len(result.Items) == 0 {
		_, err := fmt.Fprintln(streams.Out, "no duo sessions")
		return err
	}
	w := tabwriter.NewWriter(streams.Out, 0, 2, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "SESSION\tWORKSPACE\tLIFECYCLE\tVIEW\tRUNTIME INSTANCE"); err != nil {
		return err
	}
	for _, it := range result.Items {
		instance := it.RuntimeInstanceID
		if instance == "" {
			instance = "-"
		}
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", it.SessionID, it.WorkspaceID, it.Lifecycle, it.View, instance); err != nil {
			return err
		}
	}
	return w.Flush()
}
