// Package registry is the binary's single operation registry: one Go table
// of public semantic operation descriptors, from which manifest operation
// metadata, CLI verb paths, the initial MCP tool set, presentation route
// metadata, and fixture inventory all derive (duo-vnext-go-architecture.md
// §8). Nothing else in the binary may hand-maintain a duplicate operation
// table — a consumer either calls a view function here or walks a value one
// of them returned.
//
// The table is authored Go data, derived at runtime, not generated at build
// time. docs/registry/decisions.md records that choice and the row policy
// for operations the planning set names but the synced contract set cannot
// yet back with a resolvable schema or fixture.
package registry

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"slices"
)

// Projectability classifies where an operation may project
// (duo-vnext-public-contract.md §6): deterministic operations can reach
// CLI, MCP, and presentation; local_admin operations reach the CLI and
// separately enrolled local administration clients only; human LLM
// porcelain stays CLI-only.
type Projectability string

const (
	// Deterministic operations can project into CLI, MCP, and
	// presentation forms.
	Deterministic Projectability = "deterministic"
	// LocalAdmin operations project into the CLI and an authorized local
	// administration client, never the generated MCP tool set.
	LocalAdmin Projectability = "local_admin"
	// HumanLLMPorcelain stays CLI-only and never generates into MCP tools
	// or harness instructions.
	HumanLLMPorcelain Projectability = "human_llm_porcelain"
)

// Idempotency is the registry's closed idempotency requirement enum,
// matching the duo.manifest/v1 operations item.
type Idempotency string

// The three idempotency requirements. A row records the strictest
// applicable answer; conditional exemptions (session.launch --dry-run)
// stay prose in the descriptor's comment and the launch contract.
const (
	IdempotencyRequired      Idempotency = "required"
	IdempotencyOptional      Idempotency = "optional"
	IdempotencyNotApplicable Idempotency = "not_applicable"
)

// AuditCategory names the required audit category for an operation. The
// vocabulary is closed here; "none" is explicit rather than an absent
// field so a row cannot skip the question by omission.
type AuditCategory string

const (
	// AuditNone requires no audit record beyond the shared
	// authorization-decision history.
	AuditNone AuditCategory = "none"
	// AuditSensitiveWrite marks a write whose payload or effect is
	// sensitive (prompt delivery, lease acquisition) — audited with payload
	// sensitivity classing per duo-vnext-access-errors-audit.md §5.
	AuditSensitiveWrite AuditCategory = "sensitive_write"
	// AuditPrivilegedWrite marks a privileged lifecycle write (launch,
	// enroll, detach, reattach, remove). Every attempt and every resolution
	// failure is audited.
	AuditPrivilegedWrite AuditCategory = "privileged_write"
)

// AuditCategories is the closed audit vocabulary rows may use.
var AuditCategories = []AuditCategory{AuditNone, AuditSensitiveWrite, AuditPrivilegedWrite}

// ErrorClass is one of the closed public error classes
// (duo-vnext-access-errors-audit.md §4).
type ErrorClass string

// ErrorClasses is the closed class list. Order follows the contract table.
var ErrorClasses = []ErrorClass{
	"invalid", "not_found", "conflict", "unsupported", "unavailable",
	"degraded", "unauthorized", "disconnected", "timeout", "indeterminate",
	"internal",
}

// SchemaRef names a schema in the embedded contract set. Family is the
// public schema family string (e.g. "duo.external/v1"); File is the path of
// the backing JSON Schema inside contracts.FS; Def optionally names a key
// under that schema's $defs when the operation has a dedicated definition.
// An empty Def means the reference is to the family's envelope itself —
// the per-operation constraint then lives in the schema's own conditional
// (allOf/if) blocks, not in a named definition. The synced contract set
// carries no named request definitions yet, so every request ref currently
// has an empty Def; docs/registry/decisions.md holds the row policy.
type SchemaRef struct {
	Family string
	File   string
	Def    string
}

// String renders the manifest-facing reference form: the family alone, or
// "family#def" when a named definition exists.
func (r SchemaRef) String() string {
	if r.Def == "" {
		return r.Family
	}
	return r.Family + "#" + r.Def
}

// Resolve checks the reference against an embedded contract filesystem:
// the file must exist and parse as JSON, and Def (when set) must name a key
// under its $defs. This is the same check the registry tests run against
// contracts.FS, exported so a future `duo doctor` static pass can reuse it.
func (r SchemaRef) Resolve(fsys fs.FS) error {
	data, err := fs.ReadFile(fsys, r.File)
	if err != nil {
		return fmt.Errorf("registry: schema ref %q: %w", r.String(), err)
	}
	var doc struct {
		Defs map[string]json.RawMessage `json:"$defs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("registry: schema ref %q: parsing %s: %w", r.String(), r.File, err)
	}
	if r.Def == "" {
		return nil
	}
	if _, ok := doc.Defs[r.Def]; !ok {
		return fmt.Errorf("registry: schema ref %q: %s has no $defs/%s", r.String(), r.File, r.Def)
	}
	return nil
}

// Route is an operation's presentation-service projection: method plus the
// v1 resource path template from duo-vnext-projection-contracts.md §4.2.
// The route syntax is a projection; the registry row remains the semantic
// authority.
type Route struct {
	Method string
	Path   string
}

// Descriptor is one public semantic operation's registry entry. The
// contract-owned fields (Name through Audit) follow
// duo-vnext-public-contract.md §6; the projection fields (CLI, MCPTool,
// Route) and the evidence fields (ErrorCodes, Fixtures, Milestone) are the
// derivation inputs §8 of the architecture asks the registry to carry.
type Descriptor struct {
	// Name is the stable dotted operation name, e.g. "session.launch".
	Name string
	// Projectability classifies which projections may offer the
	// operation. It answers a different question than Permissions.
	Projectability Projectability
	// RequestSchema and ResultSchema reference the embedded contracts.
	RequestSchema SchemaRef
	ResultSchema  SchemaRef
	// Permissions are the required permission names
	// (duo-vnext-access-errors-audit.md §1.1). Empty means no grant is
	// required (installed-product reads like manifest.show).
	Permissions []string
	// Idempotency is the operation's idempotency requirement. A single
	// enum per the manifest shape: session.launch records "required"
	// even though the contract exempts --dry-run, which creates nothing.
	Idempotency Idempotency
	// StreamFamily is the result stream when one exists
	// (duo-vnext-external-surfaces.md §4.7 accepted scopes), else empty.
	StreamFamily string
	// Audit is the required audit category.
	Audit AuditCategory
	// CLI is the verb path under the duo root, e.g. {"session", "launch"};
	// nil means no CLI projection (no such row exists today — every
	// registered operation projects to the CLI).
	CLI []string
	// MCPTool is the stable MCP tool name projecting this operation, or
	// empty when the operation is absent from the initial generated MCP
	// tool set. The subscription opener tool is shared across subscribe
	// operations and disambiguated by StreamFamily.
	MCPTool string
	// Route is the presentation route, or nil when the v1 resource shape
	// defines none (local_admin operations and manage-group lifecycle).
	Route *Route
	// ErrorCodes lists operation-specific stable codes beyond the shared
	// baseline (invalid.request, object.not_found, policy.*, ...), which
	// applies to every operation and is not repeated per row. Every entry
	// must be in the registered stable set.
	ErrorCodes []string
	// Fixtures lists contracts.FS fixture files that exemplify this
	// operation's envelopes. Empty when the contract set ships none yet
	// (the Stage 1 gate authors enrollment and detach fixtures, G-13).
	Fixtures []string
	// Milestone marks operations the dogfood milestone ships
	// (2026-08-23 handoff 20 amendment): these get full descriptors that
	// Stage 0/1 wiring consumes. Non-milestone rows are contract data
	// recorded ahead of their implementation.
	Milestone bool
}

// All returns every registered descriptor. The returned slice is a copy;
// the descriptors' reference fields are shared and must not be mutated.
func All() []Descriptor {
	return slices.Clone(table)
}

// Lookup returns the descriptor registered under name.
func Lookup(name string) (Descriptor, bool) {
	for _, d := range table {
		if d.Name == name {
			return d, true
		}
	}
	return Descriptor{}, false
}

// StableErrorCodes returns the registered stable error-code set as a
// code-to-class map (duo-vnext-access-errors-audit.md §4.1). The returned
// map is a copy.
func StableErrorCodes() map[string]ErrorClass {
	out := make(map[string]ErrorClass, len(stableErrorCodes))
	for code, class := range stableErrorCodes {
		out[code] = class
	}
	return out
}

// DeferredOperations returns the operations the synced contract set names
// but the table deliberately omits, mapped to the reason each has no row
// (docs/registry/decisions.md, "Attestation is necessary, not sufficient").
// The returned map is a copy.
func DeferredOperations() map[string]string {
	out := make(map[string]string, len(deferredOperations))
	for name, reason := range deferredOperations {
		out[name] = reason
	}
	return out
}

// PermissionNames returns the registered permission vocabulary
// (duo-vnext-access-errors-audit.md §1.1) as a sorted slice.
func PermissionNames() []string {
	out := slices.Clone(permissionNames)
	slices.Sort(out)
	return out
}
