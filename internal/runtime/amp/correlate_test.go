package amp_test

import (
	"context"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/amp"
)

const testIntegrationInstanceID = "amp"

func TestCorrelateEmptyThreadIDDoesNotBind(t *testing.T) {
	r := amp.New(testIntegrationInstanceID)
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID: testIntegrationInstanceID,
		WorkingDirectory:      "/home/dev/Code/duo",
		TranscriptPath:        "/tmp/would-be-wrong.jsonl",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if evidence.Bound {
		t.Fatal("expected Bound false: a working directory or path cannot bind without a thread id")
	}
}

func TestCorrelateThreadIDBindsWithoutCwd(t *testing.T) {
	r := amp.New(testIntegrationInstanceID)
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "T-loop-c-ok",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatal("expected Bound true for an Amp thread id")
	}
	if evidence.ExternalAgentSessionID != "T-loop-c-ok" {
		t.Fatalf("ExternalAgentSessionID = %q, want T-loop-c-ok", evidence.ExternalAgentSessionID)
	}
	if evidence.TranscriptID != "" {
		t.Fatalf("TranscriptID = %q, want empty (Amp reads are server-resident, no local document)", evidence.TranscriptID)
	}
	if evidence.Confidence != amp.ConfidenceInferred {
		t.Fatalf("Confidence = %q, want %q", evidence.Confidence, amp.ConfidenceInferred)
	}
}

func TestCorrelateWrongIntegrationInstanceErrors(t *testing.T) {
	r := amp.New(testIntegrationInstanceID)
	_, err := r.Correlate(context.Background(), runtime.RuntimeClaim{
		IntegrationInstanceID:  "some-other-integration",
		ExternalAgentSessionID: "T-loop-c-ok",
	})
	if err == nil {
		t.Fatal("expected an error for a claim addressed to a different integration instance")
	}
}

func TestCorrelateReporterCredentialErrors(t *testing.T) {
	r := amp.New(testIntegrationInstanceID)
	_, err := r.Correlate(context.Background(), runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "T-loop-c-ok",
		ReporterCredential:     "x",
	})
	if err == nil {
		t.Fatal("expected an error when the claim carries a reporter credential this adapter does not issue")
	}
}
