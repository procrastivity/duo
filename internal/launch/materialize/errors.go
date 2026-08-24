package materialize

import (
	"github.com/procrastivity/duo/internal/domain"
	"github.com/procrastivity/duo/internal/duoerr"
	"github.com/procrastivity/duo/internal/registry"
)

// CodeHostUnresolved reports that the fixed deduction ranking produced no
// single host instance for this launch. It is an installation-and-state
// fact, never a caller's malformed request, which is why its class is
// unavailable: the operator's ways out are naming a host, enabling a kind,
// or rebinding the workspace, all of which the details name.
const CodeHostUnresolved = "launch.host_unresolved"

// effectNoEffect is the only effect materialization can carry. M1/M2 run
// entirely before the launch-resolution record commits, so nothing exists
// that a failure here could have half-changed.
const effectNoEffect = "no_effect"

// stableClasses binds a code to its registered class, read once. This
// package never invents a class for a code, for the same reason
// internal/launch does not: duo-vnext-access-errors-audit.md §4.1's
// classification is single-sourced in internal/registry.
var stableClasses = registry.StableErrorCodes()

// Error is one materialization failure in the duo.external/v1 error shape,
// so marshaling it yields exactly the object a session.launch error
// envelope carries.
//
// It duplicates internal/launch's Error shape rather than reusing the type,
// and that is structural, not an oversight: step 12 makes the resolver
// consume this package's evidence bundle, so internal/launch imports
// internal/launch/materialize and the reverse edge cannot exist. The two
// shapes are field-for-field identical and validate against the same
// schema, which is what lets one renderer handle both.
type Error struct {
	Class   string `json:"class"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Retry   Retry  `json:"retry"`
	Effect  string `json:"effect"`
	Details any    `json:"details,omitempty"`

	// result is the partial materialization the failure happened in. It
	// carries the trail and the captured evidence so `duo doctor` can
	// explain a workspace that would not launch without re-running
	// anything.
	result Result
}

// Retry is duo.external/v1's retry-advice object.
type Retry struct {
	Safe   bool   `json:"safe"`
	Action string `json:"action"`
}

func (e *Error) Error() string { return e.Message }

// Duo projects the failure onto the chassis's structured error, which is
// what the CLI renders. Class, retry advice, and details do not survive the
// projection; a caller that needs them consumes *Error directly.
func (e *Error) Duo() *duoerr.Error { return duoerr.New(e.Code, e.Message) }

// Result returns the partial materialization: the full deduction trail,
// whatever evidence was captured, and the provider snapshot. The deduced
// host is absent, which is the failure.
func (e *Error) Result() Result { return e.result }

// PointerSet is the closed set of ways out a launch failure names.
// duo.external/v1's `launch_pointer_set`.
type PointerSet struct {
	OverrideFlag        string `json:"override_flag,omitempty"`
	ProviderEnable      string `json:"provider_enable,omitempty"`
	WorkspaceHostRebind string `json:"workspace_host_rebind,omitempty"`
}

// hostUnresolvedPointers is the pointer set this failure always names: the
// override flag and the audited rebind verb. Provider enablement is not a
// way out of an unresolved host, so it is deliberately absent.
var hostUnresolvedPointers = PointerSet{
	OverrideFlag:        "--host",
	WorkspaceHostRebind: "duo workspace host rebind",
}

// HostUnresolvedDetails is the safe detail payload of a
// launch.host_unresolved failure. duo.external/v1's
// `launch_host_unresolved_details`.
//
// The trail is the whole point of the payload: an operator reading it sees
// every rung, whether it was consulted, and why it produced nothing — which
// is the difference between "Duo could not find a host" and an actionable
// message.
type HostUnresolvedDetails struct {
	WorkspaceID     string      `json:"workspace_id,omitempty"`
	RequestedPreset string      `json:"requested_preset,omitempty"`
	DeductionTrail  []WireRung  `json:"deduction_trail"`
	EnabledKinds    []string    `json:"enabled_kinds"`
	EvidenceBundle  *WireBundle `json:"evidence_bundle,omitempty"`
	Pointers        PointerSet  `json:"pointers"`
}

// WireRung is one deduction rung in its duo.external/v1 spelling.
type WireRung struct {
	Source        string `json:"source"`
	Consulted     bool   `json:"consulted"`
	YieldedHost   bool   `json:"yielded_host"`
	Kind          string `json:"kind,omitempty"`
	InstanceLabel string `json:"instance_label,omitempty"`
	Detail        string `json:"detail,omitempty"`
}

// WireBundle is the evidence bundle's reference set in its
// duo.external/v1 spelling: what the failure rested on, by ID, so a later
// reader can replay it.
type WireBundle struct {
	CorrelationFactID string        `json:"correlation_fact_id,omitempty"`
	AmbientCaptures   []WireCapture `json:"ambient_captures,omitempty"`
	ProviderFactIDs   []string      `json:"provider_fact_ids,omitempty"`
}

// WireCapture is one ambient capture in its duo.external/v1 spelling.
type WireCapture struct {
	Name       string `json:"name"`
	Value      string `json:"value,omitempty"`
	CapturedAt string `json:"captured_at,omitempty"`
}

// hostUnresolved builds the failure from a partial materialization.
//
// The message is one fixed sentence across every way the ladder can come up
// empty — no rung yielded, an explicit kind matched no instance, discovery
// found several. The variation lives in the rung details, where it can be
// read, rather than in the message, where a fixture would have to enumerate
// it.
func hostUnresolved(r Result, preset string, enabledKinds []string) *Error {
	trail := make([]WireRung, 0, len(r.trail))
	for _, rung := range r.trail {
		trail = append(trail, WireRung{
			Source:        string(rung.Source),
			Consulted:     rung.Consulted,
			YieldedHost:   rung.YieldedHost,
			Kind:          rung.Kind,
			InstanceLabel: rung.Instance,
			Detail:        rung.Detail,
		})
	}
	if enabledKinds == nil {
		enabledKinds = []string{}
	}

	details := HostUnresolvedDetails{
		WorkspaceID:     string(r.workspaceID),
		RequestedPreset: preset,
		DeductionTrail:  trail,
		EnabledKinds:    enabledKinds,
		EvidenceBundle:  wireBundle(r.bundle),
		Pointers:        hostUnresolvedPointers,
	}
	return newError(CodeHostUnresolved,
		"Host deduction produced no candidate host for this launch.",
		Retry{Safe: true, Action: "supply_host_or_enable_kind"},
		details, r)
}

// wireBundle renders the bundle's reference set, or nil when the bundle
// references nothing at all — an empty object would claim evidence that
// does not exist.
func wireBundle(b EvidenceBundle) *WireBundle {
	out := &WireBundle{}
	if id, ok := b.CorrelationFactID(); ok {
		out.CorrelationFactID = string(id)
	}
	for _, c := range b.AmbientCaptures() {
		out.AmbientCaptures = append(out.AmbientCaptures, WireCapture(c))
	}
	for _, id := range b.ProviderFactIDs() {
		out.ProviderFactIDs = append(out.ProviderFactIDs, string(id))
	}
	if out.CorrelationFactID == "" && out.AmbientCaptures == nil && out.ProviderFactIDs == nil {
		return nil
	}
	return out
}

// newError builds a failure, taking its class from the registered stable
// set so code and class can never disagree.
func newError(code, message string, retry Retry, details any, r Result) *Error {
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
		result:  r,
	}
}

// compile-time proof that the wire capture and the in-memory capture stay
// convertible: the conversion in wireBundle is what breaks if a field is
// added to one and not the other.
var _ = func(c AmbientCapture) WireCapture { return WireCapture(c) }

// compile-time proof that domain's host-source vocabulary is what the wire
// rung's `source` carries.
var _ domain.HostSource = domain.HostSourceExplicitFlag
