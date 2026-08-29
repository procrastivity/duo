package devin_test

import (
	"context"
	"testing"

	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/devin"
)

const testIntegrationInstanceID = "devin"

func TestCorrelateEmptySessionIDDoesNotBind(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
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
		t.Fatal("expected Bound false: a working directory or path cannot bind without a session id")
	}
}

func TestCorrelateHostIDBindsWithoutCwd(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	ctx := context.Background()

	evidence, err := r.Correlate(ctx, runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "lace-pegasus",
	})
	if err != nil {
		t.Fatalf("Correlate: %v", err)
	}
	if !evidence.Bound {
		t.Fatal("expected Bound true for a host-named Devin session id")
	}
	if evidence.ExternalAgentSessionID != "lace-pegasus" {
		t.Fatalf("ExternalAgentSessionID = %q, want lace-pegasus", evidence.ExternalAgentSessionID)
	}
	if evidence.TranscriptID != "" {
		t.Fatalf("TranscriptID = %q, want empty (ATIF is Stage C)", evidence.TranscriptID)
	}
	if evidence.Confidence != devin.ConfidenceInferred {
		t.Fatalf("Confidence = %q, want %q", evidence.Confidence, devin.ConfidenceInferred)
	}
}

func TestCorrelateWrongIntegrationInstanceErrors(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	_, err := r.Correlate(context.Background(), runtime.RuntimeClaim{
		IntegrationInstanceID:  "some-other-integration",
		ExternalAgentSessionID: "lace-pegasus",
	})
	if err == nil {
		t.Fatal("expected an error for a claim addressed to a different integration instance")
	}
}

func TestCorrelateReporterCredentialErrors(t *testing.T) {
	r := devin.New(testIntegrationInstanceID)
	_, err := r.Correlate(context.Background(), runtime.RuntimeClaim{
		IntegrationInstanceID:  testIntegrationInstanceID,
		ExternalAgentSessionID: "lace-pegasus",
		ReporterCredential:     "x",
	})
	if err == nil {
		t.Fatal("expected an error when the claim carries a reporter credential this adapter does not issue")
	}
}
