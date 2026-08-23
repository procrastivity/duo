package conformance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// integrationNamePairs are (session_host, agent_runtime) pairs the dynamic
// half of the neutrality gate runs the encoders against: two pairs drawn
// from real integration names named in the synced fixtures/planning set,
// plus the fake host/runtime pair the step asks for. The fake pair is
// spelled out as plain fixture data here — not imported from
// internal/host/fake or internal/runtime/fake, which another agent owns
// concurrently in this tree. Using literal strings instead of importing
// those packages is deliberate, not a placeholder: the gate only needs a
// second, made-up integration identity flowing through as opaque data, and
// owning the string keeps this package's boundary (internal/conformance/**)
// intact.
var integrationNamePairs = []struct{ host, runtime string }{
	{host: "local_tmux", runtime: "codex_default"},   // contracts/fixtures/duo-external-v1/config.json
	{host: "local_herdr", runtime: "claude_default"}, // a second real-shaped pair, distinct from the first
	{host: "fake_host", runtime: "fake_runtime"},     // the fake host + fake runtime pair
}

// neutralityEnvelope builds a session.inspect-shaped result envelope
// carrying host and runtime as opaque identity fields, alongside fields
// that never vary across the pairs. Only session_host and agent_runtime
// differ between calls; everything else is fixed, so any structural
// difference in a projection's output between two pairs can only come from
// the encoder branching on the name.
func neutralityEnvelope(host, runtime string) Canonical {
	return Canonical{
		"schema":     "duo.external/v1",
		"request_id": "req_neutrality_probe",
		"operation":  "session.inspect",
		"result": Canonical{
			"session_id":    "ses_neutrality_1",
			"workspace_id":  "wsp_neutrality_1",
			"lifecycle":     "active",
			"session_host":  host,
			"agent_runtime": runtime,
			"condition": Canonical{
				"value":    "idle",
				"revision": "1",
			},
		},
		"warnings": []any{},
		"features": []any{},
	}
}

// neutralize replaces every occurrence of host and runtime in s with fixed
// placeholders, so encoded output from different name pairs can be compared
// for structural equality once the opaque data itself is masked out.
func neutralize(s, host, runtime string) string {
	s = strings.ReplaceAll(s, host, "\x00HOST\x00")
	s = strings.ReplaceAll(s, runtime, "\x00RUNTIME\x00")
	return s
}

// neutralityStreamItem builds a runtime_configuration-shaped stream item
// carrying host and runtime as opaque identity fields, for the presentation
// projection's SSE encoding path (EncodeHTTPBody only exercises the
// plain-body path; stream items take the SSE framing path instead — see
// roundtrip_test.go).
func neutralityStreamItem(host, runtime string) Canonical {
	return Canonical{
		"schema": "duo.external/v1",
		"stream": "session.runtime_configuration",
		"scope": Canonical{
			"session_id":          "ses_neutrality_1",
			"runtime_instance_id": runtime,
		},
		"item_id":     "sti_neutrality_1",
		"position":    "1",
		"recorded_at": "2026-08-13T12:00:00Z",
		"kind":        "view.changed",
		"subject":     Canonical{"kind": "runtime_configuration", "id": "ses_neutrality_1"},
		"data": Canonical{
			"session_host": host,
			"revision":     "1",
		},
	}
}

// TestIntegrationNameNeutralityGate is the dynamic half of the gate: for
// each projection encoder, running the same envelope shape through every
// integration-name pair — including the fake host + fake runtime pair —
// must (a) round-trip each pair back to its own exact envelope, and (b)
// produce structurally identical encoded output once the pair's own name
// values are masked out. (b) is the actual neutrality proof: if an encoder
// special-cased any name (an "if session_host == …" branch), masking the
// name values afterward would not erase the structural difference the
// branch introduced, and the comparison below would fail.
//
// Negative-test note (run manually, not by CI): temporarily add a branch
// like `if host == "fake_host" { out["quirk"] = true }` inside EncodeMCP or
// EncodeHTTPBody, rerun this test, and it fails on the structural
// comparison; revert and it passes again. TestEncodersContainNoIntegrationNameLiteral
// below would also catch a literal like "fake_host" appearing in the
// encoder source directly.
func TestIntegrationNameNeutralityGate(t *testing.T) {
	if len(integrationNamePairs) < 2 {
		t.Fatal("neutrality gate needs at least two name pairs to compare")
	}

	t.Run("CLI", func(t *testing.T) {
		checkNeutral(t, func(v Canonical) []byte { return EncodeCLI(v) }, DecodeCLI)
	})
	t.Run("MCP", func(t *testing.T) {
		checkNeutral(t, EncodeMCP, DecodeMCP)
	})
	t.Run("presentation", func(t *testing.T) {
		checkNeutral(t, func(v Canonical) []byte { return EncodeHTTPBody(v) }, DecodeHTTPBody)
	})
	t.Run("presentation_sse", func(t *testing.T) {
		checkNeutralStreamItem(t, EncodeSSE, DecodeSSE)
	})
}

func checkNeutral(t *testing.T, encode func(Canonical) []byte, decode func([]byte) (Canonical, error)) {
	t.Helper()

	var normalized []string
	for _, pair := range integrationNamePairs {
		envelope := neutralityEnvelope(pair.host, pair.runtime)

		encoded := encode(cloneJSON(envelope))
		decoded, err := decode(encoded)
		if err != nil {
			t.Fatalf("pair %s/%s: decode: %v", pair.host, pair.runtime, err)
		}
		if !equalCanonical(decoded, envelope) {
			t.Fatalf("pair %s/%s: round trip changed the envelope:\n got:  %#v\n want: %#v", pair.host, pair.runtime, decoded, envelope)
		}

		normalized = append(normalized, neutralize(string(encoded), pair.host, pair.runtime))
	}

	for i := 1; i < len(normalized); i++ {
		if normalized[i] != normalized[0] {
			t.Errorf("encoded output differs by more than the substituted integration names:\npair 0: %s\npair %d: %s", normalized[0], i, normalized[i])
		}
	}
}

func checkNeutralStreamItem(t *testing.T, encode func(Canonical) []byte, decode func([]byte) (Canonical, error)) {
	t.Helper()

	var normalized []string
	for _, pair := range integrationNamePairs {
		item := neutralityStreamItem(pair.host, pair.runtime)

		encoded := encode(cloneJSON(item))
		decoded, err := decode(encoded)
		if err != nil {
			t.Fatalf("pair %s/%s: decode: %v", pair.host, pair.runtime, err)
		}
		if !equalCanonical(decoded, item) {
			t.Fatalf("pair %s/%s: round trip changed the stream item:\n got:  %#v\n want: %#v", pair.host, pair.runtime, decoded, item)
		}

		normalized = append(normalized, neutralize(string(encoded), pair.host, pair.runtime))
	}

	for i := 1; i < len(normalized); i++ {
		if normalized[i] != normalized[0] {
			t.Errorf("encoded output differs by more than the substituted integration names:\npair 0: %s\npair %d: %s", normalized[0], i, normalized[i])
		}
	}
}

// forbiddenIntegrationNameTokens are integration/adapter names that must
// never appear as literal text in this package's non-test source: the
// session-host, agent-runtime, and protocol implementations Duo names in
// the planning set (duo-vnext-chat-view-contract.md §4's own list), plus
// this package's own fake pair. A real branch on any of these is the
// concrete failure mode the neutrality gate exists to prevent.
var forbiddenIntegrationNameTokens = []string{
	"tmux", "codex", "herdr", "solo", "opencode", "ownpty",
	"claude_code", "claude code", "fake_host", "fake_runtime",
}

// TestEncodersContainNoIntegrationNameLiteral is the static half of the
// gate: it scans every non-test .go file in this package for a forbidden
// token. `go test` always runs with the package directory as the working
// directory, so "." is this package. A file that fails this test is
// branching (or about to branch) on an integration name rather than
// treating it as opaque data flowing through Canonical.
func TestEncodersContainNoIntegrationNameLiteral(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}

	var scanned int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		scanned++
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		lower := strings.ToLower(string(data))
		for _, token := range forbiddenIntegrationNameTokens {
			if strings.Contains(lower, strings.ToLower(token)) {
				t.Errorf("%s mentions integration-name token %q; encoders must stay integration-name-neutral (see TestIntegrationNameNeutralityGate)", name, token)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("scanned zero source files; the package layout changed under this test")
	}
}
