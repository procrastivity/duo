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
	"github.com/procrastivity/duo/internal/launchrecord"
	"github.com/procrastivity/duo/internal/runtime/claude"
	"github.com/procrastivity/duo/internal/runtime/pi"
	"github.com/procrastivity/duo/internal/scrub"
	"github.com/procrastivity/duo/internal/surface"
)

// defaultLaunchConfigPath resolves the duo.config/v2 launch-preset document
// the launch resolver reads. No planning document fixes a normative default
// path yet (Step 24, "the user's real duo.config/v2", is where the shipped
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
					return duoerr.New("internal.config_path_unresolved", fmt.Sprintf("resolving the default duo.config/v2 path: %v", err))
				}
				path = p
			}
			doc, err := config.LoadV2(path)
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

			resolver, err := launch.NewResolver(doc, launch.Options{
				Support: stage1Support{doc: doc},
				Random:  launch.CryptoSource{},
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
				launcher, err := launch.NewLauncher(resolver, recorder, stage1HostSet{doc: doc})
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
	cmd.Flags().StringVar(&configPath, "config", "", "path to the duo.config/v2 document (defaults to $XDG_CONFIG_HOME/duo/duo.config.yaml)")
	cmd.Flags().StringArrayVar(&requireFlags, "require", nil, "a non-relenting launch constraint, axis=value (agent_runtime or model_line); repeatable")
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
				fmt.Sprintf("constraint axis %q is not a declared axis (agent_runtime, model_line).", axis))
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
// lookup over the two adapter factories this build carries real conformance
// records for (Herdr as the only supported session host, Claude Code and
// Pi as the only supported agent runtimes — the dogfood milestone's
// narrowed Stage 1, roadmap Stage E). It performs no I/O and no live probe:
// every digest it cites comes from a factory's static Descriptor(), never
// from Probe(), which is what keeps it on §7.1's accepted "configuration
// plus installed evidence" rung.
type stage1Support struct {
	doc config.DocumentV2
}

// herdrDigest and the two runtime digests are read once, as constants of
// this build, exactly like doctor's registeredAdapters reads them for the
// health report.
var (
	herdrDigest  = herdr.Factory{}.Descriptor().ConformanceRecordDigest
	claudeDigest = claude.Factory{}.Descriptor().ConformanceRecordDigest
	piDigest     = pi.Factory{}.Descriptor().ConformanceRecordDigest
)

func (s stage1Support) Supported(t launch.Tuple) launch.Verdict {
	decl, ok := s.doc.SessionHosts[t.IntegrationInstanceID]
	kind, _ := decl["kind"].(string)
	if !ok || kind != "herdr" {
		return launch.Verdict{OK: false}
	}
	switch t.AgentRuntime {
	case "claude":
		return launch.Verdict{OK: true, RecordDigest: herdrDigest + "+" + claudeDigest}
	case "pi":
		return launch.Verdict{OK: true, RecordDigest: herdrDigest + "+" + piDigest}
	default:
		return launch.Verdict{OK: false}
	}
}

// stage1HostSet resolves a materialized launch tuple's session host to a
// live Herdr adapter, by the session-host declaration's "kind" field —
// the same config-map convention internal/launch/catalog.go already reads
// "kind" off an agent-runtime declaration with. Stage 1 supports exactly
// one session-host kind; anything else (a declared but unsupported "tmux"
// entry, say) refuses with a clear, unambiguous message rather than
// guessing at a launcher for it.
type stage1HostSet struct {
	doc config.DocumentV2
}

func (hs stage1HostSet) LauncherFor(t launch.Tuple) (host.HostLauncher, error) {
	decl, ok := hs.doc.SessionHosts[t.IntegrationInstanceID]
	if !ok {
		return nil, fmt.Errorf("cli: session host %q is not declared in the loaded duo.config/v2 document", t.IntegrationInstanceID)
	}
	kind, _ := decl["kind"].(string)
	switch kind {
	case "herdr":
		socketPath, _ := decl["socket_path"].(string)
		if socketPath == "" {
			return nil, fmt.Errorf("cli: session host %q declares kind \"herdr\" but no socket_path", t.IntegrationInstanceID)
		}
		h, err := herdr.New(herdr.Config{
			IntegrationInstanceID: t.IntegrationInstanceID,
			SocketPath:            socketPath,
		})
		if err != nil {
			return nil, fmt.Errorf("cli: building the herdr host for %q: %w", t.IntegrationInstanceID, err)
		}
		return h, nil
	default:
		return nil, fmt.Errorf(
			"cli: session host %q declares kind %q, which this Stage-1 build does not support (only \"herdr\")",
			t.IntegrationInstanceID, kind)
	}
}
