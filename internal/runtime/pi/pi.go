package pi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
)

// PinnedExternalVersion is the pi version this build's evidence was gathered
// against (conformance §7.4, re-pinned 2026-08-23). A different detected
// version probes as unverified, not incompatible: the transcript format and
// the extension surface may well be unchanged, but nothing here has been
// re-verified against it.
const PinnedExternalVersion = "0.83.0"

// TranscriptFormatIdentity names the on-disk transcript format this build
// parses: pi session JSONL, header "version": 3.
const TranscriptFormatIdentity = "pi-session-jsonl/v3"

// Confidence labels this adapter puts on a binding. The vocabulary is per
// adapter family (§5.3); these two are Pi's.
const (
	// ConfidenceExtensionExact: the claim carried the reporter credential
	// issued to this runtime instance, so the session id came from inside
	// the agent process and is authenticated to it.
	ConfidenceExtensionExact = "pi-extension-exact"
	// ConfidenceTranscriptHeuristic: no credential was issued for this
	// instance, so the session id is taken at face value from the
	// transcript channel. Correct in a single-instance workspace, not
	// authenticated against anything.
	ConfidenceTranscriptHeuristic = "pi-transcript-heuristic"
)

// Runtime is the Pi agent-runtime adapter for one integration instance. It
// holds only configuration — the session id and transcript live in pi's own
// process and on disk — so its methods are safe for concurrent use.
type Runtime struct {
	integrationInstanceID string
	reporterCredential    string
	sessionsRoot          string
}

var (
	_ runtime.RuntimeCorrelator     = (*Runtime)(nil)
	_ runtime.ConversationProvider  = (*Runtime)(nil)
	_ runtime.ConditionProvider     = (*Runtime)(nil)
	_ runtime.RuntimePromptProvider = (*Runtime)(nil)
	_ runtime.RuntimeReadyProvider  = (*Runtime)(nil)
	_ adapter.Factory[*Runtime]     = Factory{}
)

// Option configures a Runtime.
type Option func(*Runtime)

// WithSessionsRoot overrides the pi sessions directory. Tests use it; so
// would a Duo launch variant that sets PI_CODING_AGENT_SESSION_DIR for the
// instance it spawns.
func WithSessionsRoot(dir string) Option {
	return func(r *Runtime) { r.sessionsRoot = dir }
}

// WithReporterCredential records the credential Duo issued to this exact
// runtime instance's generated extension. With one set, Correlate binds only
// claims that present it. Without one — a pi Duo did not spawn — Correlate
// falls back to the weaker transcript-channel binding.
func WithReporterCredential(credential string) Option {
	return func(r *Runtime) { r.reporterCredential = credential }
}

// New returns a Pi runtime adapter for one integration instance.
func New(integrationInstanceID string, opts ...Option) *Runtime {
	r := &Runtime{
		integrationInstanceID: integrationInstanceID,
		sessionsRoot:          DefaultSessionsRoot(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// DefaultSessionsRoot resolves pi's sessions directory the way pi itself
// does: PI_CODING_AGENT_SESSION_DIR wins, then PI_CODING_AGENT_DIR/sessions,
// then ~/.pi/agent/sessions.
func DefaultSessionsRoot() string {
	if dir := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); dir != "" {
		return dir
	}
	if dir := os.Getenv("PI_CODING_AGENT_DIR"); dir != "" {
		return filepath.Join(dir, "sessions")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".pi", "agent", "sessions")
}

// Factory is the Pi adapter's §5.1 factory.
type Factory struct {
	IntegrationInstanceID string
	// Binary is the pi executable to probe; empty means "pi" on PATH.
	Binary string
	// SessionsRoot overrides the sessions directory for the built adapter;
	// empty means DefaultSessionsRoot.
	SessionsRoot string
	// ReporterCredential is the credential issued to this instance's
	// generated extension, when Duo spawned it.
	ReporterCredential string
}

// Descriptor implements adapter.Factory.
func (f Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID:                 "pi",
		Role:                      adapter.RoleRuntime,
		BuildVersion:              "stage1",
		SupportedExternalVersions: []string{PinnedExternalVersion},
		ConformanceRecordDigest:   "pi-0.83.0-2026-08-23",
		// Every diagnostic from this adapter can carry the reporter
		// credential (claims) and transcript text (conversation), so it
		// goes through the strictest policy the redaction step defines.
		DiagnosticRedactionPolicy: "credential-and-prompt-content",
	}
}

// Probe implements adapter.Factory: it asks the pi binary for its version and
// reports nothing else. It does not read a transcript, so it never claims to
// have observed the on-disk format — ProtocolOrFormatIdentity names the
// format this build parses, and ReadConversation re-verifies the header of
// every file it opens.
func (f Factory) Probe(ctx context.Context) (adapter.Probe, error) {
	binary := f.Binary
	if binary == "" {
		binary = "pi"
	}
	probe := adapter.Probe{
		ProtocolOrFormatIdentity: TranscriptFormatIdentity,
		ConnectionState:          "absent",
		Compatibility:            adapter.CompatibilityUnavailable,
	}

	path, err := exec.LookPath(binary)
	if err != nil {
		return probe, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		probe.ConnectionState = "unreadable"
		return probe, nil
	}

	probe.DetectedVersion = strings.TrimSpace(firstLine(string(out)))
	probe.ConnectionState = "installed"
	if probe.DetectedVersion == PinnedExternalVersion {
		probe.Compatibility = adapter.CompatibilitySupported
	} else {
		probe.Compatibility = adapter.CompatibilityUnverified
	}
	return probe, nil
}

// New implements adapter.Factory.
func (f Factory) New(_ context.Context, _ adapter.Probe) (*Runtime, error) {
	opts := []Option{}
	if f.SessionsRoot != "" {
		opts = append(opts, WithSessionsRoot(f.SessionsRoot))
	}
	if f.ReporterCredential != "" {
		opts = append(opts, WithReporterCredential(f.ReporterCredential))
	}
	return New(f.IntegrationInstanceID, opts...), nil
}

// Correlate implements runtime.RuntimeCorrelator.
//
// What binds, and what does not:
//
//   - A claim with no external agent-session id never binds, whatever else it
//     carries. §5.3: a transcript path or working directory cannot bind a
//     runtime instance. On Pi that rule has teeth — the cwd slug that names a
//     transcript directory is lossy and shared by every session ever run in
//     that directory.
//   - With a reporter credential issued for this instance, the claim must
//     present it. A missing or wrong credential is not an error: it is a
//     claim from something other than this instance's reporter, which is
//     exactly "insufficient evidence to bind".
//   - A claim whose transcript path names a different session id than the
//     claim's own is contradictory evidence and does not bind.
//
// A bound result carries the transcript's absolute path as TranscriptID —
// on Pi the path is the transcript's identity, and its file name embeds the
// session id. TranscriptID is empty on a bound result when no transcript file
// could be located: pi run with --no-session writes none, and a just-started
// session may not have one yet. Correlation binds the runtime instance;
// ReadConversation is where a missing transcript becomes an error.
func (r *Runtime) Correlate(ctx context.Context, claim runtime.RuntimeClaim) (runtime.RuntimeCorrelationEvidence, error) {
	if claim.IntegrationInstanceID != r.integrationInstanceID {
		return runtime.RuntimeCorrelationEvidence{}, fmt.Errorf(
			"pi runtime %s: claim for integration instance %s",
			r.integrationInstanceID, claim.IntegrationInstanceID)
	}
	if err := ctx.Err(); err != nil {
		return runtime.RuntimeCorrelationEvidence{}, err
	}

	unbound := runtime.RuntimeCorrelationEvidence{Bound: false}
	if claim.ExternalAgentSessionID == "" {
		return unbound, nil
	}

	confidence := ConfidenceTranscriptHeuristic
	if r.reporterCredential != "" {
		if claim.ReporterCredential != r.reporterCredential {
			return unbound, nil
		}
		confidence = ConfidenceExtensionExact
	}

	if claim.TranscriptPath != "" {
		named := SessionIDFromTranscriptName(claim.TranscriptPath)
		if named != "" && named != claim.ExternalAgentSessionID {
			return unbound, nil
		}
	}

	transcriptID, err := r.resolveTranscript(claim.ExternalAgentSessionID, claim.WorkingDirectory, claim.TranscriptPath)
	if err != nil {
		// Not locating a transcript weakens the evidence but does not
		// unbind it; see the method comment.
		transcriptID = ""
	}

	return runtime.RuntimeCorrelationEvidence{
		ExternalAgentSessionID: claim.ExternalAgentSessionID,
		TranscriptID:           transcriptID,
		Bound:                  true,
		Confidence:             confidence,
	}, nil
}

// SessionSlug returns the sessions-directory name pi derives from a working
// directory: the path's separators become "-", and the result is wrapped in
// "--" on both sides (/home/dev/Code/wip -> --home-dev-Code-wip--).
//
// The mapping is lossy — a cwd that already contains "-" is
// indistinguishable from one with a separator there — so a slug is a lookup
// hint, never proof of identity. Everything this package resolves through a
// slug is re-checked against the transcript's own header.
func SessionSlug(cwd string) string {
	trimmed := strings.Trim(filepath.Clean(cwd), string(filepath.Separator))
	return "--" + strings.ReplaceAll(trimmed, string(filepath.Separator), "-") + "--"
}

// SessionIDFromTranscriptName extracts the session id a pi transcript file
// name embeds (<ts>_<uuid>.jsonl). It returns "" for a name in another shape.
func SessionIDFromTranscriptName(path string) string {
	base := filepath.Base(path)
	if !strings.HasSuffix(base, ".jsonl") {
		return ""
	}
	base = strings.TrimSuffix(base, ".jsonl")
	i := strings.LastIndex(base, "_")
	if i < 0 || i == len(base)-1 {
		return ""
	}
	return base[i+1:]
}

// resolveTranscript finds the transcript file for one session id. The hint —
// the path a reporter claim carried — is used when it exists on disk;
// otherwise the sessions tree is searched, narrowed by the cwd slug when
// there is one.
func (r *Runtime) resolveTranscript(sessionID, cwd, hint string) (string, error) {
	if hint != "" {
		if !filepath.IsAbs(hint) {
			return "", fmt.Errorf("pi: transcript path %q is not absolute", hint)
		}
		if info, err := os.Stat(hint); err == nil && !info.IsDir() {
			return hint, nil
		}
	}
	if r.sessionsRoot == "" {
		return "", fmt.Errorf("pi: no sessions root configured")
	}

	pattern := "*_" + sessionID + ".jsonl"
	if cwd != "" {
		matches, err := filepath.Glob(filepath.Join(r.sessionsRoot, SessionSlug(cwd), pattern))
		if err == nil && len(matches) == 1 {
			return matches[0], nil
		}
	}

	entries, err := os.ReadDir(r.sessionsRoot)
	if err != nil {
		return "", fmt.Errorf("pi: read sessions root: %w", err)
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		matches, err := filepath.Glob(filepath.Join(r.sessionsRoot, e.Name(), pattern))
		if err != nil {
			continue
		}
		found = append(found, matches...)
	}
	sort.Strings(found)

	switch len(found) {
	case 0:
		return "", fmt.Errorf("pi: no transcript for session %s under %s", sessionID, r.sessionsRoot)
	case 1:
		return found[0], nil
	default:
		return "", fmt.Errorf("pi: %d transcripts claim session %s: %s",
			len(found), sessionID, strings.Join(found, ", "))
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
