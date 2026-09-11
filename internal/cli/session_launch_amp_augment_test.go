package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/iostreams"
	runtimeamp "github.com/procrastivity/duo/internal/runtime/amp"
)

// ampScenarioYAML is bindScenarioYAML's Amp-runtime twin (same shape as
// piScenarioYAML/devinScenarioYAML): one "daily" preset, one leaf named
// "primary", declared agent-runtime kind "amp". The declared executable is
// "bash", not "amp" itself — the leaf augmenter can only append args and
// env (never change argv[0] or feed stdin), so the mint wrapper script it
// materializes has to arrive as an appended argument to a shell able to
// run it (stage1LeafAugmenter's doc comment, session_launch.go).
const ampScenarioYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
agent_runtimes:
  amp_default:
    kind: amp
    executable: bash
launch_variants:
  daily:
    agent_runtime: amp_default
    model_line: amp-default
    model_family: amp
presets:
  daily:
    selection: ordered
    leaves:
      primary:
        candidates:
          - variant: daily
`

// newAmpBindHarness is newBindHarness (session_launch_bind_test.go) with
// ampScenarioYAML parsed in place of bindScenarioYAML — everything else
// about the harness (its isolated store, its isolated XDG roots, its
// streams) is identical, so launchWithAugmenter works unchanged against it,
// mirroring newDevinBindHarness / newPiBindHarness
// (session_launch_close_on_exit_test.go).
func newAmpBindHarness(t *testing.T) *bindHarness {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	doc, err := config.ParseV3([]byte(ampScenarioYAML))
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

// assertAmpMintWrapperFlags checks the shape every Amp leaf launch owes,
// whatever closeOnExit is: the materialized mint script's own path
// appended as the leaf's sole argument, a real settings file materialized
// beside it, and the mint prompt delivered through the environment rather
// than argv (mirrors assertDevinPrintMintFlags,
// session_launch_close_on_exit_test.go).
func assertAmpMintWrapperFlags(t *testing.T, req host.HostLaunchRequest) {
	t.Helper()
	args := req.ResolvedLaunchTuple.Args
	if len(args) != 1 {
		t.Fatalf("leaf args = %v, want exactly one appended argument (the mint script path)", args)
	}
	scriptPath := args[0]
	if !filepath.IsAbs(scriptPath) {
		t.Errorf("mint script path %q is not absolute", scriptPath)
	}
	if filepath.Base(scriptPath) != runtimeamp.MintScriptFileName {
		t.Errorf("mint script path basename = %q, want %q", filepath.Base(scriptPath), runtimeamp.MintScriptFileName)
	}
	if _, err := os.Stat(scriptPath); err != nil {
		t.Errorf("the materialized mint script is missing: %v", err)
	}

	settingsPath := filepath.Join(filepath.Dir(scriptPath), runtimeamp.SettingsFileName)
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("the materialized settings file is missing: %v", err)
	}

	if got := req.ResolvedLaunchTuple.Env[runtimeamp.MintPromptEnvVar]; got != runtimeamp.LaunchMintPrompt {
		t.Errorf("env %s = %q, want %q", runtimeamp.MintPromptEnvVar, got, runtimeamp.LaunchMintPrompt)
	}
}

// TestAmpMintWrapperAppended is the product-default path: an ordinary
// launch materializes the mint wrapper and settings file, and the leaf's
// final launch arguments carry only the wrapper's own path.
func TestAmpMintWrapperAppended(t *testing.T) {
	h := newAmpBindHarness(t)
	result, hosts := launchWithAugmenter(t, h, false)
	if result.Report.LaunchResolutionID == "" {
		t.Fatal("the launch was not recorded")
	}
	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	assertAmpMintWrapperFlags(t, req)
}

// TestAmpMintWrapperStillAppendedOnRemainOnExit pins that the Amp leg is
// not gated on close-on-exit, mirroring
// TestDevinPrintMintStillAppendedOnRemainOnExit: --remain-on-exit changes
// nothing about the mint wrapper.
func TestAmpMintWrapperStillAppendedOnRemainOnExit(t *testing.T) {
	h := newAmpBindHarness(t)
	_, hosts := launchWithAugmenter(t, h, true)
	req, ok := hosts.captured["primary"]
	if !ok {
		t.Fatal("leaf \"primary\" never reached PrepareLaunch")
	}
	assertAmpMintWrapperFlags(t, req)
}
