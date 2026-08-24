package herdr_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/procrastivity/duo/internal/host/herdr"
)

// seedSession creates one Herdr session directory under root's config tree,
// with an API socket in it when socket is true — the "server is running" and
// "server exited and left its directory behind" cases side by side.
func seedSession(t *testing.T, root, session string, socket bool) string {
	t.Helper()
	dir := filepath.Join(root, herdr.ConfigDirName, herdr.SessionsDirName, session)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", dir, err)
	}
	path := filepath.Join(dir, herdr.SessionSocketName)
	if !socket {
		return ""
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatalf("WriteFile %s: %v", path, err)
	}
	return path
}

// TestDiscoverInstancesReadsTheSessionsLayout pins the convention the
// policy-default deduction rung rests on: one instance per
// $XDG_CONFIG_HOME/herdr/sessions/<session>/herdr.sock, named by its session
// directory, in session order.
func TestDiscoverInstancesReadsTheSessionsLayout(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	beta := seedSession(t, root, "beta", true)
	alpha := seedSession(t, root, "alpha", true)

	found, err := herdr.DiscoverInstances()
	if err != nil {
		t.Fatalf("DiscoverInstances: %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("found %d instances, want 2: %+v", len(found), found)
	}
	if found[0].Session != "alpha" || found[0].SocketPath != alpha {
		t.Errorf("instance 0 = %+v, want session alpha at %s", found[0], alpha)
	}
	if found[1].Session != "beta" || found[1].SocketPath != beta {
		t.Errorf("instance 1 = %+v, want session beta at %s", found[1], beta)
	}
	if found[0].InstanceID != herdr.InstanceIDForSession("alpha") {
		t.Errorf("instance 0 id = %q, want %q", found[0].InstanceID, herdr.InstanceIDForSession("alpha"))
	}
}

// TestDiscoverInstancesSkipsASessionWithNoSocket covers the leftover
// directory Herdr leaves behind when a server exits (observed live,
// internal/scrub/testdata/scrub-live-2026-08-23.md). It addresses nothing,
// so it must not be offered as an instance — a default rung that picked it
// would resolve a launch onto a server that is not there.
func TestDiscoverInstancesSkipsASessionWithNoSocket(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)
	seedSession(t, root, "exited", false)

	found, err := herdr.DiscoverInstances()
	if err != nil {
		t.Fatalf("DiscoverInstances: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found %+v, want no instances", found)
	}
}

// TestDiscoverInstancesOnAMachineWithNoHerdr is the "not installed" answer:
// zero instances and no error. Materialization turns that into its own
// "discovery found no herdr instance" trail line, which is the honest
// message; an error here would report a broken installation instead.
func TestDiscoverInstancesOnAMachineWithNoHerdr(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	found, err := herdr.DiscoverInstances()
	if err != nil {
		t.Fatalf("DiscoverInstances: %v", err)
	}
	if len(found) != 0 {
		t.Fatalf("found %+v, want no instances", found)
	}
}

// TestSessionForInstanceIDRoundTripsAndRejectsALocator pins both halves of
// the convention the fingerprint bridge reads: an ID this package minted
// round-trips, and the `<kind>:<socket path>` locator the launch layer falls
// back to when no rung knew an instance ID does not masquerade as a session
// name.
func TestSessionForInstanceIDRoundTripsAndRejectsALocator(t *testing.T) {
	if got := herdr.SessionForInstanceID(herdr.InstanceIDForSession("alpha")); got != "alpha" {
		t.Errorf("SessionForInstanceID round trip = %q, want %q", got, "alpha")
	}
	for _, id := range []string{
		"herdr:/run/user/1000/herdr/sessions/alpha/herdr.sock",
		"tmux:alpha",
		"herdr:",
		"",
	} {
		if got := herdr.SessionForInstanceID(id); got != "" {
			t.Errorf("SessionForInstanceID(%q) = %q, want \"\"", id, got)
		}
	}
}
