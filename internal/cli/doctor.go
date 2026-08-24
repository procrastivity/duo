package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/adapter"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/doctor"
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/launch/materialize"
	runtimefake "github.com/procrastivity/duo/internal/runtime/fake"
	"github.com/procrastivity/duo/internal/surface"
)

// registeredAdapters reports the adapter factories this composition root
// registers, probed for their compatibility verdict. Stage 0 registers the
// permanent fake pair (the cross-composition gate adapters); real host and
// runtime adapters join this list in Stage 1. A probe error reports the
// adapter as unavailable rather than dropping the row — doctor's job is to
// show what is registered, not only what is healthy.
func registeredAdapters(cmd *cobra.Command) []doctor.Adapter {
	hostFactory := hostfake.Factory{}
	runtimeFactory := runtimefake.Factory{}

	out := make([]doctor.Adapter, 0, 2)
	for _, probe := range []struct {
		descriptor func() adapter.Descriptor
		probe      func(context.Context) (adapter.Probe, error)
	}{
		{hostFactory.Descriptor, hostFactory.Probe},
		{runtimeFactory.Descriptor, runtimeFactory.Probe},
	} {
		compatibility := adapter.CompatibilityUnavailable
		if p, err := probe.probe(cmd.Context()); err == nil {
			compatibility = p.Compatibility
		}
		out = append(out, doctor.FromDescriptor(probe.descriptor(), compatibility))
	}
	return out
}

// doctorCommand constructs the `duo doctor` verb: internal/registry's
// "doctor.run" operation, CLI path {"doctor"}. Step 10 wired the core
// checks — authority-store health and the registered-adapters section;
// docs/doctor/decisions.md records what a later step still owes
// (generated-artifact drift, harness trust, live socket checks).
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
func doctorCommand(streams *iostreams.Streams) *cobra.Command {
	var workspace string

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

			root, err := workspaceRoot(workspace)
			if err != nil {
				return err
			}

			// openReadAuthority never creates a store that was not already
			// there (session.go), the same "a missing store is not an
			// error" discipline doctor.Run's own probeStore applies — a
			// fresh installation reports an unbound workspace and no
			// standing provider facts, not a freshly-created database.
			a, closer, err := openReadAuthority(cmd.Context())
			if err != nil {
				return err
			}
			defer func() { _ = closer.Close() }()

			configPath, err := defaultLaunchConfigPath()
			if err != nil {
				return duoerr.New("internal.config_path_unresolved", fmt.Sprintf("resolving the default duo.config path: %v", err))
			}
			configSection, policy := doctorConfigStatus(configPath)

			report := doctorReport{
				Report:        base,
				HostBinding:   doctorHostBinding(a, root),
				HostDeduction: doctorHostDeduction(cmd.Context(), a, root, policy),
				Providers:     doctorProviders(a),
				Config:        configSection,
			}

			if flags.JSON {
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
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// humanReport renders report in duo doctor's default (non-JSON) form as one
// string, so the command handler needs exactly one write (and one error
// check) rather than one per line.
func humanReport(report doctorReport) string {
	var b strings.Builder

	b.WriteString("duo doctor\n")
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
	}

	writeHostBindingSection(&b, report.HostBinding)
	writeHostDeductionSection(&b, report.HostDeduction)
	writeProvidersSection(&b, report.Providers)
	writeConfigSection(&b, report.Config)

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
	HostBinding   workspaceHostShowResult    `json:"host_binding"`
	HostDeduction doctorHostDeductionSection `json:"host_deduction"`
	Providers     []doctorProviderStanding   `json:"providers"`
	Config        doctorConfigSection        `json:"config"`
}

// doctorHostDeductionSection is what M1 would deduce right now for the
// report's workspace.
type doctorHostDeductionSection struct {
	// Host is the instance M1 would deduce, nil when no rung yields one.
	Host *doctorDeducedHost `json:"host,omitempty"`
	// HostSource duplicates Host.HostSource at the section's top level, so
	// a --json reader can check "would this deduce, and from where" with
	// one field lookup, the same shape session.launch's own launch output
	// names at its top level.
	HostSource string `json:"host_source,omitempty"`
	// OutrankedEvidence is every rung that was consulted and either
	// produced a host or carried a correlation/ambient capture, but did
	// not win. Always present (as "[]" when empty), never null.
	OutrankedEvidence []doctorOutrankedEvidence `json:"outranked_evidence"`
	// DeductionTrail is every rung materialize.Rungs walked, in rank
	// order. It is populated only when Host is nil: a resolved deduction
	// already names its winner and what it outranked, and repeating all
	// four rows on top of that would explain nothing further.
	DeductionTrail []materialize.WireRung `json:"deduction_trail,omitempty"`
	// Detail carries an unexpected materialization failure that is not
	// itself a "no host deduced" answer (e.g. the working directory could
	// not be resolved).
	Detail string `json:"detail,omitempty"`
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
		fmt.Fprintf(b, "    %s:%s (host_source=%s)\n", d.Host.Kind, d.Host.InstanceLabel, d.Host.HostSource)
	}
	if len(d.OutrankedEvidence) > 0 {
		b.WriteString("    outranked:\n")
		for _, e := range d.OutrankedEvidence {
			fmt.Fprintf(b, "      %s: %s\n", e.Source, e.Detail)
		}
	}
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
func doctorHostDeduction(ctx context.Context, a *domain.Authority, root string, policy config.SessionHostPolicy) doctorHostDeductionSection {
	section := doctorHostDeductionSection{OutrankedEvidence: []doctorOutrankedEvidence{}}

	result, mErr := materialize.Materialize(ctx, materialize.Options{
		WorkspaceFlag: root,
		Policy:        policy,
		Correlations:  a,
		Providers:     a,
		Discovery:     stage1Discovery{},
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
func doctorConfigStatus(path string) (doctorConfigSection, config.SessionHostPolicy) {
	section := doctorConfigSection{Path: path}

	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		section.Schema = "missing"
		section.Detail = fmt.Sprintf("no config file at %s", path)
		return section, config.SessionHostPolicy{}
	} else if statErr != nil {
		section.Schema = "unreadable"
		section.Detail = statErr.Error()
		return section, config.SessionHostPolicy{}
	}

	doc, err := config.LoadV3(path)
	if err == nil {
		section.Schema = config.SchemaV3
		return section, doc.SessionHosts
	}

	de, ok := err.(*duoerr.Error)
	if !ok {
		section.Schema = "unreadable"
		section.Detail = err.Error()
		return section, config.SessionHostPolicy{}
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
	return section, config.SessionHostPolicy{}
}
