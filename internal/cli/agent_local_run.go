package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// run owns one direct child only while this CLI stays in the foreground. It
// does not reattach to an orphan or infer death from a broken owner socket.
func agentLocalRun(streams *iostreams.Streams) *cobra.Command {
	var root, program, python, workspace, resume, actor string
	var routes []string
	cmd := &cobra.Command{
		Use: "run <owner-session-id>", Args: cobra.ExactArgs(1),
		Short: "foreground owner and one hostless Duo instance (private development path)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !filepath.IsAbs(root) || !filepath.IsAbs(program) || !filepath.IsAbs(python) {
				return duoerr.New("invalid.request", "--state-dir, --program and --python must be absolute")
			}
			if workspace == "" {
				var err error
				workspace, err = os.Getwd()
				if err != nil {
					return duoerr.New("invalid.request", "workspace directory unavailable")
				}
			}
			argv := []string{"-I", "-B", program, "--state-dir", root}
			for _, route := range routes {
				argv = append(argv, "--opencode-route", route)
			}
			child := exec.Command(python, argv...)
			child.Env = []string{"PYTHONDONTWRITEBYTECODE=1"}
			child.Stderr = streams.Err
			if err := child.Start(); err != nil {
				return duoerr.New("operation.temporarily_unavailable", fmt.Sprintf("starting the private agent: %v", err))
			}
			wait := make(chan error, 1)
			go func() { wait <- child.Wait() }()
			waited := false
			var instance domain.InstanceID
			defer func() {
				if waited {
					return
				}
				_ = child.Process.Signal(syscall.SIGTERM)
				select {
				case <-wait:
				case <-time.After(3 * time.Second):
					_ = child.Process.Kill()
					<-wait
				}
				if instance != "" && child.ProcessState != nil {
					ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
					defer cancel()
					_ = recordLocalChildExit(ctx, instance, actor, child.Process.Pid, child.ProcessState)
				}
			}()

			var owner localOwner
			ready := time.NewTimer(3 * time.Second)
			defer ready.Stop()
			for {
				peer, err := localSocketPeer(filepath.Join(root, "agent.sock"))
				if err == nil && peer != child.Process.Pid {
					return duoerr.New("operation.temporarily_unavailable", "private owner socket is not served by the direct child")
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), 300*time.Millisecond)
				var found localOwner
				if err == nil {
					found, err = openLocalOwner(ctx, root)
				}
				cancel()
				if err == nil {
					owner = found
					break
				}
				select {
				case err := <-wait:
					waited = true
					return duoerr.New("operation.temporarily_unavailable", fmt.Sprintf("private agent exited before readiness: %v", err))
				case <-ready.C:
					return duoerr.New("operation.temporarily_unavailable", "private agent did not become ready")
				case <-cmd.Context().Done():
					return cmd.Context().Err()
				case <-time.After(20 * time.Millisecond):
				}
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			var inspected struct {
				OwnerID       string `json:"owner_id"`
				SessionID     string `json:"session_id"`
				IncarnationID string `json:"incarnation_id"`
			}
			err := localCall(ctx, owner.Client, "session.inspect", map[string]any{
				"target": map[string]string{"owner_id": owner.ID, "session_id": args[0]},
			}, "session", &inspected)
			cancel()
			if err != nil || inspected.OwnerID != owner.ID || inspected.SessionID != args[0] || inspected.IncarnationID == "" {
				return duoerr.New("operation.temporarily_unavailable", "private owner session is not available in this child incarnation")
			}
			var session domain.SessionID
			instance, session, err = associateLocalChild(cmd.Context(), owner.ID, args[0], resume, workspace, actor)
			if err != nil {
				return err
			}
			if err := writeLocalResult(streams, map[string]string{
				"duo_session_id": string(session), "instance_id": string(instance), "owner_session_id": args[0],
			}); err != nil {
				return err
			}

			signals := make(chan os.Signal, 1)
			signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(signals)
			var waitErr error
			select {
			case waitErr = <-wait:
			case sig := <-signals:
				_ = child.Process.Signal(sig)
				select {
				case waitErr = <-wait:
				case <-time.After(3 * time.Second):
					_ = child.Process.Kill()
					waitErr = <-wait
				}
			case <-cmd.Context().Done():
				_ = child.Process.Signal(syscall.SIGTERM)
				select {
				case waitErr = <-wait:
				case <-time.After(3 * time.Second):
					_ = child.Process.Kill()
					waitErr = <-wait
				}
			}
			waited = true
			if child.ProcessState == nil {
				return duoerr.New("operation.temporarily_unavailable", "direct child wait did not provide exit evidence")
			}
			exitCtx, stop := context.WithTimeout(context.Background(), 3*time.Second)
			defer stop()
			if err := recordLocalChildExit(exitCtx, instance, actor, child.Process.Pid, child.ProcessState); err != nil {
				return err
			}
			if waitErr != nil {
				return duoerr.New("operation.temporarily_unavailable", fmt.Sprintf("private agent exited: %v", waitErr))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "state-dir", "", "absolute private owner state directory")
	cmd.Flags().StringVar(&program, "program", "", "absolute path to the separate first-party agent source")
	cmd.Flags().StringVar(&python, "python", "/usr/bin/python3", "absolute Python executable for the private agent")
	cmd.Flags().StringVar(&workspace, "workspace", "", "Duo workspace root (defaults to current directory)")
	cmd.Flags().StringVar(&resume, "resume", "", "explicit inactive Duo session to resume with a new instance")
	cmd.Flags().StringVar(&actor, "actor", "cli", "operator making the explicit association")
	cmd.Flags().StringArrayVar(&routes, "opencode-route", nil, "pass a named OpenCode route to the child (repeatable)")
	_ = cmd.MarkFlagRequired("state-dir")
	_ = cmd.MarkFlagRequired("program")
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func localSocketPeer(path string) (int, error) {
	conn, err := net.DialTimeout("unix", path, 100*time.Millisecond)
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	raw, err := conn.(*net.UnixConn).SyscallConn()
	if err != nil {
		return 0, err
	}
	var peer *syscall.Ucred
	if err := raw.Control(func(fd uintptr) {
		peer, err = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if err != nil {
		return 0, err
	}
	if peer.Uid != uint32(os.Getuid()) {
		return 0, errors.New("private owner socket is owned by another UID")
	}
	return int(peer.Pid), nil
}

func associateLocalChild(ctx context.Context, ownerID, ownerSessionID, resume, workspace, actor string) (domain.InstanceID, domain.SessionID, error) {
	a, store, err := openWriteAuthority(ctx)
	if err != nil {
		return "", "", err
	}
	defer func() { _ = store.Close() }()
	scope := "agent.harness/v0@" + ownerID
	var session domain.SessionID
	var instance domain.InstanceID
	if resume == "" {
		launched, err := a.Launch(ctx, domain.LaunchRequest{RootPath: workspace, Actor: actor, Reason: "foreground private agent child"})
		if err != nil {
			return "", "", duoerrFromDomain(err)
		}
		session, instance = launched.Session, launched.Instance
	} else {
		prior, ok := a.Session(domain.SessionID(resume))
		if !ok || prior.State != domain.SessionInactive || len(a.Attachments(prior.ID)) != 0 {
			return "", "", duoerr.New("operation.temporarily_unavailable", "resume requires an inactive hostless Duo session")
		}
		matched := false
		for _, c := range a.Correlations(domain.TargetInstance, string(prior.Current)) {
			if c.Status == domain.CorrelationRetired && c.ExternalKind == "agent.session" &&
				c.Scope == scope && c.ExternalValue == ownerSessionID {
				matched = true
			}
		}
		if !matched {
			return "", "", duoerr.New("operation.temporarily_unavailable", "resume does not match the retired private owner binding")
		}
		instance, err = a.Resume(ctx, domain.ResumeRequest{
			Session: prior.ID, Actor: actor, Attestation: domain.Attestation{Source: domain.SourceOwner, Subject: actor},
			Reason: "explicit foreground private owner relaunch",
		})
		if err != nil {
			return "", "", duoerrFromDomain(err)
		}
		session = prior.ID
	}
	if err := a.Bind(ctx, domain.BindRequest{
		Session: session, Instance: instance, Actor: actor,
		Attestation:  domain.Attestation{Source: domain.SourceOwner, Subject: actor},
		AgentSession: domain.AgentSessionRef{IntegrationInstance: scope, SessionID: ownerSessionID},
		Reason:       "authenticated foreground private owner child",
	}); err != nil {
		return instance, session, duoerrFromDomain(err)
	}
	if err := a.MarkLive(ctx, instance, actor, "authenticated foreground private owner child"); err != nil {
		return instance, session, duoerrFromDomain(err)
	}
	return instance, session, nil
}

func recordLocalChildExit(ctx context.Context, id domain.InstanceID, actor string, pid int, state *os.ProcessState) error {
	if state == nil {
		return duoerr.New("operation.temporarily_unavailable", "direct child wait has no process state")
	}
	a, store, err := openWriteAuthority(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	evidence := fmt.Sprintf("foreground direct child pid=%d wait=%s", pid, state.String())
	if status, ok := state.Sys().(syscall.WaitStatus); ok {
		evidence = fmt.Sprintf("foreground direct child pid=%d raw_wait=%d", pid, status)
	}
	if err := a.Exit(ctx, id, actor, evidence); err != nil {
		if errors.Is(err, domain.ErrInstanceExited) {
			return duoerr.New("operation.temporarily_unavailable", "private agent instance was already exited by another authority")
		}
		return duoerrFromDomain(err)
	}
	return nil
}
