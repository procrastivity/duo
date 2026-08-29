package devin

import (
	"context"
	"fmt"

	"github.com/procrastivity/duo/internal/runtime"
)

// Correlate implements runtime.RuntimeCorrelator.
//
// §5.3: a transcript path or working directory cannot bind a runtime
// instance by itself. An empty ExternalAgentSessionID never binds.
//
// A matching instance plus a non-empty session id binds at
// ConfidenceInferred. Cwd is not required: after trust, Herdr reports
// kind=id (notes/60: hulking-ferry, lace-pegasus). This stage does not
// read sessions.db or invent an ATIF path; TranscriptID stays empty until
// Stage C.
//
// A ReporterCredential on the claim is an error: this adapter issues none,
// so a present credential is a claim about a different runtime instance
// (same shape as Claude when the instance has no credential configured).
func (r *Runtime) Correlate(_ context.Context, claim runtime.RuntimeClaim) (runtime.RuntimeCorrelationEvidence, error) {
	if claim.IntegrationInstanceID != r.integrationInstanceID {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
			"devin runtime %s: claim for integration instance %s",
			r.integrationInstanceID, claim.IntegrationInstanceID)
	}

	if claim.ReporterCredential != "" {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
			"devin runtime %s: reporter credential on claim does not match this runtime instance",
			r.integrationInstanceID)
	}

	if claim.ExternalAgentSessionID == "" {
		return runtime.RuntimeCorrelationEvidence{Bound: false}, nil
	}

	return runtime.RuntimeCorrelationEvidence{
		ExternalAgentSessionID: claim.ExternalAgentSessionID,
		TranscriptID:           "",
		Bound:                  true,
		Confidence:             ConfidenceInferred,
	}, nil
}
