package launch

import (
	"testing"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/host"
)

// TestResolvePlacementSources covers the three target_source values
// notes/51 record 2 locks: explicit-flag, config-default, built-in.
// Herdr's built-in remains tab (notes/51 record 3).
func TestResolvePlacementSources(t *testing.T) {
	tab := "tab"
	pane := "pane"
	policyWithTab := config.SessionHostPolicy{
		Kinds: map[string]config.SessionHostKind{
			"herdr": {LaunchTarget: &tab},
		},
	}
	policyWithPane := config.SessionHostPolicy{
		Kinds: map[string]config.SessionHostKind{
			"herdr": {LaunchTarget: &pane},
		},
	}
	emptyPolicy := config.SessionHostPolicy{}

	cases := []struct {
		name       string
		flag       host.LaunchTarget
		kind       string
		policy     config.SessionHostPolicy
		wantTarget host.LaunchTarget
		wantSource string
	}{
		{
			name:       "explicit-flag wins over config",
			flag:       host.LaunchTargetPane,
			kind:       "herdr",
			policy:     policyWithTab,
			wantTarget: host.LaunchTargetPane,
			wantSource: TargetSourceExplicitFlag,
		},
		{
			name:       "config-default when flag absent",
			flag:       "",
			kind:       "herdr",
			policy:     policyWithPane,
			wantTarget: host.LaunchTargetPane,
			wantSource: TargetSourceConfigDefault,
		},
		{
			name:       "built-in when flag and config absent",
			flag:       "",
			kind:       "herdr",
			policy:     emptyPolicy,
			wantTarget: host.LaunchTargetTab,
			wantSource: TargetSourceBuiltIn,
		},
		{
			name:       "built-in when kind stanza has no launch_target",
			flag:       "",
			kind:       "herdr",
			policy:     config.SessionHostPolicy{Kinds: map[string]config.SessionHostKind{"herdr": {}}},
			wantTarget: host.LaunchTargetTab,
			wantSource: TargetSourceBuiltIn,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, source, err := ResolvePlacement(tc.flag, tc.kind, tc.policy)
			if err != nil {
				t.Fatalf("ResolvePlacement: %v", err)
			}
			if got != tc.wantTarget {
				t.Errorf("target = %q, want %q", got, tc.wantTarget)
			}
			if source != tc.wantSource {
				t.Errorf("source = %q, want %q", source, tc.wantSource)
			}
		})
	}
}

// TestConfigurationDigestPinsLaunchTarget proves that setting
// session_hosts.kinds.<kind>.launch_target changes configurationDigest,
// without rewriting digest scope (session_hosts is still hashed wholesale).
func TestConfigurationDigestPinsLaunchTarget(t *testing.T) {
	enabled := true
	pane := "pane"

	base := config.DocumentV3{
		Presets:        map[string]config.PresetV3{"review": {}},
		LaunchVariants: map[string]config.LaunchVariant{},
		AgentRuntimes:  map[string]config.AgentRuntime{},
		SessionHosts: config.SessionHostPolicy{
			Prefer: []string{"herdr"},
			Kinds:  map[string]config.SessionHostKind{"herdr": {Enabled: &enabled}},
		},
	}
	withTarget := config.DocumentV3{
		Presets:        map[string]config.PresetV3{"review": {}},
		LaunchVariants: map[string]config.LaunchVariant{},
		AgentRuntimes:  map[string]config.AgentRuntime{},
		SessionHosts: config.SessionHostPolicy{
			Prefer: []string{"herdr"},
			Kinds: map[string]config.SessionHostKind{
				"herdr": {Enabled: &enabled, LaunchTarget: &pane},
			},
		},
	}

	baseDigest, err := configurationDigest(base)
	if err != nil {
		t.Fatalf("configurationDigest(base): %v", err)
	}
	withDigest, err := configurationDigest(withTarget)
	if err != nil {
		t.Fatalf("configurationDigest(withTarget): %v", err)
	}
	if baseDigest == withDigest {
		t.Fatalf("launch_target did not change configurationDigest: both %q", baseDigest)
	}
	again, err := configurationDigest(withTarget)
	if err != nil {
		t.Fatalf("configurationDigest(withTarget) again: %v", err)
	}
	if again != withDigest {
		t.Errorf("digest not stable: %q != %q", again, withDigest)
	}
	if withDigest != wantLaunchTargetConfigurationDigest {
		t.Errorf("configurationDigest with launch_target = %q, want pinned %q", withDigest, wantLaunchTargetConfigurationDigest)
	}
}

// wantLaunchTargetConfigurationDigest pins the digest of a document whose
// herdr kind stanza sets launch_target: pane. Update only when the
// launch-relevant subset intentionally changes.
const wantLaunchTargetConfigurationDigest = "sha256:6e00b8f68d6abc6a5c1a132371d7ceadc1600e7f2a869e05795ae4ab686c134b"
