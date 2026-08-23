// Package runtime defines the architecture's §5.3 agent-runtime adapter
// contract: independent interfaces an agent-runtime adapter can implement,
// plus the evidence and request/result types they share.
//
// Stage-1 scope, deliberately: this package implements only
// RuntimeCorrelator and ConversationProvider — the two interfaces the Step
// 11 spec names. §5.3 also defines ConditionProvider, RuntimePromptProvider,
// UsageProvider, RuntimeConfigurationProvider, and HarnessRenderer; those
// are condition, prompt, usage, configuration, and rendering concerns the
// spec assigns to Stage 2+. They are intentionally not scaffolded here — no
// empty interfaces, no TODO types — because a stub signature would commit
// this step to field shapes (a ConditionObservationStream, a
// UsageObservation, ...) that later sessions have not yet decided. See
// docs/adapters/decisions.md.
//
// RuntimeClaim, RuntimeCorrelationEvidence, and RuntimeCorrelator are
// copied verbatim from §5.3's Go code block, including the "Runtime"
// prefix that stutters as runtime.RuntimeX from another package. That
// stutter is the normative contract, not a naming choice this step made,
// so the lint exclusions on those three declarations are load-bearing.
//
// This package must never import internal/host; internal/adapter's
// TestRoleSeparation enforces that mechanically.
package runtime

import (
	"context"
	"time"
)

// RuntimeClaim is what RuntimeCorrelator.Correlate is asked to bind:
// scoped, individually insufficient identifiers for one external agent
// runtime instance. §5.3: "Runtime evidence keeps the external
// agent-session and transcript identifiers as scoped correlations. A
// transcript path or working directory cannot bind a runtime instance" by
// itself.
//
//nolint:revive // name is §5.3's Go code block verbatim; see package doc comment.
type RuntimeClaim struct {
	IntegrationInstanceID  string
	ExternalAgentSessionID string
	TranscriptPath         string
	WorkingDirectory       string
	// ReporterCredential is set when the claim originates from a generated
	// hook. §5.3: "Generated hooks receive a reporter credential for one
	// exact runtime instance." Empty when the claim comes from another
	// evidence source (e.g. a direct probe).
	ReporterCredential string
}

// RuntimeCorrelationEvidence is Correlate's result: the scoped correlation
// between a claim's external identifiers and one Duo-known runtime
// instance.
//
//nolint:revive // name is §5.3's Go code block verbatim; see package doc comment.
type RuntimeCorrelationEvidence struct {
	ExternalAgentSessionID string
	TranscriptID           string
	// Bound is false when the claim's identifiers do not resolve to one
	// runtime instance (insufficient or ambiguous evidence). A false
	// Bound is a valid, non-error result — Correlate returns an error only
	// for a claim it cannot even attempt (wrong integration instance,
	// transport failure).
	Bound bool
	// Confidence is an adapter-defined strength label for the binding
	// (e.g. "exact", "heuristic"); its vocabulary is per adapter family.
	Confidence string
}

// RuntimeCorrelator binds an external runtime claim to a Duo-known runtime
// instance. §5.3.
//
//nolint:revive // name is §5.3's Go code block verbatim; see package doc comment.
type RuntimeCorrelator interface {
	Correlate(context.Context, RuntimeClaim) (RuntimeCorrelationEvidence, error)
}

// ConversationReadRequest scopes ConversationProvider.ReadConversation to
// one bound runtime instance's transcript and a cursor into it.
type ConversationReadRequest struct {
	ExternalAgentSessionID string
	TranscriptID           string
	// After is an opaque cursor from a prior ConversationBatch.NextCursor.
	// Empty reads from the start of the transcript.
	After string
	// Limit bounds the number of turns returned; zero means the adapter's
	// own default.
	Limit int
}

// ConversationTurn is one semantic transcript entry. The architecture does
// not fix a wire shape for this at Stage 1 (that is
// `conversation.subscribe`'s eventual contract, still a data row per
// docs/registry/decisions.md); this is the minimal shape a
// ConversationProvider needs to be exercised now.
type ConversationTurn struct {
	ID   string
	Role string
	Text string
	At   time.Time
}

// ConversationBatch is ReadConversation's result.
type ConversationBatch struct {
	Turns []ConversationTurn
	// NextCursor continues after this batch; empty when nothing further
	// is available yet (not necessarily the same as Complete — a live
	// transcript can still be empty-cursor "caught up" without being
	// permanently complete).
	NextCursor string
	// Complete is true when this batch reaches the current end of the
	// transcript.
	Complete bool
}

// ConversationProvider reads an agent runtime's transcript as semantic
// turns. §5.3.
type ConversationProvider interface {
	ReadConversation(context.Context, ConversationReadRequest) (ConversationBatch, error)
}
