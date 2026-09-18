package portablelauncher

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	// linuxProcessBirthLayout is the millisecond UTC representation carried by
	// ProcessIdentity.StartTime and by launch/attachment evidence.
	linuxProcessBirthLayout = "2006-01-02T15:04:05.000Z"

	// Linux stop delivery is asynchronous. Two seconds is deliberately much
	// longer than an ordinary scheduler transition while still bounding a
	// failed induction well below the suite's observation deadlines.
	linuxStopObservationTimeout = 2 * time.Second
	linuxStopPollInterval       = 20 * time.Millisecond

	// Linux exposes process start time in USER_HZ units. USER_HZ is 100 on
	// every Linux architecture supported by Go, matching the Herdr adapter's
	// documented procfs birth-evidence assumption.
	linuxClockTicksPerSecond = int64(100)
)

// LinuxProcessControl owns pidfds for exact conformance-fixture process
// identities. It never degrades to PID-only signaling.
type LinuxProcessControl struct {
	mu sync.RWMutex

	pidfds       map[ProcessIdentity]int
	closed       bool
	stopTimeout  time.Duration
	pollInterval time.Duration
}

// NewLinuxProcessControl constructs an empty pidfd-backed process control.
func NewLinuxProcessControl() *LinuxProcessControl {
	return newLinuxProcessControl(linuxStopObservationTimeout, linuxStopPollInterval)
}

func newLinuxProcessControl(stopTimeout, pollInterval time.Duration) *LinuxProcessControl {
	return &LinuxProcessControl{
		pidfds:       make(map[ProcessIdentity]int),
		stopTimeout:  stopTimeout,
		pollInterval: pollInterval,
	}
}

// AcquireExactTarget opens a pidfd before re-reading procfs birth evidence.
// That order closes the PID-reuse window between evidence capture and pidfd
// acquisition. Every failure after PidfdOpen closes the new descriptor.
func (c *LinuxProcessControl) AcquireExactTarget(ctx context.Context, target ProcessIdentity) error {
	if err := validateLinuxProcessIdentity(target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	fd, err := unix.PidfdOpen(target.PID, 0)
	if err != nil {
		return fmt.Errorf("pidfd_open exact target %d (pidfd support is required): %w", target.PID, err)
	}
	owned := false
	defer func() {
		if !owned {
			_ = unix.Close(fd)
		}
	}()

	observation, err := readLinuxProcessObservation(target.PID)
	if err != nil {
		return fmt.Errorf("verify exact target after pidfd_open: %w", err)
	}
	if err := compareLinuxProcessBirth(target, observation); err != nil {
		return fmt.Errorf("verify exact target after pidfd_open: %w", err)
	}
	if !linuxProcessStateLive(observation.state) {
		return fmt.Errorf("verify exact target after pidfd_open: process state %q is not live", observation.state)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return fmt.Errorf("linux process control is closed")
	}
	if _, exists := c.pidfds[target]; exists {
		return fmt.Errorf("exact target is already acquired")
	}
	c.pidfds[target] = fd
	owned = true
	return nil
}

// VerifyExactTarget independently re-reads procfs and requires the complete
// identity to still select the process represented by an owned pidfd.
func (c *LinuxProcessControl) VerifyExactTarget(ctx context.Context, target ProcessIdentity) error {
	if err := validateLinuxProcessIdentity(target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	observation, err := c.verifyExactTargetLocked(target)
	if err != nil {
		return err
	}
	if !linuxProcessStateLive(observation.state) {
		return fmt.Errorf("exact target process state %q is not live", observation.state)
	}
	return ctx.Err()
}

// SendSIGSTOP sends SIGSTOP through the acquired pidfd. There is intentionally
// no kill(2), shell-command, terminal, or other PID-only fallback.
func (c *LinuxProcessControl) SendSIGSTOP(ctx context.Context, target ProcessIdentity) error {
	return c.sendOwnedPidfdSignal(ctx, target, unix.SIGSTOP, "SIGSTOP")
}

// SendSIGKILL sends SIGKILL through the acquired pidfd for exact cleanup
// escalation. It retains the pidfd so the caller continues to own observation
// and must release the target explicitly.
func (c *LinuxProcessControl) SendSIGKILL(ctx context.Context, target ProcessIdentity) error {
	return c.sendOwnedPidfdSignal(ctx, target, unix.SIGKILL, "SIGKILL")
}

func (c *LinuxProcessControl) sendOwnedPidfdSignal(ctx context.Context, target ProcessIdentity, signal unix.Signal, signalName string) error {
	if err := validateLinuxProcessIdentity(target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	c.mu.RLock()
	defer c.mu.RUnlock()
	fd, ok := c.pidfds[target]
	if !ok {
		return fmt.Errorf("exact target is not acquired")
	}
	if err := unix.PidfdSendSignal(fd, signal, nil, 0); err != nil {
		return fmt.Errorf("pidfd_send_signal %s: %w", signalName, err)
	}
	return nil
}

// VerifyStopped waits for Linux to report a stopped or tracing-stop state,
// rechecking exact process birth on every observation. It returns early for a
// missing, replaced, zombie, or dead process and honors caller cancellation.
func (c *LinuxProcessControl) VerifyStopped(ctx context.Context, target ProcessIdentity) error {
	if err := validateLinuxProcessIdentity(target); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.stopTimeout <= 0 || c.pollInterval <= 0 {
		return fmt.Errorf("linux process control has invalid stop observation bounds")
	}

	stopContext, cancel := context.WithTimeout(ctx, c.stopTimeout)
	defer cancel()
	for {
		c.mu.RLock()
		observation, err := c.verifyExactTargetLocked(target)
		c.mu.RUnlock()
		if err != nil {
			return err
		}
		switch observation.state {
		case 'T', 't':
			return nil
		case 'Z', 'X', 'x':
			return fmt.Errorf("exact target exited before stopped-state verification (state %q)", observation.state)
		}

		timer := time.NewTimer(c.pollInterval)
		select {
		case <-stopContext.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("exact target did not enter a stopped state within %s: %w", c.stopTimeout, stopContext.Err())
		case <-timer.C:
		}
	}
}

// Release closes and forgets the pidfd owned for exactly target.
func (c *LinuxProcessControl) Release(target ProcessIdentity) error {
	if err := validateLinuxProcessIdentity(target); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fd, ok := c.pidfds[target]
	if !ok {
		return fmt.Errorf("exact target is not acquired")
	}
	delete(c.pidfds, target)
	if err := unix.Close(fd); err != nil {
		return fmt.Errorf("close exact target pidfd: %w", err)
	}
	return nil
}

// Close closes every owned pidfd. It is safe to call more than once; after it
// returns, the control cannot acquire new targets.
func (c *LinuxProcessControl) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	var closeErrors []error
	for target, fd := range c.pidfds {
		delete(c.pidfds, target)
		if err := unix.Close(fd); err != nil {
			closeErrors = append(closeErrors, fmt.Errorf("close pidfd for pid %d: %w", target.PID, err))
		}
	}
	return errors.Join(closeErrors...)
}

func (c *LinuxProcessControl) verifyExactTargetLocked(target ProcessIdentity) (linuxProcessObservation, error) {
	if _, ok := c.pidfds[target]; !ok {
		return linuxProcessObservation{}, fmt.Errorf("exact target is not acquired")
	}
	observation, err := readLinuxProcessObservation(target.PID)
	if err != nil {
		return linuxProcessObservation{}, fmt.Errorf("read exact target: %w", err)
	}
	if err := compareLinuxProcessBirth(target, observation); err != nil {
		return linuxProcessObservation{}, err
	}
	return observation, nil
}

func validateLinuxProcessIdentity(target ProcessIdentity) error {
	if target.PID <= 0 {
		return fmt.Errorf("exact target requires a positive PID")
	}
	if strings.TrimSpace(target.TerminalID) == "" || strings.TrimSpace(target.AttachmentID) == "" {
		return fmt.Errorf("exact target requires a complete attachment identity")
	}
	parsed, err := time.Parse(linuxProcessBirthLayout, target.StartTime)
	if err != nil || parsed.UTC().Format(linuxProcessBirthLayout) != target.StartTime || parsed.UnixMilli() < 0 {
		return fmt.Errorf("exact target requires a canonical non-negative millisecond UTC start time")
	}
	return nil
}

type linuxProcessObservation struct {
	state     byte
	startTime string
}

func readLinuxProcessObservation(pid int) (linuxProcessObservation, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return linuxProcessObservation{}, fmt.Errorf("read /proc/%d/stat: %w", pid, err)
	}
	space := bytes.IndexByte(raw, ' ')
	if space <= 0 {
		return linuxProcessObservation{}, fmt.Errorf("/proc/%d/stat has no PID field", pid)
	}
	reportedPID, err := strconv.Atoi(string(raw[:space]))
	if err != nil || reportedPID != pid {
		return linuxProcessObservation{}, fmt.Errorf("/proc/%d/stat PID field does not match", pid)
	}
	// comm is parenthesized and can itself contain spaces or ')'. The final
	// ')' is therefore the only safe place from which to count later fields.
	end := bytes.LastIndexByte(raw, ')')
	if end < space+2 || end+2 >= len(raw) || raw[end+1] != ' ' {
		return linuxProcessObservation{}, fmt.Errorf("/proc/%d/stat has no complete comm field", pid)
	}
	fields := bytes.Fields(raw[end+2:])
	// After comm, field 3 (state) is index 0 and field 22 (starttime) is 19.
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex || len(fields[0]) != 1 {
		return linuxProcessObservation{}, fmt.Errorf("/proc/%d/stat is incomplete", pid)
	}
	if !linuxProcessStateValid(fields[0][0]) {
		return linuxProcessObservation{}, fmt.Errorf("/proc/%d/stat has invalid process state %q", pid, fields[0][0])
	}
	ticks, err := strconv.ParseInt(string(fields[startTimeIndex]), 10, 64)
	if err != nil || ticks < 0 {
		return linuxProcessObservation{}, fmt.Errorf("/proc/%d/stat has invalid starttime", pid)
	}

	bootSeconds, err := readLinuxBootTime()
	if err != nil {
		return linuxProcessObservation{}, err
	}
	wholeSeconds := ticks / linuxClockTicksPerSecond
	if wholeSeconds > math.MaxInt64-bootSeconds {
		return linuxProcessObservation{}, fmt.Errorf("/proc/%d/stat starttime overflows wall clock", pid)
	}
	nanos := (ticks % linuxClockTicksPerSecond) * (int64(time.Second) / linuxClockTicksPerSecond)
	started := time.Unix(bootSeconds+wholeSeconds, nanos).UTC()
	return linuxProcessObservation{state: fields[0][0], startTime: started.Format(linuxProcessBirthLayout)}, nil
}

func readLinuxBootTime() (int64, error) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, fmt.Errorf("read /proc/stat: %w", err)
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		fields := bytes.Fields(line)
		if len(fields) == 0 || !bytes.Equal(fields[0], []byte("btime")) {
			continue
		}
		if len(fields) != 2 {
			return 0, fmt.Errorf("/proc/stat has malformed btime")
		}
		seconds, err := strconv.ParseInt(string(fields[1]), 10, 64)
		if err != nil || seconds < 0 {
			return 0, fmt.Errorf("/proc/stat has invalid btime")
		}
		return seconds, nil
	}
	return 0, fmt.Errorf("/proc/stat has no btime")
}

func compareLinuxProcessBirth(target ProcessIdentity, observation linuxProcessObservation) error {
	if observation.startTime != target.StartTime {
		return fmt.Errorf("exact target process birth mismatch: observed %s, requested %s", observation.startTime, target.StartTime)
	}
	return nil
}

func linuxProcessStateLive(state byte) bool {
	switch state {
	case 'Z', 'X', 'x':
		return false
	default:
		return true
	}
}

func linuxProcessStateValid(state byte) bool {
	switch state {
	case 'R', 'S', 'D', 'Z', 'T', 't', 'X', 'x', 'K', 'W', 'P', 'I':
		return true
	default:
		return false
	}
}
