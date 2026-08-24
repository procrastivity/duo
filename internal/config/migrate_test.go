package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/duoerr"
)

// TestMigrateV2ToV3_Fixture is the golden test: migrating
// terminal-multiplexers' pre-v3 fixture (a copy of the commit ce0a12c
// fixtures/duo-external-v1/config.json — the last commit before the v3
// schema and fixture landed, checked into testdata/duo-config-v2.legacy-
// fixture.json) produces a document ParseV3 accepts once every
// model_family is authored.
func TestMigrateV2ToV3_Fixture(t *testing.T) {
	data, err := os.ReadFile("testdata/duo-config-v2.legacy-fixture.json")
	if err != nil {
		t.Fatalf("reading testdata fixture: %v", err)
	}

	// Without overrides, the draft is well-formed but every model_family
	// reads "manual" — proving that alone first.
	unresolved, err := MigrateV2ToV3(data, nil)
	if err != nil {
		t.Fatalf("MigrateV2ToV3(no overrides): %v", err)
	}
	pending := unresolved.PendingModelFamilies()
	if len(pending) != 1 || pending[0] != "review" {
		t.Fatalf("PendingModelFamilies = %v, want [review]", pending)
	}
	// "manual" is syntactically valid (ParseV3 only requires model_family
	// to be non-empty) — the unresolved draft still parses. It is Write,
	// not ParseV3, that gates on the literal "manual" (§1.3 item 7,
	// TestWrite_RefusesWhileModelFamilyManual below).
	rendered, err := unresolved.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, err := ParseV3(rendered); err != nil {
		t.Fatalf("ParseV3(unresolved migrated document): %v (model_family=\"manual\" should still parse)", err)
	}

	if unresolved.Format != "json" {
		t.Errorf("Format = %q, want %q (the source fixture is JSON)", unresolved.Format, "json")
	}

	// With the model_family authored, the migrated document must parse
	// cleanly under the strict v3 resolver.
	resolved, err := MigrateV2ToV3(data, map[string]string{"review": "openai"})
	if err != nil {
		t.Fatalf("MigrateV2ToV3(with override): %v", err)
	}
	if got := resolved.PendingModelFamilies(); len(got) != 0 {
		t.Fatalf("PendingModelFamilies = %v, want none", got)
	}
	rendered, err = resolved.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	doc, err := ParseV3(rendered)
	if err != nil {
		t.Fatalf("ParseV3(migrated fixture): %v\nrendered:\n%s", err, rendered)
	}

	variant, ok := doc.LaunchVariants["review"]
	if !ok {
		t.Fatal(`migrated document has no launch_variants["review"] (want the composition name to become the variant name)`)
	}
	if variant.ModelLine != "gpt-5.6" {
		t.Errorf("review.ModelLine = %q, want %q", variant.ModelLine, "gpt-5.6")
	}
	if variant.ModelFamily != "openai" {
		t.Errorf("review.ModelFamily = %q, want %q", variant.ModelFamily, "openai")
	}
	if variant.AgentRuntime != "codex_default" {
		t.Errorf("review.AgentRuntime = %q, want %q", variant.AgentRuntime, "codex_default")
	}

	rt, ok := doc.AgentRuntimes["codex_default"]
	if !ok {
		t.Fatal("migrated document has no agent_runtimes[codex_default]")
	}
	if rt.Kind != "codex" {
		t.Errorf("agent_runtimes[codex_default].Kind = %q, want %q", rt.Kind, "codex")
	}
	if rt.Executable != "codex" {
		t.Errorf("agent_runtimes[codex_default].Executable = %q, want %q (moved from launch_variants.codex_in_tmux.executable)", rt.Executable, "codex")
	}

	preset, ok := doc.Presets["review"]
	if !ok {
		t.Fatal("migrated document has no presets[review]")
	}
	leaf, ok := preset.Leaves["main"]
	if !ok || len(leaf.Candidates) != 1 || leaf.Candidates[0].Variant != "review" {
		t.Errorf("presets[review].Leaves[main] = %+v, want one candidate referencing variant %q", leaf, "review")
	}

	// session_host/socket_path never appear in the v3 document; the drop
	// is reported instead.
	if doc.SessionHosts.Prefer == nil || len(doc.SessionHosts.Prefer) != 1 || doc.SessionHosts.Prefer[0] != "tmux" {
		t.Errorf("SessionHosts.Prefer = %v, want [tmux]", doc.SessionHosts.Prefer)
	}
	if len(resolved.Report.StateToBind) != 1 {
		t.Fatalf("Report.StateToBind = %+v, want exactly one entry", resolved.Report.StateToBind)
	}
	stb := resolved.Report.StateToBind[0]
	if stb.Variant != "review" || stb.SessionHost != "local_tmux" || stb.Kind != "tmux" {
		t.Errorf("StateToBind[0] = %+v, want variant=review session_host=local_tmux kind=tmux", stb)
	}
	if stb.SocketPath != "" {
		t.Errorf("StateToBind[0].SocketPath = %q, want empty (the source fixture records no socket_path)", stb.SocketPath)
	}
}

// TestMigrateV2ToV3_V1Refused pins §1.3 item 8: a duo.config/v1 input is
// refused with a message naming the truth (no v1-to-v3 path exists) rather
// than pointing at a command that does not exist.
func TestMigrateV2ToV3_V1Refused(t *testing.T) {
	_, err := MigrateV2ToV3([]byte("schema: duo.config/v1\n"), nil)
	var derr *duoerr.Error
	if !errors.As(err, &derr) {
		t.Fatalf("MigrateV2ToV3: got non-duoerr error: %v", err)
	}
	if derr.Code != ErrCodeMigrateV1Unsupported {
		t.Errorf("error code = %q, want %q", derr.Code, ErrCodeMigrateV1Unsupported)
	}
	if !strings.Contains(derr.Message, "no duo.config/v1-to-duo.config/v3 migration path") {
		t.Errorf("message = %q, want it to explain no v1-to-v3 path exists (and to name no nonexistent command)", derr.Message)
	}
}

func TestMigrateV2ToV3_AlreadyV3Refused(t *testing.T) {
	_, err := MigrateV2ToV3([]byte("schema: duo.config/v3\n"), nil)
	var derr *duoerr.Error
	if !errors.As(err, &derr) || derr.Code != ErrCodeMigrateNotV2 {
		t.Fatalf("MigrateV2ToV3(already v3): got %v, want code %q", err, ErrCodeMigrateNotV2)
	}
}

// TestWrite_RefusesWhileModelFamilyManual pins §1.3 item 7's first clause.
func TestWrite_RefusesWhileModelFamilyManual(t *testing.T) {
	data, err := os.ReadFile("testdata/duo-config-v2.legacy-fixture.json")
	if err != nil {
		t.Fatalf("reading testdata fixture: %v", err)
	}
	result, err := MigrateV2ToV3(data, nil)
	if err != nil {
		t.Fatalf("MigrateV2ToV3: %v", err)
	}

	target := filepath.Join(t.TempDir(), "out.json")
	err = result.Write(target)
	var derr *duoerr.Error
	if !errors.As(err, &derr) {
		t.Fatalf("Write: got non-duoerr error: %v", err)
	}
	if derr.Code != ErrCodeMigrateModelFamilyManual {
		t.Errorf("error code = %q, want %q", derr.Code, ErrCodeMigrateModelFamilyManual)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("Write left a file at %s despite refusing", target)
	}
}

// TestWrite_SucceedsOnceFamiliesAreAuthored proves the positive path: with
// every model_family authored, Write succeeds, and the written file itself
// parses under ParseV3.
func TestWrite_SucceedsOnceFamiliesAreAuthored(t *testing.T) {
	data, err := os.ReadFile("testdata/duo-config-v2.legacy-fixture.json")
	if err != nil {
		t.Fatalf("reading testdata fixture: %v", err)
	}
	result, err := MigrateV2ToV3(data, map[string]string{"review": "openai"})
	if err != nil {
		t.Fatalf("MigrateV2ToV3: %v", err)
	}

	target := filepath.Join(t.TempDir(), "out.json")
	if err := result.Write(target); err != nil {
		t.Fatalf("Write: %v", err)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if _, err := ParseV3(written); err != nil {
		t.Fatalf("ParseV3(written file): %v", err)
	}
}

// TestWrite_NeverOverwritesUnrecognizedFile pins §1.3 item 1: "It never
// overwrites an unrecognized file."
func TestWrite_NeverOverwritesUnrecognizedFile(t *testing.T) {
	data, err := os.ReadFile("testdata/duo-config-v2.legacy-fixture.json")
	if err != nil {
		t.Fatalf("reading testdata fixture: %v", err)
	}
	result, err := MigrateV2ToV3(data, map[string]string{"review": "openai"})
	if err != nil {
		t.Fatalf("MigrateV2ToV3: %v", err)
	}

	target := filepath.Join(t.TempDir(), "not-a-config.txt")
	if err := os.WriteFile(target, []byte("some unrelated file content\n"), 0o644); err != nil {
		t.Fatalf("seeding unrecognized target: %v", err)
	}

	err = result.Write(target)
	var derr *duoerr.Error
	if !errors.As(err, &derr) || derr.Code != ErrCodeMigrateWriteTargetUnrecognized {
		t.Fatalf("Write(unrecognized existing target): got %v, want code %q", err, ErrCodeMigrateWriteTargetUnrecognized)
	}
	after, _ := os.ReadFile(target)
	if string(after) != "some unrelated file content\n" {
		t.Error("Write clobbered the unrecognized existing file despite refusing")
	}
}

// TestWrite_ReplacesRecognizedExistingFile proves the other half of item 1:
// an existing file that IS a recognized duo.config document (any version)
// may be replaced.
func TestWrite_ReplacesRecognizedExistingFile(t *testing.T) {
	data, err := os.ReadFile("testdata/duo-config-v2.legacy-fixture.json")
	if err != nil {
		t.Fatalf("reading testdata fixture: %v", err)
	}
	result, err := MigrateV2ToV3(data, map[string]string{"review": "openai"})
	if err != nil {
		t.Fatalf("MigrateV2ToV3: %v", err)
	}

	target := filepath.Join(t.TempDir(), "existing-v2.json")
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatalf("seeding recognized existing target: %v", err)
	}
	if err := result.Write(target); err != nil {
		t.Fatalf("Write(recognized existing target): %v", err)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("reading written file: %v", err)
	}
	if _, err := ParseV3(written); err != nil {
		t.Fatalf("ParseV3(replaced file): %v", err)
	}
}

// TestMigrateV2ToV3_OrderOfFirstAppearanceAndDisabledKind exercises a
// synthetic multi-composition document: two host kinds, declared in
// compositions/launch_variants order "herdr" then "tmux" even though the
// session_hosts block itself declares "tmux" first — proving prefer order
// follows the migrated declarations (§1.3 item 5), not the source
// session_hosts block — and one host explicitly disabled.
func TestMigrateV2ToV3_OrderOfFirstAppearanceAndDisabledKind(t *testing.T) {
	const doc = `
schema: duo.config/v2
session_hosts:
  local_tmux:
    kind: tmux
    enabled: false
  local_herdr:
    kind: herdr
agent_runtimes:
  claude_rt:
    kind: claude
launch_variants:
  claude_in_herdr:
    agent_runtime: claude_rt
    session_host: local_herdr
    executable: claude
    arguments: ["--flag"]
  claude_in_tmux:
    agent_runtime: claude_rt
    session_host: local_tmux
    executable: claude
    arguments: ["--flag", "--extra"]
compositions:
  first:
    launch_variant: claude_in_herdr
    model_line: sonnet-5
  second:
    launch_variant: claude_in_tmux
    model_line: sonnet-5
presets:
  review:
    leaves:
      main:
        candidates:
          - composition: first
          - composition: second
`
	result, err := MigrateV2ToV3([]byte(doc), map[string]string{"first": "claude", "second": "claude"})
	if err != nil {
		t.Fatalf("MigrateV2ToV3: %v", err)
	}

	sh := result.Document.SessionHosts
	if sh == nil {
		t.Fatal("SessionHosts is nil")
	}
	if len(sh.Prefer) != 2 || sh.Prefer[0] != "herdr" || sh.Prefer[1] != "tmux" {
		t.Errorf("Prefer = %v, want [herdr tmux] (first-appearance order among the migrated declarations, not the session_hosts block order)", sh.Prefer)
	}
	if flag, ok := sh.Kinds["tmux"]; !ok || flag.Enabled {
		t.Errorf("Kinds[tmux] = %+v, want present with enabled=false", sh.Kinds["tmux"])
	}
	if _, ok := sh.Kinds["herdr"]; ok {
		t.Error("Kinds[herdr] is present, want absent (never disabled, so no stanza is needed)")
	}

	// The runtime's base executable/arguments come from whichever
	// composition claimed the runtime first ("first" / claude_in_herdr);
	// "second" / claude_in_tmux has different arguments, so it carries its
	// own as append_arguments instead of silently overwriting the runtime.
	rt, ok := result.Document.AgentRuntimes["claude_rt"]
	if !ok {
		t.Fatal("AgentRuntimes[claude_rt] missing")
	}
	if len(rt.Arguments) != 1 || rt.Arguments[0] != "--flag" {
		t.Errorf("claude_rt.Arguments = %v, want [--flag] (claimed by the first composition)", rt.Arguments)
	}
	first := result.Document.LaunchVariants["first"]
	if len(first.AppendArguments) != 0 {
		t.Errorf("first.AppendArguments = %v, want none (it set the runtime's base)", first.AppendArguments)
	}
	second := result.Document.LaunchVariants["second"]
	if len(second.AppendArguments) != 2 || second.AppendArguments[0] != "--flag" || second.AppendArguments[1] != "--extra" {
		t.Errorf("second.AppendArguments = %v, want [--flag --extra] (its own arguments diverge from the runtime's base)", second.AppendArguments)
	}

	rendered, err := result.Render()
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if _, err := ParseV3(rendered); err != nil {
		t.Fatalf("ParseV3(synthetic migrated document): %v\nrendered:\n%s", err, rendered)
	}
}

// TestMigrateV2ToV3_SetModelFamilyUnknownVariant proves --set-model-family
// (the CLI's authoring path) refuses a variant name the migration never
// produced, rather than silently ignoring it.
func TestMigrateV2ToV3_SetModelFamilyUnknownVariant(t *testing.T) {
	data, err := os.ReadFile("testdata/duo-config-v2.legacy-fixture.json")
	if err != nil {
		t.Fatalf("reading testdata fixture: %v", err)
	}
	_, err = MigrateV2ToV3(data, map[string]string{"nonexistent": "claude"})
	var derr *duoerr.Error
	if !errors.As(err, &derr) || derr.Code != ErrCodeMigrateSetModelFamilyMalformed {
		t.Fatalf("MigrateV2ToV3(unknown --set-model-family variant): got %v, want code %q", err, ErrCodeMigrateSetModelFamilyMalformed)
	}
}
