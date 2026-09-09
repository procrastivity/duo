// Package amp is the Amp CLI agent-runtime adapter. Step-02
// (duo-amp-exclusive-writer) implements only runtime.RuntimeCorrelator
// plus its §5.1 factory: Correlate binds a claim carrying an Amp thread
// id, Probe looks the "amp" binary up on PATH, and MintLogPath /
// ThreadIDFromMintLog / MaterializeSettings / MaterializeMintScript build
// and read the thread-creation mint (`amp -x` creates a thread; see
// mint.go and materialize.go). Step-03 adds runtime.RuntimePromptProvider:
// PromptPath and DeliverPrompt over per-turn `amp threads continue`
// (prompt.go), plus the typed executor-lock collision this adapter reads
// from the per-thread debug log. ConversationProvider and
// ConditionProvider belong to the lag-aware sibling matter, not this one,
// and are not scaffolded here.
//
// AdapterID is "amp", matching launch-tuple kind. See
// docs/adapters/decisions.md's 2026-09-09 "Amp exclusive-writer scope is
// per-turn, not per-session" section and docs/cli/decisions.md's
// 2026-09-09 "Amp mint delivery needs a Duo-materialized wrapper script"
// section for the design calls this package implements; both cite
// notes/61 through notes/63 (the evidence archive, not part of this
// repository) as their source evidence.
package amp

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
)

// AdapterID names the Amp integration, matching launch-tuple kind.
const AdapterID = "amp"

// PinnedExternalVersion is the Amp CLI version the executor-lock and
// delivery facts (docs/adapters/decisions.md, 2026-09-09) were verified
// at. Amp's build train ships hourly and auto-updates by default (see
// Factory.Probe's doc comment), so this is a floor this package was
// proven against, not a claim that a live binary still matches it.
const PinnedExternalVersion = "0.0.1788048110-g570348"

// ThreadIDFormatIdentity names the identity channel Correlate binds: an
// Amp thread id (the stream-JSON "session_id" field; see
// ThreadIDFromMintLog). Amp has no separate transcript-document identity
// this adapter reads locally — Correlate always leaves TranscriptID
// empty.
const ThreadIDFormatIdentity = "amp-thread-id"

// ConfidenceInferred is the only label this adapter returns. A bound
// thread id is inferred, not authoritative: this adapter issues no
// reporter credential, so nothing raises a binding above inferred
// confidence.
const ConfidenceInferred = "inferred"

// Runtime is the Amp CLI agent-runtime adapter for one integration
// instance. Session identity is an Amp thread id; Amp reads are
// server-resident, so this adapter keeps no local transcript document
// and Correlate never fills TranscriptID.
type Runtime struct {
	integrationInstanceID string
	// ContinueCommand is the argv for per-turn delivery via `amp threads
	// continue`. Empty means {"amp"}. Unused until step-03; kept here now
	// as a test seam mirroring devin.Runtime's ResumeCommand.
	ContinueCommand []string
	// DebugLogRoot overrides Amp's per-thread debug log directory,
	// ~/.cache/amp/logs/threads (docs/adapters/decisions.md, 2026-09-09:
	// the executor-collision cause is only visible there, never in CLI
	// stderr). Empty means Amp's own default directory under the user's
	// home. Tests point this at a t.TempDir() forest of fake logs.
	DebugLogRoot string
	// SettingsFile is passed as `amp --settings-file <path>` on every
	// DeliverPrompt spawn when non-empty (MaterializeSettings is the
	// production writer of that file). Empty omits the flag entirely,
	// matching Amp's own default settings resolution.
	SettingsFile string
}

var (
	_ runtime.RuntimeCorrelator     = (*Runtime)(nil)
	_ runtime.RuntimePromptProvider = (*Runtime)(nil)
	_ adapter.Factory[*Runtime]     = Factory{}
)

// New returns an Amp runtime adapter for one integration instance.
func New(integrationInstanceID string) *Runtime {
	return &Runtime{integrationInstanceID: integrationInstanceID}
}

// Factory is the Amp runtime's §5.1 adapter factory.
type Factory struct {
	IntegrationInstanceID string
	// Binary is the executable Probe looks up; empty means "amp" on
	// PATH. Tests point it at a missing path or at os.Args[0].
	Binary string
}

// Descriptor implements adapter.Factory.
func (f Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID:                 AdapterID,
		Role:                      adapter.RoleRuntime,
		BuildVersion:              "stage1",
		SupportedExternalVersions: []string{PinnedExternalVersion},
		// Same "names the evidence until a conformance record exists"
		// pattern as devin's notes59-devin-3000.6.7. Must match
		// internal/cli's amp evidence digest, once that lands.
		ConformanceRecordDigest: "notes63-amp-0.0.1788048110-g570348",
		// Copied from Devin's: transcripts and mint logs carry raw prompt
		// and result text, and every Amp operation needs the account
		// credential.
		DiagnosticRedactionPolicy: "redact-credentials-and-transcript-content",
	}
}

// Probe implements adapter.Factory. It does not exec `amp --version`
// (same I-D7 pin-hazard rationale as Devin's Probe: Amp's build train
// ships hourly and auto-updates by default, so a version exec would name
// a version this build was never verified against). LookPath only: found
// is Unverified, missing is Unavailable. A probe does not publish live
// Duo operation support by itself (§5.1).
func (f Factory) Probe(context.Context) (adapter.Probe, error) {
	binary := f.Binary
	if binary == "" {
		binary = "amp"
	}
	probe := adapter.Probe{
		ProtocolOrFormatIdentity: ThreadIDFormatIdentity,
		ConnectionState:          "absent",
		Compatibility:            adapter.CompatibilityUnavailable,
	}
	if _, err := exec.LookPath(binary); err != nil {
		return probe, nil
	}
	probe.ConnectionState = "found"
	probe.Compatibility = adapter.CompatibilityUnverified
	return probe, nil
}

// New implements adapter.Factory.
func (f Factory) New(_ context.Context, probe adapter.Probe) (*Runtime, error) {
	if probe.Compatibility == adapter.CompatibilityUnavailable {
		return nil, fmt.Errorf("amp runtime %s: probe reported unavailable, refusing to build an adapter", f.IntegrationInstanceID)
	}
	return New(f.IntegrationInstanceID), nil
}
