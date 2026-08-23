package domain_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/store"
)

// harness is one authority over one temporary store, with a reopen that
// rebuilds the kernel from the durable fact log alone.
type harness struct {
	t    *testing.T
	path string
	s    *store.Store
	a    *domain.Authority
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, path: filepath.Join(t.TempDir(), "duo.db")}
	h.open()
	return h
}

func (h *harness) open() {
	h.t.Helper()
	s, err := store.OpenAuthority(h.path)
	if err != nil {
		h.t.Fatalf("OpenAuthority: %v", err)
	}
	a, err := domain.Open(context.Background(), storerepo.New(s))
	if err != nil {
		_ = s.Close()
		h.t.Fatalf("domain.Open: %v", err)
	}
	h.s, h.a = s, a
	h.t.Cleanup(func() { _ = s.Close() })
}

// reopen closes the store and builds a fresh authority from the same
// database — the authority-restart path (decision-01 §4.4).
func (h *harness) reopen() {
	h.t.Helper()
	if err := h.s.Close(); err != nil {
		h.t.Fatalf("closing store: %v", err)
	}
	h.open()
}

// A Herdr-shaped fingerprint. The probe at 0.8.2 (notes/19 §5) found no
// server epoch: the epoch-equivalent is the pane's terminal_id, and the
// process fingerprint comes from pane.process_info.
func herdrFingerprint(pane, terminalID string, pid int) domain.Fingerprint {
	return domain.Fingerprint{
		IntegrationInstance: "herdr@/run/user/1000/herdr.sock",
		Epoch: domain.HostEpoch{
			Kind:  "herdr.terminal_id",
			Value: terminalID,
			Scope: domain.EpochScopePane,
		},
		Container: pane,
		Process: domain.ProcessBirth{
			PID:        pid,
			StartedAt:  "2026-08-23T10:00:0" + strconv.Itoa(pid%10) + ".000Z",
			Executable: "/usr/bin/claude",
		},
	}
}

// testRepo returns a second repository over the same store handle, for tests
// that need two kernels racing over one durable index.
func testRepo(t *testing.T, h *harness) domain.Repository {
	t.Helper()
	return storerepo.New(h.s)
}

func candidate(root string, fp domain.Fingerprint) domain.Candidate {
	return domain.Candidate{
		Fingerprint: fp,
		RootPath:    root,
		Hints:       map[string]string{"cwd": root, "command": "claude"},
	}
}

func mustEnroll(t *testing.T, a *domain.Authority, c domain.Candidate) domain.EnrollResult {
	t.Helper()
	res, err := a.Enroll(context.Background(), domain.EnrollRequest{Candidate: c, Actor: "user:beau"})
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}
	return res
}
