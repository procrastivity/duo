package launch

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// recordPrefix is the launch-resolution record's Duo ID prefix. §6.4 graft
// 7: the record "has a Duo-authored ID and is the evidence object", while
// composition and preset keys appear only as declaration locators. Like
// every other Duo ID prefix it is diagnostic sugar — nothing parses an ID.
const recordPrefix = "lrr"

// The per-candidate outcomes a record carries. The ordinary result only
// ever shows "selected" (duo-external-v1's launch_resolution_leaf fixes its
// outcome enum to that one value); the rest exist so the record can explain
// what happened to everything that was not selected.
const (
	OutcomeSelected   = "selected"
	OutcomeEligible   = "eligible"
	OutcomeEliminated = "eliminated"
)

// The stable elimination reason codes (§6.4 graft 3). They are a closed
// vocabulary: a caller can branch on them, and a new reason is a contract
// change rather than a new sentence.
//
// I-2 extension (duo-config-v3 step 10, by reference to step 02's
// duo-external-v1 amendment, 2026-08-24 handoff 22): ReasonProviderDisabled
// is the vocabulary's first extension since it closed. It is an addition,
// not a substitution — the four v2 reasons are unchanged — and step 10
// wires the name only; nothing emits it until step 12's M2 provider-fact
// check runs. avoid_matched stays the only reason the relent re-run may
// undo.
const (
	// ReasonSessionHostDisabled: the tuple's session host is declared
	// disabled. Installed policy, not a live reachability claim.
	ReasonSessionHostDisabled = "session_host_disabled"
	// ReasonNoConformanceEvidence: no accepted immutable conformance
	// record establishes that this tuple is supported.
	ReasonNoConformanceEvidence = "no_conformance_evidence"
	// ReasonRequireUnmatched: a non-relenting require named a different
	// value on one of this candidate's axes.
	ReasonRequireUnmatched = "require_unmatched"
	// ReasonAvoidMatched: a soft avoid matched this candidate. It is the
	// one reason that can be undone, by the relent re-run.
	ReasonAvoidMatched = "avoid_matched"
	// ReasonProviderDisabled: the tuple's provider is a standing-disabled
	// fact (M2's snapshot). Installed policy, not a live reachability
	// claim, the same shape as ReasonSessionHostDisabled. It is never
	// undone by the relent re-run, which touches avoid_matched only.
	ReasonProviderDisabled = "provider_disabled"
)

// Record is the launch-resolution record: §6.9's immutable, Duo-authored
// evidence of the complete choice made before any spawn. It is the third
// kind of state, and it is neither selected configuration (a runtime's
// observed current choice) nor effective configuration (immutable
// observation of what served a turn); neither observation can revise it,
// and it can stand in for neither.
//
// The record is where everything the ordinary result does not carry lives:
// every candidate and its elimination reason, relation rejections, the
// restored pool from an avoid relent, the consulted digests, and the random
// draw (§7.3).
type Record struct {
	// ID is the Duo-authored record ID ("lrr_...").
	ID string `json:"id"`
	// RequestID and Caller attribute the request; ResolvedAt stamps it.
	RequestID  string `json:"request_id,omitempty"`
	Caller     string `json:"caller,omitempty"`
	ResolvedAt string `json:"resolved_at"`

	// RequestedPreset is the request argument as given; PresetLocator is
	// the declaration it resolved to.
	RequestedPreset string `json:"requested_preset"`
	PresetLocator   string `json:"preset_locator"`

	// ConfigurationDigest is the digest of the launch-relevant declaration
	// subset this resolution consulted, and ConsultedRecordDigests are the
	// conformance records Support cited. Together with the normalized
	// constraints and the draw they are §7.1's complete consulted-input
	// list, which is what makes the resolution replayable.
	ConfigurationDigest    string   `json:"configuration_digest"`
	ConsultedRecordDigests []string `json:"consulted_record_digests"`

	Selection   string      `json:"selection"`
	Constraints Constraints `json:"constraints"`

	// Host is the single session host M1 deduced for this launch, with the
	// rung that produced it and every piece of evidence a higher rung beat
	// (duo-config-v3 step 13). Under v2 the host was a config declaration
	// name on the composition chain and needed no separate record entry;
	// under v3 it is *state*, deduced once per launch, so a record that did
	// not carry it could not explain — or replay — where the session went.
	//
	// EvidenceBundle is what that deduction and step 3 rested on, by
	// reference: the correlation fact, the ambient variables read once at
	// materialization, and the standing provider facts M2 snapshotted.
	// Together with ConfigurationDigest and ConsultedRecordDigests it
	// completes §7.1's consulted-input list for v3: configuration, evidence,
	// and now state.
	//
	// The bundle here is the *full* one, ambient captures included, unlike
	// the reference set a failure's safe details carry. The record is the
	// evidence object, and an ambient capture is evidence even though it is
	// not a durable fact with an ID.
	Host           RecordHost              `json:"host"`
	EvidenceBundle *materialize.WireBundle `json:"evidence_bundle,omitempty"`

	// Target and TargetSource are the informational placement pair
	// (notes/51 record 2). The resolver never sets them (I-3); the
	// launcher stamps them after Resolve and before Commit.
	Target       string `json:"target,omitempty"`
	TargetSource string `json:"target_source,omitempty"`

	Leaves []RecordLeaf `json:"leaves"`
	// Relations are the cross-leaf relations the preset declared, and
	// RelationRejections are the complete assignments they rejected. The
	// declaration is recorded beside the rejections because a rejection
	// list on its own cannot say whether a relation was enforced and
	// rejected nothing, or was never declared.
	Relations          []WireRelation      `json:"relations"`
	RelationRejections []RelationRejection `json:"relation_rejections"`

	// AvoidRelented records that the strict post-avoid pools yielded no
	// complete assignment and §6.7 step 8's re-run over the post-require,
	// pre-avoid pools was performed. RestoredCandidates lists every
	// candidate that re-entered a pool because of it.
	AvoidRelented      bool     `json:"avoid_relented"`
	RestoredCandidates []string `json:"restored_candidates"`

	// EligibleAssignments is how many complete assignments survived, and
	// Draw is the random evidence when Selection is random.
	EligibleAssignments int   `json:"eligible_assignments"`
	Draw                *Draw `json:"draw,omitempty"`

	// Assignment is the selected complete plan, one entry per leaf in the
	// resolver's leaf order.
	Assignment []Assignment `json:"assignment"`

	// SessionID and InstanceIDs are the eventual links §6.9 requires. They
	// are empty in the value the resolver produces and filled by the
	// commit that creates the session, which is the same transaction the
	// record commits in.
	SessionID   string   `json:"session_id,omitempty"`
	InstanceIDs []string `json:"instance_ids,omitempty"`
}

// RecordLeaf is one leaf's complete history through the pipeline.
type RecordLeaf struct {
	Name    string `json:"name"`
	Locator string `json:"locator"`
	// DeclaredKind is "determined" for a one-candidate leaf and "open"
	// otherwise. It describes the declaration, never the outcome: a leaf
	// whose require narrowed it to one surviving candidate is still open
	// (§6.5 rule 7).
	DeclaredKind string            `json:"declared_kind"`
	Candidates   []RecordCandidate `json:"candidates"`
	Survivors    Survivors         `json:"survivors"`
}

// RecordCandidate is one declared candidate's full row.
type RecordCandidate struct {
	Tuple Tuple `json:"tuple"`
	// Order is the candidate's index in the declared array, which is its
	// preference rank.
	Order             int    `json:"order"`
	Outcome           string `json:"outcome"`
	EliminationReason string `json:"elimination_reason,omitempty"`
	Restored          bool   `json:"restored,omitempty"`
	// SupportRecordDigest is the conformance record consulted for this
	// tuple, whatever the verdict was.
	SupportRecordDigest string `json:"support_record_digest,omitempty"`
	// ProviderFactID is the standing provider fact that eliminated this
	// candidate, present only on a provider_disabled row. It is the fact
	// M2 snapshotted, not a re-read: a later `duo provider enable` does
	// not change what this record says was consulted.
	ProviderFactID string `json:"provider_fact_id,omitempty"`
}

// RecordHost is the deduced host as the record carries it: the wire shape
// duo-external-v1's `launch_deduced_host` fixes, with the outranked
// evidence beside it.
//
// It is a type alias in all but name for the failure payload's WireHost,
// and deliberately the same object: the host a failure blames and the host
// a record commits are the same deduction, and two spellings of it would be
// two things to keep in step.
type RecordHost = WireHost

// MintedComposition is the variant x deduced-host join, named.
// duo-external-v1's `minted_composition` (2026-08-24 handoff 22).
//
// Under `duo.config/v2` a composition was authored and had a declaration
// locator; under v3 nothing authors one, and this object is the only place
// a composition is named. That is why it carries the labels rather than a
// locator: there is no declaration to point at, so the name has to stand on
// its own parts.
type MintedComposition struct {
	// Name is the minted name, `<variant>@<host_kind>`. It is not a
	// locator and never appears in configuration.
	Name string `json:"name,omitempty"`
	// Variant is the bare declared launch-variant name — the one required
	// field, because the variant is the half of the join that was authored.
	Variant        string `json:"variant"`
	AgentRuntime   string `json:"agent_runtime,omitempty"`
	ModelLine      string `json:"model_line,omitempty"`
	ModelFamily    string `json:"model_family,omitempty"`
	HostKind       string `json:"host_kind,omitempty"`
	HostInstanceID string `json:"host_instance_id,omitempty"`
}

// MintedComposition projects the composition minted for one tuple. Every
// field is already on the tuple: this is a projection onto the contract's
// shape, never a second source of truth.
func (t Tuple) MintedComposition() MintedComposition {
	return MintedComposition{
		Name:           t.Composition,
		Variant:        t.Variant,
		AgentRuntime:   t.AgentRuntime,
		ModelLine:      t.ModelLine,
		ModelFamily:    t.ModelFamily,
		HostKind:       t.HostKind,
		HostInstanceID: t.IntegrationInstanceID,
	}
}

// RelationRejection is one complete assignment a cross-leaf relation
// rejected, named by the assignment's per-leaf declaration locators.
type RelationRejection struct {
	Kind       string   `json:"kind"`
	Leaves     []string `json:"leaves"`
	Assignment []string `json:"assignment"`
	Reason     string   `json:"reason"`
}

// Assignment is one leaf's place in the selected complete plan.
type Assignment struct {
	Leaf  string `json:"leaf"`
	Tuple Tuple  `json:"tuple"`
	// Composition is the composition this leaf's join minted. It is
	// derivable from Tuple and stated anyway: the minted composition is the
	// thing v3 says a launch *is*, and a record that made a reader re-mint
	// it would be asking them to re-run the join to read the decision.
	Composition MintedComposition `json:"composition"`
	// RelentedAvoids are the avoid predicates this leaf's chosen candidate
	// actually matches. §6.6 reports "the matched avoids on the selected
	// assignment" and nothing more: a relent that restored candidates this
	// assignment did not use is record detail, not result detail.
	RelentedAvoids []Predicate `json:"relented_avoids"`
}

// Resolution is one successful launch resolution: the complete record, plus
// the projections a caller needs.
type Resolution struct {
	// Record is the immutable evidence object. A caller commits it before
	// spawning anything; see Launcher.
	Record Record
}

// Report is §6.9's ordinary successful result — the safe projection that
// duo-external-v1's launch_resolution_report fixes.
//
// It is a bounded, deliberate departure from the strictest thin-projection
// precedent (§7.3): per leaf it names the chosen public agent runtime and
// declared model line, the declared kind, the outcome, and only the avoid
// predicates that assignment actually relented on. It exposes no
// composition locator as identity, no losing candidate, no rejection table,
// no conformance internals, and no random evidence — all of those are
// reachable through LaunchResolutionID.
//
// A multi-leaf result stays per-leaf and invents no session-wide model
// line: a caller chaining on a model line chooses the leaf it is chaining.
type Report struct {
	SessionID          string `json:"session_id,omitempty"`
	LaunchResolutionID string `json:"launch_resolution_id,omitempty"`
	Selection          string `json:"selection"`
	// Preview marks a dry run's result. §6.10: a dry run uses the same
	// resolver, creates no session and no durable record, and in random
	// mode does not promise reuse of its draw.
	Preview bool         `json:"preview,omitempty"`
	Leaves  []ReportLeaf `json:"leaves"`
	// Host is the deduced session host this launch resolved against
	// (duo-config-v3 step 13). It is the one *growth* of the thin
	// projection beyond the per-leaf list, and it is not decoration: under
	// v3 the host is late-bound state, so a caller reading a result cannot
	// otherwise tell which host their session went to, or that a stale
	// workspace correlation chose it. Step 03's re-authored
	// contracts/fixtures/duo-external-v1/session-launch.json carries it in
	// `result`, and `launch_resolution_report` has the property.
	//
	// It is a pointer so a hand-built Report without one omits the key
	// rather than asserting an empty deduction.
	Host *WireHost `json:"host,omitempty"`
	// Target and TargetSource are the informational placement pair
	// (tab|pane and explicit-flag|config-default|built-in). Placement is
	// a launcher input; the resolver never consults it (I-3, notes/51
	// record 2).
	Target       string `json:"target,omitempty"`
	TargetSource string `json:"target_source,omitempty"`
	// Relations are the preset's declared cross-leaf relations, present
	// only when it declares any. They are a property of the declaration
	// the caller asked for, not of any leaf, and
	// contracts/fixtures/duo-external-v1/session-launch-distinct-model-family.json
	// carries them in `result`.
	Relations []WireRelation `json:"relations,omitempty"`
}

// ReportLeaf is one leaf of the ordinary result.
//
// Its field set is exactly `launch_resolution_leaf`, which is
// `additionalProperties: false`: name, the three public axis labels, the
// minted composition, the declared kind, the outcome, and the relented
// avoids. ModelFamily and Composition are step 02's adds — the family
// because v3 makes it a required label and a constraint axis, and the
// composition because v3 mints one and a caller has no other way to learn
// its name.
type ReportLeaf struct {
	Name           string            `json:"name"`
	AgentRuntime   string            `json:"agent_runtime"`
	ModelLine      string            `json:"model_line"`
	ModelFamily    string            `json:"model_family"`
	Composition    MintedComposition `json:"composition"`
	DeclaredKind   string            `json:"declared_kind"`
	Outcome        string            `json:"outcome"`
	RelentedAvoids []Predicate       `json:"relented_avoids"`
}

// Report projects the ordinary result out of the record.
func (r *Resolution) Report() Report {
	kinds := make(map[string]string, len(r.Record.Leaves))
	for _, leaf := range r.Record.Leaves {
		kinds[leaf.Name] = leaf.DeclaredKind
	}
	host := r.Record.Host
	out := Report{
		SessionID:          r.Record.SessionID,
		LaunchResolutionID: r.Record.ID,
		Selection:          r.Record.Selection,
		Leaves:             make([]ReportLeaf, 0, len(r.Record.Assignment)),
		Host:               &host,
		Target:             r.Record.Target,
		TargetSource:       r.Record.TargetSource,
		Relations:          r.Record.Relations,
	}
	if host.Kind == "" {
		out.Host = nil
	}
	for _, a := range r.Record.Assignment {
		relented := a.RelentedAvoids
		if relented == nil {
			relented = []Predicate{}
		}
		out.Leaves = append(out.Leaves, ReportLeaf{
			Name:           a.Leaf,
			AgentRuntime:   a.Tuple.AgentRuntime,
			ModelLine:      a.Tuple.ModelLine,
			ModelFamily:    a.Tuple.ModelFamily,
			Composition:    a.Composition,
			DeclaredKind:   kinds[a.Leaf],
			Outcome:        OutcomeSelected,
			RelentedAvoids: relented,
		})
	}
	return out
}

// Assignments returns the selected plan, one entry per leaf.
func (r *Resolution) Assignments() []Assignment { return r.Record.Assignment }

// mintRecordID returns a fresh launch-resolution record ID. 128 random bits,
// matching internal/domain's ID discipline: IDs are never reused, and the
// space is large enough that "never reused" holds without a counter a
// restored or copied store could rewind.
func mintRecordID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("launch: minting %s id: %w", recordPrefix, err)
	}
	return recordPrefix + "_" + hex.EncodeToString(b[:]), nil
}

// configurationDigest hashes the launch-relevant subset of a resolved
// configuration document: the declaration families resolution reads.
//
// §7.1 calls this "the resolved launch-configuration digest — the merged
// declaration and policy, not observed effective runtime configuration".
// Digesting only what resolution consults is deliberate: an edit to an
// unrelated section (authority socket, presentation clients) must not
// invalidate the replay of a launch it could not have changed.
//
// Under v3 there are four families, not five: `compositions` is gone
// (nothing authors one), and `session_hosts` is host-*kind policy* rather
// than a set of instance declarations. The deduced host itself is not in
// the digest and must not be — it is state, and it is replayed from the
// evidence bundle the record cites separately. Two launches of the same
// document against two different sockets share a configuration digest,
// which is the honest answer: the configuration did not change.
func configurationDigest(doc config.DocumentV3) (string, error) {
	// encoding/json sorts map keys, so the same document always produces
	// the same bytes regardless of decode order.
	subset := struct {
		Presets        map[string]config.PresetV3      `json:"presets"`
		LaunchVariants map[string]config.LaunchVariant `json:"launch_variants"`
		AgentRuntimes  map[string]config.AgentRuntime  `json:"agent_runtimes"`
		SessionHosts   config.SessionHostPolicy        `json:"session_hosts"`
	}{
		Presets:        doc.Presets,
		LaunchVariants: doc.LaunchVariants,
		AgentRuntimes:  doc.AgentRuntimes,
		SessionHosts:   doc.SessionHosts,
	}
	data, err := json.Marshal(subset)
	if err != nil {
		return "", fmt.Errorf("launch: digesting launch configuration: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
