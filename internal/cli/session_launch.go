package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/asset"
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
		configPath   string
		requireFlags []string
		avoidFlags   []string
		dryRun       bool
		owner, actor string
		bindActor    string
	)

	cmd := &cobra.Command{
		Use:   "launch <preset>",
		Short: "resolve and launch a named preset (Herdr+Claude Code or Herdr+Pi, Stage 1)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode, err := outputMode(cmd)
			if err != nil {
				return err
			}

			ws := workspace
			if ws == "" {
				wd, err := os.Getwd()
				if err != nil {
					return duoerr.New("internal.cwd_unresolved", fmt.Sprintf("resolving the working directory: %v", err))
				}
				ws = wd
			}

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

			// --- STEP-12 TEMPORARY SHIM -------------------------------
			// duo-config-v3 step 12 replaced the resolver's v2 entry
			// point, which left this command uncompilable. This block is
			// the smallest thing that keeps the binary and its tests
			// honest until step 14 owns the launch wiring properly.
			//
			// What it does NOT do, and what step 14 adds: the --host
			// flag, the workspace<->host correlation and standing
			// provider read models (M1 rungs 2 and 4's state inputs),
			// instance discovery, the first-bind write after a successful
			// spawn, and the launch-output rail that names the deduced
			// instance, its host_source, and the outranked evidence. With
			// no correlation source and no discoverer, deduction here
			// reaches only the ambient-environment rung.
			mat, err := materialize.Materialize(cmd.Context(), materialize.Options{
				WorkspaceFlag:   ws,
				RequestedPreset: args[0],
				Policy:          doc.SessionHosts,
			})
			if err != nil {
				return err // already a *duoerr.Error
			}

			resolver, err := launch.NewResolver(doc, mat, launch.Options{
				Support:      stage1Support{},
				Random:       launch.CryptoSource{},
				HostVersions: stage1HostVersions(),
			})
			if err != nil {
				return duoerr.New("internal.launch_resolver_build_failed", err.Error())
			}
			// --- end STEP-12 TEMPORARY SHIM ---------------------------

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
					return launchDuoErr(err)
				}
				report = resolution.Report()
				report.LaunchResolutionID = "" // §6.10: a dry run commits no record.
				report.Preview = true
			} else {
				a, s, err := openWriteAuthority(cmd.Context())
				if err != nil {
					return err
				}
				defer func() { _ = s.Close() }()

				recorder, err := launchrecord.New(a, launchrecord.Options{
					WorkspacePath: ws,
					Actor:         actor,
					Owner:         owner,
					BindActor:     domain.ActorID(bindActor),
				})
				if err != nil {
					return duoerr.New("internal.launch_recorder_build_failed", err.Error())
				}
				launcher, err := launch.NewLauncher(resolver, recorder, stage1HostSet{})
				if err != nil {
					return duoerr.New("internal.launcher_build_failed", err.Error())
				}
				result, err := launcher.Launch(cmd.Context(), launch.SpawnRequest{
					Request:       req,
					WorkspacePath: ws,
				})
				if err != nil {
					return launchDuoErr(err)
				}
				report = result.Report
			}

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

// launchDuoErr projects a launch-resolution failure onto the chassis's
// structured error. A *launch.Error carries its own registered stable code
// (internal/launch/errors.go); a *scrub.RefusalError is the spawn-environment
// gate tripping, which is a guard refusal and not an internal failure;
// anything else reached here from Launcher.Launch's host-side spawn step,
// which this package's host set and Stage-1 support oracle raise as plain
// errors.
func launchDuoErr(err error) *duoerr.Error {
	var lerr *launch.Error
	if errors.As(err, &lerr) {
		return lerr.Duo()
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
	return duoerr.New("internal.launch_failed", err.Error())
}

func renderLaunchReportText(streams *iostreams.Streams, r launch.Report) error {
	if r.Preview {
		if _, err := fmt.Fprintln(streams.Out, "preview only: no session, no durable record, no spawn"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(streams.Out, "session:  %s\nrecord:   %s\n", r.SessionID, r.LaunchResolutionID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(streams.Out, "selection: %s\n", r.Selection); err != nil {
		return err
	}
	for _, leaf := range r.Leaves {
		if _, err := fmt.Fprintf(streams.Out, "  %s: %s / %s (%s) -> %s\n",
			leaf.Name, leaf.AgentRuntime, leaf.ModelLine, leaf.DeclaredKind, leaf.Outcome); err != nil {
			return err
		}
	}
	return nil
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
// STEP-12 TEMPORARY SHIM: the kind and the instance now come off the tuple
// (the host M1 deduced) rather than off a config session-host declaration,
// which is the shape step 14 keeps. Everything around it is still step
// 14's: the --host flag that feeds the deduction, the correlation read
// model, and the first bind after a successful spawn.
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
