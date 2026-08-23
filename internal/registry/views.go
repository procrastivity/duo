package registry

import "slices"

// This file holds the derived views §8 of the architecture asks for: the
// manifest operations array, the initial MCP tool set, and the fixture
// inventory all derive from the one table — no consumer keeps its own copy.

// ManifestOperation is one entry of the manifest's operations array, shaped
// to the duo.manifest/v1 operations item.
type ManifestOperation struct {
	Name           string   `json:"name"`
	Projectability string   `json:"projectability"`
	RequestSchema  string   `json:"request_schema"`
	ResultSchema   string   `json:"result_schema"`
	Permissions    []string `json:"permissions"`
	Idempotency    string   `json:"idempotency"`
	StreamFamily   string   `json:"stream_family,omitempty"`
	Audit          string   `json:"audit"`
}

// ManifestOperations derives the manifest's operations array purely from
// the registry table, in table order.
func ManifestOperations() []ManifestOperation {
	out := make([]ManifestOperation, 0, len(table))
	for _, d := range table {
		perms := d.Permissions
		if perms == nil {
			perms = []string{} // duo.manifest/v1 wants an array, not null
		}
		out = append(out, ManifestOperation{
			Name:           d.Name,
			Projectability: string(d.Projectability),
			RequestSchema:  d.RequestSchema.String(),
			ResultSchema:   d.ResultSchema.String(),
			Permissions:    slices.Clone(perms),
			Idempotency:    string(d.Idempotency),
			StreamFamily:   d.StreamFamily,
			Audit:          string(d.Audit),
		})
	}
	return out
}

// MCPTools derives the initial generated MCP tool set: the distinct tool
// names of every descriptor that projects into MCP, sorted. Operations the
// ledger excludes (session.launch, timer mutation, dead-letter requeue)
// simply carry no tool name and so never appear here.
func MCPTools() []string {
	var out []string
	for _, d := range table {
		if d.MCPTool != "" && !slices.Contains(out, d.MCPTool) {
			out = append(out, d.MCPTool)
		}
	}
	slices.Sort(out)
	return out
}

// FixtureFiles derives the fixture inventory: every contracts.FS fixture
// path a descriptor claims, sorted and deduplicated.
func FixtureFiles() []string {
	var out []string
	for _, d := range table {
		for _, f := range d.Fixtures {
			if !slices.Contains(out, f) {
				out = append(out, f)
			}
		}
	}
	slices.Sort(out)
	return out
}
