package manifest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/procrastivity/duo/internal/surface"
)

// outputSchemaAnnotation is the cobra.Command.Annotations key a verb's
// declared output schema (if any) is recorded under, mirroring
// internal/surface's own annotation-on-the-command approach: the
// registration lives on the command itself, so this walk needs no parallel
// registry.
const outputSchemaAnnotation = "duo.output-schema"

// SetOutputSchema records schema as cmd's declared --output json shape. No
// verb calls this yet — the field exists in the manifest shape ready for
// the first verb that earns one; it is not filled speculatively.
func SetOutputSchema(cmd *cobra.Command, schema json.RawMessage) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[outputSchemaAnnotation] = string(schema)
}

// walkVerbs collects every leaf, non-hidden command under root, regardless
// of surface kind. A hidden command (e.g. internal/selftest's fixture,
// gated behind DUO_SELFTEST) never appears in a real install or a manifest
// walk, per its own doc comment.
func walkVerbs(root *cobra.Command) ([]Verb, error) {
	var verbs []Verb
	if err := collect(root, root, &verbs); err != nil {
		return nil, err
	}
	return verbs, nil
}

// cobraBuiltins names the commands Cobra adds to every root by default
// (help, completion and its per-shell subcommands) — infrastructure of the
// CLI framework, not a duo verb, and never annotated with a surface kind.
var cobraBuiltins = map[string]bool{"help": true, "completion": true}

func collect(root, cmd *cobra.Command, out *[]Verb) error {
	for _, child := range cmd.Commands() {
		if child.Hidden || cobraBuiltins[child.Name()] {
			continue
		}
		if len(child.Commands()) > 0 {
			if err := collect(root, child, out); err != nil {
				return err
			}
			continue
		}
		if !child.Runnable() {
			continue
		}

		kind, ok := surface.Of(child)
		if !ok {
			return fmt.Errorf("verb %q declares no manifest surface kind — every registered verb must call surface.Annotate", verbPath(root, child))
		}

		v := Verb{
			Name:        verbPath(root, child),
			Kind:        kind,
			Args:        walkArgs(child),
			Description: child.Short,
		}
		if schema, ok := child.Annotations[outputSchemaAnnotation]; ok && schema != "" {
			v.OutputSchema = json.RawMessage(schema)
		}
		*out = append(*out, v)
	}
	return nil
}

// verbPath names a command by its full subcommand path, minus the root
// command's own name — "session list", not "duo session list" — mirroring
// internal/cli.Execute's own verbPath helper (the chassis's error-message
// rendering uses the same shape for a different purpose).
func verbPath(root, cmd *cobra.Command) string {
	path := strings.TrimPrefix(cmd.CommandPath(), root.Name())
	return strings.TrimSpace(path)
}

// cobraBuiltinFlags names flags Cobra adds to every command automatically
// (--help/-h) — framework infrastructure, not a verb-declared arg.
var cobraBuiltinFlags = map[string]bool{"help": true}

// walkArgs describes cmd's own declared flags (LocalFlags — not the
// inherited global --output/-v pair the chassis binds at root).
func walkArgs(cmd *cobra.Command) []Arg {
	var args []Arg
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if cobraBuiltinFlags[f.Name] {
			return
		}
		_, required := f.Annotations[cobra.BashCompOneRequiredFlag]
		args = append(args, Arg{
			Name:     f.Name,
			Type:     f.Value.Type(),
			Required: required,
		})
	})
	return args
}
