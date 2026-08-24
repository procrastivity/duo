package herdr

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// instances.go answers "which Herdr servers exist on this machine", which
// is the one question host.HostDiscovery cannot answer.
//
// That contract (§5.2) discovers transient runtime candidates *inside* one
// already-addressed host: building a Host needs a socket path before
// Discover can be called at all. The launch materializer's policy-default
// rung, and the kind-only `--host herdr` form, both ask the prior question
// — which instances of this kind are there — so it is answered here, in
// the package that owns the Herdr convention, and injected into the
// materializer as an interface it defines (duo-config-v3 step 11's
// InstanceDiscovery).
//
// Enumerating is not attesting (invariant I-3). Nothing below dials a
// socket, sends a ping, or otherwise proves a server is alive: a socket
// file that is there is an instance, and a dead one fails at spawn, which
// is the only place that failure is honest.

// instanceIDPrefix is the one place the Herdr integration-instance ID's
// shape is written down. InstanceIDForSession builds one and
// SessionForInstanceID reads one back, so the two can never disagree.
const instanceIDPrefix = "herdr:"

// SessionsDirName, SessionSocketName, and the layout they compose are
// Herdr 0.8.2's observed on-disk convention:
//
//	$XDG_CONFIG_HOME/herdr/sessions/<session>/herdr.sock
//
// It is the layout internal/scrub's live test builds a disposable server
// at (scrub/live_test.go), and the one HERDR_SOCKET_PATH points into
// inside a pane. herdr-client.sock, which lives beside it, is the UI
// transport and carries no API — only herdr.sock is an addressable
// instance.
const (
	// ConfigDirName is Herdr's directory under the XDG config root.
	ConfigDirName = "herdr"
	// SessionsDirName holds one directory per named Herdr session.
	SessionsDirName = "sessions"
	// SessionSocketName is the API socket inside a session directory.
	SessionSocketName = "herdr.sock"
)

// InstanceIDForSession is the conventional integration-instance ID for a
// Herdr session name. Herdr has no server identity of its own, so the
// session name — which is what selects a socket — is the identity Duo
// records.
func InstanceIDForSession(session string) string {
	return instanceIDPrefix + session
}

// SessionForInstanceID is InstanceIDForSession's inverse: the Herdr session
// name inside an integration-instance ID, or "" for a value this convention
// did not produce.
//
// It exists for the launch layer's evidence bridge, which has to put a
// session name in a domain.HostFingerprint and has only the instance ID to
// read it from (host.Evidence carries no session name; §5.2's identity core
// is epoch, container, and process birth).
//
// A remainder containing a path separator is rejected rather than returned.
// That is not defensiveness about malformed input: when no deduction rung
// knew an instance ID, the launch layer falls back to the `<kind>:<instance>`
// locator, whose instance half is a socket *path* — and a socket path is
// not a session name. A Herdr session name is one directory component
// (SessionsDir's layout), so the separator is exactly what tells the two
// apart.
func SessionForInstanceID(id string) string {
	if len(id) <= len(instanceIDPrefix) || id[:len(instanceIDPrefix)] != instanceIDPrefix {
		return ""
	}
	session := id[len(instanceIDPrefix):]
	if strings.ContainsAny(session, `/\`) {
		return ""
	}
	return session
}

// Instance is one Herdr server found on disk: its session name, the API
// socket that addresses it, and the integration-instance ID Duo scopes
// evidence by.
type Instance struct {
	Session    string
	SocketPath string
	InstanceID string
}

// SessionsDir returns $XDG_CONFIG_HOME/herdr/sessions, falling back to
// ~/.config when XDG_CONFIG_HOME is unset — the same resolution
// internal/asset applies to Duo's own override directory, because it is the
// same base directory specification.
func SessionsDir() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("herdr: resolving the XDG_CONFIG_HOME fallback: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, ConfigDirName, SessionsDirName), nil
}

// DiscoverInstances lists every Herdr server whose API socket exists under
// SessionsDir, in session-name order.
//
// A missing sessions directory is zero instances, not an error: a machine
// with no Herdr installed has none, and that is an answer. An empty session
// directory is skipped for the same reason — Herdr leaves one behind when a
// server exits (observed live, internal/scrub/testdata), and a directory
// with no socket in it addresses nothing.
func DiscoverInstances() ([]Instance, error) {
	dir, err := SessionsDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("herdr: reading %s: %w", dir, err)
	}

	out := make([]Instance, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		socket := filepath.Join(dir, entry.Name(), SessionSocketName)
		if _, err := os.Stat(socket); err != nil {
			// A session directory with no socket is a server that is not
			// running (or never finished starting). Not an error, and not
			// an instance.
			continue
		}
		out = append(out, Instance{
			Session:    entry.Name(),
			SocketPath: socket,
			InstanceID: InstanceIDForSession(entry.Name()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Session < out[j].Session })
	return out, nil
}
