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

// parseManifest reads MANIFEST: one "sha256  path" line per contract file.
func parseManifest(t *testing.T) (files map[string]string) {
	t.Helper()
	data, err := FS.ReadFile("MANIFEST")
	if err != nil {
		t.Fatalf("reading embedded MANIFEST: %v (run `make contracts-manifest`)", err)
	}

	files = map[string]string{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "" || strings.HasPrefix(line, "#"):
			continue
		default:
			// sha256sum output: "<64 hex>  <path>".
			parts := strings.SplitN(line, "  ", 2)
			if len(parts) != 2 || len(parts[0]) != 64 {
				t.Fatalf("MANIFEST line not sha256sum-shaped: %q", line)
			}
			files[parts[1]] = parts[0]
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanning MANIFEST: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("MANIFEST lists no files")
	}
	return files
}

// TestEmbeddedContractsMatchSource walks the embedded FS and verifies every
// file MANIFEST lists exists with a matching sha256, and that nothing beyond
// MANIFEST's list (and MANIFEST itself) is embedded.
func TestEmbeddedContractsMatchSource(t *testing.T) {
	want := parseManifest(t)

	seen := map[string]bool{}
	err := fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || path == "MANIFEST" {
			return nil
		}
		wantSum, listed := want[path]
		if !listed {
			return fmt.Errorf("embedded file %q is not listed in MANIFEST", path)
		}
		data, err := FS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading embedded %q: %w", path, err)
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != wantSum {
			return fmt.Errorf("embedded %q sha256 = %s, MANIFEST says %s", path, got, wantSum)
		}
		seen[path] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	for path := range want {
		if !seen[path] {
			t.Errorf("MANIFEST lists %q but it is not embedded", path)
		}
	}
}
