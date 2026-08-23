package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/procrastivity/duo/contracts"
)

// Contracts is the embedded contract snapshot's digest record: the planning
// repository commit the snapshot was synced from, and every synced file's
// sha256, both read verbatim from the embedded contracts/SOURCE manifest.
// This is how `duo manifest` reports schema and conformance digests
// (roadmap Stage 0 exit gate): the schemas/ rows are the schema digests and
// the fixtures/ rows (projection-cases.json included) are the conformance
// digests. The field is a chassis extra the duo.manifest/v1 root's
// additionalProperties permits, because the contract's public_schemas list
// carries family names only.
type Contracts struct {
	SourceSHA string         `json:"source_sha"`
	Files     []ContractFile `json:"files"`
}

// ContractFile is one synced contract file and its sha256 from SOURCE.
type ContractFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// loadContracts parses the embedded contracts/SOURCE manifest: a comment
// header, one "source-sha: <sha>" line, then "<sha256>  <path>" rows.
func loadContracts() (Contracts, error) {
	data, err := contracts.FS.ReadFile("SOURCE")
	if err != nil {
		return Contracts{}, fmt.Errorf("reading embedded contracts/SOURCE: %w", err)
	}

	var out Contracts
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		case strings.HasPrefix(line, "source-sha:"):
			out.SourceSHA = strings.TrimSpace(strings.TrimPrefix(line, "source-sha:"))
		default:
			fields := strings.Fields(line)
			if len(fields) != 2 {
				return Contracts{}, fmt.Errorf("contracts/SOURCE: unparseable row %q", line)
			}
			out.Files = append(out.Files, ContractFile{Path: fields[1], SHA256: fields[0]})
		}
	}
	if err := scanner.Err(); err != nil {
		return Contracts{}, fmt.Errorf("scanning contracts/SOURCE: %w", err)
	}
	if out.SourceSHA == "" || len(out.Files) == 0 {
		return Contracts{}, fmt.Errorf("contracts/SOURCE carries no source-sha or no file rows (run `make sync-contracts`)")
	}
	return out, nil
}
