package herdr

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/scrub"
)

// This file is the whole of Duo's spawn-environment consumption point for
// the Herdr host: the place internal/scrub's deny list stops being a
// library and starts refusing launches. Keeping it in one file is
// deliberate — a reader asking "where does the Stage-1 scrub gate
// actually fire?" should find one answer, not three call sites.
//
// # Why this host verifies and refuses instead of scrubbing
//
// internal/scrub offers two scrubbing shapes, and Stage 1's Herdr
// integration can use neither on the live launch path:
//
//   - Shape (a), scrub.Guard, scrubs an environment *before* something
//     starts. It applies to a Herdr server Duo launches itself, whose
//     panes then inherit a clean environment. Stage 1 attaches to a server
//     Duo did not start (Config.SocketPath names an existing socket;
//     nothing in this package spawns a server), so there is no spawn
//     environment here for Guard to build.
//
//   - Shape (b), scrub.PaneCommand, wraps a *command line* so the scrub
//     runs inside the pane at exec time. Herdr takes no command line for
//     an agent: agent.start names a `kind` from a fixed manifest enum and
//     the server resolves the executable (docs/adapters/decisions.md,
//     "Launch mapping is lossy"). There is no argv for PaneCommand to
//     wrap on the method this adapter actually uses.
//
// And Herdr's own env map is one-directional: verified live at 0.8.2, it
// can set a variable in the new pane's shell but cannot unset one the pane
// inherited from the server process. So on this host, at this stage, an
// inherited marker cannot be removed by anything Duo is able to call.
//
// What remains is the one sound fail-closed shape: observe the pane's
// actual inherited environment, and refuse the launch if it carries a
// marker. Conformance §8 makes the scrubbed spawn a hard Stage-1 gate, and
// a gate whose only available verdict is "no" is still a gate — it is
// warning-and-continuing that §8 rules out, not refusing.
//
// # Why after createPane rather than before
//
// The exact environment the agent will inherit belongs to the pane that
// will host it, and that pane does not exist until createPane returns.
// Sampling some *other* existing pane's environment before creating one
// would be a proxy, wrong in both directions (an older pane may have been
// created with a different env map) and unavailable at all in the
// empty-session case, where this adapter creates the workspace itself.
// Start already reads the new pane's pre-agent foreground process for its
// launch-evidence baseline, so the exact answer costs one procfs read and
// a pane.close on the refusal path.

// PaneEnvironResolver reads the environment one pane process actually
// inherited, in os.Environ() shape ("KEY=VALUE" entries).
//
// It is a Config field for the same reason ProcessBirthResolver is: procfs
// is a Linux fact, not a universal one, and a remote Herdr server's panes
// are not readable from this process at all. A deployment that cannot read
// a pane's environment does not get a silently-skipped gate — it gets a
// refusal (see verifyPaneEnvironment) — but it does get an honest seam to
// supply a different source through.
type PaneEnvironResolver func(ctx context.Context, pid int) ([]string, error)

// The two operator-facing phrases this gate refuses with. They are
// constants rather than inline strings so the live gate and its tests
// cannot drift apart on what the operator is told.
const (
	paneEnvironSubject = "the environment the new session-host pane inherited from its server"
	paneEnvironRemedy  = "Restart the session host from a shell that no agent harness is running in, then launch again: a pane inherits the server's environment, and the host's env map can set a pane variable but never unset an inherited one."

	requestEnvSubject = "the environment this launch asks the session host to set on the pane"
	requestEnvRemedy  = "Remove it from the launch request's environment: Duo does not re-introduce a marker it exists to scrub."
)

// verifyRequestEnv is the cheap half of the gate: Duo must not itself set a
// marker through the one environment channel it does control. It runs in
// PrepareLaunch, before anything on the server has been touched, so this
// refusal costs no pane and needs no teardown.
func verifyRequestEnv(env map[string]string) error {
	if len(env) == 0 {
		return nil
	}
	entries := make([]string, 0, len(env))
	for k, v := range env {
		entries = append(entries, k+"="+v)
	}
	return scrub.Gate(requestEnvSubject, requestEnvRemedy, entries)
}

// verifyPaneEnvironment is the load-bearing half: the pane exists, its
// pre-agent foreground process is the shell that will fork the agent, and
// that process's environment is precisely what an inherited marker leaks
// through.
//
// Every failure to observe is a refusal, never a pass. A missing procfs
// entry, a permission error, a process that exited between two calls, or a
// host that reported no PID at all are all "could not check", and this
// package's other unprovable verdict — process birth — already resolves
// the same way (see sameBirth: unproven evidence is never "same").
func (h *Host) verifyPaneEnvironment(ctx context.Context, paneID string, baseline host.ProcessBirthEvidence) error {
	subject := scrub.RefusalSubject(paneEnvironSubject, paneID)
	if baseline.PID <= 0 {
		return scrub.Unreadable(subject, paneEnvironRemedy,
			fmt.Errorf("herdr reported no process for pane %s", paneID))
	}
	environ, err := h.cfg.ResolvePaneEnviron(ctx, baseline.PID)
	if err != nil {
		return scrub.Unreadable(subject, paneEnvironRemedy, err)
	}
	return scrub.Gate(subject, paneEnvironRemedy, environ)
}

// closePaneAfterRefusal tears the just-created pane down so a refused
// launch leaves nothing running. A teardown failure does not turn the
// refusal into a success; it is recorded on the refusal so the operator
// learns about the pane that is still there.
func (h *Host) closePaneAfterRefusal(ctx context.Context, paneID string, refusal error) error {
	err := h.client.call(ctx, "pane.close", paneTargetParams{PaneID: paneID}, nil)
	if err == nil || ErrorCode(err) == CodePaneNotFound {
		return refusal
	}
	var typed *scrub.RefusalError
	if errors.As(refusal, &typed) {
		typed.Cleanup = fmt.Errorf("pane %s could not be closed: %w", paneID, err)
	}
	return refusal
}

// procfsEnviron is the default PaneEnvironResolver: /proc/<pid>/environ,
// the NUL-separated environment the process was exec'd with.
//
// Exec-time, not current, is the right reading for this gate. The variables
// a pane inherits from the Herdr server are fixed at the moment the pane's
// shell execs, and they are exactly the set Herdr's env map cannot unset —
// the leak this gate exists to catch. A variable a user's own shell
// startup file exports afterwards is invisible here; that is a known limit,
// recorded in docs/scrub/decisions.md rather than papered over.
func procfsEnviron(_ context.Context, pid int) ([]string, error) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return nil, fmt.Errorf("read pane environment: %w", err)
	}
	parts := bytes.Split(raw, []byte{0})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if len(p) == 0 {
			continue
		}
		out = append(out, string(p))
	}
	return out, nil
}
