package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/adapter"

	"github.com/procrastivity/duo/internal/buildinfo"
	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/doctor"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/domain/storerepo"
	"github.com/procrastivity/duo/internal/duoerr"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/host/herdr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch"
	"github.com/procrastivity/duo/internal/launch/materialize"
	"github.com/procrastivity/duo/internal/manifest"
	runtimedevin "github.com/procrastivity/duo/internal/runtime/devin"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
	"github.com/procrastivity/duo/internal/scrub"
	"github.com/procrastivity/duo/internal/store"
	"github.com/procrastivity/duo/internal/surface"
)

// registeredAdapters reports the adapter factories this composition root
// registers, probed for their compatibility verdict. A probe error reports
// the adapter as unavailable rather than dropping the row — doctor's job is
// to show what is registered, not only what is healthy. Devin's pin is kept
// separate from its supported-version list because the list intentionally
// retains the earlier 3000.6.2 evidence.
func registeredAdapters(cmd *cobra.Command) []doctor.Adapter {
	hostFactory := hostfake.Factory{}
	runtimeFactory := runtimefake.Factory{}
	devinFactory := runtimedevin.Factory{}

	out := make([]doctor.Adapter, 0, 3)
	for _, registered := range []struct {
		descriptor func() adapter.Descriptor
		probe      func(context.Context) (adapter.Probe, error)
		pinned     string
	}{
		{descriptor: hostFactory.Descriptor, probe: hostFactory.Probe},
		{descriptor: runtimeFactory.Descriptor, probe: runtimeFactory.Probe},
		{descriptor: devinFactory.Descriptor, probe: devinFactory.Probe, pinned: runtimedevin.PinnedExternalVersion},
	} {
		p := adapter.Probe{Compatibility: adapter.CompatibilityUnavailable}
		if probed, err := registered.probe(cmd.Context()); err == nil {
			p = probed
		}
		out = append(out, doctor.FromProbe(registered.descriptor(), p, registered.pinned))
	}
	return out
}

// doctorCommand constructs the `duo doctor` verb: internal/registry's
// "doctor.run" operation, CLI path {"doctor"}. It preserves the original
// authority, adapter, and visibility sections and adds the portable-launcher
// readiness contract as a stable top-level sibling.
//
// The visibility rail and launcher preflight are diagnostic reads. Harness
// directories are reported but never reaped; the authority opens through
// SQLite mode=ro; projection inspection never repairs; the only live request
// is a bounded, non-mutating ping to the selected host.
//
// Step 15 (config-v3) adds the visibility rail: the cwd workspace's (or
// --workspace's) current host correlation, what M1 would deduce right now
// with its host_source and outranked evidence, the standing provider
// facts, and the loaded launch-config document's schema marker. Every one
// of these reads an existing model read-only — Materialize (internal/
// launch/materialize, Step 11) never writes and never dials a socket
// (I-3), and this command never calls a bind/rebind API or touches the
// launch path itself, so the new sections cost a diagnostic read, nothing
// more.
//
// The store path resolves from $XDG_DATA_HOME (internal/doctor.
// DefaultStorePath) — the chassis's own "environment variables can select
// configuration and data roots" allowance
// (duo-vnext-installation-contract.md §1.2) — rather than a dedicated
// --store-path flag, which no spec for this step asks for. The launch
// config path resolves the same way session.launch's own default does
// (defaultLaunchConfigPath, session_launch.go).
func doctorCommand(streams *iostreams.Streams, build buildinfo.Info) *cobra.Command {
	var (
		workspace  string
		configPath string
		hostFlag   string
	)

	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "report duo's authority-store, adapter, host-binding, deduction, provider, and config health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cliflags.FromContext(cmd.Context())

			storePath, err := doctor.DefaultStorePath()
			if err != nil {
				return duoerr.New("internal.doctor_store_path_unresolved", fmt.Sprintf("resolving the default store path: %v", err))
			}

			if flags.Verbose {
				if _, err := fmt.Fprintf(streams.Err, "doctor: probing the authority store at %s\n", storePath); err != nil {
					return err
				}
			}

			base := doctor.Run(storePath, registeredAdapters(cmd))

			root, workspaceStatus, workspaceErr := doctorWorkspace(workspace)

			// Doctor has a stricter read seam than ordinary legacy read
			// commands: SQLite mode=ro, no migration, and no lease. A replay
			// failure becomes an authority finding while the remaining
			// independent checks continue against an empty read model.
			a, closer, replayErr := openDoctorAuthority(cmd.Context(), storePath)
			if replayErr != nil {
				base.Store.Healthy = false
				if base.Store.Error == "" {
					base.Store.Error = replayErr.Error()
				}
				a, err = emptyDoctorAuthority(cmd.Context())
				if err != nil {
					return duoerr.New("internal.authority_open_failed", fmt.Sprintf("opening fallback diagnostic authority: %v", err))
				}
				closer = nopCloser{}
			}
			defer func() { _ = closer.Close() }()

			selectedConfigPath := configPath
			if selectedConfigPath == "" {
				selectedConfigPath, err = defaultLaunchConfigPath()
				if err != nil {
					return duoerr.New("internal.config_path_unresolved", fmt.Sprintf("resolving the default duo.config path: %v", err))
				}
			}
			selectedConfigPath, err = filepath.Abs(selectedConfigPath)
			if err != nil {
				return duoerr.New("internal.config_path_unresolved", fmt.Sprintf("resolving the selected duo.config path: %v", err))
			}
			selectedConfigPath = filepath.Clean(selectedConfigPath)
			configSection, policy, configDoc := doctorConfigStatus(selectedConfigPath)

			deduction := doctorHostDeduction(cmd.Context(), a, root, hostFlag, policy)
			harnessRoot, err := doctor.DefaultHarnessRoot()
			if err != nil {
				return duoerr.New("internal.doctor_harness_path_unresolved", fmt.Sprintf("resolving the harness directory: %v", err))
			}
			sweep, err := doctor.InspectHarnessDirs(harnessRoot, keepLiveHarness(a))
			if err != nil {
				sweep = doctor.HarnessSweep{IDs: []string{}, ReadOnly: true, Error: err.Error()}
			}

			m, err := manifest.Build(cmd.Root(), build)
			if err != nil {
				return duoerr.New("internal.manifest_build_failed", fmt.Sprintf("building the diagnostic manifest: %v", err))
			}
			preflight := doctorLauncherPreflight(cmd.Context(), doctorPreflightInput{
				Build: build, Store: base.Store, ReplayError: replayErr,
				ConfigPath: selectedConfigPath, ConfigSection: configSection, Config: configDoc,
				Workspace: workspaceStatus, WorkspaceError: workspaceErr,
				Deduction: deduction, Manifest: m,
			})
			if workspaceErr != nil {
				// Legacy sections remain useful and shape-compatible. They use
				// the selected (possibly invalid) absolute path but do not turn
				// workspace invalidity into a command-level failure.
				deduction.Detail = workspaceErr.Error()
			}
			report := doctorReport{
				Report:              base,
				LauncherPreflight:   preflight,
				HostBinding:         doctorHostBinding(a, root),
				HostDeduction:       deduction,
				Providers:           doctorProviders(a),
				Config:              configSection,
				RecoveringInstances: len(a.Recovering()),
				ScrubGate:           doctorScrubGate(deduction),
				HarnessSweep:        sweep,
				DevinProjection:     doctorDevinProjection(a, root),
			}

			if flags.JSON() {
				b, err := json.Marshal(report)
				if err != nil {
					return duoerr.New("internal.doctor_encode_failed", fmt.Sprintf("encoding the doctor report: %v", err))
				}
				_, err = fmt.Fprintln(streams.Out, string(b))
				return err
			}

			_, err = fmt.Fprint(streams.Out, humanReport(report))
			return err
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace root path (defaults to the current directory)")
	cmd.Flags().StringVar(&configPath, "config", "", "path to the duo.config/v3 document (defaults to $XDG_CONFIG_HOME/duo/duo.config.yaml)")
	cmd.Flags().StringVar(&hostFlag, "host", "", `the session host to diagnose, "<kind>" or "<kind>:<instance>", using launch precedence`)
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// humanReport renders report in duo doctor's default (non-JSON) form as one
// string, so the command handler needs exactly one write (and one error
// check) rather than one per line.
func humanReport(report doctorReport) string {
	var b strings.Builder

	b.WriteString("duo doctor\n")
	writeLauncherPreflightSection(&b, report.LauncherPreflight)
	fmt.Fprintf(&b, "  store: %s\n", report.Store.Path)
	switch {
	case !report.Store.Present:
		b.WriteString("    status: not yet initialized\n")
	case report.Store.Error != "":
		fmt.Fprintf(&b, "    status: unhealthy (%s)\n", report.Store.Error)
	default:
		b.WriteString("    status: healthy\n")
		fmt.Fprintf(&b, "    schema version: %d\n", report.Store.SchemaVersion)
		if w := report.Store.Writer; w != nil {
			if w.Active {
				fmt.Fprintf(&b, "    writer: active (incarnation=%s, pid=%d, host=%s, expires=%s)\n",
					w.Incarnation, w.PID, w.Hostname, w.ExpiresAt)
			} else {
				b.WriteString("    writer: none active\n")
			}
		}
	}

	fmt.Fprintf(&b, "  adapters: %d registered\n", len(report.Adapters.Registered))
	if len(report.Adapters.Registered) == 0 {
		b.WriteString("    (no session-host or agent-runtime adapter registers yet)\n")
	}
	for _, a := range report.Adapters.Registered {
		fmt.Fprintf(&b, "    %s (%s): %s\n", a.Name, a.Kind, a.Status)
		if a.PinnedExternalVersion != "" {
			detected := a.DetectedExternalVersion
			if detected == "" {
				detected = "not probed"
			}
			fmt.Fprintf(&b, "      external version: detected=%s, pinned=%s, supported=%s\n",
				detected, a.PinnedExternalVersion, strings.Join(a.SupportedExternalVersions, ", "))
		}
	}

	writeHostBindingSection(&b, report.HostBinding)
	writeHostDeductionSection(&b, report.HostDeduction)
	writeScrubGateSection(&b, report.ScrubGate)
	writeProvidersSection(&b, report.Providers)
	writeConfigSection(&b, report.Config)
	writeDevinProjectionSection(&b, report.DevinProjection)

	if report.RecoveringInstances > 0 {
		noun := "instances"
		if report.RecoveringInstances == 1 {
			noun = "instance"
		}
		fmt.Fprintf(&b, "  recovering: %d runtime %s await reconciliation (duo session reconcile)\n",
			report.RecoveringInstances, noun)
	}

	if report.HarnessSweep.Orphaned > 0 {
		noun := "directories"
		if report.HarnessSweep.Orphaned == 1 {
			noun = "directory"
		}
		fmt.Fprintf(&b, "  harness: found %d orphan %s (read-only; nothing reaped)\n", report.HarnessSweep.Orphaned, noun)
	}
	if report.HarnessSweep.Error != "" {
		fmt.Fprintf(&b, "  harness: inspection unavailable (%s)\n", report.HarnessSweep.Error)
	}

	return b.String()
}

// --- Step 15: the config-v3 visibility rail --------------------------------
//
// Everything below reads three existing read models — none of it writes,
// and none of it is wired into the launch path:
//
//   - the workspace↔host correlation (internal/domain/hostcorrelation.go,
//     Step 09), the same read `duo workspace host show` prints — doctor
//     builds it with workspace.go's own hostView/hostProvenance helpers
//     (unexported, same package) so the two commands can never spell one
//     binding two different ways;
//   - the M1/M2 materializer (internal/launch/materialize, Step 11),
//     called with the real correlation and provider read models
//     (*domain.Authority satisfies both narrow interfaces) and the same
//     stage1Discovery the launch path wires (Step 14), so doctor and
//     `duo session launch` deduce from identical inputs and can never
//     disagree about what the next launch would do; Materialize itself
//     never checks reachability (I-3) and never writes, and enumerating
//     instances is a directory read, not a dial — so calling it here is
//     exactly as read-only as calling it from the launch path would be,
//     just without a spawn following it;
//   - the standing provider facts (domain.Authority.StandingProviderFacts,
//     Step 08).
//
// doctorReport embeds doctor.Report anonymously so Step 10's "store" and
// "adapters" JSON keys stay exactly where they were — every field below is
// an additive top-level key, never a rename.
type doctorReport struct {
	doctor.Report
	LauncherPreflight   doctor.LauncherPreflight   `json:"launcher_preflight"`
	HostBinding         workspaceHostShowResult    `json:"host_binding"`
	HostDeduction       doctorHostDeductionSection `json:"host_deduction"`
	Providers           []doctorProviderStanding   `json:"providers"`
	Config              doctorConfigSection        `json:"config"`
	RecoveringInstances int                        `json:"recovering_instances"`
	// ScrubGate is set when the deduced host's panes would trip the
	// environment-scrub gate (notes/51 9d). Omitted when clean or when
	// no host was deduced / the listener environ could not be observed.
	// Doctor warns; launch still refuses.
	ScrubGate *doctorScrubGateWarning `json:"scrub_gate,omitempty"`
	// HarnessSweep preserves the legacy key while now carrying a read-only
	// inspection: orphan directories are named but Reaped remains zero.
	HarnessSweep doctor.HarnessSweep `json:"harness_sweep"`
	// DevinProjection is the launch-workspace hook projection and its
	// session-start drift status. It is additive so existing doctor readers
	// keep their store/adapters and visibility sections unchanged.
	DevinProjection runtimedevin.ProjectionInspection `json:"devin_projection"`
}

func writeLauncherPreflightSection(b *strings.Builder, p doctor.LauncherPreflight) {
	status := strings.ReplaceAll(p.Status, "_", " ")
	fmt.Fprintf(b, "launcher preflight: %s\n", status)
	for _, check := range p.Checks {
		label := strings.ReplaceAll(check.Status, "_", " ")
		fmt.Fprintf(b, "[%s] %s: %s", label, check.ID, check.Summary)
		if check.ID == doctor.CheckSkillProjection {
			fmt.Fprintf(b, " (state=%s, identity=%s@%s, file=%s)",
				p.SkillProjection.State, p.SkillProjection.FormatVersion,
				p.SkillProjection.ContentDigest, p.SkillProjection.File)
		}
		b.WriteByte('\n')
		if check.Required && check.Status != "pass" && check.Action != "" {
			fmt.Fprintf(b, "  action: %s\n", check.Action)
		}
	}
}

// doctorHostDeductionSection is what M1 would deduce right now for the
// report's workspace.
type doctorHostDeductionSection struct {
	// Host is the instance M1 would deduce, nil when no rung yields one.
	Host *doctorDeducedHost `json:"host,omitempty"`
	// HostSource duplicates Host.HostSource at the section's top level, so
	// an --output json reader can check "would this deduce, and from where" with
	// one field lookup, the same shape session.launch's own launch output
	// names at its top level.
	HostSource string `json:"host_source,omitempty"`
	// Ranking is materialize.Rungs in rank order — the locked five-rung
	// ladder (planning-foundation handoff 24 clause). Always present so a
	// dogfood reader can see cwd-correlation outrank ambient-env without
	// opening evidence.go.
	Ranking []string `json:"ranking"`
	// OutrankedEvidence is every rung that was consulted and either
	// produced a host or carried a correlation/ambient capture, but did
	// not win. Always present (as "[]" when empty), never null.
	OutrankedEvidence []doctorOutrankedEvidence `json:"outranked_evidence"`
	// DeductionTrail is every rung materialize.Rungs walked, in rank
	// order. It is populated only when Host is nil: a resolved deduction
	// already names its winner and what it outranked, and repeating every
	// rung's row on top of that would explain nothing further.
	DeductionTrail []materialize.WireRung `json:"deduction_trail,omitempty"`
	// Detail carries an unexpected materialization failure that is not
	// itself a "no host deduced" answer (e.g. the working directory could
	// not be resolved).
	Detail string `json:"detail,omitempty"`
}

// doctorScrubGateWarning names the deduced host whose panes would be
// refused by scrub.Gate, and the surviving marker names (values never
// printed — same discipline as scrub.RefusalError).
type doctorScrubGateWarning struct {
	Host      string   `json:"host"`
	Survivors []string `json:"survivors"`
}

// doctorDeducedHost mirrors duo.external/v1's launch_deduced_host shape
// (see internal/launch/materialize/evidence.go's DeducedHost doc comment
// for the wire-name mapping this follows): Kind is `kind`, Instance is
// `instance_label`, InstanceID is `instance_id`, Source is `host_source`.
type doctorDeducedHost struct {
	Kind          string `json:"kind"`
	InstanceLabel string `json:"instance_label"`
	InstanceID    string `json:"instance_id,omitempty"`
	HostSource    string `json:"host_source"`
}

// doctorOutrankedEvidence is one piece of host evidence Materialize
// captured and a higher rung beat, in duo.external/v1's
// launch_outranked_evidence spelling (materialize.OutrankedEvidence's
// unexported-field Go shape, given JSON tags here because that type is
// deliberately wire-agnostic).
type doctorOutrankedEvidence struct {
	Source        string                    `json:"source"`
	Kind          string                    `json:"kind,omitempty"`
	InstanceLabel string                    `json:"instance_label,omitempty"`
	FactID        string                    `json:"fact_id,omitempty"`
	Captures      []materialize.WireCapture `json:"captures,omitempty"`
	Detail        string                    `json:"detail,omitempty"`
}

// doctorProviderStanding is one provider's standing fact. Only names with
// a recorded fact appear here — a name with no entry has no standing fact
// at all, which by the kernel's default-enabled rule means enabled without
// a fact ID to cite (domain.Authority.StandingProviderFacts's own doc
// comment).
type doctorProviderStanding struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	FactID  string `json:"fact_id,omitempty"`
}

// doctorConfigSection is the loaded launch-config document's schema
// marker, stated plainly. Schema is one of "duo.config/v3" (the schema
// this resolver speaks), "duo.config/v2" (with MigrateHint set),
// "duo.config/v1", "missing" (no file at Path, or a file with no "schema"
// field), or "unreadable" (an unrecognized marker, a decode failure, or a
// duo.config/v3-marked document that fails its own strict validation).
type doctorConfigSection struct {
	Path string `json:"path"`
	// Schema is the closed set named above.
	Schema string `json:"schema"`
	// MigrateHint is set only when Schema is "duo.config/v2": the pointer
	// at the one implemented migration path.
	MigrateHint string `json:"migrate_hint,omitempty"`
	Detail      string `json:"detail,omitempty"`
}

// writeHostBindingSection prints the current workspace↔host correlation,
// in the same words `duo workspace host show`'s own renderWorkspaceHostShow
// uses for the fields they share.
func writeHostBindingSection(b *strings.Builder, r workspaceHostShowResult) {
	fmt.Fprintf(b, "  workspace:       %s\n", r.WorkspaceRoot)
	fmt.Fprintf(b, "    workspace id:  %s\n", orDash(r.WorkspaceID))
	if !r.Bound {
		b.WriteString("    host binding:  none\n")
		if r.Detail != "" {
			fmt.Fprintf(b, "                   (%s)\n", r.Detail)
		}
		return
	}
	fmt.Fprintf(b, "    host binding:  %s:%s (host_source=%s, fact=%s)\n",
		r.Host.Kind, r.Host.InstanceLabel, r.Host.HostSource, r.Provenance.FactID)
	fmt.Fprintf(b, "      fingerprint: session=%s pane_id=%s terminal_id=%s\n",
		orDash(r.Host.Fingerprint.SessionName), orDash(r.Host.Fingerprint.PaneID), orDash(r.Host.Fingerprint.TerminalID))
	if r.Previous != nil {
		fmt.Fprintf(b, "      replaced:    %s:%s\n", r.Previous.Kind, r.Previous.InstanceLabel)
	}
}

// writeHostDeductionSection prints what M1 would deduce right now.
func writeHostDeductionSection(b *strings.Builder, d doctorHostDeductionSection) {
	b.WriteString("  host deduction (what M1 would deduce now):\n")
	if len(d.Ranking) > 0 {
		fmt.Fprintf(b, "    ranking: %s\n", strings.Join(d.Ranking, " > "))
	}
	if d.Detail != "" {
		fmt.Fprintf(b, "    could not deduce: %s\n", d.Detail)
		return
	}
	if d.Host == nil {
		b.WriteString("    no host would be deduced\n")
		for _, rung := range d.DeductionTrail {
			consulted := "not consulted"
			if rung.Consulted {
				consulted = "consulted"
			}
			fmt.Fprintf(b, "      %-22s %-14s %s\n", rung.Source, consulted, rung.Detail)
		}
	} else {
		fmt.Fprintf(b, "    winner:  %s (%s:%s)\n", d.Host.HostSource, d.Host.Kind, d.Host.InstanceLabel)
	}
	if len(d.OutrankedEvidence) > 0 {
		b.WriteString("    outranked:\n")
		for _, e := range d.OutrankedEvidence {
			fmt.Fprintf(b, "      %s: %s\n", e.Source, e.Detail)
		}
	}
}

// writeScrubGateSection prints the notes/51 9d warning when the deduced
// host's panes would trip scrub.Gate. Absent when clean or unobserved.
func writeScrubGateSection(b *strings.Builder, w *doctorScrubGateWarning) {
	if w == nil {
		return
	}
	b.WriteString("  scrub gate:\n")
	fmt.Fprintf(b, "    warning: deduced host %s panes would be refused; survivors: %s\n",
		w.Host, strings.Join(w.Survivors, ", "))
}

// writeProvidersSection prints the standing provider facts.
func writeProvidersSection(b *strings.Builder, providers []doctorProviderStanding) {
	b.WriteString("  providers:\n")
	if len(providers) == 0 {
		b.WriteString("    (no standing provider facts; every provider a variant names is enabled by default)\n")
		return
	}
	for _, p := range providers {
		state := "enabled"
		if !p.Enabled {
			state = "disabled"
		}
		fmt.Fprintf(b, "    %s: %s (fact %s)\n", p.Name, state, p.FactID)
	}
}

// writeConfigSection prints the loaded launch-config document's schema
// marker.
func writeConfigSection(b *strings.Builder, c doctorConfigSection) {
	fmt.Fprintf(b, "  config:          %s\n", c.Path)
	fmt.Fprintf(b, "    schema:        %s\n", c.Schema)
	if c.MigrateHint != "" {
		fmt.Fprintf(b, "    migrate:       %s\n", c.MigrateHint)
	}
	if c.Detail != "" {
		fmt.Fprintf(b, "    detail:        %s\n", c.Detail)
	}
}

func writeDevinProjectionSection(b *strings.Builder, p runtimedevin.ProjectionInspection) {
	fmt.Fprintf(b, "  devin projection: %s\n", p.Status)
	fmt.Fprintf(b, "    hooks file:    %s\n", p.HooksPath)
	fmt.Fprintf(b, "    posture file:  %s\n", p.PosturePath)
	if p.ActiveLaunches > 0 {
		fmt.Fprintf(b, "    active launches: %d\n", p.ActiveLaunches)
	}
	if p.Detail != "" {
		fmt.Fprintf(b, "    detail:        %s\n", p.Detail)
	}
}

// doctorHostBinding builds the current workspace↔host correlation section
// for root, reusing workspace.go's own hostView/hostProvenance helpers
// (unexported, same package) so this can never drift from what
// `duo workspace host show` prints for the same workspace. It never
// writes: WorkspaceForRoot and HostCorrelation are both pure reads.
func doctorHostBinding(a *domain.Authority, root string) workspaceHostShowResult {
	result := workspaceHostShowResult{WorkspaceRoot: root}
	ws, ok := a.WorkspaceForRoot(root)
	if !ok {
		result.Detail = "duo has no workspace for this root path yet"
		return result
	}
	result.WorkspaceID = string(ws.ID)
	c, bound := a.HostCorrelation(ws.ID)
	if !bound {
		result.Detail = "no host correlation; the next launch in this workspace deduces one and binds it"
		return result
	}
	result.Bound = true
	result.Host = hostView(c.Binding)
	result.Provenance = hostProvenance(c)
	if c.Previous != nil {
		result.Previous = hostView(*c.Previous)
	}
	return result
}

// doctorDevinProjection reads the launch-owned projection in the same
// workspace doctor is already inspecting. Active Devin launches are derived
// from the durable session/launch records; if a later launch regenerated the
// file, an older still-running session makes the projection stale because
// Devin loaded its earlier file only at session start.
func doctorDevinProjection(a *domain.Authority, root string) runtimedevin.ProjectionInspection {
	active := make([]runtimedevin.ProjectionActiveLaunch, 0)
	workspace, ok := a.WorkspaceForRoot(root)
	if ok {
		for _, session := range a.Sessions() {
			if session.Workspace != workspace.ID || session.Current == "" {
				continue
			}
			instance, ok := a.Instance(session.Current)
			if !ok || instance.State.Terminal() {
				continue
			}
			resolution, ok := a.SessionLaunchResolution(session.ID)
			if !ok {
				continue
			}
			var record launch.Record
			if err := json.Unmarshal(resolution.Body, &record); err != nil {
				continue
			}
			for _, assignment := range record.Assignment {
				if assignment.Tuple.AgentRuntime == "devin" {
					active = append(active, runtimedevin.ProjectionActiveLaunch{InstallationID: string(resolution.ID)})
					break
				}
			}
		}
	}
	return runtimedevin.InspectProjection(root, active)
}

// doctorHostDeduction runs Materialize read-only against the real
// correlation and provider read models for root, and reports what it
// deduced (or why nothing resolved).
//
// This is the whole of Step 15's "read-only reuse": no bind/rebind API is
// ever called here. Discovery is stage1Discovery, the same discoverer the
// launch path wires (Step 14) — doctor's job is to report what the next
// launch would deduce, and a doctor that deduced from a smaller set of
// inputs than the launcher would report a different answer than the one
// the operator is about to get.
func doctorHostDeduction(ctx context.Context, a *domain.Authority, root, hostFlag string, policy config.SessionHostPolicy) doctorHostDeductionSection {
	section := doctorHostDeductionSection{
		OutrankedEvidence: []doctorOutrankedEvidence{},
		Ranking:           doctorRanking(),
	}

	result, mErr := materialize.Materialize(ctx, materialize.Options{
		WorkspaceFlag: root,
		HostFlag:      hostFlag,
		Policy:        policy,
		Correlations:  a,
		Providers:     a,
		Discovery:     stage1Discovery{},
		Roots:         stage1Discovery{},
	})

	var partial *materialize.Error
	switch {
	case mErr == nil:
		// result already holds the successful materialization.
	case errors.As(mErr, &partial):
		// A *materialize.Error still carries the full deduction trail and
		// every captured evidence entry; only the deduced host is absent.
		result = partial.Result()
	default:
		section.Detail = mErr.Error()
		return section
	}

	if host := result.Host(); host.Present() {
		section.Host = &doctorDeducedHost{
			Kind:          host.Kind,
			InstanceLabel: host.Instance,
			InstanceID:    host.InstanceID,
			HostSource:    string(host.Source),
		}
		section.HostSource = string(host.Source)
	}

	for _, e := range result.OutrankedEvidence() {
		captures := make([]materialize.WireCapture, 0, len(e.Captures))
		for _, c := range e.Captures {
			captures = append(captures, materialize.WireCapture(c))
		}
		section.OutrankedEvidence = append(section.OutrankedEvidence, doctorOutrankedEvidence{
			Source:        string(e.Source),
			Kind:          e.Kind,
			InstanceLabel: e.Instance,
			FactID:        string(e.FactID),
			Captures:      captures,
			Detail:        e.Detail,
		})
	}

	if section.Host == nil {
		for _, rung := range result.Trail() {
			section.DeductionTrail = append(section.DeductionTrail, materialize.WireRung{
				Source:        string(rung.Source),
				Consulted:     rung.Consulted,
				YieldedHost:   rung.YieldedHost,
				Kind:          rung.Kind,
				InstanceLabel: rung.Instance,
				Detail:        rung.Detail,
			})
		}
	}

	return section
}

// doctorRanking copies materialize.Rungs as strings — the locked five-rung
// order, never a policy list.
func doctorRanking() []string {
	out := make([]string, len(materialize.Rungs))
	for i, r := range materialize.Rungs {
		out[i] = string(r)
	}
	return out
}

// doctorHostEnviron reads the environment a deduced herdr host's panes
// would inherit. It is a package-level var so tests can inject a fixed
// environ without a live listener; production points at
// herdr.ListenerEnviron (procfs only — no dial, I-3).
var doctorHostEnviron = herdr.ListenerEnviron

// doctorScrubGate warns when the deduced host's panes would trip
// scrub.Gate. It reuses SurvivingMarkers the same way the launch path's
// refusal projection names survivors; it never refuses (launch still does).
// An unobservable listener is silence, not a warning — doctor names
// survivors, and an unreadable environ has none to name.
func doctorScrubGate(d doctorHostDeductionSection) *doctorScrubGateWarning {
	if d.Host == nil || d.Host.Kind != "herdr" || d.Host.InstanceLabel == "" {
		return nil
	}
	environ, err := doctorHostEnviron(d.Host.InstanceLabel)
	if err != nil {
		return nil
	}
	survivors := scrub.SurvivingMarkers(environ)
	if len(survivors) == 0 {
		return nil
	}
	return &doctorScrubGateWarning{
		Host:      d.Host.Kind + ":" + d.Host.InstanceLabel,
		Survivors: survivors,
	}
}

// keepLiveHarness is the doctor inspection's keep predicate: a harness directory
// named for a launch-resolution id stays only when that id still has a
// committed record whose minted runtime instance is not terminal. No record
// (a refused launch, or any dir whose launch never committed) is reported
// orphaned, never reaped.
// Terminal is InstanceState.Terminal — exited — not the recovering view
// Open() derives on every load, which would otherwise reap every live
// session the moment doctor opened the store. Session.Current is not
// consulted: a restart mints a new instance that the original harness
// directory does not belong to.
func keepLiveHarness(a *domain.Authority) doctor.KeepHarnessDir {
	return func(id string) bool {
		_, instanceID, ok := a.LaunchResolutionBinding(domain.LaunchResolutionID(id))
		if !ok {
			return false
		}
		inst, found := a.Instance(instanceID)
		return found && !inst.State.Terminal()
	}
}

// doctorProviders builds the standing-provider-facts section from a's read
// model, sorted by name for a stable report.
func doctorProviders(a *domain.Authority) []doctorProviderStanding {
	standing := a.StandingProviderFacts()
	names := make([]string, 0, len(standing))
	for name := range standing {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]doctorProviderStanding, 0, len(names))
	for _, name := range names {
		st := standing[name]
		out = append(out, doctorProviderStanding{Name: name, Enabled: st.Enabled, FactID: string(st.FactID)})
	}
	return out
}

// doctorConfigStatus resolves path's schema marker and, when it loads as
// duo.config/v3, the session_hosts policy Materialize consults. Every
// other outcome (missing file, no marker, v1, v2, or an unreadable
// document) reports plainly and returns a zero-value policy — Materialize
// treats an empty SessionHostPolicy as "no enabled kind" at the
// policy-default rung, which is the honest answer when there is no policy
// to read.
func doctorConfigStatus(path string) (doctorConfigSection, config.SessionHostPolicy, config.DocumentV3) {
	section := doctorConfigSection{Path: path}

	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		section.Schema = "missing"
		section.Detail = fmt.Sprintf("no config file at %s", path)
		return section, config.SessionHostPolicy{}, config.DocumentV3{}
	} else if statErr != nil {
		section.Schema = "unreadable"
		section.Detail = statErr.Error()
		return section, config.SessionHostPolicy{}, config.DocumentV3{}
	}

	doc, err := config.LoadV3(path)
	if err == nil {
		section.Schema = config.SchemaV3
		return section, doc.SessionHosts, doc
	}

	de, ok := err.(*duoerr.Error)
	if !ok {
		section.Schema = "unreadable"
		section.Detail = err.Error()
		return section, config.SessionHostPolicy{}, config.DocumentV3{}
	}

	switch de.Code {
	case config.ErrCodeSchemaV2Unsupported:
		section.Schema = config.SchemaV2
		section.MigrateHint = "duo config migrate --to duo.config/v3"
		section.Detail = de.Message
	case config.ErrCodeSchemaV1Unsupported:
		section.Schema = config.SchemaV1
		section.Detail = de.Message
	case config.ErrCodeSchemaMissing:
		section.Schema = "missing"
		section.Detail = de.Message
	default:
		// config.ErrCodeSchemaUnrecognized, config.ErrCodeDecodeFailed, or
		// one of the v3-only strict-validation codes (e.g. a
		// duo.config/v3 document missing a required model_family): the
		// marker itself may be fine, but the document does not load, so
		// doctor reports it the same way it would an unrecognized marker
		// rather than inventing a sixth category.
		section.Schema = "unreadable"
		section.Detail = de.Message
	}
	return section, config.SessionHostPolicy{}, config.DocumentV3{}
}

// doctorWorkspace applies launch's --workspace > cwd precedence, then makes
// the selected path absolute and clean for the preflight wire report.
func doctorWorkspace(requested string) (string, doctor.WorkspacePreflight, error) {
	status := doctor.WorkspacePreflight{RequestedPath: requested, Source: "cwd"}
	if requested != "" {
		status.Source = "flag"
	}
	selected, err := workspaceRoot(requested)
	if err != nil {
		return "", status, err
	}
	selected, err = filepath.Abs(selected)
	if err != nil {
		return "", status, fmt.Errorf("resolving selected workspace %q: %w", selected, err)
	}
	selected = filepath.Clean(selected)
	status.SelectedPath = selected
	info, err := os.Stat(selected)
	if err != nil {
		return selected, status, fmt.Errorf("workspace %q is not locally accessible: %w", selected, err)
	}
	status.Exists = true
	status.Directory = info.IsDir()
	if !status.Directory {
		return selected, status, fmt.Errorf("workspace %q is not a directory", selected)
	}
	return selected, status, nil
}

// openDoctorAuthority is diagnosis's zero-write authority seam. Unlike the
// older general read helper it cannot create/migrate a database because the
// SQLite handle itself is mode=ro.
func openDoctorAuthority(ctx context.Context, path string) (*domain.Authority, io.Closer, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		a, openErr := emptyDoctorAuthority(ctx)
		return a, nopCloser{}, openErr
	} else if err != nil {
		return nil, nil, err
	}
	s, err := store.OpenReadOnly(path)
	if err != nil {
		return nil, nil, err
	}
	a, err := domain.Open(ctx, storerepo.New(s))
	if err != nil {
		_ = s.Close()
		return nil, nil, fmt.Errorf("replaying authority store: %w", err)
	}
	return a, s, nil
}

func emptyDoctorAuthority(ctx context.Context) (*domain.Authority, error) {
	return domain.Open(ctx, emptyRepository{})
}

type doctorPreflightInput struct {
	Build          buildinfo.Info
	Store          doctor.StoreStatus
	ReplayError    error
	ConfigPath     string
	ConfigSection  doctorConfigSection
	Config         config.DocumentV3
	Workspace      doctor.WorkspacePreflight
	WorkspaceError error
	Deduction      doctorHostDeductionSection
	Manifest       manifest.Manifest
}

func doctorLauncherPreflight(ctx context.Context, in doctorPreflightInput) doctor.LauncherPreflight {
	p := doctor.NewLauncherPreflight()
	doctorExecutableCheck(&p, in.Build)
	doctorEffectiveConfigCheck(&p, in)
	doctorAuthorityCheck(&p, in.Store, in.ReplayError)
	doctorWorkspaceCheck(&p, in.Workspace, in.WorkspaceError)
	doctorHostChecks(ctx, &p, in)
	doctorProjectionCheck(&p, in)
	p.Finalize()
	return p
}

func doctorExecutableCheck(p *doctor.LauncherPreflight, build buildinfo.Info) {
	p.Duo.Version, p.Duo.Commit, p.Duo.BuildDate = build.Version, build.Commit, build.Date
	executable, err := os.Executable()
	if err == nil {
		executable, err = filepath.Abs(executable)
	}
	if err == nil {
		executable, err = filepath.EvalSymlinks(executable)
	}
	if err == nil {
		var info os.FileInfo
		info, err = os.Stat(executable)
		if err == nil && info.IsDir() {
			err = fmt.Errorf("resolved executable is a directory")
		}
	}
	if err == nil {
		p.Duo.ExecutablePath = filepath.Clean(executable)
	}
	if err != nil || build.Version == "" || build.Commit == "" || build.Date == "" {
		summary := "running Duo executable does not have a complete local build identity"
		action := "Executable stage: run the intended installed Duo binary and record its version, commit, and build date."
		p.SetCheck(doctor.CheckDuoExecutable, "fail", "executable.identity_unavailable", summary, action)
		return
	}
	p.SetCheck(doctor.CheckDuoExecutable, "pass", "ok", "running Duo executable has a reportable build identity", "")
}

func doctorEffectiveConfigCheck(p *doctor.LauncherPreflight, in doctorPreflightInput) {
	p.Config.Path = in.ConfigPath
	p.Config.Schema = in.ConfigSection.Schema
	if in.ConfigSection.Schema != config.SchemaV3 {
		code := "config.invalid"
		if in.ConfigSection.Schema == "missing" {
			code = "config.missing"
		}
		p.SetCheck(doctor.CheckEffectiveConfig, "fail", code,
			"effective launch config is not a valid duo.config/v3 document",
			fmt.Sprintf("Config stage: install or fix duo.config/v3 at %s, or migrate a v2 document with duo config migrate --to duo.config/v3.", in.ConfigPath))
		return
	}
	digest, err := launch.ConfigurationDigest(in.Config)
	if err != nil {
		p.SetCheck(doctor.CheckEffectiveConfig, "fail", "config.invalid", "effective launch config could not be identified",
			fmt.Sprintf("Config stage: fix the validated configuration at %s and rerun doctor.", in.ConfigPath))
		return
	}
	presets := make([]string, 0, len(in.Config.Presets))
	for name := range in.Config.Presets {
		presets = append(presets, name)
	}
	sort.Strings(presets)
	p.Config.Valid = true
	p.Config.EffectiveDigest = digest
	p.Config.Presets = presets
	p.SetCheck(doctor.CheckEffectiveConfig, "pass", "ok", "effective duo.config/v3 intent is valid and deterministically identified", "")
}

func doctorAuthorityCheck(p *doctor.LauncherPreflight, status doctor.StoreStatus, replayErr error) {
	p.Authority = doctor.AuthorityPreflight{
		Path: status.Path, State: "unavailable", Present: status.Present,
		Healthy: status.Healthy, SchemaVersion: status.SchemaVersion,
	}
	if status.Writer != nil {
		p.Authority.WriterActive = status.Writer.Active
	}
	switch {
	case !status.Present && status.Error == "" && replayErr == nil:
		p.Authority.State = "initializable"
		p.Authority.Healthy = true
		p.SetCheck(doctor.CheckAuthorityStore, "pass", "ok", "authority store is absent and locally initializable by the first real write", "")
	case status.Writer != nil && status.Writer.Active:
		p.Authority.State = "writer_active"
		p.Authority.Healthy = true
		p.SetCheck(doctor.CheckAuthorityStore, "fail", "authority.writer_active", "authority store is held by an unexpired writer lease",
			fmt.Sprintf("Authority stage: wait for or normally stop writer pid %d on %s for %s, then rerun doctor.", status.Writer.PID, status.Writer.Hostname, status.Path))
	case strings.Contains(status.Error, "unsupported schema"):
		p.Authority.State = "incompatible"
		p.SetCheck(doctor.CheckAuthorityStore, "fail", "authority.incompatible", "authority store schema is incompatible with this Duo build",
			fmt.Sprintf("Authority stage: use a compatible Duo build for %s; doctor will not migrate it.", status.Path))
	case !status.Present && status.Error != "":
		p.Authority.State = "unavailable"
		p.SetCheck(doctor.CheckAuthorityStore, "fail", "authority.unavailable", "authority store path is not locally addressable",
			fmt.Sprintf("Authority stage: make the XDG-selected path %s locally accessible, then rerun doctor.", status.Path))
	case replayErr != nil || (status.Present && status.Error != ""):
		p.Authority.State = "unhealthy"
		p.SetCheck(doctor.CheckAuthorityStore, "fail", "authority.unhealthy", "authority store is not readable and replayable",
			fmt.Sprintf("Authority stage: inspect and repair or restore the local store at %s, then rerun doctor.", status.Path))
	default:
		p.Authority.State = "ready"
		p.SetCheck(doctor.CheckAuthorityStore, "pass", "ok", "authority store is readable, compatible, replayable, and has no active writer", "")
	}
}

func doctorWorkspaceCheck(p *doctor.LauncherPreflight, status doctor.WorkspacePreflight, err error) {
	p.Workspace = status
	if err != nil || status.SelectedPath == "" || !filepath.IsAbs(status.SelectedPath) || !status.Exists || !status.Directory {
		p.SetCheck(doctor.CheckWorkspace, "fail", "workspace.invalid", "selected workspace is not an absolute accessible directory",
			fmt.Sprintf("Workspace stage: pass the actual existing project root with --workspace; selected resource was %q.", status.SelectedPath))
		return
	}
	p.SetCheck(doctor.CheckWorkspace, "pass", "ok", "selected workspace is the absolute project root used for launch and skill lookup", "")
}

func checkPassed(p *doctor.LauncherPreflight, id string) bool {
	for _, check := range p.Checks {
		if check.ID == id {
			return check.Status == "pass"
		}
	}
	return false
}

// doctorProbeHerdr is injectable so CLI tests can pin the compatibility
// boundary without relying on an installed herdr schema-export binary.
var doctorProbeHerdr = func(ctx context.Context, cfg herdr.Config) (adapter.Probe, error) {
	return (herdr.Factory{Config: cfg}).Probe(ctx)
}

func doctorHostChecks(ctx context.Context, p *doctor.LauncherPreflight, in doctorPreflightInput) {
	prerequisites := checkPassed(p, doctor.CheckEffectiveConfig) && checkPassed(p, doctor.CheckWorkspace)
	if !prerequisites {
		p.SetCheck(doctor.CheckHostSelection, "not_checked", "prerequisite.not_reached", "host selection was not evaluated because config or workspace failed",
			"Host selection stage: fix the config and workspace findings, then rerun doctor with the intended --host value.")
		p.SetCheck(doctor.CheckHostReachability, "not_checked", "prerequisite.not_reached", "host reachability was not checked because no trustworthy host was selected",
			"Host reachability stage: resolve the prerequisite findings, then start or select the intended host.")
		p.SetCheck(doctor.CheckHostCompatibility, "not_checked", "prerequisite.not_reached", "host compatibility was not checked because no host answered", "")
		return
	}
	if in.Deduction.Host == nil {
		p.SetCheck(doctor.CheckHostSelection, "fail", "launch.host_unresolved", "launch materialization did not deduce exactly one enabled host",
			fmt.Sprintf("Host selection stage: supply --host, repair the workspace correlation, or correct host policy for %s.", in.Workspace.SelectedPath))
		p.SetCheck(doctor.CheckHostReachability, "not_checked", "prerequisite.not_reached", "host reachability was not checked because host selection failed",
			"Host reachability stage: resolve host selection and rerun doctor.")
		p.SetCheck(doctor.CheckHostCompatibility, "not_checked", "prerequisite.not_reached", "host compatibility was not checked because no host answered", "")
		return
	}
	host := in.Deduction.Host
	p.Host.Selected = true
	p.Host.Kind = host.Kind
	p.Host.InstanceID = host.InstanceID
	p.Host.InstanceLabel = host.InstanceLabel
	p.Host.HostSource = host.HostSource
	p.SetCheck(doctor.CheckHostSelection, "pass", "ok", "launch materialization selected exactly one enabled host", "")

	if host.Kind != herdr.AdapterID {
		p.Host.Compatibility = "unknown"
		p.SetCheck(doctor.CheckHostReachability, "fail", "host.unreachable", "selected host has no bounded reachability probe in this build",
			fmt.Sprintf("Host reachability stage: select a supported Herdr host for %s and rerun doctor.", host.InstanceLabel))
		p.SetCheck(doctor.CheckHostCompatibility, "not_checked", "prerequisite.not_reached", "host compatibility was not checked because no host answered", "")
		return
	}
	probeID := host.InstanceID
	if probeID == "" {
		probeID = herdr.AdapterID + ":selected"
	}
	probeCtx, cancel := context.WithTimeout(ctx, 750*time.Millisecond)
	defer cancel()
	probe, err := doctorProbeHerdr(probeCtx, herdr.Config{
		IntegrationInstanceID: probeID,
		SocketPath:            host.InstanceLabel,
		CallTimeout:           500 * time.Millisecond,
	})
	if err != nil || probe.ConnectionState != "connected" {
		p.Host.Compatibility = "unavailable"
		p.SetCheck(doctor.CheckHostReachability, "fail", "host.unreachable", "selected Herdr host did not answer the bounded ping",
			fmt.Sprintf("Host reachability stage: start or select the Herdr server at %s and make its socket reachable.", host.InstanceLabel))
		p.SetCheck(doctor.CheckHostCompatibility, "not_checked", "prerequisite.not_reached", "host compatibility was not checked because no host answered", "")
		return
	}
	p.Host.Reachable = true
	p.Host.DetectedVersion = probe.DetectedVersion
	p.Host.ProtocolIdentity = probe.ProtocolOrFormatIdentity
	p.Host.Compatibility = string(probe.Compatibility)
	if p.Host.Compatibility == "" {
		p.Host.Compatibility = "unknown"
	}
	p.SetCheck(doctor.CheckHostReachability, "pass", "ok", "selected Herdr host answered the bounded non-mutating ping", "")
	if probe.Compatibility == adapter.CompatibilitySupported {
		p.SetCheck(doctor.CheckHostCompatibility, "pass", "ok", "reachable host matches the pinned version, protocol, and schema evidence", "")
		return
	}
	code := "host.compatibility_unverified"
	if probe.Compatibility == adapter.CompatibilityIncompatible {
		code = "host.compatibility_incompatible"
	}
	p.SetCheck(doctor.CheckHostCompatibility, "warning", code, "reachable host does not match the complete pinned compatibility evidence", "")
}

func doctorProjectionCheck(p *doctor.LauncherPreflight, in doctorPreflightInput) {
	target := in.Manifest.HarnessTargets[0]
	root := filepath.Join(in.Workspace.SelectedPath, filepath.FromSlash(target.ProjectionRoot))
	p.SkillProjection = doctor.SkillProjection{
		Target: target.Name, Name: target.Artifact.Name, FormatVersion: target.ProjectionFormat,
		ContentDigest: target.Artifact.ContentDigest, Root: root,
		File:  filepath.Join(root, filepath.FromSlash(target.Artifact.OutputPath)),
		Stamp: filepath.Join(root, filepath.FromSlash(target.StampFile)), State: "incompatible",
	}
	if !checkPassed(p, doctor.CheckWorkspace) {
		p.SetCheck(doctor.CheckSkillProjection, "not_checked", "prerequisite.not_reached", "portable skill projection was not inspected because workspace selection failed",
			"Skill projection stage: pass an accessible project root with --workspace, then rerun doctor.")
		return
	}
	inspection, err := manifest.InspectPortableLaunchers(in.Workspace.SelectedPath, in.Manifest)
	if err != nil {
		p.SetCheck(doctor.CheckSkillProjection, "fail", "projection.incompatible", "portable skill projection could not be safely inspected",
			fmt.Sprintf("Skill projection stage: inspect or move the projection at %s, then run the installer when allowed.", root))
		return
	}
	p.SkillProjection.State = string(inspection.State)
	p.SkillProjection.Root = inspection.Root
	p.SkillProjection.File = filepath.Join(inspection.Root, manifest.PortableSkillFile)
	p.SkillProjection.Stamp = filepath.Join(inspection.Root, manifest.ProjectionStampFile)
	p.SkillProjection.InstallationID = inspection.InstallationID
	if inspection.State == manifest.StateCurrent {
		p.SetCheck(doctor.CheckSkillProjection, "pass", "ok", "portable skill projection is current for this Duo manifest", "")
		return
	}
	code := map[manifest.ProjectionState]string{
		manifest.StateMissing:         "projection.missing",
		manifest.StateStale:           "projection.stale",
		manifest.StateModified:        "projection.modified",
		manifest.StateIncompatible:    "projection.incompatible",
		manifest.StateUnownedConflict: "projection.user_file_conflict",
	}[inspection.State]
	if code == "" {
		code = "projection.incompatible"
	}
	p.SetCheck(doctor.CheckSkillProjection, "fail", code,
		fmt.Sprintf("portable skill projection is %s for this Duo manifest", inspection.State),
		fmt.Sprintf("Skill projection stage: inspect %s and run duo install portable-launchers --workspace %s --repair when allowed; move modified or unowned content first.", inspection.Root, in.Workspace.SelectedPath))
}
