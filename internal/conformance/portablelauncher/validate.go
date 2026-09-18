package portablelauncher

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	digestPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	blobPattern      = regexp.MustCompile(`^blob:sha256:[0-9a-f]{64}$`)
	operationPattern = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
)

// Problems accumulates deterministic validation failures.
type Problems struct{ Items []string }

// Add records one validation failure.
func (p *Problems) Add(problem string) { p.Items = append(p.Items, problem) }

// Err sorts the recorded failures and returns nil when none exist.
func (p *Problems) Err() error {
	if len(p.Items) == 0 {
		return nil
	}
	sort.Strings(p.Items)
	return p
}
func (p *Problems) Error() string { return strings.Join(p.Items, "\n") }

// BlobReader resolves one content-addressed evidence reference.
type BlobReader func(reference string) ([]byte, error)

// ValidationInput contains a result, evidence index, and blob resolver.
type ValidationInput struct {
	ResultJSON []byte
	IndexJSON  []byte
	ReadBlob   BlobReader
}

type observationKey struct {
	Sequence    int
	AssertionID string
}

// Validate performs complete offline validation. The canonical scenario and
// oracle are compiled into this package; launcher output never supplies an
// expectation.
func Validate(input ValidationInput) (*Result, error) {
	var problems Problems
	if findings := Scrub(input.ResultJSON); len(findings) != 0 {
		problems.Add("result scrub failed: " + strings.Join(findings, "; "))
	}
	if findings := Scrub(input.IndexJSON); len(findings) != 0 {
		problems.Add("index scrub failed: " + strings.Join(findings, "; "))
	}

	var result Result
	if err := decodeStrict(input.ResultJSON, &result); err != nil {
		return nil, fmt.Errorf("decode result: %w", err)
	}
	var index EvidenceIndex
	if err := decodeStrict(input.IndexJSON, &index); err != nil {
		return nil, fmt.Errorf("decode evidence index: %w", err)
	}
	scenario := CanonicalScenario()
	scenarioJSON, err := ScenarioJSON(scenario)
	if err != nil {
		return nil, err
	}

	validateHeader(result, scenarioJSON, &problems)
	validatePins(result.Pins, &problems)
	validateStages(result, scenario, &problems)
	observations := validateEvidence(result, index, input.ReadBlob, &problems)
	validateAssertions(result, scenario, observations, &problems)
	validateSummary(result, &problems)
	validateSpecialOutcomes(result, &problems)
	if err := problems.Err(); err != nil {
		return &result, err
	}
	return &result, nil
}

func decodeStrict(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}

func validateHeader(r Result, scenarioJSON []byte, p *Problems) {
	if r.Schema != ResultSchema {
		p.Add("schema: wrong result identity")
	}
	if r.Suite.Name != ScenarioName || r.Suite.Revision != ScenarioRevision {
		p.Add("suite: wrong name or revision")
	}
	if r.Suite.ManifestDigest != Digest(scenarioJSON) {
		p.Add("suite.manifest_digest: does not match canonical scenario bytes")
	}
	if r.Suite.OracleDigest != OracleDigest() {
		p.Add("suite.oracle_digest: does not match canonical oracle")
	}
	if r.Run.RunID == "" {
		p.Add("run.run_id: missing")
	}
	if parsed, err := time.Parse(time.RFC3339, r.Run.ObservedAt); err != nil || parsed.Location() != time.UTC {
		p.Add("run.observed_at: must be RFC3339 UTC")
	}
	if r.Run.HostOS != "linux" || r.Run.HostArch != "x86_64" || r.Run.FixtureRoot != "$RUN" {
		p.Add("run: unsupported host or unrewritten fixture root")
	}
	if r.Run.PluginsEnabled || r.Run.MCPEnabled || r.Run.TerminalInputUsed {
		p.Add("run: plugins, MCP, and terminal input must all be disabled")
	}
	if r.Scrub.Status != "pass" || r.Scrub.Policy != ScrubPolicy || len(r.Scrub.Findings) != 0 {
		p.Add("scrub: capture did not pass the required policy")
	}
}

func validatePins(v Pins, p *Problems) {
	if !validLauncherPin(v.Launcher) {
		p.Add("pins.launcher: recognized name and exact per-run version and executable digest required")
	}
	if !validDuoPin(v.Duo) {
		p.Add("pins.duo: incomplete or mismatched build identity")
	}
	if v.Skill.Name != SkillName || v.Skill.FormatVersion != SkillFormat || v.Skill.ContentDigest != SkillContentDigest || v.Skill.InstallationID == "" {
		p.Add("pins.skill: incomplete or mismatched projected skill identity")
	}
	if v.Config.Schema != "duo.config/v3" || !digestPattern.MatchString(v.Config.EffectiveDigest) {
		p.Add("pins.config: incomplete or mismatched config identity")
	}
	if v.Authority.SchemaVersion != 1 || !digestPattern.MatchString(v.Authority.StoreDigestBefore) || !digestPattern.MatchString(v.Authority.StoreDigestAfter) {
		p.Add("pins.authority: incomplete authority identity")
	}
	if v.Host.Name != "herdr" || v.Host.Version != "0.8.2" || v.Host.Protocol != "herdr-socket-api/20" || v.Host.SchemaDigest != HostSchemaDigest || !digestPattern.MatchString(v.Host.ExecutableSHA256) {
		p.Add("pins.host: exact Herdr 0.8.2/protocol 20/schema pin required")
	}
	if v.Runtime.Name != "pi" || v.Runtime.Version != "0.83.0" || v.Runtime.Format != "pi-session-jsonl/v3" || v.Runtime.AdapterBuild != "stage1" || v.Runtime.ConformanceRecord != "pi-0.83.0-2026-08-23" || v.Runtime.DeliveryAssetSHA256 != DeliveryAssetDigest || !digestPattern.MatchString(v.Runtime.ExecutableSHA256) {
		p.Add("pins.runtime: exact Pi 0.83.0 identity required")
	}
	if v.Model.Provider != "openai-codex" || v.Model.ModelLine != "gpt-5.6-luna" {
		p.Add("pins.model: selected provider/model does not match the fixture")
	}
	if v.ExternalSchema.Identity != "duo.external/v1" || v.ExternalSchema.Digest != ExternalSchemaDigest {
		p.Add("pins.external_schema: exact public wire identity required")
	}
}

func validateStages(r Result, scenario Scenario, p *Problems) {
	if len(r.Stages) != len(scenario.Steps) {
		p.Add(fmt.Sprintf("stages: got %d records, want %d", len(r.Stages), len(scenario.Steps)))
	}
	limit := len(r.Stages)
	if len(scenario.Steps) < limit {
		limit = len(scenario.Steps)
	}
	var priorEnd int64
	completeDeadlineFailed := false
	for i := 0; i < limit; i++ {
		got, want := r.Stages[i], scenario.Steps[i]
		if got.Sequence != i+1 || got.Sequence != want.Sequence {
			p.Add(fmt.Sprintf("stages[%d].sequence: not exact and monotonic", i))
		}
		if got.Stage != want.Stage || got.Case != want.Case {
			p.Add(fmt.Sprintf("stages[%d]: got %s/%s, want %s/%s", i, got.Case, got.Stage, want.Case, want.Stage))
		}
		if got.StartedOffsetMS < priorEnd || got.DurationMS < 0 {
			p.Add(fmt.Sprintf("stages[%d]: timing is not monotonic", i))
		}
		if i == 0 && got.StartedOffsetMS != 0 {
			p.Add("stages[0]: setup must start at monotonic offset zero")
		}
		stageMaximum := want.DeadlineMS
		if got.Case == "timeout" && got.Stage == "observe" {
			stageMaximum = CanonicalOracle().TimeoutMaximumMS
		}
		deadlineFailure := got.Verdict == "fail" && got.Error != nil && (got.Error.Code == "stage.deadline_exceeded" || got.Error.Code == "run.deadline_exceeded")
		if got.DurationMS > stageMaximum && !deadlineFailure {
			p.Add(fmt.Sprintf("stages[%d]: stage deadline exceeded", i))
		}
		if got.Verdict == "fail" && got.Error != nil && got.Error.Code == "run.deadline_exceeded" {
			completeDeadlineFailed = true
		}
		if got.StartedOffsetMS+got.DurationMS > scenario.DeadlinesMS["complete_run"] && !completeDeadlineFailed {
			p.Add(fmt.Sprintf("stages[%d]: complete run deadline exceeded", i))
		}
		priorEnd = got.StartedOffsetMS + got.DurationMS
		if got.Verdict != "pass" && got.Verdict != "fail" {
			p.Add(fmt.Sprintf("stages[%d].verdict: unknown value", i))
		}
		if !oneOf(got.Outcome, "success", "blocked", "exited", "timeout", "error") {
			p.Add(fmt.Sprintf("stages[%d].outcome: unknown value", i))
		}
		if got.Verdict == "pass" {
			if got.Error != nil {
				p.Add(fmt.Sprintf("stages[%d]: passing stage has an error", i))
			}
			if got.Outcome != want.Outcome {
				p.Add(fmt.Sprintf("stages[%d]: passing outcome %q, want %q", i, got.Outcome, want.Outcome))
			}
		} else {
			if got.Outcome != "error" || got.Error == nil || got.Error.Code == "" || got.Error.Message == "" || got.Error.Effect == "" || got.Error.Retry.Action == "" {
				p.Add(fmt.Sprintf("stages[%d]: failed stage lacks complete error metadata", i))
			}
		}
		if len(got.Evidence) == 0 {
			p.Add(fmt.Sprintf("stages[%d]: no evidence references", i))
		}
		seenEvidence := map[string]bool{}
		for _, ref := range got.Evidence {
			if !blobPattern.MatchString(ref) {
				p.Add(fmt.Sprintf("stages[%d]: malformed evidence reference %q", i, ref))
			}
			if seenEvidence[ref] {
				p.Add(fmt.Sprintf("stages[%d]: duplicate evidence reference", i))
			}
			seenEvidence[ref] = true
		}
	}
	if len(r.Stages) > 0 {
		last := r.Stages[len(r.Stages)-1]
		if last.Stage != "cleanup" || last.Case != "run" {
			p.Add("stages: cleanup must be last exactly once")
		}
		cleanupCount := 0
		for _, stage := range r.Stages {
			if stage.Stage == "cleanup" {
				cleanupCount++
			}
		}
		if cleanupCount != 1 {
			p.Add("stages: cleanup must appear exactly once")
		}
		if last.Verdict != "pass" {
			p.Add("cleanup: failed or omitted cleanup invalidates the capture")
		}
	}
}

func validateEvidence(r Result, index EvidenceIndex, read BlobReader, p *Problems) map[observationKey]capturedObservation {
	observations := map[observationKey]capturedObservation{}
	if index.Schema != IndexSchema || index.Policy != ScrubPolicy {
		p.Add("evidence index: wrong schema or scrub policy")
	}
	referenced := map[string]bool{}
	for _, stage := range r.Stages {
		for _, ref := range stage.Evidence {
			referenced[ref] = true
		}
	}
	indexed := map[string]bool{}
	for i, entry := range index.Blobs {
		path := fmt.Sprintf("evidence_index.blobs[%d]", i)
		if indexed[entry.Reference] {
			p.Add(path + ": duplicate blob")
		}
		indexed[entry.Reference] = true
		if !referenced[entry.Reference] {
			p.Add(path + ": unreferenced blob")
		}
		if entry.MediaType != "application/json" || entry.ScrubStatus != "pass" || !oneOf(entry.Producer, "common_collector", "controller", "duo", "launcher") {
			p.Add(path + ": incomplete media/scrub/producer metadata")
		}
		if read == nil {
			p.Add(path + ": no blob reader")
			continue
		}
		data, err := read(entry.Reference)
		if err != nil {
			p.Add(path + ": missing blob: " + err.Error())
			continue
		}
		if int64(len(data)) != entry.Bytes || Digest(data) != entry.SHA256 || "blob:"+Digest(data) != entry.Reference {
			p.Add(path + ": byte count or digest mismatch")
		}
		if findings := Scrub(data); len(findings) != 0 {
			p.Add(path + ": scrub failed: " + strings.Join(findings, "; "))
		}
		var evidence Evidence
		if err := decodeStrict(data, &evidence); err != nil {
			p.Add(path + ": malformed evidence: " + err.Error())
			continue
		}
		if evidence.Schema != EvidenceSchema || evidence.ScrubStatus != "pass" {
			p.Add(path + ": wrong evidence schema or scrub status")
		}
		for _, obs := range evidence.Observations {
			key := observationKey{Sequence: obs.Sequence, AssertionID: obs.AssertionID}
			if _, exists := observations[key]; exists {
				p.Add(fmt.Sprintf("evidence: duplicate observation for sequence %d assertion %s", obs.Sequence, obs.AssertionID))
			}
			observations[key] = capturedObservation{Observation: obs, Reference: entry.Reference}
		}
	}
	for ref := range referenced {
		if !indexed[ref] {
			p.Add("evidence: referenced blob missing from index: " + ref)
		}
	}
	asserted := map[observationKey]bool{}
	for _, stage := range r.Stages {
		for _, assertion := range stage.Assertions {
			asserted[observationKey{Sequence: stage.Sequence, AssertionID: assertion.ID}] = true
		}
	}
	for key := range observations {
		if !asserted[key] {
			p.Add(fmt.Sprintf("evidence: unclaimed observation for sequence %d assertion %s", key.Sequence, key.AssertionID))
		}
	}
	return observations
}

func validateSummary(r Result, p *Problems) {
	var first *StageResult
	allPass := true
	for i := range r.Stages {
		if r.Stages[i].Verdict == "fail" {
			allPass = false
			if first == nil {
				first = &r.Stages[i]
			}
		}
	}
	if allPass {
		if r.Summary.Verdict != "pass" || r.Summary.FirstFailedStage != nil || r.Summary.FirstFailedCase != nil {
			p.Add("summary: inconsistent all-pass summary")
		}
		p.Add("summary: overall pass forbidden while supported blocked evidence is unavailable")
		return
	}
	if r.Summary.Verdict != "fail" || first == nil || r.Summary.FirstFailedStage == nil || r.Summary.FirstFailedCase == nil || *r.Summary.FirstFailedStage != first.Stage || *r.Summary.FirstFailedCase != first.Case {
		p.Add("summary: inconsistent first failure")
	}
}

func validateSpecialOutcomes(r Result, p *Problems) {
	for i, stage := range r.Stages {
		if stage.Case == "blocked" && stage.Verdict == "pass" {
			p.Add(fmt.Sprintf("stages[%d]: supported_admitted_then_blocked_producer is unavailable", i))
		}
		if stage.Case == "blocked" && stage.Stage == "launch" && stage.Verdict == "fail" {
			if stage.Error == nil || stage.Error.Code != "prerequisite.blocked_induction_unavailable" || stage.Error.Effect != "no_effect" || stage.Error.Retry.Safe || stage.Error.Retry.Action != "add_supported_blocked_evidence" {
				p.Add(fmt.Sprintf("stages[%d]: blocked prerequisite failure metadata is not exact", i))
			}
		}
		if stage.Case == "timeout" && stage.Stage == "observe" && stage.Verdict == "pass" {
			actual := assertionActual(stage, "observe.timeout")
			e := TimeoutEvidence{
				ElapsedMS: int64Number(actual["elapsed_ms"]), ProcessSucceeded: boolValue(actual["process_success"]),
				StoppedVerified: boolValue(actual["stopped_verified"]), Delivered: boolValue(actual["delivered"]),
				Attempts: int(int64Number(actual["attempts"])), AssistantBlocks: int(int64Number(actual["assistant_blocks"])),
				OuterTimedOut: boolValue(actual["outer_timed_out"]),
			}
			if err := ValidateIntentionalTimeout(e); err != nil {
				p.Add(fmt.Sprintf("stages[%d]: %v", i, err))
			}
			if stage.DurationMS < 20_000 || stage.DurationMS > 22_500 {
				p.Add(fmt.Sprintf("stages[%d]: timeout duration outside 20000..22500", i))
			}
		}
	}
}

func assertionActual(stage StageResult, id string) map[string]any {
	for _, a := range stage.Assertions {
		if a.ID == id {
			return a.Actual
		}
	}
	return nil
}

func int64Number(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}

func boolValue(v any) bool { b, _ := v.(bool); return b }

func stringValue(v any) string { s, _ := v.(string); return s }

func isNumber(v any) bool {
	switch v.(type) {
	case float64, int64, int:
		return true
	default:
		return false
	}
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
