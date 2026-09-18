package portablelauncher

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const processControlHelperEnvironment = "DUO_PROCESS_CONTROL_HELPER=1"

func TestProcessControlHelperProcess(_ *testing.T) {
	if os.Getenv("DUO_PROCESS_CONTROL_HELPER") != "1" {
		return
	}
	for {
		time.Sleep(time.Hour)
	}
}

func TestLinuxProcessControlAcquireStopVerifyRelease(t *testing.T) {
	child := startProcessControlChild(t)
	control := NewLinuxProcessControl()
	t.Cleanup(func() { _ = control.Close() })
	target := child.identity(t, "terminal-success", "attachment-success")

	if err := control.AcquireExactTarget(context.Background(), target); err != nil {
		t.Fatalf("AcquireExactTarget: %v", err)
	}
	if err := control.VerifyExactTarget(context.Background(), target); err != nil {
		t.Fatalf("VerifyExactTarget: %v", err)
	}
	if err := control.SendSIGSTOP(context.Background(), target); err != nil {
		t.Fatalf("SendSIGSTOP: %v", err)
	}
	if err := control.VerifyStopped(context.Background(), target); err != nil {
		t.Fatalf("VerifyStopped: %v", err)
	}
	if state := testProcessState(t, child.pid); state != 'T' && state != 't' {
		t.Fatalf("child state = %q, want stopped or tracing-stop", state)
	}
	if err := control.Release(target); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if err := control.VerifyExactTarget(context.Background(), target); err == nil || !strings.Contains(err.Error(), "not acquired") {
		t.Fatalf("VerifyExactTarget after Release error = %v, want not acquired", err)
	}
}

func TestLinuxProcessControlSendSIGKILLTerminatesOnlyExactAcquiredChild(t *testing.T) {
	targetChild := startProcessControlChild(t)
	bystander := startProcessControlChild(t)
	control := NewLinuxProcessControl()
	t.Cleanup(func() { _ = control.Close() })
	target := targetChild.identity(t, "terminal-kill", "attachment-kill")

	if err := control.AcquireExactTarget(context.Background(), target); err != nil {
		t.Fatalf("AcquireExactTarget: %v", err)
	}
	ownedFD := control.pidfds[target]
	if err := control.SendSIGKILL(context.Background(), target); err != nil {
		t.Fatalf("SendSIGKILL: %v", err)
	}
	targetChild.waitForExit(t)

	if got := control.pidfds[target]; got != ownedFD {
		t.Fatalf("owned pidfd after SIGKILL = %d, want retained fd %d", got, ownedFD)
	}
	if _, err := unix.FcntlInt(uintptr(ownedFD), unix.F_GETFD, 0); err != nil {
		t.Fatalf("owned pidfd after SIGKILL is not open: %v", err)
	}
	assertProcessControlChildLive(t, bystander)
	if err := control.Release(target); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, err := unix.FcntlInt(uintptr(ownedFD), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
		t.Fatalf("F_GETFD(%d) after Release error = %v, want EBADF", ownedFD, err)
	}
}

func TestLinuxProcessControlWrongBirthFailsAcquireAndLeavesChildRunning(t *testing.T) {
	child := startProcessControlChild(t)
	control := NewLinuxProcessControl()
	t.Cleanup(func() { _ = control.Close() })
	target := child.identity(t, "terminal-wrong-birth", "attachment-wrong-birth")
	started, err := time.Parse(testProcessBirthLayout, target.StartTime)
	if err != nil {
		t.Fatalf("parse independently derived start time: %v", err)
	}
	target.StartTime = started.Add(time.Millisecond).Format(testProcessBirthLayout)
	before := countOpenTestPidfds(t)

	if err := control.AcquireExactTarget(context.Background(), target); err == nil || !strings.Contains(err.Error(), "birth mismatch") {
		t.Fatalf("AcquireExactTarget error = %v, want birth mismatch", err)
	}
	if state := testProcessState(t, child.pid); state == 'T' || state == 't' || state == 'Z' || state == 'X' || state == 'x' {
		t.Fatalf("child state after failed acquire = %q, want running live process", state)
	}
	if got := len(control.pidfds); got != 0 {
		t.Fatalf("owned pidfds after failed acquire = %d, want 0", got)
	}
	if got := countOpenTestPidfds(t); got != before {
		t.Fatalf("pidfd count after failed acquire = %d, want %d", got, before)
	}
}

func TestLinuxProcessControlOperationsWithoutAcquireFail(t *testing.T) {
	child := startProcessControlChild(t)
	control := NewLinuxProcessControl()
	t.Cleanup(func() { _ = control.Close() })
	target := child.identity(t, "terminal-unacquired", "attachment-unacquired")

	operations := []struct {
		name string
		run  func() error
	}{
		{name: "verify", run: func() error { return control.VerifyExactTarget(context.Background(), target) }},
		{name: "stop", run: func() error { return control.SendSIGSTOP(context.Background(), target) }},
		{name: "kill", run: func() error { return control.SendSIGKILL(context.Background(), target) }},
		{name: "verify stopped", run: func() error { return control.VerifyStopped(context.Background(), target) }},
		{name: "release", run: func() error { return control.Release(target) }},
	}
	for _, operation := range operations {
		if err := operation.run(); err == nil || !strings.Contains(err.Error(), "not acquired") {
			t.Errorf("%s error = %v, want not acquired", operation.name, err)
		}
	}
	assertProcessControlChildLive(t, child)
}

func TestLinuxProcessControlDuplicateAcquirePreservesExistingHandle(t *testing.T) {
	child := startProcessControlChild(t)
	control := NewLinuxProcessControl()
	t.Cleanup(func() { _ = control.Close() })
	target := child.identity(t, "terminal-duplicate", "attachment-duplicate")
	before := countOpenTestPidfds(t)

	if err := control.AcquireExactTarget(context.Background(), target); err != nil {
		t.Fatalf("first AcquireExactTarget: %v", err)
	}
	fd := control.pidfds[target]
	if got := countOpenTestPidfds(t); got != before+1 {
		t.Fatalf("pidfd count after first acquire = %d, want %d", got, before+1)
	}
	if err := control.AcquireExactTarget(context.Background(), target); err == nil || !strings.Contains(err.Error(), "already acquired") {
		t.Fatalf("duplicate AcquireExactTarget error = %v, want already acquired", err)
	}
	if got := control.pidfds[target]; got != fd {
		t.Fatalf("pidfd after duplicate acquire = %d, want original %d", got, fd)
	}
	if got := countOpenTestPidfds(t); got != before+1 {
		t.Fatalf("pidfd count after duplicate acquire = %d, want %d", got, before+1)
	}
	if err := control.SendSIGSTOP(context.Background(), target); err != nil {
		t.Fatalf("SendSIGSTOP through preserved handle: %v", err)
	}
	if err := control.VerifyStopped(context.Background(), target); err != nil {
		t.Fatalf("VerifyStopped through preserved handle: %v", err)
	}
	if err := control.Release(target); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if got := countOpenTestPidfds(t); got != before {
		t.Fatalf("pidfd count after release = %d, want %d", got, before)
	}
}

func TestLinuxProcessControlVerifyStoppedHonorsCancellationAndTimeout(t *testing.T) {
	child := startProcessControlChild(t)
	target := child.identity(t, "terminal-bounds", "attachment-bounds")

	t.Run("caller cancellation", func(t *testing.T) {
		control := NewLinuxProcessControl()
		t.Cleanup(func() { _ = control.Close() })
		if err := control.AcquireExactTarget(context.Background(), target); err != nil {
			t.Fatalf("AcquireExactTarget: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := control.VerifyStopped(ctx, target)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("VerifyStopped error = %v, want context canceled", err)
		}
	})

	t.Run("bounded timeout", func(t *testing.T) {
		control := newLinuxProcessControl(45*time.Millisecond, 10*time.Millisecond)
		t.Cleanup(func() { _ = control.Close() })
		if err := control.AcquireExactTarget(context.Background(), target); err != nil {
			t.Fatalf("AcquireExactTarget: %v", err)
		}
		err := control.VerifyStopped(context.Background(), target)
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("VerifyStopped error = %v, want bounded deadline", err)
		}
	})
}

func TestLinuxProcessControlCloseAllIsIdempotent(t *testing.T) {
	first := startProcessControlChild(t)
	second := startProcessControlChild(t)
	control := NewLinuxProcessControl()
	firstTarget := first.identity(t, "terminal-close-first", "attachment-close-first")
	secondTarget := second.identity(t, "terminal-close-second", "attachment-close-second")
	for _, target := range []ProcessIdentity{firstTarget, secondTarget} {
		if err := control.AcquireExactTarget(context.Background(), target); err != nil {
			t.Fatalf("AcquireExactTarget(%d): %v", target.PID, err)
		}
	}
	fds := []int{control.pidfds[firstTarget], control.pidfds[secondTarget]}

	if err := control.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := control.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if got := len(control.pidfds); got != 0 {
		t.Fatalf("owned pidfds after Close = %d, want 0", got)
	}
	for _, fd := range fds {
		if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
			t.Fatalf("F_GETFD(%d) error = %v, want EBADF", fd, err)
		}
	}
	if err := control.AcquireExactTarget(context.Background(), firstTarget); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Fatalf("AcquireExactTarget after Close error = %v, want closed", err)
	}
}

func TestLinuxProcessControlDoesNotReuseStateAcrossFullIdentities(t *testing.T) {
	child := startProcessControlChild(t)
	control := NewLinuxProcessControl()
	t.Cleanup(func() { _ = control.Close() })
	acquired := child.identity(t, "terminal-original", "attachment-original")
	if err := control.AcquireExactTarget(context.Background(), acquired); err != nil {
		t.Fatalf("AcquireExactTarget: %v", err)
	}

	mismatches := []ProcessIdentity{
		{PID: acquired.PID, StartTime: acquired.StartTime, TerminalID: "terminal-other", AttachmentID: acquired.AttachmentID},
		{PID: acquired.PID, StartTime: acquired.StartTime, TerminalID: acquired.TerminalID, AttachmentID: "attachment-other"},
	}
	for _, mismatch := range mismatches {
		if err := control.VerifyExactTarget(context.Background(), mismatch); err == nil || !strings.Contains(err.Error(), "not acquired") {
			t.Errorf("VerifyExactTarget(%+v) error = %v, want not acquired", mismatch, err)
		}
		if err := control.SendSIGSTOP(context.Background(), mismatch); err == nil || !strings.Contains(err.Error(), "not acquired") {
			t.Errorf("SendSIGSTOP(%+v) error = %v, want not acquired", mismatch, err)
		}
		if err := control.SendSIGKILL(context.Background(), mismatch); err == nil || !strings.Contains(err.Error(), "not acquired") {
			t.Errorf("SendSIGKILL(%+v) error = %v, want not acquired", mismatch, err)
		}
	}
	assertProcessControlChildLive(t, child)
	if err := control.VerifyExactTarget(context.Background(), acquired); err != nil {
		t.Fatalf("VerifyExactTarget(original): %v", err)
	}
}

func TestLinuxProcessControlSendSIGKILLValidatesIdentityAndContext(t *testing.T) {
	child := startProcessControlChild(t)
	control := NewLinuxProcessControl()
	t.Cleanup(func() { _ = control.Close() })
	target := child.identity(t, "terminal-kill-validation", "attachment-kill-validation")
	if err := control.AcquireExactTarget(context.Background(), target); err != nil {
		t.Fatalf("AcquireExactTarget: %v", err)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := control.SendSIGKILL(canceled, target); !errors.Is(err, context.Canceled) {
		t.Fatalf("SendSIGKILL with canceled context error = %v, want context canceled", err)
	}
	malformed := target
	malformed.AttachmentID = ""
	if err := control.SendSIGKILL(context.Background(), malformed); err == nil || !strings.Contains(err.Error(), "complete attachment identity") {
		t.Fatalf("SendSIGKILL with incomplete identity error = %v, want identity refusal", err)
	}
	assertProcessControlChildLive(t, child)
}

func TestLinuxProcessControlRejectsMalformedIdentityBeforePidfdOpen(t *testing.T) {
	validTime := time.Unix(1_700_000_000, 0).UTC().Format(testProcessBirthLayout)
	tests := []ProcessIdentity{
		{},
		{PID: -1, StartTime: validTime, TerminalID: "terminal", AttachmentID: "attachment"},
		{PID: 1, StartTime: "not-a-time", TerminalID: "terminal", AttachmentID: "attachment"},
		{PID: 1, StartTime: "1960-01-01T00:00:00.000Z", TerminalID: "terminal", AttachmentID: "attachment"},
		{PID: 1, StartTime: validTime, TerminalID: "", AttachmentID: "attachment"},
		{PID: 1, StartTime: validTime, TerminalID: "terminal", AttachmentID: ""},
	}
	for _, target := range tests {
		control := NewLinuxProcessControl()
		if err := control.AcquireExactTarget(context.Background(), target); err == nil {
			t.Errorf("AcquireExactTarget(%+v) succeeded", target)
		}
		if got := len(control.pidfds); got != 0 {
			t.Errorf("AcquireExactTarget(%+v) owned %d pidfds, want 0", target, got)
		}
		_ = control.Close()
	}
}

func TestLinuxProcessControlSourceHasNoPIDSignalFallback(t *testing.T) {
	source, err := os.ReadFile("process_control_linux.go")
	if err != nil {
		t.Fatalf("read implementation source: %v", err)
	}
	if !bytes.Contains(source, []byte("unix.PidfdSendSignal(")) {
		t.Fatal("implementation does not contain pidfd signal delivery")
	}
	for _, forbidden := range []string{
		"unix.Kill(", "syscall.Kill(", "exec.Command(", "os.Process.Signal(",
		"os.Process.Kill(", "pkill", "/bin/kill", "pane.send_text", "pane.send_keys",
	} {
		if bytes.Contains(source, []byte(forbidden)) {
			t.Errorf("implementation contains forbidden PID/command fallback %q", forbidden)
		}
	}
}

type processControlChild struct {
	pid       int
	cleanupFD int
	command   *exec.Cmd
	once      sync.Once
}

func startProcessControlChild(t *testing.T) *processControlChild {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestProcessControlHelperProcess$")
	command.Env = append(os.Environ(), processControlHelperEnvironment)
	if err := command.Start(); err != nil {
		t.Fatalf("start disposable child: %v", err)
	}
	cleanupFD, err := unix.PidfdOpen(command.Process.Pid, 0)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("open test cleanup pidfd: %v", err)
	}
	child := &processControlChild{pid: command.Process.Pid, cleanupFD: cleanupFD, command: command}
	t.Cleanup(func() { child.cleanup(t) })
	return child
}

func (c *processControlChild) cleanup(t *testing.T) {
	t.Helper()
	c.once.Do(func() {
		if err := unix.PidfdSendSignal(c.cleanupFD, unix.SIGCONT, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			t.Errorf("resume disposable child: %v", err)
		}
		if err := unix.PidfdSendSignal(c.cleanupFD, unix.SIGKILL, nil, 0); err != nil && !errors.Is(err, unix.ESRCH) {
			t.Errorf("kill disposable child: %v", err)
		}
		c.waitAndClose(t)
	})
}

func (c *processControlChild) waitForExit(t *testing.T) {
	t.Helper()
	c.once.Do(func() { c.waitAndClose(t) })
}

func (c *processControlChild) waitAndClose(t *testing.T) {
	t.Helper()
	pollFDs := []unix.PollFd{{Fd: int32(c.cleanupFD), Events: unix.POLLIN}}
	ready, err := unix.Poll(pollFDs, 2_000)
	if err != nil || ready != 1 || pollFDs[0].Revents&unix.POLLIN == 0 {
		t.Errorf("disposable child pidfd did not report exit: ready=%d revents=%#x err=%v", ready, pollFDs[0].Revents, err)
		if killErr := unix.PidfdSendSignal(c.cleanupFD, unix.SIGKILL, nil, 0); killErr != nil && !errors.Is(killErr, unix.ESRCH) {
			t.Errorf("fallback kill disposable child: %v", killErr)
		}
		pollFDs[0].Revents = 0
		if cleanupReady, cleanupErr := unix.Poll(pollFDs, 2_000); cleanupErr != nil || cleanupReady != 1 || pollFDs[0].Revents&unix.POLLIN == 0 {
			t.Errorf("disposable child pidfd did not report exit after fallback cleanup: ready=%d revents=%#x err=%v", cleanupReady, pollFDs[0].Revents, cleanupErr)
		}
	}
	if err := c.command.Wait(); err != nil {
		var exitError *exec.ExitError
		if !errors.As(err, &exitError) {
			t.Errorf("wait for disposable child: %v", err)
		}
	}
	if err := unix.Close(c.cleanupFD); err != nil {
		t.Errorf("close test cleanup pidfd: %v", err)
	}
}

func assertProcessControlChildLive(t *testing.T, child *processControlChild) {
	t.Helper()
	if err := unix.PidfdSendSignal(child.cleanupFD, 0, nil, 0); err != nil {
		t.Fatalf("disposable child is not live: %v", err)
	}
	if state := testProcessState(t, child.pid); state == 'Z' || state == 'X' || state == 'x' {
		t.Fatalf("disposable child state = %q, want live", state)
	}
}

const (
	testProcessBirthLayout = "2006-01-02T15:04:05.000Z"
	testLinuxClockTicks    = int64(100)
)

func (c *processControlChild) identity(t *testing.T, terminalID, attachmentID string) ProcessIdentity {
	t.Helper()
	return ProcessIdentity{
		PID:          c.pid,
		StartTime:    independentlyDerivedProcessBirth(t, c.pid),
		TerminalID:   terminalID,
		AttachmentID: attachmentID,
	}
}

func independentlyDerivedProcessBirth(t *testing.T, pid int) string {
	t.Helper()
	raw := readTestProcStat(t, pid)
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 || end+2 >= len(raw) {
		t.Fatalf("child /proc stat has no comm terminator: %q", raw)
	}
	fields := bytes.Fields(raw[end+2:])
	if len(fields) <= 19 {
		t.Fatalf("child /proc stat has %d post-comm fields", len(fields))
	}
	ticks, err := strconv.ParseInt(string(fields[19]), 10, 64)
	if err != nil {
		t.Fatalf("parse child start ticks: %v", err)
	}
	procStat, err := os.ReadFile("/proc/stat")
	if err != nil {
		t.Fatalf("read /proc/stat: %v", err)
	}
	var bootSeconds int64 = -1
	for _, line := range bytes.Split(procStat, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) == 2 && string(fields[0]) == "btime" {
			bootSeconds, err = strconv.ParseInt(string(fields[1]), 10, 64)
			if err != nil {
				t.Fatalf("parse test btime: %v", err)
			}
			break
		}
	}
	if bootSeconds < 0 {
		t.Fatal("/proc/stat has no valid btime")
	}
	seconds := ticks / testLinuxClockTicks
	nanos := (ticks % testLinuxClockTicks) * int64(time.Second) / testLinuxClockTicks
	return time.Unix(bootSeconds+seconds, nanos).UTC().Format(testProcessBirthLayout)
}

func testProcessState(t *testing.T, pid int) byte {
	t.Helper()
	raw := readTestProcStat(t, pid)
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 || end+2 >= len(raw) {
		t.Fatalf("child /proc stat has no state: %q", raw)
	}
	fields := bytes.Fields(raw[end+2:])
	if len(fields) == 0 || len(fields[0]) != 1 {
		t.Fatalf("child /proc stat has malformed state: %q", raw)
	}
	return fields[0][0]
}

func readTestProcStat(t *testing.T, pid int) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		t.Fatalf("read child /proc stat: %v", err)
	}
	return raw
}

func countOpenTestPidfds(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatalf("read /proc/self/fd: %v", err)
	}
	count := 0
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join("/proc/self/fd", entry.Name()))
		if err == nil && target == "anon_inode:[pidfd]" {
			count++
		}
	}
	return count
}
