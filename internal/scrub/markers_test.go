package scrub

import "testing"

func TestIsMarker(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"CLAUDE_CODE_CHILD_SESSION", true},
		{"CLAUDECODE", true},
		{"AI_AGENT", true},
		{"CLAUDE_CODE_ENTRYPOINT", true},      // wildcard
		{"CLAUDE_CONFIG_DIR", true},           // wildcard
		{"CLAUDE_CODE_MESSAGING_TOKEN", true}, // wildcard, decisions.md G-24 candidate
		{"CLAUDE_", true},                     // the bare prefix itself is a marker name
		{"", false},
		{"PATH", false},
		{"HOME", false},
		{"DUO_SESSION", false},
		{"AI_AGENT_X", false}, // AI_AGENT is exact-name only; see TestAIAgentExactNameOnly
		{"NOT_CLAUDE_", false},
		{"CLAUDE", false}, // no trailing underscore: does not match the CLAUDE_ prefix
	}
	for _, c := range cases {
		if got := IsMarker(c.name); got != c.want {
			t.Errorf("IsMarker(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestAIAgentExactNameOnly documents a deliberate scope decision: AI_AGENT
// is an exact-name marker (like CLAUDECODE), not a prefix. AI_AGENT_FOO is
// therefore not scrubbed unless a future WildcardPrefixes entry says so.
// This test exists so that decision is visible and intentional rather
// than an accident of the case table above.
func TestAIAgentExactNameOnly(t *testing.T) {
	if IsMarker("AI_AGENT_FOO") {
		t.Error("AI_AGENT_FOO scrubs, but AI_AGENT is an exact-name marker, not a prefix: " +
			"either markers.go grew an AI_AGENT_ wildcard prefix (update docs/scrub/decisions.md " +
			"to record the scope change) or this test's premise is stale")
	}
}

func TestMarkersHasNoDuplicates(t *testing.T) {
	seen := make(map[string]bool)
	for _, m := range Markers {
		if seen[m] {
			t.Errorf("Markers contains duplicate %q", m)
		}
		seen[m] = true
	}
}
