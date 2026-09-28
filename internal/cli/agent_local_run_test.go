package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
)

func TestAgentLocalForegroundExitAndResume(t *testing.T) {
	program := os.Getenv("DUO_AGENT_PROGRAM")
	if program == "" {
		t.Skip("requires separate pinned duo-agent checkout")
	}
	t.Setenv("DUO_AGENT_LOCAL", "1")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "owner")
	t.Cleanup(func() {
		if pid, err := localSocketPeer(filepath.Join(root, "agent.sock")); err == nil {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	})
	bootstrap := exec.Command("/usr/bin/python3", "-I", "-B", program, "--state-dir", root)
	bootstrap.Env = []string{"PYTHONDONTWRITEBYTECODE=1"}
	if err := bootstrap.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if bootstrap.ProcessState == nil {
			_ = bootstrap.Process.Signal(syscall.SIGTERM)
			_ = bootstrap.Wait()
		}
	})
	limit := time.Now().Add(3 * time.Second)
	for time.Now().Before(limit) {
		conn, err := net.DialTimeout("unix", filepath.Join(root, "agent.sock"), 50*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second).Format("2006-01-02T15:04:05Z")
	code, raw, stderr := runSession(t, "agent-local", "create", "--state-dir", root,
		"--label", "supervised", "--key", "supervised_create", "--deadline", deadline)
	if code != exitcode.Success {
		t.Fatalf("bootstrap create: %d %q", code, stderr)
	}
	var created struct {
		OwnerSessionID string `json:"owner_session_id"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil || created.OwnerSessionID == "" {
		t.Fatalf("bootstrap create result: %q %v", raw, err)
	}
	if err := bootstrap.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = bootstrap.Wait()
	args := []string{
		"agent-local", "run", created.OwnerSessionID, "--program", program,
		"--python", "/usr/bin/python3", "--state-dir", root, "--workspace", t.TempDir(),
	}
	type outcome struct {
		code   int
		stdout string
		stderr string
	}
	launch := func(argv []string) <-chan outcome {
		done := make(chan outcome, 1)
		go func() {
			code, out, errOut := runSession(t, argv...)
			done <- outcome{code, out, errOut}
		}()
		return done
	}
	awaitLive := func() (domain.Session, domain.RuntimeInstance, int) {
		t.Helper()
		limit := time.Now().Add(3 * time.Second)
		for time.Now().Before(limit) {
			pid, err := localSocketPeer(filepath.Join(root, "agent.sock"))
			if err == nil {
				a, closer, err := openReadAuthority(context.Background())
				if err == nil {
					for _, s := range a.Sessions() {
						if instance, ok := a.Instance(s.Current); ok && s.State == domain.SessionActive && instance.State == domain.InstanceLive {
							_, writer, err := openWriteAuthority(context.Background())
							if err == nil {
								_ = writer.Close()
								_ = closer.Close()
								return s, instance, pid
							}
							break
						}
					}
					_ = closer.Close()
				}
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("supervised owner did not produce a live Duo instance")
		return domain.Session{}, domain.RuntimeInstance{}, 0
	}
	firstDone := launch(args)
	first, initial, firstPID := awaitLive()
	if first.Attachment != "" || firstPID == 0 {
		t.Fatal("foreground instance is not hostless or has no direct child PID")
	}
	if code, _, stderr := runSession(t, args...); code == exitcode.Success ||
		!strings.Contains(stderr, "not served by the direct child") {
		t.Fatalf("competing child adopted the existing owner's socket: %d %q", code, stderr)
	}
	// The foreground command must not hold the authority-writer lease while
	// it waits for the child; another Duo verb may write concurrently.
	_, writer, err := openWriteAuthority(context.Background())
	if err != nil {
		t.Fatalf("supervisor held writer lease while waiting: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	turn := []string{
		"agent-local", "turn", string(first.ID), "--state-dir", root,
		"--text", "Live 13", "--key", "supervised_first",
	}
	if code, out, stderr := runSession(t, turn...); code != exitcode.Success ||
		!strings.Contains(out, "31 eviL") || stderr != "" {
		t.Fatalf("supervised child could not serve a Duo turn: %d %q %q", code, out, stderr)
	}
	if err := syscall.Kill(firstPID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-firstDone:
		if result.code != exitcode.Success || !strings.Contains(result.stdout, string(first.ID)) {
			t.Fatalf("foreground wait did not report its session and child exit: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("foreground command did not wait for its child")
	}
	a, closer, err := openReadAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, ok := a.Session(first.ID)
	old, present := a.Instance(initial.ID)
	if !ok || !present || s.State != domain.SessionInactive || old.State != domain.InstanceExited || old.ExitedAt == "" {
		t.Fatalf("direct child wait was not committed as exit: session=%+v instance=%+v", s, old)
	}
	for _, c := range a.Correlations(domain.TargetInstance, string(old.ID)) {
		if c.ExternalKind == "agent.session" && c.Status != domain.CorrelationRetired {
			t.Fatalf("exited instance retained active owner binding: %+v", c)
		}
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	wrong := append(append([]string(nil), args...), "--resume", string(first.ID)+"-wrong")
	if result := <-launch(wrong); result.code == exitcode.Success {
		t.Fatal("unknown Duo session was resumed")
	}
	secondArgs := append(append([]string(nil), args...), "--resume", string(first.ID))
	secondDone := launch(secondArgs)
	second, replacement, secondPID := awaitLive()
	if second.ID != first.ID || replacement.ID == initial.ID || secondPID == 0 {
		t.Fatalf("explicit resume did not preserve session and replace instance: %+v %+v", second, replacement)
	}
	secondTurn := []string{
		"agent-local", "turn", string(first.ID), "--state-dir", root,
		"--text", "After 27", "--key", "supervised_second",
	}
	if code, out, stderr := runSession(t, secondTurn...); code != exitcode.Success ||
		!strings.Contains(out, "72 retfA") || stderr != "" {
		t.Fatalf("resumed child could not serve the second Duo turn: %d %q %q", code, out, stderr)
	}
	if err := syscall.Kill(secondPID, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-secondDone:
		if result.code != exitcode.Success || !strings.Contains(result.stdout, string(first.ID)) {
			t.Fatalf("replacement child exit was not reported: %+v", result)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("replacement owner did not exit")
	}
	a, closer, err = openReadAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	s, ok = a.Session(first.ID)
	restarted, present := a.Instance(replacement.ID)
	if !ok || !present || len(a.Sessions()) != 1 || s.Current != replacement.ID ||
		s.State != domain.SessionInactive || restarted.State != domain.InstanceExited {
		t.Fatalf("replacement exit did not preserve single Duo session: %+v %+v", s, restarted)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentLocalSupervisorCrashLeavesExitUnknown(t *testing.T) {
	program := os.Getenv("DUO_AGENT_PROGRAM")
	if program == "" {
		t.Skip("requires separate pinned duo-agent checkout")
	}
	t.Setenv("DUO_AGENT_LOCAL", "1")
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	root := filepath.Join(t.TempDir(), "owner")
	bootstrap := exec.Command("/usr/bin/python3", "-I", "-B", program, "--state-dir", root)
	bootstrap.Env = []string{"PYTHONDONTWRITEBYTECODE=1"}
	if err := bootstrap.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if bootstrap.ProcessState == nil {
			_ = bootstrap.Process.Signal(syscall.SIGTERM)
			_ = bootstrap.Wait()
		}
		if pid, err := localSocketPeer(filepath.Join(root, "agent.sock")); err == nil {
			_ = syscall.Kill(pid, syscall.SIGTERM)
		}
	})
	limit := time.Now().Add(3 * time.Second)
	for time.Now().Before(limit) {
		if _, err := localSocketPeer(filepath.Join(root, "agent.sock")); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	deadline := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second).Format("2006-01-02T15:04:05Z")
	code, raw, stderr := runSession(t, "agent-local", "create", "--state-dir", root,
		"--label", "crash", "--key", "crash_create", "--deadline", deadline)
	if code != exitcode.Success {
		t.Fatalf("bootstrap create: %d %q", code, stderr)
	}
	var created struct {
		OwnerSessionID string `json:"owner_session_id"`
	}
	if err := json.Unmarshal([]byte(raw), &created); err != nil || created.OwnerSessionID == "" {
		t.Fatalf("bootstrap create result: %q %v", raw, err)
	}
	if err := bootstrap.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	_ = bootstrap.Wait()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper := exec.Command(executable, "-test.run=^TestAgentLocalRunSubprocess$")
	helperLog, err := os.CreateTemp(t.TempDir(), "supervisor-*.log")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = helperLog.Close() }()
	helper.Stderr = helperLog
	stdout, err := helper.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	helper.Env = append(
		os.Environ(),
		"DUO_AGENT_RUN_HELPER=1", "DUO_AGENT_RUN_ROOT="+root,
		"DUO_AGENT_RUN_SESSION="+created.OwnerSessionID,
		"DUO_AGENT_RUN_WORKSPACE="+t.TempDir(),
	)
	if err := helper.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if helper.ProcessState == nil {
			_ = helper.Process.Kill()
			_ = helper.Wait()
		}
	})
	type readyLine struct {
		line string
		err  error
	}
	ready := make(chan readyLine, 1)
	go func() {
		line, err := bufio.NewReader(stdout).ReadString('\n')
		ready <- readyLine{line, err}
	}()
	var line readyLine
	select {
	case line = <-ready:
	case <-time.After(5 * time.Second):
		line.err = context.DeadlineExceeded
	}
	if line.err != nil {
		_ = helper.Process.Kill()
		_ = helper.Wait()
		log, _ := os.ReadFile(helperLog.Name())
		t.Fatalf("subprocess supervisor did not report readiness: %v %s", line.err, log)
	}
	var launched struct {
		DuoSessionID string `json:"duo_session_id"`
		InstanceID   string `json:"instance_id"`
	}
	if err := json.Unmarshal([]byte(line.line), &launched); err != nil || launched.InstanceID == "" {
		t.Fatalf("subprocess readiness result: %q %v", line.line, err)
	}
	a, closer, err := openReadAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	session, ok := a.Session(domain.SessionID(launched.DuoSessionID))
	instance, present := a.Instance(domain.InstanceID(launched.InstanceID))
	if !ok || !present || session.State != domain.SessionActive || instance.State != domain.InstanceLive {
		t.Fatalf("subprocess readiness did not commit a live Duo instance: %+v %+v", session, instance)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := helper.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = helper.Wait()
	if _, err := localSocketPeer(filepath.Join(root, "agent.sock")); err != nil {
		t.Fatalf("owner child did not survive supervisor crash: %v", err)
	}
	a, closer, err = openReadAuthority(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	after, ok := a.Session(session.ID)
	still, present := a.Instance(instance.ID)
	if !ok || !present || after.State != domain.SessionActive || still.State != domain.InstanceLive || still.ExitedAt != "" {
		t.Fatalf("supervisor crash invented direct-child exit evidence: %+v %+v", after, still)
	}
	if err := closer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAgentLocalRunSubprocess(t *testing.T) {
	if os.Getenv("DUO_AGENT_RUN_HELPER") != "1" {
		t.Skip("subprocess fixture only")
	}
	streams := &iostreams.Streams{Out: os.Stdout, Err: os.Stderr}
	command := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test"})
	command.SetArgs([]string{
		"agent-local", "run", os.Getenv("DUO_AGENT_RUN_SESSION"),
		"--state-dir", os.Getenv("DUO_AGENT_RUN_ROOT"), "--program", os.Getenv("DUO_AGENT_PROGRAM"),
		"--python", "/usr/bin/python3", "--workspace", os.Getenv("DUO_AGENT_RUN_WORKSPACE"),
	})
	code := Execute(command, streams)
	t.Fatalf("subprocess supervisor stopped without a child wait: %d", code)
}
