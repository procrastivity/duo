package materialize_test

import (
	"context"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// fakeRoots is the RootDiscovery fake: a session-metadata read model with no
// filesystem behind it, so a test can claim any path (real or not) for any
// instance and count how many times the cwd rung asked.
type fakeRoots struct {
	byKind map[string][]materialize.InstanceRoots
	err    error
	calls  int
}

func (f *fakeRoots) DiscoverInstanceRoots(_ context.Context, kind string) ([]materialize.InstanceRoots, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.byKind[kind], nil
}

// TestCwdCorrelationBeatsAmbientEnvAndRecordsTheCapture: a session's claim on
// the workspace directory is observed identity, stronger than the pane this
// Duo happens to be standing in, so it wins over the ambient environment —
// and the ambient rung is still consulted and its capture still recorded as
// outranked, exactly as the correlation rung already does to it.
func TestCwdCorrelationBeatsAmbientEnvAndRecordsTheCapture(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{
		"HERDR_SOCKET_PATH": envSocket,
		"HERDR_SESSION":     "ambient",
	}).lookup
	opts.Roots = &fakeRoots{byKind: map[string][]materialize.InstanceRoots{
		"herdr": {{
			Instance: materialize.Instance{Locator: cwdSocket, InstanceID: "herdr:claimed"},
			Roots:    []string{workspacePath},
		}},
	}}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceCwdCorrelation {
		t.Fatalf("host_source = %q, want cwd-correlation", got.Host().Source)
	}
	if got.Host().Instance != cwdSocket {
		t.Errorf("instance = %q, want %q", got.Host().Instance, cwdSocket)
	}

	env := rungFor(t, got.Trail(), domain.HostSourceAmbientEnv)
	if !env.Consulted || !env.YieldedHost {
		t.Errorf("the ambient rung was not consulted and recorded as yielding: %+v", env)
	}

	var outranked *materialize.OutrankedEvidence
	evidence := got.OutrankedEvidence()
	for i := range evidence {
		if evidence[i].Source == domain.HostSourceAmbientEnv {
			outranked = &evidence[i]
		}
	}
	if outranked == nil {
		t.Fatalf("no ambient-env outranked evidence: %+v", got.OutrankedEvidence())
	}
	if outranked.Detail != "outranked by cwd-correlation" {
		t.Errorf("outranked detail = %q", outranked.Detail)
	}
	if len(outranked.Captures) != 2 {
		t.Fatalf("outranked captures = %+v, want both variables", outranked.Captures)
	}
}

// TestCorrelationBeatsCwdCorrelation: a persisted workspace↔host binding is
// declared intent, which outranks a session's mere claim on the directory —
// and the beaten cwd claim still shows up as outranked evidence, next to the
// rebind pointer.
func TestCorrelationBeatsCwdCorrelation(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = boundCorrelations()
	opts.Roots = &fakeRoots{byKind: map[string][]materialize.InstanceRoots{
		"herdr": {{
			Instance: materialize.Instance{Locator: cwdSocket, InstanceID: "herdr:claimed"},
			Roots:    []string{workspacePath},
		}},
	}}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceWorkspaceCorrelation {
		t.Fatalf("host_source = %q, want workspace-correlation", got.Host().Source)
	}

	var outranked *materialize.OutrankedEvidence
	evidence := got.OutrankedEvidence()
	for i := range evidence {
		if evidence[i].Source == domain.HostSourceCwdCorrelation {
			outranked = &evidence[i]
		}
	}
	if outranked == nil {
		t.Fatalf("no cwd-correlation outranked evidence: %+v", got.OutrankedEvidence())
	}
	if outranked.Instance != cwdSocket {
		t.Errorf("outranked instance = %q, want %q", outranked.Instance, cwdSocket)
	}
	if outranked.Detail != "outranked by workspace-correlation" {
		t.Errorf("outranked detail = %q", outranked.Detail)
	}
}

// TestCwdCorrelationTieYieldsNothing: two sessions claiming the same
// directory at equal depth is an ambiguity, not evidence for either one —
// the rung reports it and yields nothing, so the ambient rung below it still
// gets its chance.
func TestCwdCorrelationTieYieldsNothing(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{
		"HERDR_SOCKET_PATH": envSocket,
		"HERDR_SESSION":     "ambient",
	}).lookup
	opts.Roots = &fakeRoots{byKind: map[string][]materialize.InstanceRoots{
		"herdr": {
			{
				Instance: materialize.Instance{Locator: cwdSocket, InstanceID: "herdr:claimed"},
				Roots:    []string{workspacePath},
			},
			{
				Instance: materialize.Instance{Locator: boundSocket, InstanceID: "herdr:work"},
				Roots:    []string{workspacePath},
			},
		},
	}}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceAmbientEnv {
		t.Fatalf("host_source = %q, want ambient-env", got.Host().Source)
	}

	rung := rungFor(t, got.Trail(), domain.HostSourceCwdCorrelation)
	if !rung.Consulted || rung.YieldedHost {
		t.Errorf("cwd rung = %+v, want consulted and not yielding", rung)
	}
	if !strings.Contains(rung.Detail, "2 herdr sessions claim") {
		t.Errorf("cwd rung detail = %q, want it to name the ambiguity", rung.Detail)
	}
	if !strings.Contains(rung.Detail, "name one as --host herdr:<instance>") {
		t.Errorf("cwd rung detail = %q, want it to point at --host", rung.Detail)
	}
}

// TestCwdCorrelationDeepestRootWins: when one instance claims an ancestor of
// the workspace path and another claims the workspace path itself, the
// deepest (most specific) root wins — and the same holds for a path
// strictly under the winning root.
func TestCwdCorrelationDeepestRootWins(t *testing.T) {
	const ancestorSocket = "/run/user/1000/herdr/ancestor.sock"

	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.Roots = &fakeRoots{byKind: map[string][]materialize.InstanceRoots{
		"herdr": {
			{
				Instance: materialize.Instance{Locator: ancestorSocket, InstanceID: "herdr:ancestor"},
				Roots:    []string{"/home/dev/Code"},
			},
			{
				Instance: materialize.Instance{Locator: cwdSocket, InstanceID: "herdr:claimed"},
				Roots:    []string{workspacePath},
			},
		},
	}}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceCwdCorrelation || got.Host().Instance != cwdSocket {
		t.Fatalf("host = %+v, want the deepest-root instance", got.Host())
	}

	// A subdirectory of the winning root matches it too, and only the
	// deepest claimant remains in play.
	opts2 := baseOptions()
	opts2.WorkspaceFlag = workspacePath + "/nested"
	opts2.Correlations = unboundCorrelations()
	opts2.Roots = &fakeRoots{byKind: map[string][]materialize.InstanceRoots{
		"herdr": {{
			Instance: materialize.Instance{Locator: cwdSocket, InstanceID: "herdr:claimed"},
			Roots:    []string{workspacePath},
		}},
	}}

	got2, err := materialize.Materialize(context.Background(), opts2)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got2.Host().Source != domain.HostSourceCwdCorrelation || got2.Host().Instance != cwdSocket {
		t.Fatalf("host = %+v, want the claiming instance for the subdirectory", got2.Host())
	}
	rung := rungFor(t, got2.Trail(), domain.HostSourceCwdCorrelation)
	if !strings.Contains(rung.Detail, "contains") {
		t.Errorf("cwd rung detail = %q, want it to say the root contains the path", rung.Detail)
	}
}

// TestCwdCorrelationNilRootsIsReported: a build with no root discoverer
// wired can still deduce from the environment; the cwd rung says out loud
// why it had nothing to say.
func TestCwdCorrelationNilRootsIsReported(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{
		"HERDR_SOCKET_PATH": envSocket,
		"HERDR_SESSION":     "ambient",
	}).lookup
	opts.Roots = nil

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceAmbientEnv {
		t.Fatalf("host_source = %q, want ambient-env", got.Host().Source)
	}
	rung := rungFor(t, got.Trail(), domain.HostSourceCwdCorrelation)
	if !rung.Consulted || rung.YieldedHost {
		t.Errorf("cwd rung = %+v, want consulted and not yielding", rung)
	}
	if rung.Detail != "no session-root discovery is available in this build" {
		t.Errorf("cwd rung detail = %q", rung.Detail)
	}
}

// TestDisabledCwdRungIsSkipped: `session_hosts.deduce.cwd: false` switches
// the rung off without touching the ranking below it, and a skipped rung
// must not cost the I/O it would have spent reading session metadata.
func TestDisabledCwdRungIsSkipped(t *testing.T) {
	no := false
	opts := baseOptions()
	opts.Policy = config.SessionHostPolicy{
		Prefer: []string{"herdr"},
		Deduce: map[string]config.SessionHostDeduceSource{"cwd": {Enabled: &no}},
	}
	opts.Correlations = unboundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{
		"HERDR_SOCKET_PATH": envSocket,
		"HERDR_SESSION":     "ambient",
	}).lookup
	roots := &fakeRoots{byKind: map[string][]materialize.InstanceRoots{
		"herdr": {{
			Instance: materialize.Instance{Locator: cwdSocket, InstanceID: "herdr:claimed"},
			Roots:    []string{workspacePath},
		}},
	}}
	opts.Roots = roots

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceAmbientEnv {
		t.Fatalf("host_source = %q, want ambient-env", got.Host().Source)
	}
	rung := rungFor(t, got.Trail(), domain.HostSourceCwdCorrelation)
	if rung.Consulted {
		t.Errorf("a disabled cwd rung is recorded as consulted: %+v", rung)
	}
	if !strings.Contains(rung.Detail, "disabled by session_hosts.deduce") {
		t.Errorf("disabled cwd rung detail = %q", rung.Detail)
	}
	if roots.calls != 0 {
		t.Errorf("a disabled cwd rung still read session roots: %d calls", roots.calls)
	}
}

// TestCwdRootDiscoveryFailureIsNonFatal: a metadata read error is folded
// into the trail rather than returned — a broken session.json must not fail
// a launch a higher (or lower) rung can still resolve.
func TestCwdRootDiscoveryFailureIsNonFatal(t *testing.T) {
	boom := context.DeadlineExceeded
	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{
		"HERDR_SOCKET_PATH": envSocket,
		"HERDR_SESSION":     "ambient",
	}).lookup
	opts.Roots = &fakeRoots{err: boom}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceAmbientEnv {
		t.Fatalf("host_source = %q, want ambient-env", got.Host().Source)
	}
	rung := rungFor(t, got.Trail(), domain.HostSourceCwdCorrelation)
	if !rung.Consulted || rung.YieldedHost {
		t.Errorf("cwd rung = %+v, want consulted and not yielding", rung)
	}
	if !strings.Contains(rung.Detail, "session-root discovery failed") {
		t.Errorf("cwd rung detail = %q", rung.Detail)
	}
}
