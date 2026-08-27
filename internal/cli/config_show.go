package cli

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
	"github.com/procrastivity/duo/internal/surface"
)

// configShowOperation is the operation this verb implements: the one
// registered operation name this file may name as a literal (see
// config.go's file-split note).
const configShowOperation = "config.show"

// configShowCommand constructs `duo config show`, projecting
// internal/registry's config.show operation at CLI path {"config", "show"}.
// It reads the loaded duo.config/v3 document and lists preset names and leaf
// keys (JSON) or preset names alone (text); it never opens the authority store.
func configShowCommand(streams *iostreams.Streams) *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "list presets from the loaded duo.config/v3 document",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runConfigShow(cmd, streams, configPath)
		},
	}
	providerConfigFlag(cmd, &configPath)
	surface.Annotate(cmd, surface.Plumbing)
	return cmd
}

type configShowResult struct {
	Schema        string              `json:"schema"`
	Path          string              `json:"path"`
	Presets       []configShowPreset  `json:"presets"`
	AgentRuntimes []configShowRuntime `json:"agent_runtimes"`
}

type configShowPreset struct {
	Name   string   `json:"name"`
	Leaves []string `json:"leaves"`
}

type configShowRuntime struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

func runConfigShow(cmd *cobra.Command, streams *iostreams.Streams, configPath string) error {
	mode := cliflags.FromContext(cmd.Context()).Output

	path := configPath
	if path == "" {
		p, err := defaultLaunchConfigPath()
		if err != nil {
			return duoerr.New("internal.config_path_unresolved",
				fmt.Sprintf("resolving the default duo.config/v3 path: %v", err))
		}
		path = p
	}

	doc, err := config.LoadV3(path)
	if err != nil {
		return err
	}

	result := configShowResult{
		Schema:        doc.Schema,
		Path:          path,
		Presets:       buildConfigShowPresets(doc.Presets),
		AgentRuntimes: buildConfigShowRuntimes(doc.AgentRuntimes),
	}

	if mode == "json" {
		b, err := json.Marshal(newEnvelope(configShowOperation, result))
		if err != nil {
			return duoerr.New("internal.config_show_encode_failed", err.Error())
		}
		_, err = fmt.Fprintln(streams.Out, string(b))
		return err
	}

	if len(result.Presets) == 0 {
		_, err := fmt.Fprintln(streams.Out, "no presets")
		return err
	}
	for _, preset := range result.Presets {
		if _, err := fmt.Fprintln(streams.Out, preset.Name); err != nil {
			return err
		}
	}
	return nil
}

func buildConfigShowPresets(presets map[string]config.PresetV3) []configShowPreset {
	names := make([]string, 0, len(presets))
	for name := range presets {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]configShowPreset, 0, len(names))
	for _, name := range names {
		preset := presets[name]
		leaves := make([]string, 0, len(preset.Leaves))
		for leaf := range preset.Leaves {
			leaves = append(leaves, leaf)
		}
		sort.Strings(leaves)
		out = append(out, configShowPreset{Name: name, Leaves: leaves})
	}
	return out
}

func buildConfigShowRuntimes(runtimes map[string]config.AgentRuntime) []configShowRuntime {
	names := make([]string, 0, len(runtimes))
	for name := range runtimes {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]configShowRuntime, 0, len(names))
	for _, name := range names {
		out = append(out, configShowRuntime{Name: name, Kind: runtimes[name].Kind})
	}
	return out
}
