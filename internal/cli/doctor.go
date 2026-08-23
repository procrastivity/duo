package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/adapter"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/doctor"
	"github.com/procrastivity/duo/internal/duoerr"
	hostfake "github.com/procrastivity/duo/internal/host/fake"
	"github.com/procrastivity/duo/internal/iostreams"
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
// "doctor.run" operation, CLI path {"doctor"}. Step 10 wires the core
// checks only — authority-store health and the (currently empty)
// registered-adapters section; docs/doctor/decisions.md records what a
// later step still owes (generated-artifact drift, harness trust, live
// socket checks).
//
// The store path resolves from $XDG_DATA_HOME (internal/doctor.
// DefaultStorePath) — the chassis's own "environment variables can select
// configuration and data roots" allowance
// (duo-vnext-installation-contract.md §1.2) — rather than a dedicated
// --store-path flag, which no spec for this step asks for.
func doctorCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "report duo's authority-store and adapter health",
		RunE: func(cmd *cobra.Command, _ []string) error {
			flags := cliflags.FromContext(cmd.Context())

			path, err := doctor.DefaultStorePath()
			if err != nil {
				return duoerr.New("internal.doctor_store_path_unresolved", fmt.Sprintf("resolving the default store path: %v", err))
			}

			if flags.Verbose {
				if _, err := fmt.Fprintf(streams.Err, "doctor: probing the authority store at %s\n", path); err != nil {
					return err
				}
			}

			report := doctor.Run(path, registeredAdapters(cmd))

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
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

// humanReport renders report in duo doctor's default (non-JSON) form as one
// string, so the command handler needs exactly one write (and one error
// check) rather than one per line.
func humanReport(report doctor.Report) string {
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

	return b.String()
}
