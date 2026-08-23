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
	// CodeCompositionUnresolved reports a missing or ambiguous
	// composition/launch-variant link, a malformed preset declaration, or
	// an exceeded complexity limit. It stays what it has always been:
	// declaration ambiguity, never narrowing.
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

// unresolved reports a declaration defect: a missing or ambiguous
// reference, a malformed preset, or an exceeded complexity limit. locator
// is the declaration locator at fault, and reason continues the sentence it
// starts.
func unresolved(locator, reason string) *Error {
	return newError(CodeCompositionUnresolved,
		fmt.Sprintf("%s %s.", locator, reason),
		Retry{Safe: true, Action: "fix_the_declaration"},
		map[string]any{"declaration_locator": locator, "reason": reason})
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

// exhaustionDetails is the safe detail §6.8 requires on
// launch.constraints_exhausted and launch.no_eligible_candidate, in the
// shape contracts/fixtures/duo-external-v1/session-launch-exhausted.json
// fixes.
//
// Provenance is deliberately absent. §6.8's prose asks for "constraints
// with provenance", but the normative fixture's constraints object carries
// axis/value pairs only (launch_constraint_predicate has
// additionalProperties: false). The fixture wins, and the provenance lives
// where §6.9 puts everything else that explains a resolution: the
// launch-resolution record.
type exhaustionDetails struct {
	RequestedPreset             string          `json:"requested_preset"`
	Constraints                 wireConstraints `json:"constraints"`
	Candidates                  []wireCandidate `json:"candidates"`
	Survivors                   Survivors       `json:"survivors"`
	CompleteAssignmentsSurvived int             `json:"complete_assignments_survived"`
}

// wireCandidate is one candidate's safe row: which leaf declared it, its
// declaration locator, its two public axis values, and — when it did not
// survive — the stable reason it was eliminated.
type wireCandidate struct {
	Leaf               string `json:"leaf"`
	DeclarationLocator string `json:"declaration_locator"`
	AgentRuntime       string `json:"agent_runtime"`
	ModelLine          string `json:"model_line"`
	EliminationReason  string `json:"elimination_reason,omitempty"`
}

// Survivors is §6.4 graft 3's explicit pool report: the declaration
// locators that survived each stage of the pipeline.
//
// In an error's safe details the lists are flat across leaves — leaf order,
// then candidate order, with a locator two leaves share listed once —
// because the fixture fixes them as arrays of locators; which leaf declared
// a locator is recoverable from the candidate rows, which name the leaf. On
// a record leaf they are that one leaf's pools.
type Survivors struct {
	BeforeConstraints     []string `json:"before_constraints"`
	AfterRequire          []string `json:"after_require"`
	AfterAvoid            []string `json:"after_avoid"`
	AfterAvoidRestoration []string `json:"after_avoid_restoration"`
}
