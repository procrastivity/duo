// Package portablelauncher owns the launcher-neutral portable launcher
// conformance scenario, capture model, oracle, and offline validator.
package portablelauncher

import "encoding/json"

// Public schema and identity constants for the portable-launcher suite.
const (
	ResultSchema     = "duo.portable-launcher-conformance-result/v1"
	ScenarioSchema   = "duo.portable-launcher-conformance-scenario/v1"
	EvidenceSchema   = "duo.portable-launcher-conformance-evidence/v1"
	IndexSchema      = "duo.portable-launcher-conformance-evidence-index/v1"
	ScenarioName     = "portable-launcher-delegation"
	ScenarioID       = "portable-launcher-delegation/v1"
	ScenarioRevision = 2
	ScrubPolicy      = "portable-launcher-evidence/v1"
)

// Result is one complete portable-launcher conformance result.
type Result struct {
	Schema  string        `json:"schema"`
	Suite   SuiteIdentity `json:"suite"`
	Run     RunIdentity   `json:"run"`
	Pins    Pins          `json:"pins"`
	Stages  []StageResult `json:"stages"`
	Summary Summary       `json:"summary"`
	Scrub   ScrubRecord   `json:"scrub"`
}

// SuiteIdentity pins the scenario and oracle used to interpret a result.
type SuiteIdentity struct {
	Name           string `json:"name"`
	Revision       int    `json:"revision"`
	ManifestDigest string `json:"manifest_digest"`
	OracleDigest   string `json:"oracle_digest"`
}

// RunIdentity records the isolated host and invocation properties of a run.
type RunIdentity struct {
	RunID             string `json:"run_id"`
	ObservedAt        string `json:"observed_at"`
	HostOS            string `json:"host_os"`
	HostArch          string `json:"host_arch"`
	FixtureRoot       string `json:"fixture_root"`
	PluginsEnabled    bool   `json:"plugins_enabled"`
	MCPEnabled        bool   `json:"mcp_enabled"`
	TerminalInputUsed bool   `json:"terminal_input_used"`
}

// Pins records every product and runtime identity required to reproduce a run.
type Pins struct {
	Launcher       LauncherPin       `json:"launcher"`
	Duo            DuoPin            `json:"duo"`
	Skill          SkillPin          `json:"skill"`
	Config         ConfigPin         `json:"config"`
	Authority      AuthorityPin      `json:"authority"`
	Host           HostPin           `json:"host"`
	Runtime        RuntimePin        `json:"runtime"`
	Model          ModelPin          `json:"model"`
	ExternalSchema ExternalSchemaPin `json:"external_schema"`
}

// LauncherPin identifies the tested outer launcher executable.
type LauncherPin struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	ExecutableSHA256 string `json:"executable_sha256"`
}

// DuoPin identifies the Duo build under test.
type DuoPin struct {
	Version          string `json:"version"`
	Commit           string `json:"commit"`
	BuildDate        string `json:"build_date"`
	ExecutableSHA256 string `json:"executable_sha256"`
}

// SkillPin identifies the projected portable-launcher skill installation.
type SkillPin struct {
	Name           string `json:"name"`
	FormatVersion  string `json:"format_version"`
	ContentDigest  string `json:"content_digest"`
	InstallationID string `json:"installation_id"`
}

// ConfigPin identifies the effective Duo configuration.
type ConfigPin struct {
	Schema          string `json:"schema"`
	EffectiveDigest string `json:"effective_digest"`
}

// AuthorityPin records the authority-store state around a run.
type AuthorityPin struct {
	SchemaVersion     int    `json:"schema_version"`
	StoreDigestBefore string `json:"store_digest_before"`
	StoreDigestAfter  string `json:"store_digest_after"`
}

// HostPin identifies the host executable and protocol.
type HostPin struct {
	Name             string `json:"name"`
	Version          string `json:"version"`
	Protocol         string `json:"protocol"`
	SchemaDigest     string `json:"schema_digest"`
	ExecutableSHA256 string `json:"executable_sha256"`
}

// RuntimePin identifies the admitted runtime and delivery adapter evidence.
type RuntimePin struct {
	Name                string `json:"name"`
	Version             string `json:"version"`
	Format              string `json:"format"`
	AdapterBuild        string `json:"adapter_build"`
	ConformanceRecord   string `json:"conformance_record"`
	ExecutableSHA256    string `json:"executable_sha256"`
	DeliveryAssetSHA256 string `json:"delivery_asset_sha256"`
}

// ModelPin identifies the selected provider and model line.
type ModelPin struct {
	Provider  string `json:"provider"`
	ModelLine string `json:"model_line"`
}

// ExternalSchemaPin identifies the public Duo envelope schema.
type ExternalSchemaPin struct {
	Identity string `json:"identity"`
	Digest   string `json:"digest"`
}

// StageResult records one ordered scenario stage and its evidence.
type StageResult struct {
	Sequence        int         `json:"sequence"`
	Stage           string      `json:"stage"`
	Case            string      `json:"case"`
	Verdict         string      `json:"verdict"`
	Outcome         string      `json:"outcome"`
	StartedOffsetMS int64       `json:"started_offset_ms"`
	DurationMS      int64       `json:"duration_ms"`
	Assertions      []Assertion `json:"assertions"`
	Evidence        []string    `json:"evidence"`
	Error           *StageError `json:"error"`
}

// Assertion records an expected value and the independently derived actual.
type Assertion struct {
	ID       string         `json:"id"`
	Expected map[string]any `json:"expected"`
	Actual   map[string]any `json:"actual"`
	Matched  bool           `json:"matched"`
}

// StageError records a failed stage's stable error metadata.
type StageError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Effect  string `json:"effect"`
	Retry   Retry  `json:"retry"`
}

// Retry records whether and how a caller may retry after a stage error.
type Retry struct {
	Safe   bool   `json:"safe"`
	Action string `json:"action"`
}

// Summary records the whole-run verdict and first failure.
type Summary struct {
	Verdict          string  `json:"verdict"`
	FirstFailedStage *string `json:"first_failed_stage"`
	FirstFailedCase  *string `json:"first_failed_case"`
}

// ScrubRecord records the evidence-scrubbing policy outcome.
type ScrubRecord struct {
	Status   string   `json:"status"`
	Policy   string   `json:"policy"`
	Findings []string `json:"findings"`
}

// EvidenceIndex lists the content-addressed blobs supporting a result.
type EvidenceIndex struct {
	Schema string      `json:"schema"`
	Policy string      `json:"policy"`
	Blobs  []BlobEntry `json:"blobs"`
}

// BlobEntry identifies one scrubbed evidence blob and its producer.
type BlobEntry struct {
	Reference   string `json:"ref"`
	MediaType   string `json:"media_type"`
	Bytes       int64  `json:"bytes"`
	SHA256      string `json:"sha256"`
	ScrubStatus string `json:"scrub_status"`
	Producer    string `json:"producer"`
}

// Evidence contains independently captured observations.
type Evidence struct {
	Schema       string        `json:"schema"`
	ScrubStatus  string        `json:"scrub_status"`
	Observations []Observation `json:"observations"`
}

// Observation binds captured evidence to one stage assertion.
type Observation struct {
	Sequence    int            `json:"sequence"`
	AssertionID string         `json:"assertion_id"`
	Source      string         `json:"source"`
	Evidence    map[string]any `json:"evidence"`
}

// RawResult is the exact, untrusted result object collected from a launcher.
// It is intentionally not interpreted by a driver.
type RawResult = json.RawMessage
