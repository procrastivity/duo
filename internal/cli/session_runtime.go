package cli

import (
	"fmt"
	"strings"
	"sync"

	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/registry"
	"github.com/procrastivity/duo/internal/runtime"
	"github.com/procrastivity/duo/internal/runtime/claude"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
	runtimepi "github.com/procrastivity/duo/internal/runtime/pi"
)

// registeredOpByCLI returns the registry operation name whose CLI path is
// cli, or empty when none matches. Call sites that need an operation name
// derive it here so they do not invent a second inventory of literals
// (docs/registry/decisions.md single-source rule).
func registeredOpByCLI(cli ...string) string {
	for _, d := range registry.All() {
		if len(d.CLI) != len(cli) {
			continue
		}
		match := true
		for i := range cli {
			if d.CLI[i] != cli[i] {
				match = false
				break
			}
		}
		if match {
			return d.Name
		}
	}
	return ""
}

// agentRuntimeBindings holds the external agent-session and transcript
// correlations on one Duo runtime instance, when present.
type agentRuntimeBindings struct {
	IntegrationInstance    string
	ExternalAgentSessionID string
	TranscriptID           string
}

// agentBindingsFor looks up the active agent.session and transcript
// correlations on the session's current runtime instance. ok is false when
// the session has no current instance or no agent.session correlation.
func agentBindingsFor(a *domain.Authority, s domain.Session) (agentRuntimeBindings, bool) {
	if s.Current == "" {
		return agentRuntimeBindings{}, false
	}
	var out agentRuntimeBindings
	for _, c := range a.Correlations(domain.TargetInstance, string(s.Current)) {
		if c.Status != domain.CorrelationActive {
			continue
		}
		switch c.ExternalKind {
		case "agent.session":
			out.IntegrationInstance = c.Scope
			out.ExternalAgentSessionID = c.ExternalValue
		case "transcript":
			out.TranscriptID = c.ExternalValue
			if out.IntegrationInstance == "" {
				out.IntegrationInstance = c.Scope
			}
		}
	}
	if out.ExternalAgentSessionID == "" {
		return agentRuntimeBindings{}, false
	}
	return out, true
}

// registeredAgentRuntimes maps an agent-runtime integration instance ID to
// a live adapter handle. Tests with the fake pair (D6) register a seeded
// fake here so SeedCondition / SeedTranscript survive across CLI calls.
// Production path falls back to openKnownAgentRuntime.
var registeredAgentRuntimes sync.Map // string -> any

// RegisterAgentRuntime binds rt as the agent-runtime adapter for
// integrationInstanceID. Tests call this with a seeded fake; production
// dogfood enrolls that use a known AdapterID need no registration.
func RegisterAgentRuntime(integrationInstanceID string, rt any) {
	registeredAgentRuntimes.Store(integrationInstanceID, rt)
}

// UnregisterAgentRuntime drops a prior RegisterAgentRuntime entry.
func UnregisterAgentRuntime(integrationInstanceID string) {
	registeredAgentRuntimes.Delete(integrationInstanceID)
}

// openAgentRuntime returns the agent-runtime adapter for one integration
// instance. Prefer an explicitly registered handle; otherwise construct a
// Stage-1 adapter whose AdapterID equals the integration instance ID
// (claude-code, pi, fake-runtime). Unknown IDs fail — no invented kind
// heuristics.
func openAgentRuntime(integrationInstanceID string) (any, error) {
	if integrationInstanceID == "" {
		return nil, fmt.Errorf("cli: empty agent-runtime integration instance")
	}
	if v, ok := registeredAgentRuntimes.Load(integrationInstanceID); ok {
		return v, nil
	}
	return openKnownAgentRuntime(integrationInstanceID)
}

func openKnownAgentRuntime(integrationInstanceID string) (any, error) {
	switch integrationInstanceID {
	case "fake-runtime":
		return runtimefake.New(integrationInstanceID), nil
	case "claude-code":
		return claude.New(integrationInstanceID, "", "")
	case "pi":
		return runtimepi.New(integrationInstanceID), nil
	default:
		return nil, fmt.Errorf("cli: no agent-runtime adapter for integration instance %q", integrationInstanceID)
	}
}

// agentRuntimeIntegrationID maps a launch-tuple public agent-runtime kind
// onto the adapter integration-instance ID openAgentRuntime knows.
func agentRuntimeIntegrationID(kind string) string {
	switch kind {
	case "claude":
		return "claude-code"
	case "pi":
		return "pi"
	default:
		return kind
	}
}

// conditionObservationRequest builds the SnapshotCondition request from
// store correlations. Adapters key on these external identifiers, not Duo
// session IDs.
func conditionObservationRequest(b agentRuntimeBindings) runtime.ConditionObservationRequest {
	return runtime.ConditionObservationRequest{
		ExternalAgentSessionID: b.ExternalAgentSessionID,
		TranscriptID:           b.TranscriptID,
	}
}

// conversationReadRequest builds the ReadConversation request from store
// correlations plus optional pagination.
func conversationReadRequest(b agentRuntimeBindings, after string, limit int) runtime.ConversationReadRequest {
	return runtime.ConversationReadRequest{
		ExternalAgentSessionID: b.ExternalAgentSessionID,
		TranscriptID:           b.TranscriptID,
		After:                  after,
		Limit:                  limit,
	}
}

// registeredHostPromptProviders maps a session-host integration instance
// ID to a HostPromptProvider. Tests register a seeded fake-host so
// ScriptPromptOutcome survives across CLI calls; production falls back to
// openKnownHostPromptProvider.
var registeredHostPromptProviders sync.Map // string -> host.HostPromptProvider

// RegisterHostPromptProvider binds p as the host prompt adapter for
// integrationInstanceID. Tests call this with a seeded fake.
func RegisterHostPromptProvider(integrationInstanceID string, p host.HostPromptProvider) {
	registeredHostPromptProviders.Store(integrationInstanceID, p)
}

// UnregisterHostPromptProvider drops a prior RegisterHostPromptProvider entry.
func UnregisterHostPromptProvider(integrationInstanceID string) {
	registeredHostPromptProviders.Delete(integrationInstanceID)
}

// openHostPromptProviderFor returns the HostPromptProvider for one
// integration instance. Prefer an explicitly registered handle; otherwise
// construct fake-host or Herdr. Unknown IDs yield a nil provider (runtime
// path only) rather than inventing a host kind.
func openHostPromptProviderFor(integrationInstanceID string) (host.HostPromptProvider, error) {
	if integrationInstanceID == "" {
		return nil, nil
	}
	if v, ok := registeredHostPromptProviders.Load(integrationInstanceID); ok {
		return v.(host.HostPromptProvider), nil
	}
	return openKnownHostPromptProvider(integrationInstanceID)
}

func openKnownHostPromptProvider(integrationInstanceID string) (host.HostPromptProvider, error) {
	switch {
	case integrationInstanceID == "fake-host":
		return hostfake.New(integrationInstanceID), nil
	case integrationInstanceID == herdr.AdapterID || strings.HasPrefix(integrationInstanceID, "herdr:"):
		socket, err := herdrSocketForIntegration(integrationInstanceID)
		if err != nil {
			return nil, duoerr.New("operation.temporarily_unavailable",
				fmt.Sprintf("No session-host prompt adapter is available for %q.", integrationInstanceID))
		}
		return herdr.New(herdr.Config{
			IntegrationInstanceID: integrationInstanceID,
			SocketPath:            socket,
		})
	default:
		return nil, nil
	}
}
