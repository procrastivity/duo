package contracts

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

// parseSource reads SOURCE's manifest: the source-sha line plus one
// "sha256  path" line per synced file.
func parseSource(t *testing.T) (sha string, files map[string]string) {
	t.Helper()
	data, err := FS.ReadFile("SOURCE")
	if err != nil {
		t.Fatalf("reading embedded SOURCE: %v (run `make sync-contracts`)", err)
	}

	files = map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		case strings.HasPrefix(line, "source-sha: "):
			sha = strings.TrimPrefix(line, "source-sha: ")
		default:
			// sha256sum output: "<64 hex>  <path>".
			parts := strings.SplitN(line, "  ", 2)
			if len(parts) != 2 || len(parts[0]) != 64 {
				t.Fatalf("SOURCE line not sha256sum-shaped: %q", line)
			}
			files[parts[1]] = parts[0]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning SOURCE: %v", err)
	}
	if sha == "" {
		t.Fatal("SOURCE carries no source-sha line")
	}
	if len(files) == 0 {
		t.Fatal("SOURCE lists no files")
	}
	return sha, files
}

// TestEmbeddedContractsMatchSource walks the embedded FS and verifies every
// file SOURCE lists exists with a matching sha256, and that nothing beyond
// SOURCE's list (and SOURCE itself) is embedded.
func TestEmbeddedContractsMatchSource(t *testing.T) {
	_, want := parseSource(t)

	seen := map[string]bool{}
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path == "SOURCE" {
			return nil
		}
		wantSum, listed := want[path]
		if !listed {
			return fmt.Errorf("embedded file %q is not listed in SOURCE", path)
		}
		data, err := FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded %q: %w", path, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != wantSum {
			return fmt.Errorf("embedded %q sha256 = %s, SOURCE says %s", path, got, wantSum)
		}
		seen[path] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for path := range want {
		if !seen[path] {
			t.Errorf("SOURCE lists %q but it is not embedded", path)
		}
	}
}
