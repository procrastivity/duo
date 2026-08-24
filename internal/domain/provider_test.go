package domain_test

import (
	"context"
	"errors"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
)

// TestProviderDefaultEnabled: a provider name with no standing fact is
// absent from StandingProviderFacts — the default-enabled rule applies at
// the reader, not by seeding the map (workplan Step 08, "default is
// enabled").
func TestProviderDefaultEnabled(t *testing.T) {
	h := newHarness(t)

	standing := h.a.StandingProviderFacts()
	if _, ok := standing["anthropic"]; ok {
		t.Fatalf("StandingProviderFacts reports an entry for a name with no fact: %+v", standing["anthropic"])
	}
}

// TestProviderDisableThenEnable_LatestWins is the replay-order proof: the
// latest provider.disabled or provider.enabled fact for one name wins, and
// the read model keeps that fact's ID (step 11 snapshots provider state by
// fact ID). The check runs once against the live kernel and again after a
// reopen, so replay order agrees with post-commit application.
func TestProviderDisableThenEnable_LatestWins(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	disabledID, err := h.a.DisableProvider(ctx, "anthropic", "user:beau", "usage limit")
	if err != nil {
		t.Fatalf("DisableProvider: %v", err)
	}
	standing := h.a.StandingProviderFacts()
	st, ok := standing["anthropic"]
	if !ok {
		t.Fatal("StandingProviderFacts has no entry for anthropic after DisableProvider")
	}
	if st.Enabled {
		t.Fatal("anthropic reports enabled right after DisableProvider")
	}
	if st.FactID != disabledID {
		t.Fatalf("fact id = %s, want %s (the disable fact)", st.FactID, disabledID)
	}

	enabledID, err := h.a.EnableProvider(ctx, "anthropic", "user:beau", "limit reset")
	if err != nil {
		t.Fatalf("EnableProvider: %v", err)
	}
	if enabledID == disabledID {
		t.Fatal("EnableProvider minted the same fact id as DisableProvider")
	}
	standing = h.a.StandingProviderFacts()
	st, ok = standing["anthropic"]
	if !ok {
		t.Fatal("StandingProviderFacts has no entry for anthropic after EnableProvider")
	}
	if !st.Enabled {
		t.Fatal("anthropic reports disabled after EnableProvider; the latest fact should win")
	}
	if st.FactID != enabledID {
		t.Fatalf("fact id = %s, want %s (the enable fact, the latest one)", st.FactID, enabledID)
	}

	// A second, unrelated provider stays out of the map entirely: its
	// absence is the default-enabled signal, not a seeded true entry.
	if _, ok := standing["openai"]; ok {
		t.Fatal("StandingProviderFacts reports an entry for a provider with no fact")
	}

	// Reopen: the kernel rebuilds entirely from the durable fact log, and
	// replay order must agree with what post-commit application already
	// showed above (decision-01 §4.4's reload).
	h.reopen()
	standing = h.a.StandingProviderFacts()
	st, ok = standing["anthropic"]
	if !ok {
		t.Fatal("StandingProviderFacts has no entry for anthropic after reopen")
	}
	if !st.Enabled || st.FactID != enabledID {
		t.Fatalf("after reopen: standing = %+v, want enabled=true fact_id=%s", st, enabledID)
	}
}

// TestProviderDisableAllowsUnknownName: the kernel holds no config and
// cannot know whether any launch_variant names the provider, so it never
// refuses an unrecognized name — only an empty one.
func TestProviderDisableAllowsUnknownName(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	if _, err := h.a.DisableProvider(ctx, "no-such-provider", "user:beau", ""); err != nil {
		t.Fatalf("DisableProvider on an unrecognized name: %v", err)
	}
	standing := h.a.StandingProviderFacts()
	if standing["no-such-provider"].Enabled {
		t.Fatal("no-such-provider reports enabled right after DisableProvider")
	}
}

// TestProviderNameRequired: an empty name is refused, distinctly from an
// unrecognized one.
func TestProviderNameRequired(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	_, err := h.a.DisableProvider(ctx, "", "user:beau", "")
	if !errors.Is(err, domain.ErrProviderNameRequired) {
		t.Fatalf("DisableProvider(\"\") error = %v, want ErrProviderNameRequired", err)
	}
	_, err = h.a.EnableProvider(ctx, "", "user:beau", "")
	if !errors.Is(err, domain.ErrProviderNameRequired) {
		t.Fatalf("EnableProvider(\"\") error = %v, want ErrProviderNameRequired", err)
	}
}
