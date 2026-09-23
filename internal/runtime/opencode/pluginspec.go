package opencode

import (
	"fmt"
	"strings"

	"github.com/procrastivity/duo/internal/duoerr"
)

// ErrCodePluginWildcardRemoval is the fail-closed refusal
// ValidatePluginSpecs raises when a generated plugin directive list
// carries a wildcard removal.
const ErrCodePluginWildcardRemoval = "refusal.opencode_plugin_wildcard"

// ValidatePluginSpecs vets the ordered plugin directive list a Duo
// projection is about to write into an OpenCode config document, and
// refuses any wildcard remove directive.
//
// OpenCode merges plugin directives across ordered config documents from
// low to high priority, and a "-<selector>" entry removes what the
// selector matched earlier in that stream. At exact pin 2.0.12 —
// executable SHA-256
// 2b0825721cb12f9bca3d5099588087d557a21ed2b5b56efebea3f17dc5f79e6a,
// source commit 2670273ff17da96f85c5826ced57aa1b368754fa, pin dated and
// live-reverified 2026-09-23 — a high-priority "-*" with no re-add
// removed not only its low-priority target but an unrelated sibling
// declared in the same lower document: in two independent repetitions
// both modules evaluated but neither set up, entered inventory, or
// called back (duo-lab opencode-v2-plugin-mcp-continuation step-02,
// V2PC-06). A sibling re-declared after the wildcard in the higher
// document stayed active in the earlier separate capture, but nothing
// establishes that a re-add rescues a sibling declared before "-*" — and
// a projection cannot enumerate the declarations a user's lower-priority
// documents carry, so it can never prove a wildcard's blast radius stops
// at Duo's own specs.
//
// Duo therefore fails closed: a generated plugin list never contains a
// wildcard removal — "-*", a "-prefix.*" glob, or any "-" selector
// containing "*". Exact-ID removals ("-<id>") are the only emitted
// removal form, and only for a target the projection itself owns. When a
// desired end state cannot be expressed under that rule, the renderer
// reports the optional component unverified (installation contract §4)
// instead of writing a config that silently drops plugins it does not
// own.
func ValidatePluginSpecs(specs []string) error {
	for i, spec := range specs {
		selector, isRemoval := strings.CutPrefix(spec, "-")
		if !isRemoval {
			continue
		}
		if selector == "" || strings.Contains(selector, "*") {
			return duoerr.New(ErrCodePluginWildcardRemoval,
				fmt.Sprintf("opencode: plugin spec %d %q is a wildcard removal; at OpenCode 2.0.12 a high-priority \"-*\" removes every declaration ordered before it across merged config documents, including plugins Duo does not own — emit exact-ID removals only", i, spec))
		}
	}
	return nil
}
