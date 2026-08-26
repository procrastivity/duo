package claude

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// Interrupt user-entry texts notes/16 §2 records. These close an open
// turn (Escape mid-generation, dismiss-on-tool). They are not a new
// human prompt: treating them as one would report working after the
// agent has already stopped. ConversationProvider still projects them
// as ordinary user turns; condition is the place that special-cases
// the shape.
const (
	interruptUserText = "[Request interrupted by user]"
	interruptToolText = "[Request interrupted by user for tool use]"
)

// ObserveCondition implements runtime.ConditionProvider. The stream
// emits one transcript-derived snapshot and stays open until Close —
// no file watch, no UserPromptSubmit (or any other) hook. Confidence
// is always inferred: D2 cuts the credentialed rank-2 reporter, so
// this adapter never claims reported.
//
// A missing or unreadable transcript degrades to unknown; it is not an
// error. Correlate stays an identity bind. Mapping HostLifecycleSource
// process-exit onto exited is a caller concern; this package does not
// import internal/host and does not emit exited.
func (r *Runtime) ObserveCondition(_ context.Context, req runtime.ConditionObservationRequest) (runtime.ConditionObservationStream, error) {
	return runtime.NewStaticConditionStream(snapshotCondition(req)), nil
}

func snapshotCondition(req runtime.ConditionObservationRequest) runtime.ConditionObservation {
	now := time.Now().UTC()
	unknown := runtime.ConditionObservation{
		Value:      runtime.ConditionUnknown,
		Confidence: runtime.ConditionConfidenceInferred,
		Freshness:  runtime.ConditionFreshnessUnknown,
		ComputedAt: now,
		Reasons:    []string{"missing transcript"},
	}
	if req.TranscriptID == "" {
		return unknown
	}
	obs, err := deriveClaudeCondition(req.TranscriptID)
	if err != nil {
		unknown.Reasons = []string{"missing transcript"}
		return unknown
	}
	obs.ComputedAt = now
	if obs.Confidence == "" {
		obs.Confidence = runtime.ConditionConfidenceInferred
	}
	return obs
}

type claudeTurnState int

const (
	claudeTurnNone claudeTurnState = iota
	claudeTurnOpen
	claudeTurnSettled
)

func deriveClaudeCondition(path string) (runtime.ConditionObservation, error) {
	f, err := os.Open(path)
	if err != nil {
		return runtime.ConditionObservation{}, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineSize)

	var (
		state       claudeTurnState
		effectiveAt time.Time
		obsID       string
		reason      string
	)
	for scanner.Scan() {
		applyClaudeLine(scanner.Bytes(), &state, &effectiveAt, &obsID, &reason)
	}
	if err := scanner.Err(); err != nil {
		return runtime.ConditionObservation{}, err
	}

	obs := runtime.ConditionObservation{
		ObservationID: obsID,
		Confidence:    runtime.ConditionConfidenceInferred,
		Freshness:     runtime.ConditionFreshnessFresh,
		EffectiveAt:   effectiveAt,
	}
	switch state {
	case claudeTurnOpen:
		obs.Value = runtime.ConditionWorking
		obs.Reasons = []string{reason}
	case claudeTurnSettled:
		obs.Value = runtime.ConditionIdle
		obs.Reasons = []string{reason}
	default:
		obs.Value = runtime.ConditionUnknown
		obs.Freshness = runtime.ConditionFreshnessUnknown
		obs.Reasons = []string{"transcript has no turn-boundary evidence"}
	}
	return obs, nil
}

func applyClaudeLine(raw []byte, state *claudeTurnState, at *time.Time, obsID, reason *string) {
	var e claudeEntry
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	if e.IsSidechain {
		return
	}
	ts, _ := time.Parse(time.RFC3339Nano, e.Timestamp)

	switch e.Type {
	case "system":
		if e.Subtype == "turn_duration" {
			markClaude(state, claudeTurnSettled, ts, e.UUID, "inferred from system/turn_duration", at, obsID, reason)
		}
	case "user":
		applyClaudeUser(e, ts, state, at, obsID, reason)
	case "assistant":
		applyClaudeAssistant(e, ts, state, at, obsID, reason)
	}
}

func applyClaudeUser(e claudeEntry, ts time.Time, state *claudeTurnState, at *time.Time, obsID, reason *string) {
	if e.IsMeta {
		return
	}
	var m claudeMessage
	if json.Unmarshal(e.Message, &m) != nil {
		return
	}

	var text string
	if json.Unmarshal(m.Content, &text) == nil {
		// Interrupt markers close the turn. A peer-origin user prompt
		// still opens one: the agent is working, even though
		// UserPromptSubmit cannot tell peer from human (notes/16 §5)
		// and must not be treated as draft evidence.
		if text == interruptUserText || text == interruptToolText {
			markClaude(state, claudeTurnSettled, ts, e.UUID, "inferred from interrupt user entry", at, obsID, reason)
			return
		}
		markClaude(state, claudeTurnOpen, ts, e.UUID, "open turn after user prompt", at, obsID, reason)
		return
	}

	// Tool-result user envelope: the turn is still in progress.
	var blocks []claudeBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return
	}
	for _, b := range blocks {
		if b.Type == "tool_result" {
			markClaude(state, claudeTurnOpen, ts, e.UUID, "open turn after tool_result", at, obsID, reason)
			return
		}
	}
}

func applyClaudeAssistant(e claudeEntry, ts time.Time, state *claudeTurnState, at *time.Time, obsID, reason *string) {
	var m claudeMessage
	if json.Unmarshal(e.Message, &m) != nil {
		return
	}
	switch m.StopReason {
	case "end_turn", "stop_sequence":
		// Print mode never writes turn_duration. A snapshot of a
		// finished file treats the last end_turn as settled; a live
		// tail would wait for silence, which this milestone does not.
		markClaude(state, claudeTurnSettled, ts, e.UUID, "inferred from stop_reason "+m.StopReason, at, obsID, reason)
	case "tool_use":
		markClaude(state, claudeTurnOpen, ts, e.UUID, "open turn after stop_reason tool_use", at, obsID, reason)
	default:
		// Partial assistant line (thinking, streaming text) while a
		// turn is open, or the first assistant bytes of a new turn.
		markClaude(state, claudeTurnOpen, ts, e.UUID, "open turn after assistant entry", at, obsID, reason)
	}
}

func markClaude(state *claudeTurnState, next claudeTurnState, ts time.Time, id, why string, at *time.Time, obsID, reason *string) {
	*state = next
	if !ts.IsZero() {
		*at = ts
	}
	if id != "" {
		*obsID = id
	}
	*reason = why
}
