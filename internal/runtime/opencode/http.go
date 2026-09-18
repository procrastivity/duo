package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/procrastivity/duo/internal/runtime"
)

const (
	maxHealthBody  = 64 << 10
	maxSchemaBody  = 4 << 20
	maxSessionBody = 8 << 20
	maxMessageBody = 32 << 20
	maxStatusBody  = 4 << 20
)

// Session is the read-only projection of an OpenCode session.
type Session struct {
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// Message is the read-only text projection of an OpenCode message.
type Message struct {
	ID        string
	SessionID string
	Role      string
	Text      string
}

func (r *Runtime) request(ctx context.Context, path string, limit int64, out any) error {
	if err := r.ensureAdmitted(); err != nil {
		return err
	}
	if err := r.binding.validate(); err != nil {
		return err
	}
	if r.credential == "" {
		return fmt.Errorf("opencode: credentials required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.binding.Endpoint, "/")+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", r.credential)
	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("opencode: read unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("opencode: authentication rejected")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("opencode: read rejected")
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return fmt.Errorf("opencode: malformed response")
	}
	if int64(len(body)) > limit {
		return fmt.Errorf("opencode: response too large")
	}
	if bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		return fmt.Errorf("opencode: malformed response")
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("opencode: malformed response")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return fmt.Errorf("opencode: trailing response data")
	}
	return nil
}

// Health reads the OpenCode global health projection.
func (r *Runtime) Health(ctx context.Context) (map[string]any, error) {
	var v map[string]any
	err := r.request(ctx, "/global/health", maxHealthBody, &v)
	return v, err
}

// Schema reads the OpenCode API schema projection.
func (r *Runtime) Schema(ctx context.Context) (json.RawMessage, error) {
	var v json.RawMessage
	err := r.request(ctx, "/doc", maxSchemaBody, &v)
	return v, err
}

// Sessions reads and filters sessions to the admitted session.
func (r *Runtime) Sessions(ctx context.Context) ([]Session, error) {
	var v json.RawMessage
	if err := r.request(ctx, "/session", maxSessionBody, &v); err != nil {
		return nil, err
	}
	a, err := decodeSessions(v)
	if err != nil {
		return nil, err
	}
	for _, s := range a {
		if s.ID == r.binding.SessionID {
			return []Session{s}, nil
		}
	}
	return nil, fmt.Errorf("opencode: bound session absent")
}

// Session reads the admitted session by its bound ID.
func (r *Runtime) Session(ctx context.Context, id string) (Session, error) {
	if id != r.binding.SessionID {
		return Session{}, fmt.Errorf("opencode: session is outside binding")
	}
	var raw json.RawMessage
	if !sessionIDRE.MatchString(id) {
		return Session{}, fmt.Errorf("opencode: invalid session id")
	}
	if err := r.request(ctx, "/session/"+url.PathEscape(id), maxSessionBody, &raw); err != nil {
		return Session{}, err
	}
	var v Session
	dec := json.NewDecoder(bytes.NewReader(raw))
	if dec.Decode(&v) != nil || !exactJSONEnd(dec) {
		return v, fmt.Errorf("opencode: malformed session response")
	}
	if v.ID != id {
		return v, fmt.Errorf("opencode: contradictory session identity")
	}
	return v, nil
}

// Messages reads text messages from the admitted session.
func (r *Runtime) Messages(ctx context.Context) ([]Message, error) {
	var v json.RawMessage
	if err := r.request(ctx, r.binding.sessionPath("/message"), maxMessageBody, &v); err != nil {
		return nil, err
	}
	trimmed := bytes.TrimSpace(v)
	if len(trimmed) == 0 || trimmed[0] != '[' {
		return nil, fmt.Errorf("opencode: malformed message response")
	}
	return decodeMessages(v, r.binding.SessionID)
}

// Todo reads the admitted session's todo projection.
func (r *Runtime) Todo(ctx context.Context) (json.RawMessage, error) {
	var v json.RawMessage
	return v, r.request(ctx, r.binding.sessionPath("/todo"), maxMessageBody, &v)
}

// Status reads the admitted session's status projection.
func (r *Runtime) Status(ctx context.Context) (json.RawMessage, error) {
	var v map[string]json.RawMessage
	if err := r.request(ctx, "/session/status", maxStatusBody, &v); err != nil {
		return nil, err
	}
	status, ok := v[r.binding.SessionID]
	if !ok || len(status) == 0 || bytes.Equal(bytes.TrimSpace(status), []byte("null")) {
		return nil, fmt.Errorf("opencode: bound session status absent")
	}
	for id := range v {
		if !sessionIDRE.MatchString(id) {
			return nil, fmt.Errorf("opencode: contradictory session status")
		}
	}
	return status, nil
}

func decodeSessions(raw []byte) ([]Session, error) {
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return nil, fmt.Errorf("opencode: malformed session response")
	}
	var a []Session
	dec0 := json.NewDecoder(bytes.NewReader(raw))
	if dec0.Decode(&a) == nil && exactJSONEnd(dec0) {
		return checkSessions(a)
	}
	var w struct {
		Sessions []Session `json:"sessions"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if dec.Decode(&w) != nil || !exactJSONEnd(dec) {
		return nil, fmt.Errorf("opencode: malformed session response")
	}
	if w.Sessions == nil {
		return nil, fmt.Errorf("opencode: missing sessions")
	}
	return checkSessions(w.Sessions)
}

func checkSessions(a []Session) ([]Session, error) {
	for _, s := range a {
		if !sessionIDRE.MatchString(s.ID) {
			return nil, fmt.Errorf("opencode: unknown session schema")
		}
	}
	return a, nil
}

func decodeMessages(raw []byte, sid string) ([]Message, error) {
	var a []struct {
		ID        string `json:"id"`
		SessionID string `json:"sessionID"`
		Info      struct {
			ID        string `json:"id"`
			SessionID string `json:"sessionID"`
			Role      string `json:"role"`
		} `json:"info"`
		Parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"parts"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if dec.Decode(&a) != nil || !exactJSONEnd(dec) || a == nil {
		return nil, fmt.Errorf("opencode: malformed message response")
	}
	out := make([]Message, 0, len(a))
	for _, x := range a {
		if x.ID != "" && x.Info.ID != "" && x.ID != x.Info.ID {
			return nil, fmt.Errorf("opencode: contradictory message identity")
		}
		id := x.ID
		if id == "" {
			id = x.Info.ID
		}
		if x.SessionID != "" && x.Info.SessionID != "" && x.SessionID != x.Info.SessionID {
			return nil, fmt.Errorf("opencode: contradictory message session")
		}
		xsid := x.SessionID
		if xsid == "" {
			xsid = x.Info.SessionID
		}
		if xsid == "" || xsid != sid {
			return nil, fmt.Errorf("opencode: cross-session message")
		}
		if !validMessageID(id) || (x.Info.Role != "user" && x.Info.Role != "assistant") || len(x.Parts) == 0 {
			return nil, fmt.Errorf("opencode: unknown message schema")
		}
		var b strings.Builder
		for _, p := range x.Parts {
			if p.Type != "text" {
				// OpenCode parts are extensible. Unsupported parts are not
				// conversation text, but do not invalidate an otherwise valid
				// pinned message.
				continue
			}
			b.WriteString(p.Text)
		}
		out = append(out, Message{ID: id, SessionID: sid, Role: x.Info.Role, Text: b.String()})
	}
	return out, nil
}

func validMessageID(id string) bool {
	if id == "" || len(id) > 256 {
		return false
	}
	for _, r := range id {
		if r < 0x21 || r > 0x7e {
			return false
		}
	}
	return true
}

func exactJSONEnd(dec *json.Decoder) bool {
	var extra any
	return dec.Decode(&extra) == io.EOF
}

// ReadConversation returns a complete, non-paginated snapshot of the bound session.
func (r *Runtime) ReadConversation(ctx context.Context, req runtime.ConversationReadRequest) (runtime.ConversationBatch, error) {
	if err := r.ensureAdmitted(); err != nil {
		return runtime.ConversationBatch{}, err
	}
	if req.ExternalAgentSessionID != r.binding.SessionID {
		return runtime.ConversationBatch{}, fmt.Errorf("opencode: conversation is outside binding")
	}
	if req.Limit < 0 || req.After != "" {
		return runtime.ConversationBatch{}, fmt.Errorf("opencode: pagination unsupported")
	}
	msgs, err := r.Messages(ctx)
	if err != nil {
		return runtime.ConversationBatch{}, err
	}
	turns := make([]runtime.ConversationTurn, 0, len(msgs))
	for _, m := range msgs {
		turns = append(turns, runtime.ConversationTurn{ID: m.ID, Role: m.Role, Text: m.Text})
	}
	if req.Limit > 0 && len(turns) > req.Limit {
		return runtime.ConversationBatch{}, fmt.Errorf("opencode: limit would truncate a non-paginated snapshot")
	}
	return runtime.ConversationBatch{Turns: turns, Complete: true}, nil
}

// ObserveCondition returns a conservative unknown observation for the bound session.
func (r *Runtime) ObserveCondition(_ context.Context, req runtime.ConditionObservationRequest) (runtime.ConditionObservationStream, error) {
	if err := r.ensureAdmitted(); err != nil {
		return nil, err
	}
	if req.ExternalAgentSessionID != r.binding.SessionID || req.ExternalAgentSessionID == "" {
		return nil, fmt.Errorf("opencode: condition is outside binding")
	}
	return runtime.NewStaticConditionStream(runtime.ConditionObservation{Value: runtime.ConditionUnknown, Confidence: runtime.ConditionConfidenceUnknown, Freshness: runtime.ConditionFreshnessUnknown, Reasons: []string{"fresh status/event boundary unavailable"}}), nil
}
