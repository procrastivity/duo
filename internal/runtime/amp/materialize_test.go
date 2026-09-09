package amp_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/runtime/amp"
)

func TestDefaultHarnessDirUsesXDGDataHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", root)
	got, err := amp.DefaultHarnessDir("lr_test", "primary")
	if err != nil {
		t.Fatalf("DefaultHarnessDir: %v", err)
	}
	want := filepath.Join(root, "duo", "harness", "lr_test", "primary")
	if got != want {
		t.Fatalf("DefaultHarnessDir = %q, want %q", got, want)
	}
}

func TestDefaultHarnessDirEmptyLaunchResolutionIDErrors(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	if _, err := amp.DefaultHarnessDir("", "primary"); err == nil {
		t.Fatal("empty launch-resolution ID: want an error")
	}
}

func TestMaterializeSettingsWritesDisabledUpdatesAndAnimation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "harness")
	path, err := amp.MaterializeSettings(dir)
	if err != nil {
		t.Fatalf("MaterializeSettings: %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Fatalf("MaterializeSettings path = %q, want it inside %q", path, dir)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading materialized settings: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("settings file is not valid JSON: %v", err)
	}
	if got["amp.updates.mode"] != "disabled" {
		t.Fatalf(`settings["amp.updates.mode"] = %v, want "disabled"`, got["amp.updates.mode"])
	}
	if got["amp.terminal.animation"] != false {
		t.Fatalf(`settings["amp.terminal.animation"] = %v, want false`, got["amp.terminal.animation"])
	}
}

func TestMaterializeMintScriptIsExecutableAndQuotesPaths(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "harness")
	settingsPath := filepath.Join(dir, amp.SettingsFileName)
	mintLogPath := filepath.Join(t.TempDir(), "has space", "leaf.jsonl")

	scriptPath, err := amp.MaterializeMintScript(dir, settingsPath, mintLogPath)
	if err != nil {
		t.Fatalf("MaterializeMintScript: %v", err)
	}

	info, err := os.Stat(scriptPath)
	if err != nil {
		t.Fatalf("stat materialized mint script: %v", err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("mint script %s is not owner-executable: mode %s", scriptPath, info.Mode())
	}

	raw, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("reading materialized mint script: %v", err)
	}
	script := string(raw)

	if !strings.HasPrefix(script, "#!/bin/bash\n") {
		t.Fatalf("mint script does not start with a bash shebang: %q", script)
	}
	if !strings.Contains(script, `"$`+amp.MintPromptEnvVar+`"`) {
		t.Fatalf("mint script does not read the prompt from %s", amp.MintPromptEnvVar)
	}
	if !strings.Contains(script, "amp -x --no-archive-after-execute") {
		t.Fatal("mint script does not invoke `amp -x --no-archive-after-execute`")
	}
	if !strings.Contains(script, "--settings-file '"+settingsPath+"'") {
		t.Fatalf("mint script does not shell-quote the settings path %s", settingsPath)
	}
	if !strings.Contains(script, "tee '"+mintLogPath+"'") {
		t.Fatalf("mint script does not shell-quote the mint log path %s", mintLogPath)
	}
}

// TestMintScriptExitPropagatesAmpFailure runs the materialized script
// against a fake `amp` on PATH: a failing mint must fail the script even
// though the trailing `tee` succeeds, and a clean mint must exit 0 with
// the stream output tee'd into the mint log.
func TestMintScriptExitPropagatesAmpFailure(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skipf("bash unavailable: %v", err)
	}

	for _, tc := range []struct {
		name     string
		fakeAmp  string
		wantExit int
	}{
		{name: "amp failure fails the script", fakeAmp: "#!/bin/sh\ncat >/dev/null\necho '{\"session_id\":\"T-1\"}'\nexit 7\n", wantExit: 7},
		{name: "clean mint exits zero", fakeAmp: "#!/bin/sh\ncat >/dev/null\necho '{\"session_id\":\"T-1\"}'\nexit 0\n", wantExit: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			binDir := t.TempDir()
			if err := os.WriteFile(filepath.Join(binDir, "amp"), []byte(tc.fakeAmp), 0o700); err != nil {
				t.Fatalf("writing fake amp: %v", err)
			}

			harness := filepath.Join(t.TempDir(), "harness")
			mintLogPath := filepath.Join(t.TempDir(), "leaf.jsonl")
			scriptPath, err := amp.MaterializeMintScript(harness, filepath.Join(harness, amp.SettingsFileName), mintLogPath)
			if err != nil {
				t.Fatalf("MaterializeMintScript: %v", err)
			}

			cmd := exec.Command(scriptPath)
			cmd.Env = append(os.Environ(),
				"PATH="+binDir+string(os.PathListSeparator)+os.Getenv("PATH"),
				amp.MintPromptEnvVar+"=probe",
			)
			err = cmd.Run()
			exit := 0
			if err != nil {
				exitErr, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatalf("running mint script: %v", err)
				}
				exit = exitErr.ExitCode()
			}
			if exit != tc.wantExit {
				t.Fatalf("mint script exit = %d, want %d", exit, tc.wantExit)
			}

			raw, err := os.ReadFile(mintLogPath)
			if err != nil {
				t.Fatalf("reading tee'd mint log: %v", err)
			}
			if !strings.Contains(string(raw), `"session_id":"T-1"`) {
				t.Fatalf("mint log missing tee'd stream output: %q", raw)
			}
		})
	}
}
