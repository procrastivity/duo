package registry_test

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/registry"
)

// operationName is the duo.external/v1 envelope's operation pattern; error
// codes share the same dotted shape.
var operationName = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)

// cliSegment is one CLI verb-path segment: lowercase, digits, hyphens.
var cliSegment = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func TestDescriptorNamesUniqueAndWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, d := range registry.All() {
		if !operationName.MatchString(d.Name) {
			t.Errorf("operation %q does not match the envelope operation pattern", d.Name)
		}
		if seen[d.Name] {
			t.Errorf("operation %q registered twice", d.Name)
		}
		seen[d.Name] = true
	}
}

func TestCLIPathsUniqueAndWellFormed(t *testing.T) {
	seen := map[string]string{}
	for _, d := range registry.All() {
		if len(d.CLI) == 0 {
			t.Errorf("operation %q has no CLI projection; every registered operation projects to the CLI", d.Name)
			continue
		}
		for _, seg := range d.CLI {
			if !cliSegment.MatchString(seg) {
				t.Errorf("operation %q CLI segment %q is not a lowercase verb-path segment", d.Name, seg)
			}
		}
		path := strings.Join(d.CLI, " ")
		if prev, ok := seen[path]; ok {
			t.Errorf("CLI path %q claimed by both %q and %q", path, prev, d.Name)
		}
		seen[path] = d.Name
	}
}

// TestMCPToolNamesUnique allows exactly one sharing shape: the subscription
// opener tool multiplexes stream families, so descriptors may share a tool
// name only when each carries a distinct, non-empty StreamFamily.
func TestMCPToolNamesUnique(t *testing.T) {
	byTool := map[string][]registry.Descriptor{}
	for _, d := range registry.All() {
		if d.MCPTool == "" {
			continue
		}
		byTool[d.MCPTool] = append(byTool[d.MCPTool], d)
	}
	for tool, ds := range byTool {
		if len(ds) == 1 {
			continue
		}
		families := map[string]bool{}
		for _, d := range ds {
			if d.StreamFamily == "" {
				t.Errorf("MCP tool %q shared by %q, which has no stream family to disambiguate it", tool, d.Name)
			}
			if families[d.StreamFamily] {
				t.Errorf("MCP tool %q shared by two operations on stream family %q", tool, d.StreamFamily)
			}
			families[d.StreamFamily] = true
		}
	}
}

func TestRoutesUnique(t *testing.T) {
	seen := map[string]string{}
	for _, d := range registry.All() {
		if d.Route == nil {
			continue
		}
		key := d.Route.Method + " " + d.Route.Path
		if prev, ok := seen[key]; ok {
			t.Errorf("route %q claimed by both %q and %q", key, prev, d.Name)
		}
		seen[key] = d.Name
	}
}

func TestSchemaReferencesResolve(t *testing.T) {
	for _, d := range registry.All() {
		if err := d.RequestSchema.Resolve(contracts.FS); err != nil {
			t.Errorf("operation %q request schema: %v", d.Name, err)
		}
		if err := d.ResultSchema.Resolve(contracts.FS); err != nil {
			t.Errorf("operation %q result schema: %v", d.Name, err)
		}
	}
}

func TestFixtureReferencesResolve(t *testing.T) {
	for _, path := range registry.FixtureFiles() {
		if _, err := contracts.FS.ReadFile(path); err != nil {
			t.Errorf("fixture %q not readable from embedded contracts: %v", path, err)
		}
	}
}

func TestErrorCodesAreRegistered(t *testing.T) {
	stable := registry.StableErrorCodes()
	classes := map[registry.ErrorClass]bool{}
	for _, c := range registry.ErrorClasses {
		classes[c] = true
	}
	for code, class := range stable {
		if !operationName.MatchString(code) {
			t.Errorf("stable code %q does not match the envelope code pattern", code)
		}
		if !classes[class] {
			t.Errorf("stable code %q maps to unknown class %q", code, class)
		}
	}
	for _, d := range registry.All() {
		for _, code := range d.ErrorCodes {
			if _, ok := stable[code]; !ok {
				t.Errorf("operation %q claims error code %q, which is not in the registered stable set", d.Name, code)
			}
		}
	}
}

func TestPermissionsAndAuditAreRegistered(t *testing.T) {
	perms := registry.PermissionNames()
	for _, d := range registry.All() {
		for _, p := range d.Permissions {
			if !slices.Contains(perms, p) {
				t.Errorf("operation %q claims permission %q, which is not in the registered vocabulary", d.Name, p)
			}
		}
		if !slices.Contains(registry.AuditCategories, d.Audit) {
			t.Errorf("operation %q claims audit category %q, which is not registered", d.Name, d.Audit)
		}
	}
}

func TestSessionLaunchDescriptor(t *testing.T) {
	d, ok := registry.Lookup("session.launch")
	if !ok {
		t.Fatal("session.launch is not registered")
	}
	if d.Projectability != registry.LocalAdmin {
		t.Errorf("session.launch projectability = %q, want local_admin", d.Projectability)
	}
	if d.MCPTool != "" {
		t.Errorf("session.launch projects MCP tool %q; the initial generated MCP tool set excludes it", d.MCPTool)
	}
	if !slices.Equal(d.Permissions, []string{"session.manage"}) {
		t.Errorf("session.launch permissions = %v, want [session.manage]", d.Permissions)
	}
	if d.Audit != registry.AuditPrivilegedWrite {
		t.Errorf("session.launch audit = %q; every spawn attempt and resolution failure is audited", d.Audit)
	}
	for _, code := range []string{"preset.not_found", "launch.constraints_exhausted", "launch.no_eligible_candidate"} {
		if !slices.Contains(d.ErrorCodes, code) {
			t.Errorf("session.launch is missing stable error code %q", code)
		}
	}
	if !d.Milestone {
		t.Error("session.launch is not marked as a dogfood-milestone operation")
	}
}

// TestMCPProjectionHonorsProjectability: only deterministic operations may
// carry an MCP tool name — local_admin and human_llm_porcelain never
// appear in the generated tool set.
func TestMCPProjectionHonorsProjectability(t *testing.T) {
	for _, d := range registry.All() {
		if d.MCPTool != "" && d.Projectability != registry.Deterministic {
			t.Errorf("operation %q is %q but projects MCP tool %q", d.Name, d.Projectability, d.MCPTool)
		}
	}
	tools := registry.MCPTools()
	for _, banned := range []string{"duo_session_launch", "duo_timer_mutate", "duo_notification_requeue"} {
		if slices.Contains(tools, banned) {
			t.Errorf("initial MCP tool set contains %q, which the ledger excludes", banned)
		}
	}
}

func TestMilestoneCoverage(t *testing.T) {
	want := []string{
		"session.launch", "session.list", "session.inspect", "session.enroll",
		"session.detach", "session.reattach", "manifest.show", "doctor.run",
	}
	for _, name := range want {
		d, ok := registry.Lookup(name)
		if !ok {
			t.Errorf("dogfood-milestone operation %q is not registered", name)
			continue
		}
		if !d.Milestone {
			t.Errorf("operation %q is registered but not marked as a milestone operation", name)
		}
	}
}

// TestManifestOperationsDerivePurelyFromRegistry proves a consumer view is
// a projection of the table and nothing else: every manifest entry matches
// its descriptor field for field, in table order, with no extra entries.
func TestManifestOperationsDerivePurelyFromRegistry(t *testing.T) {
	all := registry.All()
	ops := registry.ManifestOperations()
	if len(ops) != len(all) {
		t.Fatalf("ManifestOperations has %d entries, registry has %d descriptors", len(ops), len(all))
	}
	for i, op := range ops {
		d := all[i]
		if op.Name != d.Name {
			t.Errorf("entry %d: name %q, descriptor %q", i, op.Name, d.Name)
		}
		if op.Projectability != string(d.Projectability) {
			t.Errorf("%s: projectability %q, descriptor %q", op.Name, op.Projectability, d.Projectability)
		}
		if op.RequestSchema != d.RequestSchema.String() {
			t.Errorf("%s: request_schema %q, descriptor %q", op.Name, op.RequestSchema, d.RequestSchema)
		}
		if op.ResultSchema != d.ResultSchema.String() {
			t.Errorf("%s: result_schema %q, descriptor %q", op.Name, op.ResultSchema, d.ResultSchema)
		}
		if op.Permissions == nil {
			t.Errorf("%s: permissions is null; duo.manifest/v1 wants an array", op.Name)
		}
		if !slices.Equal(op.Permissions, d.Permissions) && len(d.Permissions) != 0 {
			t.Errorf("%s: permissions %v, descriptor %v", op.Name, op.Permissions, d.Permissions)
		}
		if op.Idempotency != string(d.Idempotency) {
			t.Errorf("%s: idempotency %q, descriptor %q", op.Name, op.Idempotency, d.Idempotency)
		}
		if op.StreamFamily != d.StreamFamily {
			t.Errorf("%s: stream_family %q, descriptor %q", op.Name, op.StreamFamily, d.StreamFamily)
		}
		if op.Audit != string(d.Audit) {
			t.Errorf("%s: audit %q, descriptor %q", op.Name, op.Audit, d.Audit)
		}
	}
}
