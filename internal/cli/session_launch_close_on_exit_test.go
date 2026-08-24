package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launchrecord"
)

// capturingHosts is a launch.HostSet over the first-class fake host adapter
// (internal/host/fake, the fake host CLI tests use throughout this
// package), wrapped so every leaf's final host.HostLaunchRequest — in
// particular its Args and Env, after any installed launch.LeafAugmenter ran — is
// recorded for the test to inspect. It is the same shape as
// session_launch_bind_test.go's spawningHosts/refusingStart pair, built for
// a different observation.
type capturingHosts struct {
	captured map[string]host.HostLaunchRequest // leaf -> request
}

func newCapturingHosts() *capturingHosts {
	return &capturingHosts{captured: map[string]host.HostLaunchRequest{}}
}

func (h *capturingHosts) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	return &capturingHost{Host: hostfake.New(t.IntegrationInstanceID), into: h.captured}, nil
}

type capturingHost struct {
	*hostfake.Host
	into map[string]host.HostLaunchRequest
}

func (h *capturingHost) PrepareLaunch(ctx context.Context, req host.HostLaunchRequest) (host.PreparedHostLaunch, error) {
	h.into[req.ResolvedLaunchTuple.Leaf] = req
	return h.Host.PrepareLaunch(ctx, req)
}

// launchWithAugmenter runs the real resolve-commit-spawn path
// (launch.Launcher.Launch, the same call session_launch.go's RunE makes
// through launchAndBind) against a fresh bindHarness, with the CLI's real
// stage1LeafAugmenter installed exactly as sessionLaunchCommand wires it —
// so these tests exercise the production seam, not a reimplementation of
// it — and a capturingHosts in place of the real Herdr-backed
// stage1HostSet, so no test needs a live Herdr socket.
func launchWithAugmenter(t *testing.T, h *bindHarness, closeOnExit bool) (*launch.Result, *capturingHosts) {
	t.Helper()
	m := h.materializeWith("herdr:"+bindSocket, nil)

	resolver, err := launch.NewResolver(h.doc, m, launch.Options{
		Support: launch.AllSupported{RecordDigest: "sha256:test"},
	})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}
	recorder, err := launchrecord.New(h.authority, launchrecord.Options{
		WorkspacePath: m.WorkspacePath(),
		Actor:         "user:test",
	})
	if err != nil {
		t.Fatalf("launchrecord.New: %v", err)
	}

	hosts := newCapturingHosts()
	launcher, err := launch.NewLauncher(resolver, recorder, hosts, launch.WithLeafAugmenter(stage1LeafAugmenter{}))
	if err != nil {
		t.Fatalf("NewLauncher: %v", err)
	}

	result, err := launcher.Launch(context.Background(), launch.SpawnRequest{
		Request:       launch.Request{Preset: "daily", RequestID: "req_test", Caller: "user:test"},
		WorkspacePath: m.WorkspacePath(),
		CloseOnExit:   closeOnExit,
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	return result, hosts
}

// findFlag returns the value following name in args, and whether name was
// present at all.
func findFlag(args []string, name string) (value string, present bool) {
	for i, a := range args {
		if a == name {
			if i+1 < len(args) {
				return args[i+1], true
			}
			return "", true
		}
	}
	return "", false
}

// TestCloseOnExitAppendsSettingsFlagForAClaudeLeaf is the flag's whole
// point end to end at the launch-orchestration boundary: --close-on-exit
// threads from launch.SpawnRequest.CloseOnExit through
// stage1LeafAugmenter, and a claude leaf's final launch arguments gain
// `--settings <absolute path>`, with a real, readable settings file at
// that path.
func TestCloseOnExitAppendsSettingsFlagForAClaudeLeaf(t *testing.T) {
	h := newBindHarness(t, nil)

	result, hosts := launchWithAugmenter(t, h, true)
	if result.Report.LaunchResolutionID == "" {
		t.Fatal("the launch was not recorded")
	}

	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	if !req.ResolvedLaunchTuple.CloseOnExit {
		t.Error("the resolved launch tuple does not carry CloseOnExit through")
	}

	settingsPath, present := findFlag(req.ResolvedLaunchTuple.Args, "--settings")
	if !present {
		t.Fatalf("leaf args = %v, want a --settings flag", req.ResolvedLaunchTuple.Args)
	}
	if !filepath.IsAbs(settingsPath) {
		t.Errorf("--settings path %q is not absolute", settingsPath)
	}
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("the materialized settings file is missing: %v", err)
	}
}

// TestCloseOnExitLeavesArgsUnchangedWithoutTheFlag pins the other half:
// nothing about a claude leaf's launch arguments changes when
// --close-on-exit was never passed, so an ordinary launch is byte-for-byte
// what it was before this feature existed.
func TestCloseOnExitLeavesArgsUnchangedWithoutTheFlag(t *testing.T) {
	h := newBindHarness(t, nil)

	_, hosts := launchWithAugmenter(t, h, false)

	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	if req.ResolvedLaunchTuple.CloseOnExit {
		t.Error("CloseOnExit is set on the resolved launch tuple despite --close-on-exit never being passed")
	}
	if _, present := findFlag(req.ResolvedLaunchTuple.Args, "--settings"); present {
		t.Errorf("leaf args = %v, want no --settings flag", req.ResolvedLaunchTuple.Args)
	}
}

// piScenarioYAML is bindScenarioYAML's pi-runtime twin: same preset/leaf
// shape (a "daily" preset, one leaf named "primary"), but the declared
// agent runtime is kind "pi" rather than "claude" — the config-authoring
// difference that flips stage1LeafAugmenter onto its env-setting leg
// instead of its settings-materializing one.
const piScenarioYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
agent_runtimes:
  pi_default:
    kind: pi
    executable: pi
launch_variants:
  daily:
    agent_runtime: pi_default
    model_line: pi-default
    model_family: pi
presets:
  daily:
    selection: ordered
    leaves:
      primary:
        candidates:
          - variant: daily
`

// newPiBindHarness is newBindHarness (session_launch_bind_test.go) with
// piScenarioYAML parsed in place of bindScenarioYAML — everything else
// about the harness (its isolated store, its isolated XDG roots, its
// streams) is identical, so launchWithAugmenter works unchanged against
// either harness.
func newPiBindHarness(t *testing.T) *bindHarness {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	doc, err := config.ParseV3([]byte(piScenarioYAML))
	if err != nil {
		t.Fatalf("ParseV3: %v", err)
	}

	a, store, err := openWriteAuthority(context.Background())
	if err != nil {
		t.Fatalf("openWriteAuthority: %v", err)
	}

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	h := &bindHarness{
		t:         t,
		root:      t.TempDir(),
		authority: a,
		store:     store,
		streams:   &iostreams.Streams{Out: out, Err: errOut},
		out:       out,
		err:       errOut,
		doc:       doc,
	}
	t.Cleanup(h.close)
	return h
}

// TestCloseOnExitSetsEnvVarForAPiLeaf is the env-setting leg's whole point
// end to end: --close-on-exit threads from launch.SpawnRequest.CloseOnExit
// through stage1LeafAugmenter, and a pi leaf's pane-creation env — the
// ResolvedLaunchTuple.Env a HostLauncher.PrepareLaunch receives, which the
// herdr adapter copies verbatim into workspace.create/tab.create/pane.split
// (internal/host/herdr/doc.go's "environment scrub duty" section) — gains
// DUO_CLOSE_PANE_ON_EXIT=1, the exact key and value
// internal/runtime/pi/extension/duo-pi-reporter.ts reads.
func TestCloseOnExitSetsEnvVarForAPiLeaf(t *testing.T) {
	h := newPiBindHarness(t)

	result, hosts := launchWithAugmenter(t, h, true)
	if result.Report.LaunchResolutionID == "" {
		t.Fatal("the launch was not recorded")
	}

	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	if !req.ResolvedLaunchTuple.CloseOnExit {
		t.Error("the resolved launch tuple does not carry CloseOnExit through")
	}
	if got := req.ResolvedLaunchTuple.Env["DUO_CLOSE_PANE_ON_EXIT"]; got != "1" {
		t.Errorf("pane-creation env DUO_CLOSE_PANE_ON_EXIT = %q, want \"1\"", got)
	}
}

// TestCloseOnExitLeavesEnvUnchangedForAPiLeafWithoutTheFlag pins the other
// half: a pi leaf's pane-creation env carries no
// DUO_CLOSE_PANE_ON_EXIT at all when --close-on-exit was never passed, so
// an ordinary pi launch is unaffected by this feature existing.
func TestCloseOnExitLeavesEnvUnchangedForAPiLeafWithoutTheFlag(t *testing.T) {
	h := newPiBindHarness(t)

	_, hosts := launchWithAugmenter(t, h, false)

	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	if req.ResolvedLaunchTuple.CloseOnExit {
		t.Error("CloseOnExit is set on the resolved launch tuple despite --close-on-exit never being passed")
	}
	if _, present := req.ResolvedLaunchTuple.Env["DUO_CLOSE_PANE_ON_EXIT"]; present {
		t.Errorf("pane-creation env unexpectedly carries DUO_CLOSE_PANE_ON_EXIT = %q",
			req.ResolvedLaunchTuple.Env["DUO_CLOSE_PANE_ON_EXIT"])
	}
}
