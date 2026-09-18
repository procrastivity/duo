//go:build linux

package portablelauncher

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"
)

const commandClock = "CLOCK_BOOTTIME"

var commandCaptureFiles = []string{
	"argv.json",
	"complete.json",
	"metadata.json",
	"started.json",
	"stderr.raw",
	"stdout.raw",
}

// RecordedExecutable identifies the exact Duo executable used by a recorded
// invocation. SHA256 is the unprefixed, lowercase digest recorded by the
// command journal.
type RecordedExecutable struct {
	Device uint64
	Inode  uint64
	SHA256 string
}

func recordedExecutableForPath(path string) (RecordedExecutable, error) {
	executable, err := identifyCommandExecutable(path)
	if err != nil {
		return RecordedExecutable{}, err
	}
	return RecordedExecutable(executable), nil
}

// RecordedCommand is one validated raw command fact. It deliberately carries
// no scenario, stage, case, assertion, verdict, environment, or host path.
type RecordedCommand struct {
	InvocationID        string
	Arguments           []string
	Stdout              []byte
	Stderr              []byte
	WorkingDirectory    string
	StartOffsetNS       int64
	EndOffsetNS         int64
	RecorderPID         int
	ChildPID            int
	ChildProcStartTicks uint64
	Executable          RecordedExecutable
	ExitCode            int
	Signal              int
	SignalName          string
}

// LoadRecordedCommands validates and loads completed command journals from
// the exact capture directory. expectedExecutable must contain the pinned
// device, inode, and unprefixed lowercase SHA-256 identity. Results are sorted
// by start offset, then invocation identity; overlapping intervals are kept.
func LoadRecordedCommands(captureDirectory string, expectedRunOriginBootTimeNS int64, expectedExecutable RecordedExecutable) ([]RecordedCommand, error) {
	if expectedRunOriginBootTimeNS <= 0 {
		return nil, fmt.Errorf("expected command run origin must be positive")
	}
	if expectedExecutable.Device == 0 || expectedExecutable.Inode == 0 || !validCommandDigest(expectedExecutable.SHA256) {
		return nil, fmt.Errorf("expected Duo executable identity is invalid")
	}
	captureDirectory, _, err := validateCommandCaptureDirectory(captureDirectory)
	if err != nil {
		return nil, err
	}

	capture, err := openCommandCaptureDirectory(captureDirectory)
	if err != nil {
		return nil, err
	}
	defer func() { _ = capture.Close() }()
	commands, missing, err := openCommandDirectoryAt(capture, "duo-commands")
	if err != nil {
		return nil, err
	}
	if missing {
		return []RecordedCommand{}, nil
	}
	defer func() { _ = commands.Close() }()
	if err := requireCommandDirectoryMode(commands, "command records directory"); err != nil {
		return nil, err
	}

	entries, err := commands.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("enumerate command records: %w", err)
	}
	records := make([]RecordedCommand, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasSuffix(name, ".inflight") {
			if entry.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("command records contain a symlink")
			}
			inflight, _, openErr := openCommandDirectoryAt(commands, name)
			if openErr == nil {
				_ = inflight.Close()
				return nil, fmt.Errorf("command capture is incomplete")
			}
			return nil, fmt.Errorf("inflight command entry is not a safe directory")
		}
		invocationID, recorderPID, ok := parseCompletedInvocationName(name)
		if !ok {
			return nil, fmt.Errorf("unknown or malformed command record entry %q", name)
		}
		if _, duplicate := seen[invocationID]; duplicate {
			return nil, fmt.Errorf("duplicate command invocation identity %q", invocationID)
		}
		seen[invocationID] = struct{}{}
		invocation, missing, openErr := openCommandDirectoryAt(commands, name)
		if openErr != nil || missing {
			return nil, fmt.Errorf("completed command entry is not a safe directory")
		}
		if modeErr := requireCommandDirectoryMode(invocation, "completed command directory"); modeErr != nil {
			_ = invocation.Close()
			return nil, modeErr
		}
		record, loadErr := loadRecordedCommand(invocation, invocationID, recorderPID, expectedRunOriginBootTimeNS, expectedExecutable)
		closeErr := invocation.Close()
		if loadErr != nil {
			return nil, fmt.Errorf("command %q: %w", invocationID, loadErr)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close completed command directory: %w", closeErr)
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].StartOffsetNS != records[j].StartOffsetNS {
			return records[i].StartOffsetNS < records[j].StartOffsetNS
		}
		return records[i].InvocationID < records[j].InvocationID
	})
	return records, nil
}

func openCommandCaptureDirectory(captureDirectory string) (*os.File, error) {
	fd, err := unix.Open(captureDirectory, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open validated command capture directory: %w", err)
	}
	return os.NewFile(uintptr(fd), "command-capture"), nil
}

func openCommandDirectoryAt(parent *os.File, name string) (*os.File, bool, error) {
	fd, err := unix.Openat(int(parent.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_DIRECTORY|unix.O_NOFOLLOW, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	return os.NewFile(uintptr(fd), name), false, nil
}

func requireCommandDirectoryMode(directory *os.File, label string) error {
	info, err := directory.Stat()
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0o700 {
		return fmt.Errorf("%s must be a mode-0700 directory", label)
	}
	return nil
}

func parseCompletedInvocationName(name string) (string, int, bool) {
	if !strings.HasSuffix(name, ".complete") {
		return "", 0, false
	}
	identity := strings.TrimSuffix(name, ".complete")
	parts := strings.Split(identity, "-")
	if len(parts) != 3 || parts[0] != "invocation" || parts[1] == "" || parts[2] == "" {
		return "", 0, false
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil || pid <= 0 || strconv.Itoa(pid) != parts[1] || len(parts[2]) != 32 || !lowerHex(parts[2]) {
		return "", 0, false
	}
	return identity, pid, true
}

func loadRecordedCommand(invocation *os.File, invocationID string, nameRecorderPID int, expectedOrigin int64, expectedExecutable RecordedExecutable) (RecordedCommand, error) {
	entries, err := invocation.ReadDir(-1)
	if err != nil {
		return RecordedCommand{}, fmt.Errorf("enumerate invocation files: %w", err)
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !containsCommandCaptureFile(name) {
			return RecordedCommand{}, fmt.Errorf("unexpected invocation file %q", name)
		}
		if _, duplicate := seen[name]; duplicate {
			return RecordedCommand{}, fmt.Errorf("duplicate invocation file %q", name)
		}
		seen[name] = struct{}{}
	}
	for _, name := range commandCaptureFiles {
		if _, ok := seen[name]; !ok {
			return RecordedCommand{}, fmt.Errorf("missing invocation file %q", name)
		}
	}

	metadataBytes, err := readCommandFileAt(invocation, "metadata.json")
	if err != nil {
		return RecordedCommand{}, err
	}
	argvBytes, err := readCommandFileAt(invocation, "argv.json")
	if err != nil {
		return RecordedCommand{}, err
	}
	startedBytes, err := readCommandFileAt(invocation, "started.json")
	if err != nil {
		return RecordedCommand{}, err
	}
	completeBytes, err := readCommandFileAt(invocation, "complete.json")
	if err != nil {
		return RecordedCommand{}, err
	}
	stdout, err := readCommandFileAt(invocation, "stdout.raw")
	if err != nil {
		return RecordedCommand{}, err
	}
	stderr, err := readCommandFileAt(invocation, "stderr.raw")
	if err != nil {
		return RecordedCommand{}, err
	}

	var metadata commandMetadata
	if err := decodeCommandJSON(metadataBytes, &metadata,
		[]string{"schema", "clock", "run_origin_boottime_ns", "start_offset_ns", "working_directory", "recorder_pid", "executable"},
		map[string][]string{"executable": {"device", "inode", "sha256"}}); err != nil {
		return RecordedCommand{}, fmt.Errorf("decode metadata.json: %w", err)
	}
	var argv commandArgv
	if err := decodeCommandJSON(argvBytes, &argv,
		[]string{"schema", "excludes_argv0", "arguments"}, nil); err != nil {
		return RecordedCommand{}, fmt.Errorf("decode argv.json: %w", err)
	}
	var started commandStarted
	if err := decodeCommandJSON(startedBytes, &started,
		[]string{"schema", "child_pid", "child_proc_start_ticks"}, nil); err != nil {
		return RecordedCommand{}, fmt.Errorf("decode started.json: %w", err)
	}
	var complete commandComplete
	if err := decodeCommandJSON(completeBytes, &complete,
		[]string{"schema", "clock", "run_origin_boottime_ns", "start_offset_ns", "end_offset_ns", "working_directory", "recorder_pid", "child_pid", "child_proc_start_ticks", "executable", "exit_code", "signal", "signal_name", "stdout", "stderr"},
		map[string][]string{
			"executable": {"device", "inode", "sha256"},
			"stdout":     {"bytes", "sha256"},
			"stderr":     {"bytes", "sha256"},
		}); err != nil {
		return RecordedCommand{}, fmt.Errorf("decode complete.json: %w", err)
	}

	if metadata.Schema != commandSchema || argv.Schema != commandSchema || started.Schema != commandSchema || complete.Schema != commandSchema {
		return RecordedCommand{}, fmt.Errorf("command journal schema mismatch")
	}
	if metadata.Clock != commandClock || complete.Clock != commandClock {
		return RecordedCommand{}, fmt.Errorf("command journal clock mismatch")
	}
	if metadata.RunOriginBootTimeNS != expectedOrigin || complete.RunOriginBootTimeNS != expectedOrigin {
		return RecordedCommand{}, fmt.Errorf("command journal run origin mismatch")
	}
	if metadata.StartOffsetNS < 0 || complete.StartOffsetNS < 0 || complete.EndOffsetNS < 0 || complete.EndOffsetNS < metadata.StartOffsetNS {
		return RecordedCommand{}, fmt.Errorf("command journal monotonic offsets are invalid")
	}
	if metadata.StartOffsetNS != complete.StartOffsetNS || metadata.WorkingDirectory != complete.WorkingDirectory || metadata.RecorderPID != complete.RecorderPID || metadata.Executable != complete.Executable {
		return RecordedCommand{}, fmt.Errorf("metadata and completion facts disagree")
	}
	if !validCommandWorkingDirectory(metadata.WorkingDirectory) {
		return RecordedCommand{}, fmt.Errorf("working directory is not a safe run token")
	}
	if metadata.RecorderPID <= 0 || metadata.RecorderPID != nameRecorderPID {
		return RecordedCommand{}, fmt.Errorf("recorder process identity mismatch")
	}
	if started.ChildPID <= 0 || started.ChildPID == metadata.RecorderPID || started.ChildProcStartTicks == 0 || complete.ChildPID != started.ChildPID || complete.ChildProcStartTicks != started.ChildProcStartTicks {
		return RecordedCommand{}, fmt.Errorf("child process identity mismatch")
	}
	journalExecutable := RecordedExecutable{Device: metadata.Executable.Device, Inode: metadata.Executable.Inode, SHA256: metadata.Executable.SHA256}
	if journalExecutable.Device == 0 || journalExecutable.Inode == 0 || !validCommandDigest(journalExecutable.SHA256) || journalExecutable != expectedExecutable {
		return RecordedCommand{}, fmt.Errorf("duo executable identity mismatch")
	}
	if !argv.ExcludesArgv0 {
		return RecordedCommand{}, fmt.Errorf("argv record must exclude argv[0]")
	}
	if err := validateCommandExitStatus(complete); err != nil {
		return RecordedCommand{}, err
	}
	if err := validateCommandStream("stdout", stdout, complete.Stdout); err != nil {
		return RecordedCommand{}, err
	}
	if err := validateCommandStream("stderr", stderr, complete.Stderr); err != nil {
		return RecordedCommand{}, err
	}

	return RecordedCommand{
		InvocationID: invocationID, Arguments: append([]string(nil), argv.Arguments...),
		Stdout: append([]byte(nil), stdout...), Stderr: append([]byte(nil), stderr...),
		WorkingDirectory: metadata.WorkingDirectory,
		StartOffsetNS:    metadata.StartOffsetNS, EndOffsetNS: complete.EndOffsetNS,
		RecorderPID: metadata.RecorderPID, ChildPID: started.ChildPID, ChildProcStartTicks: started.ChildProcStartTicks,
		Executable: journalExecutable, ExitCode: complete.ExitCode, Signal: complete.Signal, SignalName: complete.SignalName,
	}, nil
}

func containsCommandCaptureFile(name string) bool {
	index := sort.SearchStrings(commandCaptureFiles, name)
	return index < len(commandCaptureFiles) && commandCaptureFiles[index] == name
}

func readCommandFileAt(directory *os.File, name string) ([]byte, error) {
	fd, err := unix.Openat(int(directory.Fd()), name, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fmt.Errorf("open invocation file %q: %w", name, err)
	}
	file := os.NewFile(uintptr(fd), name)
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		_ = file.Close()
		return nil, fmt.Errorf("invocation file %q must be a mode-0600 regular file", name)
	}
	data, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, fmt.Errorf("read invocation file %q: %w", name, err)
	}
	return data, nil
}

func decodeCommandJSON(data []byte, target any, required []string, nested map[string][]string) error {
	if err := rejectDuplicateJSONFields(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return fmt.Errorf("trailing JSON: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return fmt.Errorf("JSON record must be an object") //nolint:staticcheck // JSON is the proper initialism.
	}
	if err := requireCommandJSONFields(object, required); err != nil {
		return err
	}
	for field, fields := range nested {
		var child map[string]json.RawMessage
		if err := json.Unmarshal(object[field], &child); err != nil || child == nil {
			return fmt.Errorf("field %q must be an object", field)
		}
		if err := requireCommandJSONFields(child, fields); err != nil {
			return fmt.Errorf("field %q: %w", field, err)
		}
	}
	return nil
}

func requireCommandJSONFields(object map[string]json.RawMessage, required []string) error {
	for _, field := range required {
		if _, ok := object[field]; !ok {
			return fmt.Errorf("missing required field %q", field)
		}
	}
	return nil
}

func rejectDuplicateJSONFields(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := walkCommandJSONValue(decoder); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON")
	}
	return nil
}

func walkCommandJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string") //nolint:staticcheck // JSON is the proper initialism.
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := walkCommandJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	case '[':
		for decoder.More() {
			if err := walkCommandJSONValue(decoder); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	default:
		return fmt.Errorf("unexpected JSON delimiter")
	}
}

func validCommandWorkingDirectory(value string) bool {
	if value == "$RUN" {
		return true
	}
	if !strings.HasPrefix(value, "$RUN/") || strings.Contains(value, "\\") || strings.ContainsRune(value, '\x00') {
		return false
	}
	relative := strings.TrimPrefix(value, "$RUN/")
	return relative != "" && path.Clean(relative) == relative && !path.IsAbs(relative) && relative != ".." && !strings.HasPrefix(relative, "../")
}

func validateCommandExitStatus(complete commandComplete) error {
	if complete.ExitCode < 0 || complete.ExitCode > 255 {
		return fmt.Errorf("command exit code is invalid")
	}
	if complete.Signal == 0 {
		if complete.SignalName != "" {
			return fmt.Errorf("unsignaled command has a signal name")
		}
		return nil
	}
	if complete.Signal < 1 || complete.Signal > 64 || complete.ExitCode != 128+complete.Signal || complete.SignalName != syscall.Signal(complete.Signal).String() {
		return fmt.Errorf("command signal status is inconsistent")
	}
	return nil
}

func validateCommandStream(name string, data []byte, result commandStreamResult) error {
	if result.Bytes < 0 || result.Bytes != int64(len(data)) || !validCommandDigest(result.SHA256) {
		return fmt.Errorf("%s byte count or digest is invalid", name)
	}
	sum := sha256.Sum256(data)
	if result.SHA256 != hex.EncodeToString(sum[:]) {
		return fmt.Errorf("%s digest mismatch", name)
	}
	return nil
}

func validCommandDigest(value string) bool {
	return len(value) == sha256.Size*2 && lowerHex(value)
}

func lowerHex(value string) bool {
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}
