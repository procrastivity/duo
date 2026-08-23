package conformance

import (
	"testing"

	"github.com/procrastivity/duo/contracts"
)

// TestFixtureRoundTrip is the step's core requirement: every applicable
// fixture decodes and round-trips with canonical equality across the CLI,
// MCP, and presentation projections; every fixture this harness cannot
// project is skipped with an explicit, printed reason (t.Skip, never a
// silent continue) — run `go test -v ./internal/conformance/...` to see
// every case and, for the skipped ones, why.
func TestFixtureRoundTrip(t *testing.T) {
	cases, err := LoadFixtureCases(contracts.FS)
	if err != nil {
		t.Fatalf("loading fixture cases: %v", err)
	}

	var applicable, skipped int
	for _, c := range cases {
		t.Run(c.Path, func(t *testing.T) {
			if c.Skipped() {
				skipped++
				t.Skipf("skipped: %s", c.Reason)
				return
			}
			applicable++
			roundTripAllProjections(t, c.Data)
		})
	}

	t.Logf("fixture round trip: %d applicable, %d skipped, %d total", applicable, skipped, len(cases))
	if applicable == 0 {
		t.Fatal("no fixture was applicable; the classifier or the registry table regressed")
	}
}

// roundTripAllProjections encodes v through each projection's encoder,
// decodes it back, and asserts every decode equals v and equals every other
// projection's decode. This is the "VALUE half" the step asks for: the
// canonical (domain) value, not the registry's CLI-path/MCP-tool/route
// metadata (internal/registry's TestProjectionCasesMatchRegistry covers
// that half already).
func roundTripAllProjections(t *testing.T, v Canonical) {
	t.Helper()

	cliGot, err := DecodeCLI(EncodeCLI(v))
	if err != nil {
		t.Fatalf("CLI round trip: %v", err)
	}
	if !equalCanonical(cliGot, v) {
		t.Errorf("CLI round trip changed the canonical value:\n got:  %#v\n want: %#v", cliGot, v)
	}

	mcpGot, err := DecodeMCP(EncodeMCP(v))
	if err != nil {
		t.Fatalf("MCP round trip: %v", err)
	}
	if !equalCanonical(mcpGot, v) {
		t.Errorf("MCP round trip changed the canonical value:\n got:  %#v\n want: %#v", mcpGot, v)
	}

	var presGot Canonical
	if _, isStreamItem := v["stream"]; isStreamItem {
		presGot, err = DecodeSSE(EncodeSSE(v))
	} else {
		presGot, err = DecodeHTTPBody(EncodeHTTPBody(v))
	}
	if err != nil {
		t.Fatalf("presentation round trip: %v", err)
	}
	if !equalCanonical(presGot, v) {
		t.Errorf("presentation round trip changed the canonical value:\n got:  %#v\n want: %#v", presGot, v)
	}

	// The point of the exercise: all three projections, decoded
	// independently, land on the identical canonical value.
	if !equalCanonical(cliGot, mcpGot) {
		t.Errorf("CLI and MCP decoded to different canonical values:\n CLI: %#v\n MCP: %#v", cliGot, mcpGot)
	}
	if !equalCanonical(cliGot, presGot) {
		t.Errorf("CLI and presentation decoded to different canonical values:\n CLI:          %#v\n presentation: %#v", cliGot, presGot)
	}
}
