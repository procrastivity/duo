// Package claude is the Claude Code agent-runtime adapter: the §5.3
// Stage-1 surface only (RuntimeCorrelator, ConversationProvider) plus a
// §5.1 factory, built against the evidence in
// terminal-multiplexers/notes/16-claude-refresh.md and
// terminal-multiplexers/review/05-close-report.md at the re-pinned
// evidence floor, Claude Code 2.1.240 (2.1.241 corroborates the tmux-
// coordinate registry field only). See docs/adapters/decisions.md's
// "Claude Code runtime adapter" section for the design calls this package
// forced.
//
// Deliberately out of scope, matching the internal/runtime package's own
// Stage-1 cut: ConditionProvider, RuntimePromptProvider, UsageProvider,
// RuntimeConfigurationProvider, and HarnessRenderer. Correlate is built to
// accept a runtime.RuntimeClaim that already carries hook-reported identity
// (the session id and the launch-env reporter credential passed through to
// hook env, notes/16 §10), not to generate or install the hook
// configuration a correlation-reporting hook would need — that installation
// path is still out of scope here.
//
// closeonexit.go is a narrow exception: it generates and materializes one
// single-purpose SessionEnd hook plus a `claude --settings` document for
// close-on-exit, entirely independent of Correlate and the RuntimeClaim
// path above. See host.ResolvedLaunchTuple.CloseOnExit.
package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/procrastivity/duo/internal/adapter"
	"github.com/procrastivity/duo/internal/runtime"
)

// Runtime is the Claude Code agent-runtime adapter.
type Runtime struct {
	integrationInstanceID string
	// reporterCredential is the Duo-issued, instance-scoped secret this
	// runtime instance's Claude Code process receives on its launch
	// environment. notes/16 §10 verified that a DUO_* launch-env var
	// arrives verbatim in every hook process's environment, so a claim
	// that carries this exact value back can only have originated from a
	// generated hook belonging to this instance.
	reporterCredential string
	// claudeDir is this instance's Claude Code home (normally ~/.claude);
	// overridable so tests and multi-account hosts do not depend on the
	// real user's home directory. Both the session registry
	// (registry.go) and the projects transcript tree (conversation.go)
	// live under it.
	claudeDir string
}

var (
	_ runtime.RuntimeCorrelator    = (*Runtime)(nil)
	_ runtime.ConversationProvider = (*Runtime)(nil)
	_ adapter.Factory[*Runtime]    = Factory{}
)

// New returns a Claude Code runtime adapter for one integration instance.
// claudeDir empty defaults to $HOME/.claude (see defaultClaudeDir).
func New(integrationInstanceID, reporterCredential, claudeDir string) (*Runtime, error) {
	if claudeDir == "" {
		dir, err := defaultClaudeDir()
		if err != nil {
			return nil, fmt.Errorf("claude runtime %s: %w", integrationInstanceID, err)
		}
		claudeDir = dir
	}
	return &Runtime{
		integrationInstanceID: integrationInstanceID,
		reporterCredential:    reporterCredential,
		claudeDir:             claudeDir,
	}, nil
}

// defaultClaudeDir returns $HOME/.claude. Claude Code's on-disk state
// (projects/, sessions/) lives here unconditionally — notes/06-claude.md
// and notes/16 never observed an XDG override, unlike this repo's own
// config/store paths.
func defaultClaudeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory for ~/.claude: %w", err)
	}
	return filepath.Join(home, ".claude"), nil
}

// Factory is the Claude Code runtime's §5.1 adapter factory.
type Factory struct {
	// IntegrationInstanceID scopes this factory's adapters, as with the
	// fake runtime's Factory.
	IntegrationInstanceID string
	// ReporterCredential is the secret Duo placed on this instance's
	// `claude` launch environment (notes/16 §10). Empty is valid — it
	// means this instance was not launched with one, so Correlate can
	// only ever reach inferred (registry) confidence for it, never
	// authoritative.
	ReporterCredential string
	// ClaudeDir overrides $HOME/.claude. Empty uses the default.
	ClaudeDir string
}

// Descriptor implements adapter.Factory.
func (f Factory) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		AdapterID: "claude-code",
		Role:      adapter.RoleRuntime,
		// BuildVersion is this adapter build's own version, not Claude
		// Code's. Stage 1 has no release process yet.
		BuildVersion: "stage1",
		// The re-pinned evidence floor (review/05-close-report.md
		// "Re-pinned"): 2.1.240 is the probed floor; 2.1.241 corroborates
		// only the tmux-coordinate registry field (notes/16 §6) after an
		// in-session auto-update. Any other version is Unverified by
		// Probe, not rejected outright — §5.1 leaves the version-rule
		// syntax to a future evaluator, so this list stays a literal set
		// rather than a range expression this package would be
		// inventing unilaterally.
		SupportedExternalVersions: []string{"2.1.240", "2.1.241"},
		// No conformance-record digest scheme has been decided for a
		// non-fake adapter yet (docs/adapters/decisions.md, this
		// package's section: open item). This string names the evidence
		// this build was validated against until one exists.
		ConformanceRecordDigest: "notes16-claude-2.1.240",
		// §9's redaction duty (credentials, prompt content, protected
		// collaboration content, unapproved paths never logged in the
		// clear) applies in full to this adapter: transcripts carry raw
		// prompt/response text and the reporter credential is a secret.
		// No named redaction-policy registry exists yet in this repo
		// (open item, same doc section); this names the duty inline.
		DiagnosticRedactionPolicy: "redact-credentials-and-transcript-content",
	}
}

// Probe implements adapter.Factory. It deliberately does not exec `claude
// --version` or otherwise launch the runtime — this step's boundary rules
// out live probing (see the Step 18 spec's Boundaries section) — so
// DetectedVersion stays unset. What it can check without launching
// anything is whether this instance's Claude Code home is reachable at
// all.
func (f Factory) Probe(context.Context) (adapter.Probe, error) {
	claudeDir := f.ClaudeDir
	if claudeDir == "" {
		dir, err := defaultClaudeDir()
		if err != nil {
			return adapter.Probe{}, fmt.Errorf("claude runtime %s: probe: %w", f.IntegrationInstanceID, err)
		}
		claudeDir = dir
	}

	if info, err := os.Stat(claudeDir); err != nil || !info.IsDir() {
		return adapter.Probe{
			ProtocolOrFormatIdentity: "claude-code-jsonl-transcript",
			ConnectionState:          "unreachable",
			Compatibility:            adapter.CompatibilityUnavailable,
		}, nil
	}

	return adapter.Probe{
		// Left empty on purpose: no live version read this step (see the
		// doc comment above). A caller that needs the detected version
		// must supply it out of band (e.g. from a hook's own payload,
		// which does carry `version` per notes/16 §1) until this
		// package's Probe is allowed to launch `claude --version`.
		DetectedVersion:          "",
		ProtocolOrFormatIdentity: "claude-code-jsonl-transcript",
		ConnectionState:          "found",
		// Unverified, not Supported: reachability of ~/.claude proves
		// nothing about which Claude Code version wrote it. §5.1: "A
		// probe does not publish live Duo operation support by itself."
		Compatibility: adapter.CompatibilityUnverified,
	}, nil
}

// New implements adapter.Factory.
func (f Factory) New(_ context.Context, probe adapter.Probe) (*Runtime, error) {
	if probe.Compatibility == adapter.CompatibilityUnavailable {
		return nil, fmt.Errorf("claude runtime %s: probe reported unavailable, refusing to build an adapter", f.IntegrationInstanceID)
	}
	return New(f.IntegrationInstanceID, f.ReporterCredential, f.ClaudeDir)
}
