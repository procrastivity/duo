package launch_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// The deduced host every test resolves against, unless it says otherwise.
// Under duo.config/v3 the host is not in the document at all: it is
// materialized (M1) before resolution and handed to the resolver as a
// value, so a test fixes it by fixing the materialization inputs rather
// than by writing a session_hosts declaration.
const (
	testHostKind       = "tmux"
	testHostInstance   = "/run/tmux-1000/default"
	testHostInstanceID = "local_tmux"
	testHostVersion    = "3.5a"
)

// scenarioYAML is notes/30 §6.5's accepted configuration shape carried onto
// duo.config/v3: the compositions are gone, the preset's candidates name
// launch variants directly, every variant carries the required model_family,
// and the command shape (executable plus base arguments) has moved onto the
// agent runtime, leaving the variant only append_arguments.
//
// The declared model lines and runtime kinds are deliberately unchanged from
// the v2 scenario, so
// contracts/fixtures/duo-external-v1/session-launch.json still describes the
// same ordinary result: three ordered candidates under one leaf named
// `reviewer`, whose first candidate is codex on gpt-5.6.
//
// The provider tags are v3's addition. `review_opencode_gpt56` is
// deliberately left untagged, because "an untagged variant is never affected
// by a provider fact" is a rule that needs an untagged variant to test it.
const scenarioYAML = `
schema: duo.config/v3
workspaces:
  main:
    root: /work/example
session_hosts:
  prefer: [tmux]
agent_runtimes:
  codex_default:
    kind: codex
    executable: codex
  opencode_default:
    kind: opencode
    executable: opencode
  claude_default:
    kind: claude
    executable: claude
    arguments: ["--continue"]
launch_variants:
  review_codex_gpt56:
    agent_runtime: codex_default
    model_line: gpt-5.6
    model_family: gpt
    provider: openai
  review_opencode_gpt56:
    agent_runtime: opencode_default
    model_line: gpt-5.6
    model_family: gpt
  review_claude_opus4:
    agent_runtime: claude_default
    model_line: claude-opus-4
    model_family: claude
    provider: anthropic
presets:
  review:
    selection: ordered
    leaves:
      reviewer:
        candidates:
          - variant: review_codex_gpt56
          - variant: review_opencode_gpt56
          - variant: review_claude_opus4
  review_random:
    selection: random
    leaves:
      reviewer:
        candidates:
          - variant: review_codex_gpt56
          - variant: review_opencode_gpt56
          - variant: review_claude_opus4
  determined_review:
    selection: ordered
    leaves:
      reviewer:
        candidates:
          - variant: review_codex_gpt56
  adversarial_pair:
    selection: ordered
    leaves:
      first:
        candidates:
          - variant: review_codex_gpt56
          - variant: review_opencode_gpt56
          - variant: review_claude_opus4
      second:
        candidates:
          - variant: review_codex_gpt56
          - variant: review_opencode_gpt56
          - variant: review_claude_opus4
    relations:
      - kind: distinct_model_line
        leaves: [first, second]
  distinct_family_pair:
    selection: ordered
    leaves:
      first:
        candidates:
          - variant: review_codex_gpt56
          - variant: review_opencode_gpt56
          - variant: review_claude_opus4
      second:
        candidates:
          - variant: review_codex_gpt56
          - variant: review_opencode_gpt56
          - variant: review_claude_opus4
    relations:
      - kind: distinct_model_family
        leaves: [first, second]
  mixed_pair:
    selection: ordered
    leaves:
      alpha:
        candidates:
          - variant: review_codex_gpt56
      beta:
        candidates:
          - variant: review_claude_opus4
`

// exhaustionYAML is the declaration
// contracts/fixtures/duo-external-v1/session-launch-exhausted.json reports
// on, carried onto v3: a `review` preset whose one leaf `reviewer` declares
// exactly one candidate. The fixture's survivor pools list that one
// locator, which is only true of a one-candidate declaration.
const exhaustionYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [tmux]
agent_runtimes:
  codex_default:
    kind: codex
    executable: codex
launch_variants:
  review_codex_gpt56:
    agent_runtime: codex_default
    model_line: gpt-5.6
    model_family: gpt
presets:
  review:
    selection: ordered
    leaves:
      reviewer:
        candidates:
          - variant: review_codex_gpt56
`

// parseDoc resolves one duo.config/v3 document through the real strict
// resolver. Every test starts from a document internal/config accepted, so
// nothing here can pass on a shape the configuration boundary would reject.
func parseDoc(t *testing.T, yaml string) config.DocumentV3 {
	t.Helper()
	doc, err := config.ParseV3([]byte(yaml))
	if err != nil {
		t.Fatalf("config.ParseV3: %v", err)
	}
	return doc
}

// fixedDiscovery is an instance discoverer that reports one instance of one
// kind. It is what lets the policy-default rung of M1 produce a host with a
// known instance ID in a test, with no environment and no store.
type fixedDiscovery struct {
	kind      string
	instances []materialize.Instance
}

func (d fixedDiscovery) DiscoverInstances(_ context.Context, kind string) ([]materialize.Instance, error) {
	if kind != d.kind {
		return nil, nil
	}
	return d.instances, nil
}

// fixedProviders is a standing-provider read model. M2 snapshots it once,
// and the resolver reads the snapshot, never this.
type fixedProviders struct {
	standing map[string]domain.ProviderStanding
}

func (p fixedProviders) StandingProviderFacts() map[string]domain.ProviderStanding {
	return p.standing
}

// disabledProviders builds a read model in which every named provider is
// standing-disabled, each with its own fact ID.
func disabledProviders(names ...string) fixedProviders {
	standing := map[string]domain.ProviderStanding{}
	for _, name := range names {
		standing[name] = domain.ProviderStanding{Enabled: false, FactID: domain.FactID("f_disabled_" + name)}
	}
	return fixedProviders{standing: standing}
}

// materialized runs the real M1/M2 pass with every outside input pinned:
// no environment, no correlation store, and one discoverable instance of
// the scenario's host kind. The result is what a v3 resolver is built over.
func materialized(t *testing.T, doc config.DocumentV3, opts ...func(*materialize.Options)) materialize.Result {
	t.Helper()
	o := materialize.Options{
		WorkspaceFlag: "/work/example",
		Policy:        doc.SessionHosts,
		Discovery: fixedDiscovery{kind: testHostKind, instances: []materialize.Instance{
			{Locator: testHostInstance, InstanceID: testHostInstanceID},
		}},
		LookupEnv: func(string) (string, bool) { return "", false },
		Now:       fixedClock(),
	}
	for _, apply := range opts {
		apply(&o)
	}
	res, err := materialize.Materialize(context.Background(), o)
	if err != nil {
		t.Fatalf("materialize.Materialize: %v", err)
	}
	return res
}

// fixedIDs mints predictable record IDs so a test can compare a whole
// record or a whole envelope.
func fixedIDs(ids ...string) func() (string, error) {
	i := 0
	return func() (string, error) {
		if i >= len(ids) {
			return "lrr_test_overflow", nil
		}
		id := ids[i]
		i++
		return id, nil
	}
}

// fixedClock pins the record's timestamp so two resolutions of the same
// request are comparable as whole documents.
func fixedClock() func() time.Time {
	return func() time.Time { return time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC) }
}

// defaultOptions are the resolver options every test starts from: the
// permissive installed-evidence oracle, a pinned ID, and the pinned host
// version the SupportKey is built from.
func defaultOptions() launch.Options {
	return launch.Options{
		Support:      launch.AllSupported{RecordDigest: "sha256:conformance-fixture"},
		NewID:        fixedIDs("lrr_test_1", "lrr_test_2", "lrr_test_3"),
		HostVersions: map[string]string{testHostKind: testHostVersion},
	}
}

// newResolver builds a resolver over one document and the default
// materialization.
func newResolver(t *testing.T, yaml string, opts ...func(*launch.Options)) *launch.Resolver {
	t.Helper()
	doc := parseDoc(t, yaml)
	return newResolverOver(t, doc, materialized(t, doc), opts...)
}

// newResolverOver builds a resolver over an explicit materialization, for
// the tests that are about what M1/M2 produced.
func newResolverOver(t *testing.T, doc config.DocumentV3, mat materialize.Result, opts ...func(*launch.Options)) *launch.Resolver {
	t.Helper()
	o := defaultOptions()
	for _, apply := range opts {
		apply(&o)
	}
	r, err := launch.NewResolver(doc, mat, o)
	if err != nil {
		t.Fatalf("launch.NewResolver: %v", err)
	}
	return r
}

// resolveErr resolves and requires a *launch.Error.
func resolveErr(t *testing.T, r *launch.Resolver, req launch.Request) *launch.Error {
	t.Helper()
	res, err := r.Resolve(req)
	if err == nil {
		t.Fatalf("Resolve(%q) succeeded with %+v, want a failure", req.Preset, res.Report())
	}
	var le *launch.Error
	if !asLaunchError(err, &le) {
		t.Fatalf("Resolve(%q) returned %T (%v), want *launch.Error", req.Preset, err, err)
	}
	return le
}

func asLaunchError(err error, target **launch.Error) bool {
	le, ok := err.(*launch.Error)
	if ok {
		*target = le
	}
	return ok
}

// resolveOK resolves and requires success.
func resolveOK(t *testing.T, r *launch.Resolver, req launch.Request) *launch.Resolution {
	t.Helper()
	res, err := r.Resolve(req)
	if err != nil {
		t.Fatalf("Resolve(%q): %v", req.Preset, err)
	}
	return res
}

// canonical round-trips v through JSON into the plain map/slice shape
// internal/conformance calls Canonical, so two values can be compared for
// equality without a hand-written field walk.
func canonical(t *testing.T, v any) map[string]any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling %T: %v", v, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("decoding %T: %v", v, err)
	}
	return out
}

// callLog records the order of the interactions the pre-spawn gate is about.
type callLog struct {
	entries []string
}

func (l *callLog) add(entry string) { l.entries = append(l.entries, entry) }

// recordingRecorder is a Recorder that logs its commit and can be made to
// fail.
type recordingRecorder struct {
	log         *callLog
	fail        error
	sessionID   string
	instanceIDs []string
	committed   []launch.Record
}

func (r *recordingRecorder) CommitLaunchResolution(_ context.Context, rec launch.Record) (launch.Commit, error) {
	r.log.add("commit:" + rec.ID)
	if r.fail != nil {
		return launch.Commit{}, r.fail
	}
	r.committed = append(r.committed, rec)
	return launch.Commit{SessionID: r.sessionID, InstanceIDs: r.instanceIDs}, nil
}

// recordingHost wraps the first-class fake host adapter and logs the two
// HostLauncher calls. It wraps rather than replaces: the launch tuple still
// has to satisfy the real fake's integration-instance check, so a wrong
// tuple fails here the way it would against a host adapter.
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

// oneHost is a HostSet with a single launcher behind it.
type oneHost struct {
	launcher host.HostLauncher
}

func (s oneHost) LauncherFor(launch.Tuple) (host.HostLauncher, error) { return s.launcher, nil }

// newLauncher wires a resolver to a recording recorder and a recording host
// over the fake adapter, returning the launcher and the shared call log.
func newLauncher(t *testing.T, r *launch.Resolver, recorder *recordingRecorder, log *callLog) *launch.Launcher {
	t.Helper()
	h := &recordingHost{inner: fake.New(testHostInstanceID), log: log}
	l, err := launch.NewLauncher(r, recorder, oneHost{launcher: h})
	if err != nil {
		t.Fatalf("launch.NewLauncher: %v", err)
	}
	return l
}
