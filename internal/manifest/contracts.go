package manifest

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"

	"github.com/procrastivity/duo/contracts"
)

// Contracts is the digest record of the contract set authored in this
// repository: every contract file's sha256, read verbatim from the
// embedded contracts/MANIFEST. This is how `duo manifest` reports schema
// and conformance digests (roadmap Stage 0 exit gate): the schemas/ rows
// are the schema digests and the fixtures/ rows (projection-cases.json
// included) are the conformance digests. The field is a chassis extra the
// duo.manifest/v1 root's additionalProperties permits, because the
// contract's public_schemas list carries family names only.
type Contracts struct {
	Files []ContractFile `json:"files"`
}

// ContractFile is one contract file and its sha256 from MANIFEST.
type ContractFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// loadContracts parses the embedded contracts/MANIFEST: comment lines, then
// "<sha256>  <path>" rows.
func loadContracts() (Contracts, error) {
	data, err := contracts.FS.ReadFile("MANIFEST")
	if err != nil {
		return Contracts{}, fmt.Errorf("reading embedded contracts/MANIFEST: %w", err)
	}

	var out Contracts
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
		default:
			fields := strings.Fields(line)
			if len(fields) != 2 {
				return Contracts{}, fmt.Errorf("contracts/MANIFEST: unparseable row %q", line)
			}
			out.Files = append(out.Files, ContractFile{Path: fields[1], SHA256: fields[0]})
		}
	}
	if err := scanner.Err(); err != nil {
		return Contracts{}, fmt.Errorf("scanning contracts/MANIFEST: %w", err)
	}
	if len(out.Files) == 0 {
		return Contracts{}, fmt.Errorf("contracts/MANIFEST: no file rows (run `make contracts-manifest`)")
	}
	return out, nil
}
