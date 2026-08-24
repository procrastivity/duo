package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/asset"
	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/host"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
	"github.com/procrastivity/duo/internal/launchrecord"
	"github.com/procrastivity/duo/internal/runtime/claude"
	"github.com/procrastivity/duo/internal/runtime/pi"
	"github.com/procrastivity/duo/internal/scrub"
	"github.com/procrastivity/duo/internal/surface"
)

// defaultLaunchConfigPath resolves the duo.config/v3 launch-preset document
// the launch resolver reads. No planning document fixes a normative default
// path yet (Step 24, "the user's real duo.config/v3", is where the shipped
// dogfood document lands); this follows internal/asset and internal/doctor's
// existing "resolve under $XDG_CONFIG_HOME/duo" convention rather than
// invent a new one. --config overrides it. See the step-21 wip findings.
func defaultLaunchConfigPath() (string, error) {
	dir, err := asset.OverrideDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "duo.config.yaml"), nil
}

// sessionLaunchCommand constructs `duo session launch`: internal/registry's
// "session.launch" operation, CLI path {"session", "launch"}. It is wired
// directly against internal/launch's resolver (Step 15) and
// internal/launchrecord's Recorder (the launch.Recorder that commits the
// launch-resolution record and the Duo identities it explains in one
// domain transaction, before any spawn) — §4.2's launch-resolution
// boundary, entered exactly once per launch.
//
// This operation is registered local_admin (internal/registry: "an
// in-session agent that can launch more agents is a self-activation and
// resource-amplification loop"), so it never gets an MCP tool — nothing
// here adds one.
func sessionLaunchCommand(streams *iostreams.Streams) *cobra.Command {
	var (
		workspace    string
		hostFlag     string
		targetFlag   string
		configPath   string
		requireFlags []string
		avoidFlags   []string
		dryRun       bool
		owner, actor string
		bindActor    string
		closeOnExit  bool
	)

	cmd := &cobra.Command{
		Use:   "launch <preset>",
		Short: "resolve and launch a named preset (Herdr+Claude Code or Herdr+Pi, Stage 1)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cliflags.FromContext(cmd.Context()).Output

			path := configPath
			if path == "" {
				p, err := defaultLaunchConfigPath()
				if err != nil {
					return duoerr.New("internal.config_path_unresolved", fmt.Sprintf("resolving the default duo.config/v3 path: %v", err))
				}
				path = p
			}
			doc, err := config.LoadV3(path)
			if err != nil {
				return err // already a *duoerr.Error (internal/config wraps every failure)
			}

			require, err := parseConstraints(requireFlags)
			if err != nil {
				return err
			}
			avoid, err := parseConstraints(avoidFlags)
			if err != nil {
				return err
			}

			target, err := parseLaunchTarget(targetFlag)
			if err != nil {
				return err
			}

			// The authority opens before materialization, not after: M1's
			// correlation rung and M2's provider snapshot both read it, and
			// a dry run must read exactly what a real launch would. A dry
			// run opens read-only, so previewing a launch never takes the
			// authority-writer lease and never creates a store.
			a, closer, err := openLaunchAuthority(cmd.Context(), !dryRun)
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()

			// M1/M2 (internal/launch/materialize, step 11) with the real
			// read models: *domain.Authority satisfies both narrow
			// interfaces, and stage1Discovery enumerates the session-host
			// instances this build has an adapter for. Nothing here checks
			// that the deduced instance is reachable (I-3).
			mat, err := materialize.Materialize(cmd.Context(), materialize.Options{
				WorkspaceFlag:   workspace,
				HostFlag:        hostFlag,
				RequestedPreset: args[0],
				Policy:          doc.SessionHosts,
				Correlations:    a,
				Providers:       a,
				Discovery:       stage1Discovery{},
				Roots:           stage1Discovery{},
			})
			if err != nil {
				return launchFailure(streams, mode, err)
			}

			resolver, err := launch.NewResolver(doc, mat, launch.Options{
				Support:      stage1Support{},
				Random:       launch.CryptoSource{},
				HostVersions: stage1HostVersions(),
			})
			if err != nil {
				return duoerr.New("internal.launch_resolver_build_failed", err.Error())
			}

			req := launch.Request{
				Preset:    args[0],
				Require:   require,
				Avoid:     avoid,
				RequestID: requestID(),
				Caller:    actor,
			}

			var report launch.Report
			if dryRun {
				resolution, err := resolver.Resolve(req)
				if err != nil {
					return launchFailure(streams, mode, err)
				}
				report = resolution.Report()
				report.LaunchResolutionID = "" // §6.10: a dry run commits no record.
				report.Preview = true
			} else {
				recorder, err := launchrecord.New(a, launchrecord.Options{
					WorkspacePath: mat.WorkspacePath(),
					Actor:         actor,
					Owner:         owner,
					BindActor:     domain.ActorID(bindActor),
				})
				if err != nil {
					return duoerr.New("internal.launch_recorder_build_failed", err.Error())
				}
				launcher, err := launch.NewLauncher(resolver, recorder, stage1HostSet{}, launch.WithLeafAugmenter(stage1LeafAugmenter{}))
				if err != nil {
					return duoerr.New("internal.launcher_build_failed", err.Error())
				}
				report, err = launchAndBind(cmd.Context(), streams, a, launcher, launch.SpawnRequest{
					Request:       req,
					WorkspacePath: mat.WorkspacePath(),
					Target:        target,
					CloseOnExit:   closeOnExit,
				}, mat, actor)
				if err != nil {
					return launchFailure(streams, mode, err)
				}
			}

			writeCorrelationNote(streams, report.Host)

			if mode == "json" {
				b, err := json.Marshal(newEnvelope("session.launch", report))
				if err != nil {
					return duoerr.New("internal.session_launch_encode_failed", err.Error())
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}
			return renderLaunchReportText(streams, report)
		},
	}

	cmd.Flags().StringVar(&workspace, "workspace", "", "the launched execution's working directory (defaults to the current directory)")
	cmd.Flags().StringVar(&hostFlag, "host", "",
		`the session host to launch into, "<kind>" or "<kind>:<instance>": deduction rung 0, outranking the workspace correlation, the ambient environment, and the policy default`)
	cmd.Flags().StringVar(&targetFlag, "target", "",
		`where the launched agent's container is created in the deduced host, "tab" or "pane": a placement override like --host, not a constraint axis (defaults to the host's built-in placement; provisional pending change control, see notes/44)`)
	cmd.Flags().BoolVar(&closeOnExit, "close-on-exit", false,
		`close the launched agent's host-side container when the agent exits cleanly, from an agent-side SessionEnd hook -- never a watcher, send-keys, or shell injection (crashes and kills leave the container open by design; provisional pending change control, see notes/46)`)
	cmd.Flags().StringVar(&configPath, "config", "", "path to the duo.config/v3 document (defaults to $XDG_CONFIG_HOME/duo/duo.config.yaml)")
	cmd.Flags().StringArrayVar(&requireFlags, "require", nil, "a non-relenting launch constraint, axis=value (agent_runtime, model_line, or model_family); repeatable")
	cmd.Flags().StringArrayVar(&avoidFlags, "avoid", nil, "a soft launch constraint, axis=value; repeatable")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "preview the resolution: no durable record, no session, no spawn")
	cmd.Flags().StringVar(&owner, "owner", "", "the session's owning subject (defaults to --actor)")
	cmd.Flags().StringVar(&actor, "actor", "cli", "the responsible actor recorded on every fact, and the record's caller")
	cmd.Flags().StringVar(&bindActor, "bind-actor", "", "bind a durable agent actor at launch")

	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// parseConstraints parses repeated "axis=value" flag values into launch
// constraints, provenance "flag" (notes/30 §6.6: repeated equal constraints
// normalize while preserving provenance).
func parseConstraints(raw []string) ([]launch.Constraint, error) {
	out := make([]launch.Constraint, 0, len(raw))
	for _, r := range raw {
		axis, value, ok := strings.Cut(r, "=")
		if !ok || axis == "" || value == "" {
			return nil, duoerr.New("invalid.request", fmt.Sprintf("constraint %q must be axis=value.", r))
		}
		a := launch.Axis(axis)
		if !a.Valid() {
			return nil, duoerr.New("invalid.request",
				fmt.Sprintf("constraint axis %q is not a declared axis (agent_runtime, model_line, model_family).", axis))
		}
		out = append(out, launch.Constraint{Axis: a, Value: value, Source: "flag"})
	}
	return out, nil
}

// parseLaunchTarget parses --target: a placement override on the deduced
// host's containment model, deliberately not a fourth constraint axis
// (the --host precedent: an override never joins require/avoid).
func parseLaunchTarget(raw string) (host.LaunchTarget, error) {
	switch host.LaunchTarget(raw) {
	case "", host.LaunchTargetTab, host.LaunchTargetPane:
		return host.LaunchTarget(raw), nil
	}
	return "", duoerr.New("invalid.request",
		fmt.Sprintf("launch target %q is not a placement this build knows (tab, pane).", raw))
}

// openLaunchAuthority opens the authority store for one launch: as the
// durable writer for a real launch, read-only for a dry run.
//
// A dry run reads the same two models a real launch does — the workspace↔
// host correlation M1 consults and the standing provider facts M2
// snapshots — so its preview is the decision the real launch would make.
// What it must not do is take the authority-writer lease or create a store
// that was not there: §6.10 makes a dry run create no durable anything, and
// a preview that locked out a concurrent duo would be a write in all but
// name.
func openLaunchAuthority(ctx context.Context, write bool) (*domain.Authority, io.Closer, error) {
	if !write {
		return openReadAuthority(ctx)
	}
	a, s, err := openWriteAuthority(ctx)
	if err != nil {
		return nil, nil, err
	}
	// A typed nil *store.Store would make the returned io.Closer non-nil,
	// so the success path is the only one that wraps it.
	var closer io.Closer = s
	return a, closer, nil
}

// launchAndBind is the whole of the post-resolution path: spawn, and then —
// only if the spawn succeeded — the per-leaf host attachment and the
// cold-start first bind, in that order.
//
// They live in one function so the ordering is a property of the code
// rather than of the order two statements happen to appear in a command
// handler. Launcher.Launch already makes "record before spawn" unforgeable
// (its unexported `committed` token, invariant I-1); this makes "spawn
// before recording what the spawn proved" just as plain: neither
// post-spawn write is reachable from a Launch that returned an error, so a
// failing Start records no attachment and no correlation fact.
//
// The attachment goes first because it is the session's own fact and asks
// nobody anything, while the first bind may stop to confirm an ambient-env
// deduction on the terminal.
func launchAndBind(
	ctx context.Context,
	streams *iostreams.Streams,
	a *domain.Authority,
	launcher *launch.Launcher,
	req launch.SpawnRequest,
	mat materialize.Result,
	actor string,
) (launch.Report, error) {
	result, err := launcher.Launch(ctx, req)
	if err != nil {
		// A resolution failure recorded nothing; a spawn failure after the
		// commit left the record standing (§7.4). Neither is a session
		// running on the deduced host, and only a running session is
		// evidence a correlation may rest on.
		return launch.Report{}, err
	}
	recordLaunchAttachments(ctx, streams, a, mat, result, actor)
	bindFirstHost(ctx, streams, a, mat, result, actor)
	return result.Report, nil
}

// launchFailure renders whatever a failure owes the operator beyond its
// message, then projects it onto the chassis's structured error.
//
// In text mode the pointer set goes to stderr as typed commands, because
// duoerr.Render's human line carries the message and nothing else. In JSON
// mode nothing is written here: the whole safe detail payload — tallies,
// deduced host, evidence bundle, pointers — rides the error envelope
// duoerr.Render emits, which is where a machine reader expects it.
func launchFailure(streams *iostreams.Streams, mode string, err error) *duoerr.Error {
	de := launchDuoErr(err)
	if mode != "json" {
		writeFailureRail(streams, de.Details)
	}
	return de
}

// launchDuoErr projects a launch-resolution failure onto the chassis's
// structured error.
//
// A *launch.Error and a *materialize.Error each carry their own registered
// stable code and their own duo.external/v1 safe details, which travel with
// the projection so --output json emits the whole object. A
// *scrub.RefusalError is the spawn-environment gate tripping, which is a
// guard refusal and not an internal failure. An already-structured
// *duoerr.Error passes through unchanged — ParseHostFlag raises one for a
// malformed --host, and re-wrapping it would bury a caller-correctable
// grammar error under an internal code. Anything else reached here from
// Launcher.Launch's host-side spawn step, which this package's host set and
// Stage-1 support oracle raise as plain errors.
func launchDuoErr(err error) *duoerr.Error {
	var lerr *launch.Error
	if errors.As(err, &lerr) {
		return lerr.Duo().WithDetails(lerr.Details)
	}
	var merr *materialize.Error
	if errors.As(err, &merr) {
		return merr.Duo().WithDetails(merr.Details)
	}
	var refusal *scrub.RefusalError
	if errors.As(err, &refusal) {
		// "refusal." is the chassis's exit-3 prefix (internal/exitcode, and
		// docs/cli/decisions.md's mapping): a guard tripped, the operator
		// can act on it, and nothing partial was left behind. The code is a
		// local diagnostic token like refusal.session_guard, not a member of
		// internal/registry's closed stable set — no planning document
		// registers a v1 wire code for this gate.
		return duoerr.New("refusal.spawn_environment", refusal.Error())
	}
	var derr *duoerr.Error
	if errors.As(err, &derr) {
		return derr
	}
	return duoerr.New("internal.launch_failed", err.Error())
}

// failureRail is the part of a launch failure's safe details this package
// re-renders for a human: the deduced host and the pointer set.
//
// It is decoded out of the details payload rather than type-asserted onto
// it, and that is deliberate. internal/launch's failureDetails and
// internal/launch/materialize's HostUnresolvedDetails are two unexported
// (or package-local) shapes that agree on exactly these duo.external/v1
// members; going through the wire encoding reads what the contract fixes,
// and cannot bind this renderer to either package's Go type.
type failureRail struct {
	Host struct {
		Kind          string `json:"kind"`
		InstanceLabel string `json:"instance_label"`
		HostSource    string `json:"host_source"`
	} `json:"host"`
	Pointers *materialize.PointerSet `json:"pointers"`
}

// writeFailureRail prints the deduced host and the pointer set — the ways
// out — on stderr.
//
// The pointer set is `details.pointers` verbatim: the same three members
// the JSON envelope carries, in the same words, so an operator reading the
// human output and one reading the envelope are told to type the same
// things (duo-vnext-projection-contracts.md §2.1's launch-verb block).
func writeFailureRail(streams *iostreams.Streams, details any) {
	if details == nil {
		return
	}
	raw, err := json.Marshal(details)
	if err != nil {
		return
	}
	var rail failureRail
	if err := json.Unmarshal(raw, &rail); err != nil {
		return
	}

	if rail.Host.Kind != "" {
		_, _ = fmt.Fprintf(streams.Err, "duo: host:      %s:%s (host_source=%s)\n",
			rail.Host.Kind, rail.Host.InstanceLabel, rail.Host.HostSource)
	}
	if rail.Pointers == nil || *rail.Pointers == (materialize.PointerSet{}) {
		return
	}
	_, _ = fmt.Fprintln(streams.Err, "duo: ways out:")
	for _, pointer := range []struct{ label, value string }{
		{"override flag", rail.Pointers.OverrideFlag},
		{"provider enable", rail.Pointers.ProviderEnable},
		{"workspace host rebind", rail.Pointers.WorkspaceHostRebind},
	} {
		if pointer.value == "" {
			continue
		}
		_, _ = fmt.Fprintf(streams.Err, "duo:   %-22s %s\n", pointer.label+":", pointer.value)
	}
}

// writeCorrelationNote is thread 3 (nested launch), which this step owns.
//
// A persisted correlation outranks the ambient environment by design: it is
// intent, the pane is accident, and notes/19 §0's inherited-socket footgun
// is why Duo inverts Herdr's own precedence. The cost of that choice is
// that a Duo run from inside pane B, in a workspace bound to server A,
// launches into A — correctly, and invisibly, unless the output says so.
//
// So whenever the correlation — recorded or cwd-deduced — won over a
// captured ambient environment, the note names both instances and the
// audited verb that changes the binding. It goes to stderr in both output
// modes: --output json's stdout carries exactly one envelope, and the
// machine reader already has the same facts in
// `result.host.outranked_evidence`.
func writeCorrelationNote(streams *iostreams.Streams, host *launch.WireHost) {
	if host == nil {
		return
	}
	var chose string
	switch host.HostSource {
	case string(domain.HostSourceWorkspaceCorrelation):
		chose = "this workspace's recorded correlation chose"
	case string(domain.HostSourceCwdCorrelation):
		chose = "the session claiming this directory chose"
	default:
		return
	}
	for _, outranked := range host.OutrankedEvidence {
		if outranked.Source != string(domain.HostSourceAmbientEnv) || len(outranked.Captures) == 0 {
			continue
		}
		_, _ = fmt.Fprintf(streams.Err,
			"duo: %s %s:%s and outranked the ambient environment of the pane you are in.\n",
			chose, host.Kind, host.InstanceLabel)
		for _, capture := range outranked.Captures {
			_, _ = fmt.Fprintf(streams.Err, "duo:   outranked: %s=%s\n", capture.Name, capture.Value)
		}
		_, _ = fmt.Fprintf(streams.Err, "duo: %s\n", rebindPointer(host.Kind+":"+host.InstanceLabel))
		return
	}
}

func renderLaunchReportText(streams *iostreams.Streams, r launch.Report) error {
	var b strings.Builder
	if r.Preview {
		b.WriteString("preview only: no session, no durable record, no spawn\n")
	} else {
		fmt.Fprintf(&b, "session:  %s\nrecord:   %s\n", r.SessionID, r.LaunchResolutionID)
	}
	writeDeducedHostLines(&b, r.Host)
	fmt.Fprintf(&b, "selection: %s\n", r.Selection)
	for _, leaf := range r.Leaves {
		fmt.Fprintf(&b, "  %s: %s / %s (%s) -> %s\n",
			leaf.Name, leaf.AgentRuntime, leaf.ModelLine, leaf.DeclaredKind, leaf.Outcome)
	}
	_, err := fmt.Fprint(streams.Out, b.String())
	return err
}

// writeDeducedHostLines prints the late-bound host every v3 launch resolved
// against: the instance, the rung that produced it, and every piece of
// evidence a higher rung beat.
//
// It is not decoration. Under v3 the host is state, deduced per launch, so
// an operator reading a result has no other way to tell which server their
// session went to — or that a stale workspace correlation chose it
// (duo-vnext-installation-contract.md §1.1, "the deduced instance and its
// host_source are explicit in launch output and in duo doctor").
func writeDeducedHostLines(b *strings.Builder, host *launch.WireHost) {
	if host == nil || host.Kind == "" {
		return
	}
	fmt.Fprintf(b, "host:      %s:%s (host_source=%s)\n", host.Kind, host.InstanceLabel, host.HostSource)
	for _, outranked := range host.OutrankedEvidence {
		detail := outranked.Detail
		if outranked.InstanceLabel != "" {
			detail = outranked.Kind + ":" + outranked.InstanceLabel + " (" + detail + ")"
		}
		fmt.Fprintf(b, "  outranked %-22s %s\n", outranked.Source+":", detail)
	}
}

// --- Stage-1 host set and support oracle --------------------------------

// stage1Support is the CLI's launch.Support: an installation-evidence
// lookup over the adapter factories this build carries real conformance
// records for (Herdr as the only supported session host, Claude Code and
// Pi as the only supported agent runtimes — the dogfood milestone's
// narrowed Stage 1, roadmap Stage E). It performs no I/O and no live probe:
// every digest it cites comes from a factory's static Descriptor(), never
// from Probe(), which is what keeps it on §7.1's accepted "configuration
// plus installed evidence" rung.
//
// duo-config-v3 step 12 re-keyed it: the lookup is on
// launch.Tuple.SupportKey() — (host kind, host version, agent-runtime
// kind) — and no longer on a config session-host declaration name. Two
// workspaces bound to two Herdr sockets are one evidence key.
type stage1Support struct{}

// herdrDigest and the two runtime digests are read once, as constants of
// this build, exactly like doctor's registeredAdapters reads them for the
// health report.
var (
	herdrDigest  = herdr.Factory{}.Descriptor().ConformanceRecordDigest
	claudeDigest = claude.Factory{}.Descriptor().ConformanceRecordDigest
	piDigest     = pi.Factory{}.Descriptor().ConformanceRecordDigest
)

// stage1HostVersions is the pinned-version table launch.Options.HostVersions
// takes: for every session-host kind this build carries an adapter for, the
// external version string that adapter's own descriptor declares support
// for. It is a build constant read off the descriptor, never a probe and
// never a detected version — the thread-5 position (launch.SupportKey).
func stage1HostVersions() map[string]string {
	d := herdr.Factory{}.Descriptor()
	versions := map[string]string{}
	if len(d.SupportedExternalVersions) > 0 {
		versions[d.AdapterID] = d.SupportedExternalVersions[0]
	}
	return versions
}

func (stage1Support) Supported(t launch.Tuple) launch.Verdict {
	key := t.SupportKey()
	if key.HostKind != herdr.AdapterID || key.HostVersion != herdr.PinnedVersion {
		return launch.Verdict{OK: false}
	}
	switch key.AgentRuntime {
	case "claude":
		return launch.Verdict{OK: true, RecordDigest: herdrDigest + "+" + claudeDigest}
	case "pi":
		return launch.Verdict{OK: true, RecordDigest: herdrDigest + "+" + piDigest}
	default:
		return launch.Verdict{OK: false}
	}
}

// stage1HostSet resolves a materialized launch tuple's deduced session host
// to a live Herdr adapter. Stage 1 supports exactly one session-host kind;
// anything else refuses with a clear, unambiguous message rather than
// guessing at a launcher for it.
//
// The kind and the instance come off the tuple — the one host M1 deduced
// for this launch — and never off a config session-host declaration:
// `duo.config/v3`'s session_hosts block is host-kind policy and carries no
// instance at all. Two workspaces bound to two Herdr sockets build two
// adapters from one configuration.
type stage1HostSet struct{}

func (stage1HostSet) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	switch t.HostKind {
	case herdr.AdapterID:
		if t.HostInstance == "" {
			return nil, fmt.Errorf("cli: the deduced herdr host carries no instance locator")
		}
		h, err := herdr.New(herdr.Config{
			IntegrationInstanceID: t.IntegrationInstanceID,
			SocketPath:            t.HostInstance,
		})
		if err != nil {
			return nil, fmt.Errorf("cli: building the herdr host for %q: %w", t.IntegrationInstanceID, err)
		}
		return h, nil
	default:
		return nil, fmt.Errorf(
			"cli: the deduced session host %q is of kind %q, which this Stage-1 build does not support (only %q)",
			t.IntegrationInstanceID, t.HostKind, herdr.AdapterID)
	}
}

// stage1LeafAugmenter is the CLI's launch.LeafAugmenter: the concrete,
// adapter-aware seam --close-on-exit's two runtime legs use to contribute
// what each needs for a leaf's launch — claude materializes a generated
// SessionEnd hook and settings file and appends `--settings <path>` to
// that leaf's launch arguments; pi sets a pane-creation env marker its
// extension reads.
//
// internal/launch stays agnostic of Claude Code, Pi, Herdr, or any other
// adapter by name (Augment there receives only a launch.Tuple, never a
// runtime adapter); this CLI-level implementation is the one place that
// knows what "claude" and "pi" mean and what each buys from
// --close-on-exit. It is host-agnostic in acceptance the same way --target
// is: nothing here refuses --close-on-exit for any deduced session-host
// kind. The claude leg's hook itself is what guards on being inside a
// Herdr pane (HERDR_ENV, closeonexit/session-end.sh) — a leaf on some
// future non-Herdr host simply gets a settings file whose hook exits 0
// immediately. The pi leg's env marker is inert unless the pi extension
// that reads it is also running inside a Herdr pane (duo-pi-reporter.ts).
//
// PROVISIONAL (dogfood, 2026-08-24): see host.ResolvedLaunchTuple.CloseOnExit
// and terminal-multiplexers notes/46. Every agent runtime other than
// "claude" and "pi" is untouched: Augment is a no-op unless closeOnExit is
// set and t.AgentRuntime is one of those two.
type stage1LeafAugmenter struct{}

// closePaneOnExitEnvVar is the exact key duo-pi-reporter.ts reads
// (internal/runtime/pi/extension/duo-pi-reporter.ts:
// `process.env["DUO_CLOSE_PANE_ON_EXIT"] === "1"`) to decide, on a
// session_shutdown with reason "quit", whether to close its own pane.
// Both the key and the value "1" are exact-match contracts with that
// script, not a convention this package chose.
const closePaneOnExitEnvVar = "DUO_CLOSE_PANE_ON_EXIT"

func (stage1LeafAugmenter) Augment(_ context.Context, launchResolutionID, leaf string, t launch.Tuple, closeOnExit bool) (launch.LeafAugmentation, error) {
	if !closeOnExit {
		return launch.LeafAugmentation{}, nil
	}
	switch t.AgentRuntime {
	case "claude":
		dir, err := claude.DefaultHarnessDir(launchResolutionID, leaf)
		if err != nil {
			return launch.LeafAugmentation{}, fmt.Errorf("cli: resolving the close-on-exit harness directory for leaf %s: %w", leaf, err)
		}
		settingsPath, err := claude.MaterializeCloseOnExit(dir)
		if err != nil {
			return launch.LeafAugmentation{}, fmt.Errorf("cli: materializing the close-on-exit harness for leaf %s: %w", leaf, err)
		}
		return launch.LeafAugmentation{Args: []string{"--settings", settingsPath}}, nil
	case "pi":
		return launch.LeafAugmentation{Env: map[string]string{closePaneOnExitEnvVar: "1"}}, nil
	default:
		return launch.LeafAugmentation{}, nil
	}
}
