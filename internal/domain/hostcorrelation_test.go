package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
)

// The workspace↔host correlation is the host sibling of the path rebind:
// one current binding per workspace, established once and changed only by an
// audited rebind that records old and new instance with fingerprints
// (notes/42 §8, notes/43 item 13). These tests hold the three properties the
// step's acceptance names — a first bind writes host_bound with its
// host_source, a rebind writes exactly one host_rebound carrying both
// instances, and the read model reports the latest binding — plus the
// refusals that keep "never runs implicitly" true.

const (
	socketA = "/run/user/1000/herdr/alpha.sock"
	socketB = "/run/user/1000/herdr/beta.sock"
)

func hostFingerprint(session, pane, terminal string, pid int) domain.HostFingerprint {
	return domain.HostFingerprint{
		SessionName: session,
		PaneID:      pane,
		TerminalID:  terminal,
		Process: domain.ProcessBirth{
			PID:        pid,
			StartedAt:  "2026-08-24T10:00:00.000Z",
			Executable: "/usr/bin/claude",
		},
	}
}

// boundWorkspace enrolls a session so a workspace exists, and returns it.
func boundWorkspace(t *testing.T, h *harness, root string) domain.WorkspaceID {
	t.Helper()
	res := mustEnroll(t, h.a, candidate(root, herdrFingerprint("w1:p1", "term_1", 4001)))
	return res.Workspace
}

func mustBind(t *testing.T, h *harness, ws domain.WorkspaceID, source domain.HostSource, instance string) domain.HostCorrelation {
	t.Helper()
	c, err := h.a.BindWorkspaceHost(context.Background(), domain.BindHostRequest{
		Workspace:   ws,
		Kind:        "herdr",
		Instance:    instance,
		InstanceID:  "herdr:alpha",
		Source:      source,
		Fingerprint: hostFingerprint("alpha", "w1:p1", "term_1", 4001),
		Actor:       "user:beau",
		Evidence:    "spawn evidence from the herdr adapter",
	})
	if err != nil {
		t.Fatalf("BindWorkspaceHost: %v", err)
	}
	return c
}

// countFacts returns how many facts of kind are in the durable log.
func countFacts(t *testing.T, h *harness, kind domain.FactKind) []domain.Fact {
	t.Helper()
	facts, err := testRepo(t, h).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	var out []domain.Fact
	for _, f := range facts {
		if f.Kind == kind {
			out = append(out, f)
		}
	}
	return out
}

// TestBindWorkspaceHost_WritesHostBoundFact is the first-bind property: one
// workspace.host_bound fact carrying the instance, the host_source, and the
// notes/19 §5 fingerprint set — and a read model that survives a restart,
// because the fact log is the record of truth.
func TestBindWorkspaceHost_WritesHostBoundFact(t *testing.T) {
	h := newHarness(t)
	ws := boundWorkspace(t, h, "/tmp/duo-host-bind")

	if _, bound := h.a.HostCorrelation(ws); bound {
		t.Fatal("a fresh workspace already reports a host correlation")
	}

	c := mustBind(t, h, ws, domain.HostSourceAmbientEnv, socketA)

	if c.FactKind != domain.FactWorkspaceHostBound {
		t.Errorf("fact kind = %q, want %q", c.FactKind, domain.FactWorkspaceHostBound)
	}
	if c.FactID == "" {
		t.Error("the correlation carries no fact id; the evidence bundle cites it")
	}
	if c.Binding.Source != domain.HostSourceAmbientEnv {
		t.Errorf("host_source = %q, want %q", c.Binding.Source, domain.HostSourceAmbientEnv)
	}
	if c.Binding.Locator() != "herdr:"+socketA {
		t.Errorf("locator = %q, want %q", c.Binding.Locator(), "herdr:"+socketA)
	}
	if c.Previous != nil {
		t.Error("a first bind reports a previous binding; there was none")
	}

	written := countFacts(t, h, domain.FactWorkspaceHostBound)
	if len(written) != 1 {
		t.Fatalf("wrote %d workspace.host_bound facts, want 1", len(written))
	}
	if written[0].HostBinding == nil {
		t.Fatal("the fact carries no host binding payload")
	}
	if got := written[0].HostBinding.Fingerprint; got.TerminalID != "term_1" || got.PaneID != "w1:p1" || got.SessionName != "alpha" {
		t.Errorf("fingerprint = %+v, want the notes/19 §5 set (session name, pane id, terminal id)", got)
	}
	if written[0].HostBinding.Fingerprint.Process.PID != 4001 {
		t.Error("the fingerprint lost its process-birth evidence")
	}

	// Replay: a restarted kernel reads the same binding from the log alone.
	h.reopen()
	replayed, bound := h.a.HostCorrelation(ws)
	if !bound {
		t.Fatal("the correlation did not survive an authority restart")
	}
	if replayed.Binding.Instance != socketA || replayed.Binding.Source != domain.HostSourceAmbientEnv {
		t.Errorf("replayed binding = %+v, want %s at %q", replayed.Binding, domain.HostSourceAmbientEnv, socketA)
	}
	if replayed.FactID != c.FactID {
		t.Errorf("replayed fact id = %q, want %q", replayed.FactID, c.FactID)
	}
}

// TestRebindWorkspaceHost_WritesOneFactWithBothInstances is the rebind
// property notes/42 §11 states: one audited fact, old and new instance, both
// with fingerprints.
func TestRebindWorkspaceHost_WritesOneFactWithBothInstances(t *testing.T) {
	h := newHarness(t)
	ws := boundWorkspace(t, h, "/tmp/duo-host-rebind")
	first := mustBind(t, h, ws, domain.HostSourcePolicyDefault, socketA)

	c, err := h.a.RebindWorkspaceHost(context.Background(), domain.RebindHostRequest{
		Workspace:   ws,
		Kind:        "herdr",
		Instance:    socketB,
		InstanceID:  "herdr:beta",
		Fingerprint: hostFingerprint("beta", "w2:p3", "term_9", 5100),
		Actor:       "user:beau",
		Reason:      "the alpha server was retired",
		Evidence:    "herdr status server --json on beta, 2026-08-24",
	})
	if err != nil {
		t.Fatalf("RebindWorkspaceHost: %v", err)
	}

	facts := countFacts(t, h, domain.FactWorkspaceHostRebound)
	if len(facts) != 1 {
		t.Fatalf("wrote %d workspace.host_rebound facts, want exactly 1", len(facts))
	}
	f := facts[0]
	if f.HostBinding == nil || f.PreviousHostBinding == nil {
		t.Fatal("the rebind fact does not carry both instances")
	}
	if f.PreviousHostBinding.Instance != socketA || f.HostBinding.Instance != socketB {
		t.Errorf("fact records %q -> %q, want %q -> %q",
			f.PreviousHostBinding.Instance, f.HostBinding.Instance, socketA, socketB)
	}
	if !f.PreviousHostBinding.Fingerprint.Present() || !f.HostBinding.Fingerprint.Present() {
		t.Error("one side of the rebind carries no fingerprint")
	}
	if f.PreviousHostBinding.Fingerprint.TerminalID == f.HostBinding.Fingerprint.TerminalID {
		t.Error("both sides share a terminal id; the fact cannot distinguish the incarnations")
	}
	if f.Evidence == "" {
		t.Error("the rebind fact names no evidence")
	}
	// A rebind's provenance is explicit-flag by construction: it cannot
	// happen without an explicit target.
	if f.HostBinding.Source != domain.HostSourceExplicitFlag {
		t.Errorf("rebind host_source = %q, want %q", f.HostBinding.Source, domain.HostSourceExplicitFlag)
	}

	// No second host_bound fact was written: a rebind replaces, it does not
	// re-bind.
	if bound := countFacts(t, h, domain.FactWorkspaceHostBound); len(bound) != 1 {
		t.Errorf("the log holds %d workspace.host_bound facts, want the original 1", len(bound))
	}

	if c.Previous == nil || c.Previous.Instance != first.Binding.Instance {
		t.Error("the result does not report the binding it replaced")
	}
}

// TestHostCorrelation_ReadsLatestBinding is what `duo workspace host show`
// prints: the latest fact for a workspace wins, before and after a restart.
func TestHostCorrelation_ReadsLatestBinding(t *testing.T) {
	h := newHarness(t)
	ws := boundWorkspace(t, h, "/tmp/duo-host-latest")
	mustBind(t, h, ws, domain.HostSourceExplicitFlag, socketA)

	for _, instance := range []string{socketB, socketA, socketB} {
		if _, err := h.a.RebindWorkspaceHost(context.Background(), domain.RebindHostRequest{
			Workspace:   ws,
			Kind:        "herdr",
			Instance:    instance,
			Fingerprint: hostFingerprint("s", "w1:p1", "term_"+instance, 6000),
			Evidence:    "operator rebind",
		}); err != nil {
			t.Fatalf("RebindWorkspaceHost(%s): %v", instance, err)
		}
	}

	h.reopen()
	c, bound := h.a.HostCorrelation(ws)
	if !bound {
		t.Fatal("no correlation after replay")
	}
	if c.Binding.Instance != socketB {
		t.Errorf("current instance = %q, want the latest fact's %q", c.Binding.Instance, socketB)
	}
	if c.Previous == nil || c.Previous.Instance != socketA {
		t.Errorf("previous instance = %+v, want %q", c.Previous, socketA)
	}
	if got := h.a.HostCorrelations(); len(got) != 1 {
		t.Errorf("HostCorrelations lists %d workspaces, want 1", len(got))
	}
}

// TestBindWorkspaceHost_Refusals covers the guards that keep the first bind
// a first bind and the correlation checkable.
func TestBindWorkspaceHost_Refusals(t *testing.T) {
	h := newHarness(t)
	ws := boundWorkspace(t, h, "/tmp/duo-host-refusals")
	full := hostFingerprint("alpha", "w1:p1", "term_1", 4001)

	cases := []struct {
		name string
		req  domain.BindHostRequest
		want error
	}{
		{
			name: "unknown workspace",
			req:  domain.BindHostRequest{Workspace: "ws_nope", Kind: "herdr", Instance: socketA, Source: domain.HostSourcePolicyDefault, Fingerprint: full},
			want: domain.ErrUnknownObject,
		},
		{
			name: "no instance locator",
			req:  domain.BindHostRequest{Workspace: ws, Kind: "herdr", Source: domain.HostSourcePolicyDefault, Fingerprint: full},
			want: domain.ErrHostTargetRequired,
		},
		{
			name: "provenance outside the host_source vocabulary",
			req:  domain.BindHostRequest{Workspace: ws, Kind: "herdr", Instance: socketA, Source: "guessed", Fingerprint: full},
			want: domain.ErrHostSourceUnknown,
		},
		{
			name: "no fingerprint evidence",
			req:  domain.BindHostRequest{Workspace: ws, Kind: "herdr", Instance: socketA, Source: domain.HostSourcePolicyDefault},
			want: domain.ErrHostFingerprintRequired,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := h.a.BindWorkspaceHost(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}

	mustBind(t, h, ws, domain.HostSourcePolicyDefault, socketA)
	_, err := h.a.BindWorkspaceHost(context.Background(), domain.BindHostRequest{
		Workspace: ws, Kind: "herdr", Instance: socketB,
		Source: domain.HostSourcePolicyDefault, Fingerprint: full,
	})
	if !errors.Is(err, domain.ErrHostAlreadyBound) {
		t.Errorf("second bind error = %v, want %v", err, domain.ErrHostAlreadyBound)
	}
	if got := countFacts(t, h, domain.FactWorkspaceHostBound); len(got) != 1 {
		t.Errorf("the refused bind still wrote a fact: %d host_bound facts", len(got))
	}
}

// TestRebindWorkspaceHost_Refusals holds the two constraints that keep a
// rebind from ever running implicitly: it needs an existing binding to
// replace, and it must name its evidence.
func TestRebindWorkspaceHost_Refusals(t *testing.T) {
	h := newHarness(t)
	ws := boundWorkspace(t, h, "/tmp/duo-host-rebind-refusals")
	full := hostFingerprint("beta", "w2:p3", "term_9", 5100)

	_, err := h.a.RebindWorkspaceHost(context.Background(), domain.RebindHostRequest{
		Workspace: ws, Kind: "herdr", Instance: socketB, Fingerprint: full, Evidence: "e",
	})
	if !errors.Is(err, domain.ErrHostNotBound) {
		t.Errorf("rebind with no binding: error = %v, want %v", err, domain.ErrHostNotBound)
	}

	mustBind(t, h, ws, domain.HostSourceAmbientEnv, socketA)

	_, err = h.a.RebindWorkspaceHost(context.Background(), domain.RebindHostRequest{
		Workspace: ws, Kind: "herdr", Instance: socketB, Fingerprint: full,
	})
	if !errors.Is(err, domain.ErrHostEvidenceRequired) {
		t.Errorf("rebind with no evidence: error = %v, want %v", err, domain.ErrHostEvidenceRequired)
	}

	_, err = h.a.RebindWorkspaceHost(context.Background(), domain.RebindHostRequest{
		Workspace: ws, Kind: "herdr", Instance: socketB, Evidence: "e",
	})
	if !errors.Is(err, domain.ErrHostFingerprintRequired) {
		t.Errorf("rebind with no fingerprint: error = %v, want %v", err, domain.ErrHostFingerprintRequired)
	}

	if got := countFacts(t, h, domain.FactWorkspaceHostRebound); len(got) != 0 {
		t.Errorf("a refused rebind wrote %d facts, want 0", len(got))
	}
}

// TestHostSourceVocabularyIsClosed pins the enum to duo.external/v1's sealed
// host_source values. A sixth rung is a schema change, not a code change.
func TestHostSourceVocabularyIsClosed(t *testing.T) {
	want := []string{"explicit-flag", "workspace-correlation", "cwd-correlation", "ambient-env", "policy-default"}
	if len(domain.HostSources) != len(want) {
		t.Fatalf("HostSources has %d values, want %d", len(domain.HostSources), len(want))
	}
	for i, v := range want {
		if string(domain.HostSources[i]) != v {
			t.Errorf("HostSources[%d] = %q, want %q (rank order is fixed)", i, domain.HostSources[i], v)
		}
		if !domain.HostSource(v).Valid() {
			t.Errorf("%q is not accepted by HostSource.Valid", v)
		}
	}
	if domain.HostSource("workspace_correlation").Valid() {
		t.Error("an underscored spelling is accepted; the vocabulary is the schema's, hyphens included")
	}
}

// TestWorkspaceForRoot resolves a workspace by the directory an operator is
// standing in — the addressing both verbs use.
func TestWorkspaceForRoot(t *testing.T) {
	h := newHarness(t)
	ws := boundWorkspace(t, h, "/tmp/duo-host-root")

	got, ok := h.a.WorkspaceForRoot("/tmp/duo-host-root/")
	if !ok || got.ID != ws {
		t.Errorf("WorkspaceForRoot = (%v, %t), want %s", got.ID, ok, ws)
	}
	if _, ok := h.a.WorkspaceForRoot("/tmp/duo-host-root-other"); ok {
		t.Error("an unknown root resolved to a workspace")
	}
	if _, ok := h.a.WorkspaceForRoot("relative/path"); ok {
		t.Error("a relative path resolved to a workspace")
	}
}
