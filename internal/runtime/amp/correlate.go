package amp

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
// A matching instance plus a non-empty thread id (this adapter's session
// identity kind, ThreadIDFormatIdentity) binds at ConfidenceInferred.
// TranscriptID always stays empty: Amp keeps no local transcript
// document this adapter can name a path for — Amp reads are
// server-resident, and every read needs the account credential and bills
// the account the same way a write does. Correlate never dials Amp: it
// only classifies the claim already in hand, exactly like Devin's
// Correlate does not read sessions.db or invent an ATIF path.
//
// A ReporterCredential on the claim is an error: this adapter issues
// none, so a present credential is a claim about a different runtime
// instance (same shape as Devin and Claude when the instance has no
// credential configured).
func (r *Runtime) Correlate(_ context.Context, claim runtime.RuntimeClaim) (runtime.RuntimeCorrelationEvidence, error) {
	if claim.IntegrationInstanceID != r.integrationInstanceID {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
			"amp runtime %s: claim for integration instance %s",
			r.integrationInstanceID, claim.IntegrationInstanceID)
	}

	if claim.ReporterCredential != "" {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
			"amp runtime %s: reporter credential on claim does not match this runtime instance",
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
