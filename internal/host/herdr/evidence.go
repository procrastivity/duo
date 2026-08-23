package herdr

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/procrastivity/duo/internal/host"
)

// NoServerEpoch is what this adapter puts in host.Evidence.HostServerEpoch:
// nothing. Probed and refuted at Herdr 0.8.2 — ping, `herdr status server
// --json`, and session.snapshot expose no instance ID, start time, or
// epoch, and the server restores workspace/tab/pane IDs across a restart.
// The epoch-equivalent is per-pane and lives in HostContainerID
// (terminal_id). Writing a pane-scoped value into a server-scoped field
// would make a guarantee Herdr does not offer.
const NoServerEpoch = ""

// Start-time sources this adapter can attribute a process birth to.
const (
	// StartTimeSourceProcfs is a kernel start time read from
	// /proc/<pid>/stat plus /proc/stat's btime. Only this source counts as
	// proven birth evidence.
	StartTimeSourceProcfs = "procfs"
	// StartTimeSourceUnavailable means Herdr named a PID but no start time
	// could be obtained (no procfs, a vanished process, a remote server).
	// A PID alone is not continuity evidence, so continuity verdicts read
	// this as "cannot prove", never as "same".
	StartTimeSourceUnavailable = "unavailable"
)

// ProcessBirthResolver turns Herdr's PID-only process report into birth
// evidence. It is a Config field so a non-Linux host, a remote Herdr
// server, or a test can supply its own source without this package
// pretending procfs is universal.
type ProcessBirthResolver func(ctx context.Context, info processInfo) host.ProcessBirthEvidence

// evidenceFor builds the §5.2 evidence core for one pane.
//
// The mapping, from the P7 identity probe (notes/19 §5):
//
//	IntegrationInstanceID  one Herdr server (its session name folded in)
//	HostServerEpoch        none exists — NoServerEpoch
//	HostContainerID        terminal_id, the pane's incarnation
//	PaneID                 pane_id, addressing only
//	ProcessBirth           foreground process (or pane shell) + start time
func evidenceFor(instanceID string, pane paneInfo, birth host.ProcessBirthEvidence) host.Evidence {
	return host.Evidence{
		IntegrationInstanceID: instanceID,
		HostServerEpoch:       NoServerEpoch,
		HostContainerID:       pane.TerminalID,
		ProcessBirth:          birth,
		PaneID:                pane.PaneID,
	}
}

// AttachmentFor is evidenceFor's projection: the addressing subset a
// caller stores from a launch or discovery result and later hands back as
// a HostAttachmentClaim. Keeping it here rather than leaving each caller
// to assemble it means the terminal_id-into-HostContainerID mapping is
// written down once.
func AttachmentFor(e host.Evidence) host.Attachment {
	return host.Attachment{
		IntegrationInstanceID: e.IntegrationInstanceID,
		HostServerEpoch:       e.HostServerEpoch,
		HostContainerID:       e.HostContainerID,
		PaneID:                e.PaneID,
	}
}

// pidFor picks the process a pane's birth evidence describes: the pane's
// foreground process when one is running, otherwise the pane shell. A pane
// sitting at its prompt reports the shell as its own foreground process,
// so the two usually agree; when they do not, the foreground process is
// the one a runtime occupies.
func pidFor(info processInfo) int {
	if len(info.ForegroundProcesses) > 0 {
		// Herdr lists the foreground process group; the last entry is the
		// deepest descendant, which is the process actually holding the
		// terminal.
		return info.ForegroundProcesses[len(info.ForegroundProcesses)-1].PID
	}
	return info.ShellPID
}

// birthProven reports whether b is strong enough to decide continuity. A
// PID by itself is not: PIDs are reused, and §5.2 is explicit that "a pane
// ID or PID alone cannot prove runtime continuity".
func birthProven(b host.ProcessBirthEvidence) bool {
	return b.PID > 0 && b.StartTimeSource == StartTimeSourceProcfs && !b.StartTime.IsZero()
}

// sameBirth reports whether two birth fingerprints describe the same
// process. Unproven evidence on either side is never "same" — an
// unprovable continuity claim resolves to a new runtime instance, which
// over-issues identities rather than silently merging two processes.
func sameBirth(live, claimed host.ProcessBirthEvidence) bool {
	if !birthProven(live) || !birthProven(claimed) {
		return false
	}
	return live.PID == claimed.PID && live.StartTime.Equal(claimed.StartTime)
}

// procfsBirth is the default ProcessBirthResolver: a Linux kernel start
// time for the pane's process. On any failure it returns PID-only evidence
// tagged StartTimeSourceUnavailable rather than an error, because a
// missing start time is a weaker verdict, not a broken adapter.
func procfsBirth(_ context.Context, info processInfo) host.ProcessBirthEvidence {
	pid := pidFor(info)
	if pid <= 0 {
		return host.ProcessBirthEvidence{StartTimeSource: StartTimeSourceUnavailable}
	}
	start, err := procStartTime(pid)
	if err != nil {
		return host.ProcessBirthEvidence{PID: pid, StartTimeSource: StartTimeSourceUnavailable}
	}
	return host.ProcessBirthEvidence{PID: pid, StartTime: start, StartTimeSource: StartTimeSourceProcfs}
}

// clockTicksPerSecond is the kernel's USER_HZ, which is 100 on every Linux
// port Go builds for. The value only has to be consistent between two
// readings of the same host for the fingerprint to work.
const clockTicksPerSecond = 100

// procStartTime reads /proc/<pid>/stat field 22 (start time in clock ticks
// since boot) and turns it into a wall-clock instant using /proc/stat's
// btime.
func procStartTime(pid int) (time.Time, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return time.Time{}, fmt.Errorf("read proc stat: %w", err)
	}
	// The comm field is parenthesized and may contain spaces or ')', so
	// fields are counted from after the last ')'.
	end := bytes.LastIndexByte(raw, ')')
	if end < 0 || end+2 >= len(raw) {
		return time.Time{}, fmt.Errorf("proc stat for pid %d: no comm field", pid)
	}
	fields := bytes.Fields(raw[end+2:])
	// After comm, field 3 (state) is index 0, so field 22 is index 19.
	const startTimeIndex = 19
	if len(fields) <= startTimeIndex {
		return time.Time{}, fmt.Errorf("proc stat for pid %d: %d fields", pid, len(fields))
	}
	ticks, err := strconv.ParseInt(string(fields[startTimeIndex]), 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("proc stat for pid %d: start time: %w", pid, err)
	}
	boot, err := bootTime()
	if err != nil {
		return time.Time{}, err
	}
	nanos := (ticks % clockTicksPerSecond) * (int64(time.Second) / clockTicksPerSecond)
	return boot.Add(time.Duration(ticks/clockTicksPerSecond)*time.Second + time.Duration(nanos)).UTC(), nil
}

func bootTime() (time.Time, error) {
	raw, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}, fmt.Errorf("read proc stat: %w", err)
	}
	for _, line := range bytes.Split(raw, []byte("\n")) {
		if !bytes.HasPrefix(line, []byte("btime ")) {
			continue
		}
		secs, err := strconv.ParseInt(string(bytes.TrimSpace(line[len("btime "):])), 10, 64)
		if err != nil {
			return time.Time{}, fmt.Errorf("proc stat btime: %w", err)
		}
		return time.Unix(secs, 0).UTC(), nil
	}
	return time.Time{}, fmt.Errorf("proc stat: no btime line")
}
