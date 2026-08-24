package launch

import (
	"fmt"

	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/registry"
)

// The stable error codes launch resolution raises (§6.8's failure table).
// Every one is registered in internal/registry's stable set, which is also
// where each code's closed class comes from — this package never invents a
// class for a code.
const (
	// CodePresetNotFound reports a requested preset no declaration
	// defines. Safe detail: the requested name.
	CodePresetNotFound = "preset.not_found"
	// CodeInvalidRequest reports malformed constraint grammar or
	// contradictory same-axis requirements.
	CodeInvalidRequest = "invalid.request"
	// CodeVariantUnresolved reports a missing or ambiguous launch-variant
	// or agent-runtime reference, a malformed preset declaration, or an
	// exceeded complexity limit, under `duo.config/v3`. It is declaration
	// ambiguity, never narrowing, and its class is unavailable
	// (duo-vnext-access-errors-audit.md §4.1, "duo.config/v3 launch
	// codes", 2026-08-24 handoff 22).
	//
	// It is the successor to CodeCompositionUnresolved, not an addition
	// beside it: this resolver reads `duo.config/v3` documents only, so
	// every declaration defect it can find is a variant or runtime one.
	CodeVariantUnresolved = "config.variant_unresolved"
	// CodeCompositionUnresolved is CodeVariantUnresolved's deprecated
	// predecessor, deprecated 2026-08-24 (handoff 22).
	//
	// It stays a registered stable code and stays exported, because
	// clients must keep accepting it while `duo.config/v2` documents
	// remain loadable — but nothing in this package emits it any more.
	// This resolver is v3-only (NewResolver takes a config.DocumentV3), so
	// there is no v2 path left here to raise it from; the constant is a
	// name a v2-era projection can still compare against, and the registry
	// keeps its class.
	//
	// Deprecated: use CodeVariantUnresolved. Emitted for duo.config/v2
	// documents only; no duo.config/v3 resolution raises it.
	CodeCompositionUnresolved = "config.composition_unresolved"
	// CodeConstraintsExhausted reports that a require or a cross-leaf
	// relation left no complete assignment even after every avoid relented.
	// It is the caller-correctable narrowing error, class invalid.
	CodeConstraintsExhausted = "launch.constraints_exhausted"
	// CodeNoEligibleCandidate reports that installed evidence or
	// enabled-host declarations left no complete assignment *before* any
	// launch constraint applied. It is an installation fact, class
	// unavailable, and never a narrowing failure.
	CodeNoEligibleCandidate = "launch.no_eligible_candidate"
	// CodeInternalFailure reports entropy failure before a random
	// selection was recorded, or an internal invariant break.
	CodeInternalFailure = "internal.failure"
)

// effectNoEffect is the only effect a launch failure can carry. §6.8:
// "Every row has effect: no_effect; no runtime instance or process exists."
// Resolution runs entirely before the pre-spawn commit, so there is no
// state a failure could have half-changed.
const effectNoEffect = "no_effect"

// stableClasses is the registry's code-to-class binding, read once. Binding
// the class here rather than restating it keeps
// duo-vnext-access-errors-audit.md §4.1's closed classification
// single-sourced; TestErrorCodesAreRegisteredForSessionLaunch pins that
// every code this package raises is in that set.
var stableClasses = registry.StableErrorCodes()

// Error is one launch-resolution failure in the duo.external/v1 error
// shape. Marshaling it yields exactly the object a session.launch error
// envelope carries, which is what lets a fixture comparison be an equality
// check rather than a field-by-field inspection.
//
// Details is the *safe* detail only. §6.8 keeps paths, environment, raw
// adapter failures, and other sensitive local detail behind
// diagnostics.read; nothing this package puts in Details needs a
// permission beyond the operation's own.
type Error struct {
	Class   string `json:"class"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Retry   Retry  `json:"retry"`
	Effect  string `json:"effect"`
	Details any    `json:"details,omitempty"`
}

// Retry is duo-external-v1's retry advice object.
type Retry struct {
	Safe   bool   `json:"safe"`
	Action string `json:"action"`
}

func (e *Error) Error() string { return e.Message }

// Duo projects the launch error onto the chassis's structured error type,
// which is what the CLI renders. The class, retry advice, and safe details
// do not survive the projection — duoerr.Error carries a code and a message
// only — so a projection that needs them consumes *Error directly.
func (e *Error) Duo() *duoerr.Error { return duoerr.New(e.Code, e.Message) }

// newError builds a failure, taking its class from the registered stable
// set so that code and class can never disagree.
func newError(code, message string, retry Retry, details any) *Error {
	class := "internal"
	if c, ok := stableClasses[code]; ok {
		class = string(c)
	}
	return &Error{
		Class:   class,
		Code:    code,
		Message: message,
		Retry:   retry,
		Effect:  effectNoEffect,
		Details: details,
	}
}

// Message style follows the synced error fixtures: one capitalized
// sentence ending in a period, with no package or verb prefix. The chassis
// adds "duo: <verb>: " when it renders a duoerr.Error in human mode, and
// the JSON envelope carries the sentence unchanged.

// invalidRequest reports malformed grammar or a contradictory requirement.
func invalidRequest(message string) *Error {
	return newError(CodeInvalidRequest, message,
		Retry{Safe: true, Action: "correct_the_request"}, nil)
}

// presetNotFound reports an unknown preset. Its safe detail is the
// requested name and nothing else: listing the declared presets would leak
// installation shape to a caller that could not launch them anyway.
func presetNotFound(name string) *Error {
	return newError(CodePresetNotFound,
		fmt.Sprintf("No preset named %q is declared.", name),
		Retry{Safe: true, Action: "choose_a_declared_preset"},
		map[string]any{"requested_preset": name})
}

// configSchemaV3 is the configuration schema marker every declaration
// failure this resolver raises names. It is a constant rather than a value
// read off the document because the resolver is v3-only: a document that
// reached it was parsed by config.ParseV3 or it did not reach it at all.
const configSchemaV3 = "duo.config/v3"

// unresolved reports a declaration defect: a missing or ambiguous
// launch-variant or agent-runtime reference, a malformed preset, or an
// exceeded complexity limit. locator is the declaration locator at fault,
// and reason continues the sentence it starts.
//
// Its safe detail is duo-external-v1's `config_declaration_details`: the
// offending locator, the configuration schema that produced it, and the
// reason. Naming the schema is what makes the successor code readable — a
// caller that sees config.variant_unresolved with `duo.config/v3` knows the
// document it must fix is a v3 one, and a v2-era client that still knows
// only config.composition_unresolved knows why it is seeing a code it does
// not recognize.
func unresolved(locator, reason string) *Error {
	return newError(CodeVariantUnresolved,
		fmt.Sprintf("%s %s.", locator, reason),
		Retry{Safe: true, Action: "fix_the_declaration"},
		declarationDetails{Locator: locator, ConfigSchema: configSchemaV3, Reason: reason})
}

// declarationDetails is duo-external-v1's `config_declaration_details`.
type declarationDetails struct {
	Locator      string `json:"locator"`
	ConfigSchema string `json:"config_schema"`
	Reason       string `json:"reason"`
}

// internalFailure reports an internal break — today only a failure to draw
// entropy before a random selection was recorded. reference is a
// diagnostics reference, never the underlying error text, which may name
// local paths.
func internalFailure(reference string) *Error {
	return newError(CodeInternalFailure,
		"Launch resolution failed internally.",
		Retry{Safe: true, Action: "retry_or_report"},
		map[string]any{"diagnostics_reference": reference})
}
