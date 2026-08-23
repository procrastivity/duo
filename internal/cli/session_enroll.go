package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// sessionEnrollResult is session.enroll's result.
type sessionEnrollResult struct {
	SessionID    string `json:"session_id"`
	WorkspaceID  string `json:"workspace_id"`
	InstanceID   string `json:"runtime_instance_id"`
	AttachmentID string `json:"attachment_id"`
	// Repeat marks that this fingerprint was already enrolled: the
	// existing session was returned, nothing new was created
	// (decision-01 §4.2 step 3).
	Repeat bool `json:"repeat,omitempty"`
	// Credential is the instance-scoped reporter enrollment credential.
	// decision-01 §3.6: it is returned exactly once, at creation, and
	// never stored in the clear; a repeat enrollment carries none.
	Credential string `json:"credential,omitempty"`
}

// sessionEnrollCommand constructs `duo session enroll`: internal/registry's
// "session.enroll" operation, CLI path {"session", "enroll"}.
//
// This is the operator-driven enrollment path: the caller supplies the
// live-runtime fingerprint directly, as an explicit owner/administrator
// action (domain.SourceOwner). A host-adapter-driven "discover, then
// enroll the candidate the adapter found" path is future work — Step 21's
// dependencies (14, 15) do not include a session-host adapter (17/18/19),
// and nothing in this codebase yet bridges host.Evidence into
// domain.Fingerprint (see the step-21 wip findings).
func sessionEnrollCommand(streams *iostreams.Streams) *cobra.Command {
	var (
		rootPath              string
		owner, actor          string
		bindActor             string
		integrationInstance   string
		epochKind, epochScope string
		epochValue            string
		container             string
		processHost           string
		processPID            int
		processStartedAt      string
		processExecutable     string
		agentIntegration      string
		agentSessionID        string
		transcript            string
	)

	cmd := &cobra.Command{
		Use:   "enroll",
		Short: "adopt an already-running external runtime as a duo session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}

			root := rootPath
			if root == "" {
				wd, err := os.Getwd()
				if err != nil {
					return duoerr.New("internal.cwd_unresolved", fmt.Sprintf("resolving the working directory: %v", err))
				}
				root = wd
			}

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

			cand := domain.Candidate{Fingerprint: fp, RootPath: root}
			if agentIntegration != "" || agentSessionID != "" {
				cand.AgentSession = domain.AgentSessionRef{IntegrationInstance: agentIntegration, SessionID: agentSessionID}
				cand.Transcript = transcript
			}

			req := domain.EnrollRequest{
				Candidate:   cand,
				Actor:       actor,
				Owner:       owner,
				Attestation: domain.Attestation{Source: domain.SourceOwner, Subject: actor},
			}
			if bindActor != "" {
				req.BindActor = domain.ActorID(bindActor)
			}

			a, s, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			res, err := a.Enroll(cmd.Context(), req)
			if err != nil {
				return duoerrFromDomain(err)
			}
			result := sessionEnrollResult{
				SessionID:    string(res.Session),
				WorkspaceID:  string(res.Workspace),
				InstanceID:   string(res.Instance),
				AttachmentID: string(res.Attachment),
				Repeat:       res.Repeat,
				Credential:   res.Credential,
			}

			if mode == "json" {
				b, err := json.Marshal(newEnvelope("session.enroll", result))
				if err != nil {
					return duoerr.New("internal.session_enroll_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			verb := "enrolled"
			if result.Repeat {
				verb = "already enrolled"
			}
			if _, err := fmt.Fprintf(streams.Out, "session %s: %s (workspace %s, runtime instance %s)\n",
				verb, result.SessionID, result.WorkspaceID, result.InstanceID); err != nil {
				return err
			}
			if result.Credential != "" {
				_, err = fmt.Fprintf(streams.Out, "reporter credential (shown once, not stored): %s\n", result.Credential)
			}
			return err
		},
	}

	cmd.Flags().StringVar(&rootPath, "root-path", "", "workspace root path (defaults to the current directory)")
	cmd.Flags().StringVar(&owner, "owner", "", "the session's owning subject (defaults to --actor)")
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on every fact")
	cmd.Flags().StringVar(&bindActor, "bind-actor", "", "bind a durable agent actor at enrollment")
	cmd.Flags().StringVar(&integrationInstance, "integration-instance", "", "the session-host integration instance (required)")
	cmd.Flags().StringVar(&epochKind, "epoch-kind", "", "the host epoch-equivalent's kind, e.g. herdr.terminal_id (required)")
	cmd.Flags().StringVar(&epochValue, "epoch-value", "", "the host epoch-equivalent's value (required)")
	cmd.Flags().StringVar(&epochScope, "epoch-scope", "", `the epoch's proven reach: "server" or "pane" (required)`)
	cmd.Flags().StringVar(&container, "container", "", "the exact host container, e.g. a pane id (required)")
	cmd.Flags().StringVar(&processHost, "process-host", "", "process-birth host/boot scope, when known")
	cmd.Flags().IntVar(&processPID, "process-pid", 0, "process-birth pid, when known")
	cmd.Flags().StringVar(&processStartedAt, "process-started-at", "", "process-birth start time, when known")
	cmd.Flags().StringVar(&processExecutable, "process-executable", "", "process-birth resolved executable, when known")
	cmd.Flags().StringVar(&agentIntegration, "agent-integration-instance", "", "agent-runtime integration instance, for an attested agent-session correlation")
	cmd.Flags().StringVar(&agentSessionID, "agent-session-id", "", "the agent runtime's own session id")
	cmd.Flags().StringVar(&transcript, "transcript", "", "the agent-runtime transcript path")
	_ = cmd.MarkFlagRequired("integration-instance")
	_ = cmd.MarkFlagRequired("epoch-kind")
	_ = cmd.MarkFlagRequired("epoch-value")
	_ = cmd.MarkFlagRequired("epoch-scope")
	_ = cmd.MarkFlagRequired("container")

	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// parseEpochScope validates a --epoch-scope flag value against the domain's
// two proven epoch reaches.
func parseEpochScope(v string) (domain.EpochScope, error) {
	switch domain.EpochScope(v) {
	case domain.EpochScopeServer, domain.EpochScopePane:
		return domain.EpochScope(v), nil
	default:
		return "", duoerr.New("invalid.request", fmt.Sprintf(`--epoch-scope must be "server" or "pane", not %q.`, v))
	}
}
