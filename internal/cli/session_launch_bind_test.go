package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/host"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
	"github.com/procrastivity/duo/internal/launchrecord"
)

// bindScenarioYAML is the smallest duo.config/v3 document that resolves:
// one preset, one leaf, one variant. The session host is deliberately
// absent from it — under v3 the document carries host *policy* and never an
// instance, and the instance in every test below comes from the
// materialization ladder instead.
const bindScenarioYAML = `
schema: duo.config/v3
session_hosts:
  prefer: [herdr]
agent_runtimes:
  claude_default:
    kind: claude
    executable: claude
launch_variants:
  daily:
    agent_runtime: claude_default
    model_line: claude-opus-4
    model_family: claude
presets:
  daily:
    selection: ordered
    leaves:
      primary:
        candidates:
          - variant: daily
`

const (
	bindSocket  = "/run/user/1000/herdr/sessions/alpha/herdr.sock"
	bindSession = "alpha"
)

// --- host set doubles ------------------------------------------------------

// spawningHosts is launch.HostSet over the first-class fake host adapter,
// built per tuple so the fake's own integration-instance check passes
// whatever ID the deduction produced.
type spawningHosts struct{ failStart bool }

func (h spawningHosts) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	fake := hostfake.New(t.IntegrationInstanceID)
	if h.failStart {
		return refusingStart{fake}, nil
	}
	return fake, nil
}

// refusingStart prepares a launch exactly as the fake host does and then
// refuses to start it — a live socket that went away between resolution and
// spawn, which is the failure invariant I-3 deliberately defers to this
// point.
type refusingStart struct{ *hostfake.Host }

func (refusingStart) Start(context.Context, host.PreparedHostLaunch) (host.HostLaunchEvidence, error) {
	return host.HostLaunchEvidence{}, errors.New("fake host: the session host refused to start the pane")
}

// --- confirmation doubles --------------------------------------------------

// terminalReader is an iostreams.InteractiveReader: a canned answer from
// something that claims to be a terminal.
type terminalReader struct{ *strings.Reader }

func (terminalReader) IsTerminal() bool { return true }

func interactive(answer string) io.Reader { return terminalReader{strings.NewReader(answer)} }

// --- harness ---------------------------------------------------------------

// bindHarness is one isolated installation: its own store, its own config
// root, and its own streams. Everything a launch would touch is inside
// t.TempDir(), so no test sees a developer's real Herdr session or duo.db.
type bindHarness struct {
	t         *testing.T
	root      string
	authority *domain.Authority
	store     io.Closer
	closed    bool
	streams   *iostreams.Streams
	out       *bytes.Buffer
	err       *bytes.Buffer
	doc       config.DocumentV3
}

// close releases the authority-writer lease the harness holds, so a
// separate `duo` invocation (runSession) can open the same installation.
// The registered cleanup calls it too, so a test that never needs a second
// process pays no attention to it.
func (h *bindHarness) close() {
	if h.closed {
		return
	}
	h.closed = true
	if err := h.store.Close(); err != nil {
		h.t.Fatalf("closing the authority store: %v", err)
	}
}

func newBindHarness(t *testing.T, in io.Reader) *bindHarness {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	doc, err := config.ParseV3([]byte(bindScenarioYAML))
	if err != nil {
		t.Fatalf("ParseV3: %v", err)
	}

	a, store, err := openWriteAuthority(context.Background())
	if err != nil {
		t.Fatalf("openWriteAuthority: %v", err)
	}

	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	h := &bindHarness{
		t:         t,
		root:      t.TempDir(),
		authority: a,
		store:     store,
		streams:   &iostreams.Streams{In: in, Out: out, Err: errOut},
		out:       out,
		err:       errOut,
		doc:       doc,
	}
	t.Cleanup(h.close)
	return h
}

// materializeWith runs the real M1/M2 pass against this harness's authority.
// hostFlag and env choose which rung wins; nothing here is stubbed but the
// process environment, which is injected so a test never depends on the
// developer's own HERDR_* variables.
func (h *bindHarness) materializeWith(hostFlag string, env map[string]string) materialize.Result {
	h.t.Helper()
	mat, err := materialize.Materialize(context.Background(), materialize.Options{
		WorkspaceFlag:   h.root,
		HostFlag:        hostFlag,
		RequestedPreset: "daily",
		Policy:          h.doc.SessionHosts,
		Correlations:    h.authority,
		Providers:       h.authority,
		Discovery:       stage1Discovery{},
		LookupEnv: func(name string) (string, bool) {
			v, ok := env[name]
			return v, ok
		},
	})
	if err != nil {
		h.t.Fatalf("Materialize: %v", err)
	}
	return mat
}

// launch runs the whole post-resolution path under test: resolve, commit,
// spawn, and — only on success — the cold-start first bind.
func (h *bindHarness) launch(mat materialize.Result, hosts launch.HostSet, dryRun bool) (launch.Report, error) {
	h.t.Helper()
	resolver, err := launch.NewResolver(h.doc, mat, launch.Options{
		Support: launch.AllSupported{RecordDigest: "sha256:test"},
	})
	if err != nil {
		h.t.Fatalf("NewResolver: %v", err)
	}
	recorder, err := launchrecord.New(h.authority, launchrecord.Options{
		WorkspacePath: mat.WorkspacePath(),
		Actor:         "user:test",
	})
	if err != nil {
		h.t.Fatalf("launchrecord.New: %v", err)
	}
	launcher, err := launch.NewLauncher(resolver, recorder, hosts)
	if err != nil {
		h.t.Fatalf("NewLauncher: %v", err)
	}
	return launchAndBind(context.Background(), h.streams, h.authority, launcher, launch.SpawnRequest{
		Request:       launch.Request{Preset: "daily", RequestID: "req_test", Caller: "user:test"},
		WorkspacePath: mat.WorkspacePath(),
		DryRun:        dryRun,
	}, mat, "user:test")
}

// correlation returns this harness's workspace↔host correlation, and whether
// one was ever written.
func (h *bindHarness) correlation() (domain.HostCorrelation, bool) {
	h.t.Helper()
	ws, ok := h.authority.WorkspaceForRoot(h.root)
	if !ok {
		return domain.HostCorrelation{}, false
	}
	return h.authority.HostCorrelation(ws.ID)
}

// ambientEnv is the environment of a pane a Herdr server published into.
func ambientEnv() map[string]string {
	return map[string]string{"HERDR_SOCKET_PATH": bindSocket, "HERDR_SESSION": bindSession}
}

// --- the bind rule ---------------------------------------------------------

// TestBindIsWrittenOnlyAfterStartSucceeds is the ordering this step owes,
// one level above invariant I-1's record-before-spawn: a correlation attests
// where a *running* session went, so a spawn that never started writes no
// fact. The launch-resolution record still stands (§7.4), which is what
// makes the assertion meaningful — something was committed, and the bind was
// still not.
func TestBindIsWrittenOnlyAfterStartSucceeds(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	if _, err := h.launch(mat, spawningHosts{failStart: true}, false); err == nil {
		t.Fatal("launch succeeded, want the host's Start refusal")
	}
	if c, bound := h.correlation(); bound {
		t.Fatalf("a host correlation was written after a failed Start: %+v", c.Binding)
	}
}

// TestExplicitFlagBindWritesWithLoudOutput is notes/43 item 13's
// declared-intent half: the operator named the host, so the bind is not
// taxed with a question — but it is announced, with the audited verb that
// changes it.
func TestExplicitFlagBindWritesWithLoudOutput(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	if _, err := h.launch(mat, spawningHosts{}, false); err != nil {
		t.Fatalf("launch: %v", err)
	}

	c, bound := h.correlation()
	if !bound {
		t.Fatal("no host correlation was written after a successful explicit-flag launch")
	}
	if c.Binding.Source != domain.HostSourceExplicitFlag {
		t.Errorf("binding source = %q, want %q", c.Binding.Source, domain.HostSourceExplicitFlag)
	}
	if c.Binding.Locator() != "herdr:"+bindSocket {
		t.Errorf("binding locator = %q, want %q", c.Binding.Locator(), "herdr:"+bindSocket)
	}
	if !c.Binding.Fingerprint.Present() {
		t.Error("the binding carries no fingerprint evidence")
	}

	loud := h.err.String()
	for _, want := range []string{"host bound", bindSocket, string(domain.HostSourceExplicitFlag), "duo workspace host rebind"} {
		if !strings.Contains(loud, want) {
			t.Errorf("bind output does not name %q:\n%s", want, loud)
		}
	}
}

// TestAmbientBindIsRefusedWithoutConfirmation is the hazard half: an
// ambient-env bind on a non-interactive run asks nobody and writes nothing.
// The launch itself still succeeded, and the output has to say both.
func TestAmbientBindIsRefusedWithoutConfirmation(t *testing.T) {
	h := newBindHarness(t, nil) // no In at all: nothing to ask
	mat := h.materializeWith("", ambientEnv())
	if mat.Host().Source != domain.HostSourceAmbientEnv {
		t.Fatalf("host_source = %q, want %q", mat.Host().Source, domain.HostSourceAmbientEnv)
	}

	if _, err := h.launch(mat, spawningHosts{}, false); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if c, bound := h.correlation(); bound {
		t.Fatalf("an ambient-env bind was written without confirmation: %+v", c.Binding)
	}

	said := h.err.String()
	for _, want := range []string{"host binding skipped", "the launch itself succeeded", "--host"} {
		if !strings.Contains(said, want) {
			t.Errorf("refusal output does not say %q:\n%s", want, said)
		}
	}
}

// TestAmbientBindIsWrittenWhenConfirmed is the same rung answered "y" on a
// terminal: the question is the whole safeguard, so a person who answers it
// gets the bind.
func TestAmbientBindIsWrittenWhenConfirmed(t *testing.T) {
	h := newBindHarness(t, interactive("y\n"))
	mat := h.materializeWith("", ambientEnv())

	if _, err := h.launch(mat, spawningHosts{}, false); err != nil {
		t.Fatalf("launch: %v", err)
	}

	c, bound := h.correlation()
	if !bound {
		t.Fatalf("no correlation was written after a confirmed ambient-env bind:\n%s", h.err.String())
	}
	if c.Binding.Source != domain.HostSourceAmbientEnv {
		t.Errorf("binding source = %q, want %q", c.Binding.Source, domain.HostSourceAmbientEnv)
	}
	if c.Binding.InstanceID != herdr.InstanceIDForSession(bindSession) {
		t.Errorf("binding instance id = %q, want %q", c.Binding.InstanceID, herdr.InstanceIDForSession(bindSession))
	}
	if c.Binding.Fingerprint.SessionName != bindSession {
		t.Errorf("fingerprint session name = %q, want %q", c.Binding.Fingerprint.SessionName, bindSession)
	}
	if !strings.Contains(h.err.String(), "[y/N]") {
		t.Errorf("no confirmation prompt was written:\n%s", h.err.String())
	}
}

// TestAmbientBindIsRefusedWhenDeclined pins the default: anything but an
// explicit yes leaves the workspace unbound.
func TestAmbientBindIsRefusedWhenDeclined(t *testing.T) {
	h := newBindHarness(t, interactive("\n"))
	mat := h.materializeWith("", ambientEnv())

	if _, err := h.launch(mat, spawningHosts{}, false); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if c, bound := h.correlation(); bound {
		t.Fatalf("a declined ambient-env bind was written anyway: %+v", c.Binding)
	}
}

// TestDryRunWritesNoRecordAndNoBind is §6.10 plus this step's addition: a
// preview shows the deduction and writes nothing at all — no session, no
// launch-resolution record, and no correlation.
func TestDryRunWritesNoRecordAndNoBind(t *testing.T) {
	h := newBindHarness(t, interactive("y\n")) // even with a yes waiting
	mat := h.materializeWith("herdr:"+bindSocket, nil)

	report, err := h.launch(mat, spawningHosts{}, true)
	if err != nil {
		t.Fatalf("launch --dry-run: %v", err)
	}
	if !report.Preview {
		t.Error("the report is not marked as a preview")
	}
	if report.LaunchResolutionID != "" {
		t.Errorf("launch_resolution_id = %q, want empty on a dry run", report.LaunchResolutionID)
	}
	if report.SessionID != "" {
		t.Errorf("session_id = %q, want empty on a dry run", report.SessionID)
	}
	if _, ok := h.authority.WorkspaceForRoot(h.root); ok {
		t.Error("a dry run enrolled a workspace")
	}
	if c, bound := h.correlation(); bound {
		t.Fatalf("a dry run wrote a host correlation: %+v", c.Binding)
	}
	// The deduction is still shown: that is the whole point of previewing.
	if report.Host == nil || report.Host.InstanceLabel != bindSocket {
		t.Fatalf("report.host = %+v, want the deduced instance %s", report.Host, bindSocket)
	}
}

// TestASecondLaunchDoesNotRebind is the boundary between the first bind and
// the audited rebind: once a workspace has a correlation, the launch path
// never touches it again, whatever the deduction produced.
func TestASecondLaunchDoesNotRebind(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)
	if _, err := h.launch(mat, spawningHosts{}, false); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	first, bound := h.correlation()
	if !bound {
		t.Fatal("the first launch wrote no correlation")
	}

	// A second launch from a pane belonging to a different server. The
	// correlation outranks it (thread 3), and nothing rebinds.
	second := h.materializeWith("", map[string]string{
		"HERDR_SOCKET_PATH": "/run/user/1000/herdr/sessions/other/herdr.sock",
		"HERDR_SESSION":     "other",
	})
	if second.Host().Source != domain.HostSourceWorkspaceCorrelation {
		t.Fatalf("host_source = %q, want %q", second.Host().Source, domain.HostSourceWorkspaceCorrelation)
	}
	if _, err := h.launch(second, spawningHosts{}, false); err != nil {
		t.Fatalf("second launch: %v", err)
	}

	after, _ := h.correlation()
	if after.FactID != first.FactID {
		t.Errorf("the correlation fact changed from %s to %s; only the audited rebind may do that",
			first.FactID, after.FactID)
	}
}

// TestCorrelationNoteNamesTheOutrankedPaneAndTheRebindPath is thread 3
// (nested launch), the residual footgun of "correlation beats the enclosing
// pane by design": the launch is correct and would otherwise be silent, so
// the output has to name both instances and the verb that changes the
// binding.
func TestCorrelationNoteNamesTheOutrankedPaneAndTheRebindPath(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)
	if _, err := h.launch(mat, spawningHosts{}, false); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	h.err.Reset()

	otherSocket := "/run/user/1000/herdr/sessions/other/herdr.sock"
	second := h.materializeWith("", map[string]string{
		"HERDR_SOCKET_PATH": otherSocket,
		"HERDR_SESSION":     "other",
	})
	report, err := h.launch(second, spawningHosts{}, false)
	if err != nil {
		t.Fatalf("second launch: %v", err)
	}
	writeCorrelationNote(h.streams, report.Host)

	note := h.err.String()
	for _, want := range []string{bindSocket, otherSocket, "HERDR_SOCKET_PATH", "duo workspace host rebind"} {
		if !strings.Contains(note, want) {
			t.Errorf("the nested-launch note does not name %q:\n%s", want, note)
		}
	}
}

// TestNoCorrelationNoteWhenNothingWasOutranked keeps the note from becoming
// noise: an ordinary launch in a bound workspace, run from outside any pane,
// outranked nothing and has nothing to warn about.
func TestNoCorrelationNoteWhenNothingWasOutranked(t *testing.T) {
	h := newBindHarness(t, nil)
	mat := h.materializeWith("herdr:"+bindSocket, nil)
	if _, err := h.launch(mat, spawningHosts{}, false); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	h.err.Reset()

	second := h.materializeWith("", nil)
	report, err := h.launch(second, spawningHosts{}, false)
	if err != nil {
		t.Fatalf("second launch: %v", err)
	}
	writeCorrelationNote(h.streams, report.Host)

	if note := h.err.String(); note != "" {
		t.Errorf("a nested-launch note was written for a launch that outranked nothing:\n%s", note)
	}
}
