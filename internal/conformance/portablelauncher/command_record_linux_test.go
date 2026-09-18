//go:build linux

package portablelauncher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
)

const commandRecorderHelperEnv = "DUO_COMMAND_RECORDER_TEST_HELPER"

func TestCommandRecorderHelper(_ *testing.T) {
	if os.Getenv(commandRecorderHelperEnv) != "1" {
		return
	}
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(119)
	}
	handled, code := recordCommand(commandRecorderInvocation{
		args: os.Args, env: os.Environ(), cwd: cwd,
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
		exe: "/proc/self/exe", pid: os.Getpid(),
	})
	if handled {
		os.Exit(code)
	}
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		os.Exit(118)
	}
	switch os.Args[separator+1] {
	case "streams":
		input, readErr := io.ReadAll(os.Stdin)
		if readErr != nil {
			os.Exit(117)
		}
		_, _ = os.Stdout.Write(append([]byte("stdout:"), input...))
		_, _ = os.Stderr.Write([]byte("stderr:\x00\xff\n"))
		os.Exit(0)
	case "exit":
		code, _ := strconv.Atoi(os.Args[separator+2])
		os.Exit(code)
	case "signal":
		_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
		select {}
	case "quiet":
		os.Exit(0)
	default:
		os.Exit(116)
	}
}

func TestCommandRecorderDisabledDoesNotHandleExecution(t *testing.T) {
	in := commandTestInvocation(t, nil, "quiet")
	in.env = removeCommandRecorderEnv(os.Environ())
	handled, code := recordCommand(in)
	if handled || code != 0 {
		t.Fatalf("recordCommand disabled = (%v, %d), want (false, 0)", handled, code)
	}
	if _, err := os.Stat(filepath.Join(in.cwd, "..", "capture", "duo-commands")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled recorder touched capture: %v", err)
	}
}

func TestRecordedCommandForwardsAndCapturesIndependentStreamsAndStdin(t *testing.T) {
	input := []byte("input\x00bytes\n")
	in := commandTestInvocation(t, input, "streams", "arg with spaces", "literal-byte-marker")
	var stdout, stderr bytes.Buffer
	in.stdout, in.stderr = &stdout, &stderr
	handled, code := recordCommand(in)
	if !handled || code != 0 {
		t.Fatalf("recordCommand = (%v, %d), stderr %q", handled, code, stderr.String())
	}
	wantStdout := append([]byte("stdout:"), input...)
	wantStderr := []byte("stderr:\x00\xff\n")
	if !bytes.Equal(stdout.Bytes(), wantStdout) || !bytes.Equal(stderr.Bytes(), wantStderr) {
		t.Fatalf("forwarded streams stdout=%q stderr=%q", stdout.Bytes(), stderr.Bytes())
	}

	dir := onlyCompletedCommand(t, captureForInvocation(t, in))
	assertFileBytes(t, filepath.Join(dir, "stdout.raw"), wantStdout)
	assertFileBytes(t, filepath.Join(dir, "stderr.raw"), wantStderr)
	var argv commandArgv
	readCommandJSON(t, filepath.Join(dir, "argv.json"), &argv)
	if !argv.ExcludesArgv0 || !reflect.DeepEqual(argv.Arguments, in.args[1:]) {
		t.Fatalf("argv record = %#v, want %#v", argv, in.args[1:])
	}
	var complete commandComplete
	readCommandJSON(t, filepath.Join(dir, "complete.json"), &complete)
	assertCommandStreamResult(t, complete.Stdout, wantStdout)
	assertCommandStreamResult(t, complete.Stderr, wantStderr)
}

func TestRecordedCommandMetadataAndNoEnvironmentValues(t *testing.T) {
	in := commandTestInvocation(t, nil, "quiet", "exact-argument")
	secret := "environment-value-must-not-appear-9f7ce8"
	in.env = append(in.env, "RECORDER_SENTINEL="+secret)
	before, err := bootTimeNanoseconds()
	if err != nil {
		t.Fatal(err)
	}
	handled, code := recordCommand(in)
	after, afterErr := bootTimeNanoseconds()
	if afterErr != nil {
		t.Fatal(afterErr)
	}
	if !handled || code != 0 {
		t.Fatalf("recordCommand = (%v, %d)", handled, code)
	}
	dir := onlyCompletedCommand(t, captureForInvocation(t, in))
	var metadata commandMetadata
	readCommandJSON(t, filepath.Join(dir, "metadata.json"), &metadata)
	var started commandStarted
	readCommandJSON(t, filepath.Join(dir, "started.json"), &started)
	var complete commandComplete
	readCommandJSON(t, filepath.Join(dir, "complete.json"), &complete)
	origin, _ := strconv.ParseInt(envValue(in.env, CommandRunOriginEnv), 10, 64)
	if metadata.Schema != commandSchema || metadata.Clock != "CLOCK_BOOTTIME" || metadata.RunOriginBootTimeNS != origin {
		t.Fatalf("metadata identity = %#v", metadata)
	}
	if metadata.WorkingDirectory != "$RUN/workspace" || metadata.RecorderPID != in.pid {
		t.Fatalf("metadata process/path = %#v", metadata)
	}
	if metadata.Executable.Device == 0 || metadata.Executable.Inode == 0 || len(metadata.Executable.SHA256) != 64 {
		t.Fatalf("metadata executable = %#v", metadata.Executable)
	}
	if started.ChildPID <= 0 || started.ChildProcStartTicks == 0 || complete.ChildPID != started.ChildPID || complete.ChildProcStartTicks != started.ChildProcStartTicks {
		t.Fatalf("start/complete process identity = %#v / %#v", started, complete)
	}
	if metadata.StartOffsetNS < before-origin || metadata.StartOffsetNS > after-origin || complete.EndOffsetNS < metadata.StartOffsetNS || complete.EndOffsetNS > after-origin {
		t.Fatalf("monotonic offsets start=%d end=%d bounds=[%d,%d]", metadata.StartOffsetNS, complete.EndOffsetNS, before-origin, after-origin)
	}
	for _, name := range []string{"metadata.json", "argv.json", "started.json", "complete.json"} {
		data, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			t.Fatal(readErr)
		}
		if bytes.Contains(data, []byte(secret)) || bytes.Contains(data, []byte("RECORDER_SENTINEL")) {
			t.Fatalf("%s recorded environment content", name)
		}
	}
	assertPrivateCommandModes(t, dir)
}

func TestRecordedCommandReturnsNonzeroAndSignalExit(t *testing.T) {
	t.Run("nonzero", func(t *testing.T) {
		in := commandTestInvocation(t, nil, "exit", "37")
		handled, code := recordCommand(in)
		if !handled || code != 37 {
			t.Fatalf("recordCommand = (%v, %d), want exit 37", handled, code)
		}
		var complete commandComplete
		readCommandJSON(t, filepath.Join(onlyCompletedCommand(t, captureForInvocation(t, in)), "complete.json"), &complete)
		if complete.ExitCode != 37 || complete.Signal != 0 || complete.SignalName != "" {
			t.Fatalf("completion status = %#v", complete)
		}
	})
	t.Run("signal", func(t *testing.T) {
		in := commandTestInvocation(t, nil, "signal")
		handled, code := recordCommand(in)
		if !handled || code != 128+int(syscall.SIGTERM) {
			t.Fatalf("recordCommand = (%v, %d), want signal exit", handled, code)
		}
		var complete commandComplete
		readCommandJSON(t, filepath.Join(onlyCompletedCommand(t, captureForInvocation(t, in)), "complete.json"), &complete)
		if complete.Signal != int(syscall.SIGTERM) || complete.SignalName != syscall.SIGTERM.String() {
			t.Fatalf("completion signal = %#v", complete)
		}
	})
}

func TestCommandRecorderConcurrentInvocationsUseUniqueRecords(t *testing.T) {
	_, capture, workspace, origin := newCommandRun(t)
	const count = 8
	var wg sync.WaitGroup
	errorsCh := make(chan string, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			in := newCommandTestInvocation(capture, workspace, origin, nil, "exit", strconv.Itoa(i%3))
			handled, code := recordCommand(in)
			if !handled || code != i%3 {
				errorsCh <- "unexpected concurrent result"
			}
		}(i)
	}
	wg.Wait()
	close(errorsCh)
	for message := range errorsCh {
		t.Error(message)
	}
	entries := commandRecordEntries(t, capture)
	if len(entries) != count {
		t.Fatalf("record count = %d, want %d (%v)", len(entries), count, entries)
	}
	seen := make(map[string]bool, count)
	for _, entry := range entries {
		if filepath.Ext(entry) != ".complete" || seen[entry] {
			t.Fatalf("non-unique or incomplete concurrent record %q", entry)
		}
		seen[entry] = true
	}
}

func TestCommandRecorderMarkerWithoutCapabilityCannotBypass(t *testing.T) {
	in := commandTestInvocation(t, nil, "quiet")
	in.env = setEnv(in.env, commandBypassEnv, "spoofed-marker")
	handled, code := recordCommand(in)
	if !handled || code != 0 {
		t.Fatalf("spoofed marker result = (%v, %d)", handled, code)
	}
	if len(commandRecordEntries(t, captureForInvocation(t, in))) != 1 {
		t.Fatal("spoofed marker bypassed command recording")
	}
}

func TestCommandRecorderPreservesStartedIncompleteRecord(t *testing.T) {
	in := commandTestInvocation(t, nil, "streams")
	in.stdout = failingCommandWriter{}
	var stderr bytes.Buffer
	in.stderr = &stderr
	handled, code := recordCommand(in)
	if !handled || code != commandRecorderFail {
		t.Fatalf("stream failure result = (%v, %d), stderr=%q", handled, code, stderr.String())
	}
	entries := commandRecordEntries(t, captureForInvocation(t, in))
	if len(entries) != 1 || filepath.Ext(entries[0]) != ".inflight" {
		t.Fatalf("incomplete entries = %v", entries)
	}
	dir := filepath.Join(captureForInvocation(t, in), "duo-commands", entries[0])
	if _, err := os.Stat(filepath.Join(dir, "started.json")); err != nil {
		t.Fatalf("incomplete record has no usable start record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "complete.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("incomplete record has completion: %v", err)
	}
}

func TestCommandRecorderRefusesSymlinkAndPathEscape(t *testing.T) {
	_, capture, workspace, origin := newCommandRun(t)
	outside := t.TempDir()
	link := filepath.Join(filepath.Dir(capture), "capture-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		capture string
		cwd     string
	}{
		{name: "capture symlink", capture: link, cwd: workspace},
		{name: "cwd escape", capture: capture, cwd: outside},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in := newCommandTestInvocation(test.capture, test.cwd, origin, nil, "quiet")
			var stderr bytes.Buffer
			in.stderr = &stderr
			handled, code := recordCommand(in)
			if !handled || code != commandRecorderFail || stderr.Len() == 0 {
				t.Fatalf("unsafe path result = (%v, %d), stderr=%q", handled, code, stderr.String())
			}
		})
	}
	if entries := commandRecordEntries(t, capture); len(entries) != 0 {
		t.Fatalf("unsafe path ran child or wrote record: %v", entries)
	}
}

type failingCommandWriter struct{}

func (failingCommandWriter) Write([]byte) (int, error) {
	return 0, errors.New("injected forwarding failure")
}

func commandTestInvocation(t *testing.T, input []byte, helperArgs ...string) commandRecorderInvocation {
	t.Helper()
	_, capture, workspace, origin := newCommandRun(t)
	return newCommandTestInvocation(capture, workspace, origin, input, helperArgs...)
}

func newCommandTestInvocation(capture, workspace string, origin int64, input []byte, helperArgs ...string) commandRecorderInvocation {
	args := []string{"duo-test", "-test.run=^TestCommandRecorderHelper$", "--"}
	args = append(args, helperArgs...)
	env := removeCommandRecorderEnv(os.Environ())
	env = append(env,
		commandRecorderHelperEnv+"=1",
		CommandCaptureDirectoryEnv+"="+capture,
		CommandRunOriginEnv+"="+strconv.FormatInt(origin, 10),
	)
	return commandRecorderInvocation{
		args: args, env: env, cwd: workspace, stdin: bytes.NewReader(input),
		stdout: io.Discard, stderr: io.Discard, exe: "/proc/self/exe", pid: os.Getpid(),
	}
}

func newCommandRun(t *testing.T) (root, capture, workspace string, origin int64) {
	t.Helper()
	base := t.TempDir()
	root, err := os.MkdirTemp(base, "duo-portable-launcher-")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	capture = filepath.Join(root, "capture")
	workspace = filepath.Join(root, "workspace")
	for _, dir := range []string{capture, workspace} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	now, err := bootTimeNanoseconds()
	if err != nil {
		t.Fatal(err)
	}
	return root, capture, workspace, now - 1_000_000
}

func removeCommandRecorderEnv(env []string) []string {
	keys := []string{CommandCaptureDirectoryEnv + "=", CommandRunOriginEnv + "=", commandBypassEnv + "=", commandRecorderHelperEnv + "="}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		keep := true
		for _, key := range keys {
			if strings.HasPrefix(entry, key) {
				keep = false
			}
		}
		if keep {
			out = append(out, entry)
		}
	}
	return out
}

func captureForInvocation(t *testing.T, in commandRecorderInvocation) string {
	t.Helper()
	return envValue(in.env, CommandCaptureDirectoryEnv)
}

func commandRecordEntries(t *testing.T, capture string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(capture, "duo-commands"))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
}

func onlyCompletedCommand(t *testing.T, capture string) string {
	t.Helper()
	entries := commandRecordEntries(t, capture)
	if len(entries) != 1 || filepath.Ext(entries[0]) != ".complete" {
		t.Fatalf("command entries = %v, want one complete record", entries)
	}
	return filepath.Join(capture, "duo-commands", entries[0])
}

func readCommandJSON(t *testing.T, path string, target any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		t.Fatalf("trailing JSON in %s: %v", path, err)
	}
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertCommandStreamResult(t *testing.T, got commandStreamResult, data []byte) {
	t.Helper()
	sum := sha256.Sum256(data)
	if got.Bytes != int64(len(data)) || got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("stream result = %#v for %d bytes", got, len(data))
	}
}

func assertPrivateCommandModes(t *testing.T, invocation string) {
	t.Helper()
	dirs := []string{filepath.Dir(invocation), invocation}
	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("stat directory %s: %v", dir, err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("directory %s mode = %v", dir, info.Mode().Perm())
		}
	}
	entries, err := os.ReadDir(invocation)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, infoErr := entry.Info()
		if infoErr != nil {
			t.Fatalf("stat file %s: %v", entry.Name(), infoErr)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("file %s mode = %v", entry.Name(), info.Mode().Perm())
		}
	}
}
