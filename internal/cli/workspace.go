// workspace.go is the composition root for the `duo workspace` verb family:
// internal/registry's workspace.host.show and workspace.host.rebind
// operations. Both read or write the workspace↔host-instance correlation the
// domain kernel keeps (internal/domain/hostcorrelation.go) — the state
// `duo.config/v3` stopped authoring in configuration.
//
// Neither verb launches anything, deduces anything, or touches a socket. The
// M1 deduction ladder (explicit flag > correlation > ambient env > policy
// default) reads this correlation at its second rung, and the first bind is
// written by the launch path through domain.BindWorkspaceHost after a spawn
// succeeds. Nothing in this file participates in either.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// workspaceCommand builds the `duo workspace` parent verb and its host
// correlation subcommands. Result format comes from the chassis's one
// global --output flag (internal/cliflags), read via context like every
// other global flag.
func workspaceCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workspace",
		Short: "inspect and change duo workspace state",
	}

	host := &cobra.Command{
		Use:   "host",
		Short: "the workspace's session-host correlation: which host instance new spawns go to",
	}
	host.AddCommand(workspaceHostShowCommand(streams))
	host.AddCommand(workspaceHostRebindCommand(streams))

	cmd.AddCommand(host)
	return cmd
}

// --- result shapes ------------------------------------------------------
//
// The field names follow duo.external/v1's launch_deduced_host shape (kind,
// instance_id, instance_label, host_source) rather than the Go struct's, so
// the binding an operator reads here and the deduced host a launch record
// prints are the same words for the same thing.

type workspaceHostFingerprint struct {
	SessionName       string `json:"session_name,omitempty"`
	PaneID            string `json:"pane_id,omitempty"`
	TerminalID        string `json:"terminal_id,omitempty"`
	ProcessHost       string `json:"process_host,omitempty"`
	ProcessPID        int    `json:"process_pid,omitempty"`
	ProcessStartedAt  string `json:"process_started_at,omitempty"`
	ProcessExecutable string `json:"process_executable,omitempty"`
}

type workspaceHostView struct {
	Kind          string                   `json:"kind"`
	InstanceLabel string                   `json:"instance_label"`
	InstanceID    string                   `json:"instance_id,omitempty"`
	HostSource    string                   `json:"host_source"`
	Fingerprint   workspaceHostFingerprint `json:"fingerprint"`
}

// workspaceHostProvenance is the "when, by whom, on what" half of a
// binding: the durable fact that recorded it. The fact ID is the value the
// evidence bundle of a later launch cites.
type workspaceHostProvenance struct {
	FactID   string `json:"fact_id"`
	FactKind string `json:"fact_kind"`
	At       string `json:"at"`
	Actor    string `json:"actor,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Evidence string `json:"evidence,omitempty"`
}

// workspaceHostShowResult is workspace.host.show's result. Bound is explicit
// rather than implied by a null host, so "this workspace has no correlation"
// is a stated answer and not an absence a reader has to interpret.
type workspaceHostShowResult struct {
	WorkspaceRoot string                   `json:"workspace_root"`
	WorkspaceID   string                   `json:"workspace_id,omitempty"`
	Bound         bool                     `json:"bound"`
	Host          *workspaceHostView       `json:"host,omitempty"`
	Provenance    *workspaceHostProvenance `json:"provenance,omitempty"`
	Previous      *workspaceHostView       `json:"previous_host,omitempty"`
	Detail        string                   `json:"detail,omitempty"`
}

// workspaceHostRebindResult is workspace.host.rebind's result: both
// instances, so the response carries the same "old and new" the fact does.
type workspaceHostRebindResult struct {
	WorkspaceRoot string                   `json:"workspace_root"`
	WorkspaceID   string                   `json:"workspace_id"`
	Previous      *workspaceHostView       `json:"previous_host"`
	Host          *workspaceHostView       `json:"host"`
	Provenance    *workspaceHostProvenance `json:"provenance"`
}

func hostView(b domain.HostBinding) *workspaceHostView {
	return &workspaceHostView{
		Kind:          b.Kind,
		InstanceLabel: b.Instance,
		InstanceID:    b.InstanceID,
		HostSource:    string(b.Source),
		Fingerprint: workspaceHostFingerprint{
			SessionName:       b.Fingerprint.SessionName,
			PaneID:            b.Fingerprint.PaneID,
			TerminalID:        b.Fingerprint.TerminalID,
			ProcessHost:       b.Fingerprint.Process.Host,
			ProcessPID:        b.Fingerprint.Process.PID,
			ProcessStartedAt:  b.Fingerprint.Process.StartedAt,
			ProcessExecutable: b.Fingerprint.Process.Executable,
		},
	}
}

func hostProvenance(c domain.HostCorrelation) *workspaceHostProvenance {
	return &workspaceHostProvenance{
		FactID:   string(c.FactID),
		FactKind: string(c.FactKind),
		At:       c.At,
		Actor:    c.Actor,
		Reason:   c.Reason,
		Evidence: c.Evidence,
	}
}

// --- duo workspace host show --------------------------------------------

// workspaceHostShowCommand constructs `duo workspace host show`:
// internal/registry's "workspace.host.show" operation, CLI path
// {"workspace", "host", "show"}. It never writes — it opens the authority
// store read-only, so running it against an installation that has never
// written anything creates nothing.
func workspaceHostShowCommand(streams *iostreams.Streams) *cobra.Command {
	var workspace string

	cmd := &cobra.Command{
		Use:   "show",
		Short: "print the workspace's current session-host correlation, its provenance, and its fingerprints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output
			root, err := workspaceRoot(workspace)
			if err != nil {
				return err
			}

			a, closer, err := openReadAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()

			result := workspaceHostShowResult{WorkspaceRoot: root}
			switch ws, ok := a.WorkspaceForRoot(root); {
			case !ok:
				// No workspace, therefore no correlation. That is an
				// unbound answer, not a missing object: duo enrolls a
				// workspace when it first launches or enrolls into one.
				result.Detail = "duo has no workspace for this root path yet"
			default:
				result.WorkspaceID = string(ws.ID)
				if c, bound := a.HostCorrelation(ws.ID); bound {
					result.Bound = true
					result.Host = hostView(c.Binding)
					result.Provenance = hostProvenance(c)
					if c.Previous != nil {
						result.Previous = hostView(*c.Previous)
					}
				} else {
					result.Detail = "no host correlation; the next launch in this workspace deduces one and binds it"
				}
			}

			if mode == "json" {
				b, err := json.Marshal(newEnvelope("workspace.host.show", result))
				if err != nil {
					return duoerr.New("internal.workspace_host_show_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			return renderWorkspaceHostShow(streams, result)
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace root path (defaults to the current directory)")

	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func renderWorkspaceHostShow(streams *iostreams.Streams, r workspaceHostShowResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "workspace root:  %s\n", r.WorkspaceRoot)
	fmt.Fprintf(&b, "workspace:       %s\n", orDash(r.WorkspaceID))
	if !r.Bound {
		b.WriteString("host:            none\n")
		if r.Detail != "" {
			fmt.Fprintf(&b, "                 (%s)\n", r.Detail)
		}
		_, err := fmt.Fprint(streams.Out, b.String())
		return err
	}

	fmt.Fprintf(&b, "host:            %s:%s\n", r.Host.Kind, r.Host.InstanceLabel)
	fmt.Fprintf(&b, "host instance:   %s\n", orDash(r.Host.InstanceID))
	fmt.Fprintf(&b, "host source:     %s\n", r.Host.HostSource)
	writeFingerprintLines(&b, r.Host.Fingerprint)
	fmt.Fprintf(&b, "bound by:        %s (%s)\n", r.Provenance.FactKind, r.Provenance.FactID)
	fmt.Fprintf(&b, "bound at:        %s\n", r.Provenance.At)
	fmt.Fprintf(&b, "actor:           %s\n", orDash(r.Provenance.Actor))
	fmt.Fprintf(&b, "reason:          %s\n", orDash(r.Provenance.Reason))
	fmt.Fprintf(&b, "evidence:        %s\n", orDash(r.Provenance.Evidence))
	if r.Previous != nil {
		fmt.Fprintf(&b, "replaced:        %s:%s\n", r.Previous.Kind, r.Previous.InstanceLabel)
	}
	_, err := fmt.Fprint(streams.Out, b.String())
	return err
}

func writeFingerprintLines(b *strings.Builder, f workspaceHostFingerprint) {
	fmt.Fprintf(b, "session name:    %s\n", orDash(f.SessionName))
	fmt.Fprintf(b, "pane id:         %s\n", orDash(f.PaneID))
	fmt.Fprintf(b, "terminal id:     %s\n", orDash(f.TerminalID))
	process := "-"
	if f.ProcessPID > 0 {
		process = "pid=" + strconv.Itoa(f.ProcessPID)
		if f.ProcessStartedAt != "" {
			process += " started=" + f.ProcessStartedAt
		}
	}
	fmt.Fprintf(b, "process birth:   %s\n", process)
}

func orDash(v string) string {
	if v == "" {
		return "-"
	}
	return v
}

// --- duo workspace host rebind ------------------------------------------

// workspaceHostRebindCommand constructs `duo workspace host rebind`:
// internal/registry's "workspace.host.rebind" operation, CLI path
// {"workspace", "host", "rebind"}.
//
// notes/42 §11 fixes three properties, and the flag set is how each becomes
// mechanical: the verb records old and new instance with fingerprints (the
// old comes from the read model, the new from --host plus the fingerprint
// flags), it names its evidence (--evidence is required), and it never runs
// implicitly (--host is required and has no deduced default; nothing here
// falls back to the environment, to a launch, or to discovery).
func workspaceHostRebindCommand(streams *iostreams.Streams) *cobra.Command {
	var (
		workspace  string
		target     string
		instanceID string
		evidence   string
		reason     string
		actor      string

		sessionName       string
		paneID            string
		terminalID        string
		processHost       string
		processPID        int
		processStartedAt  string
		processExecutable string
	)

	cmd := &cobra.Command{
		Use:   "rebind",
		Short: "change the workspace's session-host correlation, recording old and new instance with fingerprints",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output
			root, err := workspaceRoot(workspace)
			if err != nil {
				return err
			}
			kind, instance, err := parseHostTarget(target)
			if err != nil {
				return err
			}

			a, s, err := openWriteAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = s.Close() }()

			ws, ok := a.WorkspaceForRoot(root)
			if !ok {
				return duoerr.New("object.not_found",
					fmt.Sprintf("duo has no workspace for %q, so there is no host correlation to rebind.", root))
			}

			req := domain.RebindHostRequest{
				Workspace:  ws.ID,
				Kind:       kind,
				Instance:   instance,
				InstanceID: instanceID,
				Fingerprint: domain.HostFingerprint{
					SessionName: sessionName,
					PaneID:      paneID,
					TerminalID:  terminalID,
					Process: domain.ProcessBirth{
						Host:       processHost,
						PID:        processPID,
						StartedAt:  processStartedAt,
						Executable: processExecutable,
					},
				},
				Actor:    actor,
				Reason:   reason,
				Evidence: evidence,
			}
			c, err := a.RebindWorkspaceHost(cmd.Context(), req)
			if err != nil {
				return duoerrFromHostCorrelation(err)
			}

			result := workspaceHostRebindResult{
				WorkspaceRoot: root,
				WorkspaceID:   string(ws.ID),
				Host:          hostView(c.Binding),
				Provenance:    hostProvenance(c),
			}
			if c.Previous != nil {
				result.Previous = hostView(*c.Previous)
			}

			if mode == "json" {
				b, err := json.Marshal(newEnvelope("workspace.host.rebind", result))
				if err != nil {
					return duoerr.New("internal.workspace_host_rebind_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			return renderWorkspaceHostRebind(streams, result)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace root path (defaults to the current directory)")
	cmd.Flags().StringVar(&target, "host", "", `the new host instance as "<kind>:<instance>", e.g. herdr:/run/user/1000/herdr/dev.sock (required)`)
	cmd.Flags().StringVar(&instanceID, "host-instance-id", "", "the duo integration-instance id for the new host, when one is known")
	cmd.Flags().StringVar(&evidence, "evidence", "", "what this rebind rests on, recorded on the fact (required)")
	cmd.Flags().StringVar(&reason, "reason", "", "why the correlation changed, in one phrase")
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on the fact")
	cmd.Flags().StringVar(&sessionName, "session-name", "", "fingerprint: the host session name")
	cmd.Flags().StringVar(&paneID, "pane-id", "", "fingerprint: the host container/pane id")
	cmd.Flags().StringVar(&terminalID, "terminal-id", "", "fingerprint: the pane's terminal id, the epoch-equivalent")
	cmd.Flags().StringVar(&processHost, "process-host", "", "fingerprint: process-birth host/boot scope, when known")
	cmd.Flags().IntVar(&processPID, "process-pid", 0, "fingerprint: process-birth pid, when known")
	cmd.Flags().StringVar(&processStartedAt, "process-started-at", "", "fingerprint: process-birth start time, when known")
	cmd.Flags().StringVar(&processExecutable, "process-executable", "", "fingerprint: process-birth resolved executable, when known")
	_ = cmd.MarkFlagRequired("host")
	_ = cmd.MarkFlagRequired("evidence")

	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func renderWorkspaceHostRebind(streams *iostreams.Streams, r workspaceHostRebindResult) error {
	var b strings.Builder
	fmt.Fprintf(&b, "workspace:       %s (%s)\n", r.WorkspaceID, r.WorkspaceRoot)
	fmt.Fprintf(&b, "was:             %s:%s\n", r.Previous.Kind, r.Previous.InstanceLabel)
	fmt.Fprintf(&b, "now:             %s:%s\n", r.Host.Kind, r.Host.InstanceLabel)
	fmt.Fprintf(&b, "host source:     %s\n", r.Host.HostSource)
	writeFingerprintLines(&b, r.Host.Fingerprint)
	fmt.Fprintf(&b, "recorded as:     %s (%s)\n", r.Provenance.FactKind, r.Provenance.FactID)
	fmt.Fprintf(&b, "evidence:        %s\n", orDash(r.Provenance.Evidence))
	_, err := fmt.Fprint(streams.Out, b.String())
	return err
}

// --- shared helpers -----------------------------------------------------

// workspaceRoot resolves the workspace root a verb acts on: the --workspace
// flag, else the current directory. It is `duo session launch`'s rule
// (`--workspace` > cwd), kept identical on purpose — a workspace addressed
// one way by launch and another way by these verbs would be two rules for
// one question.
func workspaceRoot(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", duoerr.New("internal.cwd_unresolved", fmt.Sprintf("resolving the working directory: %v", err))
	}
	return wd, nil
}

// parseHostTarget splits the rebind verb's `--host <kind>:<instance>` target.
//
// The syntax is the same one step 11's launch-time `--host` override uses,
// and Locator() renders a binding back into it, so a value read out of
// `show` can be typed straight into `rebind`. The split is on the *first*
// colon: a Herdr instance is a socket path, which contains slashes rather
// than colons, and a future host whose locator carries one keeps it intact.
//
// A bare `--host <kind>` is refused rather than treated as "any instance of
// that kind": a rebind needs an explicit target, and resolving a kind to an
// instance would be deduction, which this verb does not do.
func parseHostTarget(target string) (kind, instance string, err error) {
	kind, instance, found := strings.Cut(strings.TrimSpace(target), ":")
	switch {
	case !found || strings.TrimSpace(instance) == "":
		return "", "", duoerr.New("invalid.request",
			fmt.Sprintf("--host must name one instance as \"<kind>:<instance>\", not %q. "+
				"A rebind needs an explicit target; it never resolves a kind to an instance.", target))
	case strings.TrimSpace(kind) == "":
		return "", "", duoerr.New("invalid.request",
			fmt.Sprintf("--host %q names no host kind; the form is \"<kind>:<instance>\".", target))
	}
	return strings.TrimSpace(kind), strings.TrimSpace(instance), nil
}

// duoerrFromHostCorrelation maps a correlation refusal onto the chassis's
// structured error. It sits here rather than in duoerrFromDomain because
// every code below is specific to this verb pair; the session family's map
// answers a different set of questions.
func duoerrFromHostCorrelation(err error) *duoerr.Error {
	switch {
	case errors.Is(err, domain.ErrUnknownObject):
		return duoerr.New("object.not_found", err.Error())
	case errors.Is(err, domain.ErrHostTargetRequired),
		errors.Is(err, domain.ErrHostSourceUnknown),
		errors.Is(err, domain.ErrHostFingerprintRequired),
		errors.Is(err, domain.ErrHostEvidenceRequired):
		return duoerr.New("invalid.request", err.Error())
	case errors.Is(err, domain.ErrHostNotBound):
		return duoerr.New("invalid.precondition", err.Error())
	case errors.Is(err, domain.ErrHostAlreadyBound):
		return duoerr.New("refusal.host_already_bound", err.Error())
	default:
		return duoerr.New("internal.workspace_host_command_failed", err.Error())
	}
}
