package domain_test

import (
	"context"
	"testing"

	"github.com/procrastivity/duo/internal/domain"
)

// TestLaunchResolutionBindingKeysTheCommittedInstance pins the reverse
// lookup the doctor harness sweep uses: an lrr id maps to the session and
// instance minted in the same launch.resolved fact, not to Session.Current.
func TestLaunchResolutionBindingKeysTheCommittedInstance(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)

	res, err := h.a.Launch(ctx, domain.LaunchRequest{
		RootPath: "/home/dev/Code/duo",
		Actor:    "user:test",
		Resolution: &domain.LaunchResolution{
			ID:   "lrr_bind",
			Body: []byte(`{"id":"lrr_bind"}`),
		},
	})
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	session, instance, ok := h.a.LaunchResolutionBinding("lrr_bind")
	if !ok {
		t.Fatal("LaunchResolutionBinding(lrr_bind) returned false, want the committed pair")
	}
	if session != res.Session || instance != res.Instance {
		t.Errorf("LaunchResolutionBinding = (%s, %s), want (%s, %s)",
			session, instance, res.Session, res.Instance)
	}

	if _, _, ok := h.a.LaunchResolutionBinding("lrr_never_committed"); ok {
		t.Error("LaunchResolutionBinding returned true for an id that was never committed")
	}
}
