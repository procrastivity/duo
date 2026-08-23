// Package fake is the Stage 0 fake agent-runtime adapter: a first-class,
// permanent implementation of the §5.3 Stage-1 runtime interfaces. Every
// cross-composition gate runs it alongside internal/host/fake.
package fake

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
)

// Runtime is the fake agent-runtime adapter.
type Runtime struct {
	mu                    sync.Mutex
	integrationInstanceID string
	bound                 map[string]string // external agent-session ID -> transcript ID
	turns                 map[string][]runtime.ConversationTurn
}

var (
	_ runtime.RuntimeCorrelator    = (*Runtime)(nil)
	_ runtime.ConversationProvider = (*Runtime)(nil)
	_ adapter.Factory[*Runtime]    = Factory{}
)

// New returns a fake runtime adapter for one integration instance.
func New(integrationInstanceID string) *Runtime {
	return &Runtime{
		integrationInstanceID: integrationInstanceID,
		bound:                 make(map[string]string),
		turns:                 make(map[string][]runtime.ConversationTurn),
	}
}

// Factory is the fake runtime's §5.1 adapter factory: a fixed descriptor,
// an always-supported probe, and a New that hands back a fresh Runtime.
type Factory struct {
	IntegrationInstanceID string
}

// Descriptor implements adapter.Factory.
func (f Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID:                 "fake-runtime",
		Role:                      adapter.RoleRuntime,
		BuildVersion:              "stage0",
		SupportedExternalVersions: []string{"*"},
		ConformanceRecordDigest:   "fake-runtime-v0",
		DiagnosticRedactionPolicy: "none",
	}
}

// Probe implements adapter.Factory. The fake runtime is always reachable
// and always compatible with itself.
func (f Factory) Probe(context.Context) (adapter.Probe, error) {
	return adapter.Probe{
		DetectedVersion:          "fake",
		ProtocolOrFormatIdentity: "duo-fake-runtime/v0",
		ConnectionState:          "connected",
		Compatibility:            adapter.CompatibilitySupported,
	}, nil
}

// New implements adapter.Factory.
func (f Factory) New(context.Context, adapter.Probe) (*Runtime, error) {
	return New(f.IntegrationInstanceID), nil
}

// SeedTranscript registers turns for an external agent-session ID so a
// later ReadConversation call has something to return. Test and gate setup
// use this to script the fake's transcript; it is not part of the §5.3
// contract.
func (r *Runtime) SeedTranscript(externalAgentSessionID string, turns ...runtime.ConversationTurn) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.turns[externalAgentSessionID] = append(r.turns[externalAgentSessionID], turns...)
}

// Correlate implements runtime.RuntimeCorrelator. §5.3: "A transcript path
// or working directory cannot bind a runtime instance" alone, so the fake
// only binds when the claim carries a non-empty ExternalAgentSessionID —
// the one identifier the contract treats as load-bearing.
func (r *Runtime) Correlate(_ context.Context, claim runtime.RuntimeClaim) (runtime.RuntimeCorrelationEvidence, error) {
	if claim.IntegrationInstanceID != r.integrationInstanceID {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
			"fake runtime %s: claim for integration instance %s",
			r.integrationInstanceID, claim.IntegrationInstanceID)
	}
	if claim.ExternalAgentSessionID == "" {
		return runtime.RuntimeCorrelationEvidence{Bound: false}, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	transcriptID, ok := r.bound[claim.ExternalAgentSessionID]
	if !ok {
		transcriptID = "transcript-" + claim.ExternalAgentSessionID
		r.bound[claim.ExternalAgentSessionID] = transcriptID
	}
	return runtime.RuntimeCorrelationEvidence{
		ExternalAgentSessionID: claim.ExternalAgentSessionID,
		TranscriptID:           transcriptID,
		Bound:                  true,
		Confidence:             "fake-exact",
	}, nil
}

// ReadConversation implements runtime.ConversationProvider. The cursor
// format is an opaque decimal offset into the seeded turn slice; callers
// must not construct one themselves other than by round-tripping
// NextCursor.
func (r *Runtime) ReadConversation(_ context.Context, req runtime.ConversationReadRequest) (runtime.ConversationBatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	all := r.turns[req.ExternalAgentSessionID]
	offset, err := cursorOffset(req.After)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}
	if offset > len(all) {
		offset = len(all)
	}

	remaining := all[offset:]
	limit := req.Limit
	if limit <= 0 || limit > len(remaining) {
		limit = len(remaining)
	}
	batch := append([]runtime.ConversationTurn(nil), remaining[:limit]...)
	nextOffset := offset + limit

	return runtime.ConversationBatch{
		Turns:      batch,
		NextCursor: strconv.Itoa(nextOffset),
		Complete:   nextOffset >= len(all),
	}, nil
}

func cursorOffset(after string) (int, error) {
	if after == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(after)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("fake runtime: invalid cursor %q", after)
	}
	return n, nil
}
