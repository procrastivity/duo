package manifest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// StampFileName is the bookkeeping file a generated install writes
// alongside its generated tree: tool.version, schemaVersion, and a content
// checksum, recorded as one file covering the whole tree rather than one
// sidecar per generated file, since the two are equivalent (every generated
// file's checksum is a key in Files) and one file is what Drift and any
// uninstall verification both need to read anyway.
const StampFileName = ".duo-manifest-stamp.json"

// Stamp records what an install last generated: the tool version and
// manifest schema version at install time, and a checksum per generated
// file (relative path -> sha256), so a later run can tell whether the
// current binary would generate something different (Drift) or whether the
// tree on disk still matches what was installed.
type Stamp struct {
	ToolVersion   string            `json:"toolVersion"`
	SchemaVersion int               `json:"schemaVersion"`
	Files         map[string]string `json:"files"`
}

// ChecksumFiles computes a Stamp's Files map from a generated tree's
// content (relative path -> bytes), the shape both an installer (to write a
// stamp) and Drift's caller (to compare against one) need.
func ChecksumFiles(files map[string][]byte) map[string]string {
	sums := make(map[string]string, len(files))
	for path, data := range files {
		sums[path] = Checksum(data)
	}
	return sums
}

// WriteStamp writes stamp as StampFileName under dir.
func WriteStamp(dir string, stamp Stamp) error {
	b, err := json.MarshalIndent(stamp, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest: encoding stamp: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, StampFileName), b, 0o644); err != nil {
		return fmt.Errorf("manifest: writing stamp: %w", err)
	}
	return nil
}

// ReadStamp reads the stamp under dir, reporting whether one was present.
func ReadStamp(dir string) (Stamp, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, StampFileName))
	if os.IsNotExist(err) {
		return Stamp{}, false, nil
	}
	if err != nil {
		return Stamp{}, false, fmt.Errorf("manifest: reading stamp: %w", err)
	}
	var stamp Stamp
	if err := json.Unmarshal(data, &stamp); err != nil {
		return Stamp{}, false, fmt.Errorf("manifest: parsing stamp: %w", err)
	}
	return stamp, true, nil
}
