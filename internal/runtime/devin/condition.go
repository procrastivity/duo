package devin

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/procrastivity/duo/internal/runtime"
)

// errHeaderMismatch is the condition-reason sentinel when an ATIF
// document's session_id does not match the requested external id.
var errHeaderMismatch = errors.New("transcript header does not match session")

// ObserveCondition implements runtime.ConditionProvider. The stream
// emits one ATIF-derived snapshot and stays open until Close — no file
// watch, no hooks (I-D13). Confidence is always inferred. A missing or
// unreadable document degrades to unknown; it is not an error.
// SessionEnd reason "other" is not consumed here; do not treat it as
// crash if a later hook consumer lands.
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
	obs, err := deriveDevinCondition(req.TranscriptID, req.ExternalAgentSessionID)
	if err != nil {
		unknown.Reasons = []string{conditionReason(err)}
		return unknown
	}
	obs.ComputedAt = now
	if obs.Confidence == "" {
		obs.Confidence = runtime.ConditionConfidenceInferred
	}
	return obs
}

func conditionReason(err error) string {
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "missing transcript"
	case errors.Is(err, errHeaderMismatch):
		return "transcript header does not match session"
	default:
		return "unreadable transcript"
	}
}

func deriveDevinCondition(path, sessionID string) (runtime.ConditionObservation, error) {
	doc, err := readATIF(path)
	if err != nil {
		return runtime.ConditionObservation{}, err
	}
	if sessionID != "" && doc.SessionID != "" && doc.SessionID != sessionID {
		return runtime.ConditionObservation{}, errHeaderMismatch
	}

	var (
		state       atifTurnState
		effectiveAt time.Time
		obsID       string
		reason      string
	)
	for _, step := range doc.Steps {
		applyATIFStep(step, &state, &effectiveAt, &obsID, &reason)
	}

	obs := runtime.ConditionObservation{
		ObservationID: obsID,
		Confidence:    runtime.ConditionConfidenceInferred,
		Freshness:     runtime.ConditionFreshnessFresh,
		EffectiveAt:   effectiveAt,
	}
	switch state {
	case atifTurnOpen:
		obs.Value = runtime.ConditionWorking
		obs.Reasons = []string{reason}
	case atifTurnSettled:
		obs.Value = runtime.ConditionIdle
		obs.Reasons = []string{reason}
	default:
		obs.Value = runtime.ConditionUnknown
		obs.Freshness = runtime.ConditionFreshnessUnknown
		obs.Reasons = []string{"transcript has no turn-boundary evidence"}
	}
	return obs, nil
}

type atifTurnState int

const (
	atifTurnNone atifTurnState = iota
	atifTurnOpen
	atifTurnSettled
)

func applyATIFStep(step atifStep, state *atifTurnState, at *time.Time, obsID, reason *string) {
	ts, _ := parseATIFTime(step.Timestamp)
	id := ""
	if step.StepID != 0 {
		id = strconv.Itoa(step.StepID)
	}
	switch step.Source {
	case "user":
		if step.Message == "" {
			return
		}
		markATIF(state, atifTurnOpen, ts, id, "open turn after user prompt", at, obsID, reason)
	case "agent":
		markATIF(state, atifTurnSettled, ts, id, "inferred from ATIF agent step", at, obsID, reason)
	}
}

func markATIF(state *atifTurnState, next atifTurnState, ts time.Time, id, why string, at *time.Time, obsID, reason *string) {
	*state = next
	if !ts.IsZero() {
		*at = ts
	}
	if id != "" {
		*obsID = id
	}
	*reason = why
}
