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

// sessionAttachmentResult is detach's and reattach's shared result shape:
// the session and the host-attachment state the verb left it in.
type sessionAttachmentResult struct {
	SessionID       string `json:"session_id"`
	AttachmentState string `json:"attachment_state"`
}

// sessionDetachCommand constructs `duo session detach`: internal/registry's
// "session.detach" operation, CLI path {"session", "detach"}.
func sessionDetachCommand(streams *iostreams.Streams) *cobra.Command {
	var actor, reason string

	cmd := &cobra.Command{
		Use:   "detach <session-id>",
		Short: "disable duo's active host attachment while the external runtime continues",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output

			a, s, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			id := domain.SessionID(args[0])
			if err := a.Detach(cmd.Context(), id, actor, reason); err != nil {
				return duoerrFromDomain(err)
			}
			return renderAttachmentResult(streams, mode, "session.detach", sessionAttachmentResult{
				SessionID: string(id), AttachmentState: string(domain.Detached),
			})
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on every fact")
	cmd.Flags().StringVar(&reason, "reason", "", "a free-form reason recorded on the audit row")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// sessionReattachCommand constructs `duo session reattach`:
// internal/registry's "session.reattach" operation, CLI path {"session",
// "reattach"}. Reattach revalidates a detached host: the caller supplies
// the fingerprint it observes now, which must be the same live-runtime
// claim the session already holds (decision-01 §5.2) — including a session
// still showing the "recovering" view after an authority restart, since
// the same revalidation is what proves its continuity.
func sessionReattachCommand(streams *iostreams.Streams) *cobra.Command {
	var (
		actor                 string
		integrationInstance   string
		epochKind, epochScope string
		epochValue            string
		container             string
		processHost           string
		processPID            int
		processStartedAt      string
		processExecutable     string
	)

	cmd := &cobra.Command{
		Use:   "reattach <session-id>",
		Short: "revalidate a detached (or recovering) host and restore observation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output
			scope, err := parseEpochScope(epochScope)
			if err != nil {
				return err
			}
			fp := domain.Fingerprint{
				IntegrationInstance: integrationInstance,
				Epoch:               domain.HostEpoch{Kind: epochKind, Value: epochValue, Scope: scope},
				Container:           container,
			}
			if processPID > 0 || processStartedAt != "" {
				fp.Process = domain.ProcessBirth{
					Host: processHost, PID: processPID, StartedAt: processStartedAt, Executable: processExecutable,
				}
			}

			a, s, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			id := domain.SessionID(args[0])
			if err := a.Reattach(cmd.Context(), id, actor, fp); err != nil {
				return duoerrFromDomain(err)
			}
			return renderAttachmentResult(streams, mode, "session.reattach", sessionAttachmentResult{
				SessionID: string(id), AttachmentState: string(domain.Attached),
			})
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on every fact")
	cmd.Flags().StringVar(&integrationInstance, "integration-instance", "", "the session-host integration instance (required)")
	cmd.Flags().StringVar(&epochKind, "epoch-kind", "", "the host epoch-equivalent's kind, e.g. herdr.terminal_id (required)")
	cmd.Flags().StringVar(&epochValue, "epoch-value", "", "the host epoch-equivalent's value (required)")
	cmd.Flags().StringVar(&epochScope, "epoch-scope", "", `the epoch's proven reach: "server" or "pane" (required)`)
	cmd.Flags().StringVar(&container, "container", "", "the exact host container, e.g. a pane id (required)")
	cmd.Flags().StringVar(&processHost, "process-host", "", "process-birth host/boot scope, when known")
	cmd.Flags().IntVar(&processPID, "process-pid", 0, "process-birth pid, when known")
	cmd.Flags().StringVar(&processStartedAt, "process-started-at", "", "process-birth start time, when known")
	cmd.Flags().StringVar(&processExecutable, "process-executable", "", "process-birth resolved executable, when known")
	_ = cmd.MarkFlagRequired("integration-instance")
	_ = cmd.MarkFlagRequired("epoch-kind")
	_ = cmd.MarkFlagRequired("epoch-value")
	_ = cmd.MarkFlagRequired("epoch-scope")
	_ = cmd.MarkFlagRequired("container")

	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func renderAttachmentResult(streams *iostreams.Streams, mode, operation string, result sessionAttachmentResult) error {
	if mode == "json" {
		b, err := json.Marshal(newEnvelope(operation, result))
		if err != nil {
			return duoerr.New("internal.session_attachment_encode_failed", err.Error())
		}
		_, err = fmt.Fprintln(streams.Out, string(b))
		return err
	}
	_, err := fmt.Fprintf(streams.Out, "session %s: attachment %s\n", result.SessionID, result.AttachmentState)
	return err
}
