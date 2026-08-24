package launchrecord_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
	"github.com/procrastivity/duo/internal/launchrecord"
	"github.com/procrastivity/duo/internal/store"
)

// scenarioYAML is the same shape internal/launch's own tests resolve
// against, trimmed to what these tests need: one ordered preset whose leaf
// declares two launch-variant candidates. Under duo.config/v3 the session
// host is not in the document at all — it is materialized before
// resolution and joined to every candidate (see newHarness).
const scenarioYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [tmux]
agent_runtimes:
  codex_default:
    kind: codex
    executable: codex
  claude_default:
    kind: claude
    executable: claude
    arguments: ["--continue"]
launch_variants:
  review_codex_gpt56:
    agent_runtime: codex_default
    model_line: gpt-5.6
    model_family: gpt
  review_claude_opus4:
    agent_runtime: claude_default
    model_line: claude-opus-4
    model_family: claude
presets:
  review:
    selection: ordered
    leaves:
      reviewer:
        candidates:
          - variant: review_codex_gpt56
          - variant: review_claude_opus4
`

const workspacePath = "/work/example"

// callLog records the order of the interactions the pre-spawn gate is
// about. It is internal/launch's own pattern: the durable boundary and the
// host adapter append to one slice, so the assertion is on the real
// interleaving rather than on two counters.
type callLog struct{ entries []string }

func (l *callLog) add(entry string) { l.entries = append(l.entries, entry) }

// loggingRepo wraps the real storerepo and logs the launch-resolution
// boundary, naming the record the Change carries. It logs and delegates —
// the transaction, its atomicity, and the durable log are the real ones.
type loggingRepo struct {
	inner domain.Repository
	log   *callLog
}

func (r *loggingRepo) Load(ctx context.Context) ([]domain.Fact, error) { return r.inner.Load(ctx) }

func (r *loggingRepo) CommitIdentity(ctx context.Context, c domain.Change) error {
	return r.inner.CommitIdentity(ctx, c)
}

func (r *loggingRepo) CommitLaunchResolution(ctx context.Context, c domain.Change) error {
	r.log.add("commit:" + recordIDIn(c))
	return r.inner.CommitLaunchResolution(ctx, c)
}

func (r *loggingRepo) CommitCommandAcceptance(ctx context.Context, c domain.Change) error {
	return r.inner.CommitCommandAcceptance(ctx, c)
}

func (r *loggingRepo) CommitObservation(ctx context.Context, c domain.Change) error {
	return r.inner.CommitObservation(ctx, c)
}

// Incarnation forwards the writer incarnation, so the wrapped kernel stamps
// its facts exactly as it would over the bare repository.
func (r *loggingRepo) Incarnation() string {
	if reporter, ok := r.inner.(domain.IncarnationReporter); ok {
		return reporter.Incarnation()
	}
	return ""
}

// recordIDIn finds the launch-resolution record a Change commits.
func recordIDIn(c domain.Change) string {
	for _, f := range c.Facts {
		if f.Kind == domain.FactLaunchResolved && f.LaunchResolution != nil {
			return string(f.LaunchResolution.ID)
		}
	}
	return "none"
}

// recordingHost wraps the first-class fake host adapter and logs the two
// HostLauncher calls. It wraps rather than replaces: the launch tuple still
// has to satisfy the real fake's integration-instance check.
type recordingHost struct {
	inner *fake.Host
	log   *callLog
}

func (h *recordingHost) PrepareLaunch(ctx context.Context, req host.HostLaunchRequest) (host.PreparedHostLaunch, error) {
	h.log.add("prepare:" + req.ResolvedLaunchTuple.LaunchResolutionID)
	return h.inner.PrepareLaunch(ctx, req)
}

func (h *recordingHost) Start(ctx context.Context, prepared host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	h.log.add("start:" + prepared.LaunchResolutionID)
	return h.inner.Start(ctx, prepared)
}

// fixedDiscovery is an instance discoverer that reports one instance of one
// kind, which is what lets M1's policy-default rung produce a host with a
// known instance ID without an environment or a store.
type fixedDiscovery struct {
	kind     string
	instance materialize.Instance
}

func (d fixedDiscovery) DiscoverInstances(_ context.Context, kind string) ([]materialize.Instance, error) {
	if kind != d.kind {
		return nil, nil
	}
	return []materialize.Instance{d.instance}, nil
}

// oneHost is a HostSet with a single launcher behind it.
type oneHost struct{ launcher host.HostLauncher }

func (s oneHost) LauncherFor(launch.Tuple) (host.HostLauncher, error) { return s.launcher, nil }

// harness is one fully wired launch path: a real store, the real
// repository, the kernel, the real launchrecord.Recorder, the real
// resolver, and the fake host adapter — with one call log through the
// middle of it.
type harness struct {
	launcher  *launch.Launcher
	authority *domain.Authority
	store     *store.Store
	log       *callLog
	launched  []domain.LaunchResult
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()

	s, err := store.OpenAuthority(filepath.Join(t.TempDir(), "duo.db"))
	if err != nil {
		t.Fatalf("store.OpenAuthority: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	h := &harness{store: s, log: &callLog{}}
	authority, err := domain.Open(ctx, &loggingRepo{inner: storerepo.New(s), log: h.log})
	if err != nil {
		t.Fatalf("domain.Open: %v", err)
	}
	h.authority = authority

	recorder, err := launchrecord.New(authority, launchrecord.Options{
		WorkspacePath: workspacePath,
		Actor:         "user:beau",
		OnLaunch:      func(res domain.LaunchResult) { h.launched = append(h.launched, res) },
	})
	if err != nil {
		t.Fatalf("launchrecord.New: %v", err)
	}

	doc, err := config.ParseV3([]byte(scenarioYAML))
	if err != nil {
		t.Fatalf("config.ParseV3: %v", err)
	}
	// M1/M2 run before resolution and hand the resolver one deduced host.
	// Everything that touches the outside world is pinned: no environment,
	// no correlation store, one discoverable instance.
	mat, err := materialize.Materialize(ctx, materialize.Options{
		WorkspaceFlag: workspacePath,
		Policy:        doc.SessionHosts,
		Discovery:     fixedDiscovery{kind: "tmux", instance: materialize.Instance{Locator: "/run/tmux-1000/default", InstanceID: "local_tmux"}},
		LookupEnv:     func(string) (string, bool) { return "", false },
	})
	if err != nil {
		t.Fatalf("materialize.Materialize: %v", err)
	}
	resolver, err := launch.NewResolver(doc, mat, launch.Options{
		Support:      launch.AllSupported{RecordDigest: "sha256:conformance-fixture"},
		NewID:        func() (string, error) { return "lrr_test_1", nil },
		HostVersions: map[string]string{"tmux": "3.5a"},
	})
	if err != nil {
		t.Fatalf("launch.NewResolver: %v", err)
	}
	launcher, err := launch.NewLauncher(resolver, recorder,
		oneHost{launcher: &recordingHost{inner: fake.New("local_tmux"), log: h.log}})
	if err != nil {
		t.Fatalf("launch.NewLauncher: %v", err)
	}
	h.launcher = launcher
	return h
}

// factsRecorded reports how many domain facts the durable log holds.
func factsRecorded(t *testing.T, s *store.Store) int {
	t.Helper()
	items, err := s.ReadStream(context.Background(), "duo.domain.fact/v1", 0, 1000)
	if err != nil {
		t.Fatalf("ReadStream: %v", err)
	}
	return len(items)
}

// TestRecordCommitsBeforePrepareLaunch is the ordering property end to end,
// with nothing faked between the resolver and SQLite:
// duo-vnext-go-architecture.md §5.2's "records the launch-resolution record
// before any HostLauncher.PrepareLaunch call", observed through the real
// domain boundary rather than a test-double recorder.
func TestRecordCommitsBeforePrepareLaunch(t *testing.T) {
	h := newHarness(t)

	result, err := h.launcher.Launch(context.Background(), launch.SpawnRequest{
		Request:       launch.Request{Preset: "review"},
		WorkspacePath: workspacePath,
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	want := []string{"commit:lrr_test_1", "prepare:lrr_test_1", "start:lrr_test_1"}
	if !reflect.DeepEqual(h.log.entries, want) {
		t.Errorf("call order = %v, want %v", h.log.entries, want)
	}

	// The commit created the session and instance the launch package's
	// Commit promises, and both reached the record and the ordinary result.
	if len(h.launched) != 1 {
		t.Fatalf("kernel launches = %d, want 1", len(h.launched))
	}
	launched := h.launched[0]
	if result.Record.SessionID != string(launched.Session) {
		t.Errorf("record session = %q, want %q", result.Record.SessionID, launched.Session)
	}
	if result.Report.SessionID != string(launched.Session) {
		t.Errorf("report session_id = %q, want %q", result.Report.SessionID, launched.Session)
	}
	if !reflect.DeepEqual(result.Record.InstanceIDs, []string{string(launched.Instance)}) {
		t.Errorf("record instance ids = %v, want [%s]", result.Record.InstanceIDs, launched.Instance)
	}
	if launched.Credential == "" {
		t.Error("OnLaunch received no reporter credential; the kernel returns it exactly once")
	}

	// The kernel holds a real session, in the state a launch leaves it.
	session, ok := h.authority.Session(launched.Session)
	if !ok {
		t.Fatalf("session %s is not in the kernel", launched.Session)
	}
	if session.State != domain.SessionActive || session.Current != launched.Instance {
		t.Errorf("session = %+v, want an active session on instance %s", session, launched.Instance)
	}
	instance, _ := h.authority.Instance(launched.Instance)
	if instance.State != domain.InstanceStarting {
		t.Errorf("instance state = %q, want starting", instance.State)
	}
}

// TestCommittedRecordIsTheResolverRecord proves the body survives the whole
// path — resolver, kernel, SQLite, and a replay under a new authority — and
// still decodes to the resolution that was made. The kernel never decoded
// it; this test is the first thing that does.
func TestCommittedRecordIsTheResolverRecord(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()

	result, err := h.launcher.Launch(ctx, launch.SpawnRequest{
		Request:       launch.Request{Preset: "review"},
		WorkspacePath: workspacePath,
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	stored, ok := h.authority.LaunchResolution("lrr_test_1")
	if !ok {
		t.Fatal("the kernel holds no launch-resolution record")
	}
	replayed, err := domain.Open(ctx, storerepo.New(h.store))
	if err != nil {
		t.Fatalf("replaying the fact log: %v", err)
	}
	fromLog, ok := replayed.LaunchResolution("lrr_test_1")
	if !ok {
		t.Fatal("the record did not come back from the durable log")
	}
	if !bytes.Equal(stored.Body, fromLog.Body) {
		t.Fatalf("replayed body differs from the committed one:\n got %q\nwant %q", fromLog.Body, stored.Body)
	}

	var decoded launch.Record
	if err := json.Unmarshal(fromLog.Body, &decoded); err != nil {
		t.Fatalf("decoding the durable record: %v", err)
	}
	if decoded.ID != "lrr_test_1" || decoded.RequestedPreset != "review" {
		t.Errorf("decoded record = %+v, want the resolution's own record", decoded)
	}
	if len(decoded.Assignment) != 1 || decoded.Assignment[0].Tuple.AgentRuntime != "codex" {
		t.Errorf("decoded assignment = %+v, want the complete resolved plan", decoded.Assignment)
	}
	// The whole record is there, not a summary: the losing candidate and
	// the consulted digests are what §6.9 keeps the record for.
	if len(decoded.Leaves) != 1 || len(decoded.Leaves[0].Candidates) != 2 {
		t.Errorf("decoded leaves = %+v, want both declared candidates", decoded.Leaves)
	}
	if len(decoded.ConsultedRecordDigests) == 0 {
		t.Error("the durable record lost the consulted conformance digests")
	}

	// The body commits before the session exists, so the links live on the
	// fact; the launcher's in-memory copy is what carries them onward.
	if decoded.SessionID != "" {
		t.Errorf("durable body carries session %q; the links belong on the fact", decoded.SessionID)
	}
	if _, ok := replayed.SessionLaunchResolution(domain.SessionID(result.Report.SessionID)); !ok {
		t.Errorf("session %s has no launch-resolution record after replay", result.Report.SessionID)
	}
}

// TestFailedResolutionCommitsNothing is §6.8's "effect: no_effect" and
// §7.4's "a failed resolution creates no session and therefore no
// launch-resolution record" — asserted against the durable log, which is
// where it actually has to be true.
func TestFailedResolutionCommitsNothing(t *testing.T) {
	h := newHarness(t)

	_, err := h.launcher.Launch(context.Background(), launch.SpawnRequest{
		Request: launch.Request{
			Preset:  "review",
			Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "pi"}},
		},
		WorkspacePath: workspacePath,
	})
	var le *launch.Error
	if !errors.As(err, &le) || le.Code != launch.CodeConstraintsExhausted {
		t.Fatalf("Launch error = %v, want launch.constraints_exhausted", err)
	}

	if len(h.log.entries) != 0 {
		t.Errorf("a failed resolution produced calls %v, want none", h.log.entries)
	}
	if len(h.launched) != 0 {
		t.Errorf("a failed resolution launched %d sessions in the kernel", len(h.launched))
	}
	if n := len(h.authority.Sessions()); n != 0 {
		t.Errorf("a failed resolution left %d sessions, want none", n)
	}
	if _, ok := h.authority.LaunchResolution("lrr_test_1"); ok {
		t.Error("a failed resolution left a launch-resolution record")
	}
	if n := factsRecorded(t, h.store); n != 0 {
		t.Errorf("a failed resolution recorded %d durable facts, want none", n)
	}
}
