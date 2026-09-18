//go:build linux

package portablelauncher

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type commandCaptureFixture struct {
	capture    string
	origin     int64
	executable RecordedExecutable
	directory  string
	metadata   commandMetadata
	argv       commandArgv
	started    commandStarted
	complete   commandComplete
	stdout     []byte
	stderr     []byte
}

func TestLoadRecordedCommandsValidExactAndDefensive(t *testing.T) {
	fixture := newCommandCaptureFixture(t, "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 50, 90)
	got, err := LoadRecordedCommands(fixture.capture, fixture.origin, fixture.executable)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("records = %d, want 1", len(got))
	}
	want := RecordedCommand{
		InvocationID: "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Arguments:    []string{"session", "launch", "arg with spaces"},
		Stdout:       []byte("stdout\x00\xff"), Stderr: []byte("stderr\n"),
		WorkingDirectory: "$RUN/workspace", StartOffsetNS: 50, EndOffsetNS: 90,
		RecorderPID: 41, ChildPID: 52, ChildProcStartTicks: 700,
		Executable: fixture.executable, ExitCode: 17,
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Fatalf("record = %#v, want %#v", got[0], want)
	}

	got[0].Arguments[0] = "changed"
	got[0].Stdout[0] = 'X'
	got[0].Stderr[0] = 'Y'
	again, err := LoadRecordedCommands(fixture.capture, fixture.origin, fixture.executable)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(again[0].Arguments, want.Arguments) || !bytes.Equal(again[0].Stdout, want.Stdout) || !bytes.Equal(again[0].Stderr, want.Stderr) {
		t.Fatalf("returned slices alias loader state: %#v", again[0])
	}
}

func TestLoadRecordedCommandsOrdersOverlappingFactsByStartThenInvocation(t *testing.T) {
	first := newCommandCaptureFixture(t, "invocation-41-cccccccccccccccccccccccccccccccc", 30, 100)
	writeCommandCaptureFixture(t, first)
	second := first
	second.directory = filepath.Join(first.capture, "duo-commands", "invocation-42-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb.complete")
	second.metadata.RecorderPID, second.complete.RecorderPID = 42, 42
	second.metadata.StartOffsetNS, second.complete.StartOffsetNS, second.complete.EndOffsetNS = 10, 10, 80
	second.started.ChildPID, second.complete.ChildPID = 53, 53
	writeCommandCaptureFixture(t, second)
	third := first
	third.directory = filepath.Join(first.capture, "duo-commands", "invocation-43-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.complete")
	third.metadata.RecorderPID, third.complete.RecorderPID = 43, 43
	third.metadata.StartOffsetNS, third.complete.StartOffsetNS, third.complete.EndOffsetNS = 10, 10, 70
	third.started.ChildPID, third.complete.ChildPID = 54, 54
	writeCommandCaptureFixture(t, third)

	got, err := LoadRecordedCommands(first.capture, first.origin, first.executable)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"invocation-42-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"invocation-43-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"invocation-41-cccccccccccccccccccccccccccccccc",
	}
	identities := make([]string, len(got))
	for i := range got {
		identities[i] = got[i].InvocationID
	}
	if !reflect.DeepEqual(identities, want) || got[0].EndOffsetNS <= got[1].StartOffsetNS || got[1].EndOffsetNS <= got[2].StartOffsetNS {
		t.Fatalf("ordered overlapping records = %v", identities)
	}
}

func TestLoadRecordedCommandsMissingAndEmptyJournal(t *testing.T) {
	_, capture, _, origin := newCommandRun(t)
	executable := RecordedExecutable{Device: 1, Inode: 2, SHA256: strings.Repeat("a", 64)}
	for _, test := range []struct {
		name  string
		setup func()
	}{
		{name: "missing"},
		{name: "empty", setup: func() {
			mustMkdirCommandCapture(t, filepath.Join(capture, "duo-commands"), 0o700)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.setup != nil {
				test.setup()
			}
			got, err := LoadRecordedCommands(capture, origin, executable)
			if err != nil || got == nil || len(got) != 0 {
				t.Fatalf("empty load = %#v, %v", got, err)
			}
		})
	}
}

func TestLoadRecordedCommandsRejectsUnsafeDirectoryEntries(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*testing.T, commandCaptureFixture)
	}{
		{name: "inflight", mutate: func(t *testing.T, f commandCaptureFixture) {
			mustMkdirCommandCapture(t, filepath.Join(f.capture, "duo-commands", "invocation-99-dddddddddddddddddddddddddddddddd.inflight"), 0o700)
		}},
		{name: "journal symlink", mutate: func(t *testing.T, f commandCaptureFixture) {
			if err := os.Symlink(f.directory, filepath.Join(f.capture, "duo-commands", "invocation-99-dddddddddddddddddddddddddddddddd.complete")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "unknown", mutate: func(t *testing.T, f commandCaptureFixture) {
			mustWriteCommandCapture(t, filepath.Join(f.capture, "duo-commands", "README"), []byte("no"), 0o600)
		}},
		{name: "non-directory", mutate: func(t *testing.T, f commandCaptureFixture) {
			mustWriteCommandCapture(t, filepath.Join(f.capture, "duo-commands", "invocation-99-dddddddddddddddddddddddddddddddd.complete"), []byte("no"), 0o600)
		}},
		{name: "malformed name", mutate: func(t *testing.T, f commandCaptureFixture) {
			mustMkdirCommandCapture(t, filepath.Join(f.capture, "duo-commands", "invocation-099-dddddddddddddddddddddddddddddddd.complete"), 0o700)
		}},
		{name: "missing file", mutate: func(t *testing.T, f commandCaptureFixture) {
			if err := os.Remove(filepath.Join(f.directory, "started.json")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "extra file", mutate: func(t *testing.T, f commandCaptureFixture) {
			mustWriteCommandCapture(t, filepath.Join(f.directory, "environment.json"), []byte("{}\n"), 0o600)
		}},
		{name: "duplicate file copy", mutate: func(t *testing.T, f commandCaptureFixture) {
			data, err := os.ReadFile(filepath.Join(f.directory, "metadata.json"))
			if err != nil {
				t.Fatal(err)
			}
			mustWriteCommandCapture(t, filepath.Join(f.directory, "metadata-copy.json"), data, 0o600)
		}},
		{name: "file symlink", mutate: func(t *testing.T, f commandCaptureFixture) {
			if err := os.Remove(filepath.Join(f.directory, "argv.json")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("metadata.json", filepath.Join(f.directory, "argv.json")); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCommandCaptureFixture(t, "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 50, 90)
			test.mutate(t, fixture)
			assertCommandCaptureLoadFails(t, fixture)
		})
	}
}

func TestLoadRecordedCommandsRejectsStrictJSONViolations(t *testing.T) {
	for _, test := range []struct {
		name string
		data []byte
	}{
		{name: "unknown", data: []byte(`{"schema":"duo-portable-launcher-command-v1","child_pid":52,"child_proc_start_ticks":700,"unknown":true}`)},
		{name: "trailing", data: []byte(`{"schema":"duo-portable-launcher-command-v1","child_pid":52,"child_proc_start_ticks":700} {}`)},
		{name: "missing field", data: []byte(`{"schema":"duo-portable-launcher-command-v1","child_pid":52}`)},
		{name: "duplicate field", data: []byte(`{"schema":"duo-portable-launcher-command-v1","child_pid":52,"child_pid":52,"child_proc_start_ticks":700}`)},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCommandCaptureFixture(t, "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 50, 90)
			data := bytes.ReplaceAll(test.data, []byte{'\\', '"'}, []byte{'"'})
			mustWriteCommandCapture(t, filepath.Join(fixture.directory, "started.json"), data, 0o600)
			assertCommandCaptureLoadFails(t, fixture)
		})
	}
}

func TestLoadRecordedCommandsRejectsFactMismatches(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*commandCaptureFixture)
	}{
		{name: "metadata schema", mutate: func(f *commandCaptureFixture) { f.metadata.Schema = "other" }},
		{name: "argv schema", mutate: func(f *commandCaptureFixture) { f.argv.Schema = "other" }},
		{name: "started schema", mutate: func(f *commandCaptureFixture) { f.started.Schema = "other" }},
		{name: "complete schema", mutate: func(f *commandCaptureFixture) { f.complete.Schema = "other" }},
		{name: "clock", mutate: func(f *commandCaptureFixture) { f.complete.Clock = "CLOCK_MONOTONIC" }},
		{name: "origin", mutate: func(f *commandCaptureFixture) { f.metadata.RunOriginBootTimeNS++ }},
		{name: "negative start", mutate: func(f *commandCaptureFixture) { f.metadata.StartOffsetNS, f.complete.StartOffsetNS = -1, -1 }},
		{name: "end before start", mutate: func(f *commandCaptureFixture) { f.complete.EndOffsetNS = f.complete.StartOffsetNS - 1 }},
		{name: "metadata cwd", mutate: func(f *commandCaptureFixture) { f.complete.WorkingDirectory = "$RUN/other" }},
		{name: "unsafe cwd", mutate: func(f *commandCaptureFixture) {
			f.metadata.WorkingDirectory, f.complete.WorkingDirectory = "/host/private", "/host/private"
		}},
		{name: "recorder pid", mutate: func(f *commandCaptureFixture) { f.metadata.RecorderPID, f.complete.RecorderPID = 42, 42 }},
		{name: "child pid", mutate: func(f *commandCaptureFixture) { f.complete.ChildPID++ }},
		{name: "child ticks", mutate: func(f *commandCaptureFixture) { f.started.ChildProcStartTicks = 0 }},
		{name: "executable metadata", mutate: func(f *commandCaptureFixture) { f.complete.Executable.Inode++ }},
		{name: "executable device absent", mutate: func(f *commandCaptureFixture) {
			f.metadata.Executable.Device, f.complete.Executable.Device, f.executable.Device = 0, 0, 0
		}},
		{name: "executable pin", mutate: func(f *commandCaptureFixture) { f.executable.Inode++ }},
		{name: "uppercase digest", mutate: func(f *commandCaptureFixture) {
			f.metadata.Executable.SHA256 = strings.ToUpper(f.metadata.Executable.SHA256)
			f.complete.Executable.SHA256 = f.metadata.Executable.SHA256
		}},
		{name: "argv includes zero", mutate: func(f *commandCaptureFixture) { f.argv.ExcludesArgv0 = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCommandCaptureFixture(t, "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 50, 90)
			test.mutate(&fixture)
			writeCommandCaptureFixture(t, fixture)
			assertCommandCaptureLoadFails(t, fixture)
		})
	}
}

func TestLoadRecordedCommandsRejectsExitAndSignalInconsistency(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*commandComplete)
	}{
		{name: "negative exit", mutate: func(c *commandComplete) { c.ExitCode = -1 }},
		{name: "unsignaled name", mutate: func(c *commandComplete) { c.SignalName = "terminated" }},
		{name: "signal exit", mutate: func(c *commandComplete) { c.Signal, c.SignalName = 15, "terminated" }},
		{name: "signal name", mutate: func(c *commandComplete) { c.ExitCode, c.Signal, c.SignalName = 143, 15, "SIGTERM" }},
		{name: "signal range", mutate: func(c *commandComplete) { c.ExitCode, c.Signal, c.SignalName = 200, 72, "signal 72" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCommandCaptureFixture(t, "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 50, 90)
			test.mutate(&fixture.complete)
			writeCommandCaptureFixture(t, fixture)
			assertCommandCaptureLoadFails(t, fixture)
		})
	}
}

func TestLoadRecordedCommandsRejectsStreamTampering(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*commandCaptureFixture)
	}{
		{name: "stdout bytes", mutate: func(f *commandCaptureFixture) { f.complete.Stdout.Bytes++ }},
		{name: "stdout digest", mutate: func(f *commandCaptureFixture) { f.complete.Stdout.SHA256 = strings.Repeat("0", 64) }},
		{name: "stdout raw", mutate: func(f *commandCaptureFixture) { f.stdout = append(f.stdout, '!') }},
		{name: "stderr bytes", mutate: func(f *commandCaptureFixture) { f.complete.Stderr.Bytes-- }},
		{name: "stderr digest", mutate: func(f *commandCaptureFixture) { f.complete.Stderr.SHA256 = strings.Repeat("A", 64) }},
		{name: "stderr raw", mutate: func(f *commandCaptureFixture) { f.stderr[0] = 'X' }},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCommandCaptureFixture(t, "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 50, 90)
			test.mutate(&fixture)
			writeCommandCaptureFixture(t, fixture)
			assertCommandCaptureLoadFails(t, fixture)
		})
	}
}

func TestLoadRecordedCommandsRejectsUnsafeModes(t *testing.T) {
	for _, test := range []struct {
		name   string
		mutate func(*testing.T, commandCaptureFixture)
	}{
		{name: "commands directory", mutate: func(t *testing.T, f commandCaptureFixture) {
			mustChmodCommandCapture(t, filepath.Dir(f.directory), 0o755)
		}},
		{name: "invocation directory", mutate: func(t *testing.T, f commandCaptureFixture) { mustChmodCommandCapture(t, f.directory, 0o750) }},
		{name: "record file", mutate: func(t *testing.T, f commandCaptureFixture) {
			mustChmodCommandCapture(t, filepath.Join(f.directory, "metadata.json"), 0o644)
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCommandCaptureFixture(t, "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 50, 90)
			test.mutate(t, fixture)
			assertCommandCaptureLoadFails(t, fixture)
		})
	}
}

func TestRecordedCommandExposesNoHostAbsolutePath(t *testing.T) {
	fixture := newCommandCaptureFixture(t, "invocation-41-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", 50, 90)
	got, err := LoadRecordedCommands(fixture.capture, fixture.origin, fixture.executable)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(fixture.capture)
	if bytes.Contains(encoded, []byte(root)) || filepath.IsAbs(got[0].WorkingDirectory) {
		t.Fatalf("record exposes host path: %s", encoded)
	}
}

func newCommandCaptureFixture(t *testing.T, identity string, start, end int64) commandCaptureFixture {
	t.Helper()
	_, capture, _, origin := newCommandRun(t)
	commands := filepath.Join(capture, "duo-commands")
	mustMkdirCommandCapture(t, commands, 0o700)
	executable := RecordedExecutable{Device: 11, Inode: 12, SHA256: strings.Repeat("a", 64)}
	stdout := []byte("stdout\x00\xff")
	stderr := []byte("stderr\n")
	fixture := commandCaptureFixture{
		capture: capture, origin: origin, executable: executable,
		directory: filepath.Join(commands, identity+".complete"),
		metadata: commandMetadata{
			Schema: commandSchema, Clock: commandClock, RunOriginBootTimeNS: origin,
			StartOffsetNS: start, WorkingDirectory: "$RUN/workspace", RecorderPID: 41,
			Executable: commandExecutable(executable),
		},
		argv:    commandArgv{Schema: commandSchema, ExcludesArgv0: true, Arguments: []string{"session", "launch", "arg with spaces"}},
		started: commandStarted{Schema: commandSchema, ChildPID: 52, ChildProcStartTicks: 700},
		stdout:  stdout, stderr: stderr,
	}
	fixture.complete = commandComplete{
		Schema: commandSchema, Clock: commandClock, RunOriginBootTimeNS: origin,
		StartOffsetNS: start, EndOffsetNS: end, WorkingDirectory: fixture.metadata.WorkingDirectory,
		RecorderPID: 41, ChildPID: fixture.started.ChildPID, ChildProcStartTicks: fixture.started.ChildProcStartTicks,
		Executable: fixture.metadata.Executable, ExitCode: 17,
		Stdout: commandStreamResult{Bytes: int64(len(stdout)), SHA256: Digest(stdout)[len("sha256:"):]},
		Stderr: commandStreamResult{Bytes: int64(len(stderr)), SHA256: Digest(stderr)[len("sha256:"):]},
	}
	writeCommandCaptureFixture(t, fixture)
	return fixture
}

func writeCommandCaptureFixture(t *testing.T, fixture commandCaptureFixture) {
	t.Helper()
	if _, err := os.Stat(fixture.directory); err == nil {
		if err := os.RemoveAll(fixture.directory); err != nil {
			t.Fatal(err)
		}
	}
	mustMkdirCommandCapture(t, fixture.directory, 0o700)
	for name, value := range map[string]any{
		"metadata.json": fixture.metadata,
		"argv.json":     fixture.argv,
		"started.json":  fixture.started,
		"complete.json": fixture.complete,
	} {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		mustWriteCommandCapture(t, filepath.Join(fixture.directory, name), append(data, '\n'), 0o600)
	}
	mustWriteCommandCapture(t, filepath.Join(fixture.directory, "stdout.raw"), fixture.stdout, 0o600)
	mustWriteCommandCapture(t, filepath.Join(fixture.directory, "stderr.raw"), fixture.stderr, 0o600)
}

func assertCommandCaptureLoadFails(t *testing.T, fixture commandCaptureFixture) {
	t.Helper()
	if got, err := LoadRecordedCommands(fixture.capture, fixture.origin, fixture.executable); err == nil {
		t.Fatalf("LoadRecordedCommands unexpectedly succeeded: %#v", got)
	}
}

func mustMkdirCommandCapture(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Mkdir(path, mode); err != nil {
		t.Fatal(err)
	}
	mustChmodCommandCapture(t, path, mode)
}

func mustWriteCommandCapture(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
	mustChmodCommandCapture(t, path, mode)
}

func mustChmodCommandCapture(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
