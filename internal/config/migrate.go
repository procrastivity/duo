package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/procrastivity/duo/internal/duoerr"
)

// This file implements the duo.config/v2 -> duo.config/v3 transform behind
// `duo config migrate --to duo.config/v3` (Step 07; internal/cli/config.go
// is the thin CLI wrapper). The transform itself is normative:
// duo-vnext-installation-contract.md §1.3 "Migration to duo.config/v3"
// (2026-08-24 handoff 22 amendment), items 1-9. Every numbered comment below
// cites the item it implements.
//
// Sharing with v2.go/v3.go: MigrateV2ToV3 calls ParseV2 and reuses the
// unexported peekSchema/SchemaV1/SchemaV2/SchemaV3 vocabulary those two
// files already export within the package — no change to either file was
// needed to get this sharing (workplan Step 07 boundary).

// Stable, named error codes MigrateV2ToV3 and the CLI wrapper raise. All are
// "refusal."-prefixed: every one is a guard declining to do something it
// could technically attempt, never a generic parse failure (ParseV2's own
// "config."-prefixed codes surface unchanged when the input fails plain v2
// validation).
const (
	// ErrCodeMigrateV1Unsupported reports a duo.config/v1 input. §1.3 item
	// 8: "Refuses a duo.config/v1 input. There is no v1 to v3 path." — the
	// message names that truthfully rather than pointing at a command that
	// does not exist.
	ErrCodeMigrateV1Unsupported = "refusal.config_migrate_v1_unsupported"
	// ErrCodeMigrateNotV2 reports an input that is neither v1 nor v2 (no
	// marker, an already-v3 document, or an unrecognized marker) presented
	// to the v2->v3 transform.
	ErrCodeMigrateNotV2 = "refusal.config_migrate_expects_v2"
	// ErrCodeMigrateSetModelFamilyMalformed reports a --set-model-family
	// value that is not "variant=label", or whose variant names no
	// migrated launch_variant, or whose label is empty.
	ErrCodeMigrateSetModelFamilyMalformed = "refusal.config_migrate_set_model_family_malformed"
	// ErrCodeMigrateModelFamilyManual reports a write attempted while at
	// least one launch_variant's model_family is still the literal string
	// "manual" — §1.3 item 7's first clause.
	ErrCodeMigrateModelFamilyManual = "refusal.config_migrate_model_family_manual"
	// ErrCodeMigrateWriteValidationFailed reports a write attempted whose
	// resulting document fails ParseV3 — §1.3 item 7's second clause
	// ("or validation fails").
	ErrCodeMigrateWriteValidationFailed = "refusal.config_migrate_write_validation_failed"
	// ErrCodeMigrateWriteTargetUnrecognized reports a --write PATH that
	// already exists and does not declare a recognized duo.config schema
	// marker — §1.3 item 1's "It never overwrites an unrecognized file."
	ErrCodeMigrateWriteTargetUnrecognized = "refusal.config_migrate_write_target_unrecognized"
)

// ModelFamilyManual is the literal model_family value MigrateV2ToV3 reports
// on every migrated variant it cannot author a value for — which, per
// §1.3 item 6, is every variant: the transform infers model_family from
// nothing (runtime kind, executable, arguments, observed provider, vendor,
// source model, or normalized family all stay unconsulted).
const ModelFamilyManual = "manual"

// StateToBind is one migrated variant's dropped session-host binding,
// reported rather than carried into the v3 document — §1.3 item 3. A
// variant with no v2 session_host reference produces no StateToBind entry.
type StateToBind struct {
	// Variant is the migrated duo.config/v3 launch_variants key — the
	// source composition's own name, unchanged (MigrateV2ToV3 names a
	// migrated variant after the composition it came from).
	Variant string
	// SessionHost is the v2 session_hosts key the source launch_variant
	// referenced.
	SessionHost string
	// Kind is that session_hosts entry's "kind" field, when present.
	Kind string
	// SocketPath is that session_hosts entry's "socket_path" field, when
	// present. Empty when the source document recorded none — the report
	// still names the entry so the operator knows a binding is owed, it
	// just cannot print a path the source never carried.
	SocketPath string
}

// RebindHint is the fixed pointer text a StateToBind report line carries:
// step 09's `duo workspace host rebind` verb is where the operator actually
// re-establishes a workspace<->host correlation. Step 07 only prints the
// pointer — it does not implement or call that verb.
const RebindHint = "duo workspace host rebind"

// MigrationReport is the general shape §1.3's intro paragraph asks every
// `duo config migrate` report to carry: "renamed, defaulted, rejected, and
// manual fields." Each slice holds dotted source-field paths (Renamed
// entries read "source -> destination"). For the v2->v3 transform
// specifically, Defaulted and Rejected stay empty: every v2 field the
// transform touches either moves somewhere in the v3 document (Renamed) or
// surfaces in StateToBind — nothing is silently defaulted or discarded. The
// fields still exist (rather than being dropped from the type) because the
// contract states the shape for `duo config migrate` in general, not only
// this one transform.
type MigrationReport struct {
	Renamed     []string
	Defaulted   []string
	Rejected    []string
	Manual      []string
	StateToBind []StateToBind
}

// MigrationResult is MigrateV2ToV3's return value: the migrated document
// (both as parsed structure and pre-rendered bytes in Format) plus the
// report the CLI prints or embeds in its --json envelope.
type MigrationResult struct {
	Document DocumentV3Draft
	Report   MigrationReport
	// Format is "json" or "yaml" — the format Render will use, chosen to
	// match the source document's own format (see detectFormat).
	Format string
}

// DocumentV3Draft is the migration's output shape: a rawDocumentV3-like
// structure MigrateV2ToV3 builds directly, kept distinct from rawDocumentV3
// (v3.go) because a draft may legitimately carry model_family: "manual" —
// syntactically valid (ParseV3 only checks non-empty), semantically
// incomplete — and because it needs both yaml and json struct tags to
// render either format from one value (v3.go's raw type only ever needs to
// decode YAML). Building this directly, rather than reusing rawDocumentV3,
// keeps v3.go untouched (workplan Step 07 boundary).
type DocumentV3Draft struct {
	Schema              string                        `yaml:"schema" json:"schema"`
	Authority           map[string]any                `yaml:"authority,omitempty" json:"authority,omitempty"`
	Workspaces          map[string]map[string]any     `yaml:"workspaces,omitempty" json:"workspaces,omitempty"`
	SessionHosts        *draftSessionHosts            `yaml:"session_hosts,omitempty" json:"session_hosts,omitempty"`
	AgentRuntimes       map[string]draftAgentRuntime  `yaml:"agent_runtimes,omitempty" json:"agent_runtimes,omitempty"`
	LaunchVariants      map[string]draftLaunchVariant `yaml:"launch_variants,omitempty" json:"launch_variants,omitempty"`
	Presets             map[string]draftPreset        `yaml:"presets,omitempty" json:"presets,omitempty"`
	Control             map[string]any                `yaml:"control,omitempty" json:"control,omitempty"`
	Collaboration       map[string]any                `yaml:"collaboration,omitempty" json:"collaboration,omitempty"`
	PresentationClients map[string]map[string]any     `yaml:"presentation_clients,omitempty" json:"presentation_clients,omitempty"`
	Assets              map[string]any                `yaml:"assets,omitempty" json:"assets,omitempty"`
	Projections         map[string]map[string]any     `yaml:"projections,omitempty" json:"projections,omitempty"`
}

type draftSessionHosts struct {
	Prefer []string                    `yaml:"prefer,omitempty" json:"prefer,omitempty"`
	Kinds  map[string]draftEnabledFlag `yaml:"kinds,omitempty" json:"kinds,omitempty"`
}

type draftEnabledFlag struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

type draftAgentRuntime struct {
	Kind       string   `yaml:"kind" json:"kind"`
	Executable string   `yaml:"executable" json:"executable"`
	Arguments  []string `yaml:"arguments,omitempty" json:"arguments,omitempty"`
}

type draftLaunchVariant struct {
	AgentRuntime    string   `yaml:"agent_runtime" json:"agent_runtime"`
	ModelLine       string   `yaml:"model_line" json:"model_line"`
	ModelFamily     string   `yaml:"model_family" json:"model_family"`
	Provider        string   `yaml:"provider,omitempty" json:"provider,omitempty"`
	AppendArguments []string `yaml:"append_arguments,omitempty" json:"append_arguments,omitempty"`
}

type draftPreset struct {
	Selection string                     `yaml:"selection,omitempty" json:"selection,omitempty"`
	Leaves    map[string]draftPresetLeaf `yaml:"leaves" json:"leaves"`
	Relations []draftPresetRelation      `yaml:"relations,omitempty" json:"relations,omitempty"`
}

type draftPresetLeaf struct {
	Candidates []draftPresetCandidate `yaml:"candidates" json:"candidates"`
}

type draftPresetCandidate struct {
	Variant string `yaml:"variant" json:"variant"`
}

type draftPresetRelation struct {
	Kind   string   `yaml:"kind" json:"kind"`
	Leaves []string `yaml:"leaves" json:"leaves"`
}

// MigrateV2ToV3File reads path and runs MigrateV2ToV3 on its content.
func MigrateV2ToV3File(path string, overrides map[string]string) (MigrationResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MigrationResult{}, duoerr.New(ErrCodeDecodeFailed,
			fmt.Sprintf("config: reading %s: %v", path, err))
	}
	return MigrateV2ToV3(data, overrides)
}

// MigrateV2ToV3 transforms data, a duo.config/v2 document, into a
// duo.config/v3 draft (duo-vnext-installation-contract.md §1.3, "Migration
// to duo.config/v3", items 1-6, 8). overrides applies a
// launch_variant-name -> model_family map on top of the "manual" default
// (item 6) — the CLI's --set-model-family authoring path (workplan Step 07:
// "pick one authoring path"). It never fails on manual model_family by
// itself; that refusal is Write's job (item 7), so a caller can always
// inspect a fully-manual draft (e.g. to print it) without --write.
func MigrateV2ToV3(data []byte, overrides map[string]string) (MigrationResult, error) {
	marker, err := peekSchema(data)
	if err != nil {
		return MigrationResult{}, err
	}
	switch marker {
	case SchemaV1:
		return MigrationResult{}, duoerr.New(ErrCodeMigrateV1Unsupported,
			`config migrate: input declares schema: duo.config/v1; there is no duo.config/v1-to-duo.config/v3 migration path (duo-vnext-installation-contract.md §1.3 item 8) — duo config migrate --to duo.config/v3 accepts a duo.config/v2 input only`)
	case SchemaV2:
		// Fall through.
	case SchemaV3:
		return MigrationResult{}, duoerr.New(ErrCodeMigrateNotV2,
			`config migrate: input already declares schema: duo.config/v3; nothing to migrate`)
	case "":
		return MigrationResult{}, duoerr.New(ErrCodeMigrateNotV2,
			`config migrate: input declares no "schema" field; duo config migrate --to duo.config/v3 accepts a duo.config/v2 input only`)
	default:
		return MigrationResult{}, duoerr.New(ErrCodeMigrateNotV2,
			fmt.Sprintf("config migrate: input declares schema: %q, which duo config migrate --to duo.config/v3 does not accept — it accepts a duo.config/v2 input only", marker))
	}

	doc, err := ParseV2(data)
	if err != nil {
		return MigrationResult{}, err
	}

	compositionOrder, err := orderedMapKeys(data, "compositions")
	if err != nil {
		return MigrationResult{}, err
	}
	// A composition ParseV2 resolved but the ordered walk did not see would
	// mean the two disagree about the document's own content, which can't
	// happen for a document ParseV2 already accepted — but iterate whatever
	// resolveCompositions found, in sorted order, as a defensive fallback
	// so a migration never silently drops a composition.
	compositionOrder = withMissing(compositionOrder, doc.Compositions)

	report := MigrationReport{}
	draft := DocumentV3Draft{
		Schema:              SchemaV3,
		Authority:           doc.Authority,
		Workspaces:          doc.Workspaces,
		Control:             doc.Control,
		Collaboration:       doc.Collaboration,
		PresentationClients: doc.PresentationClients,
		Assets:              doc.Assets,
		Projections:         doc.Projections,
	}

	runtimes := map[string]draftAgentRuntime{}
	runtimeClaimedBy := map[string]string{} // runtime name -> first composition that set its base
	variants := map[string]draftLaunchVariant{}
	preferOrder := make([]string, 0, len(compositionOrder))
	preferSeen := map[string]bool{}
	kindDisabled := map[string]bool{}

	for _, compName := range compositionOrder {
		comp, ok := doc.Compositions[compName]
		if !ok {
			continue
		}
		lvName := comp.LaunchVariant
		lvFields := doc.LaunchVariants[lvName]

		runtimeName, _ := stringField(lvFields, "agent_runtime")
		executable, _ := stringField(lvFields, "executable")
		arguments := stringSliceField(lvFields, "arguments")

		// Item 2: move executable and the base arguments onto the agent
		// runtime; a later variant sharing the same runtime with different
		// arguments keeps its own as append_arguments instead of
		// overwriting the runtime's base.
		if runtimeName != "" {
			if _, claimed := runtimes[runtimeName]; !claimed {
				kind, _ := stringField(doc.AgentRuntimes[runtimeName], "kind")
				runtimes[runtimeName] = draftAgentRuntime{
					Kind:       kind,
					Executable: executable,
					Arguments:  arguments,
				}
				runtimeClaimedBy[runtimeName] = compName
				report.Renamed = append(report.Renamed,
					fmt.Sprintf("launch_variants.%s.executable -> agent_runtimes.%s.executable", lvName, runtimeName),
					fmt.Sprintf("launch_variants.%s.arguments -> agent_runtimes.%s.arguments", lvName, runtimeName),
				)
			}
		}

		variant := draftLaunchVariant{
			AgentRuntime: runtimeName,
			ModelLine:    comp.ModelLine,
			ModelFamily:  ModelFamilyManual,
		}
		report.Renamed = append(report.Renamed,
			fmt.Sprintf("compositions.%s.model_line -> launch_variants.%s.model_line", compName, compName))
		report.Manual = append(report.Manual,
			fmt.Sprintf("launch_variants.%s.model_family", compName))

		if provider, ok := comp.Extra["provider"].(string); ok && provider != "" {
			variant.Provider = provider
		}

		base := runtimes[runtimeName]
		if runtimeName != "" && runtimeClaimedBy[runtimeName] != compName && !stringSliceEqual(arguments, base.Arguments) {
			variant.AppendArguments = arguments
			report.Renamed = append(report.Renamed,
				fmt.Sprintf("launch_variants.%s.arguments -> launch_variants.%s.append_arguments", lvName, compName))
		}

		// Item 3: drop session_host/socket_path, report as state to bind.
		if hostName, ok := stringField(lvFields, "session_host"); ok && hostName != "" {
			hostFields := doc.SessionHosts[hostName]
			kind, _ := stringField(hostFields, "kind")
			socketPath, _ := stringField(hostFields, "socket_path")
			report.StateToBind = append(report.StateToBind, StateToBind{
				Variant:     compName,
				SessionHost: hostName,
				Kind:        kind,
				SocketPath:  socketPath,
			})
			report.Renamed = append(report.Renamed,
				fmt.Sprintf("launch_variants.%s.session_host (%s) -> state to bind, reported only", lvName, hostName))

			if kind != "" {
				if !preferSeen[kind] {
					preferSeen[kind] = true
					preferOrder = append(preferOrder, kind)
				}
				if disabled, ok := boolField(hostFields, "disabled"); ok && disabled {
					kindDisabled[kind] = true
				}
				if enabled, ok := boolField(hostFields, "enabled"); ok && !enabled {
					kindDisabled[kind] = true
				}
			}
		}

		variants[compName] = variant
	}

	for name, override := range overrides {
		v, ok := variants[name]
		if !ok {
			return MigrationResult{}, duoerr.New(ErrCodeMigrateSetModelFamilyMalformed,
				fmt.Sprintf("config migrate: --set-model-family names variant %q, which no migrated launch_variant (composition) matches", name))
		}
		if override == "" {
			return MigrationResult{}, duoerr.New(ErrCodeMigrateSetModelFamilyMalformed,
				fmt.Sprintf("config migrate: --set-model-family for variant %q has an empty label", name))
		}
		v.ModelFamily = override
		variants[name] = v
		report.Manual = removeString(report.Manual, fmt.Sprintf("launch_variants.%s.model_family", name))
	}

	if len(runtimes) > 0 {
		draft.AgentRuntimes = runtimes
	}
	if len(variants) > 0 {
		draft.LaunchVariants = variants
	}

	if len(preferOrder) > 0 || len(kindDisabled) > 0 {
		sh := &draftSessionHosts{Prefer: preferOrder}
		if len(kindDisabled) > 0 {
			sh.Kinds = map[string]draftEnabledFlag{}
			kinds := make([]string, 0, len(kindDisabled))
			for k := range kindDisabled {
				kinds = append(kinds, k)
			}
			sort.Strings(kinds)
			for _, k := range kinds {
				sh.Kinds[k] = draftEnabledFlag{Enabled: false}
				report.Renamed = append(report.Renamed,
					fmt.Sprintf("session_hosts.%s (disabled) -> session_hosts.kinds.%s.enabled: false", k, k))
			}
		}
		draft.SessionHosts = sh
		report.Renamed = append(report.Renamed, "session_hosts -> session_hosts (host-kind policy only; prefer in order of first appearance among the migrated declarations)")
	}

	// Item 4: rewrite every preset candidate as a launch-variant reference;
	// the compositions root itself has no v3 counterpart.
	if len(doc.Presets) > 0 {
		draft.Presets = map[string]draftPreset{}
		for name, p := range doc.Presets {
			dp := draftPreset{Selection: p.Selection, Leaves: map[string]draftPresetLeaf{}}
			for leafName, leaf := range p.Leaves {
				cands := make([]draftPresetCandidate, 0, len(leaf.Candidates))
				for _, c := range leaf.Candidates {
					cands = append(cands, draftPresetCandidate{Variant: c.Composition})
				}
				dp.Leaves[leafName] = draftPresetLeaf{Candidates: cands}
			}
			for _, r := range p.Relations {
				dp.Relations = append(dp.Relations, draftPresetRelation{Kind: r.Kind, Leaves: append([]string(nil), r.Leaves...)})
			}
			draft.Presets[name] = dp
		}
		report.Renamed = append(report.Renamed, "presets.*.leaves.*.candidates[].composition -> presets.*.leaves.*.candidates[].variant")
		report.Rejected = append(report.Rejected, "compositions (root retired; content redistributed into launch_variants)")
	}

	sort.Strings(report.Renamed)
	sort.Strings(report.Manual)
	sort.Slice(report.StateToBind, func(i, j int) bool { return report.StateToBind[i].Variant < report.StateToBind[j].Variant })

	return MigrationResult{
		Document: draft,
		Report:   report,
		Format:   detectFormat(data),
	}, nil
}

// PendingModelFamilies returns the launch_variant names whose model_family
// is still ModelFamilyManual, sorted. An empty result means Write would not
// refuse on §1.3 item 7's first clause.
func (r MigrationResult) PendingModelFamilies() []string {
	names := make([]string, 0)
	for name, v := range r.Document.LaunchVariants {
		if v.ModelFamily == ModelFamilyManual {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// Render encodes r.Document in r.Format ("json" or "yaml").
func (r MigrationResult) Render() ([]byte, error) {
	if r.Format == "json" {
		return renderJSON(r.Document)
	}
	return renderYAML(r.Document)
}

// Write applies §1.3 item 7 (refuse while any model_family is manual, or
// the draft fails ParseV3) and item 1's create-new/validated-replacement
// semantics, then writes r's rendered document to path.
func (r MigrationResult) Write(path string) error {
	if pending := r.PendingModelFamilies(); len(pending) > 0 {
		return duoerr.New(ErrCodeMigrateModelFamilyManual,
			fmt.Sprintf("config migrate --write refuses: model_family is still %q for launch_variant(s) %s; author every model_family first (e.g. --set-model-family <variant>=<label>)",
				ModelFamilyManual, strings.Join(pending, ", ")))
	}

	rendered, err := r.Render()
	if err != nil {
		return err
	}
	if _, err := ParseV3(rendered); err != nil {
		return duoerr.New(ErrCodeMigrateWriteValidationFailed,
			fmt.Sprintf("config migrate --write refuses: the migrated document does not pass duo.config/v3 validation: %v", err))
	}

	if existing, err := os.ReadFile(path); err == nil {
		marker, perr := peekSchema(existing)
		if perr != nil || (marker != SchemaV1 && marker != SchemaV2 && marker != SchemaV3) {
			return duoerr.New(ErrCodeMigrateWriteTargetUnrecognized,
				fmt.Sprintf("config migrate --write refuses: %s exists and does not declare a recognized duo.config schema marker; refusing to overwrite an unrecognized file", path))
		}
	} else if !os.IsNotExist(err) {
		return duoerr.New(ErrCodeDecodeFailed, fmt.Sprintf("config: checking existing %s before write: %v", path, err))
	}

	if err := os.WriteFile(path, rendered, 0o644); err != nil {
		return duoerr.New("internal.config_migrate_write_failed", fmt.Sprintf("config: writing %s: %v", path, err))
	}
	return nil
}

// detectFormat sniffs data's own format: valid JSON stays JSON, anything
// else (including a YAML document that happens to use block style) renders
// back as YAML. json.Valid is intentionally the only signal — no file
// extension is available for stdin/pipe input, and JSON is the one shape
// unambiguous from content alone.
func detectFormat(data []byte) string {
	if json.Valid(data) {
		return "json"
	}
	return "yaml"
}

func renderJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, duoerr.New("internal.config_migrate_encode_failed", fmt.Sprintf("encoding the migrated document: %v", err))
	}
	return b, nil
}

func renderYAML(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, duoerr.New("internal.config_migrate_encode_failed", fmt.Sprintf("encoding the migrated document: %v", err))
	}
	if err := enc.Close(); err != nil {
		return nil, duoerr.New("internal.config_migrate_encode_failed", fmt.Sprintf("encoding the migrated document: %v", err))
	}
	return buf.Bytes(), nil
}

// orderedMapKeys returns the document-order key list of data's top-level
// section (e.g. "compositions"), by walking the yaml.v3 node tree rather
// than decoding into a Go map — a mapping node's Content preserves source
// order for both YAML and JSON input (JSON is a YAML flow-mapping
// subset), which is what "order of first appearance" (§1.3 item 5) needs
// and no map[string]V decode can give back.
func orderedMapKeys(data []byte, section string) ([]string, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, duoerr.New(ErrCodeDecodeFailed, fmt.Sprintf("config: parsing document to order %s: %v", section, err))
	}
	if len(root.Content) == 0 {
		return nil, nil
	}
	docMapping := root.Content[0]
	sectionNode := findMappingValue(docMapping, section)
	if sectionNode == nil {
		return nil, nil
	}
	keys := make([]string, 0, len(sectionNode.Content)/2)
	for i := 0; i+1 < len(sectionNode.Content); i += 2 {
		keys = append(keys, sectionNode.Content[i].Value)
	}
	return keys, nil
}

// findMappingValue returns the value node paired with key inside mapping (a
// yaml.MappingNode), or nil when mapping is not a mapping node or has no
// such key.
func findMappingValue(mapping *yaml.Node, key string) *yaml.Node {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}

// withMissing appends to order, in sorted order, any key of m not already
// in order — the defensive fallback documented on MigrateV2ToV3's
// compositionOrder computation.
func withMissing(order []string, m map[string]Composition) []string {
	seen := make(map[string]bool, len(order))
	for _, k := range order {
		seen[k] = true
	}
	var missing []string
	for k := range m {
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return append(order, missing...)
}

func stringSliceField(m map[string]any, key string) []string {
	v, ok := m[key]
	if !ok {
		return nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		s, ok := it.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}

func boolField(m map[string]any, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func removeString(s []string, target string) []string {
	out := s[:0]
	for _, v := range s {
		if v != target {
			out = append(out, v)
		}
	}
	return out
}
