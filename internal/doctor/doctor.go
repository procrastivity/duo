// Package doctor implements `duo doctor` diagnostics: authority-store health,
// registered adapters, read-only harness inspection, and the stable portable
// launcher-preflight report shape.
//
// Store diagnosis uses OpenReadOnly and direct lease inspection; it never
// creates/migrates a database or acquires a writer lease. Adapter rows and
// host/projection evidence arrive from the composition root, keeping this
// package neutral about concrete host and runtime adapters.
package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/procrastivity/duo/internal/adapter"
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

// AdaptersStatus is the registered-adapters section: the adapter factories
// the composition root registered, as reported to doctor's caller.
type AdaptersStatus struct {
	Registered []Adapter `json:"registered"`
}

// Adapter describes one registered session-host or agent-runtime adapter.
type Adapter struct {
	Name                      string   `json:"name"`
	Kind                      string   `json:"kind"` // "session_host" | "agent_runtime"
	Version                   string   `json:"version,omitempty"`
	ConformanceDigest         string   `json:"conformanceDigest,omitempty"`
	Status                    string   `json:"status,omitempty"`
	SupportedExternalVersions []string `json:"supportedExternalVersions,omitempty"`
	DetectedExternalVersion   string   `json:"detectedExternalVersion"`
	PinnedExternalVersion     string   `json:"pinnedExternalVersion,omitempty"`
}

// FromDescriptor maps one §5.1 adapter descriptor plus its probe's
// compatibility verdict into doctor's report row. The Role vocabulary
// ("host"/"runtime") widens to the report's kind vocabulary
// ("session_host"/"agent_runtime"); an unknown role passes through verbatim
// rather than being guessed.
func FromDescriptor(d adapter.Descriptor, compatibility adapter.CompatibilityState) Adapter {
	kind := string(d.Role)
	switch d.Role {
	case adapter.RoleHost:
		kind = "session_host"
	case adapter.RoleRuntime:
		kind = "agent_runtime"
	}
	return Adapter{
		Name:                      d.AdapterID,
		Kind:                      kind,
		Version:                   d.BuildVersion,
		ConformanceDigest:         d.ConformanceRecordDigest,
		Status:                    string(compatibility),
		SupportedExternalVersions: append([]string(nil), d.SupportedExternalVersions...),
	}
}

// FromProbe adds the external-version evidence a composition root gathered
// for one descriptor. pinned is supplied separately because a descriptor may
// support more than one external version while the build still pins one
// version for its evidence.
func FromProbe(d adapter.Descriptor, p adapter.Probe, pinned string) Adapter {
	report := FromDescriptor(d, p.Compatibility)
	report.DetectedExternalVersion = p.DetectedVersion
	report.PinnedExternalVersion = pinned
	return report
}

// xdgDataHome resolves the XDG data root: $XDG_DATA_HOME, falling back to
// ~/.local/share when unset. Shared by DefaultStorePath and
// DefaultHarnessRoot so the authority store and the generated harness tree
// stay under one duo data directory.
func xdgDataHome() (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("doctor: resolving XDG_DATA_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return base, nil
}

// DefaultStorePath resolves the authority store's default path:
// $XDG_DATA_HOME/duo/duo.db, falling back to ~/.local/share/duo/duo.db when
// XDG_DATA_HOME is unset — the XDG base-directory convention, mirroring
// internal/asset's XDG_CONFIG_HOME handling for config. No document in the
// planning set normatively fixes this path yet; docs/doctor/decisions.md
// records the call.
func DefaultStorePath() (string, error) {
	base, err := xdgDataHome()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "duo", "duo.db"), nil
}

// Run performs duo doctor's core checks against the store at path and
// reports the adapters the caller registered. A nil slice reports as an
// empty array, never null.
func Run(path string, adapters []Adapter) Report {
	if adapters == nil {
		adapters = []Adapter{}
	}
	return Report{
		Store:    probeStore(path),
		Adapters: AdaptersStatus{Registered: adapters},
	}
}

// probeStore reports path's health through SQLite's physical read-only mode.
// It never creates/migrates a database and observes the writer lease without
// acquiring even a transient lease.
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

	ro, err := store.OpenReadOnly(path)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.SchemaVersion = ro.Version()
	lease, writerErr := ro.InspectWriterLease(context.Background(), time.Now())
	if writerErr == nil {
		status.Writer = &WriterStatus{
			Active:      lease.Active,
			Incarnation: lease.Incarnation,
			PID:         lease.PID,
			Hostname:    lease.Hostname,
			ExpiresAt:   lease.ExpiresAt,
		}
	}
	closeErr := ro.Close()

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
