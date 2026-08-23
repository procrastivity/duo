package pi_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

const (
	injectedCwd  = "/tmp/claude-1000/-home-dev-Code-terminal-multiplexers/a227c14a-f14b-42e3-9b13-dc02b20f7bed/scratchpad/pi-probe/work2"
	fixtureToken = "duo-reporter-token-fixture"
)

// loadReportedClaim reads the recorded reporter line and turns it into a
// §5.3 claim, which is the shape Correlate is contracted on.
func loadReportedClaim(t *testing.T) runtimepi.ReportedClaim {
	t.Helper()
	line, err := os.ReadFile("testdata/reported-claim-tui.json")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	claim, err := runtimepi.DecodeReportedClaim(line)
	if err != nil {
		t.Fatalf("DecodeReportedClaim: %v", err)
	}
	if err := claim.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return claim
}

// seedSessions materializes a sessions tree holding one transcript, under the
// slug pi derives from cwd.
func seedSessions(t *testing.T, cwd, fixture string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, runtimepi.SessionSlug(cwd))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, filepath.Base(fixture)), data, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return root
}

func TestSessionSlug(t *testing.T) {
	// Left column: cwd. Right column: the directory name observed under
	// ~/.pi/agent/sessions on a machine running 0.83.0 (notes/06-pi.md,
	// notes/18 §probe hygiene).
	cases := map[string]string{
		"/home/dev":           "--home-dev--",
		"/home/dev/Code/wip":  "--home-dev-Code-wip--",
		"/home/dev/.setup":    "--home-dev-.setup--",
		"/tmp":                "--tmp--",
		"/home/dev/Code/wip/": "--home-dev-Code-wip--",
		injectedCwd: "--tmp-claude-1000--home-dev-Code-terminal-multiplexers-" +
			"a227c14a-f14b-42e3-9b13-dc02b20f7bed-scratchpad-pi-probe-work2--",
	}
	for cwd, want := range cases {
		if got := runtimepi.SessionSlug(cwd); got != want {
			t.Errorf("SessionSlug(%q) = %q, want %q", cwd, got, want)
		}
	}
}

func TestFactoryDescriptor(t *testing.T) {
	d := runtimepi.Factory{IntegrationInstanceID: "integration-1"}.Descriptor()
	if d.AdapterID != "pi" || d.Role != adapter.RoleRuntime {
		t.Fatalf("descriptor = %+v, want adapter id pi in the runtime role", d)
	}
	if len(d.SupportedExternalVersions) != 1 || d.SupportedExternalVersions[0] != runtimepi.PinnedExternalVersion {
		t.Errorf("SupportedExternalVersions = %v, want the pinned %s",
			d.SupportedExternalVersions, runtimepi.PinnedExternalVersion)
	}
	if d.ConformanceRecordDigest == "" || d.DiagnosticRedactionPolicy == "" {
		t.Errorf("descriptor must name a conformance record and a redaction policy: %+v", d)
	}
}

// Probe shells out to the pi binary, so the test supplies its own: no
// assertion here depends on pi being installed on the machine running it.
func TestFactoryProbe(t *testing.T) {
	ctx := context.Background()

	absent, err := runtimepi.Factory{Binary: "pi-not-installed-for-this-test"}.Probe(ctx)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if absent.Compatibility != adapter.CompatibilityUnavailable {
		t.Errorf("missing binary: Compatibility = %s, want unavailable", absent.Compatibility)
	}

	for _, tc := range []struct {
		version string
		want    adapter.CompatibilityState
	}{
		{runtimepi.PinnedExternalVersion, adapter.CompatibilitySupported},
		{"0.84.0", adapter.CompatibilityUnverified},
	} {
		bin := filepath.Join(t.TempDir(), "pi")
		script := "#!/bin/sh\necho " + tc.version + "\n"
		if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		probe, err := runtimepi.Factory{Binary: bin}.Probe(ctx)
		if err != nil {
			t.Fatalf("Probe: %v", err)
		}
		if probe.DetectedVersion != tc.version || probe.Compatibility != tc.want {
			t.Errorf("probe of %s = {%s %s}, want {%s %s}",
				tc.version, probe.DetectedVersion, probe.Compatibility, tc.version, tc.want)
		}
		if probe.ProtocolOrFormatIdentity != runtimepi.TranscriptFormatIdentity {
			t.Errorf("ProtocolOrFormatIdentity = %q, want %q",
				probe.ProtocolOrFormatIdentity, runtimepi.TranscriptFormatIdentity)
		}

		r, err := runtimepi.Factory{IntegrationInstanceID: "integration-1"}.New(ctx, probe)
		if err != nil || r == nil {
			t.Fatalf("New: %v (runtime %v)", err, r)
		}
	}
}

// The extension-reported claim is the evidence Correlate is built for: a
// session id and session file read from live ctx accessors inside the agent
// process, presented with the credential Duo issued to that instance.
func TestCorrelateBindsReportedClaim(t *testing.T) {
	reported := loadReportedClaim(t)
	root := seedSessions(t, injectedCwd, injectedFixture)
	r := runtimepi.New("integration-1",
		runtimepi.WithSessionsRoot(root),
		runtimepi.WithReporterCredential(fixtureToken))

	claim := reported.Claim("integration-1")
	// The recorded reporter line names the probe machine's session file. Point
	// it somewhere that certainly does not exist, so this exercises the
	// resolver's fallback from a stale reported path to the sessions tree —
	// and so the test does not depend on what is on the machine running it.
	claim.TranscriptPath = filepath.Join(t.TempDir(), filepath.Base(reported.SessionFile))

	evidence, err := r.Correlate(context.Background(), claim)
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound || evidence.Confidence != runtimepi.ConfidenceExtensionExact {
		t.Fatalf("evidence = %+v, want a bound %s", evidence, runtimepi.ConfidenceExtensionExact)
	}
	if evidence.ExternalAgentSessionID != injectedSession {
		t.Errorf("ExternalAgentSessionID = %s, want %s", evidence.ExternalAgentSessionID, injectedSession)
	}
	if !strings.HasPrefix(evidence.TranscriptID, root) ||
		filepath.Base(evidence.TranscriptID) != filepath.Base(injectedFixture) {
		t.Fatalf("TranscriptID = %q, want the transcript resolved under %s", evidence.TranscriptID, root)
	}

	// The correlation is usable: its TranscriptID reads back as turns.
	batch, err := r.ReadConversation(context.Background(), runtime.ConversationReadRequest{
		ExternalAgentSessionID: evidence.ExternalAgentSessionID,
		TranscriptID:           evidence.TranscriptID,
	})
	if err != nil {
		t.Fatalf("ReadConversation through the correlation: %v", err)
	}
	if len(batch.Turns) == 0 {
		t.Errorf("correlated transcript read back empty")
	}
}

// §5.3: "A transcript path or working directory cannot bind a runtime
// instance." On Pi the rule has teeth — the cwd slug is shared by every
// session ever run in that directory, and it is lossy besides.
func TestCorrelateRefusesPathOrCwdOnlyClaim(t *testing.T) {
	r := runtimepi.New("integration-1", runtimepi.WithReporterCredential(fixtureToken))

	for name, claim := range map[string]runtime.RuntimeClaim{
		"cwd only": {
			IntegrationInstanceID: "integration-1",
			WorkingDirectory:      injectedCwd,
			ReporterCredential:    fixtureToken,
		},
		"transcript path only": {
			IntegrationInstanceID: "integration-1",
			TranscriptPath:        injectedFixture,
			ReporterCredential:    fixtureToken,
		},
		"path and cwd": {
			IntegrationInstanceID: "integration-1",
			TranscriptPath:        injectedFixture,
			WorkingDirectory:      injectedCwd,
			ReporterCredential:    fixtureToken,
		},
	} {
		evidence, err := r.Correlate(context.Background(), claim)
		if err != nil {
			t.Fatalf("%s: Correlate: %v", name, err)
		}
		if evidence.Bound {
			t.Errorf("%s: bound without an external agent-session id", name)
		}
	}
}

func TestCorrelateRefusesWrongOrMissingCredential(t *testing.T) {
	reported := loadReportedClaim(t)
	root := seedSessions(t, injectedCwd, injectedFixture)
	r := runtimepi.New("integration-1",
		runtimepi.WithSessionsRoot(root),
		runtimepi.WithReporterCredential("the-real-credential"))

	// A claim carrying another instance's token is not an error: it is a
	// claim this instance cannot bind.
	evidence, err := r.Correlate(context.Background(), reported.Claim("integration-1"))
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if evidence.Bound {
		t.Errorf("bound a claim carrying the wrong reporter credential")
	}

	bare := reported.Claim("integration-1")
	bare.ReporterCredential = ""
	evidence, err = r.Correlate(context.Background(), bare)
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if evidence.Bound {
		t.Errorf("bound a credential-less claim while a credential was issued")
	}
}

// Without a credential of its own — a pi Duo did not spawn — the adapter
// still binds, at the weaker transcript-channel confidence. The label is the
// evidence grade; callers that need authentication read it.
func TestCorrelateHeuristicWithoutIssuedCredential(t *testing.T) {
	root := seedSessions(t, injectedCwd, injectedFixture)
	r := runtimepi.New("integration-1", runtimepi.WithSessionsRoot(root))

	evidence, err := r.Correlate(context.Background(), runtime.RuntimeClaim{
		IntegrationInstanceID:  "integration-1",
		ExternalAgentSessionID: injectedSession,
		WorkingDirectory:       injectedCwd,
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound || evidence.Confidence != runtimepi.ConfidenceTranscriptHeuristic {
		t.Fatalf("evidence = %+v, want a bound %s", evidence, runtimepi.ConfidenceTranscriptHeuristic)
	}
}

// A transcript path whose file name names a different session is
// contradictory evidence, not a hint to ignore.
func TestCorrelateRefusesDisagreeingTranscriptPath(t *testing.T) {
	root := seedSessions(t, injectedCwd, injectedFixture)
	r := runtimepi.New("integration-1", runtimepi.WithSessionsRoot(root))

	evidence, err := r.Correlate(context.Background(), runtime.RuntimeClaim{
		IntegrationInstanceID:  "integration-1",
		ExternalAgentSessionID: injectedSession,
		TranscriptPath:         basicFixture,
		WorkingDirectory:       injectedCwd,
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if evidence.Bound {
		t.Errorf("bound a claim whose transcript path names session %s", basicSession)
	}
}

// A claim for another integration instance is the one case §5.3 makes an
// error rather than an unbound result: the call could not even be attempted.
func TestCorrelateWrongIntegrationInstanceErrors(t *testing.T) {
	r := runtimepi.New("integration-1")
	_, err := r.Correlate(context.Background(), runtime.RuntimeClaim{
		IntegrationInstanceID:  "integration-2",
		ExternalAgentSessionID: injectedSession,
	})
	if err == nil {
		t.Fatalf("expected an error for a claim addressed to another integration instance")
	}
}

// A bound correlation with no transcript on disk is legitimate: `pi
// --no-session` writes none, and a just-started session may not have one yet.
func TestCorrelateBindsWithoutALocatableTranscript(t *testing.T) {
	r := runtimepi.New("integration-1",
		runtimepi.WithSessionsRoot(t.TempDir()),
		runtimepi.WithReporterCredential(fixtureToken))

	evidence, err := r.Correlate(context.Background(), runtime.RuntimeClaim{
		IntegrationInstanceID:  "integration-1",
		ExternalAgentSessionID: injectedSession,
		WorkingDirectory:       injectedCwd,
		ReporterCredential:     fixtureToken,
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatalf("a located runtime instance with no transcript file must still bind")
	}
	if evidence.TranscriptID != "" {
		t.Errorf("TranscriptID = %q, want empty when no transcript exists", evidence.TranscriptID)
	}
}

func TestReportedClaimValidation(t *testing.T) {
	reported := loadReportedClaim(t)

	rpc := reported
	rpc.Mode = "rpc"
	rpc.HasUI = true // exactly what 0.83.0 reports in rpc mode
	if err := rpc.Validate(); err == nil {
		t.Errorf("an rpc-mode claim must be refused even with hasUI true: that is the whole finding")
	}

	stale := reported
	stale.Protocol = "duo.pi.reporter/v0"
	if err := stale.Validate(); err == nil {
		t.Errorf("a claim tagged with another protocol version must be refused")
	}

	anonymous := reported
	anonymous.SessionID = ""
	if err := anonymous.Validate(); err == nil {
		t.Errorf("a claim with no session id must be refused")
	}
}

// The decoder is strict so a rendered extension from a newer build cannot be
// half-read by an older binary: the protocol tag is meant to catch that, and
// an unknown field means the tag was not bumped.
func TestDecodeReportedClaimRejectsUnknownFields(t *testing.T) {
	line := `{"protocol":"` + runtimepi.ReporterProtocol + `","sessionId":"x","mode":"tui","futureField":1}`
	if _, err := runtimepi.DecodeReportedClaim([]byte(line)); err == nil ||
		!strings.Contains(err.Error(), "futureField") {
		t.Fatalf("err = %v, want a refusal naming the unknown field", err)
	}
}
