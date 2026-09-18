package opencode

import (
	"fmt"
	"strings"

	"github.com/procrastivity/duo/internal/adapter"
)

// CapabilityReport is intentionally explicit: this adapter observes only
// health/schema/session/message/todo/status and the SSE event stream.
type CapabilityReport struct {
	Health, Schema, Session, SSE                            bool
	Conversation, Condition                                 bool
	Prompt, Abort, Permission, Question, ProviderEffect     bool
	RetryReplay, TUI, RestartDetach, HooksMCP, Herdr, Moshi bool
	SSEAvailability, ConditionStatus, ConversationStatus    string
	Deferred                                                []string
}

// Capabilities returns the observer's read-only capability report.
func (r *Runtime) Capabilities() CapabilityReport {
	return CapabilityReport{
		Health: true, Schema: true, Session: true, SSE: true, Conversation: true, Condition: true,
		SSEAvailability: "conservative-bound-stream-only", ConditionStatus: "degraded/unknown", ConversationStatus: "degraded/unknown",
		Deferred: []string{"prompt/message writes", "abort", "permissions/questions", "provider/effects", "retry/replay", "TUI", "restart/detach", "hooks/MCP", "Herdr", "Moshi", "plugin reload/lifecycle"},
	}
}

// Registration is the safe discoverability seam for manifest/doctor. It is
// a descriptor only: it contains no endpoint, ambient environment, or legacy
// CLI attachment path.
type Registration struct {
	Descriptor   adapter.Descriptor
	Capabilities CapabilityReport
}

// Registered returns the package-owned capability entry. The condition is
// deliberately degraded until an admitted runtime supplies live evidence.
func Registered() Registration {
	return Registration{Descriptor: (Factory{}).Descriptor(), Capabilities: (&Runtime{}).Capabilities()}
}

// LaunchSpec is an admission description, not a process launcher. The host
// owns spawning and must validate this exact command before handing its
// endpoint and epoch to New.
type LaunchSpec struct {
	Command string
	Args    []string
}

// PureLaunchSpec returns the exact side-effect-free OpenCode command shape.
func PureLaunchSpec(port int) (LaunchSpec, error) {
	if port < 1 || port > 65535 {
		return LaunchSpec{}, fmt.Errorf("opencode: invalid port")
	}
	return LaunchSpec{Command: "opencode", Args: []string{"serve", "--pure", "--mdns=false", "--hostname", "127.0.0.1", "--port", fmt.Sprint(port)}}, nil
}

// ValidateLaunchMetadata checks host-provided launch facts and flags.
func ValidateLaunchMetadata(m LaunchMetadata, spec LaunchSpec) error {
	if m.PID <= 0 || m.Port < 1 || m.Port > 65535 || m.ExecutableSHA256 != ExecutableSHA256 || m.SchemaSHA256 != SchemaSHA256 || !m.OwnershipVerified {
		return fmt.Errorf("opencode: launch metadata is not Duo-owned")
	}
	if !safeIdentity(m.IntegrationInstanceID) || !safeIdentity(m.ProcessEpoch) || !loopbackEndpoint(m.Endpoint) || (m.SessionID != "" && !sessionIDRE.MatchString(m.SessionID)) {
		return fmt.Errorf("opencode: incomplete launch identity")
	}
	if !strings.HasSuffix(m.Endpoint, ":"+fmt.Sprint(m.Port)) {
		return fmt.Errorf("opencode: launch endpoint is not loopback-owned")
	}
	want, err := PureLaunchSpec(m.Port)
	if err != nil || spec.Command != want.Command || len(spec.Args) != len(want.Args) || len(m.Flags) != len(want.Args) {
		return fmt.Errorf("opencode: launch flags are not pure")
	}
	for i := range want.Args {
		if spec.Args[i] != want.Args[i] || m.Flags[i] != want.Args[i] {
			return fmt.Errorf("opencode: launch flags are not pure")
		}
	}
	return nil
}

func safeIdentity(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if r < 0x21 || r > 0x7e || strings.ContainsRune("/\\\"'`", r) {
			return false
		}
	}
	return true
}

// PublicLaunchMetadata is the stable, non-sensitive diagnostic projection.
type PublicLaunchMetadata struct{ IntegrationInstanceID, ProcessEpoch, SessionID string }

// RedactLaunchMetadata removes dynamic and sensitive launch fields.
func RedactLaunchMetadata(m LaunchMetadata) PublicLaunchMetadata {
	return PublicLaunchMetadata{IntegrationInstanceID: m.IntegrationInstanceID, ProcessEpoch: m.ProcessEpoch, SessionID: m.SessionID}
}
