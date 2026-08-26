package config

import (
	"bytes"
	"fmt"
	"os"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/procrastivity/duo/internal/duoerr"
)

// SchemaV3 is the duo.config/v3 marker literal the strict resolver accepts.
//
// v3's central shape change from v2 (schemas/duo-config-v3.schema.json,
// notes/42-config-v3-late-binding.md §2): the session host late-binds at
// launch materialization instead of being authored in config. There is no
// v3 "composition" object — launch_variants are host-free (agent_runtime +
// model_line + model_family + optional provider/append_arguments), and
// preset candidates reference a variant directly instead of a composition.
const SchemaV3 = "duo.config/v3"

// Stable, named error codes the strict v3 resolver raises, on top of the
// shared vocabulary it reuses directly from v2.go (see the "shared with
// v2.go" note below): ErrCodeSchemaMissing, ErrCodeSchemaV1Unsupported,
// ErrCodeSchemaUnrecognized, and ErrCodeDecodeFailed. Every v3-only code
// stays namespaced "config." like v2's, for the same reason (see v2.go):
// internal/exitcode maps anything without a "refusal."/"internal." prefix
// to the default user-failure exit, and "config." plainly is neither.
const (
	// ErrCodeSchemaV2Unsupported reports a document explicitly marked
	// duo.config/v2, presented to the strict v3 resolver. v2 documents are
	// not silently reinterpreted as v3 — the caller migrates first with
	// duo config migrate --to duo.config/v3 (step 07). This is the one
	// marker-dispatch outcome v2.go has no equivalent for, since v2.go's
	// own resolver accepts duo.config/v2 as its native marker.
	ErrCodeSchemaV2Unsupported = "config.schema_v2_unsupported"

	// ErrCodeVariantModelLineRequired reports a launch_variants entry with
	// no model_line. duo.config/v3 requires one on every variant; the
	// resolver never infers it.
	ErrCodeVariantModelLineRequired = "config.variant_model_line_required"
	// ErrCodeVariantModelFamilyRequired reports a launch_variants entry
	// with no model_family. duo.config/v3 requires one on every variant —
	// the new third require/avoid axis (notes/42 §4) — and the resolver
	// never infers it (Step 07's migration reports it as the literal
	// string "manual" rather than guessing).
	ErrCodeVariantModelFamilyRequired = "config.variant_model_family_required"
	// ErrCodeVariantUnknownRuntime reports a launch_variants entry whose
	// agent_runtime names no entry in agent_runtimes. v3 has no schema-level
	// way to express a dangling reference, so the resolver checks it by
	// hand, the same way v2.go checks composition required fields by hand.
	ErrCodeVariantUnknownRuntime = "config.variant_unknown_runtime"
	// ErrCodeCandidateUnknownVariant reports a preset candidate whose
	// variant names no entry in launch_variants. Checked by hand for the
	// same reason as ErrCodeVariantUnknownRuntime.
	ErrCodeCandidateUnknownVariant = "config.candidate_unknown_variant"
)

// DocumentV3 is the strictly-resolved duo.config/v3 root. Every field
// mirrors schemas/duo-config-v3.schema.json's root properties. Like
// ParseV2, ParseV3 never infers, defaults, or otherwise fills a missing
// required field, with the one named contractual default it inherits from
// v2: preset.Selection defaults to "ordered".
//
// The non-launch blocks (Authority, Workspaces, Control, Collaboration,
// PresentationClients, Assets, Projections) are unchanged from
// DocumentV2 — same Go shapes, carried over per the Step 06 spec — so no
// separate v2/v3 named type exists for them.
type DocumentV3 struct {
	Schema              string
	Authority           map[string]any
	Workspaces          map[string]map[string]any
	SessionHosts        SessionHostPolicy
	AgentRuntimes       map[string]AgentRuntime
	LaunchVariants      map[string]LaunchVariant
	Presets             map[string]PresetV3
	Control             map[string]any
	Collaboration       map[string]any
	PresentationClients map[string]map[string]any
	Assets              map[string]any
	Projections         map[string]map[string]any
}

// SessionHostPolicy is the duo.config/v3 session_hosts block: host-kind
// policy only. It never carries a host instance or socket path — those are
// state (a workspace<->host correlation fact in duo.db), never config
// (schemas/duo-config-v3.schema.json $defs/session_hosts;
// notes/43-config-v3-change-control.md item 13).
type SessionHostPolicy struct {
	// Prefer is the ordered, unique host-kind preference list. It is the
	// only ordered field in the block, consumed only at the policy-default
	// rung of the resolver's fixed deduction ranking (explicit flag >
	// workspace correlation > cwd correlation > ambient environment >
	// policy default).
	//
	// Thread-2 decision (workplan Risk 1, step 01's grammar call): a
	// prefer entry does not need a matching Kinds stanza. Kinds is where a
	// kind gets *disabled*; an absent stanza (like an absent Enabled flag
	// inside one) means enabled by the schema's own stated default. So
	// ParseV3 does not validate that every Prefer name also appears in
	// Kinds — treating "prefer names known kinds" as a knowledge-not-
	// requirement, not a reference-integrity rule. This matches the
	// suggested position the Step 06 spec left open; see the "prefer
	// without a kinds stanza" finding for the fixture-level rationale.
	Prefer []string
	// Kinds holds a stanza only for kinds a document wants to say
	// something about. A kind is "known" to the schema by appearing here,
	// but ParseV3 does not require every Prefer entry to be known — see
	// Prefer's doc comment.
	Kinds map[string]SessionHostKind
	// Deduce holds a stanza only for deduction sources a document wants
	// to say something about, over the closed set {workspace, cwd, env,
	// default}. It only ever enables or disables a source — it never
	// reorders the fixed ranking (schema description; also Step 11's
	// boundary).
	Deduce map[string]SessionHostDeduceSource
}

// SessionHostKind is one session_hosts.kinds.<kind> stanza. Enabled is a
// pointer so ParseV3 preserves "absent" distinctly from "false": both the
// stanza being absent and Enabled being absent mean enabled (schema
// description), and a caller resolving the effective policy applies that
// default itself — ParseV3 never collapses the distinction away.
//
// LaunchTarget is likewise a pointer so absent stays distinct from a set
// value: absent means the host's built-in default (notes/51 record 1);
// "tab" or "pane" is the config-authored default the launcher consults.
// It is a launcher input, never a resolution input (I-3).
//
// close_on_exit is accepted on the raw stanza so the synced fixture
// decodes (KnownFields), but is not promoted here — step 04 wires it.
type SessionHostKind struct {
	Enabled      *bool
	LaunchTarget *string `json:",omitempty"`
}

// SessionHostDeduceSource is one session_hosts.deduce.<source> stanza,
// same absent-means-enabled shape as SessionHostKind.
type SessionHostDeduceSource struct {
	Enabled *bool
}

// AgentRuntime is one duo.config/v3 agent_runtimes.<name> entry: the
// executable shape a launch_variant's agent_runtime field references. Kind
// and Executable are the schema's required pair.
type AgentRuntime struct {
	Kind       string
	Executable string
	Arguments  []string
}

// LaunchVariant is one duo.config/v3 launch_variants.<name> entry: runtime
// x command shape x model line, host-free (schema description). AgentRuntime,
// ModelLine, and ModelFamily are the schema's required trio — ParseV3 checks
// all three by hand (the reference-integrity ones can't be expressed in
// JSON Schema, and the requiredness ones need a hand check the same way
// v2.go's composition fields do).
type LaunchVariant struct {
	AgentRuntime    string
	ModelLine       string
	ModelFamily     string
	Provider        string
	AppendArguments []string
}

// PresetV3 is one duo.config/v3 named launch intent. Structurally identical
// to v2's Preset except its leaves' candidates reference a launch_variant
// directly instead of a composition (PresetCandidateV3.Variant vs.
// PresetCandidate.Composition) — named distinctly (the V3 suffix) only
// because Preset/PresetLeaf/PresetCandidate/PresetRelation are already
// taken by v2.go in this package.
type PresetV3 struct {
	// Selection is "ordered" or "random"; a document that omits it
	// resolves to "ordered", same named contractual default as v2.
	Selection string
	Leaves    map[string]PresetLeafV3
	Relations []PresetRelationV3
}

// PresetLeafV3 is one named leaf of a v3 preset: an ordered list of
// launch_variant candidates.
type PresetLeafV3 struct {
	Candidates []PresetCandidateV3
}

// PresetCandidateV3 references one launch_variant by name. ParseV3 checks
// that the name resolves against LaunchVariants (ErrCodeCandidateUnknownVariant).
type PresetCandidateV3 struct {
	Variant string
}

// PresetRelationV3 is one cross-leaf relation. The v3 schema's kind enum
// adds distinct_model_family beside v2's distinct_model_line
// (schemas/duo-config-v3.schema.json $defs/preset_relation); ParseV3 copies
// Kind through without validating enum membership, the same conservatism
// v2.go applies to PresetRelation.Kind.
type PresetRelationV3 struct {
	Kind   string
	Leaves []string
}

// rawDocumentV3 is the strict decode target for a duo.config/v3 root, mirroring
// rawDocument's role for v2: decoding with the YAML decoder's KnownFields(true)
// mode rejects any unknown top-level key, which is how "no compositions key"
// and "unknown keys are refused" are both enforced — there is deliberately no
// Compositions field here, so a document carrying one fails to decode with
// ErrCodeDecodeFailed exactly like any other unrecognized key would.
//
// Unlike rawDocument, every v3 launch-side block (session_hosts,
// agent_runtimes, launch_variants, presets) is schema-closed
// (additionalProperties: false all the way down), so each decodes into a
// strictly-typed raw* struct rather than v2's looser map[string]any leaves —
// KnownFields(true) then rejects an unknown key inside those blocks too,
// satisfying "unknown keys are refused... inside the launch blocks that the
// schema closes."
type rawDocumentV3 struct {
	Schema              string                      `yaml:"schema"`
	Authority           map[string]any              `yaml:"authority"`
	Workspaces          map[string]map[string]any   `yaml:"workspaces"`
	SessionHosts        rawSessionHosts             `yaml:"session_hosts"`
	AgentRuntimes       map[string]rawAgentRuntime  `yaml:"agent_runtimes"`
	LaunchVariants      map[string]rawLaunchVariant `yaml:"launch_variants"`
	Presets             map[string]rawPresetV3      `yaml:"presets"`
	Control             map[string]any              `yaml:"control"`
	Collaboration       map[string]any              `yaml:"collaboration"`
	PresentationClients map[string]map[string]any   `yaml:"presentation_clients"`
	Assets              map[string]any              `yaml:"assets"`
	Projections         map[string]map[string]any   `yaml:"projections"`
}

type rawSessionHosts struct {
	Prefer []string                              `yaml:"prefer"`
	Kinds  map[string]rawSessionHostKind         `yaml:"kinds"`
	Deduce map[string]rawSessionHostDeduceSource `yaml:"deduce"`
}

type rawSessionHostKind struct {
	Enabled      *bool   `yaml:"enabled"`
	LaunchTarget *string `yaml:"launch_target"`
	// CloseOnExit is decoded so KnownFields accepts the synced
	// duo-external-v1 config fixture; SessionHostKind does not carry it
	// until step 04 wires close-on-exit behavior.
	CloseOnExit *bool `yaml:"close_on_exit"`
}

type rawSessionHostDeduceSource struct {
	Enabled *bool `yaml:"enabled"`
}

type rawAgentRuntime struct {
	Kind       string   `yaml:"kind"`
	Executable string   `yaml:"executable"`
	Arguments  []string `yaml:"arguments"`
}

type rawLaunchVariant struct {
	AgentRuntime    string   `yaml:"agent_runtime"`
	ModelLine       string   `yaml:"model_line"`
	ModelFamily     string   `yaml:"model_family"`
	Provider        string   `yaml:"provider"`
	AppendArguments []string `yaml:"append_arguments"`
}

type rawPresetV3 struct {
	Selection string                     `yaml:"selection"`
	Leaves    map[string]rawPresetLeafV3 `yaml:"leaves"`
	Relations []rawPresetRelationV3      `yaml:"relations"`
}

type rawPresetLeafV3 struct {
	Candidates []rawPresetCandidateV3 `yaml:"candidates"`
}

type rawPresetCandidateV3 struct {
	Variant string `yaml:"variant"`
}

type rawPresetRelationV3 struct {
	Kind   string   `yaml:"kind"`
	Leaves []string `yaml:"leaves"`
}

// LoadV3 reads path and strictly resolves it as a duo.config/v3 document.
func LoadV3(path string) (DocumentV3, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DocumentV3{}, duoerr.New(ErrCodeDecodeFailed,
			fmt.Sprintf("config: reading %s: %v", path, err))
	}
	return ParseV3(data)
}

// ParseV3 strictly parses and validates data as a duo.config/v3 document. A
// document with no "schema" field, one marked duo.config/v1, and one marked
// duo.config/v2 each fail with their own distinct, stable error code — none
// is silently reinterpreted as v3. data may be YAML or JSON, same as ParseV2.
//
// ParseV3 shares its schema-marker probe (peekSchema) and three of its
// error codes (ErrCodeSchemaMissing, ErrCodeSchemaV1Unsupported,
// ErrCodeSchemaUnrecognized, ErrCodeDecodeFailed) directly with ParseV2,
// unchanged — see v2.go. Those four codes name a document-shape problem
// (no marker, a marker naming a version this resolver flatly does not
// speak, or a decode failure) that is genuinely version-independent: which
// resolver raised it is already known from which Parse* function the
// caller invoked, and the message text (constructed fresh at each call
// site below) carries the version-specific detail. Only the "this document
// is a v2 document, specifically" case needs a v3-only code
// (ErrCodeSchemaV2Unsupported), because v2.go has no use for it — its own
// resolver accepts duo.config/v2 natively.
func ParseV3(data []byte) (DocumentV3, error) {
	marker, err := peekSchema(data)
	if err != nil {
		return DocumentV3{}, err
	}
	switch marker {
	case SchemaV3:
		// Fall through to the full strict decode below.
	case "":
		return DocumentV3{}, duoerr.New(ErrCodeSchemaMissing,
			`config: document declares no "schema" field; duo.config/v3 requires schema: duo.config/v3`)
	case SchemaV1:
		return DocumentV3{}, duoerr.New(ErrCodeSchemaV1Unsupported,
			`config: document declares schema: duo.config/v1; the strict v3 resolver never reinterprets a v1 document as v3, and no v1-to-v3 migration path exists yet`)
	case SchemaV2:
		return DocumentV3{}, duoerr.New(ErrCodeSchemaV2Unsupported,
			`config: document declares schema: duo.config/v2; the strict v3 resolver never reinterprets a v2 document as v3 — migrate it first (duo config migrate --to duo.config/v3)`)
	default:
		return DocumentV3{}, duoerr.New(ErrCodeSchemaUnrecognized,
			fmt.Sprintf("config: document declares schema: %q, which duo.config/v3 does not recognize", marker))
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var raw rawDocumentV3
	if err := dec.Decode(&raw); err != nil {
		return DocumentV3{}, duoerr.New(ErrCodeDecodeFailed,
			fmt.Sprintf("config: decoding duo.config/v3 document: %v", err))
	}

	agentRuntimes := resolveAgentRuntimes(raw.AgentRuntimes)

	launchVariants, err := resolveLaunchVariants(raw.LaunchVariants, agentRuntimes)
	if err != nil {
		return DocumentV3{}, err
	}

	presets, err := resolvePresetsV3(raw.Presets, launchVariants)
	if err != nil {
		return DocumentV3{}, err
	}

	sessionHosts, err := resolveSessionHosts(raw.SessionHosts)
	if err != nil {
		return DocumentV3{}, err
	}

	return DocumentV3{
		Schema:              raw.Schema,
		Authority:           raw.Authority,
		Workspaces:          raw.Workspaces,
		SessionHosts:        sessionHosts,
		AgentRuntimes:       agentRuntimes,
		LaunchVariants:      launchVariants,
		Presets:             presets,
		Control:             raw.Control,
		Collaboration:       raw.Collaboration,
		PresentationClients: raw.PresentationClients,
		Assets:              raw.Assets,
		Projections:         raw.Projections,
	}, nil
}

// resolveSessionHosts converts the strictly-decoded raw session_hosts block
// into SessionHostPolicy. Prefer entries are never checked against Kinds
// (thread-2; SessionHostPolicy.Prefer's doc comment). launch_target, when
// set, must be tab or pane.
func resolveSessionHosts(raw rawSessionHosts) (SessionHostPolicy, error) {
	var kinds map[string]SessionHostKind
	if raw.Kinds != nil {
		kinds = make(map[string]SessionHostKind, len(raw.Kinds))
		for name, k := range raw.Kinds {
			if k.LaunchTarget != nil {
				switch *k.LaunchTarget {
				case "tab", "pane":
				default:
					return SessionHostPolicy{}, duoerr.New(ErrCodeDecodeFailed,
						fmt.Sprintf("config: session_hosts.kinds.%s.launch_target %q is not tab or pane", name, *k.LaunchTarget))
				}
			}
			kinds[name] = SessionHostKind{
				Enabled:      k.Enabled,
				LaunchTarget: k.LaunchTarget,
			}
		}
	}

	var deduce map[string]SessionHostDeduceSource
	if raw.Deduce != nil {
		deduce = make(map[string]SessionHostDeduceSource, len(raw.Deduce))
		for name, d := range raw.Deduce {
			deduce[name] = SessionHostDeduceSource(d)
		}
	}

	return SessionHostPolicy{
		Prefer: append([]string(nil), raw.Prefer...),
		Kinds:  kinds,
		Deduce: deduce,
	}, nil
}

// resolveAgentRuntimes converts the strictly-decoded raw agent_runtimes
// block into the resolved shape. There is no cross-reference to check here
// (nothing an agent_runtime entry names has to exist elsewhere), so —
// unlike resolveLaunchVariants and resolvePresetsV3 — this never fails.
func resolveAgentRuntimes(raw map[string]rawAgentRuntime) map[string]AgentRuntime {
	if raw == nil {
		return nil
	}
	out := make(map[string]AgentRuntime, len(raw))
	for name, r := range raw {
		out[name] = AgentRuntime{
			Kind:       r.Kind,
			Executable: r.Executable,
			Arguments:  append([]string(nil), r.Arguments...),
		}
	}
	return out
}

// resolveLaunchVariants validates every variant's two required fields
// (ErrCodeVariantModelLineRequired, ErrCodeVariantModelFamilyRequired) and
// that its agent_runtime reference resolves against runtimes
// (ErrCodeVariantUnknownRuntime). Names are checked in sorted order so
// which variant's error surfaces first is deterministic, mirroring
// v2.go's resolveCompositions. Model line is checked before model family,
// and both are checked before the runtime reference — an arbitrary but
// fixed order (see TestParseV3_MissingModelLineWinsOverMissingModelFamily).
func resolveLaunchVariants(raw map[string]rawLaunchVariant, runtimes map[string]AgentRuntime) (map[string]LaunchVariant, error) {
	if raw == nil {
		return nil, nil
	}

	names := sortedKeys(raw)
	out := make(map[string]LaunchVariant, len(raw))
	for _, name := range names {
		v := raw[name]

		if v.ModelLine == "" {
			return nil, duoerr.New(ErrCodeVariantModelLineRequired,
				fmt.Sprintf("config: launch_variants.%s has no model_line — duo.config/v3 requires one, and never infers it", name))
		}
		if v.ModelFamily == "" {
			return nil, duoerr.New(ErrCodeVariantModelFamilyRequired,
				fmt.Sprintf("config: launch_variants.%s has no model_family — duo.config/v3 requires one, and never infers it", name))
		}
		if _, ok := runtimes[v.AgentRuntime]; !ok {
			return nil, duoerr.New(ErrCodeVariantUnknownRuntime,
				fmt.Sprintf("config: launch_variants.%s references agent_runtime %q, which is not declared in agent_runtimes", name, v.AgentRuntime))
		}

		out[name] = LaunchVariant{
			AgentRuntime:    v.AgentRuntime,
			ModelLine:       v.ModelLine,
			ModelFamily:     v.ModelFamily,
			Provider:        v.Provider,
			AppendArguments: append([]string(nil), v.AppendArguments...),
		}
	}
	return out, nil
}

// resolvePresetsV3 converts the strictly-decoded raw preset shape into the
// resolved PresetV3 shape, applying the selection: ordered default and
// validating that every candidate's variant reference resolves against
// variants (ErrCodeCandidateUnknownVariant). Preset names, then leaf names
// within a preset, are checked in sorted order for the same determinism
// reason as resolveLaunchVariants; candidates within a leaf are checked in
// document order, since that order is itself meaningful (ordered
// selection) and not just an iteration artifact.
func resolvePresetsV3(raw map[string]rawPresetV3, variants map[string]LaunchVariant) (map[string]PresetV3, error) {
	if raw == nil {
		return nil, nil
	}

	presetNames := sortedKeys(raw)
	out := make(map[string]PresetV3, len(raw))
	for _, name := range presetNames {
		p := raw[name]

		selection := p.Selection
		if selection == "" {
			selection = "ordered"
		}

		leafNames := sortedKeys(p.Leaves)
		leaves := make(map[string]PresetLeafV3, len(p.Leaves))
		for _, leafName := range leafNames {
			leaf := p.Leaves[leafName]
			candidates := make([]PresetCandidateV3, 0, len(leaf.Candidates))
			for _, c := range leaf.Candidates {
				if _, ok := variants[c.Variant]; !ok {
					return nil, duoerr.New(ErrCodeCandidateUnknownVariant,
						fmt.Sprintf("config: presets.%s leaf %q candidate references variant %q, which is not declared in launch_variants", name, leafName, c.Variant))
				}
				candidates = append(candidates, PresetCandidateV3(c))
			}
			leaves[leafName] = PresetLeafV3{Candidates: candidates}
		}

		relations := make([]PresetRelationV3, 0, len(p.Relations))
		for _, r := range p.Relations {
			relations = append(relations, PresetRelationV3{
				Kind:   r.Kind,
				Leaves: append([]string(nil), r.Leaves...),
			})
		}

		out[name] = PresetV3{Selection: selection, Leaves: leaves, Relations: relations}
	}
	return out, nil
}

// sortedKeys returns m's keys in sorted order, so callers that must fail on
// the first invalid entry do so deterministically regardless of Go's map
// iteration order — the same determinism v2.go's resolveCompositions
// achieves by hand for its one map.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
