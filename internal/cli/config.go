// config.go is the composition root for the `duo config` verb family:
// internal/registry's "config.migrate" operation (Step 07), CLI path
// {"config", "migrate"}. It is a thin wrapper — every transform rule lives
// in internal/config/migrate.go (MigrateV2ToV3), normative at
// duo-vnext-installation-contract.md §1.3 "Migration to duo.config/v3".

package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// configCommand builds the `duo config` parent verb.
func configCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "inspect and migrate duo configuration documents",
	}
	cmd.AddCommand(configMigrateCommand(streams))
	return cmd
}

// configMigrateCommand constructs `duo config migrate`: internal/registry's
// "config.migrate" operation. It never runs at daemon startup and is
// reached only through this CLI verb (duo-vnext-installation-contract.md
// §1.3 item 9; workplan Step 07).
//
// --write takes PATH rather than being a bare boolean, matching §1.3 item
// 1's "writes to stdout by default. An explicit --write PATH uses
// create-new or validated replacement semantics."
//
// --set-model-family is the one authoring path this step implements for
// §1.3 item 6's required-but-never-inferred model_family (workplan Step 07:
// "pick ONE authoring path... and record it"). An edited-input-file path
// was the alternative; --set-model-family was chosen because it keeps
// authoring in the same command invocation the operator is already
// running, needs no second round-trip through the filesystem, and is
// directly unit-testable without touching disk (internal/config/migrate.go
// applies overrides as a plain map, independent of any file I/O). See the
// wip finding for the full rationale.
func configMigrateCommand(streams *iostreams.Streams) *cobra.Command {
	var (
		to             string
		write          string
		setModelFamily []string
	)

	cmd := &cobra.Command{
		Use:   "migrate PATH",
		Short: `migrate a duo.config document ("--to duo.config/v3" is the only implemented target)`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			flags := cliflags.FromContext(cmd.Context())

			if to != config.SchemaV3 {
				return duoerr.New("invalid.request",
					fmt.Sprintf("--to must be %q; no other migration target is implemented.", config.SchemaV3))
			}

			overrides, err := parseModelFamilyOverrides(setModelFamily)
			if err != nil {
				return err
			}

			result, err := config.MigrateV2ToV3File(args[0], overrides)
			if err != nil {
				return err
			}

			written := false
			if write != "" {
				if err := result.Write(write); err != nil {
					return err
				}
				written = true
			}

			return renderMigrateResult(streams, flags.JSON(), result, write, written)
		},
	}

	cmd.Flags().StringVar(&to, "to", "", `migration target schema; only "duo.config/v3" is implemented (required)`)
	cmd.Flags().StringVar(&write, "write", "", "write the migrated document to PATH (create-new or validated-replacement semantics); omit to print to stdout")
	cmd.Flags().StringArrayVar(&setModelFamily, "set-model-family", nil, "author one migrated launch_variant's model_family, variant=label; repeatable")
	_ = cmd.MarkFlagRequired("to")

	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// parseModelFamilyOverrides parses repeated "variant=label" --set-model-
// family values into the map config.MigrateV2ToV3 applies on top of the
// "manual" default. An entry naming an unknown variant, or an empty label,
// is refused by MigrateV2ToV3 itself (ErrCodeMigrateSetModelFamilyMalformed)
// — this function only enforces the flag's own "variant=label" shape.
func parseModelFamilyOverrides(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(raw))
	for _, r := range raw {
		name, label, ok := strings.Cut(r, "=")
		if !ok || name == "" {
			return nil, duoerr.New("invalid.request",
				fmt.Sprintf("--set-model-family %q must be variant=label.", r))
		}
		out[name] = label
	}
	return out, nil
}

// migrateResultPayload is config.migrate's --output json result shape, wrapped in
// the shared duo.external/v1 envelope (newEnvelope, session.go).
type migrateResultPayload struct {
	Format      string               `json:"format"`
	Written     bool                 `json:"written"`
	WrittenPath string               `json:"written_path,omitempty"`
	Document    string               `json:"document"`
	Report      migrateReportPayload `json:"report"`
}

type migrateReportPayload struct {
	Renamed     []string             `json:"renamed"`
	Defaulted   []string             `json:"defaulted"`
	Rejected    []string             `json:"rejected"`
	Manual      []string             `json:"manual"`
	StateToBind []stateToBindPayload `json:"state_to_bind"`
}

type stateToBindPayload struct {
	Variant     string `json:"variant"`
	SessionHost string `json:"session_host"`
	Kind        string `json:"kind,omitempty"`
	SocketPath  string `json:"socket_path,omitempty"`
	RebindHint  string `json:"rebind_hint"`
}

// renderMigrateResult prints result, in either --output json (wrapped in the
// shared envelope) or human-readable text: the migrated document (unless
// it was written to writtenPath, in which case a one-line confirmation
// stands in for it) followed by the migration report — renamed, defaulted,
// rejected, and manual fields (duo-vnext-installation-contract.md §1.3
// intro), then the state-to-bind report (§1.3 item 3) with
// config.RebindHint as the pointer text.
func renderMigrateResult(streams *iostreams.Streams, jsonMode bool, result config.MigrationResult, writtenPath string, written bool) error {
	rendered, err := result.Render()
	if err != nil {
		return err
	}

	if jsonMode {
		payload := migrateResultPayload{
			Format:      result.Format,
			Written:     written,
			WrittenPath: writtenPath,
			Document:    string(rendered),
			Report:      reportPayload(result.Report),
		}
		b, err := json.Marshal(newEnvelope("config.migrate", payload))
		if err != nil {
			return duoerr.New("internal.config_migrate_encode_failed", fmt.Sprintf("encoding the config migrate result: %v", err))
		}
		_, err = fmt.Fprintln(streams.Out, string(b))
		return err
	}

	var b strings.Builder
	if written {
		fmt.Fprintf(&b, "config migrate: wrote %s (schema: %s)\n\n", writtenPath, result.Document.Schema)
	} else {
		b.Write(rendered)
		b.WriteString("\n\n")
	}
	writeMigrationReport(&b, result.Report)
	_, err = fmt.Fprint(streams.Out, b.String())
	return err
}

func reportPayload(report config.MigrationReport) migrateReportPayload {
	stb := make([]stateToBindPayload, 0, len(report.StateToBind))
	for _, s := range report.StateToBind {
		stb = append(stb, stateToBindPayload{
			Variant:     s.Variant,
			SessionHost: s.SessionHost,
			Kind:        s.Kind,
			SocketPath:  s.SocketPath,
			RebindHint:  config.RebindHint,
		})
	}
	return migrateReportPayload{
		Renamed:     nonNil(report.Renamed),
		Defaulted:   nonNil(report.Defaulted),
		Rejected:    nonNil(report.Rejected),
		Manual:      nonNil(report.Manual),
		StateToBind: stb,
	}
}

// nonNil turns a nil slice into an empty one so the --output json envelope's
// report arrays always render as "[]", never "null".
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func writeMigrationReport(b *strings.Builder, report config.MigrationReport) {
	b.WriteString("migration report:\n")
	writeReportSection(b, "renamed", report.Renamed)
	writeReportSection(b, "defaulted", report.Defaulted)
	writeReportSection(b, "rejected", report.Rejected)
	writeReportSection(b, "manual", report.Manual)

	if len(report.StateToBind) == 0 {
		b.WriteString("  state to bind: (none)\n")
		return
	}
	b.WriteString("  state to bind:\n")
	for _, s := range report.StateToBind {
		socket := s.SocketPath
		if socket == "" {
			socket = "(no socket_path recorded in the source document)"
		}
		fmt.Fprintf(b, "    variant %s: session_host %s (kind %s), socket %s — rebind with `%s`\n",
			s.Variant, s.SessionHost, s.Kind, socket, config.RebindHint)
	}
}

func writeReportSection(b *strings.Builder, label string, items []string) {
	if len(items) == 0 {
		fmt.Fprintf(b, "  %s: (none)\n", label)
		return
	}
	fmt.Fprintf(b, "  %s:\n", label)
	for _, it := range items {
		fmt.Fprintf(b, "    %s\n", it)
	}
}
