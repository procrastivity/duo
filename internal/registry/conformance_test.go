package registry_test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/registry"
)

// These tests hold the registry to the synced contract set rather than to
// this package's own prose: the projection metadata must reproduce
// projection-cases.json, the manifest view must satisfy the duo.manifest/v1
// operations item, every operation a fixture names must be registered or
// explicitly deferred, and no other package may keep a second table.

// projectionCases is the shape of the parts of projection-cases.json this
// test reads: the semantic operation and the three projections' metadata.
type projectionCases struct {
	Cases []struct {
		Name     string `json:"name"`
		Semantic struct {
			Operation string `json:"operation"`
		} `json:"semantic"`
		CLI struct {
			Arguments []string `json:"arguments"`
		} `json:"cli"`
		MCP struct {
			Tool string `json:"tool"`
		} `json:"mcp"`
		Presentation struct {
			Method string `json:"method"`
			Path   string `json:"path"`
		} `json:"presentation"`
	} `json:"cases"`
}

func loadProjectionCases(t *testing.T) projectionCases {
	t.Helper()
	data, err := contracts.FS.ReadFile("fixtures/duo-external-v1/projection-cases.json")
	if err != nil {
		t.Fatalf("reading projection-cases.json: %v", err)
	}
	var cases projectionCases
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("parsing projection-cases.json: %v", err)
	}
	if len(cases.Cases) == 0 {
		t.Fatal("projection-cases.json carries no cases")
	}
	return cases
}

// pathMatchesTemplate reports whether a concrete conformance-case path
// satisfies the registry's route template: same segment count, with a
// {placeholder} segment matching any non-empty concrete segment. The case's
// query string is a per-request detail, not part of the resource template.
func pathMatchesTemplate(template, concrete string) bool {
	concrete, _, _ = strings.Cut(concrete, "?")
	tSegs := strings.Split(strings.Trim(template, "/"), "/")
	cSegs := strings.Split(strings.Trim(concrete, "/"), "/")
	if len(tSegs) != len(cSegs) {
		return false
	}
	for i, seg := range tSegs {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			if cSegs[i] == "" {
				return false
			}
			continue
		}
		if seg != cSegs[i] {
			return false
		}
	}
	return true
}

// TestProjectionCasesMatchRegistry proves the registry is the source the
// three projections agree on: for every synced conformance case, the
// operation is registered and its CLI verb path, MCP tool name, and route
// are the ones the case exercises.
func TestProjectionCasesMatchRegistry(t *testing.T) {
	for _, c := range loadProjectionCases(t).Cases {
		d, ok := registry.Lookup(c.Semantic.Operation)
		if !ok {
			t.Errorf("case %q exercises operation %q, which is not registered", c.Name, c.Semantic.Operation)
			continue
		}

		if len(c.CLI.Arguments) < len(d.CLI) || !slices.Equal(c.CLI.Arguments[:len(d.CLI)], d.CLI) {
			t.Errorf("case %q CLI arguments %v do not start with the registered verb path %v",
				c.Name, c.CLI.Arguments, d.CLI)
		}

		if c.MCP.Tool != d.MCPTool {
			t.Errorf("case %q MCP tool %q, registry has %q for %q",
				c.Name, c.MCP.Tool, d.MCPTool, d.Name)
		}

		switch {
		case c.Presentation.Path == "":
			if d.Route != nil {
				t.Errorf("case %q declares no presentation projection, but %q registers route %s %s",
					c.Name, d.Name, d.Route.Method, d.Route.Path)
			}
		case d.Route == nil:
			t.Errorf("case %q exercises %s %s, but %q registers no route",
				c.Name, c.Presentation.Method, c.Presentation.Path, d.Name)
		default:
			if c.Presentation.Method != d.Route.Method {
				t.Errorf("case %q method %q, registry has %q for %q",
					c.Name, c.Presentation.Method, d.Route.Method, d.Name)
			}
			if !pathMatchesTemplate(d.Route.Path, c.Presentation.Path) {
				t.Errorf("case %q path %q does not match the registered template %q for %q",
					c.Name, c.Presentation.Path, d.Route.Path, d.Name)
			}
		}
	}
}

// fixtureOperations collects every operation name the synced fixtures claim:
// each envelope fixture's top-level "operation", plus the names inside the
// manifest fixture's operations array. Nested occurrences are per-request
// payload detail, not an operation claim, so they are not read.
func fixtureOperations(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}

	err := fs.WalkDir(contracts.FS, "fixtures", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".json") {
			return err
		}
		data, readErr := contracts.FS.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var doc struct {
			Operation  string `json:"operation"`
			Operations []struct {
				Name string `json:"name"`
			} `json:"operations"`
		}
		if json.Unmarshal(data, &doc) != nil {
			return nil // not an object-shaped fixture; nothing to claim
		}
		if doc.Operation != "" {
			out[doc.Operation] = path
		}
		for _, op := range doc.Operations {
			if op.Name != "" {
				out[op.Name] = path
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking embedded fixtures: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("no fixture names an operation; the scan is not reading the embedded set")
	}
	return out
}

// TestFixtureOperationsAreRegisteredOrDeferred enforces the row policy in
// docs/registry/decisions.md: an operation the synced contract set names is
// either a row or an entry in the deferred list with a reason. A newly
// synced family therefore fails the suite until someone decides which.
func TestFixtureOperationsAreRegisteredOrDeferred(t *testing.T) {
	deferred := registry.DeferredOperations()
	for name, path := range fixtureOperations(t) {
		if _, ok := registry.Lookup(name); ok {
			continue
		}
		reason, isDeferred := deferred[name]
		if !isDeferred {
			t.Errorf("fixture %s names operation %q, which is neither registered nor deferred "+
				"(add a row, or a deferredOperations entry with the reason)", path, name)
			continue
		}
		if reason == "" {
			t.Errorf("operation %q is deferred with no reason recorded", name)
		}
	}

	// The deferred list is a record of real gaps, not a graveyard: a name
	// that later gets a row must leave it.
	for name := range deferred {
		if _, ok := registry.Lookup(name); ok {
			t.Errorf("operation %q is both registered and listed as deferred", name)
		}
	}
}

// TestManifestOperationsMatchContractSchema checks the derived manifest view
// against the embedded duo.manifest/v1 operations item: every required key
// present, every closed enum honored. The binary carries no JSON Schema
// validator, so this reads the schema's required list and enums directly
// rather than restating them.
func TestManifestOperationsMatchContractSchema(t *testing.T) {
	data, err := contracts.FS.ReadFile("schemas/duo-manifest-v1.schema.json")
	if err != nil {
		t.Fatalf("reading duo-manifest-v1.schema.json: %v", err)
	}
	var schema struct {
		Properties struct {
			Operations struct {
				Items struct {
					Required   []string `json:"required"`
					Properties map[string]struct {
						Enum []string `json:"enum"`
					} `json:"properties"`
				} `json:"items"`
			} `json:"operations"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(data, &schema); err != nil {
		t.Fatalf("parsing duo-manifest-v1.schema.json: %v", err)
	}
	item := schema.Properties.Operations.Items
	if len(item.Required) == 0 {
		t.Fatal("the manifest schema's operations item lists no required keys; the test is reading the wrong node")
	}

	for _, op := range registry.ManifestOperations() {
		encoded, err := json.Marshal(op)
		if err != nil {
			t.Fatalf("marshalling %q: %v", op.Name, err)
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			t.Fatalf("decoding %q: %v", op.Name, err)
		}
		for _, key := range item.Required {
			if _, ok := fields[key]; !ok {
				t.Errorf("manifest entry %q omits required key %q", op.Name, key)
			}
		}
		for key, prop := range item.Properties {
			if len(prop.Enum) == 0 {
				continue
			}
			value, ok := fields[key].(string)
			if !ok {
				continue // absent optional key, or a non-string this enum does not govern
			}
			if !slices.Contains(prop.Enum, value) {
				t.Errorf("manifest entry %q has %s = %q, outside the schema enum %v", op.Name, key, value, prop.Enum)
			}
		}
	}
}

// TestLaunchConstraintsExhaustedIsRegistered pins the code the step's
// acceptance names explicitly: registered in the stable set under class
// invalid (duo-vnext-access-errors-audit.md §4.1), claimed by session.launch,
// and exemplified by a synced fixture.
func TestLaunchConstraintsExhaustedIsRegistered(t *testing.T) {
	const code = "launch.constraints_exhausted"

	class, ok := registry.StableErrorCodes()[code]
	if !ok {
		t.Fatalf("%q is not in the registered stable error-code set", code)
	}
	if class != registry.ErrorClass("invalid") {
		t.Errorf("%q maps to class %q, want invalid", code, class)
	}

	d, ok := registry.Lookup("session.launch")
	if !ok {
		t.Fatal("session.launch is not registered")
	}
	if !slices.Contains(d.ErrorCodes, code) {
		t.Errorf("session.launch does not claim %q", code)
	}

	const fixture = "fixtures/duo-external-v1/session-launch-exhausted.json"
	if !slices.Contains(d.Fixtures, fixture) {
		t.Errorf("session.launch does not carry the %s fixture", fixture)
	}
	data, err := contracts.FS.ReadFile(fixture)
	if err != nil {
		t.Fatalf("reading %s: %v", fixture, err)
	}
	var envelope struct {
		Error struct {
			Class string `json:"class"`
			Code  string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("parsing %s: %v", fixture, err)
	}
	if envelope.Error.Code != code {
		t.Errorf("%s carries code %q, want %q", fixture, envelope.Error.Code, code)
	}
	if envelope.Error.Class != string(class) {
		t.Errorf("%s classes %q as %q, registry says %q", fixture, code, envelope.Error.Class, class)
	}
}

// moduleRoot walks up from the package directory to the directory holding
// go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod found above the package directory")
		}
		dir = parent
	}
}

// duplicateTableThreshold is how many distinct registered operation names a
// single Go file outside internal/registry may mention as string literals
// before it reads as a second table. One or two literals is a call site
// naming the operation it implements; three or more is an inventory, and an
// inventory must come from registry.All() (docs/registry/decisions.md, "the
// single-source rule").
const duplicateTableThreshold = 3

// TestNoDuplicateOperationTableOutsideRegistry is the repo-wide half of the
// single-source rule: no package other than internal/registry may enumerate
// the operation set. It is a tripwire, not a proof — a consumer that needs
// the whole set derives it from a view function, and then names nothing.
func TestNoDuplicateOperationTableOutsideRegistry(t *testing.T) {
	root := moduleRoot(t)
	registryDir := filepath.Join(root, "internal", "registry")

	names := make([]string, 0, len(registry.All()))
	for _, d := range registry.All() {
		names = append(names, d.Name)
	}

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch {
			case d.Name() == ".git", d.Name() == "bin", d.Name() == "dist":
				return filepath.SkipDir
			case path == registryDir:
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		source := string(data)

		var mentioned []string
		for _, name := range names {
			if strings.Contains(source, `"`+name+`"`) {
				mentioned = append(mentioned, name)
			}
		}
		if len(mentioned) >= duplicateTableThreshold {
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s names %d registered operations as literals (%v): "+
				"an operation inventory outside internal/registry is a second table — "+
				"derive it from registry.All() instead", rel, len(mentioned), mentioned)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the module: %v", err)
	}
}
