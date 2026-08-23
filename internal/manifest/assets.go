package manifest

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/procrastivity/duo/internal/asset"

	rootassets "github.com/procrastivity/duo/assets"
)

// walkAssets lists every file under the shipped assets/ tree with a sha256
// checksum — the same tree the chassis asset-resolution chain reads
// (<prefix>/share/duo/assets at runtime if installed, else the embedded
// fallback baked in at build time; never the user-override directory, which
// is not shipped content). Adding an asset file changes this list; editing
// one changes only its checksum.
func walkAssets() ([]Asset, error) {
	if dir, err := asset.DefaultDir(); err == nil {
		if entries, ok, err := walkDiskDir(dir); err != nil {
			return nil, err
		} else if ok {
			return entries, nil
		}
	}
	return walkEmbedded()
}

func walkDiskDir(dir string) ([]Asset, bool, error) {
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, false, nil
	}

	var out []Asset
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isShippedAsset(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out = append(out, Asset{Path: filepath.ToSlash(rel), SHA256: Checksum(data)})
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("manifest: walking shipped asset dir %q: %w", dir, err)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, true, nil
}

func walkEmbedded() ([]Asset, error) {
	var out []Asset
	err := fs.WalkDir(rootassets.FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isShippedAsset(path) {
			return nil
		}
		data, err := rootassets.FS.ReadFile(path)
		if err != nil {
			return err
		}
		out = append(out, Asset{Path: path, SHA256: Checksum(data)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("manifest: walking embedded asset fallback: %w", err)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// isShippedAsset excludes the assets package's own Go source (assets.go's
// //go:embed directive matches every non-hidden file in the directory,
// including itself and any future doc.go) — package infrastructure, not
// shipped asset content.
func isShippedAsset(path string) bool {
	return !strings.HasSuffix(path, ".go")
}

// Checksum returns data's sha256 hex digest — the checksum shape used
// throughout this package (the asset list, ChecksumFiles, Drift) and by
// any generator verifying an installed tree against its Stamp.
func Checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
