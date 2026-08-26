package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// ObserveCondition implements runtime.ConditionProvider. The stream
// emits one transcript-derived snapshot and stays open until Close —
// no file watch, no agent_end (or any other) subscription. Confidence
// is always inferred: D2 cuts the credentialed rank-2 reporter, so
// the claim's idle/lastSettledAt flags are not treated as reported
// condition. The transcript analog of lastSettledAt is the timestamp
// of the last assistant entry whose stopReason is not toolUse
// (agent_settled, never agent_end — docs/adapters/decisions.md).
//
// A missing or unreadable transcript degrades to unknown; it is not an
// error. Correlate stays an identity bind. Mapping HostLifecycleSource
// process-exit onto exited is a caller concern; this package does not
// import internal/host and does not emit exited. Blocked is
// structurally absent at 0.83.0 and is never inferred.
func (r *Runtime) ObserveCondition(_ context.Context, req runtime.ConditionObservationRequest) (runtime.ConditionObservationStream, error) {
	return runtime.NewStaticConditionStream(r.snapshotCondition(req)), nil
}

func (r *Runtime) snapshotCondition(req runtime.ConditionObservationRequest) runtime.ConditionObservation {
	now := time.Now().UTC()
	unknown := runtime.ConditionObservation{
		Value:      runtime.ConditionUnknown,
		Confidence: runtime.ConditionConfidenceInferred,
		Freshness:  runtime.ConditionFreshnessUnknown,
		ComputedAt: now,
		Reasons:    []string{"missing transcript"},
	}

	path := req.TranscriptID
	if path == "" && req.ExternalAgentSessionID != "" {
		resolved, err := r.resolveTranscript(req.ExternalAgentSessionID, "", "")
		if err != nil || resolved == "" {
			return unknown
		}
		path = resolved
	}
	if path == "" {
		return unknown
	}

	obs, err := derivePiCondition(path, req.ExternalAgentSessionID)
	if err != nil {
		return unknown
	}
	obs.ComputedAt = now
	if obs.Confidence == "" {
		obs.Confidence = runtime.ConditionConfidenceInferred
	}
	return obs
}

type piTurnState int

const (
	piTurnNone piTurnState = iota
	piTurnOpen
	piTurnSettled
)

// conditionEntry is the subset of a pi JSONL line ObserveCondition
// needs. StopReason lives on the message, matching adapter_pi.go:
// anything other than toolUse ends the turn. lastSettledAt on the
// reporter claim is stamped from agent_settled at that same edge.
type conditionEntry struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	Timestamp string `json:"timestamp"`
	Message   struct {
		Role       string `json:"role"`
		StopReason string `json:"stopReason"`
		Content    []struct {
			Type string `json:"type"`
		} `json:"content"`
	} `json:"message"`
}

func derivePiCondition(path, sessionID string) (runtime.ConditionObservation, error) {
	f, err := os.Open(path)
	if err != nil {
		return runtime.ConditionObservation{}, err
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), maxTranscriptLine)

	var (
		state       piTurnState
		effectiveAt time.Time
		obsID       string
		reason      string
		line        int
	)
	for scanner.Scan() {
		line++
		raw := scanner.Bytes()
		if len(strings.TrimSpace(string(raw))) == 0 {
			continue
		}
		if line == 1 {
			if sessionID != "" {
				if err := checkHeader(raw, path, sessionID); err != nil {
					return runtime.ConditionObservation{}, err
				}
			}
			continue
		}
		applyPiLine(raw, &state, &effectiveAt, &obsID, &reason)
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
	case piTurnOpen:
		obs.Value = runtime.ConditionWorking
		obs.Reasons = []string{reason}
	case piTurnSettled:
		obs.Value = runtime.ConditionIdle
		obs.Reasons = []string{reason}
	default:
		obs.Value = runtime.ConditionUnknown
		obs.Freshness = runtime.ConditionFreshnessUnknown
		obs.Reasons = []string{"transcript has no turn-boundary evidence"}
	}
	return obs, nil
}

func applyPiLine(raw []byte, state *piTurnState, at *time.Time, obsID, reason *string) {
	var e conditionEntry
	if json.Unmarshal(raw, &e) != nil {
		return
	}
	if e.Type != "message" {
		return
	}
	ts, err := time.Parse(time.RFC3339, e.Timestamp)
	if err != nil {
		ts, _ = time.Parse(time.RFC3339Nano, e.Timestamp)
	}

	switch e.Message.Role {
	case RoleUser:
		markPi(state, piTurnOpen, ts, e.ID, "open turn after user message", at, obsID, reason)
	case RoleToolResult:
		markPi(state, piTurnOpen, ts, e.ID, "open turn after toolResult", at, obsID, reason)
	case RoleAssistant:
		if e.Message.StopReason == "toolUse" || assistantHasToolCall(e) && e.Message.StopReason == "" {
			markPi(state, piTurnOpen, ts, e.ID, "open turn after stopReason toolUse", at, obsID, reason)
			return
		}
		if e.Message.StopReason != "" {
			// stop, aborted, and any other non-toolUse stopReason
			// are the transcript analog of lastSettledAt /
			// agent_settled. agent_end is not consulted.
			markPi(state, piTurnSettled, ts, e.ID, "inferred from stopReason "+e.Message.StopReason+" (lastSettledAt analog)", at, obsID, reason)
			return
		}
		markPi(state, piTurnOpen, ts, e.ID, "open turn after assistant entry", at, obsID, reason)
	}
}

func assistantHasToolCall(e conditionEntry) bool {
	for _, b := range e.Message.Content {
		if b.Type == "toolCall" {
			return true
		}
	}
	return false
}

func markPi(state *piTurnState, next piTurnState, ts time.Time, id, why string, at *time.Time, obsID, reason *string) {
	*state = next
	if !ts.IsZero() {
		*at = ts
	}
	if id != "" {
		*obsID = id
	}
	*reason = why
}
