// Package doctor implements `duo doctor`'s core diagnostics: authority-store
// health (schema version, writer-lease state) and the registered-adapters
// inventory. It is deliberately narrow — Step 10's "core" scope — and does
// not yet cover generated-artifact drift, harness trust, or live socket
// checks (duo-vnext-installation-contract.md and duo-vnext-external-
// surfaces.md describe the full eventual `duo doctor`; docs/doctor/
// decisions.md records what this package intentionally leaves out and why).
//
// This package reads internal/store's public API only (Open, OpenAuthority,
// the lease error types) and never touches internal/host or
// internal/runtime — those own real adapter registration, which does not
// exist yet, so AdaptersStatus is always empty.
package doctor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/procrastivity/duo/internal/store"
)

// Report is duo doctor's core diagnostic report.
type Report struct {
	Store    StoreStatus    `json:"store"`
	Adapters AdaptersStatus `json:"adapters"`
}

// StoreStatus is the authority store's read-only health snapshot.
type StoreStatus struct {
	// Path is the authority store file doctor probed.
	Path string `json:"path"`
	// Present reports whether a store file exists at Path. A missing store
	// is not an error — it means duo has never run an authority write —
	// and doctor never creates one just by checking (docs/doctor/
	// decisions.md).
	Present bool `json:"present"`
	// Healthy reports whether every check that ran succeeded. It is true
	// for a missing store (nothing to check) and false only when Error is
	// set.
	Healthy bool `json:"healthy"`
	// SchemaVersion is the store's applied migration version, set only
	// when Present.
	SchemaVersion int `json:"schemaVersion,omitempty"`
	// Writer is the writer-lease probe result, set only when Present and
	// the probe itself did not error.
	Writer *WriterStatus `json:"writer,omitempty"`
	// Error is the safe message of whatever check failed, if any.
	Error string `json:"error,omitempty"`
}

// WriterStatus is the writer-lease probe's result: whether another process
// currently holds the authority-writer lease, per internal/store's lease
// rules (docs/store/decisions.md).
type WriterStatus struct {
	Active      bool   `json:"active"`
	Incarnation string `json:"incarnation,omitempty"`
	PID         int    `json:"pid,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	ExpiresAt   string `json:"expiresAt,omitempty"`
}

// AdaptersStatus is the registered-adapters section: shaped for later
// registration, always empty today (no session-host or agent-runtime
// adapter registers itself yet).
type AdaptersStatus struct {
	Registered []Adapter `json:"registered"`
}

// Adapter describes one registered session-host or agent-runtime adapter.
// Nothing constructs one yet; the shape exists so wiring a real registry
// later is additive to this JSON output, not a breaking rename.
type Adapter struct {
	Name              string `json:"name"`
	Kind              string `json:"kind"` // "session_host" | "agent_runtime"
	Version           string `json:"version,omitempty"`
	ConformanceDigest string `json:"conformanceDigest,omitempty"`
	Status            string `json:"status,omitempty"`
}

// DefaultStorePath resolves the authority store's default path:
// $XDG_DATA_HOME/duo/duo.db, falling back to ~/.local/share/duo/duo.db when
// XDG_DATA_HOME is unset — the XDG base-directory convention, mirroring
// internal/asset's XDG_CONFIG_HOME handling for config. No document in the
// planning set normatively fixes this path yet; docs/doctor/decisions.md
// records the call.
func DefaultStorePath() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("doctor: resolving XDG_DATA_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "duo", "duo.db"), nil
}

// Run performs duo doctor's core checks against the store at path.
func Run(path string) Report {
	return Report{
		Store:    probeStore(path),
		Adapters: AdaptersStatus{Registered: []Adapter{}},
	}
}

// probeStore reports path's health without ever creating a store file that
// was not already there. See docs/doctor/decisions.md for why the writer-
// lease probe (a transient OpenAuthority + immediate Close when no writer
// holds it) still counts as a read-only check.
func probeStore(path string) StoreStatus {
	status := StoreStatus{Path: path}

	info, statErr := os.Stat(path)
	switch {
	case os.IsNotExist(statErr):
		status.Healthy = true
		return status
	case statErr != nil:
		status.Error = statErr.Error()
		return status
	case info.IsDir():
		status.Error = fmt.Sprintf("doctor: %s is a directory, not a database file", path)
		return status
	}
	status.Present = true

	ro, err := store.Open(path)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.SchemaVersion = ro.Version()
	closeErr := ro.Close()

	writer, writerErr := probeWriter(path)
	status.Writer = writer

	switch {
	case closeErr != nil:
		status.Error = closeErr.Error()
	case writerErr != nil:
		status.Error = writerErr.Error()
	default:
		status.Healthy = true
	}
	return status
}

// probeWriter reports whether another process holds the authority-writer
// lease. When nothing holds it, OpenAuthority itself briefly becomes the
// writer to observe that fact — this handle is closed immediately, which
// deletes the just-minted lease row again (Store.Close's documented
// behavior), leaving the lease exactly as unclaimed as it was found.
func probeWriter(path string) (*WriterStatus, error) {
	authority, err := store.OpenAuthority(path)

	var active *store.WriterActiveError
	if errors.As(err, &active) {
		return &WriterStatus{
			Active:      true,
			Incarnation: active.Incarnation,
			PID:         active.PID,
			Hostname:    active.Hostname,
			ExpiresAt:   active.ExpiresAt,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	closeErr := authority.Close()
	return &WriterStatus{Active: false}, closeErr
}
