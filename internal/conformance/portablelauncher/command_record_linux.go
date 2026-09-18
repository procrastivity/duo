//go:build linux

package portablelauncher

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	// CommandCaptureDirectoryEnv explicitly enables process-level Duo command
	// capture. Its value must be the capture directory of a private portable
	// launcher run root.
	CommandCaptureDirectoryEnv = "DUO_CONFORMANCE_COMMAND_CAPTURE_DIR"
	// CommandRunOriginEnv is the suite controller's CLOCK_BOOTTIME origin in
	// nanoseconds. All command offsets are measured from this common origin.
	CommandRunOriginEnv = "DUO_CONFORMANCE_RUN_ORIGIN_BOOTTIME_NS"

	commandBypassEnv    = "DUO_CONFORMANCE_COMMAND_BYPASS"
	commandRecorderFail = 125
	commandSchema       = "duo-portable-launcher-command-v1"
	bypassFD            = 3
	bypassMagic         = "DUO-COMMAND-BYPASS-V1\x00"
)

// commandMetadata and the related records are deliberately concrete and
// versioned. A later assembler can decode them without interpreting CLI text.
// Arguments exclude argv[0], which is documented explicitly in commandArgv.
type commandMetadata struct {
	Schema              string            `json:"schema"`
	Clock               string            `json:"clock"`
	RunOriginBootTimeNS int64             `json:"run_origin_boottime_ns"`
	StartOffsetNS       int64             `json:"start_offset_ns"`
	WorkingDirectory    string            `json:"working_directory"`
	RecorderPID         int               `json:"recorder_pid"`
	Executable          commandExecutable `json:"executable"`
}

type commandExecutable struct {
	Device uint64 `json:"device"`
	Inode  uint64 `json:"inode"`
	SHA256 string `json:"sha256"`
}

type commandArgv struct {
	Schema        string   `json:"schema"`
	ExcludesArgv0 bool     `json:"excludes_argv0"`
	Arguments     []string `json:"arguments"`
}

type commandStarted struct {
	Schema              string `json:"schema"`
	ChildPID            int    `json:"child_pid"`
	ChildProcStartTicks uint64 `json:"child_proc_start_ticks"`
}

type commandStreamResult struct {
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256"`
}

type commandComplete struct {
	Schema              string              `json:"schema"`
	Clock               string              `json:"clock"`
	RunOriginBootTimeNS int64               `json:"run_origin_boottime_ns"`
	StartOffsetNS       int64               `json:"start_offset_ns"`
	EndOffsetNS         int64               `json:"end_offset_ns"`
	WorkingDirectory    string              `json:"working_directory"`
	RecorderPID         int                 `json:"recorder_pid"`
	ChildPID            int                 `json:"child_pid"`
	ChildProcStartTicks uint64              `json:"child_proc_start_ticks"`
	Executable          commandExecutable   `json:"executable"`
	ExitCode            int                 `json:"exit_code"`
	Signal              int                 `json:"signal"`
	SignalName          string              `json:"signal_name"`
	Stdout              commandStreamResult `json:"stdout"`
	Stderr              commandStreamResult `json:"stderr"`
}

type commandRecorderInvocation struct {
	args   []string
	env    []string
	cwd    string
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	exe    string
	pid    int
}

type commandRecorderConfig struct {
	runRoot    string
	captureDir string
	originNS   int64
	startNS    int64
	cwdToken   string
	executable commandExecutable
}

// RecordCommandIfConfigured is the Linux CLI entry seam. With neither
// recorder variable configured it returns immediately, leaving main's normal
// iostreams.System -> cli.NewRootCommand -> cli.Execute path untouched.
func RecordCommandIfConfigured() (recorded bool, exitCode int) {
	environment := os.Environ()
	if !recorderEnvironmentPresent(environment) {
		return false, 0
	}
	cwd, err := os.Getwd()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "duo command recorder: determine cwd: %v\n", err)
		return true, commandRecorderFail
	}
	recorded, exitCode = recordCommand(commandRecorderInvocation{
		args: os.Args, env: environment, cwd: cwd,
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		exe: "/proc/self/exe", pid: os.Getpid(),
	})
	if !recorded {
		_ = os.Unsetenv(commandBypassEnv)
	}
	return recorded, exitCode
}

func recordCommand(in commandRecorderInvocation) (bool, int) {
	if consumeCommandBypass(in.env, in.exe) {
		return false, 0
	}
	if !recorderEnvironmentPresent(in.env) {
		return false, 0
	}
	fail := func(err error) (bool, int) {
		_, _ = fmt.Fprintf(in.stderr, "duo command recorder: %v\n", err)
		return true, commandRecorderFail
	}
	config, err := loadCommandRecorderConfig(in)
	if err != nil {
		return fail(err)
	}
	code, err := executeRecordedCommand(in, config)
	if err != nil {
		return fail(err)
	}
	return true, code
}

func recorderEnvironmentPresent(env []string) bool {
	return envValue(env, CommandCaptureDirectoryEnv) != "" || envValue(env, CommandRunOriginEnv) != ""
}

func loadCommandRecorderConfig(in commandRecorderInvocation) (commandRecorderConfig, error) {
	capture := envValue(in.env, CommandCaptureDirectoryEnv)
	originValue := envValue(in.env, CommandRunOriginEnv)
	if capture == "" || originValue == "" {
		return commandRecorderConfig{}, fmt.Errorf("capture directory and common run origin must both be configured")
	}
	origin, err := strconv.ParseInt(originValue, 10, 64)
	if err != nil || origin <= 0 {
		return commandRecorderConfig{}, fmt.Errorf("common run origin must be a positive CLOCK_BOOTTIME nanosecond value")
	}
	now, err := bootTimeNanoseconds()
	if err != nil {
		return commandRecorderConfig{}, fmt.Errorf("read CLOCK_BOOTTIME: %w", err)
	}
	if origin > now {
		return commandRecorderConfig{}, fmt.Errorf("common run origin is after recorder start")
	}

	capture, runRoot, err := validateCommandCaptureDirectory(capture)
	if err != nil {
		return commandRecorderConfig{}, err
	}
	cwd, err := filepath.Abs(in.cwd)
	if err != nil {
		return commandRecorderConfig{}, fmt.Errorf("resolve working directory: %w", err)
	}
	resolvedCWD, err := filepath.EvalSymlinks(cwd)
	if err != nil || filepath.Clean(resolvedCWD) != filepath.Clean(cwd) || !within(runRoot, resolvedCWD) {
		return commandRecorderConfig{}, fmt.Errorf("working directory must resolve without symlinks below the run root")
	}
	rel, err := filepath.Rel(runRoot, resolvedCWD)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return commandRecorderConfig{}, fmt.Errorf("tokenize working directory below run root")
	}
	cwdToken := "$RUN"
	if rel != "." {
		cwdToken += "/" + filepath.ToSlash(rel)
	}
	executable, err := identifyCommandExecutable(in.exe)
	if err != nil {
		return commandRecorderConfig{}, err
	}
	return commandRecorderConfig{
		runRoot: runRoot, captureDir: capture, originNS: origin,
		startNS: now - origin, cwdToken: cwdToken, executable: executable,
	}, nil
}

func validateCommandCaptureDirectory(path string) (string, string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve command capture directory: %w", err)
	}
	if filepath.Clean(abs) != abs {
		return "", "", fmt.Errorf("command capture directory must be a clean absolute path")
	}
	info, err := os.Lstat(abs)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return "", "", fmt.Errorf("command capture directory must exist as a private non-symlink directory")
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil || filepath.Clean(resolved) != abs {
		return "", "", fmt.Errorf("command capture directory contains a symlink component")
	}

	for candidate := filepath.Dir(abs); ; candidate = filepath.Dir(candidate) {
		if strings.HasPrefix(filepath.Base(candidate), "duo-portable-launcher-") {
			root, rootErr := validatePrivateTempDir(candidate)
			if rootErr != nil || !within(root, abs) {
				return "", "", fmt.Errorf("command capture directory is not below a validated private run root")
			}
			if abs != filepath.Join(root, "capture") {
				return "", "", fmt.Errorf("command capture directory must be the run root capture directory")
			}
			return abs, root, nil
		}
		parent := filepath.Dir(candidate)
		if parent == candidate {
			break
		}
	}
	return "", "", fmt.Errorf("command capture directory has no duo-portable-launcher run root")
}

func identifyCommandExecutable(path string) (commandExecutable, error) {
	f, err := os.Open(path)
	if err != nil {
		return commandExecutable{}, fmt.Errorf("open recorder executable: %w", err)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return commandExecutable{}, fmt.Errorf("recorder executable is not a regular file")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return commandExecutable{}, fmt.Errorf("recorder executable has no Linux stat identity")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return commandExecutable{}, fmt.Errorf("hash recorder executable: %w", err)
	}
	return commandExecutable{Device: uint64(stat.Dev), Inode: stat.Ino, SHA256: hex.EncodeToString(hash.Sum(nil))}, nil
}

func executeRecordedCommand(in commandRecorderInvocation, config commandRecorderConfig) (exitCode int, resultErr error) {
	commandsDir := filepath.Join(config.captureDir, "duo-commands")
	if err := ensurePrivateCommandDirectory(commandsDir); err != nil {
		return 0, err
	}
	name, err := randomCommandInvocationName(in.pid)
	if err != nil {
		return 0, err
	}
	inflight := filepath.Join(commandsDir, name+".inflight")
	complete := filepath.Join(commandsDir, name+".complete")
	if err := os.Mkdir(inflight, 0o700); err != nil {
		return 0, fmt.Errorf("create invocation directory: %w", err)
	}
	if err := os.Chmod(inflight, 0o700); err != nil {
		_ = os.Remove(inflight)
		return 0, fmt.Errorf("set invocation directory mode: %w", err)
	}
	removeBeforeStart := true
	defer func() {
		if resultErr != nil && removeBeforeStart {
			resultErr = errors.Join(resultErr, os.RemoveAll(inflight))
		}
	}()

	metadata := commandMetadata{
		Schema: commandSchema, Clock: "CLOCK_BOOTTIME",
		RunOriginBootTimeNS: config.originNS, StartOffsetNS: config.startNS,
		WorkingDirectory: config.cwdToken, RecorderPID: in.pid, Executable: config.executable,
	}
	argv := commandArgv{Schema: commandSchema, ExcludesArgv0: true, Arguments: append([]string(nil), in.args[1:]...)}
	if err := writeCommandJSON(filepath.Join(inflight, "metadata.json"), metadata); err != nil {
		return 0, err
	}
	if err := writeCommandJSON(filepath.Join(inflight, "argv.json"), argv); err != nil {
		return 0, err
	}
	stdoutFile, err := openCommandFile(filepath.Join(inflight, "stdout.raw"))
	if err != nil {
		return 0, err
	}
	defer func() { _ = stdoutFile.Close() }()
	stderrFile, err := openCommandFile(filepath.Join(inflight, "stderr.raw"))
	if err != nil {
		return 0, err
	}
	defer func() { _ = stderrFile.Close() }()

	capRead, capWrite, marker, err := newCommandBypassCapability(in.pid)
	if err != nil {
		return 0, err
	}
	defer func() { _ = capRead.Close() }()
	childEnv := setEnv(in.env, commandBypassEnv, marker)
	cmd := exec.Command(in.exe, in.args[1:]...)
	cmd.Args[0] = in.args[0]
	cmd.Dir = in.cwd
	cmd.Env = childEnv
	cmd.Stdin = in.stdin
	cmd.ExtraFiles = []*os.File{capRead}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}

	var killOnce sync.Once
	var forwardingErrMu sync.Mutex
	var forwardingErr error
	killOnError := func(err error) {
		forwardingErrMu.Lock()
		forwardingErr = errors.Join(forwardingErr, err)
		forwardingErrMu.Unlock()
		killOnce.Do(func() {
			if cmd.Process != nil {
				_ = cmd.Process.Kill()
			}
		})
	}
	stdoutCapture := newCommandStreamWriter(stdoutFile, in.stdout, killOnError)
	stderrCapture := newCommandStreamWriter(stderrFile, in.stderr, killOnError)
	cmd.Stdout = stdoutCapture
	cmd.Stderr = stderrCapture
	if err := cmd.Start(); err != nil {
		_ = capWrite.Close()
		return 0, fmt.Errorf("start inner Duo CLI: %w", err)
	}
	if err := capWrite.Close(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 0, fmt.Errorf("close bypass capability: %w", err)
	}
	_ = capRead.Close()
	childTicks, err := readCommandProcStartTicks(cmd.Process.Pid)
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 0, err
	}
	started := commandStarted{Schema: commandSchema, ChildPID: cmd.Process.Pid, ChildProcStartTicks: childTicks}
	if err := writeCommandJSON(filepath.Join(inflight, "started.json"), started); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return 0, err
	}
	removeBeforeStart = false

	forwardSignals := make(chan os.Signal, 8)
	signal.Notify(forwardSignals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGQUIT)
	signalDone := make(chan struct{})
	go func() {
		defer close(signalDone)
		for sig := range forwardSignals {
			if cmd.Process != nil {
				_ = cmd.Process.Signal(sig)
			}
		}
	}()
	waitErr := cmd.Wait()
	signal.Stop(forwardSignals)
	close(forwardSignals)
	<-signalDone

	forwardingErrMu.Lock()
	streamErr := forwardingErr
	forwardingErrMu.Unlock()
	if streamErr != nil {
		return 0, fmt.Errorf("capture and forward child streams: %w", streamErr)
	}
	exitCode, signalNumber, signalName, err := commandExitStatus(cmd.ProcessState, waitErr)
	if err != nil {
		return 0, err
	}
	end, err := bootTimeNanoseconds()
	if err != nil || end < config.originNS+config.startNS {
		return 0, fmt.Errorf("read monotonic command completion")
	}
	if err := stdoutFile.Sync(); err != nil {
		return 0, fmt.Errorf("sync stdout capture: %w", err)
	}
	if err := stderrFile.Sync(); err != nil {
		return 0, fmt.Errorf("sync stderr capture: %w", err)
	}
	stdoutResult, err := stdoutCapture.result()
	if err != nil {
		return 0, fmt.Errorf("finalize stdout capture: %w", err)
	}
	stderrResult, err := stderrCapture.result()
	if err != nil {
		return 0, fmt.Errorf("finalize stderr capture: %w", err)
	}
	if err := stdoutFile.Close(); err != nil {
		return 0, fmt.Errorf("close stdout capture: %w", err)
	}
	if err := stderrFile.Close(); err != nil {
		return 0, fmt.Errorf("close stderr capture: %w", err)
	}
	completion := commandComplete{
		Schema: commandSchema, Clock: "CLOCK_BOOTTIME",
		RunOriginBootTimeNS: config.originNS, StartOffsetNS: config.startNS, EndOffsetNS: end - config.originNS,
		WorkingDirectory: config.cwdToken, RecorderPID: in.pid,
		ChildPID: cmd.Process.Pid, ChildProcStartTicks: childTicks, Executable: config.executable,
		ExitCode: exitCode, Signal: signalNumber, SignalName: signalName,
		Stdout: stdoutResult, Stderr: stderrResult,
	}
	if err := writeCommandJSON(filepath.Join(inflight, "complete.json"), completion); err != nil {
		return 0, err
	}
	if err := syncCommandDirectory(inflight); err != nil {
		return 0, err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, inflight, unix.AT_FDCWD, complete, unix.RENAME_NOREPLACE); err != nil {
		return 0, fmt.Errorf("publish completed invocation: %w", err)
	}
	if err := syncCommandDirectory(commandsDir); err != nil {
		return 0, err
	}
	return exitCode, nil
}

func ensurePrivateCommandDirectory(path string) error {
	err := os.Mkdir(path, 0o700)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("create command records directory: %w", err)
	}
	if err == nil {
		if chmodErr := os.Chmod(path, 0o700); chmodErr != nil {
			return fmt.Errorf("set command records directory mode: %w", chmodErr)
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("command records directory must be a mode-0700 non-symlink directory")
	}
	return nil
}

func randomCommandInvocationName(pid int) (string, error) {
	random := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, random); err != nil {
		return "", fmt.Errorf("generate invocation identity: %w", err)
	}
	return fmt.Sprintf("invocation-%d-%s", pid, hex.EncodeToString(random)), nil
}

func openCommandFile(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create command record %s: %w", filepath.Base(path), err)
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func writeCommandJSON(path string, value any) error {
	f, err := openCommandFile(path)
	if err != nil {
		return err
	}
	encoder := json.NewEncoder(f)
	encoder.SetEscapeHTML(false)
	writeErr := encoder.Encode(value)
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return fmt.Errorf("write command record %s: %w", filepath.Base(path), err)
	}
	return nil
}

func syncCommandDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(dir.Sync(), dir.Close())
}

type commandStreamWriter struct {
	capture *os.File
	forward io.Writer
	bytes   int64
	onError func(error)
	mu      sync.Mutex
}

func newCommandStreamWriter(capture *os.File, forward io.Writer, onError func(error)) *commandStreamWriter {
	return &commandStreamWriter{capture: capture, forward: forward, onError: onError}
}

func (w *commandStreamWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.capture.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.onError(fmt.Errorf("write raw capture: %w", err))
		return n, err
	}
	w.bytes += int64(len(p))
	// Re-hashing the complete file at result time avoids mutable hash state in
	// metadata and keeps the raw stream itself authoritative.
	n, err = w.forward.Write(p)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	if err != nil {
		w.onError(fmt.Errorf("forward raw stream: %w", err))
		return n, err
	}
	return len(p), nil
}

func (w *commandStreamWriter) result() (commandStreamResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	// Capture files are write-only, so compute the stream digest from a second
	// descriptor after all writes have completed.
	b, err := os.ReadFile(w.capture.Name())
	if err != nil {
		return commandStreamResult{}, fmt.Errorf("read completed stream capture: %w", err)
	}
	if int64(len(b)) != w.bytes {
		return commandStreamResult{}, fmt.Errorf("stream capture byte count changed")
	}
	sum := sha256.Sum256(b)
	return commandStreamResult{Bytes: w.bytes, SHA256: hex.EncodeToString(sum[:])}, nil
}

func commandExitStatus(state *os.ProcessState, waitErr error) (int, int, string, error) {
	if state == nil {
		return 0, 0, "", fmt.Errorf("inner Duo CLI has no process state")
	}
	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok {
		return 0, 0, "", fmt.Errorf("inner Duo CLI has no Linux wait status")
	}
	if status.Signaled() {
		sig := status.Signal()
		return 128 + int(sig), int(sig), sig.String(), nil
	}
	if status.Exited() {
		code := status.ExitStatus()
		if waitErr == nil || isExitError(waitErr) {
			return code, 0, "", nil
		}
	}
	return 0, 0, "", fmt.Errorf("wait for inner Duo CLI: %w", waitErr)
}

func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}

func readCommandProcStartTicks(pid int) (uint64, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, fmt.Errorf("read child process start ticks: %w", err)
	}
	end := strings.LastIndexByte(string(raw), ')')
	if end < 0 || end+2 >= len(raw) {
		return 0, fmt.Errorf("child process stat is malformed")
	}
	fields := strings.Fields(string(raw[end+2:]))
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return 0, fmt.Errorf("child process stat is incomplete")
	}
	ticks, err := strconv.ParseUint(fields[startTimeIndex], 10, 64)
	if err != nil || ticks == 0 {
		return 0, fmt.Errorf("child process start ticks are invalid")
	}
	return ticks, nil
}

func bootTimeNanoseconds() (int64, error) {
	var value unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_BOOTTIME, &value); err != nil {
		return 0, err
	}
	return value.Nano(), nil
}

func newCommandBypassCapability(parentPID int) (*os.File, *os.File, string, error) {
	read, write, err := os.Pipe()
	if err != nil {
		return nil, nil, "", fmt.Errorf("create bypass capability: %w", err)
	}
	token := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, token); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, nil, "", err
	}
	payload := make([]byte, len(bypassMagic)+8+len(token))
	copy(payload, bypassMagic)
	binary.BigEndian.PutUint64(payload[len(bypassMagic):], uint64(parentPID))
	copy(payload[len(bypassMagic)+8:], token)
	if _, err := write.Write(payload); err != nil {
		_ = read.Close()
		_ = write.Close()
		return nil, nil, "", fmt.Errorf("write bypass capability: %w", err)
	}
	digest := sha256.Sum256(payload)
	marker := strconv.Itoa(bypassFD) + ":" + hex.EncodeToString(digest[:])
	return read, write, marker, nil
}

func consumeCommandBypass(env []string, selfExe string) bool {
	marker := envValue(env, commandBypassEnv)
	parts := strings.Split(marker, ":")
	if len(parts) != 2 || parts[0] != strconv.Itoa(bypassFD) {
		return false
	}
	wantDigest, err := hex.DecodeString(parts[1])
	if err != nil || len(wantDigest) != sha256.Size {
		return false
	}
	f := os.NewFile(uintptr(bypassFD), "duo-command-bypass")
	if f == nil {
		return false
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		return false
	}
	payload, err := io.ReadAll(io.LimitReader(f, int64(len(bypassMagic)+8+32+1)))
	if err != nil || len(payload) != len(bypassMagic)+8+32 || string(payload[:len(bypassMagic)]) != bypassMagic {
		return false
	}
	digest := sha256.Sum256(payload)
	if !equalBytes(digest[:], wantDigest) {
		return false
	}
	parentPID := int(binary.BigEndian.Uint64(payload[len(bypassMagic):]))
	if parentPID <= 0 || parentPID != os.Getppid() {
		return false
	}
	self, err := os.Stat(selfExe)
	if err != nil {
		return false
	}
	parent, err := os.Stat("/proc/" + strconv.Itoa(parentPID) + "/exe")
	if err != nil || !os.SameFile(self, parent) {
		return false
	}
	return true
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var different byte
	for i := range a {
		different |= a[i] ^ b[i]
	}
	return different == 0
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for i := len(env) - 1; i >= 0; i-- {
		if strings.HasPrefix(env[i], prefix) {
			return strings.TrimPrefix(env[i], prefix)
		}
	}
	return ""
}

func setEnv(env []string, key, value string) []string {
	prefix := key + "="
	out := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, prefix) {
			out = append(out, entry)
		}
	}
	return append(out, prefix+value)
}
