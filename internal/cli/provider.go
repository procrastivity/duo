// provider.go is the composition root for the `duo provider` verb family
// (workplan Step 08): the parent verb, the shared result shapes, and the
// shared plumbing every provider subcommand uses. The three subcommands
// themselves — each naming its one registered operation — live in
// provider_disable.go, provider_enable.go, and provider_list.go
// (internal/registry/conformance_test.go's TestNoDuplicateOperationTable
// OutsideRegistry caps how many registered operation names one file may
// name as a literal; splitting by verb, the way the session family already
// does, keeps every file under that cap).
//
// The CLI wires directly against internal/domain's
// Authority.{Disable,Enable}Provider and StandingProviderFacts, and against
// internal/config's v3 loader for the "a provider exists when a variant
// names it" rule these verbs answer to (notes/42 §8, notes/43 item 14).
//
// Nothing here does what thread 4 (workplan Risk 3: --until, a recorded
// disable reason) asks for — the fact payload (domain.ProviderFact.Note)
// stays present and empty, and no flag sets it.

package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/procrastivity/duo/internal/cliflags"
	"github.com/procrastivity/duo/internal/config"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/iostreams"
)

// providerToggleResult is the disable and enable verbs' shared result shape.
type providerToggleResult struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	FactID  string `json:"fact_id"`
	// Known reports whether a launch_variant in the resolved config names
	// this provider. false is not a refusal — disabling or enabling an
	// unknown name is allowed (workplan Step 08) — it only changes what the
	// text-mode render says.
	Known bool `json:"known"`
}

// providerListItem is one entry in the list verb's result: every provider a
// launch_variant names, every provider with a standing fact, or both.
type providerListItem struct {
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	// FactID is empty when the name has no standing fact — enabled is then
	// the default-enabled value, not a recorded decision.
	FactID string `json:"fact_id,omitempty"`
	Known  bool   `json:"known"`
}

// providerListResult is the list verb's result.
type providerListResult struct {
	Providers []providerListItem `json:"providers"`
}

// providerCommand builds the `duo provider` parent verb and its three
// registered subcommands.
func providerCommand(streams *iostreams.Streams) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "list, disable, and enable providers a launch_variant names",
	}
	cmd.AddCommand(providerDisableCommand(streams))
	cmd.AddCommand(providerEnableCommand(streams))
	cmd.AddCommand(providerListCommand(streams))
	return cmd
}

// providerConfigFlag binds the --config flag every provider subcommand
// shares: the duo.config/v3 document that answers whether a launch_variant
// names a given provider.
func providerConfigFlag(cmd *cobra.Command, configPath *string) {
	cmd.Flags().StringVar(configPath, "config", "",
		"path to the duo.config/v3 document (defaults to $XDG_CONFIG_HOME/duo/duo.config.yaml)")
}

// providerActorFlags binds the --actor and --reason flags the disable and
// enable verbs share.
func providerActorFlags(cmd *cobra.Command, actor, reason *string) {
	cmd.Flags().StringVar(actor, "actor", "cli", "the responsible actor recorded on the fact")
	cmd.Flags().StringVar(reason, "reason", "", "why, in one phrase")
}

// providerToggleParams is runProviderToggle's argument bundle. operation
// carries the caller's one registered operation name (its own literal,
// named at the call site in provider_disable.go or provider_enable.go, not
// here) for the --output json envelope.
type providerToggleParams struct {
	operation  string
	name       string
	enable     bool
	configPath string
	actor      string
	reason     string
}

// runProviderToggle is the disable and enable verbs' shared body.
func runProviderToggle(cmd *cobra.Command, streams *iostreams.Streams, p providerToggleParams) error {
	mode := cliflags.FromContext(cmd.Context()).Output

	doc, err := loadProviderConfig(p.configPath)
	if err != nil {
		return err
	}
	known := variantProviders(doc)

	a, s, err := openWriteAuthority(cmd.Context())
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	var factID string
	if p.enable {
		id, err := a.EnableProvider(cmd.Context(), p.name, p.actor, p.reason)
		if err != nil {
			return duoerrFromDomain(err)
		}
		factID = string(id)
	} else {
		id, err := a.DisableProvider(cmd.Context(), p.name, p.actor, p.reason)
		if err != nil {
			return duoerrFromDomain(err)
		}
		factID = string(id)
	}

	result := providerToggleResult{
		Name:    p.name,
		Enabled: p.enable,
		FactID:  factID,
		Known:   known[p.name],
	}

	if mode == "json" {
		b, err := json.Marshal(newEnvelope(p.operation, result))
		if err != nil {
			return duoerr.New("internal.provider_toggle_encode_failed", err.Error())
		}
		_, err = fmt.Fprintln(streams.Out, string(b))
		return err
	}

	state := "disabled"
	if p.enable {
		state = "enabled"
	}
	if _, err := fmt.Fprintf(streams.Out, "provider %s: %s (fact %s)\n", p.name, state, factID); err != nil {
		return err
	}
	if !result.Known {
		if _, err := fmt.Fprintln(streams.Out, "no variant names this provider"); err != nil {
			return err
		}
	}
	return nil
}

// loadProviderConfig resolves and strictly loads the duo.config/v3 document
// the provider verbs check launch_variant provider references against. A
// missing config file is not an error — it mirrors openReadAuthority's "a
// missing store is not an error" discipline (session.go) — it just means no
// provider is known from config; every provider verb still functions
// against standing facts alone.
func loadProviderConfig(configPath string) (config.DocumentV3, error) {
	path := configPath
	if path == "" {
		p, err := defaultLaunchConfigPath()
		if err != nil {
			return config.DocumentV3{}, duoerr.New("internal.config_path_unresolved",
				fmt.Sprintf("resolving the default duo.config/v3 path: %v", err))
		}
		path = p
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return config.DocumentV3{}, nil
	} else if statErr != nil {
		return config.DocumentV3{}, duoerr.New("internal.config_stat_failed",
			fmt.Sprintf("checking %s: %v", path, statErr))
	}
	doc, err := config.LoadV3(path)
	if err != nil {
		return config.DocumentV3{}, err // already a *duoerr.Error
	}
	return doc, nil
}

// variantProviders collects the distinct, non-empty provider names doc's
// launch_variants reference: a provider exists when a variant names it
// (workplan Step 08; internal/config/v3.go's LaunchVariant.Provider).
func variantProviders(doc config.DocumentV3) map[string]bool {
	out := make(map[string]bool, len(doc.LaunchVariants))
	for _, v := range doc.LaunchVariants {
		if v.Provider != "" {
			out[v.Provider] = true
		}
	}
	return out
}
