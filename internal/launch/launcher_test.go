package launch_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/launch"
)

// newTestLauncher wires a launcher over scenarioYAML with a pinned record
// ID, returning the launcher, its recorder, and the shared call log.
func newTestLauncher(t *testing.T, yaml string, recorderFail error) (*launch.Launcher, *recordingRecorder, *callLog) {
	t.Helper()
	log := &callLog{}
	recorder := &recordingRecorder{
		log:         log,
		fail:        recorderFail,
		sessionID:   "ses_test_1",
		instanceIDs: []string{"ri_test_1"},
	}
	r := newResolver(t, yaml, func(o *launch.Options) { o.NewID = fixedIDs("lrr_test_1") })
	return newLauncher(t, r, recorder, log), recorder, log
}

// TestRecordCommitsBeforePrepareLaunch is the pre-spawn gate, observed:
// duo-vnext-go-architecture.md §5.2's "a launch resolver completes launch
// resolution and records the launch-resolution record before any
// HostLauncher.PrepareLaunch call".
//
// The recorder and the host adapter share one call log, so the assertion is
// on the real interleaving rather than on two separate counters. The same
// property is also structural — spawn takes a token only a successful
// commit can produce — but a compile-time property is invisible to a
// reviewer reading the test suite, so it is asserted here too.
func TestRecordCommitsBeforePrepareLaunch(t *testing.T) {
	l, recorder, log := newTestLauncher(t, scenarioYAML, nil)

	result, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request:       launch.Request{Preset: "review"},
		WorkspacePath: "/work/example",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	want := []string{"commit:lrr_test_1", "prepare:lrr_test_1", "start:lrr_test_1"}
	if !reflect.DeepEqual(log.entries, want) {
		t.Errorf("call order = %v, want %v", log.entries, want)
	}

	// The committed record is the resolution's, complete, and it is
	// committed before it could have gained any host evidence.
	if len(recorder.committed) != 1 {
		t.Fatalf("commits = %d, want 1", len(recorder.committed))
	}
	committed := recorder.committed[0]
	if committed.ID != "lrr_test_1" {
		t.Errorf("committed record id = %q", committed.ID)
	}
	if len(committed.Assignment) != 1 || committed.Assignment[0].Tuple.AgentRuntime != "codex" {
		t.Errorf("committed assignment = %+v, want the complete resolved plan", committed.Assignment)
	}

	// The session the commit created reaches both the record and the
	// ordinary result (§6.9's "eventual session/runtime-instance links").
	if result.Record.SessionID != "ses_test_1" {
		t.Errorf("record session = %q, want ses_test_1", result.Record.SessionID)
	}
	if result.Report.SessionID != "ses_test_1" {
		t.Errorf("report session_id = %q, want ses_test_1", result.Report.SessionID)
	}
	if result.Report.LaunchResolutionID != "lrr_test_1" {
		t.Errorf("report launch_resolution_id = %q", result.Report.LaunchResolutionID)
	}
}

// TestPreparedLaunchCarriesTheResolvedTuple proves PrepareLaunch receives
// the already-resolved tuple and nothing else to decide: the record ID, the
// integration instance of the host that was deduced for this launch, and
// the agent runtime's declared command.
func TestPreparedLaunchCarriesTheResolvedTuple(t *testing.T) {
	l, _, _ := newTestLauncher(t, scenarioYAML, nil)

	result, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request:       launch.Request{Preset: "review", Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "claude"}}},
		WorkspacePath: "/work/example",
		Env:           map[string]string{"DUO_SESSION": "ses_test_1"},
		Target:        host.LaunchTargetTab,
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if len(result.Leaves) != 1 {
		t.Fatalf("leaves launched = %d, want 1", len(result.Leaves))
	}

	prepared := result.Leaves[0].Prepared
	if prepared.LaunchResolutionID != "lrr_test_1" {
		t.Errorf("prepared launch resolution id = %q", prepared.LaunchResolutionID)
	}
	tuple, ok := prepared.Opaque.(host.ResolvedLaunchTuple)
	if !ok {
		t.Fatalf("fake host staged %T, want a host.ResolvedLaunchTuple", prepared.Opaque)
	}
	if tuple.Command != "claude" {
		t.Errorf("command = %q, want the agent runtime's declared executable", tuple.Command)
	}
	if !reflect.DeepEqual(tuple.Args, []string{"--continue"}) {
		t.Errorf("args = %v, want the agent runtime's declared arguments", tuple.Args)
	}
	if tuple.IntegrationInstanceID != testHostInstanceID {
		t.Errorf("integration instance = %q, want the deduced host %q", tuple.IntegrationInstanceID, testHostInstanceID)
	}
	if tuple.WorkspacePath != "/work/example" {
		t.Errorf("workspace path = %q", tuple.WorkspacePath)
	}
	if tuple.Target != host.LaunchTargetTab {
		t.Errorf("target = %q, want the request's placement %q", tuple.Target, host.LaunchTargetTab)
	}
}

// TestFailedResolutionCommitsNothingAndLaunchesNothing is §6.8's "effect:
// no_effect" and §7.4's "a failed resolution creates no session and
// therefore no launch-resolution record": the exhausted launch touches
// neither the recorder nor the host.
func TestFailedResolutionCommitsNothingAndLaunchesNothing(t *testing.T) {
	l, recorder, log := newTestLauncher(t, scenarioYAML, nil)

	_, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request: launch.Request{
			Preset:  "review",
			Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "pi"}},
		},
		WorkspacePath: "/work/example",
	})
	var le *launch.Error
	if !errors.As(err, &le) || le.Code != launch.CodeConstraintsExhausted {
		t.Fatalf("Launch error = %v, want launch.constraints_exhausted", err)
	}
	if len(log.entries) != 0 {
		t.Errorf("a failed resolution produced calls %v, want none", log.entries)
	}
	if len(recorder.committed) != 0 {
		t.Errorf("a failed resolution committed %d records, want none", len(recorder.committed))
	}
}

// TestCommitFailureLaunchesNothing closes the other half of the gate: a
// record that did not become durable must not be spawned against, because
// the process would then exist with no evidence explaining why it was
// chosen.
func TestCommitFailureLaunchesNothing(t *testing.T) {
	l, _, log := newTestLauncher(t, scenarioYAML, errors.New("store is read-only"))

	if _, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request:       launch.Request{Preset: "review"},
		WorkspacePath: "/work/example",
	}); err == nil {
		t.Fatal("Launch succeeded despite a failed commit")
	}
	want := []string{"commit:lrr_test_1"}
	if !reflect.DeepEqual(log.entries, want) {
		t.Errorf("call order = %v, want %v: nothing may be prepared after a failed commit", log.entries, want)
	}
}

// TestMultiLeafLaunchCommitsOnceBeforeEveryLeaf is §6.7's atomicity at the
// spawn boundary: the whole plan is recorded once, and only then is any
// leaf prepared. No leaf spawns while another is still unresolved.
func TestMultiLeafLaunchCommitsOnceBeforeEveryLeaf(t *testing.T) {
	l, recorder, log := newTestLauncher(t, scenarioYAML, nil)

	result, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request:       launch.Request{Preset: "adversarial_pair"},
		WorkspacePath: "/work/example",
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	want := []string{
		"commit:lrr_test_1",
		"prepare:lrr_test_1", "start:lrr_test_1",
		"prepare:lrr_test_1", "start:lrr_test_1",
	}
	if !reflect.DeepEqual(log.entries, want) {
		t.Errorf("call order = %v, want %v", log.entries, want)
	}
	if len(recorder.committed[0].Assignment) != 2 {
		t.Errorf("committed assignment covers %d leaves, want both", len(recorder.committed[0].Assignment))
	}
	if len(result.Leaves) != 2 {
		t.Errorf("launched %d leaves, want 2", len(result.Leaves))
	}
}

// TestAtomicMultiLeafFailureLaunchesNothing is the atomicity rule at its
// sharpest: one leaf that cannot be assigned stops the whole launch, and
// the leaf that *could* have been assigned never reaches a host.
func TestAtomicMultiLeafFailureLaunchesNothing(t *testing.T) {
	l, recorder, log := newTestLauncher(t, scenarioYAML, nil)

	_, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request: launch.Request{
			Preset:  "mixed_pair",
			Require: []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "codex"}},
		},
		WorkspacePath: "/work/example",
	})
	var le *launch.Error
	if !errors.As(err, &le) || le.Code != launch.CodeConstraintsExhausted {
		t.Fatalf("Launch error = %v, want launch.constraints_exhausted", err)
	}
	if len(log.entries) != 0 || len(recorder.committed) != 0 {
		t.Errorf("a partially assignable plan produced calls %v and %d commits, want none",
			log.entries, len(recorder.committed))
	}
}

// TestDryRunRecordsNothingAndSpawnsNothing is §6.10: a dry run uses the
// same resolver and the same static inputs, creates no session and no
// durable record, and marks its result a preview. It carries no
// launch-resolution ID, because there is no record for one to reference.
func TestDryRunRecordsNothingAndSpawnsNothing(t *testing.T) {
	l, recorder, log := newTestLauncher(t, scenarioYAML, nil)

	result, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request: launch.Request{Preset: "review"},
		DryRun:  true,
	})
	if err != nil {
		t.Fatalf("Launch(dry run): %v", err)
	}
	if len(log.entries) != 0 || len(recorder.committed) != 0 {
		t.Errorf("dry run produced calls %v and %d commits, want none", log.entries, len(recorder.committed))
	}
	if !result.Report.Preview {
		t.Error("dry-run result is not marked a preview")
	}
	if result.Report.LaunchResolutionID != "" {
		t.Errorf("dry-run result references record %q, which was never committed", result.Report.LaunchResolutionID)
	}
	if result.Report.Leaves[0].AgentRuntime != "codex" {
		t.Errorf("dry run resolved to %q, want the same answer a durable launch gets", result.Report.Leaves[0].AgentRuntime)
	}
}

// TestLaunchNeedsAWorkspacePath keeps a placement input from being
// invented. The resolver never needed it — resolution is over declarations
// — but a spawn does, and guessing one would be exactly the kind of
// implicit input §7.1 rules out.
func TestLaunchNeedsAWorkspacePath(t *testing.T) {
	l, _, log := newTestLauncher(t, scenarioYAML, nil)

	_, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request: launch.Request{Preset: "review"},
	})
	var le *launch.Error
	if !errors.As(err, &le) || le.Code != launch.CodeInvalidRequest {
		t.Fatalf("Launch error = %v, want invalid.request", err)
	}
	if len(log.entries) != 0 {
		t.Errorf("calls = %v, want none", log.entries)
	}
}

// failingHost is a HostLauncher whose PrepareLaunch always fails.
type failingHost struct {
	log *callLog
}

func (h failingHost) PrepareLaunch(context.Context, host.HostLaunchRequest) (host.PreparedHostLaunch, error) {
	h.log.add("prepare:failed")
	return host.PreparedHostLaunch{}, errors.New("no pane available")
}

func (h failingHost) Start(context.Context, host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	h.log.add("start:unreachable")
	return host.HostLaunchEvidence{}, errors.New("unreachable")
}

// TestSpawnFailureKeepsTheCommittedRecord is §7.4's last sentence: "once a
// resolution succeeds and its record is durable, a later spawn failure does
// not erase that evidence". The failure comes back with the record, so the
// caller can record the launch failure against the session the commit
// created rather than pretending the resolution never happened.
func TestSpawnFailureKeepsTheCommittedRecord(t *testing.T) {
	log := &callLog{}
	recorder := &recordingRecorder{log: log, sessionID: "ses_test_1"}
	r := newResolver(t, scenarioYAML, func(o *launch.Options) { o.NewID = fixedIDs("lrr_test_1") })
	l, err := launch.NewLauncher(r, recorder, oneHost{launcher: failingHost{log: log}})
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}

	result, err := l.Launch(context.Background(), launch.SpawnRequest{
		Request:       launch.Request{Preset: "review"},
		WorkspacePath: "/work/example",
	})
	if err == nil {
		t.Fatal("Launch succeeded despite a failing host")
	}
	if len(recorder.committed) != 1 {
		t.Fatalf("commits = %d, want the record to stand", len(recorder.committed))
	}
	if result == nil || result.Record.ID != "lrr_test_1" {
		t.Fatalf("result = %+v, want the committed record alongside the error", result)
	}
	if len(result.Leaves) != 0 {
		t.Errorf("launched leaves = %v, want none", result.Leaves)
	}
	want := []string{"commit:lrr_test_1", "prepare:failed"}
	if !reflect.DeepEqual(log.entries, want) {
		t.Errorf("call order = %v, want %v", log.entries, want)
	}
}

// TestLauncherNeedsARecorder pins the dependency that makes the gate
// possible: there is no launcher that can spawn without somewhere to record
// the resolution first.
func TestLauncherNeedsARecorder(t *testing.T) {
	r := newResolver(t, scenarioYAML)
	if _, err := launch.NewLauncher(r, nil, oneHost{}); err == nil {
		t.Fatal("NewLauncher accepted a nil recorder")
	}
}
