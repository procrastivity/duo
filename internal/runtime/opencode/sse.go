package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Event keeps transport SSE identity separate from OpenCode's event object.
type Event struct {
	SSEID string
	// Epoch is the admitted connection epoch attached by the framer. The
	// OpenCode wire does not emit process epoch and this field is not proof
	// supplied by the event itself.
	Epoch      string
	ID         string
	Type       string
	SessionID  string
	Data       json.RawMessage
	Properties json.RawMessage
}

// MaxFrameSize bounds one buffered SSE frame.
const MaxFrameSize = 1 << 20

// ParseSSEFrame parses transport data with caller-supplied context. The
// epoch argument is admission context, not evidence extracted from the wire.
func ParseSSEFrame(raw []byte, epoch, session string) (Event, error) {
	if epoch == "" || !sessionIDRE.MatchString(session) {
		return Event{}, fmt.Errorf("opencode: unbound SSE frame")
	}
	if len(raw) > MaxFrameSize {
		return Event{}, fmt.Errorf("opencode: SSE frame too large")
	}
	if !hasFrameEnd(raw) {
		return Event{}, fmt.Errorf("opencode: incomplete SSE frame")
	}
	var sid, data string
	for _, line := range strings.FieldsFunc(string(raw), func(r rune) bool { return r == '\n' || r == '\r' }) {
		if strings.HasPrefix(line, "id:") {
			sid = strings.TrimSpace(line[3:])
		}
		if strings.HasPrefix(line, "data:") {
			if data != "" {
				data += "\n"
			}
			data += strings.TrimPrefix(strings.TrimPrefix(line[5:], " "), "\t")
		}
	}
	if data == "" || data == "null" {
		return Event{}, fmt.Errorf("opencode: SSE frame has no data")
	}
	var v struct {
		ID           *string         `json:"id"`
		Type         string          `json:"type"`
		Properties   json.RawMessage `json:"properties"`
		ProcessEpoch *string         `json:"processEpoch"`
		Epoch        *string         `json:"epoch"`
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return Event{}, fmt.Errorf("opencode: malformed SSE envelope")
	}
	for key := range envelope {
		switch key {
		case "id", "type", "properties", "processEpoch", "epoch":
		default:
			return Event{}, fmt.Errorf("opencode: unknown SSE envelope")
		}
	}
	dec := json.NewDecoder(strings.NewReader(data))
	if dec.Decode(&v) != nil || v.Type == "" || len(v.Properties) == 0 || string(v.Properties) == "null" {
		return Event{}, fmt.Errorf("opencode: malformed SSE envelope")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return Event{}, fmt.Errorf("opencode: trailing SSE data")
	}
	var props struct {
		ID        *string `json:"id"`
		SessionID *string `json:"sessionID"`
		Info      *struct {
			ID        *string `json:"id"`
			SessionID *string `json:"sessionID"`
		} `json:"info"`
		ProcessEpoch *string `json:"processEpoch"`
		Epoch        *string `json:"epoch"`
	}
	pd := json.NewDecoder(bytes.NewReader(v.Properties))
	if pd.Decode(&props) != nil || !exactJSONEnd(pd) {
		return Event{}, fmt.Errorf("opencode: malformed SSE properties")
	}
	if props.ID != nil && *props.ID == "" {
		return Event{}, fmt.Errorf("opencode: invalid event id")
	}
	if props.Info != nil && props.Info.ID != nil && *props.Info.ID == "" {
		return Event{}, fmt.Errorf("opencode: invalid event id")
	}
	if v.ID != nil && *v.ID == "" {
		return Event{}, fmt.Errorf("opencode: invalid event id")
	}
	if props.ID != nil && props.Info != nil && props.Info.ID != nil && *props.ID != *props.Info.ID {
		return Event{}, fmt.Errorf("opencode: conflicting event id")
	}
	if v.ID != nil && props.ID != nil && *v.ID != *props.ID {
		return Event{}, fmt.Errorf("opencode: conflicting event id")
	}
	if v.ID != nil && props.Info != nil && props.Info.ID != nil && *v.ID != *props.Info.ID {
		return Event{}, fmt.Errorf("opencode: conflicting event id")
	}
	if props.SessionID != nil && props.Info != nil && props.Info.SessionID != nil && *props.SessionID != *props.Info.SessionID {
		return Event{}, fmt.Errorf("opencode: conflicting event session")
	}
	gotSession := ""
	if props.SessionID != nil {
		gotSession = *props.SessionID
	} else if props.Info != nil && props.Info.SessionID != nil {
		gotSession = *props.Info.SessionID
	}
	if gotSession != "" && !sessionIDRE.MatchString(gotSession) {
		return Event{}, fmt.Errorf("opencode: invalid event session")
	}
	if v.Type != "server.connected" && gotSession == "" {
		return Event{}, fmt.Errorf("opencode: event session is missing")
	}
	if gotSession != "" && gotSession != session {
		return Event{}, fmt.Errorf("opencode: event is outside session")
	}
	for _, claimed := range []*string{v.ProcessEpoch, v.Epoch} {
		if claimed != nil && *claimed != epoch {
			return Event{}, fmt.Errorf("opencode: event epoch conflicts with admitted connection")
		}
	}
	for _, claimed := range []*string{props.ProcessEpoch, props.Epoch} {
		if claimed != nil && *claimed != epoch {
			return Event{}, fmt.Errorf("opencode: event epoch conflicts with admitted connection")
		}
	}
	eid := ""
	if v.ID != nil {
		eid = *v.ID
	} else if props.ID != nil {
		eid = *props.ID
	} else if props.Info != nil && props.Info.ID != nil {
		eid = *props.Info.ID
	}
	return Event{SSEID: sid, Epoch: epoch, ID: eid, Type: v.Type, SessionID: gotSession, Properties: append(json.RawMessage(nil), v.Properties...), Data: json.RawMessage(data)}, nil
}

// Framer incrementally parses bound SSE frames without reconnecting.
type Framer struct {
	buf            []byte
	epoch, session string
	truncated      bool
}

// NewFramer creates a framer bound to one admitted epoch and session.
func NewFramer(epoch, session string) (*Framer, error) {
	if epoch == "" || !sessionIDRE.MatchString(session) {
		return nil, fmt.Errorf("opencode: invalid SSE binding")
	}
	return &Framer{epoch: epoch, session: session}, nil
}

// Feed appends transport bytes and returns complete parsed events.
func (f *Framer) Feed(p []byte) ([]Event, error) {
	f.buf = append(f.buf, p...)
	var out []Event
	for {
		i, n := frameBoundary(f.buf)
		if i < 0 {
			if len(f.buf) > MaxFrameSize {
				return out, fmt.Errorf("opencode: SSE frame too large")
			}
			return out, nil
		}
		raw := append([]byte(nil), f.buf[:i+n]...)
		f.buf = f.buf[i+n:]
		e, err := ParseSSEFrame(raw, f.epoch, f.session)
		if err != nil {
			return out, err
		}
		out = append(out, e)
	}
}

func hasFrameEnd(p []byte) bool { _, n := frameBoundary(p); return n != 0 }

// frameBoundary accepts all three line-ending forms defined by SSE.
func frameBoundary(p []byte) (int, int) {
	best, width := -1, 0
	for i := 0; i < len(p); i++ {
		var n int
		switch {
		case p[i] == '\n' && i+1 < len(p) && p[i+1] == '\n':
			n = 2
		case p[i] == '\r' && i+1 < len(p) && p[i+1] == '\r':
			n = 2
		case p[i] == '\r' && i+3 < len(p) && p[i+1] == '\n' && p[i+2] == '\r' && p[i+3] == '\n':
			n = 4
		}
		if n != 0 {
			best, width = i, n
			break
		}
	}
	return best, width
}

// Finalize declares the transport closed. A partial frame is never emitted.
func (f *Framer) Finalize() { f.truncated = len(f.buf) != 0; f.buf = nil }

// Truncated reports whether Finalize discarded an incomplete frame.
func (f *Framer) Truncated() bool { return f.truncated }
