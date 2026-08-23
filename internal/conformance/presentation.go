package conformance

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// EncodeHTTPBody renders canonical as a presentation-service JSON response
// body. Stage 0 has no generated presentation result codec
// (docs/conformance/decisions.md): like EncodeCLI, this is the identity
// encoding, present as a seam for the generated codec to replace later.
func EncodeHTTPBody(v Canonical) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("conformance: EncodeHTTPBody: %v", err))
	}
	return data
}

// DecodeHTTPBody parses a presentation JSON response body back to
// Canonical.
func DecodeHTTPBody(data []byte) (Canonical, error) {
	return decodeCanonicalObject(data)
}

// EncodeSSE frames canonical as one presentation-service Server-Sent Event,
// the transport §8 names for streamed items (a duo.external/v1 stream item
// carries a top-level "stream" key, distinguishing it from a request/result
// envelope). This is the one presentation encoding that is not Stage 0
// identity: it exercises real line framing, unlike the plain-body result
// encoding.
func EncodeSSE(v Canonical) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("conformance: EncodeSSE: %v", err))
	}
	var buf bytes.Buffer
	buf.WriteString("event: message\n")
	buf.WriteString("data: ")
	buf.Write(data)
	buf.WriteString("\n\n")
	return buf.Bytes()
}

// DecodeSSE parses one SSE frame back to Canonical. It tolerates a
// multi-line "data:" field (SSE's own continuation rule) by joining
// consecutive data lines, though the harness's own encoder never emits one.
func DecodeSSE(frame []byte) (Canonical, error) {
	var dataLines []string
	scanner := bufio.NewScanner(bytes.NewReader(frame))
	for scanner.Scan() {
		line := scanner.Text()
		if after, ok := strings.CutPrefix(line, "data:"); ok {
			dataLines = append(dataLines, strings.TrimPrefix(after, " "))
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("conformance: DecodeSSE: scanning frame: %w", err)
	}
	if len(dataLines) == 0 {
		return nil, fmt.Errorf("conformance: DecodeSSE: frame carries no data line")
	}
	return decodeCanonicalObject([]byte(strings.Join(dataLines, "\n")))
}

// capturePathParams matches a concrete request path against a registered
// route template ("/v1/sessions/{session_id}") and returns the captured
// placeholder values, plus the query string component (everything after
// "?", or "" when absent). It mirrors internal/registry's
// pathMatchesTemplate but also extracts values, which that test does not
// need.
func capturePathParams(template, concretePathAndQuery string) (params map[string]string, query string, ok bool) {
	concretePath, q, _ := strings.Cut(concretePathAndQuery, "?")
	tSegs := strings.Split(strings.Trim(template, "/"), "/")
	cSegs := strings.Split(strings.Trim(concretePath, "/"), "/")
	if len(tSegs) != len(cSegs) {
		return nil, "", false
	}
	params = map[string]string{}
	for i, seg := range tSegs {
		if after, isPlaceholder := strings.CutPrefix(seg, "{"); isPlaceholder && strings.HasSuffix(after, "}") {
			name := strings.TrimSuffix(after, "}")
			if cSegs[i] == "" {
				return nil, "", false
			}
			params[name] = cSegs[i]
			continue
		}
		if seg != cSegs[i] {
			return nil, "", false
		}
	}
	return params, q, true
}

// DecodePresentationRequest reconstructs a projection-cases.json case's
// semantic.input from its presentation projection: path placeholders
// captured against the registered route template, query parameters, and
// JSON body fields all merge into one Canonical value. numericQuery names
// query keys whose value is a JSON number rather than a string (the same
// role CaseArgSpec.Numeric plays for CLI flags); the "Idempotency-Key"
// header is deliberately not merged in — it duplicates the body's
// idempotency_key, a transport-level replay guard rather than a second
// domain field.
func DecodePresentationRequest(routeTemplate, pathAndQuery string, body Canonical, numericQuery map[string]bool) (Canonical, error) {
	params, rawQuery, ok := capturePathParams(routeTemplate, pathAndQuery)
	if !ok {
		return nil, fmt.Errorf("conformance: path %q does not match route template %q", pathAndQuery, routeTemplate)
	}

	out := make(Canonical, len(params)+len(body))
	for k, v := range params {
		out[k] = v
	}
	for k, v := range body {
		out[k] = v
	}

	if rawQuery != "" {
		values, err := url.ParseQuery(rawQuery)
		if err != nil {
			return nil, fmt.Errorf("conformance: parsing query %q: %w", rawQuery, err)
		}
		for k, vs := range values {
			if len(vs) == 0 {
				continue
			}
			out[k] = decodeArgValue(k, vs[0], numericQuery)
		}
	}
	return out, nil
}
