package claude_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/claude"
)

const testIntegrationInstanceID = "integration-1"

func newTestRuntime(t *testing.T, reporterCredential string) *claude.Runtime {
	t.Helper()
	r, err := claude.New(testIntegrationInstanceID, reporterCredential, "testdata")
	if err != nil {
		t.Fatalf("claude.New: %v", err)
	}
	return r
}

// TestCorrelateHookClaimBindsAuthoritative is one of the three tests the
// Step 18 spec names explicitly: "hook claim binds."
func TestCorrelateHookClaimBindsAuthoritative(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "session-from-hook",
		TranscriptPath:         "/home/user/.claude/projects/-home-user-work/session-from-hook.jsonl",
		ReporterCredential:     "duo-reporter-secret",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatalf("expected Bound true for a matching hook claim")
	}
	if evidence.Confidence != claude.ConfidenceAuthoritative {
		t.Fatalf("Confidence = %q, want %q", evidence.Confidence, claude.ConfidenceAuthoritative)
	}
	if evidence.ExternalAgentSessionID != "session-from-hook" {
		t.Fatalf("ExternalAgentSessionID = %q, want session-from-hook", evidence.ExternalAgentSessionID)
	}
	if evidence.TranscriptID != "/home/user/.claude/projects/-home-user-work/session-from-hook.jsonl" {
		t.Fatalf("TranscriptID = %q, want the claim's TranscriptPath trusted verbatim", evidence.TranscriptID)
	}
}

func TestCorrelateWrongReporterCredentialErrors(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()

	_, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "session-from-hook",
		ReporterCredential:     "not-the-right-secret",
	})
	if err == nil {
		t.Fatalf("expected an error for a reporter credential that does not match this runtime instance")
	}
}

func TestCorrelateCredentialOnClaimButNoneConfiguredErrors(t *testing.T) {
	// A runtime instance with no configured ReporterCredential (Factory
	// left it empty) can never authenticate a hook claim, however
	// plausible the claim's credential looks.
	r := newTestRuntime(t, "")
	ctx := context.Background()

	_, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "session-from-hook",
		ReporterCredential:     "anything",
	})
	if err == nil {
		t.Fatalf("expected an error when this runtime instance has no reporter credential configured")
	}
}

func TestCorrelateWrongIntegrationInstanceErrors(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()

	_, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  "some-other-integration",
		ExternalAgentSessionID: "session-from-hook",
		ReporterCredential:     "duo-reporter-secret",
	})
	if err == nil {
		t.Fatalf("expected an error for a claim addressed to a different integration instance")
	}
}

// TestCorrelateRegistryOnlyClaimCapsAtInferred is the second of the three
// spec-named tests: "registry-only claim caps at inferred." The registry
// fixture (testdata/sessions/1067086.json) is notes/16 §6's own 2.1.240
// capture.
func TestCorrelateRegistryOnlyClaimCapsAtInferred(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret") // even with a credential configured, an uncredentialed claim never reaches authoritative.
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "89dc3e6a-1609-44ed-8515-64dbef6f3726", // sessionId in the registry fixture.
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatalf("expected Bound true: the session id is present in the registry fixture")
	}
	if evidence.Confidence != claude.ConfidenceInferred {
		t.Fatalf("Confidence = %q, want %q (registry evidence never outranks a hook claim)", evidence.Confidence, claude.ConfidenceInferred)
	}
}

func TestCorrelateRegistryMissLeavesClaimUnbound(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "no-such-session-anywhere",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if evidence.Bound {
		t.Fatalf("expected Bound false: the session id is neither hook-credentialed nor in the registry")
	}
}

// TestCorrelatePathOrCWDOnlyClaimRefuses is the third spec-named test:
// "path/cwd-only claim refuses." §5.3: "A transcript path or working
// directory cannot bind a runtime instance" by itself.
func TestCorrelatePathOrCWDOnlyClaimRefuses(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()

	cases := []runtime.RuntimeClaim{
		{IntegrationInstanceID: testIntegrationInstanceID, WorkingDirectory: "/home/user/work"},
		{IntegrationInstanceID: testIntegrationInstanceID, TranscriptPath: "/home/user/.claude/projects/-home-user-work/some-session.jsonl"},
		{IntegrationInstanceID: testIntegrationInstanceID, WorkingDirectory: "/home/user/work", TranscriptPath: "/home/user/.claude/projects/-home-user-work/some-session.jsonl"},
	}
	for _, claim := range cases {
		evidence, err := r.Correlate(ctx, claim)
		if err != nil {
			t.Fatalf("Correlate(%+v): %v", claim, err)
		}
		if evidence.Bound {
			t.Fatalf("Correlate(%+v): expected Bound false with no ExternalAgentSessionID", claim)
		}
	}
}

func TestCorrelateResolvesTranscriptIDFromWorkingDirectory(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "session-abc",
		WorkingDirectory:       "/home/user/Code/terminal-multiplexers",
		ReporterCredential:     "duo-reporter-secret",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	want := filepath.Join("testdata", "projects", "-home-user-Code-terminal-multiplexers", "session-abc.jsonl")
	if evidence.TranscriptID != want {
		t.Fatalf("TranscriptID = %q, want %q (slug: / and . become -)", evidence.TranscriptID, want)
	}
}

func TestCorrelateHostNamedIDWithWorkingDirectoryBindsInferred(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "host-named-session",
		WorkingDirectory:       "/tmp/duo-dlb09-ws",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatal("expected Bound true: host-named id plus working directory is inferred even when the registry misses")
	}
	if evidence.Confidence != claude.ConfidenceInferred {
		t.Fatalf("Confidence = %q, want %q", evidence.Confidence, claude.ConfidenceInferred)
	}
	want := filepath.Join("testdata", "projects", "-tmp-duo-dlb09-ws", "host-named-session.jsonl")
	if evidence.TranscriptID != want {
		t.Fatalf("TranscriptID = %q, want slug path %q", evidence.TranscriptID, want)
	}
}

func TestCorrelateHostNamedIDWithTranscriptPathBindsInferred(t *testing.T) {
	r := newTestRuntime(t, "duo-reporter-secret")
	ctx := context.Background()

	path := "/tmp/pi/2026-08-26_sess.jsonl"
	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "host-named-session",
		TranscriptPath:         path,
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatal("expected Bound true: host-named id plus TranscriptPath is inferred even when the registry misses")
	}
	if evidence.Confidence != claude.ConfidenceInferred {
		t.Fatalf("Confidence = %q, want %q", evidence.Confidence, claude.ConfidenceInferred)
	}
	if evidence.TranscriptID != path {
		t.Fatalf("TranscriptID = %q, want the named path", evidence.TranscriptID)
	}
}
