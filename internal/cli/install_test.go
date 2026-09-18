package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/manifest"
)

var installTestBuild = buildinfo.Info{Version: "v-test", Commit: "abc", Date: "2026-09-17T00:00:00Z"}

func runInstallCLI(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, installTestBuild)
	root.SetArgs(args)
	return Execute(root, streams), out.String(), errOut.String()
}

func cliProjectionRoot(workspace string) string {
	return filepath.Join(workspace, filepath.FromSlash(manifest.PortableProjectionRoot))
}

func TestInstallPortableLaunchersTextAndJSONSuccess(t *testing.T) {
	textWorkspace := t.TempDir()
	code, out, errOut := runInstallCLI(t, "install", "portable-launchers", "--workspace", textWorkspace)
	if code != exitcode.Success {
		t.Fatalf("text exit = %d, stderr: %s", code, errOut)
	}
	if !bytes.Contains([]byte(out), []byte("portable-launchers: current (changed: true)")) {
		t.Fatalf("text output = %q", out)
	}

	jsonWorkspace := t.TempDir()
	code, out, errOut = runInstallCLI(t, "install", "portable-launchers", "--workspace", jsonWorkspace, "--output", "json")
	if code != exitcode.Success {
		t.Fatalf("json exit = %d, stderr: %s", code, errOut)
	}
	assertValidExternalV1(t, []byte(out))
	var envelope struct {
		Operation string                 `json:"operation"`
		Result    manifest.InstallResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, out)
	}
	if envelope.Operation != "projection.install" || envelope.Result.State != manifest.StateCurrent || !envelope.Result.Changed || envelope.Result.InstallationID == "" {
		t.Fatalf("JSON result = %#v", envelope)
	}
}

func TestInstallPortableLaunchersConflictClassesDoNotWrite(t *testing.T) {
	tests := []struct {
		name string
		code string
		seed func(t *testing.T, workspace string)
	}{
		{
			name: "modified", code: "projection.modified",
			seed: func(t *testing.T, workspace string) {
				if code, _, stderr := runInstallCLI(t, "install", "portable-launchers", "--workspace", workspace); code != 0 {
					t.Fatalf("seed failed: %s", stderr)
				}
				_ = os.WriteFile(filepath.Join(cliProjectionRoot(workspace), manifest.PortableSkillFile), []byte("edited\n"), 0o644)
			},
		},
		{
			name: "incompatible", code: "projection.incompatible",
			seed: func(_ *testing.T, workspace string) {
				root := cliProjectionRoot(workspace)
				_ = os.MkdirAll(root, 0o755)
				_ = os.WriteFile(filepath.Join(root, manifest.ProjectionStampFile), []byte("{bad\n"), 0o644)
			},
		},
		{
			name: "unowned", code: "projection.user_file_conflict",
			seed: func(_ *testing.T, workspace string) {
				root := cliProjectionRoot(workspace)
				_ = os.MkdirAll(root, 0o755)
				_ = os.WriteFile(filepath.Join(root, manifest.PortableSkillFile), []byte("mine\n"), 0o644)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			tt.seed(t, workspace)
			before := snapshotTree(t, workspace)
			code, out, errOut := runInstallCLI(t, "install", "portable-launchers", "--workspace", workspace, "--repair", "--output", "json")
			if code != exitcode.UserFail || out != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", code, out, errOut)
			}
			assertValidExternalV1(t, []byte(errOut))
			var envelope struct {
				Schema    string `json:"schema"`
				Operation string `json:"operation"`
				Error     struct {
					Class  string `json:"class"`
					Code   string `json:"code"`
					Effect string `json:"effect"`
					Retry  struct {
						Safe   bool   `json:"safe"`
						Action string `json:"action"`
					} `json:"retry"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(errOut), &envelope); err != nil || envelope.Schema != "duo.external/v1" ||
				envelope.Operation != portableInstallOperation || envelope.Error.Code != tt.code ||
				envelope.Error.Class == "" || envelope.Error.Effect != "no_effect" ||
				envelope.Error.Retry.Safe || envelope.Error.Retry.Action == "" {
				t.Fatalf("error envelope = %q (%v), want %s", errOut, err, tt.code)
			}
			after := snapshotTree(t, workspace)
			if !bytes.Equal(before, after) {
				t.Fatalf("conflict changed workspace\nbefore: %s\nafter: %s", before, after)
			}
		})
	}
}

func snapshotTree(t *testing.T, root string) []byte {
	t.Helper()
	var out bytes.Buffer
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		out.WriteString(rel)
		out.WriteByte('\n')
		if !d.IsDir() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			out.Write(data)
			out.WriteByte('\n')
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
