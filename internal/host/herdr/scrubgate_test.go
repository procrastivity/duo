package herdr

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/scrub"
)

// markerEnviron builds a pane environment carrying one of every marker the
// scrub policy denies. It is derived from internal/scrub's exported deny
// list, never retyped: TestNoDuplicateMarkerLiteralsOutsideScrub bans a
// marker literal in this package, and a test that duplicated the policy
// would also stop testing the policy the moment markers.go changed.
func markerEnviron() []string {
	out := []string{"PATH=/usr/bin", "HOME=/home/tester"}
	for _, m := range scrub.Markers {
		out = append(out, m+"=1")
	}
	for _, p := range scrub.WildcardPrefixes {
		out = append(out, p+"SOMETHING=x")
	}
	return out
}

func environResolver(environ []string, err error) PaneEnvironResolver {
	return func(context.Context, int) ([]string, error) { return environ, err }
}

// The gate's whole purpose: a pane that inherited a marker never gets an
// agent started in it. This is conformance §8's hard Stage-1 gate — the
// launch is refused, not warned about — and the pane the refusal created
// is torn down, so a refused launch leaves nothing behind.
func TestStartRefusesAPaneThatInheritedAMarker(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	before := f.paneCount()
	h := testHost(t, f, func(c *Config) { c.ResolvePaneEnviron = environResolver(markerEnviron(), nil) })
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	_, err = h.Start(ctx, prepared)
	if err == nil {
		t.Fatal("Start launched into a marker-carrying pane")
	}
	var refusal *scrub.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("Start returned %T (%v), want *scrub.RefusalError", err, err)
	}
	if len(refusal.Survivors) != len(scrub.Markers)+len(scrub.WildcardPrefixes) {
		t.Errorf("Survivors = %v, want every denied marker named", refusal.Survivors)
	}
	if n := f.callCount("agent.start"); n != 0 {
		t.Errorf("agent.start called %d times after a refusal; the gate is not fail-closed", n)
	}
	if n := f.callCount("tab.close"); n != 1 {
		t.Errorf("tab.close called %d times, want 1: a refused launch closes the tab it created", n)
	}
	if got := f.paneCount(); got != before {
		t.Errorf("pane count = %d, want %d: the refused launch's pane survived", got, before)
	}
	if refusal.Cleanup != nil {
		t.Errorf("Cleanup = %v, want nil: the tab closed successfully", refusal.Cleanup)
	}
}

// The positive leg. A pane whose inherited environment is clean launches
// normally, and the gate is not something Start can be talked out of
// consulting.
func TestStartProceedsWhenThePaneEnvironmentIsClean(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	consulted := 0
	h := testHost(t, f, func(c *Config) {
		c.ResolvePaneEnviron = func(context.Context, int) ([]string, error) {
			consulted++
			return []string{"PATH=/usr/bin", "TERM=xterm"}, nil
		}
	})
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	if _, err := h.Start(ctx, prepared); err != nil {
		t.Fatalf("Start refused a clean pane: %v", err)
	}
	if consulted != 1 {
		t.Errorf("the pane environment was read %d times, want exactly 1", consulted)
	}
	if n := f.callCount("agent.start"); n != 1 {
		t.Errorf("agent.start called %d times, want 1", n)
	}
	if n := f.callCount("pane.close"); n != 0 {
		t.Errorf("pane.close called %d times on a successful launch", n)
	}
}

// Fail-closed means an environment that cannot be read is a refusal, not a
// pass. Fail-open here would make the gate silently vacuous on exactly the
// deployments where it cannot look — a transcript quietly lost with no
// refusal anywhere, which is the failure §8 exists to stop.
func TestStartRefusesWhenThePaneEnvironmentCannotBeRead(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	readErr := errors.New("read pane environment: permission denied")
	h := testHost(t, f, func(c *Config) { c.ResolvePaneEnviron = environResolver(nil, readErr) })
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	_, err = h.Start(ctx, prepared)
	if err == nil {
		t.Fatal("Start launched into a pane whose environment it could not read")
	}
	var refusal *scrub.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("Start returned %T (%v), want *scrub.RefusalError", err, err)
	}
	if len(refusal.Survivors) != 0 {
		t.Errorf("Survivors = %v, want none: nothing was observed", refusal.Survivors)
	}
	if !errors.Is(err, readErr) {
		t.Error("the underlying read failure is not reachable through errors.Is")
	}
	if n := f.callCount("agent.start"); n != 0 {
		t.Errorf("agent.start called %d times after an unreadable environment", n)
	}
	if n := f.callCount("tab.close"); n != 1 {
		t.Errorf("tab.close called %d times, want 1", n)
	}
}

// A host that names no process for the pane is the same "could not check"
// verdict, decided before the resolver is even called.
func TestStartRefusesWhenTheHostNamesNoProcessForThePane(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	called := false
	h := testHost(t, f, func(c *Config) {
		c.ResolveProcessBirth = func(context.Context, processInfo) host.ProcessBirthEvidence {
			return host.ProcessBirthEvidence{StartTimeSource: StartTimeSourceUnavailable}
		}
		c.ResolvePaneEnviron = func(context.Context, int) ([]string, error) {
			called = true
			return nil, nil
		}
	})
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	if _, err := h.Start(ctx, prepared); err == nil {
		t.Fatal("Start launched into a pane with no known process")
	} else if !strings.Contains(err.Error(), "no process") {
		t.Errorf("refusal = %v, want it to name the missing process", err)
	}
	if called {
		t.Error("the environment resolver ran for a pane with no PID")
	}
}

// The gate reads the environment of the pane's own pre-agent foreground
// process — the shell that will fork the agent — not some other process's.
func TestStartReadsThePanesOwnProcessEnvironment(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	var sawPID int
	h := testHost(t, f, func(c *Config) {
		c.ResolvePaneEnviron = func(_ context.Context, pid int) ([]string, error) {
			sawPID = pid
			return nil, nil
		}
	})
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	evidence, err := h.Start(ctx, prepared)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if sawPID <= 0 || sawPID != evidence.Evidence.ProcessBirth.PID {
		t.Fatalf("gate read pid %d, want the launched pane's process %d",
			sawPID, evidence.Evidence.ProcessBirth.PID)
	}
}

// A teardown that fails does not soften the refusal — but the operator
// hears about the container that is still there. The default placement
// creates a tab, so the injected failure is on tab.close.
func TestRefusalRecordsAFailedTeardown(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	f.setTabCloseError("internal_error", "the server refused to close the tab")
	h := testHost(t, f, func(c *Config) { c.ResolvePaneEnviron = environResolver(markerEnviron(), nil) })
	ctx := context.Background()

	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: testTuple()})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	_, err = h.Start(ctx, prepared)
	var refusal *scrub.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("Start returned %T (%v), want *scrub.RefusalError", err, err)
	}
	if refusal.Cleanup == nil {
		t.Fatal("a failed pane.close left no trace on the refusal")
	}
	if !strings.Contains(err.Error(), "Cleanup after the refusal also failed") {
		t.Errorf("refusal message hides the orphaned pane: %v", err)
	}
}

// The pane path keeps the same teardown guarantee for a pane-target
// launch: the split pane is closed on refusal, and a close failure is
// recorded on the refusal rather than swallowed.
func TestRefusalOnAPaneTargetRecordsAFailedPaneClose(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	f.setPaneCloseError("internal_error", "the server refused to close the pane")
	h := testHost(t, f, func(c *Config) { c.ResolvePaneEnviron = environResolver(markerEnviron(), nil) })
	ctx := context.Background()

	tuple := testTuple()
	tuple.Target = host.LaunchTargetPane
	prepared, err := h.PrepareLaunch(ctx, host.HostLaunchRequest{ResolvedLaunchTuple: tuple})
	if err != nil {
		t.Fatalf("PrepareLaunch: %v", err)
	}
	_, err = h.Start(ctx, prepared)
	var refusal *scrub.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("Start returned %T (%v), want *scrub.RefusalError", err, err)
	}
	if refusal.Cleanup == nil {
		t.Fatal("a failed pane.close left no trace on the refusal")
	}
	if n := f.callCount("tab.close"); n != 0 {
		t.Errorf("tab.close called %d times on a pane-target launch", n)
	}
}

// Duo must not set a marker through the one environment channel it does
// control. This leg refuses in PrepareLaunch, before any server call, so
// it creates nothing it then has to tear down.
func TestPrepareLaunchRefusesARequestEnvironmentThatSetsAMarker(t *testing.T) {
	f := newFakeHerdr(t)
	f.addPane("w1")
	h := testHost(t, f)

	tuple := testTuple()
	tuple.Env = map[string]string{
		"DUO_SESSION":                            "s1",
		scrub.WildcardPrefixes[0] + "CONFIG_DIR": "/tmp/elsewhere",
	}
	_, err := h.PrepareLaunch(context.Background(), host.HostLaunchRequest{ResolvedLaunchTuple: tuple})
	if err == nil {
		t.Fatal("PrepareLaunch accepted a launch request that sets a scrub marker")
	}
	var refusal *scrub.RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("PrepareLaunch returned %T (%v), want *scrub.RefusalError", err, err)
	}
	if calls := f.mutatingCalls(); len(calls) != 0 {
		t.Errorf("PrepareLaunch made mutating calls before refusing: %v", calls)
	}
}

// procfsEnviron is the default resolver. It reads the exec-time
// environment, which is the set a pane inherits and cannot unset.
func TestProcfsEnvironReadsARealProcessEnvironment(t *testing.T) {
	if _, err := os.Stat("/proc/self/environ"); err != nil {
		t.Skip("no procfs on this host")
	}
	environ, err := procfsEnviron(context.Background(), os.Getpid())
	if err != nil {
		t.Fatalf("procfsEnviron: %v", err)
	}
	if len(environ) == 0 {
		t.Fatal("procfsEnviron read no entries for this process")
	}
	for _, e := range environ {
		if !strings.Contains(e, "=") {
			t.Fatalf("entry %q is not KEY=VALUE shaped", e)
		}
	}
}

func TestProcfsEnvironFailsForAProcessItCannotRead(t *testing.T) {
	if _, err := os.Stat("/proc/self/environ"); err != nil {
		t.Skip("no procfs on this host")
	}
	// PID 0 is never a readable process on Linux.
	if _, err := procfsEnviron(context.Background(), 0); err == nil {
		t.Fatal("procfsEnviron invented an environment for pid 0")
	}
}
