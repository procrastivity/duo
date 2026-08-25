package cli

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// sessionLifecycleResult is session.archive's and session.remove's shared
// result shape: the session id plus the lifecycle enum the verb left it in.
type sessionLifecycleResult struct {
	SessionID string `json:"session_id"`
	Lifecycle string `json:"lifecycle"`
}

// sessionArchiveCommand constructs `duo session archive`: internal/registry's
// "session.archive" operation, CLI path {"session", "archive"}.
func sessionArchiveCommand(streams *iostreams.Streams) *cobra.Command {
	var actor, reason string

	cmd := &cobra.Command{
		Use:   "archive <session-id>",
		Short: "move an inactive session out of ordinary active use",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output

			a, s, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			id := domain.SessionID(args[0])
			if err := a.Archive(cmd.Context(), id, actor, reason); err != nil {
				return duoerrFromDomain(err)
			}
			return renderLifecycleResult(streams, mode, "session.archive", sessionLifecycleResult{
				SessionID: string(id), Lifecycle: lifecycle(domain.SessionArchived),
			})
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on every fact")
	cmd.Flags().StringVar(&reason, "reason", "", "a free-form reason recorded on the audit row")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// sessionRemoveCommand constructs `duo session remove`: internal/registry's
// "session.remove" operation, CLI path {"session", "remove"}.
func sessionRemoveCommand(streams *iostreams.Streams) *cobra.Command {
	var actor, reason string

	cmd := &cobra.Command{
		Use:   "remove <session-id>",
		Short: "retire an archived session while keeping its tombstone",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output

			a, s, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			id := domain.SessionID(args[0])
			if err := a.Remove(cmd.Context(), id, actor, reason); err != nil {
				return duoerrFromDomain(err)
			}
			return renderLifecycleResult(streams, mode, "session.remove", sessionLifecycleResult{
				SessionID: string(id), Lifecycle: lifecycle(domain.SessionRemoved),
			})
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on every fact")
	cmd.Flags().StringVar(&reason, "reason", "", "a free-form reason recorded on the audit row")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func renderLifecycleResult(streams *iostreams.Streams, mode, operation string, result sessionLifecycleResult) error {
	if mode == "json" {
		b, err := json.Marshal(newEnvelope(operation, result))
		if err != nil {
			return duoerr.New("internal.session_lifecycle_encode_failed", err.Error())
		}
		_, err = fmt.Fprintln(streams.Out, string(b))
		return err
	}
	_, err := fmt.Fprintf(streams.Out, "session %s: %s\n", result.SessionID, result.Lifecycle)
	return err
}
