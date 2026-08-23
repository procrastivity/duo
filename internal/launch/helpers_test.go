package launch_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/launch"
)

// scenarioYAML is notes/30 §6.5's accepted configuration shape, verbatim
// for the parts §6.5 prints, plus the session-host, agent-runtime, and
// launch-variant declarations those compositions reference (§6.5's excerpt
// shows only compositions and presets, but a composition resolves through a
// launch variant to an agent runtime and a session host, and this package
// never infers any of them).
//
// The `review` preset is what
// contracts/fixtures/duo-external-v1/session-launch.json reports on: three
// ordered candidates under one leaf named `reviewer`, whose first candidate
// is codex on gpt-5.6.
const scenarioYAML = `
schema: duo.config/v2
workspaces:
  main:
    root: /work/example
session_hosts:
  local_tmux:
    kind: tmux
    server: default
agent_runtimes:
  codex_default:
    kind: codex
  opencode_default:
    kind: opencode
  claude_default:
    kind: claude
launch_variants:
  codex_in_tmux:
    agent_runtime: codex_default
    session_host: local_tmux
    executable: codex
    arguments: []
  opencode_in_tmux:
    agent_runtime: opencode_default
    session_host: local_tmux
    executable: opencode
    arguments: []
  claude_in_tmux:
    agent_runtime: claude_default
    session_host: local_tmux
    executable: claude
    arguments: ["--continue"]
compositions:
  review_codex_gpt56:
    launch_variant: codex_in_tmux
    model_line: gpt-5.6
  review_opencode_gpt56:
    launch_variant: opencode_in_tmux
    model_line: gpt-5.6
  review_claude_opus4:
    launch_variant: claude_in_tmux
    model_line: claude-opus-4
presets:
  review:
    selection: ordered
    leaves:
      reviewer:
        candidates:
          - composition: review_codex_gpt56
          - composition: review_opencode_gpt56
          - composition: review_claude_opus4
  review_random:
    selection: random
    leaves:
      reviewer:
        candidates:
          - composition: review_codex_gpt56
          - composition: review_opencode_gpt56
          - composition: review_claude_opus4
  determined_review:
    selection: ordered
    leaves:
      reviewer:
        candidates:
          - composition: review_codex_gpt56
  adversarial_pair:
    selection: ordered
    leaves:
      first:
        candidates:
          - composition: review_codex_gpt56
          - composition: review_opencode_gpt56
          - composition: review_claude_opus4
      second:
        candidates:
          - composition: review_codex_gpt56
          - composition: review_opencode_gpt56
          - composition: review_claude_opus4
    relations:
      - kind: distinct_model_line
        leaves: [first, second]
  mixed_pair:
    selection: ordered
    leaves:
      alpha:
        candidates:
          - composition: review_codex_gpt56
      beta:
        candidates:
          - composition: review_claude_opus4
`

// exhaustionYAML is the declaration
// contracts/fixtures/duo-external-v1/session-launch-exhausted.json reports
// on: a `review` preset whose one leaf `reviewer` declares exactly one
// candidate, `compositions.review_codex_gpt56`. The fixture's survivor
// pools list that one locator, which is only true of a one-candidate
// declaration.
const exhaustionYAML = `
schema: duo.config/v2
session_hosts:
  local_tmux:
    kind: tmux
agent_runtimes:
  codex_default:
    kind: codex
launch_variants:
  codex_in_tmux:
    agent_runtime: codex_default
    session_host: local_tmux
    executable: codex
    arguments: []
compositions:
  review_codex_gpt56:
    launch_variant: codex_in_tmux
    model_line: gpt-5.6
presets:
  review:
    selection: ordered
    leaves:
      reviewer:
        candidates:
          - composition: review_codex_gpt56
`

// parseDoc resolves one duo.config/v2 document through the real strict
// resolver. Every test starts from a document internal/config accepted, so
// nothing here can pass on a shape the configuration boundary would reject.
func parseDoc(t *testing.T, yaml string) config.DocumentV2 {
	t.Helper()
	doc, err := config.ParseV2([]byte(yaml))
	if err != nil {
		t.Fatalf("config.ParseV2: %v", err)
	}
	return doc
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

// newResolver builds a resolver over scenarioYAML with the permissive
// installed-evidence oracle and a pinned ID.
func newResolver(t *testing.T, yaml string, opts ...func(*launch.Options)) *launch.Resolver {
	t.Helper()
	o := launch.Options{
		Support: launch.AllSupported{RecordDigest: "sha256:conformance-fixture"},
		NewID:   fixedIDs("lrr_test_1", "lrr_test_2", "lrr_test_3"),
	}
	for _, apply := range opts {
		apply(&o)
	}
	r, err := launch.NewResolver(parseDoc(t, yaml), o)
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
	h := &recordingHost{inner: fake.New("local_tmux"), log: log}
	l, err := launch.NewLauncher(r, recorder, oneHost{launcher: h})
	if err != nil {
		t.Fatalf("launch.NewLauncher: %v", err)
	}
	return l
}
