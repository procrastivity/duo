package opencode

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestDescriptorAndCapabilityGuard(t *testing.T) {
	d := (Factory{}).Descriptor()
	if d.AdapterID != AdapterID || d.Role != "runtime" || d.SupportedExternalVersions[0] != PinnedExternalVersion {
		t.Fatalf("descriptor = %+v", d)
	}
	if d.ConformanceRecordDigest == "" || d.DiagnosticRedactionPolicy == "" {
		t.Fatal("descriptor lacks evidence or redaction policy")
	}
	if _, ok := any(&Runtime{}).(interface {
		DeliverPrompt(context.Context, any) error
	}); ok {
		t.Fatal("observer unexpectedly exposes prompt delivery")
	}
}

func TestBindingAndSSEGuards(t *testing.T) {
	b := Binding{IntegrationInstanceID: "i", ProcessEpoch: "e1", Endpoint: "http://127.0.0.1:1234", SessionID: "ses_one"}
	r, err := New(b, nil, "Bearer secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.CorrelateBinding(b); err == nil {
		t.Fatal("directly constructed runtime correlated a matching binding")
	}
	if err := r.CorrelateBinding(Binding{IntegrationInstanceID: "i", ProcessEpoch: "e2", Endpoint: b.Endpoint, SessionID: b.SessionID}); err == nil {
		t.Fatal("foreign epoch bound")
	}
	e, err := ParseSSEFrame([]byte("id: transport-1\ndata: {\"type\":\"message.updated\",\"properties\":{\"id\":\"event-1\",\"sessionID\":\"ses_one\"}}\n\n"), "e1", "ses_one")
	if err != nil || e.SSEID != "transport-1" || e.ID != "event-1" {
		t.Fatalf("event = %+v, err=%v", e, err)
	}
	if _, err := ParseSSEFrame([]byte("data: null\n\n"), "", ""); err == nil {
		t.Fatal("accepted null SSE data")
	}
	if _, err := ParseSSEFrame([]byte("data: {\"type\":\"x\",\"properties\":{\"sessionID\":\"ses_one\",\"processEpoch\":\"other\"}}\n\n"), "e1", "ses_one"); err == nil {
		t.Fatal("accepted conflicting event epoch")
	}
	f, err := NewFramer("e1", "ses_one")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := f.Feed([]byte("data: {\"type\":\"x\",\"processEpoch\":\"e1\",\"sessionID\":\"ses_one\"}")); err != nil || len(got) != 0 {
		t.Fatal("partial frame was applied")
	}
	f.Finalize()
	if !f.Truncated() {
		t.Fatal("partial frame was not marked truncated")
	}
	for _, raw := range []string{
		"data: {\"type\":\"message.updated\",\"properties\":{\"id\":\"event-1\"}}\n\n",
		"data: {\"type\":\"message.updated\",\"properties\":{\"id\":\"event-1\",\"sessionID\":\"ses_other\"}}\n\n",
	} {
		if _, err := ParseSSEFrame([]byte(raw), "e1", "ses_one"); err == nil {
			t.Fatalf("accepted session-isolated event: %s", raw)
		}
	}
	if _, err := ParseSSEFrame([]byte("data: {\"type\":\"server.connected\",\"properties\":{}}\n\n"), "e1", "ses_one"); err != nil {
		t.Fatalf("server.connected without session rejected: %v", err)
	}
	if _, err := ParseSSEFrame([]byte("data: {\"type\":\"message.updated\",\"properties\":{\"sessionID\":\"ses_one\"},\"unknown\":true}\n\n"), "e1", "ses_one"); err == nil {
		t.Fatal("accepted unknown SSE envelope")
	}
}

func TestNestedMessageUpdatedIdentityAndCrossSession(t *testing.T) {
	raw := []byte("id: transport-42\ndata: {\"type\":\"message.updated\",\"properties\":{\"info\":{\"id\":\"msg-42\",\"sessionID\":\"ses_one\"}}}\n\n")
	e, err := ParseSSEFrame(raw, "epoch-a", "ses_one")
	if err != nil || e.Type != "message.updated" || e.SSEID != "transport-42" || e.ID != "msg-42" || e.SessionID != "ses_one" {
		t.Fatalf("nested message.updated = %+v, err=%v", e, err)
	}
	cross := []byte("data: {\"type\":\"message.updated\",\"properties\":{\"info\":{\"id\":\"msg-43\",\"sessionID\":\"ses_other\"}}}\n\n")
	if _, err := ParseSSEFrame(cross, "epoch-a", "ses_one"); err == nil {
		t.Fatal("accepted nested message.updated from another session")
	}
}

func TestTopLevelEventIdentityIsSeparateFromSSEIdentity(t *testing.T) {
	raw := []byte("id: transport-9\ndata: {\"id\":\"event-9\",\"type\":\"message.updated\",\"properties\":{\"info\":{\"id\":\"event-9\",\"sessionID\":\"ses_one\"}}}\n\n")
	e, err := ParseSSEFrame(raw, "epoch-a", "ses_one")
	if err != nil {
		t.Fatal(err)
	}
	if e.SSEID != "transport-9" || e.ID != "event-9" {
		t.Fatalf("event identities = SSE %q, object %q", e.SSEID, e.ID)
	}
	if _, err := ParseSSEFrame([]byte("id: transport-9\ndata: {\"id\":\"event-9\",\"type\":\"message.updated\",\"properties\":{\"id\":\"event-other\",\"sessionID\":\"ses_one\"}}\n\n"), "epoch-a", "ses_one"); err == nil {
		t.Fatal("accepted contradictory top-level and nested event identities")
	}
}

func TestDecodeMessagesRejectsContradictoryNestedIdentity(t *testing.T) {
	raw := []byte(`[{"id":"msg-top","sessionID":"ses_one","info":{"id":"msg-nested","sessionID":"ses_one","role":"user"},"parts":[{"type":"text","text":"hello"}]}]`)
	if _, err := decodeMessages(raw, "ses_one"); err == nil {
		t.Fatal("accepted contradictory top-level and nested message identity")
	}
}

func TestHTTPReadAuthAndNormalization(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer ok" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		if req.URL.Path == "/session/ses_one/message" {
			_, _ = w.Write([]byte(`[{"info":{"id":"msg1","sessionID":"ses_one","role":"user","time":{"created":1},"metadata":{"client":"fixture"}},"parts":[{"type":"text","text":"hello"},{"type":"tool","tool":"shell","state":{"status":"completed"}}],"metadata":{"source":"fixture"}}]`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()
	// httptest is intentionally not loopback-admitted as a binding target;
	// use its loopback address while retaining the test server transport.
	endpoint := strings.Replace(ts.URL, "127.0.0.1", "127.0.0.1", 1)
	r := &Runtime{binding: Binding{IntegrationInstanceID: "i", ProcessEpoch: "e", Endpoint: endpoint, SessionID: "ses_one"}, client: ts.Client(), credential: "Bearer ok", admitted: true}
	msgs, err := r.Messages(context.Background())
	if err != nil || len(msgs) != 1 || msgs[0].Text != "hello" {
		t.Fatalf("messages=%+v err=%v", msgs, err)
	}
	if _, err = New(r.Binding(), ts.Client(), ""); err == nil {
		t.Fatal("missing credentials accepted")
	}
}

func TestProbeRejectsNonLoopbackEndpointWithDocSource(t *testing.T) {
	var requests int
	var authorization string
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		requests++
		authorization = req.Header.Get("Authorization")
		return nil, fmt.Errorf("unexpected request")
	})}
	docCalled := false
	f := Factory{
		Binary:     "/bin/echo",
		Endpoint:   "http://example.com:80",
		Credential: "Bearer secret",
		Client:     client,
		DocSource: func(context.Context) ([]byte, error) {
			docCalled = true
			return []byte(`{"schema":true}`), nil
		},
	}
	p, err := f.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.Compatibility == adapter.CompatibilitySupported {
		t.Fatal("Probe admitted a non-loopback endpoint")
	}
	if requests != 0 || authorization != "" || docCalled {
		t.Fatalf("Probe made an unsafe admission attempt: requests=%d authorization=%q docCalled=%t", requests, authorization, docCalled)
	}
}

func TestDirectConstructionCannotRead(t *testing.T) {
	r, err := New(Binding{IntegrationInstanceID: "i", ProcessEpoch: "e", Endpoint: "http://127.0.0.1:1234", SessionID: "ses_one"}, nil, "Bearer ok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Sessions(context.Background()); err == nil {
		t.Fatal("directly constructed runtime performed a read")
	}
	if _, err := r.ObserveCondition(context.Background(), runtime.ConditionObservationRequest{ExternalAgentSessionID: "ses_one"}); err == nil {
		t.Fatal("directly constructed runtime observed a condition")
	}
}

func TestValidateLaunchMetadataChecksRecordedFlags(t *testing.T) {
	spec, err := PureLaunchSpec(1234)
	if err != nil {
		t.Fatal(err)
	}
	metadata := LaunchMetadata{IntegrationInstanceID: "i", ProcessEpoch: "e", Endpoint: "http://127.0.0.1:1234", ExecutableSHA256: ExecutableSHA256, SchemaSHA256: SchemaSHA256, PID: 1, Port: 1234, Flags: append([]string(nil), spec.Args...), OwnershipVerified: true}
	if err := ValidateLaunchMetadata(metadata, spec); err != nil {
		t.Fatalf("valid flags rejected: %v", err)
	}
	metadata.Flags[1] = "--not-pure"
	if err := ValidateLaunchMetadata(metadata, spec); err == nil {
		t.Fatal("wrong recorded flags accepted")
	}
}

func TestLaunchEpochCannotAdmitAnotherBinding(t *testing.T) {
	m := LaunchMetadata{IntegrationInstanceID: "i", ProcessEpoch: "epoch-a", Endpoint: "http://127.0.0.1:1234", SessionID: "ses_one"}
	b := Binding{IntegrationInstanceID: "i", ProcessEpoch: "epoch-b", Endpoint: m.Endpoint, SessionID: m.SessionID}
	if err := validateLaunchBindingIdentity(m, b, "i", m.Endpoint); err == nil {
		t.Fatal("epoch A launch ownership admitted binding epoch B")
	}
}

func TestEventStreamCloseConcurrent(t *testing.T) {
	s := &eventStream{done: make(chan struct{})}
	var canceled int
	var mu sync.Mutex
	s.cancel = func() { mu.Lock(); canceled++; mu.Unlock() }
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _ = s.Close() }()
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if canceled != 1 {
		t.Fatalf("cancel called %d times, want once", canceled)
	}
}

func TestConversationLimitDoesNotSilentlyTruncate(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/session/ses_one/message" {
			http.NotFound(w, req)
			return
		}
		_, _ = w.Write([]byte(`[{"info":{"id":"m1","sessionID":"ses_one","role":"user"},"parts":[{"type":"text","text":"one"}]},{"info":{"id":"m2","sessionID":"ses_one","role":"assistant"},"parts":[{"type":"text","text":"two"}]},{"info":{"id":"m3","sessionID":"ses_one","role":"user"},"parts":[{"type":"text","text":"three"}]}]`))
	}))
	defer ts.Close()
	r := &Runtime{binding: Binding{IntegrationInstanceID: "i", ProcessEpoch: "e", Endpoint: ts.URL, SessionID: "ses_one"}, client: ts.Client(), credential: "Bearer ok", admitted: true}
	if _, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{ExternalAgentSessionID: "ses_one", Limit: 1}); err == nil {
		t.Fatal("limit 1 silently truncated the snapshot")
	}
	for _, limit := range []int{3, 4} {
		batch, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{ExternalAgentSessionID: "ses_one", Limit: limit})
		if err != nil || len(batch.Turns) != 3 || !batch.Complete || batch.NextCursor != "" {
			t.Fatalf("limit %d: batch=%+v err=%v", limit, batch, err)
		}
	}
}

func TestBindingOriginAndFraming(t *testing.T) {
	for _, endpoint := range []string{"https://127.0.0.1:1234", "http://localhost:1234", "http://127.0.0.2:1234", "http://user:pass@127.0.0.1:1234", "http://127.0.0.1:1234/path", "http://127.0.0.1:1234?x=1"} {
		if _, err := New(Binding{IntegrationInstanceID: "i", ProcessEpoch: "e", Endpoint: endpoint, SessionID: "ses_ok"}, nil, "Bearer x"); err == nil {
			t.Fatalf("admitted unsafe endpoint %q", endpoint)
		}
	}
	f, err := NewFramer("epoch", "ses_ok")
	if err != nil {
		t.Fatal(err)
	}
	events, err := f.Feed([]byte("id: wire\ndata: {\"type\":\"x\",\"properties\":{\"id\":\"obj\",\"sessionID\":\"ses_ok\"}}\n\n"))
	if err != nil || len(events) != 1 || events[0].SSEID != "wire" || events[0].ID != "obj" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func TestCorrelateCannotRebindAdmittedSession(t *testing.T) {
	r := &Runtime{binding: Binding{IntegrationInstanceID: "i", ProcessEpoch: "epoch-1", Endpoint: "http://127.0.0.1:1234", SessionID: "ses_old"}, admitted: true}
	for _, sid := range []string{"ses_new", "ses_old"} {
		evidence, err := r.Correlate(context.Background(), runtime.RuntimeClaim{IntegrationInstanceID: "i", ExternalAgentSessionID: sid})
		if err != nil {
			t.Fatal(err)
		}
		if sid != "ses_old" && evidence.Bound {
			t.Fatalf("foreign session inherited admission: %+v", evidence)
		}
	}
}

func TestFactoryPinAndRedaction(t *testing.T) {
	f := Factory{IntegrationInstanceID: "i", Endpoint: "http://127.0.0.1:1234", Credential: "Bearer secret", Binding: Binding{IntegrationInstanceID: "i", ProcessEpoch: "epoch", Endpoint: "http://127.0.0.1:1234", SessionID: "ses_ok"}}
	if _, err := f.New(context.Background(), adapter.Probe{Compatibility: adapter.CompatibilitySupported}); err == nil {
		t.Fatal("accepted forged probe")
	}
	public := RedactLaunchMetadata(LaunchMetadata{IntegrationInstanceID: "i", ProcessEpoch: "epoch", Endpoint: "http://127.0.0.1:1234", PID: 7, Port: 1234, SessionID: "ses_ok", Flags: []string{"--secret"}})
	if public.IntegrationInstanceID != "i" || public.SessionID != "ses_ok" || strings.Contains(public.ProcessEpoch, "1234") {
		t.Fatalf("bad diagnostic projection: %+v", public)
	}
}
