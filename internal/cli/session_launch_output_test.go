package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/exitcode"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
)

// TestLaunchTextOutputGolden pins the whole human result of a launch, byte
// for byte.
//
// The `host:` line is the point of it. Under duo.config/v3 the session host
// is late-bound state, deduced once per launch, so a result that did not
// name the instance and the rung that produced it would leave an operator
// with no way to tell which server their session went to — which is exactly
// what duo-vnext-installation-contract.md §1.1 forbids ("the deduced
// instance and its host_source are explicit in launch output and in
// duo doctor"). The outranked row is the other half: it is what makes a
// stale correlation visible instead of silent.
func TestLaunchTextOutputGolden(t *testing.T) {
	out := &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: &bytes.Buffer{}}

	report := launch.Report{
		SessionID:          "ses_01",
		LaunchResolutionID: "lrr_01",
		Selection:          "ordered",
		Host: &launch.WireHost{
			Kind:          "herdr",
			InstanceLabel: "/run/user/1000/herdr/sessions/alpha/herdr.sock",
			HostSource:    string(domain.HostSourceWorkspaceCorrelation),
			WorkspaceID:   "ws_01",
			OutrankedEvidence: []launch.WireOutranked{{
				Source:        string(domain.HostSourceAmbientEnv),
				Kind:          "herdr",
				InstanceLabel: "/run/user/1000/herdr/sessions/other/herdr.sock",
				Detail:        "outranked by workspace-correlation",
				Captures: []materialize.WireCapture{{
					Name:  "HERDR_SOCKET_PATH",
					Value: "/run/user/1000/herdr/sessions/other/herdr.sock",
				}},
			}},
		},
		Leaves: []launch.ReportLeaf{{
			Name:         "primary",
			AgentRuntime: "claude",
			ModelLine:    "claude-opus-4",
			ModelFamily:  "claude",
			DeclaredKind: "determined",
			Outcome:      "selected",
		}},
	}

	if err := renderLaunchReportText(streams, report); err != nil {
		t.Fatalf("renderLaunchReportText: %v", err)
	}

	const want = "session:  ses_01\n" +
		"record:   lrr_01\n" +
		"host:      herdr:/run/user/1000/herdr/sessions/alpha/herdr.sock (host_source=workspace-correlation)\n" +
		"  outranked ambient-env:           herdr:/run/user/1000/herdr/sessions/other/herdr.sock (outranked by workspace-correlation)\n" +
		"selection: ordered\n" +
		"  primary: claude / claude-opus-4 (determined) -> selected\n"
	if got := out.String(); got != want {
		t.Errorf("launch text output\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestDryRunTextOutputNamesTheDeduction covers §6.10's preview: it writes
// nothing durable, and it still shows the deduction — a preview that hid the
// host would be previewing the wrong half of the decision.
func TestDryRunTextOutputNamesTheDeduction(t *testing.T) {
	out := &bytes.Buffer{}
	streams := &iostreams.Streams{Out: out, Err: &bytes.Buffer{}}

	err := renderLaunchReportText(streams, launch.Report{
		Preview:   true,
		Selection: "ordered",
		Host: &launch.WireHost{
			Kind:              "herdr",
			InstanceLabel:     bindSocket,
			HostSource:        string(domain.HostSourceExplicitFlag),
			OutrankedEvidence: []launch.WireOutranked{},
		},
	})
	if err != nil {
		t.Fatalf("renderLaunchReportText: %v", err)
	}

	const want = "preview only: no session, no durable record, no spawn\n" +
		"host:      herdr:/run/user/1000/herdr/sessions/alpha/herdr.sock (host_source=explicit-flag)\n" +
		"selection: ordered\n"
	if got := out.String(); got != want {
		t.Errorf("dry-run text output\n got:\n%s\nwant:\n%s", got, want)
	}
}

// TestExhaustionNamesTheHostAndThePointerSet is the failure half of the
// rail. `details.pointers` is duo-external-v1's closed `launch_pointer_set`,
// and the human output has to carry it verbatim: an operator who reads the
// text mode and one who reads the --output json envelope must be told to type the
// same things (duo-vnext-projection-contracts.md §2.1's launch-verb block).
func TestExhaustionNamesTheHostAndThePointerSet(t *testing.T) {
	h := newBindHarness(t, nil)

	// Bind the workspace first, so the deduction lands on the correlation
	// rung — the one case where rebinding is a genuine way out, and so the
	// one where the pointer set carries the audited verb.
	mat := h.materializeWith("herdr:"+bindSocket, nil)
	if _, err := h.launch(mat, spawningHosts{}, false); err != nil {
		t.Fatalf("seeding launch: %v", err)
	}
	h.err.Reset()

	bound := h.materializeWith("", ambientEnv())
	if bound.Host().Source != domain.HostSourceWorkspaceCorrelation {
		t.Fatalf("host_source = %q, want %q", bound.Host().Source, domain.HostSourceWorkspaceCorrelation)
	}
	resolver, err := launch.NewResolver(h.doc, bound, launch.Options{
		Support: launch.AllSupported{RecordDigest: "sha256:test"},
	})
	if err != nil {
		t.Fatalf("NewResolver: %v", err)
	}

	// One require no declared candidate can satisfy: the only variant is a
	// claude one.
	_, resolveErr := resolver.Resolve(launch.Request{
		Preset:    "daily",
		Require:   []launch.Constraint{{Axis: launch.AxisAgentRuntime, Value: "codex", Source: "flag"}},
		RequestID: "req_test",
	})
	if resolveErr == nil {
		t.Fatal("resolution succeeded, want an exhaustion")
	}

	de := launchFailure(h.streams, "text", resolveErr)
	if de.Code != launch.CodeConstraintsExhausted {
		t.Fatalf("code = %q, want %q", de.Code, launch.CodeConstraintsExhausted)
	}
	if exitcode.FromError(de) == exitcode.Success {
		t.Error("an exhaustion exits zero")
	}

	rail := h.err.String()
	for _, want := range []string{
		"host:",
		bindSocket,
		string(domain.HostSourceWorkspaceCorrelation),
		"ways out:",
		"--host",
		"duo workspace host rebind",
	} {
		if !strings.Contains(rail, want) {
			t.Errorf("the failure rail does not name %q:\n%s", want, rail)
		}
	}

	// The same pointer set rides the JSON envelope instead, so --output
	// json is never told less than the human mode.
	if de.Details == nil {
		t.Fatal("the projected error carries no safe details for --output json")
	}
	var decoded failureRail
	encodeDecode(t, de.Details, &decoded)
	if decoded.Pointers == nil || decoded.Pointers.WorkspaceHostRebind != "duo workspace host rebind" {
		t.Errorf("details.pointers = %+v, want the audited rebind verb", decoded.Pointers)
	}
	if decoded.Host.HostSource != string(domain.HostSourceWorkspaceCorrelation) {
		t.Errorf("details.host.host_source = %q, want %q",
			decoded.Host.HostSource, domain.HostSourceWorkspaceCorrelation)
	}
}

// TestHostUnresolvedIsProjectedWithItsOwnCode covers the other failure this
// step's wiring can raise: no rung produced a host at all. It must reach the
// operator as materialization's own registered code with its trail, never as
// an internal failure.
func TestHostUnresolvedIsProjectedWithItsOwnCode(t *testing.T) {
	h := newBindHarness(t, nil)

	_, err := materialize.Materialize(context.Background(), materialize.Options{
		WorkspaceFlag:   h.root,
		RequestedPreset: "daily",
		Policy:          h.doc.SessionHosts,
		Correlations:    h.authority,
		Providers:       h.authority,
		Discovery:       stage1Discovery{}, // an isolated XDG_CONFIG_HOME: no herdr sessions
		LookupEnv:       func(string) (string, bool) { return "", false },
	})
	if err == nil {
		t.Fatal("materialization succeeded, want launch.host_unresolved")
	}

	de := launchFailure(h.streams, "text", err)
	if de.Code != materialize.CodeHostUnresolved {
		t.Fatalf("code = %q, want %q", de.Code, materialize.CodeHostUnresolved)
	}
	if !strings.Contains(h.err.String(), "--host") {
		t.Errorf("the failure rail does not offer the override flag:\n%s", h.err.String())
	}
}

// TestMalformedHostFlagStaysACallerError pins that a --host grammar error
// keeps its invalid.request code through the projection: it is the caller's
// to fix, and burying it under internal.launch_failed would tell them to
// file a bug instead.
func TestMalformedHostFlagStaysACallerError(t *testing.T) {
	_, err := materialize.ParseHostFlag(":no-kind")
	if err == nil {
		t.Fatal("ParseHostFlag accepted a value naming no kind")
	}
	if got := launchDuoErr(err).Code; got != "invalid.request" {
		t.Errorf("code = %q, want %q", got, "invalid.request")
	}
}

// encodeDecode round-trips a safe detail payload through its wire encoding,
// which is how this package's own renderer reads it: the launch and
// materialization detail shapes are package-private Go types that agree only
// on their duo-external-v1 members, so the encoding is the contract.
func encodeDecode(t *testing.T, from any, into any) {
	t.Helper()
	raw, err := json.Marshal(from)
	if err != nil {
		t.Fatalf("marshaling details: %v", err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		t.Fatalf("decoding details: %v", err)
	}
}
