// Package devin is the candidate Devin CLI agent-runtime adapter.
// Stage A shipped RuntimeCorrelator. Stage B adds RuntimePromptProvider
// over ACP stdio. Stage C (duo-devin-loop/observe) adds
// ConversationProvider and ConditionProvider from an ATIF-v1.7
// document path. RuntimeReadyProvider stays out. See
// terminal-multiplexers/notes/59-devin-full-sweep.md and
// notes/60-devin-launch-first-pass.md.
//
// AdapterID is "devin", matching launch-tuple kind and
// agentRuntimeIntegrationID's identity map. notes/59's draft
// conformance row said "devin-cli"; this package does not introduce a
// second ID.
package devin

import (
	"context"
	"fmt"
	"io"
	"os/exec"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
)

// PinnedExternalVersion is the Devin CLI version the Tier C sweep ended
// on (notes/59). Auto-update moved 3000.6.2 → 3000.6.7 mid-probe; both
// remain in SupportedExternalVersions. Probe never execs --version, so
// it never claims Supported against a live binary.
const PinnedExternalVersion = "3000.6.7"

// SessionIDFormatIdentity names the identity channel Correlate binds:
// a Herdr-reported agent-session id (kind=id, hyphenated name). ATIF is
// a separate TranscriptID path; Correlate still leaves it empty.
const SessionIDFormatIdentity = "devin-session-id"

// ConfidenceInferred is the only label this adapter returns. Host-named
// ids are inferred: Herdr named them, this adapter has no reporter
// credential to raise them to authoritative.
const ConfidenceInferred = "inferred"

// Runtime is the Devin CLI agent-runtime adapter for one integration
// instance. Session identity lives on the host record. TranscriptID is
// an ATIF document path when a caller has one; Correlate does not fill it.
type Runtime struct {
	integrationInstanceID string
	// ACPCommand is the argv for the ACP stdio server. Empty means
	// {"devin", "acp"}. Tests point it at a missing path to prove
	// spawn-fail is no_effect.
	ACPCommand []string
	// ACPDial, when set, replaces spawning ACPCommand. Tests inject a
	// fake stdio server. Production leaves it nil.
	ACPDial func(context.Context) (io.ReadWriteCloser, error)
	// ResumeCommand is the argv for spawn-per-prompt delivery. Empty
	// means {"devin"}. Tests point it at a fake executable.
	ResumeCommand []string
	// ListSessions, when set, replaces `devin list --format json` for
	// best-effort lock title enrichment. The cwd is the bound workspace.
	ListSessions func(context.Context, string) ([]byte, error)
	// SessionsDBPath overrides Devin's sessions.db location. Production uses
	// the XDG data directory; tests point this at a temporary read-only forest.
	SessionsDBPath string
}

var (
	_ runtime.RuntimeCorrelator     = (*Runtime)(nil)
	_ runtime.RuntimePromptProvider = (*Runtime)(nil)
	_ runtime.ConversationProvider  = (*Runtime)(nil)
	_ runtime.ConditionProvider     = (*Runtime)(nil)
	_ adapter.Factory[*Runtime]     = Factory{}
)

// New returns a Devin runtime adapter for one integration instance.
func New(integrationInstanceID string) *Runtime {
	return &Runtime{integrationInstanceID: integrationInstanceID}
}

// Factory is the Devin runtime's §5.1 adapter factory.
type Factory struct {
	IntegrationInstanceID string
	// Binary is the executable Probe looks up; empty means "devin" on
	// PATH. Tests point it at a missing path or at os.Args[0].
	Binary string
}

// Descriptor implements adapter.Factory.
func (f Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID:                 "devin",
		Role:                      adapter.RoleRuntime,
		BuildVersion:              "stage1",
		SupportedExternalVersions: []string{"3000.6.2", "3000.6.7"},
		// Same "names the evidence until a conformance record exists"
		// pattern as notes16-claude-2.1.240. Must match
		// internal/cli.devinDigest.
		ConformanceRecordDigest:   "notes59-devin-3000.6.7",
		DiagnosticRedactionPolicy: "redact-credentials-and-transcript-content",
	}
}

// Probe implements adapter.Factory. It does not exec `devin --version`
// (I-D7: auto-update is a pin hazard; notes/59 moved the binary mid-session).
// LookPath only: found is Unverified, missing is Unavailable. A probe does
// not publish live Duo operation support by itself (§5.1).
func (f Factory) Probe(context.Context) (adapter.Probe, error) {
	binary := f.Binary
	if binary == "" {
		binary = "devin"
	}
	probe := adapter.Probe{
		ProtocolOrFormatIdentity: SessionIDFormatIdentity,
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
		return nil, fmt.Errorf("devin runtime %s: probe reported unavailable, refusing to build an adapter", f.IntegrationInstanceID)
	}
	return New(f.IntegrationInstanceID), nil
}
