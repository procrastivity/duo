package materialize_test

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// --- fakes ---------------------------------------------------------------

// fakeCorrelations is the workspace↔host read model, narrow enough that a
// test never needs a store. *domain.Authority satisfies the same interface.
type fakeCorrelations struct {
	roots    map[string]domain.WorkspaceID
	bindings map[domain.WorkspaceID]domain.HostCorrelation
}

func (f fakeCorrelations) WorkspaceForRoot(root string) (domain.Workspace, bool) {
	id, ok := f.roots[root]
	if !ok {
		return domain.Workspace{}, false
	}
	return domain.Workspace{ID: id, RootPath: root}, true
}

func (f fakeCorrelations) HostCorrelation(id domain.WorkspaceID) (domain.HostCorrelation, bool) {
	c, ok := f.bindings[id]
	return c, ok
}

type fakeProviders map[string]domain.ProviderStanding

func (f fakeProviders) StandingProviderFacts() map[string]domain.ProviderStanding {
	out := make(map[string]domain.ProviderStanding, len(f))
	for k, v := range f {
		out[k] = v
	}
	return out
}

type fakeDiscovery struct {
	byKind map[string][]materialize.Instance
	err    error
	calls  int
}

func (f *fakeDiscovery) DiscoverInstances(_ context.Context, kind string) ([]materialize.Instance, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.byKind[kind], nil
}

// countingEnv records how many times each variable name was looked up, so a
// test can prove "exactly once per variable" rather than assume it.
type countingEnv struct {
	values map[string]string
	reads  map[string]int
}

func newEnv(values map[string]string) *countingEnv {
	return &countingEnv{values: values, reads: map[string]int{}}
}

func (e *countingEnv) lookup(name string) (string, bool) {
	e.reads[name]++
	v, ok := e.values[name]
	return v, ok
}

// --- shared fixtures ------------------------------------------------------

const (
	workspacePath = "/home/dev/Code/thing"
	workspaceID   = domain.WorkspaceID("ws_thing")
	boundSocket   = "/run/user/1000/herdr/bound.sock"
	envSocket     = "/run/user/1000/herdr/ambient.sock"
	flagSocket    = "/run/user/1000/herdr/flagged.sock"
	foundSocket   = "/run/user/1000/herdr/discovered.sock"
	cwdSocket     = "/run/user/1000/herdr/claimed.sock"
)

func fixedClock() func() time.Time {
	at := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

func boundCorrelations() fakeCorrelations {
	return fakeCorrelations{
		roots: map[string]domain.WorkspaceID{workspacePath: workspaceID},
		bindings: map[domain.WorkspaceID]domain.HostCorrelation{
			workspaceID: {
				Binding: domain.HostBinding{
					Workspace:   workspaceID,
					Kind:        "herdr",
					Instance:    boundSocket,
					InstanceID:  "herdr:work",
					Source:      domain.HostSourceExplicitFlag,
					Fingerprint: domain.HostFingerprint{SessionName: "work", PaneID: "pane-1"},
				},
				FactID: "fact_bind_1",
			},
		},
	}
}

func unboundCorrelations() fakeCorrelations {
	return fakeCorrelations{roots: map[string]domain.WorkspaceID{workspacePath: workspaceID}}
}

func preferHerdr() config.SessionHostPolicy {
	return config.SessionHostPolicy{Prefer: []string{"herdr"}}
}

func baseOptions() materialize.Options {
	return materialize.Options{
		WorkspaceFlag:   workspacePath,
		RequestedPreset: "review",
		Policy:          preferHerdr(),
		LookupEnv:       newEnv(nil).lookup,
		Now:             fixedClock(),
	}
}

func rungFor(t *testing.T, trail []materialize.DeductionRung, source domain.HostSource) materialize.DeductionRung {
	t.Helper()
	for _, r := range trail {
		if r.Source == source {
			return r
		}
	}
	t.Fatalf("trail has no %s rung: %+v", source, trail)
	return materialize.DeductionRung{}
}

// --- the ladder -----------------------------------------------------------

// TestMaterializeEachRungWins drives one input shape per rung and asserts
// the winner, its host_source, and the instance it produced. It is the
// ranking itself under test: explicit flag > workspace correlation >
// ambient environment > policy default.
func TestMaterializeEachRungWins(t *testing.T) {
	cases := []struct {
		name         string
		mutate       func(*materialize.Options)
		wantSource   domain.HostSource
		wantInstance string
		wantID       string
	}{
		{
			name: "explicit flag",
			mutate: func(o *materialize.Options) {
				o.HostFlag = "herdr:" + flagSocket
				o.Correlations = boundCorrelations()
				o.LookupEnv = newEnv(map[string]string{"HERDR_SOCKET_PATH": envSocket}).lookup
			},
			wantSource:   domain.HostSourceExplicitFlag,
			wantInstance: flagSocket,
		},
		{
			name: "workspace correlation",
			mutate: func(o *materialize.Options) {
				o.Correlations = boundCorrelations()
				o.LookupEnv = newEnv(map[string]string{"HERDR_SOCKET_PATH": envSocket}).lookup
			},
			wantSource:   domain.HostSourceWorkspaceCorrelation,
			wantInstance: boundSocket,
			wantID:       "herdr:work",
		},
		{
			name: "cwd correlation",
			mutate: func(o *materialize.Options) {
				o.Correlations = unboundCorrelations()
				o.LookupEnv = newEnv(map[string]string{
					"HERDR_SOCKET_PATH": envSocket,
					"HERDR_SESSION":     "ambient",
				}).lookup
				o.Roots = &fakeRoots{byKind: map[string][]materialize.InstanceRoots{
					"herdr": {{
						Instance: materialize.Instance{Locator: cwdSocket, InstanceID: "herdr:claimed"},
						Roots:    []string{workspacePath},
					}},
				}}
			},
			wantSource:   domain.HostSourceCwdCorrelation,
			wantInstance: cwdSocket,
			wantID:       "herdr:claimed",
		},
		{
			name: "ambient environment",
			mutate: func(o *materialize.Options) {
				o.Correlations = unboundCorrelations()
				o.LookupEnv = newEnv(map[string]string{
					"HERDR_SOCKET_PATH": envSocket,
					"HERDR_SESSION":     "ambient",
				}).lookup
			},
			wantSource:   domain.HostSourceAmbientEnv,
			wantInstance: envSocket,
			wantID:       "herdr:ambient",
		},
		{
			name: "policy default",
			mutate: func(o *materialize.Options) {
				o.Correlations = unboundCorrelations()
				o.Discovery = &fakeDiscovery{byKind: map[string][]materialize.Instance{
					"herdr": {{Locator: foundSocket, InstanceID: "herdr:only"}},
				}}
			},
			wantSource:   domain.HostSourcePolicyDefault,
			wantInstance: foundSocket,
			wantID:       "herdr:only",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := baseOptions()
			tc.mutate(&opts)

			got, err := materialize.Materialize(context.Background(), opts)
			if err != nil {
				t.Fatalf("Materialize: %v", err)
			}
			host := got.Host()
			if host.Source != tc.wantSource {
				t.Errorf("host_source = %q, want %q", host.Source, tc.wantSource)
			}
			if host.Kind != "herdr" {
				t.Errorf("kind = %q, want herdr", host.Kind)
			}
			if host.Instance != tc.wantInstance {
				t.Errorf("instance = %q, want %q", host.Instance, tc.wantInstance)
			}
			if host.InstanceID != tc.wantID {
				t.Errorf("instance id = %q, want %q", host.InstanceID, tc.wantID)
			}
			if host.Workspace != workspaceID {
				t.Errorf("workspace = %q, want %q", host.Workspace, workspaceID)
			}
			if got.WorkspacePath() != workspacePath {
				t.Errorf("workspace path = %q, want %q", got.WorkspacePath(), workspacePath)
			}
			if len(got.Trail()) != len(materialize.Rungs) {
				t.Errorf("trail has %d rungs, want all %d", len(got.Trail()), len(materialize.Rungs))
			}
			won := rungFor(t, got.Trail(), tc.wantSource)
			if !won.Consulted || !won.YieldedHost {
				t.Errorf("winning rung %+v is not recorded as consulted and yielding", won)
			}
		})
	}
}

// TestCorrelationBeatsAmbientEnvAndRecordsTheCapture is the case notes/19
// §0's precedence footgun made this ranking for: a Duo started inside some
// other pane must not silently launch there when the workspace is bound
// elsewhere. The ambient value is still captured, and recorded as outranked,
// which is what makes the override visible instead of silent.
func TestCorrelationBeatsAmbientEnvAndRecordsTheCapture(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = boundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{
		"HERDR_SOCKET_PATH": envSocket,
		"HERDR_SESSION":     "ambient",
	}).lookup

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceWorkspaceCorrelation {
		t.Fatalf("host_source = %q, want workspace-correlation", got.Host().Source)
	}
	if got.Host().Instance != boundSocket {
		t.Errorf("instance = %q, want the bound socket", got.Host().Instance)
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
	if outranked.Instance != envSocket {
		t.Errorf("outranked instance = %q, want %q", outranked.Instance, envSocket)
	}
	if len(outranked.Captures) != 2 {
		t.Fatalf("outranked captures = %+v, want both variables", outranked.Captures)
	}
	if outranked.Detail != "outranked by workspace-correlation" {
		t.Errorf("outranked detail = %q", outranked.Detail)
	}

	captures := got.Bundle().AmbientCaptures()
	if len(captures) != 2 {
		t.Fatalf("bundle captures = %+v, want both variables", captures)
	}
	if captures[0].Name != "HERDR_SOCKET_PATH" || captures[0].Value != envSocket {
		t.Errorf("first capture = %+v", captures[0])
	}
	if captures[0].CapturedAt != "2026-08-24T12:00:00.000Z" {
		t.Errorf("capture stamp = %q, want the injected clock in the domain layout", captures[0].CapturedAt)
	}
	if id, ok := got.Bundle().CorrelationFactID(); !ok || id != "fact_bind_1" {
		t.Errorf("correlation fact id = %q (%v), want fact_bind_1", id, ok)
	}
	fp, ok := got.Bundle().CorrelationFingerprint()
	if !ok || fp.SessionName != "work" {
		t.Errorf("correlation fingerprint = %+v (%v)", fp, ok)
	}
}

// TestExplicitFlagBeatsCorrelation proves the top rung: an operator who
// names a host gets it, and the correlation they overrode is recorded with
// its fact ID so the override is auditable.
func TestExplicitFlagBeatsCorrelation(t *testing.T) {
	opts := baseOptions()
	opts.HostFlag = "herdr:" + flagSocket
	opts.Correlations = boundCorrelations()

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceExplicitFlag || got.Host().Instance != flagSocket {
		t.Fatalf("host = %+v, want the flagged instance from the flag rung", got.Host())
	}

	var found bool
	for _, e := range got.OutrankedEvidence() {
		if e.Source != domain.HostSourceWorkspaceCorrelation {
			continue
		}
		found = true
		if e.Instance != boundSocket {
			t.Errorf("outranked correlation instance = %q, want %q", e.Instance, boundSocket)
		}
		if e.FactID != "fact_bind_1" {
			t.Errorf("outranked correlation fact id = %q, want fact_bind_1", e.FactID)
		}
	}
	if !found {
		t.Errorf("the overridden correlation is not recorded as outranked: %+v", got.OutrankedEvidence())
	}
}

// TestDisabledDeduceRungsAreSkipped covers the thread-2 boundary: the
// `deduce` list only switches a rung on or off. A disabled rung appears in
// the trail with consulted = false, and the ranking of the rungs that
// remain is unchanged.
func TestDisabledDeduceRungsAreSkipped(t *testing.T) {
	no := false
	opts := baseOptions()
	opts.Policy = config.SessionHostPolicy{
		Prefer: []string{"herdr"},
		Deduce: map[string]config.SessionHostDeduceSource{"env": {Enabled: &no}},
	}
	opts.Correlations = boundCorrelations()
	env := newEnv(map[string]string{"HERDR_SOCKET_PATH": envSocket})
	opts.LookupEnv = env.lookup

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	rung := rungFor(t, got.Trail(), domain.HostSourceAmbientEnv)
	if rung.Consulted {
		t.Errorf("a disabled rung is recorded as consulted: %+v", rung)
	}
	if !strings.Contains(rung.Detail, "disabled by session_hosts.deduce") {
		t.Errorf("disabled rung detail = %q", rung.Detail)
	}
	if len(env.reads) != 0 {
		t.Errorf("a disabled ambient rung still read the environment: %v", env.reads)
	}
	if len(got.Bundle().AmbientCaptures()) != 0 {
		t.Errorf("a disabled ambient rung still captured values: %+v", got.Bundle().AmbientCaptures())
	}
	// The rungs that remain keep their fixed order and their outcome.
	if got.Host().Source != domain.HostSourceWorkspaceCorrelation {
		t.Errorf("host_source = %q, want workspace-correlation", got.Host().Source)
	}
	for i, r := range got.Trail() {
		if r.Source != materialize.Rungs[i] {
			t.Fatalf("trail rung %d is %q, want %q — the ranking is fixed", i, r.Source, materialize.Rungs[i])
		}
	}
}

// TestDisabledCorrelationRungFallsToTheEnvironment proves the same for the
// workspace rung, and that disabling it does not promote anything: the
// environment wins because it is the next rung down, not because the list
// reordered.
func TestDisabledCorrelationRungFallsToTheEnvironment(t *testing.T) {
	no := false
	opts := baseOptions()
	opts.Policy = config.SessionHostPolicy{
		Prefer: []string{"herdr"},
		Deduce: map[string]config.SessionHostDeduceSource{"workspace": {Enabled: &no}},
	}
	opts.Correlations = boundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{"HERDR_SOCKET_PATH": envSocket}).lookup

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceAmbientEnv || got.Host().Instance != envSocket {
		t.Fatalf("host = %+v, want the ambient instance", got.Host())
	}
	if _, ok := got.Bundle().CorrelationFactID(); ok {
		t.Errorf("a disabled correlation rung still put a fact in the bundle")
	}
}

// TestAmbientVariablesAreReadExactlyOnce is the injected-environment
// property the bundle's evidence value rests on: a capture that could be
// re-read might disagree with itself, and then two rungs of one launch
// would be explaining different worlds.
func TestAmbientVariablesAreReadExactlyOnce(t *testing.T) {
	// The correlation wins here on purpose: the environment is still read
	// (to record the outranked capture), and still read only once.
	opts := baseOptions()
	opts.Correlations = boundCorrelations()
	env := newEnv(map[string]string{
		"HERDR_SOCKET_PATH": envSocket,
		"HERDR_SESSION":     "ambient",
	})
	opts.LookupEnv = env.lookup

	if _, err := materialize.Materialize(context.Background(), opts); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	want := materialize.AmbientVariableNames(materialize.DefaultAmbientSources)
	if len(want) == 0 {
		t.Fatal("the default ambient table names no variables")
	}
	for _, name := range want {
		if env.reads[name] != 1 {
			t.Errorf("variable %q was read %d times, want exactly 1", name, env.reads[name])
		}
	}
	for name, n := range env.reads {
		if n != 1 {
			t.Errorf("variable %q was read %d times, want exactly 1", name, n)
		}
	}
}

// TestAmbientSessionWithoutSocketYieldsNothing: the locator variable is
// what makes the rung yield. A session name with no socket path addresses
// nothing, so the rung records the capture and produces no host.
func TestAmbientSessionWithoutSocketYieldsNothing(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{"HERDR_SESSION": "ambient"}).lookup
	opts.Discovery = &fakeDiscovery{byKind: map[string][]materialize.Instance{
		"herdr": {{Locator: foundSocket}},
	}}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourcePolicyDefault {
		t.Fatalf("host_source = %q, want policy-default", got.Host().Source)
	}
	env := rungFor(t, got.Trail(), domain.HostSourceAmbientEnv)
	if env.YieldedHost {
		t.Errorf("the ambient rung yielded without a socket path: %+v", env)
	}
	if !strings.Contains(env.Detail, "HERDR_SOCKET_PATH not set") {
		t.Errorf("ambient rung detail = %q", env.Detail)
	}
	if len(got.Bundle().AmbientCaptures()) != 1 {
		t.Errorf("the session-name capture was not recorded: %+v", got.Bundle().AmbientCaptures())
	}
}

// --- the policy-default rung's instance question --------------------------

// TestPolicyDefaultDiscoveryIsNeverASilentPick covers workplan Risk 5: with
// no flag, correlation, or environment, the default rung has a kind and no
// instance. Zero fails, several fails naming them, and neither ever picks.
func TestPolicyDefaultDiscoveryIsNeverASilentPick(t *testing.T) {
	cases := []struct {
		name       string
		instances  []materialize.Instance
		wantDetail string
	}{
		{name: "zero", instances: nil, wantDetail: "discovery found no herdr instance"},
		{
			name: "several",
			instances: []materialize.Instance{
				{Locator: "/run/b.sock"},
				{Locator: "/run/a.sock"},
			},
			wantDetail: "discovery found 2 herdr instances (/run/a.sock, /run/b.sock)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := baseOptions()
			opts.Correlations = unboundCorrelations()
			opts.Discovery = &fakeDiscovery{byKind: map[string][]materialize.Instance{"herdr": tc.instances}}

			_, err := materialize.Materialize(context.Background(), opts)
			var merr *materialize.Error
			if !errors.As(err, &merr) {
				t.Fatalf("error = %v, want a *materialize.Error", err)
			}
			if merr.Code != materialize.CodeHostUnresolved {
				t.Errorf("code = %q, want %q", merr.Code, materialize.CodeHostUnresolved)
			}
			rung := rungFor(t, merr.Result().Trail(), domain.HostSourcePolicyDefault)
			if !strings.Contains(rung.Detail, tc.wantDetail) {
				t.Errorf("policy-default detail = %q, want it to contain %q", rung.Detail, tc.wantDetail)
			}
			if rung.YieldedHost {
				t.Errorf("the default rung picked one anyway: %+v", rung)
			}
		})
	}
}

// TestPolicyDefaultUsesTheFirstEnabledKind: a disabled kind is skipped in
// `prefer` order, and the rung never falls through to a later kind once it
// has chosen one.
func TestPolicyDefaultUsesTheFirstEnabledKind(t *testing.T) {
	no := false
	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.Policy = config.SessionHostPolicy{
		Prefer: []string{"tmux", "herdr"},
		Kinds:  map[string]config.SessionHostKind{"tmux": {Enabled: &no}},
	}
	disc := &fakeDiscovery{byKind: map[string][]materialize.Instance{
		"herdr": {{Locator: foundSocket}},
		"tmux":  {{Locator: "/run/tmux.sock"}},
	}}
	opts.Discovery = disc

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Kind != "herdr" || got.Host().Instance != foundSocket {
		t.Fatalf("host = %+v, want the first *enabled* preferred kind", got.Host())
	}
	if disc.calls != 1 {
		t.Errorf("discovery was called %d times, want exactly once", disc.calls)
	}
}

// TestDiscoveryIsNotCalledWhenAnEvidenceRungWins: the default rung is the
// only one that costs I/O, so it is asked only when nothing above it
// answered. The cwd rung reads its own RootDiscovery (a filesystem read,
// never a dial) and is never routed through InstanceDiscovery, so a nil
// opts.Roots here does not change what this test pins.
func TestDiscoveryIsNotCalledWhenAnEvidenceRungWins(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = boundCorrelations()
	disc := &fakeDiscovery{byKind: map[string][]materialize.Instance{
		"herdr": {{Locator: foundSocket}},
	}}
	opts.Discovery = disc

	if _, err := materialize.Materialize(context.Background(), opts); err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if disc.calls != 0 {
		t.Errorf("discovery ran %d times while the correlation rung won", disc.calls)
	}
}

// TestDiscoveryErrorIsReturned: "I could not look" is not "there is none",
// so a failing discoverer is an error, never an absence folded into the
// trail.
func TestDiscoveryErrorIsReturned(t *testing.T) {
	boom := errors.New("socket directory unreadable")
	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.Discovery = &fakeDiscovery{err: boom}

	_, err := materialize.Materialize(context.Background(), opts)
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want it to wrap the discovery failure", err)
	}
	var merr *materialize.Error
	if errors.As(err, &merr) {
		t.Errorf("a discovery failure was reported as %q, which claims the ladder came up empty", merr.Code)
	}
}

// --- the explicit flag ----------------------------------------------------

// TestExplicitKindOnlyResolvesThroughDiscovery: `--host <kind>` is the
// convenience form. It resolves through discovery like the default rung,
// but the provenance stays explicit-flag, because the operator chose the
// kind.
func TestExplicitKindOnlyResolvesThroughDiscovery(t *testing.T) {
	opts := baseOptions()
	opts.HostFlag = "herdr"
	opts.Correlations = boundCorrelations()
	opts.Discovery = &fakeDiscovery{byKind: map[string][]materialize.Instance{
		"herdr": {{Locator: foundSocket, InstanceID: "herdr:only"}},
	}}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.Host().Source != domain.HostSourceExplicitFlag {
		t.Errorf("host_source = %q, want explicit-flag", got.Host().Source)
	}
	if got.Host().Instance != foundSocket || got.Host().InstanceID != "herdr:only" {
		t.Errorf("host = %+v, want the discovered instance", got.Host())
	}
}

// TestUnresolvableExplicitFlagStopsTheLadder: an operator who named a host
// and did not get it must be told, never quietly handed the correlation
// they were overriding.
func TestUnresolvableExplicitFlagStopsTheLadder(t *testing.T) {
	opts := baseOptions()
	opts.HostFlag = "herdr"
	opts.Correlations = boundCorrelations()
	opts.Discovery = &fakeDiscovery{byKind: map[string][]materialize.Instance{
		"herdr": {{Locator: "/run/a.sock"}, {Locator: "/run/b.sock"}},
	}}

	_, err := materialize.Materialize(context.Background(), opts)
	var merr *materialize.Error
	if !errors.As(err, &merr) {
		t.Fatalf("error = %v, want a *materialize.Error", err)
	}
	trail := merr.Result().Trail()
	flagRung := rungFor(t, trail, domain.HostSourceExplicitFlag)
	if !flagRung.Consulted || flagRung.YieldedHost {
		t.Errorf("flag rung = %+v, want consulted and not yielding", flagRung)
	}
	for _, source := range []domain.HostSource{
		domain.HostSourceWorkspaceCorrelation,
		domain.HostSourceCwdCorrelation,
		domain.HostSourceAmbientEnv,
		domain.HostSourcePolicyDefault,
	} {
		r := rungFor(t, trail, source)
		if r.Consulted {
			t.Errorf("rung %s was consulted after the explicit flag failed: %+v", source, r)
		}
		if !strings.Contains(r.Detail, "explicit --host flag could not be resolved") {
			t.Errorf("rung %s detail = %q", source, r.Detail)
		}
	}
}

// --- the failure ----------------------------------------------------------

// TestHostUnresolvedCarriesTheFullTrail is the contract shape: class
// unavailable, effect no_effect, one rung per rank in order, the enabled
// kinds, and the pointer set. It mirrors
// fixtures/duo-external-v1/session-launch-host-unresolved.json.
func TestHostUnresolvedCarriesTheFullTrail(t *testing.T) {
	opts := baseOptions()
	opts.Policy = config.SessionHostPolicy{}
	opts.Correlations = unboundCorrelations()

	_, err := materialize.Materialize(context.Background(), opts)
	var merr *materialize.Error
	if !errors.As(err, &merr) {
		t.Fatalf("error = %v, want a *materialize.Error", err)
	}
	if merr.Code != "launch.host_unresolved" {
		t.Errorf("code = %q", merr.Code)
	}
	if merr.Class != "unavailable" {
		t.Errorf("class = %q, want unavailable (the registered class)", merr.Class)
	}
	if merr.Effect != "no_effect" {
		t.Errorf("effect = %q, want no_effect", merr.Effect)
	}
	if merr.Retry != (materialize.Retry{Safe: true, Action: "supply_host_or_enable_kind"}) {
		t.Errorf("retry = %+v", merr.Retry)
	}

	details, ok := merr.Details.(materialize.HostUnresolvedDetails)
	if !ok {
		t.Fatalf("details are %T, want HostUnresolvedDetails", merr.Details)
	}
	if details.RequestedPreset != "review" {
		t.Errorf("requested_preset = %q", details.RequestedPreset)
	}
	if details.WorkspaceID != string(workspaceID) {
		t.Errorf("workspace_id = %q", details.WorkspaceID)
	}
	if len(details.DeductionTrail) != len(materialize.Rungs) {
		t.Fatalf("deduction_trail has %d rungs, want %d", len(details.DeductionTrail), len(materialize.Rungs))
	}
	for i, rung := range details.DeductionTrail {
		if rung.Source != string(materialize.Rungs[i]) {
			t.Errorf("trail[%d].source = %q, want %q", i, rung.Source, materialize.Rungs[i])
		}
		if rung.YieldedHost {
			t.Errorf("trail[%d] claims a host: %+v", i, rung)
		}
		if rung.Detail == "" {
			t.Errorf("trail[%d] explains nothing", i)
		}
	}
	if details.DeductionTrail[0].Consulted {
		t.Errorf("the flag rung is consulted with no flag supplied")
	}
	if details.DeductionTrail[0].Detail != "no --host flag supplied" {
		t.Errorf("flag rung detail = %q", details.DeductionTrail[0].Detail)
	}
	if details.DeductionTrail[4].Detail != "no enabled kind in session_hosts.prefer" {
		t.Errorf("policy-default detail = %q", details.DeductionTrail[4].Detail)
	}
	if details.EnabledKinds == nil || len(details.EnabledKinds) != 0 {
		t.Errorf("enabled_kinds = %#v, want an empty array", details.EnabledKinds)
	}
	if details.Pointers.OverrideFlag != "--host" {
		t.Errorf("pointers.override_flag = %q", details.Pointers.OverrideFlag)
	}
	if details.Pointers.WorkspaceHostRebind != "duo workspace host rebind" {
		t.Errorf("pointers.workspace_host_rebind = %q", details.Pointers.WorkspaceHostRebind)
	}
	if details.Pointers.ProviderEnable != "" {
		t.Errorf("provider enablement is not a way out of an unresolved host, but is pointed at")
	}

	// enabled_kinds must survive JSON as [] rather than null: the schema
	// types it as an array, and a null would read as "unknown".
	blob, err := json.Marshal(details)
	if err != nil {
		t.Fatalf("marshaling details: %v", err)
	}
	if !strings.Contains(string(blob), `"enabled_kinds":[]`) {
		t.Errorf("marshaled details = %s", blob)
	}
	if !strings.Contains(string(blob), `"yielded_host":false`) {
		t.Errorf("marshaled trail omits yielded_host: %s", blob)
	}
}

// TestHostUnresolvedNamesTheEnabledKinds: when a kind is enabled but has no
// discoverable instance, the payload still says which kinds were in play —
// otherwise "no host" and "no instance of the host you have" read the same.
func TestHostUnresolvedNamesTheEnabledKinds(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = unboundCorrelations()
	opts.Discovery = &fakeDiscovery{}

	_, err := materialize.Materialize(context.Background(), opts)
	var merr *materialize.Error
	if !errors.As(err, &merr) {
		t.Fatalf("error = %v, want a *materialize.Error", err)
	}
	details := merr.Details.(materialize.HostUnresolvedDetails)
	if !reflect.DeepEqual(details.EnabledKinds, []string{"herdr"}) {
		t.Errorf("enabled_kinds = %#v, want [herdr]", details.EnabledKinds)
	}
}

// --- M2 -------------------------------------------------------------------

// TestProviderSnapshotIsTakenByFactID is M2: the resolver eliminates a
// provider-disabled variant against this snapshot, and the record cites the
// same fact IDs afterwards.
func TestProviderSnapshotIsTakenByFactID(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = boundCorrelations()
	opts.Providers = fakeProviders{
		"anthropic": {Enabled: false, FactID: "fact_disable_1"},
		"openai":    {Enabled: true, FactID: "fact_enable_9"},
	}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	bundle := got.Bundle()
	if id, ok := bundle.ProviderDisabled("anthropic"); !ok || id != "fact_disable_1" {
		t.Errorf("ProviderDisabled(anthropic) = %q, %v; want fact_disable_1, true", id, ok)
	}
	if _, ok := bundle.ProviderDisabled("openai"); ok {
		t.Errorf("an enabled provider reads as disabled")
	}
	// A name with no standing fact is enabled by default; the snapshot says
	// "no fact", and the reader applies the rule.
	if _, ok := bundle.ProviderStanding("mystery"); ok {
		t.Errorf("an unmentioned provider has a standing fact")
	}
	if _, ok := bundle.ProviderDisabled("mystery"); ok {
		t.Errorf("an unmentioned provider reads as disabled")
	}
	want := []domain.FactID{"fact_disable_1", "fact_enable_9"}
	if !reflect.DeepEqual(bundle.ProviderFactIDs(), want) {
		t.Errorf("provider fact ids = %v, want %v", bundle.ProviderFactIDs(), want)
	}
}

// TestNilProviderSourceIsAnEmptySnapshot: no read model means no standing
// fact, which by the default-enabled rule disables nothing.
func TestNilProviderSourceIsAnEmptySnapshot(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = boundCorrelations()

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if len(got.Bundle().ProviderFactIDs()) != 0 {
		t.Errorf("provider fact ids = %v, want none", got.Bundle().ProviderFactIDs())
	}
	if _, ok := got.Bundle().ProviderDisabled("anthropic"); ok {
		t.Errorf("an empty snapshot disabled a provider")
	}
}

// --- immutability ---------------------------------------------------------

// TestBundleCannotBeMutatedThroughItsAPI is invariant I-3's other half: the
// resolver's purity rests on facts that cannot change under it. Every
// accessor hands back a copy, so a caller that writes to what it got writes
// only to its own copy.
func TestBundleCannotBeMutatedThroughItsAPI(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = boundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{
		"HERDR_SOCKET_PATH": envSocket,
		"HERDR_SESSION":     "ambient",
	}).lookup
	opts.Providers = fakeProviders{"anthropic": {Enabled: false, FactID: "fact_disable_1"}}

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	bundle := got.Bundle()

	captures := bundle.AmbientCaptures()
	captures[0].Value = "tampered"
	captures[0].Name = "TAMPERED"
	if again := bundle.AmbientCaptures(); again[0].Value != envSocket || again[0].Name != "HERDR_SOCKET_PATH" {
		t.Errorf("mutating a returned capture changed the bundle: %+v", again[0])
	}

	standings := bundle.ProviderStandings()
	standings["anthropic"] = domain.ProviderStanding{Enabled: true}
	standings["injected"] = domain.ProviderStanding{Enabled: false, FactID: "fact_fake"}
	if _, ok := bundle.ProviderDisabled("anthropic"); !ok {
		t.Errorf("mutating the returned standings map re-enabled a provider")
	}
	if _, ok := bundle.ProviderStanding("injected"); ok {
		t.Errorf("a name injected into the returned map reached the bundle")
	}

	ids := bundle.ProviderFactIDs()
	if len(ids) > 0 {
		ids[0] = "fact_tampered"
		if bundle.ProviderFactIDs()[0] != "fact_disable_1" {
			t.Errorf("mutating the returned fact IDs changed the bundle")
		}
	}

	// The bundle is a value, so a copy taken before the tampering above is
	// still the same bundle.
	if id, ok := got.Bundle().CorrelationFactID(); !ok || id != "fact_bind_1" {
		t.Errorf("correlation fact id = %q (%v) after tampering", id, ok)
	}
}

// TestResultAccessorsReturnCopies extends the same rule to the trail and
// the outranked evidence: the launch record is written from these, and a
// caller must not be able to edit the explanation.
func TestResultAccessorsReturnCopies(t *testing.T) {
	opts := baseOptions()
	opts.Correlations = boundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{"HERDR_SOCKET_PATH": envSocket}).lookup

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	trail := got.Trail()
	trail[0].Detail = "tampered"
	trail[0].YieldedHost = true
	if again := got.Trail(); again[0].Detail == "tampered" || again[0].YieldedHost {
		t.Errorf("mutating the returned trail changed the result: %+v", again[0])
	}

	outranked := got.OutrankedEvidence()
	if len(outranked) == 0 {
		t.Fatal("expected outranked ambient evidence")
	}
	outranked[0].Detail = "tampered"
	if len(outranked[0].Captures) > 0 {
		outranked[0].Captures[0].Value = "tampered"
	}
	again := got.OutrankedEvidence()
	if again[0].Detail == "tampered" {
		t.Errorf("mutating the returned outranked evidence changed the result")
	}
	if len(again[0].Captures) > 0 && again[0].Captures[0].Value == "tampered" {
		t.Errorf("mutating a returned outranked capture changed the result")
	}

	// The deduced host is a value type with no reference fields, so a
	// caller's edit cannot reach back either.
	host := got.Host()
	host.Instance = "tampered"
	if got.Host().Instance == "tampered" {
		t.Errorf("mutating the returned host changed the result")
	}
}

// --- the ambient table ----------------------------------------------------

// TestAmbientHerdrRowMatchesTheAdapterConvention pins the one place this
// package spells an adapter's convention by hand. If Herdr's
// integration-instance ID convention ever changes, this fails here rather
// than silently minting IDs nothing else recognizes.
func TestAmbientHerdrRowMatchesTheAdapterConvention(t *testing.T) {
	var row materialize.AmbientSource
	for _, s := range materialize.DefaultAmbientSources {
		if s.Kind == "herdr" {
			row = s
		}
	}
	if row.Kind == "" {
		t.Fatal("the default ambient table has no herdr row")
	}
	if got, want := row.InstanceIDPrefix+"work", herdr.InstanceIDForSession("work"); got != want {
		t.Errorf("ambient instance id = %q, want herdr.InstanceIDForSession's %q", got, want)
	}
	if row.LocatorVar != "HERDR_SOCKET_PATH" || row.InstanceVar != "HERDR_SESSION" {
		t.Errorf("herdr ambient row = %+v", row)
	}
}

// TestAmbientVariableNamesAreDeduplicated: the exactly-once guarantee is
// per variable name, not per table row, so two kinds naming one variable
// still read it once.
func TestAmbientVariableNamesAreDeduplicated(t *testing.T) {
	sources := []materialize.AmbientSource{
		{Kind: "a", LocatorVar: "SHARED", InstanceVar: "A_SESSION"},
		{Kind: "b", LocatorVar: "SHARED", InstanceVar: "B_SESSION"},
	}
	want := []string{"SHARED", "A_SESSION", "B_SESSION"}
	if got := materialize.AmbientVariableNames(sources); !reflect.DeepEqual(got, want) {
		t.Errorf("names = %v, want %v", got, want)
	}
}

// TestDeduceSourceForCoversEveryRung: every rung below the flag has a
// `deduce` key, and the flag rung deliberately has none.
func TestDeduceSourceForCoversEveryRung(t *testing.T) {
	want := map[domain.HostSource]string{
		domain.HostSourceExplicitFlag:         "",
		domain.HostSourceWorkspaceCorrelation: "workspace",
		domain.HostSourceCwdCorrelation:       "cwd",
		domain.HostSourceAmbientEnv:           "env",
		domain.HostSourcePolicyDefault:        "default",
	}
	for _, rung := range materialize.Rungs {
		if got := materialize.DeduceSourceFor(rung); got != want[rung] {
			t.Errorf("DeduceSourceFor(%s) = %q, want %q", rung, got, want[rung])
		}
	}
	if !reflect.DeepEqual(materialize.Rungs, domain.HostSources) {
		t.Errorf("the package ranking and the domain vocabulary disagree:\n %v\n %v",
			materialize.Rungs, domain.HostSources)
	}
}

// --- workspace resolution -------------------------------------------------

// TestWorkspaceFallsBackToTheWorkingDirectory keeps the existing
// `--workspace` > cwd precedence that session_launch.go already had.
func TestWorkspaceFallsBackToTheWorkingDirectory(t *testing.T) {
	opts := baseOptions()
	opts.WorkspaceFlag = ""
	opts.Getwd = func() (string, error) { return workspacePath, nil }
	opts.Correlations = boundCorrelations()

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.WorkspacePath() != workspacePath {
		t.Errorf("workspace path = %q, want the working directory", got.WorkspacePath())
	}
	if got.WorkspaceID() != workspaceID {
		t.Errorf("workspace id = %q", got.WorkspaceID())
	}
}

// TestUnenrolledWorkspaceIsNotAFailure: a launch into a directory Duo has
// never seen is ordinary — the launch record's own commit enrolls it — so
// the correlation rung reports the absence and the ladder continues.
func TestUnenrolledWorkspaceIsNotAFailure(t *testing.T) {
	opts := baseOptions()
	opts.WorkspaceFlag = "/somewhere/else"
	opts.Correlations = boundCorrelations()
	opts.LookupEnv = newEnv(map[string]string{"HERDR_SOCKET_PATH": envSocket}).lookup

	got, err := materialize.Materialize(context.Background(), opts)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if got.WorkspaceID() != "" {
		t.Errorf("workspace id = %q, want empty", got.WorkspaceID())
	}
	if got.Host().Source != domain.HostSourceAmbientEnv {
		t.Errorf("host_source = %q, want ambient-env", got.Host().Source)
	}
	rung := rungFor(t, got.Trail(), domain.HostSourceWorkspaceCorrelation)
	if !rung.Consulted || rung.YieldedHost {
		t.Errorf("correlation rung = %+v", rung)
	}
	if !strings.Contains(rung.Detail, "not enrolled") {
		t.Errorf("correlation rung detail = %q", rung.Detail)
	}
}
