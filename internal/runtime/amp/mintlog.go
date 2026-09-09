package amp

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MintLogPath is the Duo-owned tee target for one Amp mint leaf:
// $XDG_DATA_HOME/duo/amp-mint/<launch-resolution-id>/<leaf>.jsonl,
// falling back to ~/.local/share/duo/amp-mint/... when XDG_DATA_HOME is
// unset. The path is a locator, not a directory-newest scan (I-6),
// mirroring devin.ATIFPath's discipline exactly.
//
// The mint wrapper script MaterializeMintScript writes tees `amp -x`'s
// stream-JSON output to this path (docs/cli/decisions.md, 2026-09-09,
// "Amp mint delivery needs a Duo-materialized wrapper script"). The
// mint-exit recovery leg reads it back at this same computed path once
// host continuity evidence proves the mint process exited.
func MintLogPath(launchResolutionID, leaf string) (string, error) {
	if launchResolutionID == "" {
		return "", fmt.Errorf("amp: mint log path needs a launch-resolution ID")
	}
	if leaf == "" {
		return "", fmt.Errorf("amp: mint log path needs a leaf")
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("amp: resolving XDG_DATA_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "duo", "amp-mint", launchResolutionID, leaf+".jsonl"), nil
}

// ErrMintIncomplete is the condition-reason sentinel ThreadIDFromMintLog
// returns when a mint log names a thread but never closed with a
// successful result record. A later step's mint-exit recovery leg
// distinguishes this, via errors.Is, from the honest "no thread minted
// yet" miss.
var ErrMintIncomplete = errors.New("amp: mint log names a thread but has no successful result record")

// mintLogLine is the narrow slice of Amp's stream-JSON shape
// ThreadIDFromMintLog needs. The mint log is tee'd stream-JSON: one JSON
// object per line (JSONL), not one document. Every stream line carries
// session_id — despite the name, this is the Amp thread id, not a
// per-turn session — and the line that closes a successful mint carries
// type "result" with subtype "success".
type mintLogLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype"`
	SessionID string `json:"session_id"`
}

// ThreadIDFromMintLog reads the Amp thread id a mint log recorded, after
// the process that wrote it is gone. It never trusts a thread id from a
// mint that did not visibly finish: a session_id line proves Amp opened
// a thread, but only a later "result"/"success" line proves the mint
// prompt was answered rather than left hanging mid-turn.
//
// A missing mint log file or a log with no session_id line at all is an
// honest miss, not an error to act on — the mint may not have started
// writing yet. ThreadIDFromMintLog returns ("", nil) for both, mirroring
// devin.SessionIDFromExport's honest-miss error shape (errors name what
// is missing, no guessing). A log that names a thread but never reaches
// a successful result line is a different, more specific condition — the
// mint started and may still be running — so that case returns a
// distinct error wrapping ErrMintIncomplete instead of an honest miss.
// Any other read or parse failure is returned as an error.
func ThreadIDFromMintLog(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("amp: opening mint log %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	var threadID string
	var success bool
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec mintLogLine
		if err := json.Unmarshal(line, &rec); err != nil {
			return "", fmt.Errorf("amp: reading mint log %s: %w", path, err)
		}
		if rec.SessionID != "" && threadID == "" {
			threadID = rec.SessionID
		}
		if rec.Type == "result" && rec.Subtype == "success" {
			success = true
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("amp: reading mint log %s: %w", path, err)
	}

	if threadID == "" {
		return "", nil
	}
	if !success {
		return "", fmt.Errorf("%w: %s", ErrMintIncomplete, path)
	}
	return threadID, nil
}
