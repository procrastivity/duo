package claude

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/procrastivity/duo/internal/runtime"
)

// The two confidence labels this adapter ever returns, named after
// review/05-close-report.md's own vocabulary ("hooks/transcript =
// authoritative; registry = inferred"). Confidence's contract
// (runtime.RuntimeCorrelationEvidence) is a per-adapter-family string; a
// third value never appears here, and Inferred never outranks
// Authoritative — Correlate enforces that by construction (only the
// hook-credential branch can return Authoritative), not by a runtime
// comparison, so there is nothing here for a caller to get backwards.
const (
	ConfidenceAuthoritative = "authoritative"
	ConfidenceInferred      = "inferred"
)

// Correlate implements runtime.RuntimeCorrelator.
//
// §5.3's rule is absolute: "A transcript path or working directory cannot
// bind a runtime instance" by itself. This adapter enforces that as the
// very first evidence check, before anything else, on every call: a claim
// with no ExternalAgentSessionID never binds, no matter what
// TranscriptPath or WorkingDirectory it carries.
//
// Above that floor, three evidence channels can bind a claim that does
// carry an ExternalAgentSessionID:
//
//  1. A claim whose ReporterCredential matches this instance's own
//     (notes/16 §10: only a generated hook belonging to this exact
//     instance ever carries it, via launch-env passthrough) binds at
//     ConfidenceAuthoritative — hooks and transcripts are the
//     authoritative channel per the close report. Credential is the
//     only upgrade to authoritative.
//  2. Absent that, a claim whose ExternalAgentSessionID appears in the
//     optional `~/.claude/sessions/<pid>.json` registry
//     (notes/16 §6: undocumented, version-fragile, present only for a
//     live interactive or -p process) binds at ConfidenceInferred.
//  3. Also inferred: a host-named ExternalAgentSessionID plus a
//     WorkingDirectory or TranscriptPath, even when the registry has
//     not listed the id. Post-launch bind fills WorkingDirectory from
//     the workspace root so conversation.list can open the slug path
//     `~/.claude/projects/<slug>/<id>.jsonl` without waiting on the
//     registry or installing reporter hooks. Registry and locator are
//     the same inferred grade; neither outranks the other.
//
// A ReporterCredential that is present but wrong is not "insufficient
// evidence" — it is a definite claim about a different runtime instance,
// so Correlate treats it the same way it treats a mismatched
// IntegrationInstanceID: an error, not a false Bound.
func (r *Runtime) Correlate(_ context.Context, claim runtime.RuntimeClaim) (runtime.RuntimeCorrelationEvidence, error) {
	if claim.IntegrationInstanceID != r.integrationInstanceID {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
			"claude runtime %s: claim for integration instance %s",
			r.integrationInstanceID, claim.IntegrationInstanceID)
	}

	if claim.ExternalAgentSessionID == "" {
		return runtime.RuntimeCorrelationEvidence{Bound: false}, nil
	}

	if claim.ReporterCredential != "" {
		if r.reporterCredential == "" || claim.ReporterCredential != r.reporterCredential {
			return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
				"claude runtime %s: reporter credential on claim does not match this runtime instance",
				r.integrationInstanceID)
		}
		return runtime.RuntimeCorrelationEvidence{
			ExternalAgentSessionID: claim.ExternalAgentSessionID,
			TranscriptID:           r.resolveTranscriptID(claim),
			Bound:                  true,
			Confidence:             ConfidenceAuthoritative,
		}, nil
	}

	// registryHasSession already folds "registry directory absent" into
	// (false, nil) — the registry is optional evidence. Anything it does
	// surface as an error here is unexpected (e.g. a permission error);
	// this adapter degrades to unbound rather than fail Correlate over an
	// optional, best-effort source, since a registry read failure is not
	// the same claim as "no registry evidence exists."
	found, _ := r.registryHasSession(claim.ExternalAgentSessionID)
	locator := claim.WorkingDirectory != "" || claim.TranscriptPath != ""
	if !found && !locator {
		return runtime.RuntimeCorrelationEvidence{Bound: false}, nil
	}

	return runtime.RuntimeCorrelationEvidence{
		ExternalAgentSessionID: claim.ExternalAgentSessionID,
		TranscriptID:           r.resolveTranscriptID(claim),
		Bound:                  true,
		Confidence:             ConfidenceInferred,
	}, nil
}

// resolveTranscriptID turns a bound claim into the ConversationProvider
// TranscriptID this adapter expects back on ConversationReadRequest: the
// absolute path to the session's JSONL file. §5.3 does not fix a
// TranscriptID shape; a disk-backed adapter's natural choice is the file
// path itself (docs/adapters/decisions.md, this package's section).
//
// TranscriptPath on the claim, when present, is trusted directly — it is
// what a generated hook's own payload reports (notes/16 §4:
// `transcript_path` on every hook event). Otherwise it is derived from
// WorkingDirectory and the session id using Claude Code's project-slug
// convention (notes/06-claude.md:10-11: cwd with `/` and `.` replaced by
// `-`). With neither, the caller gave this adapter nothing to locate the
// transcript with; TranscriptID comes back empty — a bound correlation
// with an unresolved transcript location is a valid, if degraded, result
// (Bound is about identity, not about ReadConversation being immediately
// callable).
func (r *Runtime) resolveTranscriptID(claim runtime.RuntimeClaim) string {
	if claim.TranscriptPath != "" {
		return claim.TranscriptPath
	}
	if claim.WorkingDirectory != "" {
		return filepath.Join(r.claudeDir, "projects", slugifyCWD(claim.WorkingDirectory), claim.ExternalAgentSessionID+".jsonl")
	}
	return ""
}

// slugifyCWD implements Claude Code's project-directory slug: the cwd
// with every `/` and `.` replaced by `-` (notes/06-claude.md:10-11).
func slugifyCWD(cwd string) string {
	var b strings.Builder
	b.Grow(len(cwd))
	for _, r := range cwd {
		if r == '/' || r == '.' {
			b.WriteByte('-')
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
