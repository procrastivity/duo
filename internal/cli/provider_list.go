package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// providerListOperation is the operation this verb implements: the one
// registered operation name this file may name as a literal (see
// provider.go's file-split note).
const providerListOperation = "provider.list"

// providerListCommand constructs `duo provider list`, projecting
// internal/registry's provider.list operation at CLI path
// {"provider", "list"}. It never writes: it merges the provider names a
// launch_variant declares (internal/config's v3 loader) with the standing
// facts domain.Authority.StandingProviderFacts replays, showing every name
// either source names, with its state and fact ID where one exists.
func providerListCommand(streams *iostreams.Streams) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "list every provider a launch_variant names or a standing fact records",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runProviderList(cmd, streams, configPath)
		},
	}
	providerConfigFlag(cmd, &configPath)
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

func runProviderList(cmd *cobra.Command, streams *iostreams.Streams, configPath string) error {
	mode, err := outputMode(cmd)
	if err != nil {
		return err
	}

	doc, err := loadProviderConfig(configPath)
	if err != nil {
		return err
	}
	known := variantProviders(doc)

	a, closer, err := openReadAuthority(cmd.Context())
	if err != nil {
		return err
	}
	defer func() { _ = closer.Close() }()
	standing := a.StandingProviderFacts()

	names := make(map[string]bool, len(known)+len(standing))
	for name := range known {
		names[name] = true
	}
	for name := range standing {
		names[name] = true
	}
	sorted := make([]string, 0, len(names))
	for name := range names {
		sorted = append(sorted, name)
	}
	sort.Strings(sorted)

	items := make([]providerListItem, 0, len(sorted))
	for _, name := range sorted {
		item := providerListItem{Name: name, Enabled: true, Known: known[name]}
		if st, ok := standing[name]; ok {
			item.Enabled = st.Enabled
			item.FactID = string(st.FactID)
		}
		items = append(items, item)
	}
	result := providerListResult{Providers: items}

	if mode == "json" {
		b, err := json.Marshal(newEnvelope(providerListOperation, result))
		if err != nil {
			return duoerr.New("internal.provider_list_encode_failed", err.Error())
		}
		_, err = fmt.Fprintln(streams.Out, string(b))
		return err
	}

	if len(items) == 0 {
		_, err := fmt.Fprintln(streams.Out, "no providers named by a variant or recorded")
		return err
	}
	for _, it := range items {
		state := "enabled"
		if !it.Enabled {
			state = "disabled"
		}
		fact := ""
		if it.FactID != "" {
			fact = fmt.Sprintf(" [fact %s]", it.FactID)
		}
		unknown := ""
		if !it.Known {
			unknown = " (no variant names this provider)"
		}
		if _, err := fmt.Fprintf(streams.Out, "%s: %s%s%s\n", it.Name, state, fact, unknown); err != nil {
			return err
		}
	}
	return nil
}
