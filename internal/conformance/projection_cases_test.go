package conformance

import (
	"encoding/json"
	"testing"

	"github.com/procrastivity/duo/contracts"
	"github.com/procrastivity/duo/internal/registry"
)

// projectionCasesFixture is the shape of projection-cases.json this test
// reads. internal/registry/conformance_test.go already replays this
// fixture's metadata half — the CLI verb path, MCP tool name, and route
// template each case names must match the registry row. This test covers
// the other half: decoding the CLI argument vector, the MCP tool
// arguments, and the presentation request (path + query + body) each case
// carries, and proving all three decode to the same canonical value as
// semantic.input.
type projectionCasesFixture struct {
	Cases []struct {
		Name     string `json:"name"`
		Semantic struct {
			Operation string         `json:"operation"`
			Input     map[string]any `json:"input"`
		} `json:"semantic"`
		CLI struct {
			Arguments []string `json:"arguments"`
		} `json:"cli"`
		MCP struct {
			Tool      string         `json:"tool"`
			Arguments map[string]any `json:"arguments"`
		} `json:"mcp"`
		Presentation struct {
			Method string         `json:"method"`
			Path   string         `json:"path"`
			Body   map[string]any `json:"body"`
		} `json:"presentation"`
	} `json:"cases"`
}

// caseArgSpecs maps each projection-cases.json case's stable "name" to how
// its CLI/query arguments decode. See CaseArgSpec's doc comment: this is
// per-case request shape, not an integration-name branch.
var caseArgSpecs = map[string]CaseArgSpec{
	"session_list":    {},
	"session_inspect": {Positional: []string{"session_id"}},
	"conversation_history": {
		Positional: []string{"session_id"},
		Numeric:    map[string]bool{"limit": true},
	},
	"conversation_subscribe":          {Positional: []string{"session_id"}},
	"runtime_configuration_subscribe": {Positional: []string{"session_id"}},
	"prompt_deliver": {
		Positional: []string{"session_id"},
		FlagAlias:  map[string]string{"runtime-instance": "runtime_instance_id"},
	},
	"command_inspect": {Positional: []string{"command_id"}},
	"lease_acquire": {
		Positional: []string{"session_id"},
		FlagAlias:  map[string]string{"runtime-instance": "runtime_instance_id"},
		Numeric:    map[string]bool{"ttl_ms": true},
	},
	"terminal_read": {Positional: []string{"session_id"}},
}

func loadProjectionCasesFixture(t *testing.T) projectionCasesFixture {
	t.Helper()
	data, err := contracts.FS.ReadFile("fixtures/duo-external-v1/projection-cases.json")
	if err != nil {
		t.Fatalf("reading projection-cases.json: %v", err)
	}
	var f projectionCasesFixture
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parsing projection-cases.json: %v", err)
	}
	if len(f.Cases) == 0 {
		t.Fatal("projection-cases.json carries no cases")
	}
	return f
}

// TestProjectionCasesValueEquality is the step's required
// "projection-cases.json must pass" check, VALUE half: for every case,
// decode the CLI arguments, the MCP tool arguments, and the presentation
// request into Canonical, and assert every one equals semantic.input.
func TestProjectionCasesValueEquality(t *testing.T) {
	fixture := loadProjectionCasesFixture(t)

	var covered int
	for _, c := range fixture.Cases {
		c := c
		t.Run(c.Name, func(t *testing.T) {
			spec, known := caseArgSpecs[c.Name]
			if !known {
				t.Fatalf("case %q has no CaseArgSpec; add one to caseArgSpecs", c.Name)
			}
			d, ok := registry.Lookup(c.Semantic.Operation)
			if !ok {
				t.Fatalf("case %q exercises unregistered operation %q", c.Name, c.Semantic.Operation)
			}
			if d.Route == nil {
				t.Fatalf("case %q's operation %q registers no presentation route", c.Name, c.Semantic.Operation)
			}
			want := Canonical(c.Semantic.Input)
			if want == nil {
				want = Canonical{}
			}
			covered++

			cliGot, err := DecodeCLIRequestArgs(c.CLI.Arguments, d.CLI, spec)
			if err != nil {
				t.Fatalf("decoding CLI arguments %v: %v", c.CLI.Arguments, err)
			}
			if !equalCanonical(cliGot, want) {
				t.Errorf("CLI arguments decode to a different value than semantic.input:\n got:  %#v\n want: %#v", cliGot, want)
			}

			mcpGot := DecodeMCPRequestArgs(c.MCP.Arguments)
			if !equalCanonical(mcpGot, want) {
				t.Errorf("MCP arguments decode to a different value than semantic.input:\n got:  %#v\n want: %#v", mcpGot, want)
			}

			body := Canonical(c.Presentation.Body)
			presGot, err := DecodePresentationRequest(d.Route.Path, c.Presentation.Path, body, spec.Numeric)
			if err != nil {
				t.Fatalf("decoding presentation request %s %s: %v", c.Presentation.Method, c.Presentation.Path, err)
			}
			if !equalCanonical(presGot, want) {
				t.Errorf("presentation request decodes to a different value than semantic.input:\n got:  %#v\n want: %#v", presGot, want)
			}
		})
	}

	if covered != len(fixture.Cases) {
		t.Fatalf("covered %d of %d cases; every case must run", covered, len(fixture.Cases))
	}
}
