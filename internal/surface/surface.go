// Package surface names the manifest surface kinds every verb declares on
// its registration: plumbing, llm, control-plane. Manifest generation reads
// this field directly off a command's registration — it is the mechanical
// enforcement point for the porcelain/plumbing split, never inferred from
// folder name or convention.
package surface

import "github.com/spf13/cobra"

// Kind is a verb's manifest surface kind.
type Kind string

const (
	// Plumbing verbs are deterministic: JSON + exit codes, no LLM.
	Plumbing Kind = "plumbing"
	// LLM verbs do CLI-side LLM shaping; no MCP.
	LLM Kind = "llm"
	// ControlPlane verbs require a live control-plane surface.
	ControlPlane Kind = "control-plane"
)

// annotationKind is the cobra.Command.Annotations key a verb's kind is
// recorded under. The registration lives on the command itself so a
// manifest generator can walk the command tree without a parallel registry.
const annotationKind = "duo.surface-kind"

// Annotate records kind as cmd's explicit, machine-readable surface kind.
func Annotate(cmd *cobra.Command, kind Kind) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations[annotationKind] = string(kind)
}

// Of returns the kind recorded on cmd by Annotate, and whether one was found.
func Of(cmd *cobra.Command) (Kind, bool) {
	k, ok := cmd.Annotations[annotationKind]
	return Kind(k), ok
}
