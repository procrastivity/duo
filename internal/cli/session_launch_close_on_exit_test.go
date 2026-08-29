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
	runtimedevin "github.com/procrastivity/duo/internal/runtime/devin"
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
func launchWithAugmenter(t *testing.T, h *bindHarness, remainOnExit bool) (*launch.Result, *capturingHosts) {
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
		RemainOnExit:  remainOnExit,
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

// findAllFlagValues returns every value following name in args (e.g. all
// `-e` extension paths when name is "-e").
func findAllFlagValues(args []string, name string) []string {
	var values []string
	for i, a := range args {
		if a == name && i+1 < len(args) {
			values = append(values, args[i+1])
		}
	}
	return values
}

// setCloseOnExitConfig stamps session_hosts.kinds.herdr.close_on_exit onto
// the harness document so ResolveCloseOnExit consults it.
func setCloseOnExitConfig(h *bindHarness, closeOnExit bool) {
	v := closeOnExit
	kinds := h.doc.SessionHosts.Kinds
	if kinds == nil {
		kinds = map[string]config.SessionHostKind{}
	}
	stanza := kinds["herdr"]
	stanza.CloseOnExit = &v
	kinds["herdr"] = stanza
	h.doc.SessionHosts.Kinds = kinds
}

// TestCloseOnExitDefaultMaterializesSettingsForAClaudeLeaf is the product
// default end to end: ordinary launch (no --remain-on-exit) materializes
// close-on-exit, so a claude leaf's final launch arguments gain
// `--settings <absolute path>`, with a real, readable settings file at
// that path.
func TestCloseOnExitDefaultMaterializesSettingsForAClaudeLeaf(t *testing.T) {
	h := newBindHarness(t, nil)

	result, hosts := launchWithAugmenter(t, h, false)
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

// TestRemainOnExitLeavesArgsUnchangedForAClaudeLeaf pins the opt-out:
// --remain-on-exit leaves a claude leaf's launch arguments without
// --settings.
func TestRemainOnExitLeavesArgsUnchangedForAClaudeLeaf(t *testing.T) {
	h := newBindHarness(t, nil)

	_, hosts := launchWithAugmenter(t, h, true)

	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	if req.ResolvedLaunchTuple.CloseOnExit {
		t.Error("CloseOnExit is set on the resolved launch tuple despite --remain-on-exit")
	}
	if _, present := findFlag(req.ResolvedLaunchTuple.Args, "--settings"); present {
		t.Errorf("leaf args = %v, want no --settings flag", req.ResolvedLaunchTuple.Args)
	}
}

// TestCloseOnExitConfigFalseBehavesAsRemainForAClaudeLeaf pins config
// close_on_exit: false as the same opt-out as --remain-on-exit.
func TestCloseOnExitConfigFalseBehavesAsRemainForAClaudeLeaf(t *testing.T) {
	h := newBindHarness(t, nil)
	setCloseOnExitConfig(h, false)

	_, hosts := launchWithAugmenter(t, h, false)

	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	if req.ResolvedLaunchTuple.CloseOnExit {
		t.Error("CloseOnExit is set despite config close_on_exit: false")
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

// TestCloseOnExitDefaultSetsEnvVarAndExtensionFlagForAPiLeaf is the pi
// leg's product-default path: ordinary launch materializes inject and
// close-on-exit as two `-e` flags, plus DUO_CLOSE_PANE_ON_EXIT=1.
func TestCloseOnExitDefaultSetsEnvVarAndExtensionFlagForAPiLeaf(t *testing.T) {
	h := newPiBindHarness(t)

	result, hosts := launchWithAugmenter(t, h, false)
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

	extPaths := findAllFlagValues(req.ResolvedLaunchTuple.Args, "-e")
	if len(extPaths) != 2 {
		t.Fatalf("leaf args = %v, want exactly two -e flags, got %d", req.ResolvedLaunchTuple.Args, len(extPaths))
	}
	wantBasenames := []string{"duo-inject.ts", "duo-close-on-exit.ts"}
	for i, path := range extPaths {
		if !filepath.IsAbs(path) {
			t.Errorf("-e path[%d] %q is not absolute", i, path)
		}
		if got := filepath.Base(path); got != wantBasenames[i] {
			t.Errorf("-e path[%d] basename = %q, want %q", i, got, wantBasenames[i])
		}
		if _, err := os.Stat(path); err != nil {
			t.Errorf("the materialized extension at -e path[%d] is missing: %v", i, err)
		}
	}
}

// TestRemainOnExitStillMaterializesInjectForAPiLeaf pins the opt-out for
// close-on-exit only: --remain-on-exit still materializes inject but not
// close-on-exit or DUO_CLOSE_PANE_ON_EXIT.
func TestRemainOnExitStillMaterializesInjectForAPiLeaf(t *testing.T) {
	h := newPiBindHarness(t)

	_, hosts := launchWithAugmenter(t, h, true)

	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	if req.ResolvedLaunchTuple.CloseOnExit {
		t.Error("CloseOnExit is set on the resolved launch tuple despite --remain-on-exit")
	}
	if _, present := req.ResolvedLaunchTuple.Env["DUO_CLOSE_PANE_ON_EXIT"]; present {
		t.Errorf("pane-creation env unexpectedly carries DUO_CLOSE_PANE_ON_EXIT = %q",
			req.ResolvedLaunchTuple.Env["DUO_CLOSE_PANE_ON_EXIT"])
	}

	extPaths := findAllFlagValues(req.ResolvedLaunchTuple.Args, "-e")
	if len(extPaths) != 1 {
		t.Fatalf("leaf args = %v, want exactly one -e flag, got %d", req.ResolvedLaunchTuple.Args, len(extPaths))
	}
	injectPath := extPaths[0]
	if !filepath.IsAbs(injectPath) {
		t.Errorf("-e path %q is not absolute", injectPath)
	}
	if got := filepath.Base(injectPath); got != "duo-inject.ts" {
		t.Errorf("-e path basename = %q, want duo-inject.ts", got)
	}
	if _, err := os.Stat(injectPath); err != nil {
		t.Errorf("the materialized inject extension is missing: %v", err)
	}
	for _, path := range extPaths {
		if filepath.Base(path) == "duo-close-on-exit.ts" {
			t.Errorf("leaf args = %v, want no duo-close-on-exit.ts -e flag", req.ResolvedLaunchTuple.Args)
		}
	}
}

// TestCloseOnExitConfigFalseBehavesAsRemainForAPiLeaf pins config
// close_on_exit: false as remain for close-on-exit only; inject still
// materializes.
func TestCloseOnExitConfigFalseBehavesAsRemainForAPiLeaf(t *testing.T) {
	h := newPiBindHarness(t)
	setCloseOnExitConfig(h, false)

	_, hosts := launchWithAugmenter(t, h, false)

	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	if req.ResolvedLaunchTuple.CloseOnExit {
		t.Error("CloseOnExit is set despite config close_on_exit: false")
	}
	if _, present := req.ResolvedLaunchTuple.Env["DUO_CLOSE_PANE_ON_EXIT"]; present {
		t.Errorf("pane-creation env unexpectedly carries DUO_CLOSE_PANE_ON_EXIT = %q",
			req.ResolvedLaunchTuple.Env["DUO_CLOSE_PANE_ON_EXIT"])
	}

	extPaths := findAllFlagValues(req.ResolvedLaunchTuple.Args, "-e")
	if len(extPaths) != 1 {
		t.Fatalf("leaf args = %v, want exactly one -e flag, got %d", req.ResolvedLaunchTuple.Args, len(extPaths))
	}
	injectPath := extPaths[0]
	if !filepath.IsAbs(injectPath) {
		t.Errorf("-e path %q is not absolute", injectPath)
	}
	if got := filepath.Base(injectPath); got != "duo-inject.ts" {
		t.Errorf("-e path basename = %q, want duo-inject.ts", got)
	}
	if _, err := os.Stat(injectPath); err != nil {
		t.Errorf("the materialized inject extension is missing: %v", err)
	}
	for _, path := range extPaths {
		if filepath.Base(path) == "duo-close-on-exit.ts" {
			t.Errorf("leaf args = %v, want no duo-close-on-exit.ts -e flag", req.ResolvedLaunchTuple.Args)
		}
	}
}

const devinScenarioYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
agent_runtimes:
  devin_default:
    kind: devin
    executable: devin
launch_variants:
  daily:
    agent_runtime: devin_default
    model_line: sonnet-5
    model_family: claude
presets:
  daily:
    selection: ordered
    leaves:
      primary:
        candidates:
          - variant: daily
`

func newDevinBindHarness(t *testing.T) *bindHarness {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	doc, err := config.ParseV3([]byte(devinScenarioYAML))
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

func assertDevinPrintMintFlags(t *testing.T, result *launch.Result, req host.HostLaunchRequest) {
	t.Helper()
	exportPath, present := findFlag(req.ResolvedLaunchTuple.Args, "--export")
	if !present {
		t.Fatalf("leaf args = %v, want a --export flag", req.ResolvedLaunchTuple.Args)
	}
	wantExport, err := runtimedevin.ATIFPath(result.Report.LaunchResolutionID, "primary")
	if err != nil {
		t.Fatalf("ATIFPath: %v", err)
	}
	if exportPath != wantExport {
		t.Fatalf("--export = %q, want %q", exportPath, wantExport)
	}
	printPrompt, present := findFlag(req.ResolvedLaunchTuple.Args, "--print")
	if !present {
		t.Fatalf("leaf args = %v, want a --print flag", req.ResolvedLaunchTuple.Args)
	}
	if printPrompt != runtimedevin.LaunchMintPrompt {
		t.Fatalf("--print = %q, want %q", printPrompt, runtimedevin.LaunchMintPrompt)
	}
	mode, present := findFlag(req.ResolvedLaunchTuple.Args, "--permission-mode")
	if !present {
		t.Fatalf("leaf args = %v, want a --permission-mode flag", req.ResolvedLaunchTuple.Args)
	}
	if mode != "smart" {
		t.Fatalf("--permission-mode = %q, want %q", mode, "smart")
	}
	trust, present := findFlag(req.ResolvedLaunchTuple.Args, "--respect-workspace-trust")
	if !present {
		t.Fatalf("leaf args = %v, want a --respect-workspace-trust flag", req.ResolvedLaunchTuple.Args)
	}
	if trust != "false" {
		t.Fatalf("--respect-workspace-trust = %q, want %q", trust, "false")
	}
	for _, arg := range req.ResolvedLaunchTuple.Args {
		if arg == "dangerous" {
			t.Fatalf("leaf args = %v, must not contain %q", req.ResolvedLaunchTuple.Args, "dangerous")
		}
	}
}

func TestDevinPrintMintAppended(t *testing.T) {
	h := newDevinBindHarness(t)
	result, hosts := launchWithAugmenter(t, h, false)
	if result.Report.LaunchResolutionID == "" {
		t.Fatal("the launch was not recorded")
	}
	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	assertDevinPrintMintFlags(t, result, req)
}

func TestDevinPrintMintStillAppendedOnRemainOnExit(t *testing.T) {
	h := newDevinBindHarness(t)
	result, hosts := launchWithAugmenter(t, h, true)
	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	assertDevinPrintMintFlags(t, result, req)
}

func TestDevinExportAlwaysAppended(t *testing.T) {
	h := newDevinBindHarness(t)
	result, hosts := launchWithAugmenter(t, h, false)
	if result.Report.LaunchResolutionID == "" {
		t.Fatal("the launch was not recorded")
	}
	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	exportPath, present := findFlag(req.ResolvedLaunchTuple.Args, "--export")
	if !present {
		t.Fatalf("leaf args = %v, want a --export flag", req.ResolvedLaunchTuple.Args)
	}
	want, err := runtimedevin.ATIFPath(result.Report.LaunchResolutionID, "primary")
	if err != nil {
		t.Fatalf("ATIFPath: %v", err)
	}
	if exportPath != want {
		t.Fatalf("--export = %q, want %q", exportPath, want)
	}
	if _, err := os.Stat(filepath.Dir(exportPath)); err != nil {
		t.Errorf("export parent dir missing: %v", err)
	}
}

func TestDevinExportStillAppendedOnRemainOnExit(t *testing.T) {
	h := newDevinBindHarness(t)
	result, hosts := launchWithAugmenter(t, h, true)
	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	exportPath, present := findFlag(req.ResolvedLaunchTuple.Args, "--export")
	if !present {
		t.Fatalf("leaf args = %v, want --export even with --remain-on-exit", req.ResolvedLaunchTuple.Args)
	}
	want, err := runtimedevin.ATIFPath(result.Report.LaunchResolutionID, "primary")
	if err != nil {
		t.Fatalf("ATIFPath: %v", err)
	}
	if exportPath != want {
		t.Fatalf("--export = %q, want %q", exportPath, want)
	}
}
