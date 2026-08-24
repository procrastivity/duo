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
)

// legacyV2Fixture returns internal/config/testdata/duo-config-v2.legacy-
// fixture.json's bytes — the same file internal/config/migrate_test.go's
// golden test reads, one directory up.
func legacyV2Fixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "config", "testdata", "duo-config-v2.legacy-fixture.json"))
	if err != nil {
		t.Fatalf("reading the shared v2 fixture: %v", err)
	}
	return data
}

// TestConfigMigrateCommand_WriteRefusesWhileModelFamilyManual runs
// `duo config migrate --to duo.config/v3 --write <path> <source>` with no
// --set-model-family override and checks it refuses (exit code 3, refusal
// error code) and writes nothing — the CLI-level half of
// duo-vnext-installation-contract.md §1.3 item 7's first clause (the core
// transform's own half is internal/config/migrate_test.go's
// TestWrite_RefusesWhileModelFamilyManual).
func TestConfigMigrateCommand_WriteRefusesWhileModelFamilyManual(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.json")
	if err := os.WriteFile(srcPath, legacyV2Fixture(t), 0o644); err != nil {
		t.Fatalf("seeding source fixture: %v", err)
	}
	targetPath := filepath.Join(dir, "out.json")

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"config", "migrate", "--to", "duo.config/v3", "--write", targetPath, srcPath})

	code := Execute(root, streams)
	if code != exitcode.Refusal {
		t.Fatalf("exit code = %d, want %d (refusal; stderr: %s)", code, exitcode.Refusal, errOut.String())
	}
	if _, err := os.Stat(targetPath); !os.IsNotExist(err) {
		t.Errorf("--write left a file at %s despite refusing", targetPath)
	}
}

// TestConfigMigrateCommand_WriteSucceedsWithModelFamilySet proves the
// positive path end to end through the built command tree: with
// --set-model-family authoring the one variant the legacy fixture
// produces, --write succeeds and the target file parses as duo.config/v3.
func TestConfigMigrateCommand_WriteSucceedsWithModelFamilySet(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.json")
	if err := os.WriteFile(srcPath, legacyV2Fixture(t), 0o644); err != nil {
		t.Fatalf("seeding source fixture: %v", err)
	}
	targetPath := filepath.Join(dir, "out.json")

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{
		"config", "migrate", "--to", "duo.config/v3",
		"--set-model-family", "review=openai",
		"--write", targetPath, srcPath,
	})

	code := Execute(root, streams)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut.String())
	}
	written, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("reading written target: %v", err)
	}
	var doc struct {
		Schema string `json:"schema"`
	}
	if err := json.Unmarshal(written, &doc); err != nil {
		t.Fatalf("written target is not valid JSON: %v\n%s", err, written)
	}
	if doc.Schema != "duo.config/v3" {
		t.Errorf("written target schema = %q, want duo.config/v3", doc.Schema)
	}
	if out.Len() == 0 {
		t.Error("stdout is empty; want at least the write confirmation and migration report")
	}
}

// TestConfigMigrateCommand_V1InputRefused proves a duo.config/v1 input is
// refused through the full CLI path, with a message that names the truth
// (no v1-to-v3 path exists) instead of a nonexistent command.
func TestConfigMigrateCommand_V1InputRefused(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "v1.json")
	if err := os.WriteFile(srcPath, []byte(`{"schema":"duo.config/v1"}`), 0o644); err != nil {
		t.Fatalf("seeding v1 source: %v", err)
	}

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"config", "migrate", "--to", "duo.config/v3", srcPath})

	code := Execute(root, streams)
	if code != exitcode.Refusal {
		t.Fatalf("exit code = %d, want %d (refusal)", code, exitcode.Refusal)
	}
	if errOut.Len() == 0 {
		t.Fatal("stderr is empty; want a refusal message")
	}
}

// TestConfigMigrateCommand_JSONWithoutWrite runs `duo config migrate
// --json` with no --write and checks the printed document plus report
// come back inside the shared duo.external/v1 envelope, unwritten.
func TestConfigMigrateCommand_JSONWithoutWrite(t *testing.T) {
	dir := t.TempDir()
	srcPath := filepath.Join(dir, "source.json")
	if err := os.WriteFile(srcPath, legacyV2Fixture(t), 0o644); err != nil {
		t.Fatalf("seeding source fixture: %v", err)
	}

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: errOut}
	root := NewRootCommand(streams, buildinfo.Info{Version: "v0.1.0-test", Commit: "abcdef0", Date: "2026-08-23T00:00:00Z"})
	root.SetArgs([]string{"config", "migrate", "--to", "duo.config/v3", "--json", srcPath})

	code := Execute(root, streams)
	if code != exitcode.Success {
		t.Fatalf("exit code = %d, want %d (stderr: %s)", code, exitcode.Success, errOut.String())
	}

	var envelope struct {
		Schema    string `json:"schema"`
		Operation string `json:"operation"`
		Result    struct {
			Format   string `json:"format"`
			Written  bool   `json:"written"`
			Document string `json:"document"`
			Report   struct {
				Manual      []string `json:"manual"`
				StateToBind []struct {
					Variant    string `json:"variant"`
					RebindHint string `json:"rebind_hint"`
				} `json:"state_to_bind"`
			} `json:"report"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out.String())
	}
	if envelope.Operation != "config.migrate" {
		t.Errorf("operation = %q, want config.migrate", envelope.Operation)
	}
	if envelope.Result.Written {
		t.Error("result.written = true, want false (no --write given)")
	}
	if envelope.Result.Document == "" {
		t.Error("result.document is empty")
	}
	if len(envelope.Result.Report.Manual) != 1 || envelope.Result.Report.Manual[0] != "launch_variants.review.model_family" {
		t.Errorf("report.manual = %v, want [launch_variants.review.model_family]", envelope.Result.Report.Manual)
	}
	if len(envelope.Result.Report.StateToBind) != 1 || envelope.Result.Report.StateToBind[0].Variant != "review" {
		t.Errorf("report.state_to_bind = %+v, want one entry for variant review", envelope.Result.Report.StateToBind)
	}
	if envelope.Result.Report.StateToBind[0].RebindHint == "" {
		t.Error("report.state_to_bind[0].rebind_hint is empty")
	}
}
